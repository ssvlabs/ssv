package runner

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/qa/faults"
)

// QA fault menu, branch qa/gloas-m3-fault-menu: the section 4 block values that reshape the Gloas
// proposal value (menu block-wrong-slot, block-wrong-proposer-index, payload-root-on-external,
// payload-root-zero-selfbuild, payload-root-fabricated). block-wrong-version stays in gloasBlockVersion,
// because it changes the ProposerConsensusData wrapper, not the GloasProposalData inside it.

// fabricatedExternalPayloadRoot is the non-zero payload_root payload-root-on-external stamps on an
// external bid. Any non-zero value trips the pin; a fixed one is easy to grep in a decoded value.
var fabricatedExternalPayloadRoot = phase0.Root{0xfa, 0x17}

// proposalFault is what applyProposalFault did to one slot's value. At most one of fired and
// notApplicable is set; neither is set under FAULT=none or a fault that is not a §4 value fault.
type proposalFault struct {
	// override is what the faulted node's own value check needs. decide checks the local input on every
	// operator, leader or not, so a pin-tripping value needs PermissiveCheck on followers too.
	override faults.ValueCheckOverride
	// fired carries the faults.Fired fields (the caller adds the slot) when the value was changed.
	fired []zap.Field
	// notApplicable is the faults.NotApplicable reason when the active fault had nothing to act on.
	notApplicable string
}

// applyProposalFault applies the active §4 value fault to p, the proposal this operator is about to start
// its instance with, in place. It is pure apart from mutating p, so the caller owns the logging and the
// checker swap.
//
// Honest-side strings are shared: pin 5 ("gloas payload_root presence does not match self-build bid")
// answers both payload-root-on-external and payload-root-zero-selfbuild, so every fired line carries
// honest_self_build to tell them apart from the faulted node's own evidence.
func applyProposalFault(p *gloas.GloasProposalData, duty *spectypes.ValidatorDuty) proposalFault {
	if !faults.Enabled() {
		return proposalFault{}
	}
	selfBuild := p.SelfBuild()

	switch {
	case faults.Is(faults.BlockWrongSlot):
		// PRO-10 pin 3. The duty in the wrapper stays honest, so pins 1 and 2 pass and pin 3 is the first to
		// fire. +1 keeps the epoch sane. producedRoot already holds the honest root; harmless, because this
		// value can never be decided.
		honest := p.Block.Slot
		p.Block.Slot = duty.Slot + 1
		return proposalFault{override: faults.PermissiveCheck, fired: []zap.Field{
			zap.Uint64("honest_block_slot", uint64(honest)),
			zap.Uint64("stamped_block_slot", uint64(p.Block.Slot)),
			zap.Bool("honest_self_build", selfBuild),
		}}

	case faults.Is(faults.BlockWrongProposerIndex):
		// PRO-10 pin 4. The slot stays honest, so pin 3 passes.
		honest := p.Block.ProposerIndex
		p.Block.ProposerIndex = duty.ValidatorIndex + 1
		return proposalFault{override: faults.PermissiveCheck, fired: []zap.Field{
			zap.Uint64("honest_proposer_index", uint64(honest)),
			zap.Uint64("stamped_proposer_index", uint64(p.Block.ProposerIndex)),
			zap.Bool("honest_self_build", selfBuild),
		}}

	case faults.Is(faults.PayloadRootOnExternal):
		// PRO-10 / EPE-04 pin 5, external half: a non-zero payload_root on a bid the beacon node took from an
		// external builder. Only a natural external bid is stamped (decision D1, 2026-09-28): on a self-build
		// slot no bid is forged, so on a rig without a builder (ssv-mini) this value injects nothing and says
		// so every slot, and a missing 🧪 line still means a broken rig.
		if selfBuild {
			return proposalFault{notApplicable: "self-build slot: no external bid to stamp"}
		}
		p.PayloadRoot = fabricatedExternalPayloadRoot
		return proposalFault{override: faults.PermissiveCheck, fired: []zap.Field{
			zap.Bool("honest_self_build", false),
			zap.Bool("synthetic_external", false),
			zap.Uint64("builder_index", uint64(p.Block.Body.SignedExecutionPayloadBid.Message.BuilderIndex)),
			zap.Stringer("stamped_payload_root", p.PayloadRoot),
		}}

	case faults.Is(faults.PayloadRootZeroSelfBuild):
		// PRO-10 / EPE-04 pin 5, self-build half. The produced envelope is kept: the honest round-2 value
		// releases it (matchProducedEnvelope).
		if !selfBuild {
			return proposalFault{notApplicable: "external bid: payload_root is already zero"}
		}
		realRoot := p.PayloadRoot
		p.PayloadRoot = phase0.Root{}
		return proposalFault{override: faults.PermissiveCheck, fired: []zap.Field{
			zap.Bool("honest_self_build", true),
			zap.Stringer("real_payload_root", realRoot),
			zap.Stringer("stamped_payload_root", p.PayloadRoot),
		}}

	case faults.Is(faults.PayloadRootFabricated):
		// EPE-04 leg b / FLT-06 leg A: a non-zero payload_root that is not the real one. It passes all five
		// pins everywhere, so the honest check stays. Decided, it has every operator sign an envelope nobody
		// holds the payload for: the block is submitted, no envelope is published, and the builder
		// operator's match is "other" at Debug — not the "could not match" Warn, which needs an error.
		if !selfBuild {
			return proposalFault{notApplicable: "external bid: no payload_root to fabricate"}
		}
		realRoot := p.PayloadRoot
		p.PayloadRoot[0] ^= 0xff
		return proposalFault{override: faults.KeepHonestCheck, fired: []zap.Field{
			zap.Bool("honest_self_build", true),
			zap.Stringer("real_payload_root", realRoot),
			zap.Stringer("stamped_payload_root", p.PayloadRoot),
		}}
	}

	return proposalFault{}
}

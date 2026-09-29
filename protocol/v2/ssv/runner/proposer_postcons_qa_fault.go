package runner

import (
	"encoding/hex"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/qa/faults"
)

// QA fault menu section 6 (EPE-09, MSG-03): the proposer post-consensus packet. At a Gloas slot the
// honest packet is {block} on an external-bid slot and {block, envelope} on a self-build slot. The
// four postcons-* values reshape it just before the broadcast in ProcessConsensus:
//
//   - postcons-block-only and postcons-foreign-root REPLACE the honest packet. A second packet from
//     the same signer would hit the per-signer post-consensus limit (partial_validation.go,
//     ErrTooManyPartialSigMessage) — the wrong rule and a peer-score hit. Both are self-build only;
//     on an external-bid slot they log "qa fault not applicable" every slot.
//   - postcons-3-entries and postcons-mixed-index go ALONGSIDE the honest packet (honest first). The
//     forged packet fails pubsub semantics before duty logic, and a rejected packet never updates
//     signer state, so this operator's honest share still counts. Both are wire-invalid and need
//     FAULT_NO_LOCAL_VALIDATION=true to leave the sender.
//
// Before the Gloas fork no value injects: the pre-Gloas packet has one entry and the caps differ,
// so the oracles (got 3, want 2) would not hold.

// postconsFaultPlan is what the active section 6 value does to one honest packet.
type postconsFaultPlan struct {
	// packets are broadcast in order, each as its own post-consensus message. With no injection it
	// is exactly [][]{honest}.
	packets [][]*spectypes.PartialSignatureMessage
	// fired is true when the packets differ from the honest one; firedFields then describe it.
	fired       bool
	firedFields []zap.Field
	// notApplicable is set when the active value is a section 6 value that has nothing to act on at
	// this Gloas slot; the caller logs it so a silent slot always means a broken rig.
	notApplicable string
}

// postconsFaultPackets is the pure core of the section 6 hook. It never mutates honest.
func postconsFaultPackets(f faults.Fault, honest []*spectypes.PartialSignatureMessage, gloas, selfBuild bool) postconsFaultPlan {
	identity := postconsFaultPlan{packets: [][]*spectypes.PartialSignatureMessage{honest}}
	switch f {
	case faults.PostConsBlockOnly, faults.PostCons3Entries, faults.PostConsMixedIndex, faults.PostConsForeignRoot:
	default:
		return identity
	}
	if !gloas || len(honest) == 0 {
		return identity
	}

	base := []zap.Field{
		zap.Bool("self_build", selfBuild),
		zap.Int("honest_entries", len(honest)),
		fields.ValidatorIndex(honest[0].ValidatorIndex),
	}
	selfBuildPacket := selfBuild && len(honest) == 2

	switch f {
	case faults.PostConsBlockOnly:
		if !selfBuildPacket {
			identity.notApplicable = notSelfBuildReason(selfBuild,
				"the honest packet carries only the block entry, there is no envelope entry to omit")
			return identity
		}
		forged := []*spectypes.PartialSignatureMessage{clonePartialSig(honest[0])}
		return postconsFaultPlan{
			packets: [][]*spectypes.PartialSignatureMessage{forged},
			fired:   true,
			firedFields: append(base,
				zap.Bool("replaces_honest", true),
				zap.Int("packets", 1),
				zap.Int("entries", len(forged)),
				zap.String("block_root", hexRoot(honest[0].SigningRoot)),
				zap.String("omitted_envelope_root", hexRoot(honest[1].SigningRoot))),
		}

	case faults.PostConsForeignRoot:
		if !selfBuildPacket {
			identity.notApplicable = notSelfBuildReason(selfBuild,
				"the expected roots hold only the block root, so a second entry would fail "+
					"'wrong expected roots count' before the root match")
			return identity
		}
		foreign := clonePartialSig(honest[1])
		foreign.SigningRoot[0] ^= 0xff
		forged := []*spectypes.PartialSignatureMessage{clonePartialSig(honest[0]), foreign}
		return postconsFaultPlan{
			packets: [][]*spectypes.PartialSignatureMessage{forged},
			fired:   true,
			firedFields: append(base,
				zap.Bool("replaces_honest", true),
				zap.Int("packets", 1),
				zap.Int("entries", len(forged)),
				zap.String("block_root", hexRoot(honest[0].SigningRoot)),
				zap.String("honest_envelope_root", hexRoot(honest[1].SigningRoot)),
				zap.String("foreign_root", hexRoot(foreign.SigningRoot))),
		}

	case faults.PostCons3Entries:
		forged := make([]*spectypes.PartialSignatureMessage, 0, 3)
		for _, m := range honest {
			forged = append(forged, clonePartialSig(m))
		}
		for len(forged) < 3 {
			forged = append(forged, clonePartialSig(honest[0]))
		}
		return postconsFaultPlan{
			packets: [][]*spectypes.PartialSignatureMessage{honest, forged},
			fired:   true,
			firedFields: append(base,
				zap.Bool("replaces_honest", false),
				zap.Int("packets", 2),
				zap.Int("entries", len(forged))),
		}

	case faults.PostConsMixedIndex:
		forged := []*spectypes.PartialSignatureMessage{clonePartialSig(honest[0])}
		if len(honest) >= 2 {
			forged = append(forged, clonePartialSig(honest[1]))
		} else {
			// External-bid slot: pad to exactly two entries so the entry cap (2 at a Gloas proposer
			// slot) does not fire first and the leg measures the index rule, not the cap.
			forged = append(forged, clonePartialSig(honest[0]))
		}
		forgedIndex := honest[0].ValidatorIndex + 1
		forged[1].ValidatorIndex = forgedIndex
		return postconsFaultPlan{
			packets: [][]*spectypes.PartialSignatureMessage{honest, forged},
			fired:   true,
			firedFields: append(base,
				zap.Bool("replaces_honest", false),
				zap.Int("packets", 2),
				zap.Int("entries", len(forged)),
				zap.Uint64("forged_index", uint64(forgedIndex))),
		}
	}
	return identity
}

func notSelfBuildReason(selfBuild bool, why string) string {
	if selfBuild {
		// Defensive: a self-build packet always has two entries; say so rather than stay silent.
		return "self-build packet does not have exactly two entries; " + why
	}
	return "external-bid slot: " + why
}

func clonePartialSig(m *spectypes.PartialSignatureMessage) *spectypes.PartialSignatureMessage {
	c := *m
	c.PartialSignature = append(spectypes.Signature(nil), m.PartialSignature...)
	return &c
}

func hexRoot(r [32]byte) string { return hex.EncodeToString(r[:]) }

// qaPostconsPackets applies the active section 6 value to the honest post-consensus entries and logs
// the outcome. With FAULT=none (or any other value) it returns [][]{honest} and logs nothing.
func qaPostconsPackets(logger *zap.Logger, slot phase0.Slot, gloas, selfBuild bool, honest []*spectypes.PartialSignatureMessage) [][]*spectypes.PartialSignatureMessage {
	if !faults.Enabled() {
		return [][]*spectypes.PartialSignatureMessage{honest}
	}
	plan := postconsFaultPackets(faults.Active(), honest, gloas, selfBuild)
	switch {
	case plan.fired:
		faults.Fired(logger, append([]zap.Field{fields.Slot(slot)}, plan.firedFields...)...)
	case plan.notApplicable != "":
		faults.NotApplicable(logger, plan.notApplicable, fields.Slot(slot), zap.Bool("self_build", selfBuild))
	}
	return plan.packets
}

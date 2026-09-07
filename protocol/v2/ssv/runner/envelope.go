package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

var (
	_ Runner                         = (*EnvelopeProposerRunner)(nil)
	_ EnvelopeDisseminationProcessor = (*EnvelopeProposerRunner)(nil)
)

var (
	// errEnvelopeProposalNotDecided means this operator's §4 block instance has not decided the slot yet,
	// so it cannot bind a disseminated envelope; the message is retried until the decision lands.
	errEnvelopeProposalNotDecided = errors.New("no §4 decision recorded for the envelope slot yet")
	// errNoSelectedEnvelope means this operator has not selected an envelope to sign yet (no binding
	// dissemination has arrived), so it has no expected root to validate partial signatures against.
	errNoSelectedEnvelope = errors.New("no selected envelope")
)

// EnvelopeDisseminationProcessor is implemented by the runner that consumes
// SSVEnvelopeDisseminationMsgType messages (SIP #94 §6): the validator routes a decoded dissemination
// to it alongside the runner's partial-signature traffic.
type EnvelopeDisseminationProcessor interface {
	ProcessEnvelopeDissemination(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage, dissemination *spectypes.EnvelopeDissemination) error
}

// EnvelopeProposerRunner runs the §6 execution-payload-envelope signing duty (SIP #94 §6,
// RoleEnvelopeProposer=9) for the self-build proposer. It has NO consensus phase: once §4 decides,
// bid.block_hash pins exactly one valid envelope, so there is nothing to negotiate. The flow is one
// dissemination round plus one threshold-signing round:
//
//  1. The builder operator — the one whose own produceBlockV4 response is the §4-decided block, and so
//     the only one whose beacon node holds the payload — fetches its envelope, disseminates the blinded
//     form (SSVEnvelopeDisseminationMsgType), and signs it.
//  2. Every other operator content-selects the first disseminated envelope that binds to its own §4
//     decision (ssv.ProposedBlock.Binds), skipping any that do not, and signs its root under
//     DOMAIN_BEACON_BUILDER as an EnvelopePartialSig. The single signing round reuses the pre-consensus
//     container, the same shape as the PTC and proposer-preferences runners.
//  3. On quorum every operator reconstructs the signature; only the builder operator, whose produced
//     envelope blinds to the selected one, publishes the full SignedExecutionPayloadEnvelope.
type EnvelopeProposerRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	// proposedBlocks is the §4→§6 linkage store the proposer runner writes: the duty binds disseminated
	// envelopes against the slot's decision recorded here.
	proposedBlocks *ssv.ProposedBlocks

	// producedEnvelope is the full envelope this operator's beacon node built, held only by the builder
	// operator (nil otherwise) — the publish body; producedBlinded is its blinded form, compared against
	// the selected envelope to decide whether this operator publishes.
	producedEnvelope *gloas.ExecutionPayloadEnvelope
	producedBlinded  *gloas.BlindedExecutionPayloadEnvelope
	// selectedEnvelope is the disseminated envelope this operator chose to sign — the first arrival that
	// binds to its §4 decision. Incoming partial signatures are validated against its root; nil until
	// selection, so peers' partials are retried rather than dropped until then.
	selectedEnvelope *gloas.BlindedExecutionPayloadEnvelope
}

// EnvelopeProposerRunnerOptions bundles the dependencies required by NewEnvelopeProposerRunner.
type EnvelopeProposerRunnerOptions struct {
	BaseRunnerOptions

	// ProposedBlocks is the §4→§6 linkage store shared with the validator's proposer runner.
	ProposedBlocks *ssv.ProposedBlocks
}

func NewEnvelopeProposerRunner(opts EnvelopeProposerRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}
	if opts.ProposedBlocks == nil {
		// executeDuty and the binding checks read the §4 decision from it unconditionally.
		return nil, errors.New("must have a proposed blocks store")
	}

	return &EnvelopeProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType: spectypes.RoleEnvelopeProposer,
			NetworkConfig:  opts.NetworkConfig,
			Share:          opts.Share,
		},

		beacon:         opts.Beacon,
		network:        opts.Network,
		signer:         opts.Signer,
		operatorSigner: opts.OperatorSigner,
		proposedBlocks: opts.ProposedBlocks,
	}, nil
}

func (r *EnvelopeProposerRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	// Clear any prior duty's envelopes; executeDuty re-derives them, so a non-builder stays nil.
	r.producedEnvelope, r.producedBlinded, r.selectedEnvelope = nil, nil, nil
	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

// executeDuty runs the builder operator's side of the duty (SIP #94 §6): with the slot's §4 decision
// recorded and this operator's own produce response being the decided block, it fetches the envelope
// its beacon node built, disseminates the blinded form, and signs it. Every other operator disseminates
// nothing and signs only a binding dissemination it later receives (ProcessEnvelopeDissemination).
func (r *EnvelopeProposerRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	slot := validatorDuty.DutySlot()

	proposal, ok := r.proposedBlocks.Get(slot)
	if !ok {
		// The duty is started by the proposer after the §4 decision, so this is unexpected; stay running
		// so a dissemination can still be bound once the decision is recorded.
		logger.Debug("no §4 decision recorded for the envelope slot yet, waiting for a dissemination", fields.Slot(slot))
		return nil
	}
	if !proposal.ProducedLocally {
		// Only the beacon node that built the decided block holds its payload, so on a cluster whose
		// operators run separate beacon nodes every non-builder waits for the builder's dissemination.
		logger.Debug("not the builder operator for this slot, waiting for the builder's dissemination", fields.Slot(slot))
		return nil
	}

	envelope, err := r.beacon.GetExecutionPayloadEnvelope(ctx, slot, proposal.BlockRoot)
	if err != nil {
		// The builder operator is the only one that can disseminate, so without its envelope the cluster
		// misses the slot's reveal (bounded, non-slashable; SIP #94 Security Considerations).
		return fmt.Errorf("get execution payload envelope: %w", err)
	}
	blinded, err := gloas.Blinded(envelope)
	if err != nil {
		return fmt.Errorf("blind execution payload envelope: %w", err)
	}
	r.producedEnvelope, r.producedBlinded = envelope, blinded

	if err := r.disseminate(ctx, slot, blinded); err != nil {
		return fmt.Errorf("disseminate envelope: %w", err)
	}
	logger.Debug("disseminated execution payload envelope", fields.Slot(slot))

	// The builder operator's own envelope binds by construction; anything else is a beacon-node bug.
	if !proposal.Binds(blinded) {
		return errors.New("own execution payload envelope does not bind to the decided block")
	}
	return r.selectAndSign(ctx, logger, validatorDuty, blinded)
}

// ProcessEnvelopeDissemination handles a disseminated blinded envelope (SIP #94 §6): it selects the first
// arrival that binds to this operator's §4 decision and signs it. Non-binding disseminations are skipped
// (the binding checks are runner concerns, not validation rules, so they carry no peer penalty), and
// further disseminations are ignored once an envelope is selected. A dissemination that arrives before
// the duty started, or before this operator's block instance decided, is retried rather than dropped: the
// builder broadcasts it once, so there may be no later copy.
func (r *EnvelopeProposerRunner) ProcessEnvelopeDissemination(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage, dissemination *spectypes.EnvelopeDissemination) error {
	if !r.hasDutyAssigned() {
		return NewRetryableError(ErrNoDutyAssigned)
	}
	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}
	switch {
	case dissemination.Slot > duty.Slot:
		return NewRetryableError(ErrFuturePartialSigMsg) // for a later duty this operator has not started yet
	case dissemination.Slot < duty.Slot:
		return nil // stale, for a slot already behind us
	}
	if r.hasDutySucceeded() || r.selectedEnvelope != nil {
		return nil // the slot's duty is done or already has its envelope
	}

	proposal, ok := r.proposedBlocks.Get(duty.Slot)
	if !ok {
		return NewRetryableError(errEnvelopeProposalNotDecided)
	}

	// Content-based selection: sign the first disseminated envelope that binds to the §4 decision,
	// skipping any that fail (SIP #94 §6).
	if !proposal.Binds(dissemination.Envelope) {
		logger.Debug("skipping disseminated envelope that does not bind to the decided block",
			fields.Slot(duty.Slot), zap.Uint64s("signers", signedMsg.OperatorIDs))
		return nil
	}
	return r.selectAndSign(ctx, logger, duty, dissemination.Envelope)
}

// ProcessPreConsensus runs the single threshold-signing round: it collects EnvelopePartialSig partial
// signatures over the selected envelope's root and, on quorum, reconstructs the signature. Only the
// builder operator, whose produced envelope blinds to the selected one, publishes the reveal.
func (r *EnvelopeProposerRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	if r.hasDutyAssigned() && !r.hasDutySucceeded() && r.selectedEnvelope == nil {
		// A peer's partial can arrive before this operator has selected an envelope to validate it
		// against (its dissemination or §4 decision is still in flight): retry rather than drop.
		return NewRetryableError(errNoSelectedEnvelope)
	}

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing envelope partial signature message: %w", err)
	}
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing the duty here; the quorum fires only once, so a
	// terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// only 1 root, verified in basePreConsensusMsgProcessing
	root := roots[0]
	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature is invalid, surface which partial signatures were at fault.
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got envelope signing quorum but it has invalid signatures: %w", err)
	}
	var signature phase0.BLSSignature
	copy(signature[:], fullSig)

	// Publish by content match: only the builder operator holds the full envelope behind the selected
	// blinded value; everyone else completes the duty without publishing (SIP #94 §6).
	built := r.builtSelectedEnvelope()
	recordEnvelopeBuildMatch(ctx, built)
	if !built {
		logger.Debug("envelope signature reconstructed; this operator did not build the envelope, not publishing", fields.Slot(duty.Slot))
		r.markDutySucceeded()
		return nil
	}

	signed := &gloas.SignedExecutionPayloadEnvelope{Message: r.producedEnvelope, Signature: signature}
	if err := r.beacon.SubmitExecutionPayloadEnvelope(ctx, signed); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleEnvelopeProposer)
		const errMsg = "could not submit execution payload envelope"
		logger.Error(errMsg, fields.Slot(duty.Slot), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(duty.Slot), spectypes.BNRoleEnvelopeProposer)
	r.markDutySucceeded()
	logger.Info("✅ published execution payload envelope", fields.Slot(duty.Slot))
	return nil
}

func (r *EnvelopeProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	return errors.New("no consensus phase for envelope proposer")
}

func (r *EnvelopeProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no post-consensus phase for envelope proposer")
}

// expectedPreConsensusRootsAndDomain returns the selected envelope's root under DOMAIN_BEACON_BUILDER:
// by root-equivalence it is the full envelope's signing root, so peers' partials over it are valid for
// the reveal. With nothing selected there is no expected root, and ProcessPreConsensus retries instead.
func (r *EnvelopeProposerRunner) expectedPreConsensusRootsAndDomain() ([]spectypes.HashRoot, phase0.DomainType, error) {
	if r.selectedEnvelope == nil {
		return nil, spectypes.DomainError, errNoSelectedEnvelope
	}
	return []spectypes.HashRoot{r.selectedEnvelope}, phase0.DomainType(spectypes.DomainBeaconBuilder), nil
}

func (r *EnvelopeProposerRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]spectypes.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no post-consensus roots for envelope proposer")
}

// selectAndSign records the envelope this operator signs, signs its progressive root under
// DOMAIN_BEACON_BUILDER (domain epoch = the duty slot's epoch), and broadcasts the EnvelopePartialSig.
func (r *EnvelopeProposerRunner) selectAndSign(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty, envelope *gloas.BlindedExecutionPayloadEnvelope) error {
	r.selectedEnvelope = envelope

	msg, err := signBeaconObject(ctx, r, r.NetworkConfig, duty, envelope, duty.Slot, phase0.DomainType(spectypes.DomainBeaconBuilder))
	if err != nil {
		return fmt.Errorf("could not sign blinded envelope: %w", err)
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.EnvelopePartialSig,
		Slot:     duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}
	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey, msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast envelope partial sig: %w", err)
	}
	logger.Debug("selected and signed execution payload envelope", fields.Slot(duty.Slot))
	return nil
}

// disseminate broadcasts the blinded envelope as an SSVEnvelopeDisseminationMsgType message, operator-signed
// and routed like the role's partial-signature traffic (SIP #94 §6).
func (r *EnvelopeProposerRunner) disseminate(ctx context.Context, slot phase0.Slot, envelope *gloas.BlindedExecutionPayloadEnvelope) error {
	dissemination := &spectypes.EnvelopeDissemination{Slot: slot, Envelope: envelope}
	data, err := dissemination.Encode()
	if err != nil {
		return fmt.Errorf("could not encode envelope dissemination: %w", err)
	}

	msgID := spectypes.NewValidatorMsgID(r.NetworkConfig.DomainTypeAtSlot(slot), r.GetShare().ValidatorPubKey, r.RunnerRoleType)
	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVEnvelopeDisseminationMsgType,
		MsgID:   msgID,
		Data:    data,
	}
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSVMessage: %w", err)
	}
	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}
	if err := r.network.BroadcastAtSlot(signed, slot); err != nil {
		return fmt.Errorf("could not broadcast envelope dissemination: %w", err)
	}
	return nil
}

// builtSelectedEnvelope reports whether this operator's own produced envelope blinds to the selected one —
// the publish-by-content-match: only the builder operator holds the payload behind the reconstructed
// signature.
func (r *EnvelopeProposerRunner) builtSelectedEnvelope() bool {
	if r.producedBlinded == nil || r.selectedEnvelope == nil {
		return false
	}
	produced, err := r.producedBlinded.Encode()
	if err != nil {
		return false
	}
	selected, err := r.selectedEnvelope.Encode()
	if err != nil {
		return false
	}
	return bytes.Equal(produced, selected)
}

func (r *EnvelopeProposerRunner) GetNetwork() protocolp2p.Network { return r.network }

func (r *EnvelopeProposerRunner) GetBeaconNode() beacon.BeaconNode { return r.beacon }

func (r *EnvelopeProposerRunner) GetShare() *spectypes.Share {
	// there is only one share
	for _, share := range r.Share {
		return share
	}
	return nil
}

func (r *EnvelopeProposerRunner) GetSigner() ekm.BeaconSigner { return r.signer }

func (r *EnvelopeProposerRunner) GetOperatorSigner() ssvtypes.OperatorSigner { return r.operatorSigner }

// Only BaseRunner is persisted; the produced/selected envelopes are transient per-duty state.
func (r *EnvelopeProposerRunner) MarshalJSON() ([]byte, error) {
	return marshalRunnerStateJSON(r.BaseRunner)
}

func (r *EnvelopeProposerRunner) UnmarshalJSON(data []byte) error {
	br, err := unmarshalRunnerStateJSON(data)
	if err != nil {
		return err
	}
	r.BaseRunner = br
	return nil
}

func (r *EnvelopeProposerRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *EnvelopeProposerRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

func (r *EnvelopeProposerRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode EnvelopeProposerRunner: %w", err)
	}
	return sha256.Sum256(marshaledRoot), nil
}

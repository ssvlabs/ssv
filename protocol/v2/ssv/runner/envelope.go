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
//     the only one holding its envelope, blobs, and KZG proofs — disseminates the blinded envelope
//     (SSVEnvelopeDisseminationMsgType) and signs it.
//  2. Every other operator content-selects the first disseminated envelope that binds to its own §4
//     decision (ssv.ProposedBlock.Binds), skipping any that do not, and signs its root under
//     DOMAIN_BEACON_BUILDER as an EnvelopePartialSig. The single signing round reuses the pre-consensus
//     container, the same shape as the PTC and proposer-preferences runners.
//  3. On quorum every operator reconstructs the signature; only the builder operator, whose produced
//     envelope blinds to the selected one, publishes the reveal with its blobs and KZG proofs.
type EnvelopeProposerRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner

	// proposedBlocks is the §4→§6 linkage store the proposer runner writes: the duty binds disseminated
	// envelopes against the slot's decision recorded here.
	proposedBlocks *ssv.ProposedBlocks

	// produced is the reveal data this operator's own produce response carried, held only by the builder
	// operator (nil otherwise) — the publish body; producedBlinded is its envelope's blinded form, compared
	// against the selected envelope to decide whether this operator publishes.
	produced        *gloas.ProducedEnvelope
	producedBlinded *gloas.BlindedExecutionPayloadEnvelope
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
	return r.baseStartNewNonBeaconDuty(ctx, logger, r, validatorDuty, quorum)
}

// executeDuty runs the builder operator's side of the duty (SIP #94 §6): with the slot's §4 decision
// recorded and this operator's own produce response being the decided block, it disseminates the blinded
// form of the envelope that response carried and signs it. Every other operator disseminates nothing and
// signs only a binding dissemination it later receives (ProcessEnvelopeDissemination).
func (r *EnvelopeProposerRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	slot := validatorDuty.DutySlot()

	// Single-slot runner: an accepted duty replaces the previous slot's envelopes (a non-builder stays
	// nil). This runs only once the duty passed the start guard, so a rejected duplicate start cannot
	// wipe an in-flight slot. A slot's envelope is due by half the slot (SIP #94 §6) and a stale
	// dissemination is dropped, so a validator's back-to-back self-build proposals never overlap here.
	r.releaseEnvelopes()

	proposal, ok := r.proposedBlocks.Get(slot)
	if !ok {
		// The duty is started by the proposer after the §4 decision, so this is unexpected; stay running
		// so a dissemination can still be bound once the decision is recorded.
		logger.Debug("no §4 decision recorded for the envelope slot yet, waiting for a dissemination", fields.Slot(slot))
		return nil
	}
	if !proposal.ProducedLocally {
		// Only the builder operator holds the payload, so every other operator waits for its dissemination.
		logger.Debug("not the builder operator for this slot, waiting for the builder's dissemination", fields.Slot(slot))
		return nil
	}
	// The runner takes the reveal data over from the store, so the blobs live in one place and are released
	// when this duty concludes rather than when the store's retention window evicts the decision.
	produced := r.proposedBlocks.TakeProducedEnvelope(slot)
	if produced == nil {
		// A self-build produce response is BlockContents (include_payload=true), so this is a beacon-node
		// fault — or the slot's reveal data was already taken, which the start guard rules out today. The
		// builder operator is the only one that can disseminate, so the cluster misses the slot's reveal
		// (bounded, non-slashable; SIP #94 Security Considerations).
		return errors.New("no reveal data for the decided self-build block: produceBlockV4 returned no payload (include_payload=true not honored) or the slot's reveal data was already taken")
	}

	blinded, err := gloas.Blinded(produced.Envelope)
	if err != nil {
		return fmt.Errorf("blind execution payload envelope: %w", err)
	}

	// The builder operator's own envelope binds by construction; anything else is a beacon-node fault. It
	// fails before disseminating, so no peer's one-per-slot budget is spent, and before the runner holds
	// the reveal data, so the failed duty keeps no blobs.
	if !proposal.Binds(blinded) {
		return errors.New("own execution payload envelope does not bind to the decided block")
	}
	r.produced, r.producedBlinded = produced, blinded

	if err := r.disseminate(ctx, slot, blinded); err != nil {
		return fmt.Errorf("disseminate envelope: %w", err)
	}
	logger.Debug("disseminated execution payload envelope", fields.Slot(slot))

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
	if r.hasDutyAssigned() && !r.hasDutySucceeded() && r.selectedEnvelope == nil && signedMsg.Slot == r.State.CurrentDuty.DutySlot() {
		// A peer's partial for this slot can arrive before this operator has selected an envelope to
		// validate it against (its dissemination or §4 decision is still in flight): retry rather than
		// drop. Partials for other slots take the base slot check below — a stale one is dropped, a
		// future one is retried on its own account.
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

	// Publish by content match: only the builder operator holds the reveal data behind the selected
	// blinded value; everyone else completes the duty without publishing (SIP #94 §6).
	built := r.builtSelectedEnvelope()
	recordEnvelopeBuildMatch(ctx, built)
	if !built {
		logger.Debug("envelope signature reconstructed; this operator did not build the envelope, not publishing", fields.Slot(duty.Slot))
		r.markDutySucceeded()
		r.releaseEnvelopes()
		return nil
	}

	if err := r.beacon.SubmitExecutionPayloadEnvelope(ctx, r.produced.Signed(signature)); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleEnvelopeProposer)
		const errMsg = "could not submit execution payload envelope"
		logger.Error(errMsg, fields.Slot(duty.Slot), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(duty.Slot), spectypes.BNRoleEnvelopeProposer)
	r.markDutySucceeded()
	r.releaseEnvelopes()
	logger.Info("✅ published execution payload envelope", fields.Slot(duty.Slot))
	return nil
}

// releaseEnvelopes drops the slot's envelopes — the produced reveal data, blobs included, its blinded form
// and the selected envelope — once the duty concluded or a new one starts, so they do not outlive their
// slot until the validator's next self-build proposal. A duty that never reaches quorum is the residual:
// it keeps its envelopes until the next accepted start.
func (r *EnvelopeProposerRunner) releaseEnvelopes() {
	r.produced, r.producedBlinded, r.selectedEnvelope = nil, nil, nil
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
// and routed like the role's partial-signature traffic (SIP #94 §6). The carrier rides SSVMessage.Data,
// which the spec caps (currently 726932 bytes; message validation enforces it on receipt). The blinded
// envelope is small except for ExecutionRequests, whose SSZ ceiling — 8192 deposit requests of 192 bytes —
// would exceed the cap; in practice every deposit request costs the deposit contract's gas, so a block's
// gas limit holds the list to the low thousands at most, a few hundred kilobytes and well under the cap.
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

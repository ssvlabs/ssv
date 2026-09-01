package runner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// EnvelopeProposerRunner runs the §6 execution-payload-envelope-signing duty (SIP #94 §6,
// RoleEnvelopeProposer=9). It is a second QBFT instance for the proposer's slot, started by the proposer
// only on the self-build path (external builders sign their own envelopes). The flow mirrors the proposer
// minus pre-consensus: executeDuty produces a BlindedExecutionPayloadEnvelope and runs QBFT over it;
// ProcessConsensus signs the decided blinded root under DOMAIN_BEACON_BUILDER and broadcasts a
// post-consensus partial signature; ProcessPostConsensus reconstructs the BLS signature and the builder
// publishes the full envelope.
type EnvelopeProposerRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	measurements   *dutyMeasurements

	// ValCheck validates the QBFT value (the blinded envelope). It is slot-specific (it matches the §4
	// root recorded for the duty's slot), so it is rebuilt per duty in executeDuty — as the committee
	// runner rebuilds its vote check — rather than fixed at construction.
	ValCheck ssv.ValueChecker

	// proposedBlockRoots gives executeDuty the §4-decided block root for the slot (the envelope's
	// BeaconBlockRoot), recorded by the proposer runner. Shared with ValCheck, which checks the same root.
	proposedBlockRoots *ssv.ProposedBlockRoots

	// cachedEnvelope holds the full envelope this operator fetched in produce. Post-consensus content-matches
	// it against the decided blinded value to detect whether this operator built it — only that operator
	// publishes the full SignedExecutionPayloadEnvelope.
	cachedEnvelope *gloas.ExecutionPayloadEnvelope
}

// EnvelopeProposerRunnerOptions bundles the dependencies required by NewEnvelopeProposerRunner.
type EnvelopeProposerRunnerOptions struct {
	BaseRunnerOptions

	QBFTController     *controller.Controller
	ProposedBlockRoots *ssv.ProposedBlockRoots
	HighestDecidedSlot phase0.Slot
}

func NewEnvelopeProposerRunner(opts EnvelopeProposerRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &EnvelopeProposerRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleEnvelopeProposer,
			NetworkConfig:      opts.NetworkConfig,
			Share:              opts.Share,
			QBFTController:     opts.QBFTController,
			highestDecidedSlot: opts.HighestDecidedSlot,
		},

		beacon:             opts.Beacon,
		network:            opts.Network,
		signer:             opts.Signer,
		operatorSigner:     opts.OperatorSigner,
		measurements:       newMeasurementsStore(),
		proposedBlockRoots: opts.ProposedBlockRoots,
	}, nil
}

func (r *EnvelopeProposerRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	return r.baseStartNewDuty(ctx, logger, r, validatorDuty, quorum)
}

// ProcessPreConsensus is unreachable: the envelope duty has no pre-consensus phase.
func (r *EnvelopeProposerRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre-consensus phase for envelope proposer")
}

func (r *EnvelopeProposerRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	// Reuse the existing span instead of generating a new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	decided, decidedValue, err := r.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, signedMsg, &gloas.EnvelopeConsensusData{})
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}
	// Decided returns true only once, so it is for the current running instance.
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleEnvelopeProposer)

	cd := decidedValue.(*gloas.EnvelopeConsensusData)

	blinded := &gloas.BlindedExecutionPayloadEnvelope{}
	if err := blinded.Decode(cd.DataSSZ); err != nil {
		return fmt.Errorf("could not decode blinded envelope from consensus data: %w", err)
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	// The blinded envelope's root equals the full envelope's, so this signature is valid for the full
	// SignedExecutionPayloadEnvelope. Signed under DOMAIN_BEACON_BUILDER (not DOMAIN_PROPOSER).
	span.AddEvent("signing blinded envelope")
	msg, err := signBeaconObject(ctx, r, r.NetworkConfig, duty, blinded, cd.Duty.Slot, spectypes.DomainBeaconBuilder)
	if err != nil {
		return fmt.Errorf("failed signing blinded envelope: %w", err)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     cd.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	r.measurements.StartPostConsensus()
	span.AddEvent("broadcasting post-consensus partial signature message")
	if err := r.signAndBroadcastPostConsensusMsg(r.GetNetwork(), r.operatorSigner, r.GetShare().ValidatorPubKey, postConsensusMsg); err != nil {
		return fmt.Errorf("can't broadcast partial post-consensus sig: %w", err)
	}

	return nil
}

func (r *EnvelopeProposerRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating a new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing post-consensus message: %w", err)
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

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleEnvelopeProposer)

	// only 1 root, verified by expectedPostConsensusRootsAndDomain
	root := roots[0]

	sig, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature.
		r.FallBackAndVerifyEachSignature(r.State.PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)

	cd := &gloas.EnvelopeConsensusData{}
	if err := cd.Decode(r.State.DecidedValue); err != nil {
		return fmt.Errorf("could not decode decided envelope consensus data: %w", err)
	}

	span.AddEvent("submitting execution payload envelope")
	return r.submitEnvelope(ctx, logger, cd, specSig)
}

// submitEnvelope publishes the signed execution-payload envelope. Only the operator whose cached envelope
// blinds to the decided value (content match) holds the full bytes to publish; the others just complete the
// duty. Unlike the §4 block path — where the bid-only block is itself the decided value, so every operator
// holds and re-submits it — the envelope's decided value is blinded; only its builder holds the full payload
// bytes, so content-match publication keeps a non-builder (whose cachedEnvelope is nil) from broadcasting an
// empty envelope.
func (r *EnvelopeProposerRunner) submitEnvelope(ctx context.Context, logger *zap.Logger, cd *gloas.EnvelopeConsensusData, sig phase0.BLSSignature) error {
	builtIt := r.builtDecidedEnvelope(cd.DataSSZ)
	recordEnvelopeBuildMatch(ctx, builtIt)
	if builtIt {
		signed := &gloas.SignedExecutionPayloadEnvelope{Message: r.cachedEnvelope, Signature: sig}
		if err := r.GetBeaconNode().SubmitExecutionPayloadEnvelope(ctx, signed); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleEnvelopeProposer)
			const errMsg = "could not submit execution payload envelope"
			logger.Error(errMsg, fields.Slot(cd.Duty.Slot), zap.Error(err))
			return fmt.Errorf("%s: %w", errMsg, err)
		}
		recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(cd.Duty.Slot), spectypes.BNRoleEnvelopeProposer)
		logger.Info("✅ published execution payload envelope", fields.Slot(cd.Duty.Slot))
	} else {
		logger.Debug("this operator did not build the decided envelope, skipping publication", fields.Slot(cd.Duty.Slot))
	}

	r.markDutySucceeded()
	r.measurements.EndDutyFlow()
	return nil
}

// builtDecidedEnvelope reports whether this operator's cached envelope blinds to the decided value — i.e.
// it produced the agreed envelope and so holds the full bytes to publish.
func (r *EnvelopeProposerRunner) builtDecidedEnvelope(decidedDataSSZ []byte) bool {
	if r.cachedEnvelope == nil {
		return false
	}
	blinded, err := gloas.Blinded(r.cachedEnvelope)
	if err != nil {
		return false
	}
	blindedSSZ, err := blinded.Encode()
	if err != nil {
		return false
	}
	return bytes.Equal(blindedSSZ, decidedDataSSZ)
}

func (r *EnvelopeProposerRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	r.measurements.StartDutyFlow()
	r.cachedEnvelope = nil // drop any envelope cached for a prior duty

	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	slot := validatorDuty.DutySlot()

	// The §6 value-check is slot-specific (it matches the §4 root recorded for this slot), so rebuild it
	// per duty — as the committee runner does for its vote check — before starting QBFT.
	share := r.GetShare()
	r.ValCheck = ssv.NewEnvelopeChecker(r.proposedBlockRoots, slot, share.ValidatorPubKey, share.ValidatorIndex)

	// The envelope commits to the §4-decided block, so the proposer must have decided and recorded its root.
	beaconBlockRoot, ok := r.proposedBlockRoots.Get(slot)
	if !ok {
		return fmt.Errorf("no decided block root recorded for envelope slot %d", slot)
	}

	input, err := r.produceBlindedEnvelope(ctx, validatorDuty, beaconBlockRoot)
	if err != nil {
		return fmt.Errorf("produce blinded envelope: %w", err)
	}
	logger.Debug("built execution payload envelope", fields.Slot(slot))

	r.measurements.StartConsensus()
	if err := r.decide(ctx, logger, slot, input, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}
	return nil
}

// produceBlindedEnvelope fetches this operator's execution-payload envelope for the slot, caches the full
// envelope for the later content-matched publish, and wraps its blinded form as the QBFT value.
func (r *EnvelopeProposerRunner) produceBlindedEnvelope(ctx context.Context, duty *spectypes.ValidatorDuty, beaconBlockRoot phase0.Root) (*gloas.EnvelopeConsensusData, error) {
	envelope, err := r.GetBeaconNode().GetExecutionPayloadEnvelope(ctx, duty.DutySlot(), beaconBlockRoot)
	if err != nil {
		return nil, fmt.Errorf("get execution payload envelope: %w", err)
	}
	r.cachedEnvelope = envelope

	blinded, err := gloas.Blinded(envelope)
	if err != nil {
		return nil, err
	}
	dataSSZ, err := blinded.Encode()
	if err != nil {
		return nil, fmt.Errorf("encode blinded envelope: %w", err)
	}
	return &gloas.EnvelopeConsensusData{
		Duty:    *duty,
		Version: networkconfig.DataVersionGloas,
		DataSSZ: dataSSZ,
	}, nil
}

// expectedPreConsensusRootsAndDomain is unreachable: the envelope duty has no pre-consensus phase.
func (r *EnvelopeProposerRunner) expectedPreConsensusRootsAndDomain() ([]spectypes.HashRoot, phase0.DomainType, error) {
	return nil, spectypes.DomainError, errors.New("no pre-consensus phase for envelope proposer")
}

func (r *EnvelopeProposerRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]spectypes.HashRoot, phase0.DomainType, error) {
	cd := &gloas.EnvelopeConsensusData{}
	if err := cd.Decode(r.State.DecidedValue); err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not decode envelope consensus data: %w", err)
	}
	blinded := &gloas.BlindedExecutionPayloadEnvelope{}
	if err := blinded.Decode(cd.DataSSZ); err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not decode blinded envelope: %w", err)
	}
	return []spectypes.HashRoot{blinded}, spectypes.DomainBeaconBuilder, nil
}

func (r *EnvelopeProposerRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *EnvelopeProposerRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *EnvelopeProposerRunner) GetShare() *spectypes.Share {
	for _, share := range r.Share {
		return share
	}
	return nil
}

func (r *EnvelopeProposerRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *EnvelopeProposerRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *EnvelopeProposerRunner) MarshalJSON() ([]byte, error) {
	return marshalRunnerStateJSON(r.BaseRunner)
}

func (r *EnvelopeProposerRunner) UnmarshalJSON(data []byte) error {
	br, err := unmarshalRunnerStateJSON(data)
	if err != nil {
		return err
	}
	r.BaseRunner = br
	r.ValCheck = nil
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

package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// EnvelopeBuilderRunner runs the §6 execution-payload-envelope-signing duty (SIP #94 §6,
// RoleEnvelopeBuilder=9). It is a second QBFT instance for the proposer's slot, started by the proposer
// only on the self-build path (external builders sign their own envelopes). The flow mirrors the proposer
// minus pre-consensus: executeDuty produces a BlindedExecutionPayloadEnvelope and runs QBFT over it;
// ProcessConsensus signs the decided blinded root under DOMAIN_BEACON_BUILDER and broadcasts a
// post-consensus partial signature; ProcessPostConsensus reconstructs the BLS signature and the builder
// publishes the full envelope.
//
// Produce (fetch the envelope + compute PayloadRoot) and publish (POST the full envelope) are stubbed
// pending the full Gloas execution payload + goclient endpoints; the QBFT / signing / value-check flow is
// complete.
type EnvelopeBuilderRunner struct {
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
}

// EnvelopeBuilderRunnerOptions bundles the dependencies required by NewEnvelopeBuilderRunner.
type EnvelopeBuilderRunnerOptions struct {
	BaseRunnerOptions

	QBFTController     *controller.Controller
	ProposedBlockRoots *ssv.ProposedBlockRoots
	HighestDecidedSlot phase0.Slot
}

func NewEnvelopeBuilderRunner(opts EnvelopeBuilderRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &EnvelopeBuilderRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleEnvelopeBuilder,
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

func (r *EnvelopeBuilderRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}
	return r.baseStartNewDuty(ctx, logger, r, validatorDuty, quorum)
}

// ProcessPreConsensus is unreachable: the envelope duty has no pre-consensus phase.
func (r *EnvelopeBuilderRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	return errors.New("no pre-consensus phase for envelope builder")
}

func (r *EnvelopeBuilderRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
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
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleEnvelopeBuilder)

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
	if err := r.signAndBroadcastPostConsensusMsg(r.GetNetwork(), r.operatorSigner, r.GetShare().ValidatorPubKey[:], postConsensusMsg); err != nil {
		return fmt.Errorf("can't broadcast partial post-consensus sig: %w", err)
	}

	return nil
}

func (r *EnvelopeBuilderRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
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
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleEnvelopeBuilder)

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

// submitEnvelope publishes the signed execution-payload envelope. Only the operator that built the decided
// envelope (content match) publishes; the others just complete the duty.
//
// STUB: reconstructing and POSTing the full SignedExecutionPayloadEnvelope needs the full Gloas execution
// payload (EIP-7928 block_access_list, EIP-7843 slot_number) and the goclient submit endpoint.
func (r *EnvelopeBuilderRunner) submitEnvelope(ctx context.Context, logger *zap.Logger, cd *gloas.EnvelopeConsensusData, sig phase0.BLSSignature) error {
	return errors.New("gloas envelope publication not yet implemented (needs the full Gloas execution payload)")
}

func (r *EnvelopeBuilderRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	r.measurements.StartDutyFlow()

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

	r.measurements.StartConsensus()
	if err := r.decide(ctx, logger, slot, input, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}
	return nil
}

// produceBlindedEnvelope fetches this operator's execution-payload envelope for the slot and wraps its
// blinded form as the QBFT value.
//
// STUB: needs the full Gloas execution payload to compute PayloadRoot = hash_tree_root(payload) and the
// goclient GET getExecutionPayloadEnvelope(slot, beacon_block_root).
func (r *EnvelopeBuilderRunner) produceBlindedEnvelope(ctx context.Context, duty *spectypes.ValidatorDuty, beaconBlockRoot phase0.Root) (*gloas.EnvelopeConsensusData, error) {
	return nil, errors.New("gloas envelope production not yet implemented (needs the full Gloas execution payload)")
}

// expectedPreConsensusRootsAndDomain is unreachable: the envelope duty has no pre-consensus phase.
func (r *EnvelopeBuilderRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return nil, phase0.DomainType{}, errors.New("no pre-consensus phase for envelope builder")
}

func (r *EnvelopeBuilderRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	cd := &gloas.EnvelopeConsensusData{}
	if err := cd.Decode(r.State.DecidedValue); err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not decode envelope consensus data: %w", err)
	}
	blinded := &gloas.BlindedExecutionPayloadEnvelope{}
	if err := blinded.Decode(cd.DataSSZ); err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not decode blinded envelope: %w", err)
	}
	return []ssz.HashRoot{blinded}, spectypes.DomainBeaconBuilder, nil
}

func (r *EnvelopeBuilderRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *EnvelopeBuilderRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *EnvelopeBuilderRunner) GetShare() *spectypes.Share {
	for _, share := range r.Share {
		return share
	}
	return nil
}

func (r *EnvelopeBuilderRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *EnvelopeBuilderRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *EnvelopeBuilderRunner) MarshalJSON() ([]byte, error) {
	type envelopeBuilderRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
		// ValCheck is a runtime-only dependency, ignored on decode; always marshaled as null.
		ValCheck any `json:"ValCheck"`
	}
	return json.Marshal(&envelopeBuilderRunnerJSON{
		BaseRunner: r.BaseRunner,
		ValCheck:   nil,
	})
}

func (r *EnvelopeBuilderRunner) UnmarshalJSON(data []byte) error {
	type envelopeBuilderRunnerJSON struct {
		BaseRunner *BaseRunner     `json:"BaseRunner"`
		ValCheck   json.RawMessage `json:"ValCheck"`
	}
	aux := &envelopeBuilderRunnerJSON{}
	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}
	if aux.BaseRunner == nil {
		return fmt.Errorf("missing BaseRunner")
	}
	r.BaseRunner = aux.BaseRunner
	// ValCheck is not restored from JSON. Callers must rehydrate it explicitly.
	r.ValCheck = nil
	return nil
}

func (r *EnvelopeBuilderRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

func (r *EnvelopeBuilderRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

func (r *EnvelopeBuilderRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode EnvelopeBuilderRunner: %w", err)
	}
	return sha256.Sum256(marshaledRoot), nil
}

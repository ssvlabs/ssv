package runner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"hash"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type AggregatorRunner struct {
	*BaseRunner

	beacon         beacon.BeaconNode
	network        protocolp2p.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	measurements   *dutyMeasurements

	// ValCheck is used to validate the qbft-value(s) proposed by other Operators.
	ValCheck ssv.ValueChecker

	// IsAggregator returns true if the signature is from the input validator. The committee
	// count is provided as an argument rather than imported implementation from spec. Having
	// committee count as an argument allows cheaper computation at run time.
	//
	// Spec pseudocode definition:
	//
	//	def is_aggregator(state: BeaconState, slot: Slot, index: CommitteeIndex, slot_signature: BLSSignature) -> bool:
	//	 committee = get_beacon_committee(state, slot, index)
	//	 modulo = max(1, len(committee) // TARGET_AGGREGATORS_PER_COMMITTEE)
	//	 return bytes_to_uint64(hash(slot_signature)[0:8]) % modulo == 0
	//
	// IsAggregator is an exported struct field, so it can be mocked out for easy testing.
	IsAggregator func(targetAggregatorsPerCommittee uint64, committeeCount uint64, slotSig []byte) bool `json:"-"`
}

var _ Runner = &AggregatorRunner{}

// AggregatorRunnerOptions bundles all dependencies required by NewAggregatorRunner.
type AggregatorRunnerOptions struct {
	BaseRunnerOptions

	QBFTController     *controller.Controller
	ValCheck           ssv.ValueChecker
	HighestDecidedSlot phase0.Slot
}

func NewAggregatorRunner(opts AggregatorRunnerOptions) (Runner, error) {
	if len(opts.Share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     ssvtypes.RoleAggregator,
			NetworkConfig:      opts.NetworkConfig,
			Share:              opts.Share,
			QBFTController:     opts.QBFTController,
			highestDecidedSlot: opts.HighestDecidedSlot,
		},

		beacon:         opts.Beacon,
		network:        opts.Network,
		signer:         opts.Signer,
		operatorSigner: opts.OperatorSigner,
		ValCheck:       opts.ValCheck,
		measurements:   newMeasurementsStore(),

		IsAggregator: isAggregatorFn(),
	}, nil
}

func (r *AggregatorRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	return r.baseStartNewDuty(ctx, logger, r, validatorDuty, quorum)
}

func (r *AggregatorRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePreConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing selection proof message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), ssvtypes.RoleAggregator)

	// only 1 root, verified by expectedPreConsensusRootsAndDomain
	root := roots[0]

	// reconstruct selection proof sig
	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.State.ReconstructBeaconSig(r.State.PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.FallBackAndVerifyEachSignature(r.State.PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got pre-consensus quorum but it has invalid signatures: %w", err)
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}
	span.SetAttributes(
		observability.CommitteeIndexAttribute(duty.CommitteeIndex),
		observability.ValidatorIndexAttribute(duty.ValidatorIndex),
	)

	// this is the earliest in aggregator runner flow where we get to know whether we are meant
	// to perform this aggregation duty or not
	ok := r.IsAggregator(r.NetworkConfig.TargetAggregatorsPerCommittee, duty.CommitteeLength, fullSig)
	if !ok {
		r.markDutyNotRequired()
		r.measurements.EndDutyFlow()
		recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), ssvtypes.RoleAggregator, 0)
		return nil
	}

	span.AddEvent("submitting aggregate and proof",
		trace.WithAttributes(
			observability.CommitteeIndexAttribute(duty.CommitteeIndex),
			observability.ValidatorIndexAttribute(duty.ValidatorIndex)),
	)
	res, ver, err := r.beacon.SubmitAggregateSelectionProof(ctx, duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return fmt.Errorf("failed to submit aggregate and proof: %w", err)
	}
	const submittedAggregateAndProofEvent = "submitted aggregate and proof"
	logger.Debug(submittedAggregateAndProofEvent)
	span.AddEvent(submittedAggregateAndProofEvent)

	byts, err := res.MarshalSSZ()
	if err != nil {
		return fmt.Errorf("could not marshal aggregate and proof: %w", err)
	}
	input := &spectypes.ProposerConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	r.measurements.StartConsensus()
	if err := r.decide(ctx, logger, duty.Slot, input, r.ValCheck); err != nil {
		return fmt.Errorf("qbft-decide: %w", err)
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	span.AddEvent("processing QBFT consensus msg")
	decided, encDecidedValue, err := r.baseConsensusMsgProcessing(ctx, logger, r.ValCheck.CheckValue, signedMsg, &spectypes.ProposerConsensusData{})
	if err != nil {
		return fmt.Errorf("failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), ssvtypes.RoleAggregator)

	decidedValue, err := validatorConsensusDataFromEncoder(encDecidedValue)
	if err != nil {
		return fmt.Errorf("decided value: %w", err)
	}
	span.SetAttributes(
		observability.BeaconSlotAttribute(decidedValue.Duty.Slot),
		observability.ValidatorPublicKeyAttribute(decidedValue.Duty.PubKey),
	)

	_, aggregateAndProofHashRoot, err := ssvtypes.GetAggregateAndProof(decidedValue)
	if err != nil {
		return fmt.Errorf("could not get aggregate and proof: %w", err)
	}

	duty, err := r.currentValidatorDuty()
	if err != nil {
		return fmt.Errorf("current validator duty: %w", err)
	}

	span.AddEvent("signing post consensus")
	// specific duty sig
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		duty,
		aggregateAndProofHashRoot,
		decidedValue.Duty.Slot,
		spectypes.DomainAggregateAndProof,
	)
	if err != nil {
		return fmt.Errorf("failed signing aggregate and proof: %w", err)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.RunnerRoleType)

	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return fmt.Errorf("could not encode post consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign post-consensus partial signature message: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	r.measurements.StartPostConsensus()
	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.network.Broadcast(msgID, msgToBroadcast); err != nil {
		return fmt.Errorf("can't broadcast partial post consensus sig: %w", err)
	}
	const broadcastedPostConsensusMsgEvent = "broadcasted post-consensus partial signature message"
	logger.Debug(broadcastedPostConsensusMsgEvent)
	span.AddEvent(broadcastedPostConsensusMsgEvent)

	return nil
}

func (r *AggregatorRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) (err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	hasQuorum, roots, err := r.basePostConsensusMsgProcessing(ctx, logger, r, signedMsg)
	if errors.Is(err, ErrNoDutyAssigned) || errors.Is(err, ErrRunningDutySucceeded) {
		// Since we are re-using the same runner for different duties, ErrRunningDutySucceeded error
		// also needs to be retried.
		err = NewRetryableError(err)
	}
	if err != nil {
		return fmt.Errorf("failed processing post consensus message: %w", err)
	}

	span.SetAttributes(
		observability.ValidatorHasQuorumAttribute(hasQuorum),
		observability.BeaconBlockRootCountAttribute(len(roots)),
	)

	if !hasQuorum {
		return nil
	}

	// We have quorum and are committed to completing this duty here. The quorum above fires only once,
	// so a terminal failure below won't be retried.
	defer func() {
		if err != nil {
			r.markDutyFailed(err)
		}
	}()

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), ssvtypes.RoleAggregator)

	// only 1 root, verified by expectedPostConsensusRootsAndDomain
	root := roots[0]

	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	sig, err := r.State.ReconstructBeaconSig(r.State.PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.FallBackAndVerifyEachSignature(r.State.PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return fmt.Errorf("got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)

	cd := &spectypes.ProposerConsensusData{}
	err = cd.Decode(r.State.DecidedValue)
	if err != nil {
		return fmt.Errorf("could not decode consensus data: %w", err)
	}
	aggregateAndProof, _, err := ssvtypes.GetAggregateAndProof(cd)
	if err != nil {
		return fmt.Errorf("could not get aggregate and proof: %w", err)
	}

	msg, err := constructVersionedSignedAggregateAndProof(*aggregateAndProof, specSig)
	if err != nil {
		return fmt.Errorf("could not construct versioned aggregate and proof: %w", err)
	}

	const submittingSignedAggregateProofEvent = "submitting signed aggregate and proof"
	logger.Debug(submittingSignedAggregateProofEvent)
	span.AddEvent(submittingSignedAggregateProofEvent)

	start := time.Now()
	if err := r.beacon.SubmitSignedAggregateSelectionProof(ctx, msg); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleAggregator)
		const errMsg = "could not submit to Beacon chain reconstructed contribution and proof"
		logger.Error(errMsg, fields.Took(time.Since(start)), zap.Error(err))
		return fmt.Errorf("%s: %w", errMsg, err)
	}
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}
	recordSuccessfulSubmission(ctx, 1, r.NetworkConfig.EstimatedEpochAtSlot(currentDutySlot), spectypes.BNRoleAggregator)
	const submittedSignedAggregateProofEvent = "✅ successfully submitted signed aggregate and proof"
	span.AddEvent(submittedSignedAggregateProofEvent)
	logger.Debug(submittedSignedAggregateProofEvent, fields.Took(time.Since(start)))

	r.markDutySucceeded()
	r.measurements.EndDutyFlow()
	recordTotalDutyDuration(ctx, r.measurements.TotalDutyTime(), ssvtypes.RoleAggregator, r.State.RunningInstance.State.Round)
	const dutyFinishedEvent = "✔️successfully finished duty processing"
	logger.Info(dutyFinishedEvent,
		fields.PreConsensusTime(r.measurements.PreConsensusTime()),
		fields.ConsensusTime(r.measurements.ConsensusTime()),
		fields.ConsensusRounds(uint64(r.State.RunningInstance.State.Round)),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)
	span.AddEvent(dutyFinishedEvent)

	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	currentDutySlot, err := r.currentDutySlot()
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("current duty slot: %w", err)
	}

	return []ssz.HashRoot{spectypes.SSZUint64(currentDutySlot)}, spectypes.DomainSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AggregatorRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	cd := &spectypes.ProposerConsensusData{}
	err := cd.Decode(r.State.DecidedValue)
	if err != nil {
		return nil, spectypes.DomainError, fmt.Errorf("could not create consensus data: %w", err)
	}
	_, hashRoot, err := ssvtypes.GetAggregateAndProof(cd)
	if err != nil {
		return nil, phase0.DomainType{}, fmt.Errorf("could not get aggregate and proof: %w", err)
	}

	return []ssz.HashRoot{hashRoot}, spectypes.DomainAggregateAndProof, nil
}

// executeDuty steps:
// 1) sign a partial selection proof and wait for 2f+1 partial sigs from peers
// 2) reconstruct selection proof and send SubmitAggregateSelectionProof to BN
// 3) start consensus on duty + aggregation data
// 4) Once consensus decides, sign partial aggregation data and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedAggregateSubmitRequest sig to the BN
func (r *AggregatorRunner) executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	r.measurements.StartDutyFlow()

	validatorDuty, err := validatorDutyFromDuty(duty)
	if err != nil {
		return err
	}

	// sign selection proof
	span.AddEvent("signing beacon object")
	msg, err := signBeaconObject(
		ctx,
		r,
		r.NetworkConfig,
		validatorDuty,
		spectypes.SSZUint64(validatorDuty.DutySlot()),
		validatorDuty.DutySlot(),
		spectypes.DomainSelectionProof,
	)
	if err != nil {
		return fmt.Errorf("could not sign aggregator selection proof: %w", err)
	}

	msgs := &spectypes.PartialSignatureMessages{
		Type:     ssvtypes.SelectionProofPartialSig,
		Slot:     validatorDuty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	logger.Debug("signing and broadcasting selection proof partial sig", fields.Slot(duty.DutySlot()))

	r.measurements.StartPreConsensus()
	if err := r.signAndBroadcastPartialSigMsgs(ctx, r.network, r.operatorSigner, r.GetShare().ValidatorPubKey[:], msgs); err != nil {
		return fmt.Errorf("could not sign/broadcast selection proof partial sig: %w", err)
	}

	return nil
}

func (r *AggregatorRunner) GetNetwork() protocolp2p.Network {
	return r.network
}

func (r *AggregatorRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *AggregatorRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}

func (r *AggregatorRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

func (r *AggregatorRunner) MarshalJSON() ([]byte, error) {
	type aggregatorRunnerJSON struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
		// ValCheck is intentionally kept in the JSON to preserve the historical runner state shape
		// (and thus runner state roots used by spec tests). It is a runtime-only dependency and
		// is ignored on decode, so it is always marshaled as `null` for determinism.
		ValCheck any `json:"ValCheck"`
	}

	return json.Marshal(&aggregatorRunnerJSON{
		BaseRunner: r.BaseRunner,
		ValCheck:   nil,
	})
}

func (r *AggregatorRunner) UnmarshalJSON(data []byte) error {
	type aggregatorRunnerJSON struct {
		BaseRunner *BaseRunner     `json:"BaseRunner"`
		ValCheck   json.RawMessage `json:"ValCheck"`
	}

	aux := &aggregatorRunnerJSON{}
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

// Encode returns the encoded struct in bytes or error
func (r *AggregatorRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *AggregatorRunner) Decode(data []byte) error {
	return json.Unmarshal(data, r)
}

// GetRoot returns the root used for signing and verification
func (r *AggregatorRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, fmt.Errorf("could not encode AggregatorRunner: %w", err)
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

// Constructs a VersionedSignedAggregateAndProof from a VersionedAggregateAndProof and a signature
func constructVersionedSignedAggregateAndProof(aggregateAndProof spec.VersionedAggregateAndProof, signature phase0.BLSSignature) (*spec.VersionedSignedAggregateAndProof, error) {
	ret := &spec.VersionedSignedAggregateAndProof{
		Version: aggregateAndProof.Version,
	}

	switch ret.Version {
	case spec.DataVersionPhase0:
		ret.Phase0 = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Phase0,
			Signature: signature,
		}
	case spec.DataVersionAltair:
		ret.Altair = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Altair,
			Signature: signature,
		}
	case spec.DataVersionBellatrix:
		ret.Bellatrix = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Bellatrix,
			Signature: signature,
		}
	case spec.DataVersionCapella:
		ret.Capella = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Capella,
			Signature: signature,
		}
	case spec.DataVersionDeneb:
		ret.Deneb = &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof.Deneb,
			Signature: signature,
		}
	case spec.DataVersionElectra:
		ret.Electra = &electra.SignedAggregateAndProof{
			Message:   aggregateAndProof.Electra,
			Signature: signature,
		}
	case spec.DataVersionFulu:
		ret.Fulu = &electra.SignedAggregateAndProof{
			Message:   aggregateAndProof.Fulu,
			Signature: signature,
		}
	default:
		return nil, errors.New("unknown version for signed aggregate and proof")
	}

	return ret, nil
}

// isAggregatorFn returns IsAggregator func that performs hashing in an allocation-efficient manner.
func isAggregatorFn() func(targetAggregatorsPerCommittee uint64, committeeCount uint64, slotSig []byte) bool {
	h := newHasher()
	return func(targetAggregatorsPerCommittee uint64, committeeCount uint64, slotSig []byte) bool {
		modulo := committeeCount / targetAggregatorsPerCommittee
		if modulo == 0 {
			// Modulo must be at least 1.
			modulo = 1
		}

		b := h.hashSha256(slotSig)
		return binary.LittleEndian.Uint64(b[:8])%modulo == 0
	}
}

// hasher implements efficient thread-safe data-hashing functionality by pooling hash.Hash
// instances to re-use them for different hash-requests.
type hasher struct {
	sha256Pool sync.Pool
}

func newHasher() *hasher {
	return &hasher{
		sha256Pool: sync.Pool{
			New: func() any {
				return sha256.New()
			},
		},
	}
}

// hashSha256 defines a function that returns the sha256 checksum of the data passed in.
// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/core/0_beacon-chain.md#hash
func (h *hasher) hashSha256(data []byte) [32]byte {
	hsr := h.sha256Pool.Get().(hash.Hash)
	defer h.sha256Pool.Put(hsr)

	hsr.Reset()

	var b [32]byte

	// The hash interface never returns an error, for that reason
	// we are not handling the error below. For reference, it is
	// stated here https://golang.org/pkg/hash/#Hash

	// #nosec G104
	hsr.Write(data)
	hsr.Sum(b[:0])

	return b
}

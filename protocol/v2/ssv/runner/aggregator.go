package runner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"hash"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/observability/traces"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type AggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         ekm.BeaconSigner
	operatorSigner ssvtypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF
	measurements   measurementsStore

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

func NewAggregatorRunner(
	networkConfig *networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) (*AggregatorRunner, error) {
	if len(share) != 1 {
		return nil, errors.New("must have one share")
	}

	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleAggregator,
			NetworkConfig:      networkConfig,
			Share:              share,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},

		beacon:         beacon,
		network:        network,
		signer:         signer,
		operatorSigner: operatorSigner,
		valCheck:       valCheck,
		measurements:   newMeasurementsStore(),

		IsAggregator: isAggregatorFn(),
	}, nil
}

func (r *AggregatorRunner) StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewDuty(ctx, logger, r, duty, quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AggregatorRunner) ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_pre_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing selection proof message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	epoch := r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot())
	recordSuccessfulQuorum(ctx, 1, epoch, spectypes.BNRoleAggregator, attributeConsensusPhasePreConsensus)

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleAggregator)

	// only 1 root, verified by expectedPreConsensusRootsAndDomain
	root := roots[0]

	// reconstruct selection proof sig
	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return traces.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
	}

	// signer must be same for all messages, at least 1 message must be present (this is validated prior)
	signer := signedMsg.Messages[0].Signer
	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)
	span.SetAttributes(
		observability.CommitteeIndexAttribute(duty.CommitteeIndex),
		observability.ValidatorIndexAttribute(duty.ValidatorIndex),
	)

	const gotPartialSignaturesEvent = "🧩 got partial aggregator selection proof signatures"
	span.AddEvent(gotPartialSignaturesEvent, trace.WithAttributes(observability.ValidatorSignerAttribute(signer)))
	logger.Debug(gotPartialSignaturesEvent, zap.Any("signer", signer))

	// this is the earliest in aggregator runner flow where we get to know whether we are meant
	// to perform this aggregation duty or not
	ok := r.IsAggregator(r.BaseRunner.NetworkConfig.TargetAggregatorsPerCommittee, duty.CommitteeLength, fullSig)
	if !ok {
		const aggDutyWontBeNeededEvent = "aggregation duty won't be needed from this validator for this slot"
		span.AddEvent(aggDutyWontBeNeededEvent, trace.WithAttributes(observability.ValidatorSignerAttribute(signer)))
		logger.Debug(aggDutyWontBeNeededEvent, zap.Any("signer", signer))
		span.SetStatus(codes.Ok, "")
		return nil
	}

	span.AddEvent("submitting aggregate and proof",
		trace.WithAttributes(
			observability.CommitteeIndexAttribute(duty.CommitteeIndex),
			observability.ValidatorIndexAttribute(duty.ValidatorIndex)))
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(ctx, duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return traces.Errorf(span, "failed to submit aggregate and proof: %w", err)
	}

	const submittedAggregateAndProofEvent = "submitted aggregate and proof"
	logger.Debug(submittedAggregateAndProofEvent)
	span.AddEvent(submittedAggregateAndProofEvent)

	byts, err := res.MarshalSSZ()
	if err != nil {
		return traces.Errorf(span, "could not marshal aggregate and proof: %w", err)
	}
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(ctx, logger, r, duty.Slot, input); err != nil {
		return traces.Errorf(span, "can't start new duty runner instance for duty: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_consensus"),
		trace.WithAttributes(
			observability.ValidatorMsgIDAttribute(signedMsg.SSVMessage.GetID()),
			observability.ValidatorMsgTypeAttribute(signedMsg.SSVMessage.GetType()),
			observability.RunnerRoleAttribute(signedMsg.SSVMessage.GetID().GetRoleType()),
		))
	defer span.End()

	span.AddEvent("processing QBFT consensus msg")
	decided, encDecidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r, signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return traces.Errorf(span, "failed processing consensus message: %w", err)
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		span.AddEvent("instance is not decided")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleAggregator)

	r.measurements.StartPostConsensus()

	decidedValue := encDecidedValue.(*spectypes.ValidatorConsensusData)
	span.SetAttributes(
		observability.BeaconSlotAttribute(decidedValue.Duty.Slot),
		observability.ValidatorPublicKeyAttribute(decidedValue.Duty.PubKey),
	)

	_, aggregateAndProofHashRoot, err := decidedValue.GetAggregateAndProof()
	if err != nil {
		return traces.Errorf(span, "could not get aggregate and proof: %w", err)
	}

	span.AddEvent("signing post consensus")
	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(
		ctx,
		r,
		r.BaseRunner.State.StartingDuty.(*spectypes.ValidatorDuty),
		aggregateAndProofHashRoot,
		decidedValue.Duty.Slot,
		spectypes.DomainAggregateAndProof,
	)
	if err != nil {
		return traces.Errorf(span, "failed signing aggregate and proof: %w", err)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)

	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode post consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign post-consensus partial signature message: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial post consensus sig: %w", err)
	}

	const broadcastedPostEvent = "broadcasted post consensus partial signature message"
	logger.Debug(broadcastedPostEvent)
	span.AddEvent(broadcastedPostEvent)

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.process_post_consensus"),
		trace.WithAttributes(
			observability.BeaconSlotAttribute(signedMsg.Slot),
			observability.ValidatorPartialSigMsgTypeAttribute(signedMsg.Type),
		))
	defer span.End()

	hasQuorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(ctx, r, signedMsg)
	if err != nil {
		return traces.Errorf(span, "failed processing post consensus message: %w", err)
	}

	span.SetAttributes(
		observability.ValidatorHasQuorumAttribute(hasQuorum),
		observability.BeaconBlockRootCountAttribute(len(roots)),
	)

	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleAggregator)

	const gotPostConsensusQuorumEvent = "got post consensus quorum"
	logger.Debug(gotPostConsensusQuorumEvent)
	span.AddEvent(gotPostConsensusQuorumEvent)

	// only 1 root, verified by expectedPostConsensusRootsAndDomain
	root := roots[0]

	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return traces.Errorf(span, "got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)

	cd := &spectypes.ValidatorConsensusData{}
	err = cd.Decode(r.GetState().DecidedValue)
	if err != nil {
		return traces.Errorf(span, "could not decode consensus data: %w", err)
	}
	aggregateAndProof, _, err := cd.GetAggregateAndProof()
	if err != nil {
		return traces.Errorf(span, "could not get aggregate and proof: %w", err)
	}

	msg, err := constructVersionedSignedAggregateAndProof(*aggregateAndProof, specSig)
	if err != nil {
		return traces.Errorf(span, "could not construct versioned aggregate and proof: %w", err)
	}

	const submittingSignedAggregateProofEvent = "submitting signed aggregate and proof"
	logger.Debug(submittingSignedAggregateProofEvent)
	span.AddEvent(submittingSignedAggregateProofEvent)

	start := time.Now()
	if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(ctx, msg); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleAggregator)
		const errMsg = "could not submit to Beacon chain reconstructed contribution and proof"
		logger.Error(errMsg, fields.Took(time.Since(start)), zap.Error(err))
		return traces.Errorf(span, "%s: %w", errMsg, err)
	}
	const submittedSignedAggregateProofEvent = "✅ successfully submitted signed aggregate and proof"
	span.AddEvent(submittedSignedAggregateProofEvent)
	logger.Debug(submittedSignedAggregateProofEvent, fields.Took(time.Since(start)))

	r.GetState().Finished = true
	r.measurements.EndDutyFlow()

	recordDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.BNRoleAggregator, r.GetState().RunningInstance.State.Round)
	recordSuccessfulSubmission(ctx, 1, r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot()), spectypes.BNRoleAggregator)

	const dutyFinishedEvent = "successfully finished duty processing"
	logger.Info(dutyFinishedEvent,
		fields.PreConsensusTime(r.measurements.PreConsensusTime()),
		fields.ConsensusTime(r.measurements.ConsensusTime()),
		fields.PostConsensusTime(r.measurements.PostConsensusTime()),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)
	span.AddEvent(dutyFinishedEvent)

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{spectypes.SSZUint64(r.GetState().StartingDuty.DutySlot())}, spectypes.DomainSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AggregatorRunner) expectedPostConsensusRootsAndDomain(context.Context) ([]ssz.HashRoot, phase0.DomainType, error) {
	cd := &spectypes.ValidatorConsensusData{}
	err := cd.Decode(r.GetState().DecidedValue)
	if err != nil {
		return nil, spectypes.DomainError, errors.Wrap(err, "could not create consensus data")
	}
	_, hashRoot, err := cd.GetAggregateAndProof()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get aggregate and proof")
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
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "runner.execute_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	r.measurements.StartDutyFlow()
	r.measurements.StartPreConsensus()

	// sign selection proof
	span.AddEvent("signing beacon object")
	msg, err := r.BaseRunner.signBeaconObject(
		ctx,
		r,
		duty.(*spectypes.ValidatorDuty),
		spectypes.SSZUint64(duty.DutySlot()),
		duty.DutySlot(),
		spectypes.DomainSelectionProof,
	)
	if err != nil {
		return traces.Errorf(span, "could not sign aggregator selection proof: %w", err)
	}

	const signedSelectionProofEvent = "signed aggregator selection proof"
	logger.Debug(signedSelectionProofEvent)
	span.AddEvent(signedSelectionProofEvent)

	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.SelectionProofPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return traces.Errorf(span, "could not encode selection proof partial signature message: %w", err)
	}

	r.measurements.StartConsensus()

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return traces.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting signed SSV message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return traces.Errorf(span, "can't broadcast partial selection proof sig: %w", err)
	}

	span.SetStatus(codes.Ok, "")
	return nil
}

func (r *AggregatorRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AggregatorRunner) GetNetwork() specqbft.Network {
	return r.network
}

func (r *AggregatorRunner) GetBeaconNode() beacon.BeaconNode {
	return r.beacon
}

func (r *AggregatorRunner) GetShare() *spectypes.Share {
	// TODO better solution for this
	for _, share := range r.BaseRunner.Share {
		return share
	}
	return nil
}

func (r *AggregatorRunner) GetState() *State {
	return r.BaseRunner.State
}

func (r *AggregatorRunner) GetValCheckF() specqbft.ProposedValueCheckF {
	return r.valCheck
}

func (r *AggregatorRunner) GetSigner() ekm.BeaconSigner {
	return r.signer
}
func (r *AggregatorRunner) GetOperatorSigner() ssvtypes.OperatorSigner {
	return r.operatorSigner
}

// Encode returns the encoded struct in bytes or error
func (r *AggregatorRunner) Encode() ([]byte, error) {
	return json.Marshal(r)
}

// Decode returns error if decoding failed
func (r *AggregatorRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &r)
}

// GetRoot returns the root used for signing and verification
func (r *AggregatorRunner) GetRoot() ([32]byte, error) {
	marshaledRoot, err := r.Encode()
	if err != nil {
		return [32]byte{}, errors.Wrap(err, "could not encode AggregatorRunner")
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
			New: func() interface{} {
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

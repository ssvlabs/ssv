package runner

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/attestantio/go-eth2-client/spec"
	"github.com/attestantio/go-eth2-client/spec/electra"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
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
}

var _ Runner = &AggregatorRunner{}

func NewAggregatorRunner(
	networkConfig networkconfig.Network,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer ekm.BeaconSigner,
	operatorSigner ssvtypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) (Runner, error) {
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
		measurements:   NewMeasurementsStore(),
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
		return observability.Errorf(span, "failed processing selection proof message: %w", err)
	}
	// quorum returns true only once (first time quorum achieved)
	if !hasQuorum {
		span.AddEvent("no quorum")
		span.SetStatus(codes.Ok, "")
		return nil
	}

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
		return observability.Errorf(span, "got pre-consensus quorum but it has invalid signatures: %w", err)
	}

	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)
	span.SetAttributes(
		observability.CommitteeIndexAttribute(duty.CommitteeIndex),
		observability.ValidatorIndexAttribute(duty.ValidatorIndex),
	)

	const eventMsg = "🧩 got partial signature quorum"
	span.AddEvent(eventMsg, trace.WithAttributes(observability.ValidatorSignerAttribute(signedMsg.Messages[0].Signer)))
	logger.Debug(eventMsg,
		zap.Any("signer", signedMsg.Messages[0].Signer), // TODO: always 1?
		fields.Slot(duty.Slot),
	)

	r.measurements.PauseDutyFlow()

	span.AddEvent("submitting aggregate and proof",
		trace.WithAttributes(
			observability.CommitteeIndexAttribute(duty.CommitteeIndex),
			observability.ValidatorIndexAttribute(duty.ValidatorIndex)))
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(ctx, duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return observability.Errorf(span, "failed to submit aggregate and proof: %w", err)
	}
	r.measurements.ContinueDutyFlow()

	byts, err := res.MarshalSSZ()
	if err != nil {
		return observability.Errorf(span, "could not marshal aggregate and proof: %w", err)
	}
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(ctx, logger, r, duty.Slot, input); err != nil {
		return observability.Errorf(span, "can't start new duty runner instance for duty: %w", err)
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

	decided, encDecidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r, signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return observability.Errorf(span, "failed processing consensus message: %w", err)
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
		return observability.Errorf(span, "could not get aggregate and proof: %w", err)
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
		return observability.Errorf(span, "failed signing aggregate and proof: %w", err)
	}

	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.GetDomainType(), r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)

	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return observability.Errorf(span, "could not encode post consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing post consensus partial signature message")
	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return observability.Errorf(span, "could not sign post-consensus partial signature message: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting post consensus partial signature message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return observability.Errorf(span, "can't broadcast partial post consensus sig: %w", err)
	}

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
		return observability.Errorf(span, "failed processing post consensus message: %w", err)
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

	// only 1 root, verified by expectedPostConsensusRootsAndDomain
	root := roots[0]

	span.AddEvent("reconstructing beacon signature", trace.WithAttributes(observability.BeaconBlockRootAttribute(root)))
	sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return observability.Errorf(span, "got post-consensus quorum but it has invalid signatures: %w", err)
	}
	specSig := phase0.BLSSignature{}
	copy(specSig[:], sig)

	cd := &spectypes.ValidatorConsensusData{}
	err = cd.Decode(r.GetState().DecidedValue)
	if err != nil {
		return observability.Errorf(span, "could not decode consensus data: %w", err)
	}
	aggregateAndProof, _, err := cd.GetAggregateAndProof()
	if err != nil {
		return observability.Errorf(span, "could not get aggregate and proof: %w", err)
	}

	msg, err := constructVersionedSignedAggregateAndProof(*aggregateAndProof, specSig)
	if err != nil {
		return observability.Errorf(span, "could not construct versioned aggregate and proof: %w", err)
	}

	start := time.Now()
	if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(ctx, msg); err != nil {
		recordFailedSubmission(ctx, spectypes.BNRoleAggregator)
		const errMsg = "could not submit to Beacon chain reconstructed contribution and proof"
		logger.Error(errMsg, fields.SubmissionTime(time.Since(start)), zap.Error(err))
		return observability.Errorf(span, "%s: %w", errMsg, err)
	}
	const eventMsg = "✅ successful submitted aggregate"
	span.AddEvent(eventMsg)
	logger.Debug(
		eventMsg,
		fields.SubmissionTime(time.Since(start)),
		fields.TotalConsensusTime(r.measurements.TotalConsensusTime()),
		fields.TotalDutyTime(r.measurements.TotalDutyTime()),
	)

	r.GetState().Finished = true
	r.measurements.EndDutyFlow()

	recordDutyDuration(ctx, r.measurements.TotalDutyTime(), spectypes.BNRoleAggregator, r.GetState().RunningInstance.State.Round)
	recordSuccessfulSubmission(
		ctx,
		1,
		r.BaseRunner.NetworkConfig.EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot()),
		spectypes.BNRoleAggregator,
	)

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
		return observability.Errorf(span, "could not sign randao: %w", err)
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.SelectionProofPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.NetworkConfig.GetDomainType(), r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return observability.Errorf(span, "could not encode selection proof partial signature message: %w", err)
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
		return observability.Errorf(span, "could not sign SSVMessage: %w", err)
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting signed SSV message")
	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return observability.Errorf(span, "can't broadcast partial selection proof sig: %w", err)
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

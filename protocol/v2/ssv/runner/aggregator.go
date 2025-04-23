package runner

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"fmt"
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
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/logging/fields"
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
	domainType spectypes.DomainType,
	beaconNetwork spectypes.BeaconNetwork,
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
			DomainType:         domainType,
			BeaconNetwork:      beaconNetwork,
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
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}
	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	r.measurements.EndPreConsensus()
	recordPreConsensusDuration(ctx, r.measurements.PreConsensusTime(), spectypes.RoleAggregator)

	// only 1 root, verified by basePreConsensusMsgProcessing
	root := roots[0]
	// reconstruct selection proof sig
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	// signer must be same for all messages, at least 1 message must be present (this is validated
	// prior to ProcessPreConsensus call)
	signer := signedMsg.Messages[0].Signer
	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)

	logger.Debug("🧩 got partial signature quorum",
		zap.Any("signer", signer),
		fields.Slot(duty.Slot),
	)

	// this is the earliest in aggregator runner flow where we get to know whether we are meant
	// to perform this aggregation duty or not
	ok, err := isAggregator(duty.CommitteeLength, fullSig)
	if err != nil {
		return fmt.Errorf("check if validator is an aggregator: %w", err)
	}
	if !ok {
		logger.Debug("aggregation duty won't be needed from this validator for this slot",
			zap.Any("signer", signer),
			fields.Slot(duty.Slot),
		)
		return nil
	}

	r.measurements.PauseDutyFlow()
	// get block data
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to submit aggregate and proof")
	}
	r.measurements.ContinueDutyFlow()

	byts, err := res.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal aggregate and proof")
	}
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(ctx, logger, r, duty.Slot, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	decided, encDecidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(ctx, logger, r, signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.measurements.EndConsensus()
	recordConsensusDuration(ctx, r.measurements.ConsensusTime(), spectypes.RoleAggregator)

	r.measurements.StartPostConsensus()

	decidedValue := encDecidedValue.(*spectypes.ValidatorConsensusData)

	_, aggregateAndProofHashRoot, err := decidedValue.GetAggregateAndProof()
	if err != nil {
		return errors.Wrap(err, "could not get aggregate and proof")
	}

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
		return errors.Wrap(err, "failed signing aggregate and proof")
	}
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)

	encodedMsg, err := postConsensusMsg.Encode()
	if err != nil {
		return err
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign post-consensus partial signature message")
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial post consensus sig")
	}

	return nil
}

func (r *AggregatorRunner) ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}

	r.measurements.EndPostConsensus()
	recordPostConsensusDuration(ctx, r.measurements.PostConsensusTime(), spectypes.RoleAggregator)

	var successfullySubmittedAggregates uint32
	for _, root := range roots {
		sig, err := r.GetState().ReconstructBeaconSig(r.GetState().PostConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
		if err != nil {
			// If the reconstructed signature verification failed, fall back to verifying each partial signature
			for _, root := range roots {
				r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PostConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
			}
			return errors.Wrap(err, "got post-consensus quorum but it has invalid signatures")
		}
		specSig := phase0.BLSSignature{}
		copy(specSig[:], sig)

		cd := &spectypes.ValidatorConsensusData{}
		err = cd.Decode(r.GetState().DecidedValue)
		if err != nil {
			return errors.Wrap(err, "could not create consensus data")
		}
		aggregateAndProof, _, err := cd.GetAggregateAndProof()
		if err != nil {
			return errors.Wrap(err, "could not get aggregate and proof")
		}

		msg, err := constructVersionedSignedAggregateAndProof(*aggregateAndProof, specSig)
		if err != nil {
			return errors.Wrap(err, "could not construct versioned aggregate and proof")
		}

		start := time.Now()

		if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(msg); err != nil {
			recordFailedSubmission(ctx, spectypes.BNRoleAggregator)
			logger.Error("❌ could not submit to Beacon chain reconstructed contribution and proof",
				fields.SubmissionTime(time.Since(start)),
				zap.Error(err))
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed aggregate")
		}
		successfullySubmittedAggregates++
		logger.Debug("✅ successful submitted aggregate",
			fields.SubmissionTime(time.Since(start)),
			fields.TotalConsensusTime(r.measurements.TotalConsensusTime()))
	}

	r.GetState().Finished = true

	r.measurements.EndDutyFlow()

	recordDutyDuration(ctx, r.measurements.DutyDurationTime(), spectypes.BNRoleAggregator, r.GetState().RunningInstance.State.Round)
	recordSuccessfulSubmission(ctx,
		successfullySubmittedAggregates,
		r.GetBeaconNode().GetBeaconNetwork().EstimatedEpochAtSlot(r.GetState().StartingDuty.DutySlot()),
		spectypes.BNRoleAggregator)

	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{spectypes.SSZUint64(r.GetState().StartingDuty.DutySlot())}, spectypes.DomainSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AggregatorRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
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
	r.measurements.StartDutyFlow()
	r.measurements.StartPreConsensus()

	// sign selection proof
	msg, err := r.BaseRunner.signBeaconObject(
		ctx,
		r,
		duty.(*spectypes.ValidatorDuty),
		spectypes.SSZUint64(duty.DutySlot()),
		duty.DutySlot(),
		spectypes.DomainSelectionProof,
	)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.SelectionProofPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.BaseRunner.DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return err
	}

	r.measurements.StartConsensus()

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	sig, err := r.operatorSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return errors.Wrap(err, "could not sign SSVMessage")
	}

	msgToBroadcast := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{r.operatorSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	if err := r.GetNetwork().Broadcast(msgID, msgToBroadcast); err != nil {
		return errors.Wrap(err, "can't broadcast partial selection proof sig")
	}
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

// isAggregator returns true if the signature is from the input validator. The committee
// count is provided as an argument rather than imported implementation from spec. Having
// committee count as an argument allows cheaper computation at run time.
//
// Spec pseudocode definition:
//
//	def is_aggregator(state: BeaconState, slot: Slot, index: CommitteeIndex, slot_signature: BLSSignature) -> bool:
//	 committee = get_beacon_committee(state, slot, index)
//	 modulo = max(1, len(committee) // TARGET_AGGREGATORS_PER_COMMITTEE)
//	 return bytes_to_uint64(hash(slot_signature)[0:8]) % modulo == 0
func isAggregator(committeeCount uint64, slotSig []byte) (bool, error) {
	const targetAggregatorsPerCommittee uint64 = 16

	modulo := committeeCount / targetAggregatorsPerCommittee
	if modulo == 0 {
		// Modulo must be at least 1.
		modulo = 1
	}

	b := hashSha256(slotSig)
	return binary.LittleEndian.Uint64(b[:8])%modulo == 0, nil
}

var sha256Pool = sync.Pool{New: func() interface{} {
	return sha256.New()
}}

// hashSha256 defines a function that returns the sha256 checksum of the data passed in.
// https://github.com/ethereum/consensus-specs/blob/v0.9.3/specs/core/0_beacon-chain.md#hash
func hashSha256(data []byte) [32]byte {
	h, ok := sha256Pool.Get().(hash.Hash)
	if !ok {
		h = sha256.New()
	}
	defer sha256Pool.Put(h)
	h.Reset()

	var b [32]byte

	// The hash interface never returns an error, for that reason
	// we are not handling the error below. For reference, it is
	// stated here https://golang.org/pkg/hash/#Hash

	// #nosec G104
	h.Write(data)
	h.Sum(b[:0])

	return b
}

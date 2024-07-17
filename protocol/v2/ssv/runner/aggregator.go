package runner

import (
	"crypto/sha256"
	"encoding/json"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/runner/metrics"
)

type AggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon         beacon.BeaconNode
	network        specqbft.Network
	signer         spectypes.BeaconSigner
	operatorSigner *spectypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF

	metrics metrics.ConsensusMetrics
}

var _ Runner = &AggregatorRunner{}

func NewAggregatorRunner(
	beaconNetwork spectypes.BeaconNetwork,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon beacon.BeaconNode,
	network specqbft.Network,
	signer spectypes.BeaconSigner,
	operatorSigner *spectypes.OperatorSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			RunnerRoleType:     spectypes.RoleAggregator,
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

		metrics: metrics.NewConsensusMetrics(spectypes.BNRoleAggregator),
	}
}

func (r *AggregatorRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty, quorum uint64) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty, quorum)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AggregatorRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}
	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	// only 1 root, verified by basePreConsensusMsgProcessing
	root := roots[0]
	// reconstruct selection proof sig
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	duty := r.GetState().StartingDuty.(*spectypes.ValidatorDuty)

	logger.Debug("🧩 got partial signature quorum",
		zap.Any("signer", signedMsg.Messages[0].Signer), // TODO: always 1?
		fields.Slot(duty.Slot),
	)

	r.metrics.PauseDutyFullFlow()
	// get block data
	duty = r.GetState().StartingDuty.(*spectypes.ValidatorDuty)
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(duty.Slot, duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to submit aggregate and proof")
	}
	r.metrics.ContinueDutyFullFlow()

	byts, err := res.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal aggregate and proof")
	}
	input := &spectypes.ValidatorConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(logger, r, duty.Slot, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(logger *zap.Logger, signedMsg *spectypes.SignedSSVMessage) error {
	decided, encDecidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, signedMsg, &spectypes.ValidatorConsensusData{})
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.metrics.EndConsensus()
	r.metrics.StartPostConsensus()

	decidedValue := encDecidedValue.(*spectypes.ValidatorConsensusData)

	aggregateAndProof, err := decidedValue.GetAggregateAndProof()
	if err != nil {
		return errors.Wrap(err, "could not get aggregate and proof")
	}

	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(r, r.BaseRunner.State.StartingDuty.(*spectypes.ValidatorDuty), aggregateAndProof, decidedValue.Duty.Slot, spectypes.DomainAggregateAndProof)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}
	postConsensusMsg := &spectypes.PartialSignatureMessages{
		Type:     spectypes.PostConsensusPartialSig,
		Slot:     decidedValue.Duty.Slot,
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)

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

func (r *AggregatorRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePostConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing post consensus message")
	}

	if !quorum {
		return nil
	}
	r.metrics.EndPostConsensus()

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
		aggregateAndProof, err := cd.GetAggregateAndProof()
		if err != nil {
			return errors.Wrap(err, "could not get aggregate and proof")
		}

		msg := &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof,
			Signature: specSig,
		}

		start := time.Now()

		if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(msg); err != nil {
			r.metrics.RoleSubmissionFailed()
			logger.Error("❌ could not submit to Beacon chain reconstructed contribution and proof",
				fields.SubmissionTime(time.Since(start)),
				zap.Error(err))
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed aggregate")
		}

		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		logger.Debug("✅ successful submitted aggregate",
			fields.SubmissionTime(time.Since(start)),
		)
	}
	r.GetState().Finished = true

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
		return nil, spectypes.DomainAggregateAndProof, err
	}
	aggregateAndProof, err := cd.GetAggregateAndProof()
	if err != nil {
		return nil, phase0.DomainType{}, errors.Wrap(err, "could not get aggregate and proof")
	}

	return []ssz.HashRoot{aggregateAndProof}, spectypes.DomainAggregateAndProof, nil
}

// executeDuty steps:
// 1) sign a partial selection proof and wait for 2f+1 partial sigs from peers
// 2) reconstruct selection proof and send SubmitAggregateSelectionProof to BN
// 3) start consensus on duty + aggregation data
// 4) Once consensus decides, sign partial aggregation data and broadcast
// 5) collect 2f+1 partial sigs, reconstruct and broadcast valid SignedAggregateSubmitRequest sig to the BN
func (r *AggregatorRunner) executeDuty(logger *zap.Logger, duty spectypes.Duty) error {
	r.metrics.StartDutyFullFlow()
	r.metrics.StartPreConsensus()

	// sign selection proof
	msg, err := r.BaseRunner.signBeaconObject(r, duty.(*spectypes.ValidatorDuty), spectypes.SSZUint64(duty.DutySlot()), duty.DutySlot(), spectypes.DomainSelectionProof)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}
	msgs := &spectypes.PartialSignatureMessages{
		Type:     spectypes.SelectionProofPartialSig,
		Slot:     duty.DutySlot(),
		Messages: []*spectypes.PartialSignatureMessage{msg},
	}

	msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
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

func (r *AggregatorRunner) GetSigner() spectypes.BeaconSigner {
	return r.signer
}
func (r *AggregatorRunner) GetOperatorSigner() *spectypes.OperatorSigner {
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
		return [32]byte{}, errors.Wrap(err, "could not encode DutyRunnerState")
	}
	ret := sha256.Sum256(marshaledRoot)
	return ret, nil
}

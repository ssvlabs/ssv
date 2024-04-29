package runner

import (
	"crypto/sha256"
	"encoding/json"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	"github.com/bloxapp/ssv-spec/types"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/protocol/v2/ssv/runner/metrics"
	ssvtypes "github.com/bloxapp/ssv/protocol/v2/types"
)

type AggregatorRunner struct {
	BaseRunner *BaseRunner

	beacon         specssv.BeaconNode
	network        specssv.Network
	signer         genesisspectypes.KeyManager
	beaconSigner   spectypes.BeaconSigner
	operatorSigner spectypes.OperatorSigner
	valCheck       specqbft.ProposedValueCheckF
	metrics        metrics.ConsensusMetrics
}

var _ Runner = &AggregatorRunner{}

func NewAggregatorRunner(
	beaconNetwork spectypes.BeaconNetwork,
	shares *map[phase0.ValidatorIndex]*spectypes.Share,
	qbftController *controller.Controller,
	beacon specssv.BeaconNode,
	network specssv.Network,
	signer genesisspectypes.KeyManager,
	beaconSigner spectypes.BeaconSigner,
	valCheck specqbft.ProposedValueCheckF,
	highestDecidedSlot phase0.Slot,
) Runner {
	return &AggregatorRunner{
		BaseRunner: &BaseRunner{
			BeaconRoleType:     genesisspectypes.BNRoleAggregator,
			RunnerRoleType:     spectypes.RoleAggregator,
			BeaconNetwork:      beaconNetwork,
			Shares:             *shares,
			QBFTController:     qbftController,
			highestDecidedSlot: highestDecidedSlot,
		},
		beacon:       beacon,
		network:      network,
		signer:       signer,
		beaconSigner: beaconSigner,
		valCheck:     valCheck,
		metrics:      metrics.NewConsensusMetrics(spectypes.BNRoleAggregator),
	}
}

func (r *AggregatorRunner) StartNewDuty(logger *zap.Logger, duty spectypes.Duty) error {
	return r.BaseRunner.baseStartNewDuty(logger, r, duty)
}

// HasRunningDuty returns true if a duty is already running (StartNewDuty called and returned nil)
func (r *AggregatorRunner) HasRunningDuty() bool {
	return r.BaseRunner.hasRunningDuty()
}

func (r *AggregatorRunner) ProcessPreConsensus(logger *zap.Logger, signedMsg ssvtypes.PartialSignatureMessages) error {
	quorum, roots, err := r.BaseRunner.basePreConsensusMsgProcessing(r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing selection proof message")
	}
	// quorum returns true only once (first time quorum achieved)
	if !quorum {
		return nil
	}

	r.metrics.EndPreConsensus()

	// only 1 root, verified by basePreConsensusMsgProcessing
	root := roots[0]
	// reconstruct selection proof sig
	fullSig, err := r.GetState().ReconstructBeaconSig(r.GetState().PreConsensusContainer, root, r.GetShare().ValidatorPubKey[:], r.GetShare().ValidatorIndex)
	if err != nil {
		// If the reconstructed signature verification failed, fall back to verifying each partial signature
		r.BaseRunner.FallBackAndVerifyEachSignature(r.GetState().PreConsensusContainer, root, r.GetShare().Committee, r.GetShare().ValidatorIndex)
		return errors.Wrap(err, "got pre-consensus quorum but it has invalid signatures")
	}

	duty := r.GetState().StartingDuty.(*spectypes.BeaconDuty)

	logger.Debug("🧩 got partial signature quorum",
		zap.Any("signer", signedMsg.GetSigner()),
		fields.Slot(duty.DutySlot()),
	)

	r.metrics.PauseDutyFullFlow()

	// get block data
	// TODO convert to beacon duty?
	res, ver, err := r.GetBeaconNode().SubmitAggregateSelectionProof(duty.DutySlot(), duty.CommitteeIndex, duty.CommitteeLength, duty.ValidatorIndex, fullSig)
	if err != nil {
		return errors.Wrap(err, "failed to submit aggregate and proof")
	}

	r.metrics.ContinueDutyFullFlow()
	r.metrics.StartConsensus()

	byts, err := res.MarshalSSZ()
	if err != nil {
		return errors.Wrap(err, "could not marshal aggregate and proof")
	}
	input := &spectypes.ConsensusData{
		Duty:    *duty,
		Version: ver,
		DataSSZ: byts,
	}

	if err := r.BaseRunner.decide(logger, r, input); err != nil {
		return errors.Wrap(err, "can't start new duty runner instance for duty")
	}

	return nil
}

func (r *AggregatorRunner) ProcessConsensus(logger *zap.Logger, signedMsg ssvtypes.SignedMessage) error {
	decided, decidedValue, err := r.BaseRunner.baseConsensusMsgProcessing(logger, r, signedMsg)
	if err != nil {
		return errors.Wrap(err, "failed processing consensus message")
	}

	// Decided returns true only once so if it is true it must be for the current running instance
	if !decided {
		return nil
	}

	r.metrics.EndConsensus()
	r.metrics.StartPostConsensus()

	aggregateAndProof, err := decidedValue.GetAggregateAndProof()
	if err != nil {
		return errors.Wrap(err, "could not get aggregate and proof")
	}

	// specific duty sig
	msg, err := r.BaseRunner.signBeaconObject(r, r.BaseRunner.State.StartingDuty.(*spectypes.BeaconDuty),
		aggregateAndProof,
		decidedValue.Duty.Slot,
		spectypes.DomainAggregateAndProof)
	if err != nil {
		return errors.Wrap(err, "failed signing attestation data")
	}

	if true {
		postConsensusMsg := &genesisspectypes.PartialSignatureMessages{
			Type:     genesisspectypes.PostConsensusPartialSig,
			Slot:     decidedValue.Duty.DutySlot(),
			Messages: []*genesisspectypes.PartialSignatureMessage{msg.(*genesisspectypes.PartialSignatureMessage)},
		}

		postSignedMsg, err := r.BaseRunner.signPostConsensusMsg(r, postConsensusMsg)
		if err != nil {
			return errors.Wrap(err, "could not sign post consensus msg")
		}

		data, err := postSignedMsg.Encode()
		if err != nil {
			return errors.Wrap(err, "failed to encode post consensus signature msg")
		}

		msgToBroadcast := &genesisspectypes.SSVMessage{
			MsgType: genesisspectypes.SSVPartialSignatureMsgType,
			MsgID:   genesisspectypes.NewMsgID(genesisspectypes.DomainType(r.GetShare().DomainType), r.GetShare().ValidatorPubKey[:], r.BaseRunner.BeaconRoleType),
			Data:    data,
		}

		if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
			return errors.Wrap(err, "can't broadcast partial post consensus sig")
		}
	} else {

		postConsensusMsg := &spectypes.PartialSignatureMessages{
			Type:     spectypes.PostConsensusPartialSig,
			Slot:     decidedValue.Duty.Slot,
			Messages: []*spectypes.PartialSignatureMessage{msg.(*spectypes.PartialSignatureMessage)},
		}

		msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
		msgToBroadcast, err := spectypes.PartialSignatureMessagesToSignedSSVMessage(postConsensusMsg, msgID, r.operatorSigner)
		if err != nil {
			return errors.Wrap(err, "could not sign post-consensus partial signature message")
		}
		if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
			return errors.Wrap(err, "can't broadcast partial post consensus sig")
		}
	}
	return nil
}

func (r *AggregatorRunner) ProcessPostConsensus(logger *zap.Logger, signedMsg ssvtypes.PartialSignatureMessages) error {
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

		aggregateAndProof, err := r.GetState().DecidedValue.GetAggregateAndProof()
		if err != nil {
			return errors.Wrap(err, "could not get aggregate and proof")
		}

		msg := &phase0.SignedAggregateAndProof{
			Message:   aggregateAndProof,
			Signature: specSig,
		}

		proofSubmissionEnd := r.metrics.StartBeaconSubmission()

		if err := r.GetBeaconNode().SubmitSignedAggregateSelectionProof(msg); err != nil {
			r.metrics.RoleSubmissionFailed()
			return errors.Wrap(err, "could not submit to Beacon chain reconstructed signed aggregate")
		}

		proofSubmissionEnd()
		r.metrics.EndDutyFullFlow(r.GetState().RunningInstance.State.Round)
		r.metrics.RoleSubmitted()

		logger.Debug("✅ successful submitted aggregate")
	}
	r.GetState().Finished = true

	return nil
}

func (r *AggregatorRunner) expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	return []ssz.HashRoot{spectypes.SSZUint64(r.GetState().StartingDuty.DutySlot())}, spectypes.DomainSelectionProof, nil
}

// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
func (r *AggregatorRunner) expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error) {
	aggregateAndProof, err := r.GetState().DecidedValue.GetAggregateAndProof()
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
	msg, err := r.BaseRunner.signBeaconObject(r, duty.(*spectypes.BeaconDuty), spectypes.SSZUint64(duty.DutySlot()), duty.DutySlot(),
		types.DomainSelectionProof)
	if err != nil {
		return errors.Wrap(err, "could not sign randao")
	}

	if true {
		msgs := genesisspectypes.PartialSignatureMessages{
			Type:     genesisspectypes.SelectionProofPartialSig,
			Slot:     duty.DutySlot(),
			Messages: []*genesisspectypes.PartialSignatureMessage{msg.(*genesisspectypes.PartialSignatureMessage)},
		}

		// sign msg
		signature, err := r.GetGenesisSigner().SignRoot(msgs, genesisspectypes.PartialSignatureType, r.GetShare().SharePubKey)
		if err != nil {
			return errors.Wrap(err, "could not sign PartialSignatureMessage for selection proof")
		}
		signedPartialMsg := &genesisspectypes.SignedPartialSignatureMessage{
			Message:   msgs,
			Signature: signature,
			Signer:    r.GetOperatorSigner().GetOperatorID(),
		}

		// broadcast
		data, err := signedPartialMsg.Encode()
		if err != nil {
			return errors.Wrap(err, "failed to encode selection proof pre-consensus signature msg")
		}
		msgToBroadcast := &genesisspectypes.SSVMessage{
			MsgType: genesisspectypes.SSVPartialSignatureMsgType,
			MsgID:   genesisspectypes.NewMsgID(genesisspectypes.DomainType(r.GetShare().DomainType), r.GetShare().ValidatorPubKey[:], r.BaseRunner.BeaconRoleType),
			Data:    data,
		}
		if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
			return errors.Wrap(err, "can't broadcast partial selection proof sig")
		}
	} else {
		msgs := &spectypes.PartialSignatureMessages{
			Type:     spectypes.SelectionProofPartialSig,
			Slot:     duty.DutySlot(),
			Messages: []*spectypes.PartialSignatureMessage{msg.(*spectypes.PartialSignatureMessage)},
		}

		msgID := spectypes.NewMsgID(r.GetShare().DomainType, r.GetShare().ValidatorPubKey[:], r.BaseRunner.RunnerRoleType)
		msgToBroadcast, err := spectypes.PartialSignatureMessagesToSignedSSVMessage(msgs, msgID, r.operatorSigner)
		if err != nil {
			return errors.Wrap(err, "could not sign pre-consensus partial signature message")
		}

		if err := r.GetNetwork().Broadcast(msgToBroadcast); err != nil {
			return errors.Wrap(err, "can't broadcast partial selection proof sig")
		}
	}
	return nil
}

func (r *AggregatorRunner) GetBaseRunner() *BaseRunner {
	return r.BaseRunner
}

func (r *AggregatorRunner) GetNetwork() specssv.Network {
	return r.network
}

func (r *AggregatorRunner) GetBeaconNode() specssv.BeaconNode {
	return r.beacon
}

func (r *AggregatorRunner) GetShare() *spectypes.Share {
	for _, share := range r.BaseRunner.Shares {
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

func (r *AggregatorRunner) GetGenesisSigner() genesisspectypes.KeyManager {
	return r.signer
}

func (r *AggregatorRunner) GetSigner() spectypes.BeaconSigner {
	return r.beaconSigner
}

func (r *AggregatorRunner) GetOperatorSigner() spectypes.OperatorSigner {
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

package runner

import (
	"bytes"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	"github.com/bloxapp/ssv/utils/logex"
)

// DutyRunners is a map of duty runners mapped by msg id hex.
type DutyRunners map[spectypes.BeaconRole]Runner

// DutyRunnerForMsgID returns a Runner from the provided msg ID, or nil if not found
func (dr DutyRunners) DutyRunnerForMsgID(msgID spectypes.MessageID) Runner {
	role := msgID.GetRoleType()
	return dr[role]
}

func (dr DutyRunners) Identifiers() []spectypes.MessageID {
	var identifiers []spectypes.MessageID
	for role, r := range dr {
		share := r.GetBaseRunner().Share
		if share == nil { // TODO: handle missing share?
			continue
		}
		i := spectypes.NewMsgID(r.GetBaseRunner().Share.ValidatorPubKey, role)
		identifiers = append(identifiers, i)
	}
	return identifiers
}

type Getters interface {
	GetBaseRunner() *BaseRunner
	GetBeaconNode() specssv.BeaconNode
	GetValCheckF() specqbft.ProposedValueCheckF
	GetSigner() spectypes.KeyManager
	GetNetwork() specssv.Network
}

type Runner interface {
	spectypes.Encoder
	spectypes.Root
	Getters

	Init() error
	StartNewDuty(duty *spectypes.Duty) error
	HasRunningDuty() bool
	ProcessPreConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error
	ProcessConsensus(msg *specqbft.SignedMessage) error
	ProcessPostConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error

	expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, spec.DomainType, error)
	executeDuty(duty *spectypes.Duty) error
}

type BaseRunner struct {
	State          *State
	Share          *spectypes.Share
	QBFTController *controller.Controller
	BeaconNetwork  spectypes.BeaconNetwork
	BeaconRoleType spectypes.BeaconRole
}

func (b *BaseRunner) Init() error {
	return nil
}

func (b *BaseRunner) baseStartNewDuty(runner Runner, duty *spectypes.Duty) error {
	if err := b.canStartNewDuty(); err != nil {
		return err
	}
	b.State = NewRunnerState(b.Share.Quorum, duty)
	return runner.executeDuty(duty)
}

func (b *BaseRunner) canStartNewDuty() error {
	if b.State == nil {
		return nil
	}

	// check if instance running first as we can't start new duty if it does
	if instance := b.State.RunningInstance; instance != nil {
		// check consensus decided
		if decided, _ := instance.IsDecided(); !decided {
			return errors.New("consensus on duty is running")
		}
	}
	return nil
}

func (b *BaseRunner) basePreConsensusMsgProcessing(runner Runner, signedMsg *specssv.SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := b.validatePreConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid pre-consensus message")
	}

	roots := make([][]byte, 0)
	anyQuorum := false
	for _, msg := range signedMsg.Message.Messages {
		prevQuorum := b.State.PreConsensusContainer.HasQuorum(msg.SigningRoot)

		if err := b.State.PreConsensusContainer.AddSignature(msg); err != nil {
			return false, nil, errors.Wrap(err, "could not add partial randao signature")
		}

		if prevQuorum {
			continue
		}

		quorum := b.State.PreConsensusContainer.HasQuorum(msg.SigningRoot)
		if quorum {
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots, nil
}

func (b *BaseRunner) baseConsensusMsgProcessing(runner Runner, msg *specqbft.SignedMessage) (decided bool, decidedValue *spectypes.ConsensusData, err error) {
	prevDecided := false
	if b.HasRunningDuty() && b.State != nil && b.State.RunningInstance != nil {
		prevDecided, _ = b.State.RunningInstance.IsDecided()
	}

	decidedMsg, err := b.QBFTController.ProcessMsg(msg)
	if err != nil {
		return false, nil, errors.Wrap(err, "failed to process consensus msg")
	}

	// we allow all consensus msgs to be processed, once the process finishes we check if there is an actual running duty
	if !b.HasRunningDuty() {
		return false, nil, err
	}
	if decideCorrectly, err := b.didDecideCorrectly(prevDecided, decidedMsg); !decideCorrectly {
		return false, nil, err
	}

	// get decided value
	decidedData, err := decidedMsg.Message.GetCommitData()
	if err != nil {
		return false, nil, errors.Wrap(err, "failed to get decided data")
	}

	decidedValue = &spectypes.ConsensusData{}
	if err := decidedValue.Decode(decidedData.Data); err != nil {
		return true, nil, errors.Wrap(err, "failed to parse decided value to ConsensusData")
	}

	if err := b.validateDecidedConsensusData(runner, decidedValue); err != nil {
		return true, nil, errors.Wrap(err, "decided ConsensusData invalid")
	}

	runner.GetBaseRunner().State.DecidedValue = decidedValue
	runner.GetBaseRunner().State.LastHeight = decidedMsg.Message.Height

	return true, decidedValue, nil
}

func (b *BaseRunner) basePostConsensusMsgProcessing(signedMsg *specssv.SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := b.validatePostConsensusMsg(signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid post-consensus message")
	}

	logex.GetLogger().Info("received valid partial signature")
	roots := make([][]byte, 0)
	anyQuorum := false
	for _, msg := range signedMsg.Message.Messages {
		prevQuorum := b.State.PostConsensusContainer.HasQuorum(msg.SigningRoot)

		logex.GetLogger().Info("signature added to container")
		if err := b.State.PostConsensusContainer.AddSignature(msg); err != nil {
			return false, nil, errors.Wrap(err, "could not add partial post consensus signature")
		}

		if prevQuorum {
			continue
		}

		quorum := b.State.PostConsensusContainer.HasQuorum(msg.SigningRoot)
		if quorum {
			logex.GetLogger().Info("signature quorum achieved")
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots, nil
}

func (b *BaseRunner) didDecideCorrectly(prevDecided bool, decidedMsg *specqbft.SignedMessage) (bool, error) {
	decided := decidedMsg != nil
	decidedRunningInstance := decided && decidedMsg.Message.Height == b.State.RunningInstance.GetHeight()

	if !decided {
		return false, nil
	}
	if !decidedRunningInstance {
		return false, errors.New("decided wrong instance")
	}
	// verify we decided running instance only, if not we do not proceed
	if prevDecided {
		return false, nil
	}

	return true, nil
}

func (b *BaseRunner) validatePreConsensusMsg(runner Runner, signedMsg *specssv.SignedPartialSignatureMessage) error {
	if !b.HasRunningDuty() {
		return errors.New("no running duty")
	}

	if err := b.validatePartialSigMsg(signedMsg, b.State.StartingDuty.Slot); err != nil {
		return err
	}

	roots, domain, err := runner.expectedPreConsensusRootsAndDomain()
	if err != nil {
		return err
	}

	return b.verifyExpectedRoot(runner, signedMsg, roots, domain)
}

func (b *BaseRunner) validatePostConsensusMsg(msg *specssv.SignedPartialSignatureMessage) error {
	if !b.HasRunningDuty() {
		return errors.New("no running duty")
	}

	if err := b.validatePartialSigMsg(msg, b.State.DecidedValue.Duty.Slot); err != nil {
		return errors.Wrap(err, "post consensus msg invalid")
	}

	return nil
}

func (b *BaseRunner) decide(runner Runner, input *spectypes.ConsensusData) error {
	byts, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode ConsensusData")
	}

	valc := runner.GetValCheckF()
	if valc == nil {
		return errors.New("val check nil")
	}
	if err := valc(byts); err != nil {
		return errors.Wrap(err, "input data invalid")
	}

	ctrl := runner.GetBaseRunner().QBFTController
	if err := ctrl.StartNewInstance(byts); err != nil {
		return errors.Wrap(err, "could not start new QBFT instance")
	}
	newInstance := ctrl.InstanceForHeight(ctrl.Height)
	if newInstance == nil {
		return errors.New("could not find newly created QBFT instance")
	}
	runner.GetBaseRunner().State.RunningInstance = newInstance

	return nil
}

func (b *BaseRunner) HasRunningDuty() bool {
	if b.State == nil {
		return false
	}
	return !b.State.Finished
}

func (b *BaseRunner) signBeaconObject(
	runner Runner,
	obj ssz.HashRoot,
	slot spec.Slot,
	domainType spec.DomainType,
) (*specssv.PartialSignatureMessage, error) {
	epoch := runner.GetBaseRunner().BeaconNetwork.EstimatedEpochAtSlot(slot)
	domain, err := runner.GetBeaconNode().DomainData(epoch, domainType)
	if err != nil {
		return nil, errors.Wrap(err, "could not get beacon domain")
	}

	sig, r, err := runner.GetSigner().SignBeaconObject(obj, domain, runner.GetBaseRunner().Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign beacon object")
	}

	return &specssv.PartialSignatureMessage{
		Slot:             slot,
		PartialSignature: sig,
		SigningRoot:      r,
		Signer:           runner.GetBaseRunner().Share.OperatorID,
	}, nil
}

func (b *BaseRunner) validatePartialSigMsg(
	signedMsg *specssv.SignedPartialSignatureMessage,
	slot spec.Slot,
) error {
	if err := signedMsg.Validate(); err != nil {
		return errors.Wrap(err, "SignedPartialSignatureMessage invalid")
	}

	if err := signedMsg.GetSignature().VerifyByOperators(signedMsg, b.Share.DomainType, spectypes.PartialSignatureType, b.Share.Committee); err != nil {
		return errors.Wrap(err, "failed to verify PartialSignature")
	}

	for _, msg := range signedMsg.Message.Messages {
		if slot != msg.Slot {
			return errors.New("wrong slot")
		}

		if err := b.verifyBeaconPartialSignature(msg); err != nil {
			return errors.Wrap(err, "could not verify Beacon partial Signature")
		}
	}

	return nil
}

func (b *BaseRunner) verifyBeaconPartialSignature(msg *specssv.PartialSignatureMessage) error {
	signer := msg.Signer
	signature := msg.PartialSignature
	root := msg.SigningRoot

	for _, n := range b.Share.Committee {
		if n.GetID() == signer {
			pk := &bls.PublicKey{}
			if err := pk.Deserialize(n.GetPublicKey()); err != nil {
				return errors.Wrap(err, "could not deserialized pk")
			}
			sig := &bls.Sign{}
			if err := sig.Deserialize(signature); err != nil {
				return errors.Wrap(err, "could not deserialized Signature")
			}

			// verify
			if !sig.VerifyByte(pk, root) {
				return errors.New("wrong signature")
			}
			return nil
		}
	}
	return errors.New("unknown signer")
}

func (b *BaseRunner) validateDecidedConsensusData(runner Runner, val *spectypes.ConsensusData) error {
	byts, err := val.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode decided value")
	}
	if err := runner.GetValCheckF()(byts); err != nil {
		return errors.Wrap(err, "decided value is invalid")
	}

	return nil
}

func (b *BaseRunner) verifyExpectedRoot(runner Runner, signedMsg *specssv.SignedPartialSignatureMessage, expectedRootObjs []ssz.HashRoot, domain spec.DomainType) error {
	if len(expectedRootObjs) != len(signedMsg.Message.Messages) {
		return errors.New("wrong expected roots count")
	}
	for i, msg := range signedMsg.Message.Messages {
		epoch := b.BeaconNetwork.EstimatedEpochAtSlot(b.State.StartingDuty.Slot)
		d, err := runner.GetBeaconNode().DomainData(epoch, domain)
		if err != nil {
			return errors.Wrap(err, "could not get pre consensus root domain")
		}

		r, err := spectypes.ComputeETHSigningRoot(expectedRootObjs[i], d)
		if err != nil {
			return errors.Wrap(err, "could not compute ETH signing root")
		}
		if !bytes.Equal(r[:], msg.SigningRoot) {
			return errors.New("wrong pre consensus signing root")
		}
	}
	return nil
}

func (b *BaseRunner) signPostConsensusMsg(runner Runner, msg *specssv.PartialSignatureMessages) (*specssv.SignedPartialSignatureMessage, error) {
	signature, err := runner.GetSigner().SignRoot(msg, spectypes.PartialSignatureType, b.Share.SharePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "could not sign PartialSignatureMessage for PostConsensusContainer")
	}

	return &specssv.SignedPartialSignatureMessage{
		Message:   *msg,
		Signature: signature,
		Signer:    b.Share.OperatorID,
	}, nil
}

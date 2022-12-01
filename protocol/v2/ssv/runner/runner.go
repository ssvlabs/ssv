package runner

import (
	"fmt"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"

	"github.com/pkg/errors"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
)

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

	// StartNewDuty starts a new duty for the runner, returns error if can't
	StartNewDuty(duty *spectypes.Duty) error
	// HasRunningDuty returns true if it has a running duty
	HasRunningDuty() bool
	// ProcessPreConsensus processes all pre-consensus msgs, returns error if can't process
	ProcessPreConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error
	// ProcessConsensus processes all consensus msgs, returns error if can't process
	ProcessConsensus(msg *specqbft.SignedMessage) error
	// ProcessPostConsensus processes all post-consensus msgs, returns error if can't process
	ProcessPostConsensus(signedMsg *specssv.SignedPartialSignatureMessage) error
	// expectedPreConsensusRootsAndDomain an INTERNAL function, returns the expected pre-consensus roots to sign
	expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, spec.DomainType, error)
	// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
	expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, spec.DomainType, error)
	// executeDuty an INTERNAL function, executes a duty.
	executeDuty(duty *spectypes.Duty) error
}

type BaseRunner struct {
	State          *State
	Share          *spectypes.Share
	QBFTController *controller.Controller
	BeaconNetwork  spectypes.BeaconNetwork
	BeaconRoleType spectypes.BeaconRole
}

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(runner Runner, duty *spectypes.Duty) error {
	if err := b.canStartNewDuty(); err != nil {
		return err
	}
	b.State = NewRunnerState(b.Share.Quorum, duty)
	return runner.executeDuty(duty)
}

// canStartNewDuty is a base func that all runner implementation can call to decide if a new duty can start
func (b *BaseRunner) canStartNewDuty() error {
	if b.State == nil {
		return nil
	}

	// check if instance running first as we can't start new duty if it does
	if b.State.RunningInstance != nil {
		// check consensus decided
		if decided, _ := b.State.RunningInstance.IsDecided(); !decided {
			return errors.New("consensus on duty is running")
		}
	}
	return nil
}

// basePreConsensusMsgProcessing is a base func that all runner implementation can call for processing a pre-consensus msg
func (b *BaseRunner) basePreConsensusMsgProcessing(runner Runner, signedMsg *specssv.SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := b.validatePreConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid pre-consensus message")
	}

	hasQuorum, roots, err := b.basePartialSigMsgProcessing(signedMsg, b.State.PreConsensusContainer)
	return hasQuorum, roots, errors.Wrap(err, "could not process pre-consensus partial signature msg")
}

// baseConsensusMsgProcessing is a base func that all runner implementation can call for processing a consensus msg
func (b *BaseRunner) baseConsensusMsgProcessing(runner Runner, msg *specqbft.SignedMessage) (decided bool, decidedValue *spectypes.ConsensusData, err error) {
	prevDecided := false
	if b.hasRunningDuty() && b.State != nil && b.State.RunningInstance != nil {
		prevDecided, _ = b.State.RunningInstance.IsDecided()
	}

	decidedMsg, err := b.QBFTController.ProcessMsg(msg)
	if err != nil {
		return false, nil, err
	}

	// we allow all consensus msgs to be processed, once the process finishes we check if there is an actual running duty
	if !b.hasRunningDuty() {
		return false, nil, err
	}

	if decideCorrectly, err := b.didDecideCorrectly(prevDecided, decidedMsg); !decideCorrectly {
		return false, nil, err
	} else {
		if inst := b.QBFTController.StoredInstances.FindInstance(decidedMsg.Message.Height); inst != nil {
			storedInstance := &qbftstorage.StoredInstance{
				State:          inst.State,
				DecidedMessage: decidedMsg,
			}
			if err = b.QBFTController.SaveHighestInstance(storedInstance); err != nil {
				fmt.Printf("failed to save instance: %s\n", err.Error())
			}
		}
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

	return true, decidedValue, nil
}

// basePostConsensusMsgProcessing is a base func that all runner implementation can call for processing a post-consensus msg
func (b *BaseRunner) basePostConsensusMsgProcessing(runner Runner, signedMsg *specssv.SignedPartialSignatureMessage) (bool, [][]byte, error) {
	if err := b.validatePostConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid post-consensus message")
	}

	hasQuorum, roots, err := b.basePartialSigMsgProcessing(signedMsg, b.State.PostConsensusContainer)
	return hasQuorum, roots, errors.Wrap(err, "could not process post-consensus partial signature msg")
}

// basePartialSigMsgProcessing adds an already validated partial msg to the container, checks for quorum and returns true (and roots) if quorum exists
func (b *BaseRunner) basePartialSigMsgProcessing(
	signedMsg *specssv.SignedPartialSignatureMessage,
	container *specssv.PartialSigContainer,
) (bool, [][]byte, error) {
	roots := make([][]byte, 0)
	anyQuorum := false
	for _, msg := range signedMsg.Message.Messages {
		prevQuorum := container.HasQuorum(msg.SigningRoot)

		container.AddSignature(msg)

		if prevQuorum {
			continue
		}

		quorum := container.HasQuorum(msg.SigningRoot)
		if quorum {
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots, nil
}

// didDecideCorrectly returns true if the expected consensus instance decided correctly
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

func (b *BaseRunner) decide(runner Runner, input *spectypes.ConsensusData) error {
	byts, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode ConsensusData")
	}

	if err := runner.GetValCheckF()(byts); err != nil {
		return errors.Wrap(err, "input data invalid")
	}

	if err := runner.GetBaseRunner().QBFTController.StartNewInstance(byts); err != nil {
		return errors.Wrap(err, "could not start new QBFT instance")
	}
	newInstance := runner.GetBaseRunner().QBFTController.InstanceForHeight(runner.GetBaseRunner().QBFTController.Height)
	if newInstance == nil {
		return errors.New("could not find newly created QBFT instance")
	}

	runner.GetBaseRunner().State.RunningInstance = newInstance

	// registers a timeout handler
	b.registerTimeoutHandler(newInstance, runner.GetBaseRunner().QBFTController.Height)

	return nil
}

// hasRunningDuty returns true if a new duty didn't start or an existing duty marked as finished
func (b *BaseRunner) hasRunningDuty() bool {
	if b.State == nil {
		return false
	}
	return !b.State.Finished
}

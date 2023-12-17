package runner

import (
	"sync"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	specqbft "github.com/bloxapp/ssv-spec/qbft"
	specssv "github.com/bloxapp/ssv-spec/ssv"
	spectypes "github.com/bloxapp/ssv-spec/types"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/protocol/v2/qbft/controller"
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
	StartNewDuty(logger *zap.Logger, duty *spectypes.Duty) error
	// HasRunningDuty returns true if it has a running duty
	HasRunningDuty() bool
	// ProcessPreConsensus processes all pre-consensus msgs, returns error if can't process
	ProcessPreConsensus(logger *zap.Logger, signedMsg *spectypes.SignedPartialSignatureMessage) error
	// ProcessConsensus processes all consensus msgs, returns error if can't process
	ProcessConsensus(logger *zap.Logger, msg *specqbft.SignedMessage) error
	// ProcessPostConsensus processes all post-consensus msgs, returns error if can't process
	ProcessPostConsensus(logger *zap.Logger, signedMsg *spectypes.SignedPartialSignatureMessage) error
	// expectedPreConsensusRootsAndDomain an INTERNAL function, returns the expected pre-consensus roots to sign
	expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, spec.DomainType, error)
	// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
	expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, spec.DomainType, error)
	// executeDuty an INTERNAL function, executes a duty.
	executeDuty(logger *zap.Logger, duty *spectypes.Duty) error
}

type BaseRunner struct {
	mtx            sync.RWMutex
	State          *State
	Share          *spectypes.Share
	QBFTController *controller.Controller
	BeaconNetwork  spectypes.BeaconNetwork
	BeaconRoleType spectypes.BeaconRole

	// implementation vars
	TimeoutF TimeoutF `json:"-"`

	// highestDecidedSlot holds the highest decided duty slot and gets updated after each decided is reached
	highestDecidedSlot spec.Slot
}

// SetHighestDecidedSlot set highestDecidedSlot for base runner
func (b *BaseRunner) SetHighestDecidedSlot(slot spec.Slot) {
	b.highestDecidedSlot = slot
}

// setupForNewDuty is sets the runner for a new duty
func (b *BaseRunner) baseSetupForNewDuty(duty *spectypes.Duty) {
	state := NewRunnerState(b.Share.Quorum, duty)

	// TODO: potentially incomplete locking of b.State. runner.Execute(duty) has access to
	// b.State but currently does not write to it
	b.mtx.Lock() // writes to b.State
	b.State = state
	b.mtx.Unlock()
}

func NewBaseRunner(
	state *State,
	share *spectypes.Share,
	controller *controller.Controller,
	beaconNetwork spectypes.BeaconNetwork,
	beaconRoleType spectypes.BeaconRole,
	highestDecidedSlot spec.Slot,
) *BaseRunner {
	return &BaseRunner{
		State:              state,
		Share:              share,
		QBFTController:     controller,
		BeaconNetwork:      beaconNetwork,
		BeaconRoleType:     beaconRoleType,
		highestDecidedSlot: highestDecidedSlot,
	}
}

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(logger *zap.Logger, runner Runner, duty *spectypes.Duty) error {
	if err := b.ShouldProcessDuty(duty); err != nil {
		return errors.Wrap(err, "can't start duty")
	}
	b.baseSetupForNewDuty(duty)
	return runner.executeDuty(logger, duty)
}

// baseStartNewBeaconDuty is a base func that all runner implementation can call to start a non-beacon duty
func (b *BaseRunner) baseStartNewNonBeaconDuty(logger *zap.Logger, runner Runner, duty *spectypes.Duty) error {
	if err := b.ShouldProcessNonBeaconDuty(duty); err != nil {
		return errors.Wrap(err, "can't start non-beacon duty")
	}
	b.baseSetupForNewDuty(duty)
	return runner.executeDuty(logger, duty)
}

// basePreConsensusMsgProcessing is a base func that all runner implementation can call for processing a pre-consensus msg
func (b *BaseRunner) basePreConsensusMsgProcessing(runner Runner, signedMsg *spectypes.SignedPartialSignatureMessage) (bool, [][32]byte, error) {
	if err := b.ValidatePreConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid pre-consensus message")
	}

	hasQuorum, roots, err := b.basePartialSigMsgProcessing(signedMsg, b.State.PreConsensusContainer)
	return hasQuorum, roots, errors.Wrap(err, "could not process pre-consensus partial signature msg")
}

// baseConsensusMsgProcessing is a base func that all runner implementation can call for processing a consensus msg
func (b *BaseRunner) baseConsensusMsgProcessing(logger *zap.Logger, runner Runner, msg *specqbft.SignedMessage) (decided bool, decidedValue *spectypes.ConsensusData, err error) {
	prevDecided := false
	if b.hasRunningDuty() && b.State != nil && b.State.RunningInstance != nil {
		prevDecided, _ = b.State.RunningInstance.IsDecided()
	}

	// TODO: revert after pre-consensus liveness is fixed
	if false {
		if err := b.processPreConsensusJustification(logger, runner, b.highestDecidedSlot, msg); err != nil {
			return false, nil, errors.Wrap(err, "invalid pre-consensus justification")
		}
	}

	decidedMsg, err := b.QBFTController.ProcessMsg(logger, msg)
	b.compactInstanceIfNeeded(msg)
	if err != nil {
		return false, nil, err
	}

	// we allow all consensus msgs to be processed, once the process finishes we check if there is an actual running duty
	// do not return error if no running duty
	if !b.hasRunningDuty() {
		return false, nil, nil
	}

	if decideCorrectly, err := b.didDecideCorrectly(prevDecided, decidedMsg); !decideCorrectly {
		return false, nil, err
	} else {
		if inst := b.QBFTController.StoredInstances.FindInstance(decidedMsg.Message.Height); inst != nil {
			logger := logger.With(
				zap.Uint64("msg_height", uint64(msg.Message.Height)),
				zap.Uint64("ctrl_height", uint64(b.QBFTController.Height)),
				zap.Any("signers", msg.Signers),
			)
			if err = b.QBFTController.SaveInstance(inst, decidedMsg); err != nil {
				logger.Debug("❗ failed to save instance", zap.Error(err))
			} else {
				logger.Debug("💾 saved instance")
			}
		}
	}

	// decode consensus data
	decidedValue = &spectypes.ConsensusData{}
	if err := decidedValue.Decode(decidedMsg.FullData); err != nil {
		return true, nil, errors.Wrap(err, "failed to parse decided value to ConsensusData")
	}

	// update the highest decided slot
	b.highestDecidedSlot = decidedValue.Duty.Slot

	if err := b.validateDecidedConsensusData(runner, decidedValue); err != nil {
		return true, nil, errors.Wrap(err, "decided ConsensusData invalid")
	}

	runner.GetBaseRunner().State.DecidedValue = decidedValue

	return true, decidedValue, nil
}

// basePostConsensusMsgProcessing is a base func that all runner implementation can call for processing a post-consensus msg
func (b *BaseRunner) basePostConsensusMsgProcessing(logger *zap.Logger, runner Runner, signedMsg *spectypes.SignedPartialSignatureMessage) (bool, [][32]byte, error) {
	if err := b.ValidatePostConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid post-consensus message")
	}

	hasQuorum, roots, err := b.basePartialSigMsgProcessing(signedMsg, b.State.PostConsensusContainer)
	return hasQuorum, roots, errors.Wrap(err, "could not process post-consensus partial signature msg")
}

// basePartialSigMsgProcessing adds an already validated partial msg to the container, checks for quorum and returns true (and roots) if quorum exists
func (b *BaseRunner) basePartialSigMsgProcessing(
	signedMsg *spectypes.SignedPartialSignatureMessage,
	container *specssv.PartialSigContainer,
) (bool, [][32]byte, error) {
	roots := make([][32]byte, 0)
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
	decidedRunningInstance := decided && b.State.RunningInstance != nil && decidedMsg.Message.Height == b.State.RunningInstance.GetHeight()

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

func (b *BaseRunner) decide(logger *zap.Logger, runner Runner, input *spectypes.ConsensusData) error {
	byts, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode ConsensusData")
	}

	if err := runner.GetValCheckF()(byts); err != nil {
		return errors.Wrap(err, "input data invalid")

	}

	if err := runner.GetBaseRunner().QBFTController.StartNewInstance(logger,
		specqbft.Height(input.Duty.Slot),
		byts,
	); err != nil {
		return errors.Wrap(err, "could not start new QBFT instance")
	}
	newInstance := runner.GetBaseRunner().QBFTController.InstanceForHeight(logger, runner.GetBaseRunner().QBFTController.Height)
	if newInstance == nil {
		return errors.New("could not find newly created QBFT instance")
	}

	runner.GetBaseRunner().State.RunningInstance = newInstance

	b.registerTimeoutHandler(logger, newInstance, runner.GetBaseRunner().QBFTController.Height)

	return nil
}

// hasRunningDuty returns true if a new duty didn't start or an existing duty marked as finished
func (b *BaseRunner) hasRunningDuty() bool {
	b.mtx.RLock() // reads b.State
	defer b.mtx.RUnlock()

	if b.State == nil {
		return false
	}
	return !b.State.Finished
}

func (b *BaseRunner) ShouldProcessDuty(duty *spectypes.Duty) error {
	if b.QBFTController.Height >= specqbft.Height(duty.Slot) && b.QBFTController.Height != 0 {
		return errors.Errorf("duty for slot %d already passed. Current height is %d", duty.Slot,
			b.QBFTController.Height)
	}
	return nil
}

func (b *BaseRunner) ShouldProcessNonBeaconDuty(duty *spectypes.Duty) error {
	// assume StartingDuty is not nil if state is not nil
	if b.State != nil && b.State.StartingDuty.Slot >= duty.Slot {
		return errors.Errorf("duty for slot %d already passed. Current slot is %d", duty.Slot,
			b.State.StartingDuty.Slot)
	}
	return nil
}

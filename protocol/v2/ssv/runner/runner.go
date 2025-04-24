package runner

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
)

type Getters interface {
	GetBaseRunner() *BaseRunner
	GetBeaconNode() beacon.BeaconNode
	GetValCheckF() specqbft.ProposedValueCheckF
	GetSigner() ekm.BeaconSigner
	GetOperatorSigner() ssvtypes.OperatorSigner
	GetNetwork() specqbft.Network
}

type Runner interface {
	spectypes.Encoder
	spectypes.Root
	Getters

	// StartNewDuty starts a new duty for the runner, returns error if can't
	StartNewDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty, quorum uint64) error
	// HasRunningDuty returns true if it has a running duty
	HasRunningDuty() bool
	// ProcessPreConsensus processes all pre-consensus msgs, returns error if can't process
	ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error
	// ProcessConsensus processes all consensus msgs, returns error if can't process
	ProcessConsensus(ctx context.Context, logger *zap.Logger, msg *spectypes.SignedSSVMessage) error
	// ProcessPostConsensus processes all post-consensus msgs, returns error if can't process
	ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error
	// expectedPreConsensusRootsAndDomain an INTERNAL function, returns the expected pre-consensus roots to sign
	expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error)
	// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
	expectedPostConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error)
	// executeDuty an INTERNAL function, executes a duty.
	executeDuty(ctx context.Context, logger *zap.Logger, duty spectypes.Duty) error
}

type DoppelgangerProvider interface {
	CanSign(validatorIndex phase0.ValidatorIndex) bool
	ReportQuorum(validatorIndex phase0.ValidatorIndex)
}

var _ Runner = new(CommitteeRunner)

type BaseRunner struct {
	mtx            sync.RWMutex
	State          *State
	Share          map[phase0.ValidatorIndex]*spectypes.Share
	QBFTController *controller.Controller
	DomainType     spectypes.DomainType
	BeaconNetwork  spectypes.BeaconNetwork
	RunnerRoleType spectypes.RunnerRole
	ssvtypes.OperatorSigner

	// implementation vars
	TimeoutF TimeoutF `json:"-"`

	// highestDecidedSlot holds the highest decided duty slot and gets updated after each decided is reached
	highestDecidedSlot phase0.Slot
}

func (b *BaseRunner) Encode() ([]byte, error) {
	return json.Marshal(b)
}

func (b *BaseRunner) Decode(data []byte) error {
	return json.Unmarshal(data, &b)
}

func (b *BaseRunner) MarshalJSON() ([]byte, error) {
	type BaseRunnerAlias struct {
		State              *State
		Share              map[phase0.ValidatorIndex]*spectypes.Share
		QBFTController     *controller.Controller
		BeaconNetwork      spectypes.BeaconNetwork
		RunnerRoleType     spectypes.RunnerRole
		highestDecidedSlot phase0.Slot
	}

	// Create object and marshal
	alias := &BaseRunnerAlias{
		State:              b.State,
		Share:              b.Share,
		QBFTController:     b.QBFTController,
		BeaconNetwork:      b.BeaconNetwork,
		RunnerRoleType:     b.RunnerRoleType,
		highestDecidedSlot: b.highestDecidedSlot,
	}

	byts, err := json.Marshal(alias)

	return byts, err
}

// SetHighestDecidedSlot set highestDecidedSlot for base runner
func (b *BaseRunner) SetHighestDecidedSlot(slot phase0.Slot) {
	b.highestDecidedSlot = slot
}

// setupForNewDuty is sets the runner for a new duty
func (b *BaseRunner) baseSetupForNewDuty(duty spectypes.Duty, quorum uint64) {
	// start new state
	// start new state
	// TODO nicer way to get quorum
	state := NewRunnerState(quorum, duty)

	// TODO: potentially incomplete locking of b.State. runner.Execute(duty) has access to
	// b.State but currently does not write to it
	b.mtx.Lock() // writes to b.State
	b.State = state
	b.mtx.Unlock()
}

func NewBaseRunner(
	state *State,
	share map[phase0.ValidatorIndex]*spectypes.Share,
	controller *controller.Controller,
	domainType spectypes.DomainType,
	beaconNetwork spectypes.BeaconNetwork,
	runnerRoleType spectypes.RunnerRole,
	highestDecidedSlot phase0.Slot,
) *BaseRunner {
	return &BaseRunner{
		State:              state,
		Share:              share,
		QBFTController:     controller,
		BeaconNetwork:      beaconNetwork,
		DomainType:         domainType,
		RunnerRoleType:     runnerRoleType,
		highestDecidedSlot: highestDecidedSlot,
	}
}

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty spectypes.Duty, quorum uint64) error {
	if err := b.ShouldProcessDuty(duty); err != nil {
		return errors.Wrap(err, "can't start duty")
	}

	b.baseSetupForNewDuty(duty, quorum)

	return runner.executeDuty(ctx, logger, duty)
}

// baseStartNewBeaconDuty is a base func that all runner implementation can call to start a non-beacon duty
func (b *BaseRunner) baseStartNewNonBeaconDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty *spectypes.ValidatorDuty, quorum uint64) error {
	if err := b.ShouldProcessNonBeaconDuty(duty); err != nil {
		return errors.Wrap(err, "can't start non-beacon duty")
	}
	b.baseSetupForNewDuty(duty, quorum)
	return runner.executeDuty(ctx, logger, duty)
}

// basePreConsensusMsgProcessing is a base func that all runner implementation can call for processing a pre-consensus msg
func (b *BaseRunner) basePreConsensusMsgProcessing(runner Runner, signedMsg *spectypes.PartialSignatureMessages) (bool, [][32]byte, error) {
	if err := b.ValidatePreConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid pre-consensus message")
	}

	hasQuorum, roots := b.basePartialSigMsgProcessing(signedMsg, b.State.PreConsensusContainer)
	return hasQuorum, roots, nil
}

// baseConsensusMsgProcessing is a base func that all runner implementation can call for processing a consensus msg
func (b *BaseRunner) baseConsensusMsgProcessing(ctx context.Context, logger *zap.Logger, runner Runner, msg *spectypes.SignedSSVMessage, decidedValue spectypes.Encoder) (bool, spectypes.Encoder, error) {
	prevDecided := false
	if b.hasRunningDuty() && b.State != nil && b.State.RunningInstance != nil {
		prevDecided, _ = b.State.RunningInstance.IsDecided()
	}
	if prevDecided {
		return true, nil, errors.New("not processing consensus message since consensus has already finished")
	}

	decidedMsg, err := b.QBFTController.ProcessMsg(ctx, logger, msg)
	if err != nil {
		return false, nil, err
	}

	// we allow all consensus msgs to be processed, once the process finishes we check if there is an actual running duty
	// do not return error if no running duty
	if !b.hasRunningDuty() {
		logger.Debug("no running duty")
		return false, nil, nil
	}

	if decideCorrectly, err := b.didDecideCorrectly(prevDecided, decidedMsg); !decideCorrectly {
		return false, nil, err
	}

	if err := decidedValue.Decode(decidedMsg.FullData); err != nil {
		return true, nil, errors.Wrap(err, "failed to parse decided value to ValidatorConsensusData")
	}

	if err := b.validateDecidedConsensusData(runner, decidedValue); err != nil {
		return true, nil, errors.Wrap(err, "decided ValidatorConsensusData invalid")
	}

	runner.GetBaseRunner().State.DecidedValue, err = decidedValue.Encode()
	if err != nil {
		return true, nil, errors.Wrap(err, "could not encode decided value")
	}

	// update the highest decided slot
	b.highestDecidedSlot = b.State.StartingDuty.DutySlot()

	return true, decidedValue, nil
}

// basePostConsensusMsgProcessing is a base func that all runner implementation can call for processing a post-consensus msg
func (b *BaseRunner) basePostConsensusMsgProcessing(logger *zap.Logger, runner Runner, signedMsg *spectypes.PartialSignatureMessages) (bool, [][32]byte, error) {
	if err := b.ValidatePostConsensusMsg(runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid post-consensus message")
	}

	hasQuorum, roots := b.basePartialSigMsgProcessing(signedMsg, b.State.PostConsensusContainer)
	return hasQuorum, roots, nil
}

// basePartialSigMsgProcessing adds a validated (without signature verification) validated partial msg to the container, checks for quorum and returns true (and roots) if quorum exists
func (b *BaseRunner) basePartialSigMsgProcessing(
	signedMsg *spectypes.PartialSignatureMessages,
	container *ssv.PartialSigContainer,
) (bool, [][32]byte) {
	roots := make([][32]byte, 0)
	anyQuorum := false

	for _, msg := range signedMsg.Messages {
		prevQuorum := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		// Check if it has two signatures for the same signer
		if container.HasSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			b.resolveDuplicateSignature(container, msg)
		} else {
			container.AddSignature(msg)
		}

		hasQuorum := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		if hasQuorum && !prevQuorum {
			// Notify about first quorum only
			roots = append(roots, msg.SigningRoot)
			anyQuorum = true
		}
	}

	return anyQuorum, roots
}

// didDecideCorrectly returns true if the expected consensus instance decided correctly
func (b *BaseRunner) didDecideCorrectly(prevDecided bool, signedMessage *spectypes.SignedSSVMessage) (bool, error) {
	if signedMessage == nil {
		return false, nil
	}

	if signedMessage.SSVMessage == nil {
		return false, errors.New("ssv message is nil")
	}

	decidedMessage, err := specqbft.DecodeMessage(signedMessage.SSVMessage.Data)
	if err != nil {
		return false, err
	}

	if decidedMessage == nil {
		return false, nil
	}

	if b.State.RunningInstance == nil {
		return false, errors.New("decided wrong instance")
	}

	if decidedMessage.Height != b.State.RunningInstance.GetHeight() {
		return false, errors.New("decided wrong instance")
	}

	// verify we decided running instance only, if not we do not proceed
	if prevDecided {
		return false, nil
	}

	return true, nil
}

func (b *BaseRunner) decide(ctx context.Context, logger *zap.Logger, runner Runner, slot phase0.Slot, input spectypes.Encoder) error {
	byts, err := input.Encode()
	if err != nil {
		return errors.Wrap(err, "could not encode input data for consensus")
	}

	if err := runner.GetValCheckF()(byts); err != nil {
		return errors.Wrap(err, "input data invalid")
	}

	if err := runner.GetBaseRunner().QBFTController.StartNewInstance(
		ctx,
		logger,
		specqbft.Height(slot),
		byts,
	); err != nil {
		return errors.Wrap(err, "could not start new QBFT instance")
	}
	newInstance := runner.GetBaseRunner().QBFTController.StoredInstances.FindInstance(runner.GetBaseRunner().QBFTController.Height)
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

func (b *BaseRunner) ShouldProcessDuty(duty spectypes.Duty) error {
	if b.QBFTController.Height >= specqbft.Height(duty.DutySlot()) && b.QBFTController.Height != 0 {
		return errors.Errorf("duty for slot %d already passed. Current height is %d", duty.DutySlot(),
			b.QBFTController.Height)
	}
	return nil
}

func (b *BaseRunner) ShouldProcessNonBeaconDuty(duty spectypes.Duty) error {
	// assume StartingDuty is not nil if state is not nil
	if b.State != nil && b.State.StartingDuty.DutySlot() >= duty.DutySlot() {
		return errors.Errorf("duty for slot %d already passed. Current slot is %d", duty.DutySlot(),
			b.State.StartingDuty.DutySlot())
	}
	return nil
}

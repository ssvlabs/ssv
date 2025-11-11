package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"

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
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type Getters interface {
	HasRunningQBFTInstance() bool
	HasAcceptedProposalForCurrentRound() bool
	GetShares() map[phase0.ValidatorIndex]*spectypes.Share
	GetRole() spectypes.RunnerRole
	GetLastHeight() specqbft.Height
	GetLastRound() specqbft.Round
	GetStateRoot() ([32]byte, error)
	GetBeaconNode() beacon.BeaconNode
	GetSigner() ekm.BeaconSigner
	GetOperatorSigner() ssvtypes.OperatorSigner
	GetNetwork() specqbft.Network
	GetNetworkConfig() *networkconfig.Network
}

type Setters interface {
	SetTimeoutFunc(TimeoutF)
}

type Runner interface {
	spectypes.Encoder
	spectypes.Root

	Getters
	Setters

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
	// OnTimeoutQBFT processes timeout event that can arrive during QBFT consensus phase
	OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error

	// expectedPreConsensusRootsAndDomain an INTERNAL function, returns the expected pre-consensus roots to sign
	expectedPreConsensusRootsAndDomain() ([]ssz.HashRoot, phase0.DomainType, error)
	// expectedPostConsensusRootsAndDomain an INTERNAL function, returns the expected post-consensus roots to sign
	expectedPostConsensusRootsAndDomain(ctx context.Context) ([]ssz.HashRoot, phase0.DomainType, error)
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
	NetworkConfig  *networkconfig.Network
	RunnerRoleType spectypes.RunnerRole
	ssvtypes.OperatorSigner

	// implementation vars
	TimeoutF TimeoutF `json:"-"`

	// highestDecidedSlot holds the highest decided duty slot and gets updated after each decided is reached
	highestDecidedSlot phase0.Slot
}

func (b *BaseRunner) HasRunningQBFTInstance() bool {
	var runningInstance *instance.Instance
	if b.hasRunningDuty() {
		runningInstance = b.State.RunningInstance
		if runningInstance != nil {
			decided, _ := runningInstance.IsDecided()
			return !decided
		}
	}
	return false
}

func (b *BaseRunner) HasAcceptedProposalForCurrentRound() bool {
	var runningInstance *instance.Instance
	if b.hasRunningDuty() {
		runningInstance = b.State.RunningInstance
		if runningInstance != nil {
			return runningInstance.State.ProposalAcceptedForCurrentRound != nil
		}
	}
	return false
}

func (b *BaseRunner) GetShares() map[phase0.ValidatorIndex]*spectypes.Share {
	return b.Share
}

func (b *BaseRunner) GetRole() spectypes.RunnerRole {
	return b.RunnerRoleType
}

func (b *BaseRunner) GetLastHeight() specqbft.Height {
	if ctrl := b.QBFTController; ctrl != nil {
		return ctrl.Height
	}
	return specqbft.Height(0)
}

func (b *BaseRunner) GetLastRound() specqbft.Round {
	if b.hasRunningDuty() {
		inst := b.State.RunningInstance
		if inst != nil {
			return inst.State.Round
		}
	}
	return specqbft.Round(1)
}

func (b *BaseRunner) GetStateRoot() ([32]byte, error) {
	return b.State.GetRoot()
}

func (b *BaseRunner) SetTimeoutFunc(fn TimeoutF) {
	b.TimeoutF = fn
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
		BeaconConfig       *networkconfig.Beacon
		RunnerRoleType     spectypes.RunnerRole
		highestDecidedSlot phase0.Slot
	}

	// Create object and marshal
	alias := &BaseRunnerAlias{
		State:              b.State,
		Share:              b.Share,
		QBFTController:     b.QBFTController,
		BeaconConfig:       b.NetworkConfig.Beacon,
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

// baseSetupForNewDuty is sets the runner for a new duty
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

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty spectypes.Duty, quorum uint64) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "base_runner.start_duty"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(duty.RunnerRole()),
			observability.BeaconSlotAttribute(duty.DutySlot())))
	defer span.End()

	if err := b.ShouldProcessDuty(duty); err != nil {
		return tracedErrorf(span, "can't start duty: %w", err)
	}

	b.baseSetupForNewDuty(duty, quorum)

	if err := runner.executeDuty(ctx, logger, duty); err != nil {
		return tracedErrorf(span, "failed to execute duty: %w", err)
	}
	span.SetStatus(codes.Ok, "")
	return nil
}

// baseStartNewNonBeaconDuty is a base func that all runner implementation can call to start a non-beacon duty
func (b *BaseRunner) baseStartNewNonBeaconDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty *spectypes.ValidatorDuty, quorum uint64) error {
	if err := b.ShouldProcessNonBeaconDuty(duty); err != nil {
		return errors.Wrap(err, "can't start non-beacon duty")
	}
	b.baseSetupForNewDuty(duty, quorum)
	return runner.executeDuty(ctx, logger, duty)
}

// basePreConsensusMsgProcessing is a base func that all runner implementation can call for processing a pre-consensus msg
func (b *BaseRunner) basePreConsensusMsgProcessing(
	ctx context.Context,
	runner Runner,
	signedMsg *spectypes.PartialSignatureMessages,
) (bool, [][32]byte, error) {
	if err := b.ValidatePreConsensusMsg(ctx, runner, signedMsg); err != nil {
		return false, nil, errors.Wrap(err, "invalid pre-consensus message")
	}

	hasQuorum, roots := b.basePartialSigMsgProcessing(signedMsg, b.State.PreConsensusContainer)
	return hasQuorum, roots, nil
}

// baseConsensusMsgProcessing is a base func that all runner implementation can call for processing a consensus msg
func (b *BaseRunner) baseConsensusMsgProcessing(ctx context.Context, logger *zap.Logger, valueCheckFn specqbft.ProposedValueCheckF, msg *spectypes.SignedSSVMessage, decidedValue spectypes.Encoder) (bool, spectypes.Encoder, error) {
	prevDecided := false
	if b.hasRunningDuty() && b.State != nil && b.State.RunningInstance != nil {
		prevDecided, _ = b.State.RunningInstance.IsDecided()
	}
	if prevDecided {
		return true, nil, spectypes.NewError(spectypes.SkipConsensusMessageAsConsensusHasFinishedErrorCode, "not processing consensus message since consensus has already finished")
	}

	decidedMsg, err := b.QBFTController.ProcessMsg(ctx, logger, msg)
	if controller.IsRetryable(err) {
		return false, nil, NewRetryableError(err)
	}
	if err != nil {
		return false, nil, err
	}

	if !b.hasRunningDuty() {
		logger.Debug("no running duty, applied consensus message but cannot progress further")
		return false, nil, nil
	}

	// Check if QBFT has decided.
	if decidedMsg == nil {
		return false, nil, nil
	}

	if decideCorrectly, err := b.didDecideCorrectly(prevDecided, decidedMsg); !decideCorrectly {
		return false, nil, err
	}

	if err := decidedValue.Decode(decidedMsg.FullData); err != nil {
		return true, nil, errors.Wrap(err, "failed to parse decided value to ValidatorConsensusData")
	}

	if err := b.validateDecidedConsensusData(valueCheckFn, decidedValue); err != nil {
		return true, nil, errors.Wrap(err, "decided ValidatorConsensusData invalid")
	}

	decidedValueEncoded, err := decidedValue.Encode()
	if err != nil {
		return true, nil, errors.Wrap(err, "could not encode decided value")
	}

	// update the decided and the highest decided slot
	b.State.DecidedValue = decidedValueEncoded
	b.highestDecidedSlot = b.State.CurrentDuty.DutySlot()

	return true, decidedValue, nil
}

// basePostConsensusMsgProcessing is a base func that all runner implementation can call for processing a post-consensus msg
func (b *BaseRunner) basePostConsensusMsgProcessing(ctx context.Context, runner Runner, signedMsg *spectypes.PartialSignatureMessages) (bool, [][32]byte, error) {
	if err := b.ValidatePostConsensusMsg(ctx, runner, signedMsg); err != nil {
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
	quorumReached := false

	for _, msg := range signedMsg.Messages {
		quorumReachedPreviously := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		// Check if it has two signatures for the same signer
		if container.HasSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			b.resolveDuplicateSignature(container, msg)
		} else {
			container.AddSignature(msg)
		}

		hasQuorum := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		if hasQuorum && !quorumReachedPreviously {
			// Notify about first quorum only
			roots = append(roots, msg.SigningRoot)
			quorumReached = true
		}
	}

	return quorumReached, roots
}

// didDecideCorrectly returns true if the expected consensus instance decided correctly
func (b *BaseRunner) didDecideCorrectly(prevDecided bool, signedMessage *spectypes.SignedSSVMessage) (bool, error) {
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
		return false, spectypes.NewError(spectypes.DecidedWrongInstanceErrorCode, "decided wrong instance (running instance is nil)")
	}

	if decidedMessage.Height != b.State.RunningInstance.GetHeight() {
		return false, spectypes.WrapError(spectypes.DecidedWrongInstanceErrorCode, fmt.Errorf(
			"decided wrong instance (msg_height = %d, running_instance_height = %d)",
			decidedMessage.Height,
			b.State.RunningInstance.GetHeight(),
		))
	}

	// verify we decided running instance only, if not we do not proceed
	if prevDecided {
		return false, nil
	}

	return true, nil
}

func (b *BaseRunner) decide(
	ctx context.Context,
	logger *zap.Logger,
	runner Runner,
	slot phase0.Slot,
	input spectypes.Encoder,
	valueChecker ssv.ValueChecker,
) error {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "base_runner.decide"),
		trace.WithAttributes(
			observability.RunnerRoleAttribute(runner.GetRole()),
			observability.BeaconSlotAttribute(slot)))
	defer span.End()

	byts, err := input.Encode()
	if err != nil {
		return tracedErrorf(span, "could not encode input data for consensus: %w", err)
	}

	if err := valueChecker.CheckValue(byts); err != nil {
		return tracedErrorf(span, "input data invalid: %w", err)
	}

	span.AddEvent("start new instance")
	if err := b.QBFTController.StartNewInstance(
		ctx,
		logger,
		specqbft.Height(slot),
		byts,
		valueChecker,
	); err != nil {
		return tracedErrorf(span, "could not start new QBFT instance: %w", err)
	}
	newInstance := b.QBFTController.StoredInstances.FindInstance(b.QBFTController.Height)
	if newInstance == nil {
		return tracedErrorf(span, "could not find newly created QBFT instance")
	}

	b.State.RunningInstance = newInstance

	span.AddEvent("register timeout handler")
	b.registerTimeoutHandler(ctx, logger, newInstance, b.QBFTController.Height)

	span.SetStatus(codes.Ok, "")
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
		return spectypes.NewError(
			spectypes.DutyAlreadyPassedErrorCode,
			fmt.Sprintf("duty for slot %d already passed. Current height is %d", duty.DutySlot(), b.QBFTController.Height),
		)
	}
	return nil
}

func (b *BaseRunner) ShouldProcessNonBeaconDuty(duty spectypes.Duty) error {
	// assume CurrentDuty is not nil if state is not nil
	if b.State != nil && b.State.CurrentDuty.DutySlot() >= duty.DutySlot() {
		return spectypes.NewError(
			spectypes.DutyAlreadyPassedErrorCode,
			fmt.Sprintf("duty for slot %d already passed. Current slot is %d", duty.DutySlot(), b.State.CurrentDuty.DutySlot()),
		)
	}
	return nil
}

func (b *BaseRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, msg ssvtypes.EventMsg) error {
	return b.QBFTController.OnTimeout(ctx, logger, msg)
}

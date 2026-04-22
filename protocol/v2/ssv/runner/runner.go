package runner

import (
	"context"
	"encoding/json"
	"fmt"
	"maps"
	"slices"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	"github.com/pkg/errors"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/controller"
	"github.com/ssvlabs/ssv/protocol/v2/qbft/instance"
	"github.com/ssvlabs/ssv/protocol/v2/ssv"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
)

type Getters interface {
	HasRunningDuty() bool
	HasRunningQBFTInstance() bool
	HasAcceptedProposalForCurrentRound() bool
	GetShares() map[phase0.ValidatorIndex]*spectypes.Share
	GetShare() *spectypes.Share
	GetRole() spectypes.RunnerRole
	GetLastHeight() specqbft.Height
	GetLastRound() specqbft.Round
	GetStateRoot() ([32]byte, error)
	GetSigner() ekm.BeaconSigner
	GetOperatorSigner() ssvtypes.OperatorSigner
	GetNetwork() specqbft.Network
	GetBeaconNode() beacon.BeaconNode
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
	// ProcessPreConsensus processes all pre-consensus msgs, returns error if can't process
	ProcessPreConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error
	// ProcessConsensus processes all consensus msgs, returns error if can't process
	ProcessConsensus(ctx context.Context, logger *zap.Logger, msg *spectypes.SignedSSVMessage) error
	// ProcessPostConsensus processes all post-consensus msgs, returns error if can't process
	ProcessPostConsensus(ctx context.Context, logger *zap.Logger, signedMsg *spectypes.PartialSignatureMessages) error
	// OnTimeoutQBFT processes timeout event that can arrive during QBFT consensus phase
	OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, timeoutData *ssvtypes.TimeoutData) error

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

// BaseRunnerOptions holds fields shared across all runner constructors.
// Each role-specific options struct embeds it.
type BaseRunnerOptions struct {
	NetworkConfig  *networkconfig.Network
	Share          map[phase0.ValidatorIndex]*spectypes.Share
	Beacon         beacon.BeaconNode
	Network        specqbft.Network
	Signer         ekm.BeaconSigner
	OperatorSigner ssvtypes.OperatorSigner
}

type BaseRunner struct {
	// State stores the current runner state, this state corresponds to 1 particular duty the runner is
	// currently busy with at the moment. The BaseRunner is not responsible for synchronizing any updates
	// State might need to record - the caller is responsible to ensure the updates/reads (these can happen
	// whenever runner's method is called to process a p2p message, or an event) are applied sequentially,
	// plus the caller is also responsible for ensuring there is no race with moving on to the next duty
	// (the baseSetupForNewDuty call).
	// Note, the current implementation achieves concurrent safety by making sure every State read/update
	// is done by the same go-routine, handling all the messages in queue.SSVMessage (p2p messages and events)
	// sequentially.
	State *State

	Share          map[phase0.ValidatorIndex]*spectypes.Share
	QBFTController *controller.Controller
	NetworkConfig  *networkconfig.Network
	RunnerRoleType spectypes.RunnerRole
	ssvtypes.OperatorSigner

	TimeoutF    TimeoutF           `json:"-"`
	timerCancel context.CancelFunc `json:"-"`

	// highestDecidedSlot holds the highest decided duty slot and gets updated after each decided is reached
	highestDecidedSlot phase0.Slot
}

// HasRunningDuty returns whether this runner has a running (unfinished) duty assigned to it.
// Deprecated: this func is preserved for compatibility reasons with legacy Validator-Runner code, avoid
// using it since runner shouldn't expose its internal state to the outside world.
func (b *BaseRunner) HasRunningDuty() bool {
	return b.hasDutyRunning()
}

func (b *BaseRunner) HasStartedQBFTInstance() bool {
	return b.hasDutyAssigned() && b.State.RunningInstance != nil
}

func (b *BaseRunner) HasRunningQBFTInstance() bool {
	// Note: RunningInstance.State cannot be nil for existing RunningInstance by construction.
	return b.hasDutyRunning() && b.State.RunningInstance != nil && !b.State.RunningInstance.State.Decided
}

func (b *BaseRunner) HasAcceptedProposalForCurrentRound() bool {
	var runningInstance *instance.Instance
	if b.hasDutyRunning() {
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

// GetShare returns the runner's share. Intended for single-share runners
// (all roles except Committee), whose constructors enforce len(Share) == 1.
// CommitteeRunner owns multiple shares and must iterate b.Share directly —
// calling GetShare on it returns an arbitrary entry (Go map iteration order
// is randomized) and is almost certainly a bug.
func (b *BaseRunner) GetShare() *spectypes.Share {
	for _, share := range b.Share {
		return share
	}
	return nil
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
	if b.hasDutyRunning() {
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

// Decode unmarshals persisted runner state into the receiver.
//
// Note: decoded runners are intentionally partial; runtime dependencies (e.g. `NetworkConfig`, `TimeoutF`, and
// runner-specific value checkers) must be rehydrated by the caller after decode.
func (b *BaseRunner) Decode(data []byte) error {
	if b == nil {
		return fmt.Errorf("nil BaseRunner")
	}
	// Unmarshal into the receiver, not into a copy of the pointer.
	// Unmarshalling into `&b` would only update the local pointer variable.
	return json.Unmarshal(data, b)
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

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty spectypes.Duty, quorum uint64) error {
	if err := b.ShouldProcessDuty(duty); err != nil {
		return fmt.Errorf("can't start duty: %w", err)
	}

	b.State = NewRunnerState(quorum, duty)

	if err := runner.executeDuty(ctx, logger, duty); err != nil {
		return fmt.Errorf("failed to execute duty: %w", err)
	}
	return nil
}

// baseStartNewNonBeaconDuty is a base func that all runner implementation can call to start a non-beacon duty
func (b *BaseRunner) baseStartNewNonBeaconDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty *spectypes.ValidatorDuty, quorum uint64) error {
	if err := b.ShouldProcessNonBeaconDuty(duty); err != nil {
		return fmt.Errorf("can't start non-beacon duty: %w", err)
	}
	b.State = NewRunnerState(quorum, duty)
	return runner.executeDuty(ctx, logger, duty)
}

// signAndBroadcastPartialSigMsgs encodes msgs into an SSVMessage, signs it with opSigner,
// wraps it in a SignedSSVMessage, and broadcasts via network. Shared by runners whose
// executeDuty tails are otherwise identical.
func (b *BaseRunner) signAndBroadcastPartialSigMsgs(
	ctx context.Context,
	network specqbft.Network,
	opSigner ssvtypes.OperatorSigner,
	validatorPubKey []byte,
	msgs *spectypes.PartialSignatureMessages,
) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	msgID := spectypes.NewMsgID(b.NetworkConfig.DomainType, validatorPubKey, b.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return fmt.Errorf("could not encode partial signature messages: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	span.AddEvent("signing SSV message")
	sig, err := opSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign SSVMessage: %w", err)
	}

	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{opSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	span.AddEvent("broadcasting signed SSV message")
	if err := network.Broadcast(msgID, signed); err != nil {
		return fmt.Errorf("could not broadcast signed SSV message: %w", err)
	}

	return nil
}

// basePreConsensusMsgProcessing is a base func that all runner implementation can call for processing a pre-consensus msg
func (b *BaseRunner) basePreConsensusMsgProcessing(ctx context.Context, logger *zap.Logger, runner Runner, signedMsg *spectypes.PartialSignatureMessages) (bool, [][32]byte, error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	if err := b.ValidatePreConsensusMsg(ctx, runner, signedMsg); err != nil {
		return false, nil, fmt.Errorf("invalid pre-consensus message: %w", err)
	}

	vIndices := make([]uint64, 0, len(signedMsg.Messages))
	for _, msg := range signedMsg.Messages {
		vIndices = append(vIndices, uint64(msg.ValidatorIndex))
	}
	const gotPreConsensusMsgEvent = "📬 got pre-consensus message"
	logger.Debug(
		gotPreConsensusMsgEvent,
		zap.Uint64("signer", ssvtypes.PartialSigMsgSigner(signedMsg)),
		zap.Uint64s("validators", vIndices),
	)
	span.AddEvent(gotPreConsensusMsgEvent)

	hasQuorum, quorumRoots := b.basePartialSigMsgProcessing(signedMsg, b.State.PreConsensusContainer)

	if hasQuorum {
		const gotPreConsensusQuorumEvent = "🎯 got pre-consensus quorum"
		logger.Debug(gotPreConsensusQuorumEvent, fields.QuorumRoots(quorumRoots))
		span.AddEvent(gotPreConsensusQuorumEvent)
	}

	return hasQuorum, slices.Collect(maps.Keys(quorumRoots)), nil
}

// baseConsensusMsgProcessing is a base func that all runner implementation can call for processing a consensus msg
func (b *BaseRunner) baseConsensusMsgProcessing(ctx context.Context, logger *zap.Logger, valueCheckFn specqbft.ProposedValueCheckF, msg *spectypes.SignedSSVMessage, decidedValue spectypes.Encoder) (bool, spectypes.Encoder, error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	prevDecided := false
	if b.hasDutyRunning() && b.HasStartedQBFTInstance() {
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

	if !b.hasDutyRunning() {
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
		return true, nil, fmt.Errorf("failed to parse decided value to ValidatorConsensusData: %w", err)
	}

	if err := b.validateDecidedConsensusData(valueCheckFn, decidedValue); err != nil {
		return true, nil, fmt.Errorf("decided ValidatorConsensusData invalid: %w", err)
	}

	decidedValueEncoded, err := decidedValue.Encode()
	if err != nil {
		return true, nil, fmt.Errorf("could not encode decided value: %w", err)
	}

	const qbftInstanceIsDecidedEvent = "QBFT instance is decided"
	logger.Debug(qbftInstanceIsDecidedEvent)
	span.AddEvent(qbftInstanceIsDecidedEvent)

	// update the decided and the highest decided slot
	b.State.DecidedValue = decidedValueEncoded
	currentDutySlot, err := b.currentDutySlot()
	if err != nil {
		return true, nil, fmt.Errorf("current duty slot: %w", err)
	}
	b.highestDecidedSlot = currentDutySlot

	return true, decidedValue, nil
}

// basePostConsensusMsgProcessing is a base func that all runner implementation can call for processing a post-consensus msg
func (b *BaseRunner) basePostConsensusMsgProcessing(
	ctx context.Context,
	logger *zap.Logger,
	runner Runner,
	signedMsg *spectypes.PartialSignatureMessages,
) (ok bool, roots [][32]byte, err error) {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	if err := b.ValidatePostConsensusMsg(ctx, runner, signedMsg); err != nil {
		return false, nil, fmt.Errorf("invalid post-consensus message: %w", err)
	}

	vIndices := make([]uint64, 0, len(signedMsg.Messages))
	for _, msg := range signedMsg.Messages {
		vIndices = append(vIndices, uint64(msg.ValidatorIndex))
	}
	const gotPostConsensusMsgEvent = "📬 got post-consensus message"
	logger.Debug(
		gotPostConsensusMsgEvent,
		zap.Uint64("signer", ssvtypes.PartialSigMsgSigner(signedMsg)),
		zap.Uint64s("validators", vIndices),
	)
	span.AddEvent(gotPostConsensusMsgEvent)

	hasQuorum, quorumRoots := b.basePartialSigMsgProcessing(signedMsg, b.State.PostConsensusContainer)

	if hasQuorum {
		const gotPostConsensusQuorumEvent = "🎯 got post-consensus quorum"
		logger.Debug(gotPostConsensusQuorumEvent, fields.QuorumRoots(quorumRoots))
		span.AddEvent(gotPostConsensusQuorumEvent)
	}

	return hasQuorum, slices.Collect(maps.Keys(quorumRoots)), nil
}

// basePartialSigMsgProcessing adds a validated (without signature verification) validated partial msg to the container, checks for quorum and returns true (and roots) if quorum exists
func (b *BaseRunner) basePartialSigMsgProcessing(
	signedMsg *spectypes.PartialSignatureMessages,
	container *ssv.PartialSigContainer,
) (gotAnyQuorum bool, roots map[[32]byte]map[phase0.ValidatorIndex][]spectypes.OperatorID) {
	roots = make(map[[32]byte]map[phase0.ValidatorIndex][]spectypes.OperatorID)

	for _, msg := range signedMsg.Messages {
		quorumReachedPreviously, _ := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)

		// Check if it has two signatures for the same signer
		if container.HasSignature(msg.ValidatorIndex, msg.Signer, msg.SigningRoot) {
			b.resolveDuplicateSignature(container, msg)
		} else {
			container.AddSignature(msg)
		}

		// We are interested in any quorum that occurs for the very first time (for this root).
		hasQuorum, quorumSigners := container.HasQuorum(msg.ValidatorIndex, msg.SigningRoot)
		if !quorumReachedPreviously && hasQuorum {
			if roots[msg.SigningRoot] == nil {
				roots[msg.SigningRoot] = make(map[phase0.ValidatorIndex][]spectypes.OperatorID)
			}
			roots[msg.SigningRoot][msg.ValidatorIndex] = quorumSigners
			gotAnyQuorum = true
		}
	}

	return gotAnyQuorum, roots
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

	if !b.HasStartedQBFTInstance() {
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
	slot phase0.Slot,
	input spectypes.Encoder,
	valueChecker ssv.ValueChecker,
) error {
	byts, err := input.Encode()
	if err != nil {
		return fmt.Errorf("could not encode input data for consensus: %w", err)
	}

	if err := valueChecker.CheckValue(byts); err != nil {
		return fmt.Errorf("input data invalid: %w", err)
	}

	height := specqbft.Height(slot)
	timer := b.createTimer(ctx, logger, height)

	newInstance, err := b.QBFTController.StartNewInstance(
		ctx,
		logger,
		height,
		timer,
		byts,
		valueChecker,
	)
	if err != nil {
		return fmt.Errorf("could not start new QBFT instance: %w", err)
	}
	if newInstance == nil {
		return fmt.Errorf("could not start new QBFT instance: instance is nil")
	}

	b.State.RunningInstance = newInstance

	return nil
}

func (b *BaseRunner) hasDutyAssigned() bool {
	return b.State != nil
}

func (b *BaseRunner) hasDutyRunning() bool {
	return b.hasDutyAssigned() && !b.State.Finished
}

func (b *BaseRunner) hasDutyFinished() bool {
	return b.hasDutyAssigned() && b.State.Finished
}

func (b *BaseRunner) finishDuty() {
	if b.timerCancel != nil {
		b.timerCancel()
		b.timerCancel = nil
	}

	b.State.Finished = true
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
	// CurrentDuty is not nil if State is not nil by construction.
	if b.hasDutyAssigned() && b.State.CurrentDuty.DutySlot() >= duty.DutySlot() {
		return spectypes.NewError(
			spectypes.DutyAlreadyPassedErrorCode,
			fmt.Sprintf("duty for slot %d already passed. Current slot is %d", duty.DutySlot(), b.State.CurrentDuty.DutySlot()),
		)
	}
	return nil
}

func (b *BaseRunner) OnTimeoutQBFT(ctx context.Context, logger *zap.Logger, timeoutData *ssvtypes.TimeoutData) error {
	if !b.hasDutyRunning() {
		// Duties terminate eventually, timeout-event issuer is unaware of that - that's why we can end up here.
		return nil
	}

	currentDutySlot, err := b.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}

	if timeoutData.Height != specqbft.Height(currentDutySlot) {
		// Validator-Runners are re-used to process duties targeting different slots (unlike Committee-Runners that
		// are working with exactly one slot), thus for Validator-Runners timeout events can be delayed in the queue
		// until the runner has already moved on to a new duty/slot - this is why timeout-event height(== slot)
		// might be different from the actual current slot the runner is working with, and we just skip these delayed
		// events as no longer relevant (the duty those are targeting has already expired).
		return nil
	}

	return b.QBFTController.OnTimeout(ctx, logger, timeoutData)
}

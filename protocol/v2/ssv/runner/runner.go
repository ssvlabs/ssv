package runner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	ssz "github.com/ferranbt/fastssz"
	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	protocolp2p "github.com/ssvlabs/ssv/protocol/v2/p2p"
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
	GetNetwork() protocolp2p.Network
	GetBeaconNode() beacon.BeaconNode
}

type Setters interface {
	SetQBFTRoundTimerF(ssv.QBFTRoundTimerF)
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
	// OnQBFTRoundTimeout processes timeout event that can arrive during QBFT consensus phase
	OnQBFTRoundTimeout(ctx context.Context, logger *zap.Logger, timeoutData *ssvtypes.TimeoutData) error

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
	Network        protocolp2p.Network
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

	qbftRoundTimerF ssv.QBFTRoundTimerF `json:"-"`

	// dutyConcluded carries a duty's terminal outcome (success / not-required / failure) from the
	// markers to its watcher (watchDutyOutcome), which reports it exactly once. Buffered(1) so a
	// marker never blocks even if the watcher already exited. Set and sent-on only from the single
	// message-processing goroutine (the watcher reads its own captured copy), so it needs no lock.
	dutyConcluded chan dutyConclusion `json:"-"`

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
		return ctrl.LatestInstanceHeight
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
	if b.State == nil {
		return [32]byte{}, errors.New("runner state is not initialized")
	}
	return b.State.GetRoot()
}

func (b *BaseRunner) SetQBFTRoundTimerF(factory ssv.QBFTRoundTimerF) {
	b.qbftRoundTimerF = factory
}

func (b *BaseRunner) Encode() ([]byte, error) {
	return json.Marshal(b)
}

// Decode unmarshals persisted runner state into the receiver.
//
// Note: decoded runners are intentionally partial; runtime dependencies (e.g. `NetworkConfig`, the QBFT round-timeout
// func, and runner-specific value checkers) must be rehydrated by the caller after decode.
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

// marshalRunnerStateJSON encodes a runner whose persisted state is just its BaseRunner. ValCheck is a
// runtime-only dependency but is kept in the JSON as null to preserve the historical runner-state shape
// (and thus the state roots spec tests pin); runners restore it via unmarshalRunnerStateJSON.
func marshalRunnerStateJSON(b *BaseRunner) ([]byte, error) {
	return json.Marshal(&struct {
		BaseRunner *BaseRunner `json:"BaseRunner"`
		ValCheck   any         `json:"ValCheck"`
	}{BaseRunner: b})
}

// unmarshalRunnerStateJSON restores the BaseRunner written by marshalRunnerStateJSON; ValCheck is left
// nil for the caller to rehydrate.
func unmarshalRunnerStateJSON(data []byte) (*BaseRunner, error) {
	aux := &struct {
		BaseRunner *BaseRunner     `json:"BaseRunner"`
		ValCheck   json.RawMessage `json:"ValCheck"`
	}{}
	if err := json.Unmarshal(data, aux); err != nil {
		return nil, err
	}
	if aux.BaseRunner == nil {
		return nil, fmt.Errorf("missing BaseRunner")
	}
	return aux.BaseRunner, nil
}

// baseStartNewDuty is a base func that all runner implementation can call to start a duty
func (b *BaseRunner) baseStartNewDuty(ctx context.Context, logger *zap.Logger, runner Runner, duty spectypes.Duty, quorum uint64) error {
	if err := b.ShouldProcessDuty(duty); err != nil {
		return fmt.Errorf("can't start duty: %w", err)
	}

	b.State = NewRunnerState(quorum, duty)

	b.watchDutyOutcome(ctx, logger)

	if err := runner.executeDuty(ctx, logger, duty); err != nil {
		b.markDutyFailed(err)
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

	b.watchDutyOutcome(ctx, logger)

	if err := runner.executeDuty(ctx, logger, duty); err != nil {
		b.markDutyFailed(err)
		return err
	}

	return nil
}

// dutyOutcome classifies how a duty concluded. Every duty concludes with exactly one outcome,
// reported once by watchDutyOutcome and recorded as the ssv.runner.duty.outcome metric.
type dutyOutcome string

const (
	dutyOutcomeSucceeded   dutyOutcome = "succeeded"    // full successful cycle with quorum, submitted to the beacon node
	dutyOutcomeNotRequired dutyOutcome = "not_required" // completed with nothing to submit (e.g. not selected as aggregator)
	dutyOutcomeFailed      dutyOutcome = "failed"       // terminated by a non-recoverable error
	dutyOutcomeStuck       dutyOutcome = "stuck"        // not concluded before the end of the current wall-clock slot
	dutyOutcomeNoQuorum    dutyOutcome = "no_quorum"    // reached the deadline having executed, but the signature quorum never formed
)

// dutyConclusion is handed by a marker (markDutySucceeded / markDutyNotRequired / markDutyFailed) to
// the duty's watcher (watchDutyOutcome), which reports it.
type dutyConclusion struct {
	outcome dutyOutcome
	reason  error // populated for dutyOutcomeFailed
}

// watchDutyOutcome reports a duty's terminal outcome exactly once: it records the
// ssv.runner.duty.outcome metric and warns for the outcomes worth an operator's attention (failed,
// stuck, no_quorum). It knows nothing about how duties complete — the outcome is delivered by a marker
// over dutyConcluded, not by reading runner state — so it's safe alongside the single-threaded
// message loop. It MUST be started before executeDuty so a duty that concludes synchronously is still
// reported. Each duty gets its own channel: starting the next duty overwrites the field, and the
// previous duty's watcher (if still pending) reports its own duty and is reaped by its own timer.
//
// The deadline is the end of the current wall-clock slot rather than duty.Slot's end because some
// duties are stamped with a slot in the past (a voluntary-exit duty carries blockSlot+4 but
// executes at blockSlot+12); for beacon duties the two coincide. Proposer preferences are the
// opposite case — duty.Slot is a future proposal slot and the duty executes at emission, so their
// horizon extends to that slot's start instead (see below).
func (b *BaseRunner) watchDutyOutcome(ctx context.Context, logger *zap.Logger) {
	concluded := make(chan dutyConclusion, 1)
	b.dutyConcluded = concluded

	deadline := b.NetworkConfig.SlotStartTime(b.NetworkConfig.EstimatedCurrentSlot() + 1)
	// A proposer-preferences duty emits ahead of its proposal slot and legitimately keeps converging
	// across the gap — operators broadcast their partials at their own emission ticks — so its outcome
	// horizon is the proposal slot's start (the preference is moot once that slot arrives), not the
	// end of the emission slot.
	if b.RunnerRoleType == spectypes.RoleProposerPreferences && b.State != nil {
		if d := b.NetworkConfig.SlotStartTime(b.State.CurrentDuty.DutySlot()); d.After(deadline) {
			deadline = d
		}
	}

	// A PTC attestation (SIP #94 §3) has no consensus phase, and every other way it can end already
	// marks the duty — abstain → not_required, beacon-node/sign/broadcast failure → failed. So
	// reaching the deadline unmarked means exactly one thing: the honest-convergence quorum never
	// formed. Report that as its own outcome so §3 convergence health is gaugeable, rather than
	// hiding inside the generic "likely stuck" that every role shares.
	deadlineOutcome := dutyOutcomeStuck
	if b.RunnerRoleType == spectypes.RolePTCAttester {
		deadlineOutcome = dutyOutcomeNoQuorum
	}

	report := func(c dutyConclusion) {
		recordDutyOutcome(ctx, b.GetRole(), c.outcome)
		switch c.outcome {
		case dutyOutcomeFailed:
			logger.Warn("⚠️ duty failed", zap.Error(c.reason))
		case dutyOutcomeStuck:
			logger.Warn("⚠️ duty did not complete before slot end (likely stuck)")
		case dutyOutcomeNoQuorum:
			logger.Warn("⚠️ duty did not reach signature quorum before slot end (operators did not converge)")
		case dutyOutcomeSucceeded, dutyOutcomeNotRequired:
			logger.Debug("duty concluded", zap.String("outcome", string(c.outcome)))
		}
	}

	go func() {
		select {
		case c := <-concluded:
			report(c)
		case <-ctx.Done():
			return // node/validator shutting down
		case <-time.After(time.Until(deadline)):
			// Prefer a conclusion that landed right at the deadline over reporting a false miss.
			select {
			case c := <-concluded:
				report(c)
			default:
				report(dutyConclusion{outcome: deadlineOutcome})
			}
		}
	}()
}

// signAndBroadcastPartialSigMsgs encodes msgs into an SSVMessage, signs it with opSigner,
// wraps it in a SignedSSVMessage, and broadcasts via network. Shared by runners whose
// executeDuty tails are otherwise identical.
func (b *BaseRunner) signAndBroadcastPartialSigMsgs(
	ctx context.Context,
	network protocolp2p.Network,
	opSigner ssvtypes.OperatorSigner,
	validatorPubKey spectypes.ValidatorPK,
	msgs *spectypes.PartialSignatureMessages,
) error {
	// Reuse the existing span instead of generating new one to keep tracing-data lightweight.
	span := trace.SpanFromContext(ctx)

	// Use the fork-aware domain so the pubsub message validator accepts the message after the
	// Boole fork activates (post-fork it checks NextDomainType). Mirrors CommitteeRunner and
	// QBFT domain selection. Fixes #2915.
	msgID := spectypes.NewValidatorMsgID(b.NetworkConfig.DomainTypeAtSlot(msgs.Slot), validatorPubKey, b.RunnerRoleType)
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
	if err := network.BroadcastAtSlot(signed, msgs.Slot); err != nil {
		return fmt.Errorf("could not broadcast signed SSV message: %w", err)
	}

	return nil
}

// signAndBroadcastPostConsensusMsg signs a post-consensus partial-signature message as the operator and
// broadcasts it on its slot's subnet. Unlike signAndBroadcastPartialSigMsgs (pre-consensus), it keys the
// message id by the slot's fork domain and uses BroadcastAtSlot.
func (b *BaseRunner) signAndBroadcastPostConsensusMsg(
	network protocolp2p.Network,
	opSigner ssvtypes.OperatorSigner,
	validatorPubKey spectypes.ValidatorPK,
	msgs *spectypes.PartialSignatureMessages,
) error {
	domain := b.NetworkConfig.DomainTypeAtSlot(msgs.Slot)
	msgID := spectypes.NewValidatorMsgID(domain, validatorPubKey, b.RunnerRoleType)
	encodedMsg, err := msgs.Encode()
	if err != nil {
		return fmt.Errorf("could not encode post-consensus partial signature message: %w", err)
	}

	ssvMsg := &spectypes.SSVMessage{
		MsgType: spectypes.SSVPartialSignatureMsgType,
		MsgID:   msgID,
		Data:    encodedMsg,
	}

	sig, err := opSigner.SignSSVMessage(ssvMsg)
	if err != nil {
		return fmt.Errorf("could not sign post-consensus SSV message: %w", err)
	}

	signed := &spectypes.SignedSSVMessage{
		Signatures:  [][]byte{sig},
		OperatorIDs: []spectypes.OperatorID{opSigner.GetOperatorID()},
		SSVMessage:  ssvMsg,
	}

	return network.BroadcastAtSlot(signed, msgs.Slot)
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

	decidedMsg, err := b.QBFTController.ProcessMsg(ctx, logger, msg, b.qbftRoundTimerF)
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

	newInstance, err := b.QBFTController.StartNewInstance(
		ctx,
		logger,
		specqbft.Height(slot),
		byts,
		valueChecker,
		b.qbftRoundTimerF,
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
	return b.hasDutyAssigned() && !b.State.Succeeded
}

func (b *BaseRunner) hasDutySucceeded() bool {
	return b.hasDutyAssigned() && b.State.Succeeded
}

// markDutySucceeded records that the duty completed its full cycle (pre-consensus, consensus and
// post-consensus) successfully, submitting to the beacon node.
func (b *BaseRunner) markDutySucceeded() {
	// NOTE: b.State cannot be nil at this point, by construction.
	b.State.Succeeded = true
	b.concludeDuty(dutyOutcomeSucceeded, nil)
}

// markDutyNotRequired records that the duty completed correctly with nothing to submit — e.g. the
// validator turned out not to be an aggregator. Like a success it is a full, correct completion
// (State.Succeeded is set); it is reported under a distinct outcome so the two can be told apart.
func (b *BaseRunner) markDutyNotRequired() {
	// NOTE: b.State cannot be nil at this point, by construction.
	b.State.Succeeded = true
	b.concludeDuty(dutyOutcomeNotRequired, nil)
}

// markDutyFailed records that the duty terminated with a non-recoverable error. It does NOT mark the
// duty succeeded; it only reports the outcome so the watcher classifies the duty as failed rather
// than as a silent stall.
//
// A context.Canceled reason is not a duty failure: a cancellation means the duty was abandoned
// (typically node/validator shutdown), not attempted-and-failed, so "failed" stays reserved for
// genuine, attributable failures. Filtering it here — rather than at the watcher — also means no
// failure conclusion is ever produced to race ctx.Done() in watchDutyOutcome.
func (b *BaseRunner) markDutyFailed(reason error) {
	if errors.Is(reason, context.Canceled) {
		return
	}
	b.concludeDuty(dutyOutcomeFailed, reason)
}

// concludeDuty hands the duty's terminal outcome to its watcher (watchDutyOutcome), which reports it
// exactly once. A duty concludes at most once, but nil-ing dutyConcluded after the (buffered, so
// non-blocking) send keeps this idempotent: a second call — or one before watchDutyOutcome armed the
// channel — must never block the single message-processing goroutine on the buffered(1) channel.
func (b *BaseRunner) concludeDuty(outcome dutyOutcome, reason error) {
	if b.dutyConcluded != nil {
		b.dutyConcluded <- dutyConclusion{outcome: outcome, reason: reason}
		b.dutyConcluded = nil
	}
}

func (b *BaseRunner) ShouldProcessDuty(duty spectypes.Duty) error {
	if b.QBFTController.LatestInstanceHeight >= specqbft.Height(duty.DutySlot()) && b.QBFTController.LatestInstanceHeight != 0 {
		return spectypes.NewError(
			spectypes.DutyAlreadyPassedErrorCode,
			fmt.Sprintf("duty for slot %d already passed. Current height is %d", duty.DutySlot(), b.QBFTController.LatestInstanceHeight),
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

func (b *BaseRunner) OnQBFTRoundTimeout(ctx context.Context, logger *zap.Logger, timeoutData *ssvtypes.TimeoutData) error {
	if !b.hasDutyRunning() {
		// Duties terminate eventually, timeout-event issuer is unaware of that - that's why we can end up here.
		return nil
	}

	currentDutySlot, err := b.currentDutySlot()
	if err != nil {
		return fmt.Errorf("current duty slot: %w", err)
	}

	if timeoutData.Slot != currentDutySlot {
		// Validator-Runners are re-used to process duties targeting different slots (unlike Committee-Runners that
		// are working with exactly one slot), thus for Validator-Runners timeout events can be delayed in the queue
		// until the runner has already moved on to a new duty/slot - this is why timeout-event height(== slot)
		// might be different from the actual current slot the runner is working with, and we just skip these delayed
		// events as no longer relevant (the duty those are targeting has already expired).
		return nil
	}

	return b.QBFTController.OnQBFTRoundTimeout(ctx, logger, timeoutData)
}

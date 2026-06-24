package duties

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate go tool -modfile=../../tool.mod mockgen -package=duties -destination=./scheduler_mock.go -source=./scheduler.go

const (
	// blockPropagationDelay time to propagate around the nodes
	// before kicking off duties for the block's slot.
	blockPropagationDelay = 300 * time.Millisecond
	// reorgChannelBuffer allows HandleHeadEvent to emit a reorg notification without blocking on
	// downstream fanout startup or temporary consumer lag.
	reorgChannelBuffer = 1
)

// DutiesExecutor is an interface for executing duties.
type DutiesExecutor interface {
	ExecuteDuties(ctx context.Context, duties []*spectypes.ValidatorDuty, dutyDeadline time.Time)
	ExecuteCommitteeDuties(ctx context.Context, duties committeeDutiesMap, dutyDeadline time.Time)
}

// DutyExecutor is an interface for executing duty.
type DutyExecutor interface {
	ExecuteDuty(ctx context.Context, logger *zap.Logger, duty *spectypes.ValidatorDuty)
	ExecuteCommitteeDuty(ctx context.Context, logger *zap.Logger, committeeID spectypes.CommitteeID, duty spectypes.Duty)
}

type BeaconNode interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error
	SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error
	SubscribeToHeadEvents(ctx context.Context, subscriberIdentifier string, ch chan<- *eth2apiv1.HeadEvent) error
}

type ExecutionClient interface {
	HeaderByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Header, error)
}

// ValidatorProvider represents the component that controls validators via the scheduler
type ValidatorProvider interface {
	Validators() []*types.SSVShare
	SelfValidators() []*types.SSVShare
	SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
	Validator(pubKey []byte) (*types.SSVShare, bool)
}

// ValidatorController represents the component that controls validators via the scheduler
type ValidatorController interface {
	FilterIndices(afterInit bool, filter func(*types.SSVShare) bool) []phase0.ValidatorIndex
}

type SchedulerOptions struct {
	Ctx                     context.Context
	BeaconNode              BeaconNode
	ExecutionClient         ExecutionClient
	NetworkConfig           *networkconfig.Network
	ValidatorProvider       ValidatorProvider
	ValidatorController     ValidatorController
	DutyExecutor            DutyExecutor
	IndicesChgCh            chan struct{}
	ValidatorRegistrationCh <-chan RegistrationDescriptor
	ValidatorExitCh         <-chan ExitDescriptor
	SlotTickerProvider      slotticker.Provider
	DutyStore               *dutystore.Store
	P2PNetwork              network.P2PNetwork
	// ExporterMode disables handlers that make sense only for operators
	// executing duties (e.g., validator registration). When true, scheduler
	// still fetches/stores duties for all validators but does not execute them.
	ExporterMode bool
}

type Scheduler struct {
	logger *zap.Logger

	// ctx controls the lifetime of all go-routines spawned by Scheduler.
	ctx context.Context
	// backgroundTasks tracks all go-routines spawned by Scheduler for graceful shutdown.
	backgroundTasks sync.WaitGroup

	beaconNode          BeaconNode
	executionClient     ExecutionClient
	netCfg              *networkconfig.Network
	validatorProvider   ValidatorProvider
	validatorController ValidatorController
	slotTickerProvider  slotticker.Provider
	dutyExecutor        DutyExecutor

	dutyHandlers        []dutyHandler
	blockPropagateDelay time.Duration

	reorgCh      chan ReorgEvent
	indicesChgCh chan struct{}
	ticker       slotticker.SlotTicker

	// waitCond coordinates access to headSlot for different go-routines.
	waitCond *sync.Cond
	headSlot phase0.Slot

	// lastEpoch records the epoch of the last observed block.
	lastEpoch phase0.Epoch
	// lastBlockRoot records the root of the last observed block, with the intent to end up with
	// the value for the canonical root of epoch N that can be consulted during transition from
	// epoch N to epoch N+1.
	lastBlockRoot phase0.Root
	// currentDutyDependentRoot records the canonical root of epoch CURRENT-1.
	currentDutyDependentRoot phase0.Root
	// previousDutyDependentRoot records the canonical root of epoch CURRENT-2.
	previousDutyDependentRoot phase0.Root

	exporterMode bool
}

func NewScheduler(logger *zap.Logger, opts *SchedulerOptions) *Scheduler {
	dutyStore := opts.DutyStore
	if dutyStore == nil {
		dutyStore = dutystore.New()
	}

	s := &Scheduler{
		logger:              logger.Named(log.NameDutyScheduler),
		beaconNode:          opts.BeaconNode,
		executionClient:     opts.ExecutionClient,
		netCfg:              opts.NetworkConfig,
		slotTickerProvider:  opts.SlotTickerProvider,
		dutyExecutor:        opts.DutyExecutor,
		validatorProvider:   opts.ValidatorProvider,
		validatorController: opts.ValidatorController,
		indicesChgCh:        opts.IndicesChgCh,
		blockPropagateDelay: blockPropagationDelay,

		dutyHandlers: []dutyHandler{},

		ticker:   opts.SlotTickerProvider(),
		reorgCh:  make(chan ReorgEvent, reorgChannelBuffer),
		waitCond: sync.NewCond(&sync.Mutex{}),
	}

	s.exporterMode = opts.ExporterMode

	// These handlers fetch & record duties from the beacon node and are needed in both operator & exporter modes.
	// When adding a new handler here, ensure it supports both modes.
	s.dutyHandlers = append(s.dutyHandlers,
		NewAttesterHandler(dutyStore.Attester, opts.ExporterMode),
		NewProposerHandler(dutyStore.Proposer, opts.ExporterMode),
		NewSyncCommitteeHandler(dutyStore.SyncCommittee, opts.ExporterMode),
	)
	// These handlers only execute duties and are not needed in exporter mode.
	if !opts.ExporterMode {
		s.dutyHandlers = append(s.dutyHandlers,
			NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee),
			NewAggregatorCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee),
			NewValidatorRegistrationHandler(opts.ValidatorRegistrationCh),
			NewVoluntaryExitHandler(dutyStore.VoluntaryExit, opts.ValidatorExitCh),
		)
	}
	return s
}

type ReorgEvent struct {
	// CurrentDutyDependentRootChanged indicates if the current duty dependent root change has been detected.
	CurrentDutyDependentRootChanged bool
	// PreviousDutyDependentRootChanged indicates if the previous duty dependent root change has been detected.
	PreviousDutyDependentRootChanged bool
}

// Start initializes the Scheduler and begins its operation.
// Note: This function includes blocking operations, especially within the handler's HandleInitialDuties call,
// which will block until initial duties are fully handled.
func (s *Scheduler) Start(ctx context.Context) error {
	s.logger.Info("starting duty scheduler")

	s.ctx = ctx

	s.logger.Info("subscribing to head events")
	if err := s.listenToHeadEvents(s.ctx); err != nil {
		return fmt.Errorf("failed to listen to head events: %w", err)
	}

	indicesChangeFeed := NewEventFeed[struct{}]()
	reorgEventsFeed := NewEventFeed[ReorgEvent]()

	for _, handler := range s.dutyHandlers {
		// indicesChangeCh is buffered as a temporary work-around to mitigate https://github.com/ssvlabs/ssv-node-board/issues/992
		indicesChangeCh := make(chan struct{}, 1)
		indicesChangeFeed.Subscribe(indicesChangeCh)
		// reorgEventsCh is buffered as a temporary work-around to mitigate https://github.com/ssvlabs/ssv-node-board/issues/992
		reorgEventsCh := make(chan ReorgEvent, 1)
		reorgEventsFeed.Subscribe(reorgEventsCh)

		handler.Setup(ctx, SetupOptions{
			Name:                handler.Name(),
			Logger:              s.logger,
			BeaconNode:          s.beaconNode,
			ExecutionClient:     s.executionClient,
			NetworkConfig:       s.netCfg,
			ValidatorProvider:   s.validatorProvider,
			ValidatorController: s.validatorController,
			DutiesExecutor:      s,
			SlotTickerProvider:  s.slotTickerProvider,
			ReorgEventsCh:       reorgEventsCh,
			IndicesChangeCh:     indicesChangeCh,
		})

		// This call is blocking.
		handler.HandleInitialDuties(s.ctx)

		s.backgroundTasks.Add(1)
		go func() {
			defer s.backgroundTasks.Done()
			handler.HandleDuties(s.ctx)
		}()
	}

	s.backgroundTasks.Add(1)
	go func() {
		defer s.backgroundTasks.Done()
		indicesChangeFeed.FanOut(s.ctx, s.indicesChgCh)
	}()

	s.backgroundTasks.Add(1)
	go func() {
		defer s.backgroundTasks.Done()
		reorgEventsFeed.FanOut(s.ctx, s.reorgCh)
	}()

	s.backgroundTasks.Add(1)
	go func() {
		defer s.backgroundTasks.Done()
		s.SlotTicker(s.ctx)
	}()

	s.logger.Info("duty scheduler has started")

	return nil
}

func (s *Scheduler) listenToHeadEvents(ctx context.Context) error {
	headEventHandler := s.HandleHeadEvent()

	// Subscribe to head events. This allows us to go early for attestations & sync committees if a block arrives,
	// as well as re-request duties if there is a change in beacon block.
	ch := make(chan *eth2apiv1.HeadEvent, 32)
	err := s.beaconNode.SubscribeToHeadEvents(ctx, "duty_scheduler", ch)
	if err != nil {
		return fmt.Errorf("failed to subscribe to head events: %w", err)
	}

	s.backgroundTasks.Add(1)
	go func() {
		defer s.backgroundTasks.Done()
		for {
			select {
			case <-ctx.Done():
				return
			case headEvent := <-ch:
				if headEvent == nil {
					s.logger.Warn("head event was nil, skipping")
					continue
				}
				s.logger.
					With(fields.Slot(headEvent.Slot)).
					With(fields.BlockRoot(headEvent.Block)).
					Info("received head event. Processing...")

				headEventHandler(ctx, headEvent)
			}
		}
	}()

	return nil
}

// Wait blocks until the Scheduler is finished with all it's tasks, also ensuring all the
// handlers terminate before this func returns.
func (s *Scheduler) Wait() error {
	s.backgroundTasks.Wait()

	for _, handler := range s.dutyHandlers {
		handler.WaitShutdown()
	}

	return nil
}

type EventFeed[T any] struct {
	feed *event.Feed
}

func NewEventFeed[T any]() *EventFeed[T] {
	return &EventFeed[T]{
		feed: &event.Feed{},
	}
}

func (f *EventFeed[T]) Subscribe(ch chan<- T) event.Subscription {
	return f.feed.Subscribe(ch)
}

func (f *EventFeed[T]) Send(item T) {
	_ = f.feed.Send(item)
}

func (f *EventFeed[T]) FanOut(ctx context.Context, in <-chan T) {
	for {
		select {
		case <-ctx.Done():
			return
		case item, ok := <-in:
			if !ok {
				return
			}
			// Fan out the message to all subscribers.
			f.Send(item)
		}
	}
}

// SlotTicker advances "head" slot every slot-tick once we are 1/3 of slot-time past slot start
// and only if necessary. Normally Beacon node events would trigger "head" slot updates, but in
// case event is delayed or didn't arrive for some reason we still need to advance "head" slot
// for duties to keep executing normally - so SlotTicker is a secondary mechanism for that.
func (s *Scheduler) SlotTicker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ticker.Next():
			slot := s.ticker.Slot()

			delay := s.netCfg.IntervalDuration()
			finalTime := s.netCfg.SlotStartTime(slot).Add(delay)
			waitDuration := time.Until(finalTime)
			if waitDuration > 0 {
				select {
				case <-ctx.Done():
					return
				case <-time.After(waitDuration):
				}
			}

			s.advanceHeadSlot(slot)
		}
	}
}

// HandleHeadEvent handles the "head" events from the beacon node.
func (s *Scheduler) HandleHeadEvent() func(ctx context.Context, event *eth2apiv1.HeadEvent) {
	return func(ctx context.Context, event *eth2apiv1.HeadEvent) {
		currentSlot := event.Slot
		currentEpoch := s.netCfg.EstimatedEpochAtSlot(currentSlot)
		slotNumber := uint64(currentSlot)%s.netCfg.SlotsPerEpoch + 1

		buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)
		logger := s.logger.With(zap.String("epoch_slot_pos", buildStr))

		if event.Slot < s.netCfg.EstimatedCurrentSlot() {
			// No need to process outdated events here.
			return
		}
		if event.Slot > s.netCfg.EstimatedCurrentSlot() {
			// We don't handle future events to keep things simple.
			logger.Warn("got future head event from EL, most likely cause is clock-skew between SSV node and EL")
			return
		}

		// Check for reorg & fire corresponding ReorgEvent if needed.
		if s.lastEpoch != 0 {
			var zeroRoot phase0.Root

			epochTransition := currentEpoch > s.lastEpoch

			expectedCurrentDutyDependentRoot := s.currentDutyDependentRoot[:]
			expectedPreviousDutyDependentRoot := s.previousDutyDependentRoot[:]
			if epochTransition {
				// Epoch transition case:
				// - the root tracked in s.currentDutyDependentRoot now describes the previous epoch, hence becomes
				//   the expected previous dependent root
				// - we use the latest observed block-root from the previous epoch as the expected current dependent
				//   root since it's the only thing we can compare the current dependent root we got with. This might
				//   produce a spurious (false-positive) reorg in case when another event overrode the canonical root
				//   observed (and recorded as lastBlockRoot) during prior event(s) - but it works OK for us regardless.
				expectedCurrentDutyDependentRoot = s.lastBlockRoot[:]
				expectedPreviousDutyDependentRoot = s.currentDutyDependentRoot[:]
			}

			currentDutyDependentRootChanged := !bytes.Equal(expectedCurrentDutyDependentRoot, zeroRoot[:]) &&
				!bytes.Equal(expectedCurrentDutyDependentRoot, event.CurrentDutyDependentRoot[:])
			previousDutyDependentRootChanged := !bytes.Equal(expectedPreviousDutyDependentRoot, zeroRoot[:]) &&
				!bytes.Equal(expectedPreviousDutyDependentRoot, event.PreviousDutyDependentRoot[:])

			if currentDutyDependentRootChanged || previousDutyDependentRootChanged {
				logger.Debug("🔀 reorg detected: dependent root(s) changed",
					zap.Bool("epoch_transition", epochTransition),
					zap.String("expected_previous_dependent_root", fmt.Sprintf("%#x", expectedPreviousDutyDependentRoot)),
					zap.String("got_previous_dependent_root", fmt.Sprintf("%#x", event.PreviousDutyDependentRoot[:])),
					zap.String("expected_current_dependent_root", fmt.Sprintf("%#x", expectedCurrentDutyDependentRoot)),
					zap.String("got_current_dependent_root", fmt.Sprintf("%#x", event.CurrentDutyDependentRoot[:])),
				)
				s.reorgCh <- ReorgEvent{
					CurrentDutyDependentRootChanged:  currentDutyDependentRootChanged,
					PreviousDutyDependentRootChanged: previousDutyDependentRootChanged,
				}
			}
		}

		s.lastEpoch = currentEpoch
		s.lastBlockRoot = event.Block
		s.previousDutyDependentRoot = event.PreviousDutyDependentRoot
		s.currentDutyDependentRoot = event.CurrentDutyDependentRoot

		currentTime := time.Now()
		delay := s.netCfg.IntervalDuration()
		slotStartTimeWithDelay := s.netCfg.SlotStartTime(event.Slot).Add(delay)
		if currentTime.Before(slotStartTimeWithDelay) {
			logger.Debug("🏁 Head event: Block arrived before 1/3 slot", zap.Duration("time_saved", slotStartTimeWithDelay.Sub(currentTime)))

			// We give the block some time to propagate around the rest of the
			// nodes before kicking off duties for the block's slot.
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.blockPropagateDelay):
			}

			s.advanceHeadSlot(event.Slot)
		}
	}
}

// ExecuteDuties tries to execute the provided validator duties
func (s *Scheduler) ExecuteDuties(ctx context.Context, duties []*spectypes.ValidatorDuty, dutyDeadline time.Time) {
	if s.exporterMode {
		// We never execute duties in exporter mode. The handler should skip calling this method.
		// Keeping check here to detect programming mistakes.
		s.logger.Error("ExecuteDuties should not be called in exporter mode. Possible code error in duty handlers?")
		return // early return is fine, we don't need to return an error
	}

	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "scheduler.execute_duties"),
		trace.WithAttributes(observability.DutyCountAttribute(len(duties))),
	)
	defer span.End()

	for _, duty := range duties {
		role := types.RunnerRoleForValidatorDuty(duty, s.netCfg.BooleForkAtSlot(duty.Slot))
		logger := s.loggerWithDutyContext(duty)

		const eventMsg = "🔧 executing validator duty"
		logger.Debug(eventMsg)
		span.AddEvent(eventMsg)

		slotDelay := time.Since(s.netCfg.SlotStartTime(duty.Slot))

		// For roles where duty.Slot is a shared coordination point rather
		// than the execution target (see dutySlotIsExecutionSlot), slotDelay
		// against it is meaningless.
		if dutySlotIsExecutionSlot(role) && slotDelay >= 100*time.Millisecond {
			const eventMsg = "⚠️ late duty execution"
			logger.Warn(eventMsg, zap.Duration("slot_delay", slotDelay))
			span.AddEvent(eventMsg, trace.WithAttributes(
				attribute.Int64("ssv.beacon.slot_delay_ms", slotDelay.Milliseconds()),
				observability.BeaconRoleAttribute(duty.Type),
				observability.RunnerRoleAttribute(role)))
		}
		recordDutyScheduled(ctx, role, slotDelay)

		s.backgroundTasks.Add(1)
		go func() {
			defer s.backgroundTasks.Done()

			// Cannot use parent-context itself here, have to create independent instance
			// to be able to continue working in background.
			dutyCtx, cancel := context.WithDeadline(s.ctx, dutyDeadline)
			defer cancel()

			s.dutyExecutor.ExecuteDuty(dutyCtx, logger, duty)
		}()
	}

	span.SetStatus(codes.Ok, "")
}

// ExecuteCommitteeDuties tries to execute the provided committee duties
func (s *Scheduler) ExecuteCommitteeDuties(ctx context.Context, duties committeeDutiesMap, dutyDeadline time.Time) {
	if s.exporterMode {
		// We never execute duties in exporter mode. The handler should skip calling this method.
		// Keeping check here to detect programming mistakes.
		s.logger.Error("ExecuteCommitteeDuties should not be called in exporter mode. Possible code error in duty handlers?")
		return // early return is fine, we don't need to return an error
	}

	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "scheduler.execute_committee_duties"))
	defer span.End()

	for _, committee := range duties {
		duty := committee.duty
		slot := duty.DutySlot()
		role := types.RunnerRoleForDuty(duty, s.netCfg.BooleForkAtSlot(slot))

		logger := s.loggerWithCommitteeDutyContext(committee)

		const eventMsg = "🔧 executing committee duty"
		dutyEpoch := s.netCfg.EstimatedEpochAtSlot(slot)
		logger.Debug(eventMsg, fields.Duties(dutyEpoch, committee.validatorDuties(), -1, func(duty *spectypes.ValidatorDuty) spectypes.RunnerRole {
			return types.RunnerRoleForValidatorDuty(duty, s.netCfg.BooleForkAtSlot(duty.Slot))
		}))
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.RunnerRoleAttribute(role),
			observability.CommitteeIDAttribute(committee.id),
			observability.DutyCountAttribute(len(committee.validatorDuties())),
		))

		slotDelay := time.Since(s.netCfg.SlotStartTime(slot))
		if slotDelay >= 100*time.Millisecond {
			const eventMsg = "⚠️ late duty execution"
			logger.Warn(eventMsg, zap.Duration("slot_delay", slotDelay))
			span.AddEvent(eventMsg, trace.WithAttributes(
				observability.CommitteeIDAttribute(committee.id),
				attribute.Int64("ssv.beacon.slot_delay_ms", slotDelay.Milliseconds())))
		}

		recordDutyScheduled(ctx, role, slotDelay)

		s.backgroundTasks.Add(1)
		go func() {
			defer s.backgroundTasks.Done()

			// Cannot use parent-context itself here, have to create independent instance
			// to be able to continue working in background.
			dutyCtx, cancel := context.WithDeadline(s.ctx, dutyDeadline)
			defer cancel()

			if role == spectypes.RoleCommittee {
				s.waitOneThirdIntoSlotOrValidBlock(slot)
			}
			s.dutyExecutor.ExecuteCommitteeDuty(dutyCtx, logger, committee.id, duty)
		}()
	}

	span.SetStatus(codes.Ok, "")
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (s *Scheduler) loggerWithDutyContext(duty *spectypes.ValidatorDuty) *zap.Logger {
	dutyEpoch := s.netCfg.EstimatedEpochAtSlot(duty.Slot)
	role := types.RunnerRoleForValidatorDuty(duty, s.netCfg.BooleForkAtSlot(duty.Slot))
	dutyID := fields.BuildDutyID(dutyEpoch, duty.Slot, role, duty.ValidatorIndex)

	return s.logger.
		With(fields.RunnerRole(role)).
		With(fields.Slot(duty.Slot)).
		With(fields.DutyID(dutyID)).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.ValidatorIndex(duty.ValidatorIndex)).
		With(fields.EstimatedCurrentEpoch(s.netCfg.EstimatedCurrentEpoch())).
		With(fields.EstimatedCurrentSlot(s.netCfg.EstimatedCurrentSlot()))
}

// loggerWithCommitteeDutyContext returns an instance of logger with the given committee duty's information
func (s *Scheduler) loggerWithCommitteeDutyContext(committeeDuty *committeeDuty) *zap.Logger {
	slot := committeeDuty.duty.DutySlot()

	dutyEpoch := s.netCfg.EstimatedEpochAtSlot(slot)
	role := types.RunnerRoleForDuty(committeeDuty.duty, s.netCfg.BooleForkAtSlot(slot))
	committeeDutyID := fields.BuildCommitteeDutyID(committeeDuty.operatorIDs, dutyEpoch, slot, role)

	return s.logger.
		With(fields.RunnerRole(role)).
		With(fields.Slot(slot)).
		With(fields.DutyID(committeeDutyID)).
		With(fields.CommitteeID(committeeDuty.id)).
		With(fields.EstimatedCurrentEpoch(s.netCfg.EstimatedCurrentEpoch())).
		With(fields.EstimatedCurrentSlot(s.netCfg.EstimatedCurrentSlot()))
}

// advanceHeadSlot will set s.headSlot to the provided slot (but only if the provided slot is higher,
// meaning s.headSlot value can never decrease) and notify the go-routines waiting for it to happen.
func (s *Scheduler) advanceHeadSlot(slot phase0.Slot) {
	s.logger.Debug("advancing head slot (maybe)")
	defer s.logger.Debug("advancing head slot (done)")

	s.waitCond.L.Lock()
	if slot > s.headSlot {
		s.logger.Debug("advancing head slot",
			zap.Uint64("prev_head_slot", uint64(s.headSlot)),
			zap.Uint64("slot", uint64(slot)),
		)
		s.headSlot = slot
		s.waitCond.Broadcast()
	}
	s.waitCond.L.Unlock()
}

// waitOneThirdIntoSlotOrValidBlock waits until one-third of the slot has passed (SECONDS_PER_SLOT / 3 seconds after
// slot start time), or for a head block event that might come in even sooner than one-third of the slot passes.
func (s *Scheduler) waitOneThirdIntoSlotOrValidBlock(slot phase0.Slot) {
	s.logger.Debug("waiting 1/3 into slot (maybe)")
	defer s.logger.Debug("waiting 1/3 into slot (done)")

	s.waitCond.L.Lock()
	for s.headSlot < slot {
		s.logger.Debug("waiting 1/3 into slot",
			zap.Uint64("current_head_slot", uint64(s.headSlot)),
			zap.Uint64("slot", uint64(slot)),
		)
		s.waitCond.Wait()
	}
	s.waitCond.L.Unlock()
}

func indicesFromShares(shares []*types.SSVShare) []phase0.ValidatorIndex {
	indices := make([]phase0.ValidatorIndex, len(shares))
	for i, share := range shares {
		indices[i] = share.ValidatorIndex
	}
	return indices
}

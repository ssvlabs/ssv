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
	"github.com/sourcegraph/conc/pool"
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
)

// DutiesExecutor is an interface for executing duties.
type DutiesExecutor interface {
	ExecuteDuties(ctx context.Context, duties []*spectypes.ValidatorDuty)
	ExecuteCommitteeDuties(ctx context.Context, duties committeeDutiesMap)
}

// DutyExecutor is an interface for executing duty.
type DutyExecutor interface {
	ExecuteDuty(ctx context.Context, duty *spectypes.ValidatorDuty)
	ExecuteCommitteeDuty(ctx context.Context, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty)
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
	BeaconConfig            *networkconfig.Beacon
	ValidatorProvider       ValidatorProvider
	ValidatorController     ValidatorController
	DutyExecutor            DutyExecutor
	IndicesChg              chan struct{}
	ValidatorRegistrationCh <-chan RegistrationDescriptor
	ValidatorExitCh         <-chan ExitDescriptor
	SlotTickerProvider      slotticker.Provider
	DutyStore               *dutystore.Store
	P2PNetwork              network.P2PNetwork
}

type Scheduler struct {
	logger              *zap.Logger
	beaconNode          BeaconNode
	executionClient     ExecutionClient
	beaconConfig        *networkconfig.Beacon
	validatorProvider   ValidatorProvider
	validatorController ValidatorController
	slotTickerProvider  slotticker.Provider
	dutyExecutor        DutyExecutor

	handlers            []dutyHandler
	blockPropagateDelay time.Duration

	reorg      chan ReorgEvent
	indicesChg chan struct{}
	ticker     slotticker.SlotTicker
	pool       *pool.ContextPool

	// waitCond coordinates access to headSlot for different go-routines
	waitCond *sync.Cond
	headSlot phase0.Slot

	lastBlockEpoch            phase0.Epoch
	currentDutyDependentRoot  phase0.Root
	previousDutyDependentRoot phase0.Root
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
		beaconConfig:        opts.BeaconConfig,
		slotTickerProvider:  opts.SlotTickerProvider,
		dutyExecutor:        opts.DutyExecutor,
		validatorProvider:   opts.ValidatorProvider,
		validatorController: opts.ValidatorController,
		indicesChg:          opts.IndicesChg,
		blockPropagateDelay: blockPropagationDelay,

		handlers: []dutyHandler{
			NewAttesterHandler(dutyStore.Attester),
			NewProposerHandler(dutyStore.Proposer),
			NewSyncCommitteeHandler(dutyStore.SyncCommittee),
			NewValidatorRegistrationHandler(opts.ValidatorRegistrationCh),
			NewVoluntaryExitHandler(dutyStore.VoluntaryExit, opts.ValidatorExitCh),
			NewCommitteeHandler(dutyStore.Attester, dutyStore.SyncCommittee),
		},

		ticker:   opts.SlotTickerProvider(),
		reorg:    make(chan ReorgEvent),
		waitCond: sync.NewCond(&sync.Mutex{}),
	}

	return s
}

type ReorgEvent struct {
	Slot     phase0.Slot
	Previous bool
	Current  bool
}

// Start initializes the Scheduler and begins its operation.
// Note: This function includes blocking operations, especially within the handler's HandleInitialDuties call,
// which will block until initial duties are fully handled.
func (s *Scheduler) Start(ctx context.Context) error {
	s.logger.Info("duty scheduler started")

	s.logger.Info("subscribing to head events")
	if err := s.listenToHeadEvents(ctx); err != nil {
		return fmt.Errorf("failed to listen to head events: %w", err)
	}

	s.pool = pool.New().WithContext(ctx).WithCancelOnError()

	indicesChangeFeed := NewEventFeed[struct{}]()
	reorgFeed := NewEventFeed[ReorgEvent]()

	for _, handler := range s.handlers {
		indicesChangeCh := make(chan struct{})
		indicesChangeFeed.Subscribe(indicesChangeCh)
		reorgCh := make(chan ReorgEvent)
		reorgFeed.Subscribe(reorgCh)

		handler.Setup(
			handler.Name(),
			s.logger,
			s.beaconNode,
			s.executionClient,
			s.beaconConfig,
			s.validatorProvider,
			s.validatorController,
			s,
			s.slotTickerProvider,
			reorgCh,
			indicesChangeCh,
		)

		// This call is blocking.
		handler.HandleInitialDuties(ctx)
		s.pool.Go(func(ctx context.Context) error {
			// Wait for the head event subscription to complete before starting the handler.
			handler.HandleDuties(ctx)
			return nil
		})
	}

	go s.SlotTicker(ctx)

	go indicesChangeFeed.FanOut(ctx, s.indicesChg)
	go reorgFeed.FanOut(ctx, s.reorg)

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

	go func() {
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

func (s *Scheduler) Wait() error {
	return s.pool.Wait()
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

			delay := s.beaconConfig.IntervalDuration()
			finalTime := s.beaconConfig.SlotStartTime(slot).Add(delay)
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
		var zeroRoot phase0.Root

		if event.Slot != s.beaconConfig.EstimatedCurrentSlot() {
			// No need to process outdated events here.
			return
		}

		// check for reorg
		epoch := s.beaconConfig.EstimatedEpochAtSlot(event.Slot)
		buildStr := fmt.Sprintf("e%v-s%v-#%v", epoch, event.Slot, event.Slot%32+1)
		logger := s.logger.With(zap.String("epoch_slot_pos", buildStr))
		if s.lastBlockEpoch != 0 {
			if epoch > s.lastBlockEpoch {
				// Change of epoch.
				// Ensure that the new previous dependent root is the same as the old current root.
				if !bytes.Equal(s.previousDutyDependentRoot[:], zeroRoot[:]) &&
					!bytes.Equal(s.currentDutyDependentRoot[:], event.PreviousDutyDependentRoot[:]) {
					logger.Debug("🔀 Previous duty dependent root has changed on epoch transition",
						zap.String("old_current_dependent_root", fmt.Sprintf("%#x", s.currentDutyDependentRoot[:])),
						zap.String("new_previous_dependent_root", fmt.Sprintf("%#x", event.PreviousDutyDependentRoot[:])))

					s.reorg <- ReorgEvent{
						Slot:     event.Slot,
						Previous: true,
					}
				}
			} else {
				// Same epoch
				// Ensure that the previous dependent roots are the same.
				if !bytes.Equal(s.previousDutyDependentRoot[:], zeroRoot[:]) &&
					!bytes.Equal(s.previousDutyDependentRoot[:], event.PreviousDutyDependentRoot[:]) {
					logger.Debug("🔀 Previous duty dependent root has changed",
						zap.String("old_previous_dependent_root", fmt.Sprintf("%#x", s.previousDutyDependentRoot[:])),
						zap.String("new_previous_dependent_root", fmt.Sprintf("%#x", event.PreviousDutyDependentRoot[:])))

					s.reorg <- ReorgEvent{
						Slot:     event.Slot,
						Previous: true,
					}
				}

				// Ensure that the current dependent roots are the same.
				if !bytes.Equal(s.currentDutyDependentRoot[:], zeroRoot[:]) &&
					!bytes.Equal(s.currentDutyDependentRoot[:], event.CurrentDutyDependentRoot[:]) {
					logger.Debug("🔀 Current duty dependent root has changed",
						zap.String("old_current_dependent_root", fmt.Sprintf("%#x", s.currentDutyDependentRoot[:])),
						zap.String("new_current_dependent_root", fmt.Sprintf("%#x", event.CurrentDutyDependentRoot[:])))

					s.reorg <- ReorgEvent{
						Slot:    event.Slot,
						Current: true,
					}
				}
			}
		}

		s.lastBlockEpoch = epoch
		s.previousDutyDependentRoot = event.PreviousDutyDependentRoot
		s.currentDutyDependentRoot = event.CurrentDutyDependentRoot

		currentTime := time.Now()
		delay := s.beaconConfig.IntervalDuration()
		slotStartTimeWithDelay := s.beaconConfig.SlotStartTime(event.Slot).Add(delay)
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

// ExecuteDuties tries to execute the given duties
func (s *Scheduler) ExecuteDuties(ctx context.Context, duties []*spectypes.ValidatorDuty) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "scheduler.execute_duties"),
		trace.WithAttributes(observability.DutyCountAttribute(len(duties))),
	)
	defer span.End()

	for _, duty := range duties {
		logger := s.loggerWithDutyContext(duty)

		slotDelay := time.Since(s.beaconConfig.SlotStartTime(duty.Slot))
		if slotDelay >= 100*time.Millisecond {
			const eventMsg = "⚠️ late duty execution"
			logger.Warn(eventMsg, zap.Duration("slot_delay", slotDelay))
			span.AddEvent(eventMsg, trace.WithAttributes(
				attribute.Int64("ssv.beacon.slot_delay_ms", slotDelay.Milliseconds()),
				observability.BeaconRoleAttribute(duty.Type),
				observability.RunnerRoleAttribute(duty.RunnerRole())))
		}

		slotDelayHistogram.Record(ctx, slotDelay.Seconds())

		parentDeadline, ok := ctx.Deadline()
		if !ok {
			const eventMsg = "failed to get parent-context deadline"
			span.AddEvent(eventMsg)
			s.logger.Warn(eventMsg)
			span.SetStatus(codes.Ok, "")
			return
		}

		go func() {
			// We want to inherit parent-context deadline, but we cannot use the parent-context itself
			// here because we are now running asynchronously with the go-routine that's managing
			// parent-context, and as a result once it decides it's done with the parent-context it will
			// cancel it (also canceling our operations here). Thus, we create our own context instance.
			dutyCtx, cancel := context.WithDeadline(context.Background(), parentDeadline)
			defer cancel()

			recordDutyExecuted(dutyCtx, duty.RunnerRole())
			s.dutyExecutor.ExecuteDuty(dutyCtx, duty)
		}()
	}

	span.SetStatus(codes.Ok, "")
}

// ExecuteCommitteeDuties tries to execute the given committee duties
func (s *Scheduler) ExecuteCommitteeDuties(ctx context.Context, duties committeeDutiesMap) {
	ctx, span := tracer.Start(ctx, observability.InstrumentName(observabilityNamespace, "scheduler.execute_committee_duties"))
	defer span.End()

	for _, committee := range duties {
		duty := committee.duty
		logger := s.loggerWithCommitteeDutyContext(committee)

		const eventMsg = "🔧 executing committee duty"
		dutyEpoch := s.beaconConfig.EstimatedEpochAtSlot(duty.Slot)
		logger.Debug(eventMsg, fields.Duties(dutyEpoch, duty.ValidatorDuties))
		span.AddEvent(eventMsg, trace.WithAttributes(
			observability.CommitteeIDAttribute(committee.id),
			observability.DutyCountAttribute(len(duty.ValidatorDuties)),
		))

		slotDelay := time.Since(s.beaconConfig.SlotStartTime(duty.Slot))
		if slotDelay >= 100*time.Millisecond {
			const eventMsg = "⚠️ late duty execution"
			logger.Warn(eventMsg, zap.Duration("slot_delay", slotDelay))
			span.AddEvent(eventMsg, trace.WithAttributes(
				observability.CommitteeIDAttribute(committee.id),
				attribute.Int64("ssv.beacon.slot_delay_ms", slotDelay.Milliseconds())))
		}

		slotDelayHistogram.Record(ctx, slotDelay.Seconds())

		parentDeadline, ok := ctx.Deadline()
		if !ok {
			const eventMsg = "failed to get parent-context deadline"
			span.AddEvent(eventMsg)
			s.logger.Warn(eventMsg)
			span.SetStatus(codes.Ok, "")
			return
		}

		go func() {
			// We want to inherit parent-context deadline, but we cannot use the parent-context itself
			// here because we are now running asynchronously with the go-routine that's managing
			// parent-context, and as a result once it decides it's done with the parent-context it will
			// cancel it (also canceling our operations here). Thus, we create our own context instance.
			dutyCtx, cancel := context.WithDeadline(context.Background(), parentDeadline)
			defer cancel()

			s.waitOneThirdOrValidBlock(duty.Slot)
			recordDutyExecuted(dutyCtx, duty.RunnerRole())
			s.dutyExecutor.ExecuteCommitteeDuty(dutyCtx, committee.id, duty)
		}()
	}

	span.SetStatus(codes.Ok, "")
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (s *Scheduler) loggerWithDutyContext(duty *spectypes.ValidatorDuty) *zap.Logger {
	return s.logger.
		With(fields.BeaconRole(duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(s.beaconConfig.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(s.beaconConfig.EstimatedEpochAtSlot(duty.Slot))).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.SlotStartTime(s.beaconConfig.SlotStartTime(duty.Slot)))
}

// loggerWithCommitteeDutyContext returns an instance of logger with the given committee duty's information
func (s *Scheduler) loggerWithCommitteeDutyContext(committeeDuty *committeeDuty) *zap.Logger {
	duty := committeeDuty.duty
	dutyEpoch := s.beaconConfig.EstimatedEpochAtSlot(duty.Slot)
	committeeDutyID := fields.BuildCommitteeDutyID(committeeDuty.operatorIDs, dutyEpoch, duty.Slot)

	return s.logger.
		With(fields.CommitteeID(committeeDuty.id)).
		With(fields.DutyID(committeeDutyID)).
		With(fields.RunnerRole(duty.RunnerRole())).
		With(fields.CurrentSlot(s.beaconConfig.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(dutyEpoch)).
		With(fields.SlotStartTime(s.beaconConfig.SlotStartTime(duty.Slot)))
}

// advanceHeadSlot will set s.headSlot to the provided slot (but only if the provided slot is higher,
// meaning s.headSlot value can never decrease) and notify the go-routines waiting for it to happen.
func (s *Scheduler) advanceHeadSlot(slot phase0.Slot) {
	s.waitCond.L.Lock()
	if slot > s.headSlot {
		s.headSlot = slot
		s.waitCond.Broadcast()
	}
	s.waitCond.L.Unlock()
}

// waitOneThirdOrValidBlock waits until one-third of the slot has passed (SECONDS_PER_SLOT / 3 seconds after
// slot start time), or for head block event that might come in even sooner than one-third of the slot passes.
func (s *Scheduler) waitOneThirdOrValidBlock(slot phase0.Slot) {
	s.waitCond.L.Lock()
	for s.headSlot < slot {
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

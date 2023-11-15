package duties

import (
	"bytes"
	"context"
	"fmt"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/duties/dutystore"
	"github.com/bloxapp/ssv/operator/slotticker"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/scheduler.go -source=./scheduler.go

var slotDelayHistogram = promauto.NewHistogram(prometheus.HistogramOpts{
	Name:    "slot_ticker_delay_milliseconds",
	Help:    "The delay in milliseconds of the slot ticker",
	Buckets: []float64{5, 10, 20, 100, 500, 5000}, // Buckets in milliseconds. Adjust as per your needs.
})

func init() {
	logger := zap.L()
	if err := prometheus.Register(slotDelayHistogram); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}

const (
	// blockPropagationDelay time to propagate around the nodes
	// before kicking off duties for the block's slot.
	blockPropagationDelay = 200 * time.Millisecond
)

type SlotTicker interface {
	Next() <-chan time.Time
	Slot() phase0.Slot
}

type BeaconNode interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error
	SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error
	SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error
}

// ValidatorController represents the component that controls validators via the scheduler
type ValidatorController interface {
	CommitteeActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
	AllActiveIndices(epoch phase0.Epoch) []phase0.ValidatorIndex
	GetOperatorShares() []*types.SSVShare
}

type ExecuteDutyFunc func(logger *zap.Logger, duty *spectypes.Duty)

type SchedulerOptions struct {
	Ctx                 context.Context
	BeaconNode          BeaconNode
	Network             networkconfig.NetworkConfig
	ValidatorController ValidatorController
	ExecuteDuty         ExecuteDutyFunc
	IndicesChg          chan struct{}
	SlotTickerProvider  slotticker.Provider
	BuilderProposals    bool
	DutyStore           *dutystore.Store
}

type Scheduler struct {
	beaconNode          BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	slotTickerProvider  slotticker.Provider
	executeDuty         ExecuteDutyFunc
	builderProposals    bool

	handlers            []dutyHandler
	blockPropagateDelay time.Duration

	reorg      chan ReorgEvent
	indicesChg chan struct{}
	ticker     slotticker.SlotTicker
	waitCond   *sync.Cond
	pool       *pool.ContextPool

	headSlot                  phase0.Slot
	lastBlockEpoch            phase0.Epoch
	currentDutyDependentRoot  phase0.Root
	previousDutyDependentRoot phase0.Root
}

func NewScheduler(opts *SchedulerOptions) *Scheduler {
	dutyStore := opts.DutyStore
	if dutyStore == nil {
		dutyStore = dutystore.New()
	}

	s := &Scheduler{
		beaconNode:          opts.BeaconNode,
		network:             opts.Network,
		slotTickerProvider:  opts.SlotTickerProvider,
		executeDuty:         opts.ExecuteDuty,
		validatorController: opts.ValidatorController,
		builderProposals:    opts.BuilderProposals,
		indicesChg:          opts.IndicesChg,
		blockPropagateDelay: blockPropagationDelay,

		handlers: []dutyHandler{
			NewAttesterHandler(dutyStore.Attester),
			NewProposerHandler(dutyStore.Proposer),
			NewSyncCommitteeHandler(dutyStore.SyncCommittee),
		},

		ticker:   opts.SlotTickerProvider(),
		reorg:    make(chan ReorgEvent),
		waitCond: sync.NewCond(&sync.Mutex{}),
	}
	if s.builderProposals {
		s.handlers = append(s.handlers, NewValidatorRegistrationHandler())
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
func (s *Scheduler) Start(ctx context.Context, logger *zap.Logger) error {
	logger = logger.Named(logging.NameDutyScheduler)
	logger.Info("duty scheduler started")

	// Subscribe to head events. This allows us to go early for attestations & sync committees if a block arrives,
	// as well as re-request duties if there is a change in beacon block.
	if err := s.beaconNode.Events(ctx, []string{"head"}, s.HandleHeadEvent(logger)); err != nil {
		return fmt.Errorf("failed to subscribe to head events: %w", err)
	}

	s.pool = pool.New().WithContext(ctx).WithCancelOnError()

	indicesChangeFeed := NewEventFeed[struct{}]()
	reorgFeed := NewEventFeed[ReorgEvent]()

	for _, handler := range s.handlers {
		handler := handler

		indicesChangeCh := make(chan struct{})
		indicesChangeFeed.Subscribe(indicesChangeCh)
		reorgCh := make(chan ReorgEvent)
		reorgFeed.Subscribe(reorgCh)

		handler.Setup(
			handler.Name(),
			logger,
			s.beaconNode,
			s.network,
			s.validatorController,
			s.ExecuteDuties,
			s.slotTickerProvider,
			reorgCh,
			indicesChangeCh,
		)

		// This call is blocking
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

// SlotTicker handles the "head" events from the beacon node.
func (s *Scheduler) SlotTicker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case <-s.ticker.Next():
			slot := s.ticker.Slot()

			delay := s.network.SlotDurationSec() / time.Duration(goclient.IntervalsPerSlot) /* a third of the slot duration */
			finalTime := s.network.Beacon.GetSlotStartTime(slot).Add(delay)
			waitDuration := time.Until(finalTime)

			if waitDuration > 0 {
				time.Sleep(waitDuration)

				// Lock the mutex before broadcasting
				s.waitCond.L.Lock()
				s.headSlot = slot
				s.waitCond.Broadcast()
				s.waitCond.L.Unlock()
			}
		}
	}
}

// HandleHeadEvent handles the "head" events from the beacon node.
func (s *Scheduler) HandleHeadEvent(logger *zap.Logger) func(event *eth2apiv1.Event) {
	return func(event *eth2apiv1.Event) {
		if event.Data == nil {
			return
		}

		var zeroRoot phase0.Root

		data := event.Data.(*eth2apiv1.HeadEvent)
		if data.Slot != s.network.Beacon.EstimatedCurrentSlot() {
			return
		}

		// check for reorg
		epoch := s.network.Beacon.EstimatedEpochAtSlot(data.Slot)
		buildStr := fmt.Sprintf("e%v-s%v-#%v", epoch, data.Slot, data.Slot%32+1)
		logger := logger.With(zap.String("epoch_slot_seq", buildStr))
		if s.lastBlockEpoch != 0 {
			if epoch > s.lastBlockEpoch {
				// Change of epoch.
				// Ensure that the new previous dependent root is the same as the old current root.
				if !bytes.Equal(s.previousDutyDependentRoot[:], zeroRoot[:]) &&
					!bytes.Equal(s.currentDutyDependentRoot[:], data.PreviousDutyDependentRoot[:]) {
					logger.Debug("🔀 Previous duty dependent root has changed on epoch transition",
						zap.String("old_current_dependent_root", fmt.Sprintf("%#x", s.currentDutyDependentRoot[:])),
						zap.String("new_previous_dependent_root", fmt.Sprintf("%#x", data.PreviousDutyDependentRoot[:])))

					s.reorg <- ReorgEvent{
						Slot:     data.Slot,
						Previous: true,
					}
				}
			} else {
				// Same epoch
				// Ensure that the previous dependent roots are the same.
				if !bytes.Equal(s.previousDutyDependentRoot[:], zeroRoot[:]) &&
					!bytes.Equal(s.previousDutyDependentRoot[:], data.PreviousDutyDependentRoot[:]) {
					logger.Debug("🔀 Previous duty dependent root has changed",
						zap.String("old_previous_dependent_root", fmt.Sprintf("%#x", s.previousDutyDependentRoot[:])),
						zap.String("new_previous_dependent_root", fmt.Sprintf("%#x", data.PreviousDutyDependentRoot[:])))

					s.reorg <- ReorgEvent{
						Slot:     data.Slot,
						Previous: true,
					}
				}

				// Ensure that the current dependent roots are the same.
				if !bytes.Equal(s.currentDutyDependentRoot[:], zeroRoot[:]) &&
					!bytes.Equal(s.currentDutyDependentRoot[:], data.CurrentDutyDependentRoot[:]) {
					logger.Debug("🔀 Current duty dependent root has changed",
						zap.String("old_current_dependent_root", fmt.Sprintf("%#x", s.currentDutyDependentRoot[:])),
						zap.String("new_current_dependent_root", fmt.Sprintf("%#x", data.CurrentDutyDependentRoot[:])))

					s.reorg <- ReorgEvent{
						Slot:    data.Slot,
						Current: true,
					}
				}
			}
		}

		s.lastBlockEpoch = epoch
		s.previousDutyDependentRoot = data.PreviousDutyDependentRoot
		s.currentDutyDependentRoot = data.CurrentDutyDependentRoot

		currentTime := time.Now()
		delay := s.network.SlotDurationSec() / time.Duration(goclient.IntervalsPerSlot) /* a third of the slot duration */
		slotStartTimeWithDelay := s.network.Beacon.GetSlotStartTime(data.Slot).Add(delay)
		if currentTime.Before(slotStartTimeWithDelay) {
			logger.Debug("🏁 Head event: Block arrived before 1/3 slot", zap.Duration("time_saved", slotStartTimeWithDelay.Sub(currentTime)))

			// We give the block some time to propagate around the rest of the
			// nodes before kicking off duties for the block's slot.
			time.Sleep(s.blockPropagateDelay)

			s.waitCond.L.Lock()
			s.headSlot = data.Slot
			s.waitCond.Broadcast()
			s.waitCond.L.Unlock()
		}
	}
}

// ExecuteDuties tries to execute the given duties
func (s *Scheduler) ExecuteDuties(logger *zap.Logger, duties []*spectypes.Duty) {
	for _, duty := range duties {
		duty := duty
		logger := s.loggerWithDutyContext(logger, duty)
		slotDelay := time.Since(s.network.Beacon.GetSlotStartTime(duty.Slot))
		if slotDelay >= 100*time.Millisecond {
			logger.Debug("⚠️ late duty execution", zap.Int64("slot_delay", slotDelay.Milliseconds()))
		}
		slotDelayHistogram.Observe(float64(slotDelay.Milliseconds()))
		go func() {
			if duty.Type == spectypes.BNRoleAttester || duty.Type == spectypes.BNRoleSyncCommittee {
				s.waitOneThirdOrValidBlock(duty.Slot)
			}
			s.executeDuty(logger, duty)
		}()
	}
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (s *Scheduler) loggerWithDutyContext(logger *zap.Logger, duty *spectypes.Duty) *zap.Logger {
	return logger.
		With(fields.Role(duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(s.network.Beacon.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(s.network.Beacon.EstimatedEpochAtSlot(duty.Slot))).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.StartTimeUnixMilli(s.network.Beacon.GetSlotStartTime(duty.Slot)))
}

// waitOneThirdOrValidBlock waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (s *Scheduler) waitOneThirdOrValidBlock(slot phase0.Slot) {
	// Wait for the event or signal
	s.waitCond.L.Lock()
	for s.headSlot < slot {
		s.waitCond.Wait()
	}
	s.waitCond.L.Unlock()
}

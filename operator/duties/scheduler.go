package duties

import (
	"bytes"
	"context"
	"fmt"
	"math/big"
	"sync"
	"time"

	eth2client "github.com/attestantio/go-eth2-client"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/sourcegraph/conc/pool"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient"
	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/operator/slotticker"
	"github.com/ssvlabs/ssv/protocol/v2/types"
)

//go:generate mockgen -package=duties -destination=./scheduler_mock.go -source=./scheduler.go

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

// DutiesExecutor is an interface for executing duties.
type DutiesExecutor interface {
	ExecuteGenesisDuties(logger *zap.Logger, duties []*genesisspectypes.Duty)
	ExecuteDuties(logger *zap.Logger, duties []*spectypes.BeaconDuty)
	ExecuteCommitteeDuties(logger *zap.Logger, duties committeeDutiesMap)
}

// DutyExecutor is an interface for executing duty.
type DutyExecutor interface {
	ExecuteGenesisDuty(logger *zap.Logger, duty *genesisspectypes.Duty)
	ExecuteDuty(logger *zap.Logger, duty *spectypes.BeaconDuty)
	ExecuteCommitteeDuty(logger *zap.Logger, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty)
}

type BeaconNode interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error
	SubmitBeaconCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.BeaconCommitteeSubscription) error
	SubmitSyncCommitteeSubscriptions(ctx context.Context, subscription []*eth2apiv1.SyncCommitteeSubscription) error
}

type ExecutionClient interface {
	BlockByNumber(ctx context.Context, blockNumber *big.Int) (*ethtypes.Block, error)
}

// ValidatorProvider represents the component that controls validators via the scheduler
type ValidatorProvider interface {
	ParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
	SelfParticipatingValidators(epoch phase0.Epoch) []*types.SSVShare
	Validator(pubKey []byte) *types.SSVShare
}

// ValidatorController represents the component that controls validators via the scheduler
type ValidatorController interface {
	AllActiveIndices(epoch phase0.Epoch, afterInit bool) []phase0.ValidatorIndex
}

type SchedulerOptions struct {
	Ctx                 context.Context
	BeaconNode          BeaconNode
	ExecutionClient     ExecutionClient
	Network             networkconfig.NetworkConfig
	ValidatorProvider   ValidatorProvider
	ValidatorController ValidatorController
	DutyExecutor        DutyExecutor
	IndicesChg          chan struct{}
	ValidatorExitCh     <-chan ExitDescriptor
	SlotTickerProvider  slotticker.Provider
	DutyStore           *dutystore.Store
}

type Scheduler struct {
	beaconNode          BeaconNode
	executionClient     ExecutionClient
	network             networkconfig.NetworkConfig
	validatorProvider   ValidatorProvider
	validatorController ValidatorController
	slotTickerProvider  slotticker.Provider
	dutyExecutor        DutyExecutor

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
		executionClient:     opts.ExecutionClient,
		network:             opts.Network,
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
		indicesChangeCh := make(chan struct{})
		indicesChangeFeed.Subscribe(indicesChangeCh)
		reorgCh := make(chan ReorgEvent)
		reorgFeed.Subscribe(reorgCh)

		handler.Setup(
			handler.Name(),
			logger,
			s.beaconNode,
			s.executionClient,
			s.network,
			s.validatorProvider,
			s.validatorController,
			s,
			s.slotTickerProvider,
			reorgCh,
			indicesChangeCh,
		)

		// This call is blocking.
		handler.HandleInitialDuties(ctx)

		handler := handler
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
		logger := logger.With(zap.String("epoch_slot_pos", buildStr))
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

func (s *Scheduler) ExecuteGenesisDuties(logger *zap.Logger, duties []*genesisspectypes.Duty) {
	for _, duty := range duties {
		duty := duty
		logger := s.loggerWithGenesisDutyContext(logger, duty)
		slotDelay := time.Since(s.network.Beacon.GetSlotStartTime(duty.Slot))
		if slotDelay >= 100*time.Millisecond {
			logger.Debug("⚠️ late duty execution", zap.Int64("slot_delay", slotDelay.Milliseconds()))
		}
		slotDelayHistogram.Observe(float64(slotDelay.Milliseconds()))
		go func() {
			if duty.Type == genesisspectypes.BNRoleAttester || duty.Type == genesisspectypes.BNRoleSyncCommittee {
				s.waitOneThirdOrValidBlock(duty.Slot)
			}
			s.dutyExecutor.ExecuteGenesisDuty(logger, duty)
		}()
	}
}

// ExecuteDuties tries to execute the given duties
func (s *Scheduler) ExecuteDuties(logger *zap.Logger, duties []*spectypes.BeaconDuty) {
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
			s.dutyExecutor.ExecuteDuty(logger, duty)
		}()
	}
}

// ExecuteCommitteeDuties tries to execute the given committee duties
func (s *Scheduler) ExecuteCommitteeDuties(logger *zap.Logger, duties committeeDutiesMap) {
	for committeeID, duty := range duties {
		committeeID := committeeID
		duty := duty
		logger := s.loggerWithCommitteeDutyContext(logger, committeeID, duty)
		slotDelay := time.Since(s.network.Beacon.GetSlotStartTime(duty.Slot))
		if slotDelay >= 100*time.Millisecond {
			logger.Debug("⚠️ late duty execution", zap.Int64("slot_delay", slotDelay.Milliseconds()))
		}
		slotDelayHistogram.Observe(float64(slotDelay.Milliseconds()))
		go func() {
			s.waitOneThirdOrValidBlock(duty.Slot)
			s.dutyExecutor.ExecuteCommitteeDuty(logger, committeeID, duty)
		}()
	}
}

// loggerWithGenesisDutyContext returns an instance of logger with the given genesis duty's information
func (s *Scheduler) loggerWithGenesisDutyContext(logger *zap.Logger, duty *genesisspectypes.Duty) *zap.Logger {
	return logger.
		With(zap.Stringer(fields.FieldRole, duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(s.network.Beacon.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(s.network.Beacon.EstimatedEpochAtSlot(duty.Slot))).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.StartTimeUnixMilli(s.network.Beacon.GetSlotStartTime(duty.Slot)))
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (s *Scheduler) loggerWithDutyContext(logger *zap.Logger, duty *spectypes.BeaconDuty) *zap.Logger {
	return logger.
		With(fields.BeaconRole(duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(s.network.Beacon.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(s.network.Beacon.EstimatedEpochAtSlot(duty.Slot))).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.StartTimeUnixMilli(s.network.Beacon.GetSlotStartTime(duty.Slot)))
}

// loggerWithCommitteeDutyContext returns an instance of logger with the given committee duty's information
func (s *Scheduler) loggerWithCommitteeDutyContext(logger *zap.Logger, committeeID spectypes.CommitteeID, duty *spectypes.CommitteeDuty) *zap.Logger {
	dutyEpoch := s.network.Beacon.EstimatedEpochAtSlot(duty.Slot)
	return logger.
		With(fields.CommitteeID(committeeID)).
		With(fields.Role(duty.RunnerRole())).
		With(fields.Duties(dutyEpoch, duty.BeaconDuties)).
		With(fields.CurrentSlot(s.network.Beacon.EstimatedCurrentSlot())).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(dutyEpoch)).
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

func indicesFromShares(shares []*types.SSVShare) []phase0.ValidatorIndex {
	indices := make([]phase0.ValidatorIndex, len(shares))
	for i, share := range shares {
		indices[i] = share.ValidatorIndex
	}
	return indices
}

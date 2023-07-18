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
	"github.com/prysmaticlabs/prysm/v4/async/event"
	"github.com/sourcegraph/conc/pool"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/scheduler.go -source=./scheduler.go

type SlotTicker interface {
	Subscribe(subscription chan phase0.Slot) event.Subscription
}

type BeaconNode interface {
	AttesterDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.AttesterDuty, error)
	ProposerDuties(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*eth2apiv1.ProposerDuty, error)
	SyncCommitteeDuties(ctx context.Context, epoch phase0.Epoch, indices []phase0.ValidatorIndex) ([]*eth2apiv1.SyncCommitteeDuty, error)
	Events(ctx context.Context, topics []string, handler eth2client.EventHandlerFunc) error
	SubscribeToCommitteeSubnet(subscription []*eth2apiv1.BeaconCommitteeSubscription) error
	SubmitSyncCommitteeSubscriptions(subscription []*eth2apiv1.SyncCommitteeSubscription) error
}

type ExecuteDutyFunc func(logger *zap.Logger, duty *spectypes.Duty)

type SchedulerOptions struct {
	Ctx                 context.Context
	BeaconNode          BeaconNode
	Network             networkconfig.NetworkConfig
	ValidatorController ValidatorController
	ExecuteDuty         ExecuteDutyFunc
	IndicesChg          chan bool
	Ticker              SlotTicker
	BuilderProposals    bool
}

type Scheduler struct {
	beaconNode          BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	slotTicker          SlotTicker
	executeDuty         ExecuteDutyFunc

	handlers []dutyHandler

	reorg      chan ReorgEvent
	indicesChg chan bool

	waitMutex sync.Mutex
	waitCond  *sync.Cond

	lastBlockEpoch            phase0.Epoch
	currentDutyDependentRoot  phase0.Root
	previousDutyDependentRoot phase0.Root
	builderProposals          bool
}

// ValidatorController represents the component that controls validators via the scheduler
type ValidatorController interface {
	ActiveIndices(logger *zap.Logger, epoch phase0.Epoch) []phase0.ValidatorIndex
	GetOperatorShares() []*types.SSVShare
}

func NewScheduler(opts *SchedulerOptions) *Scheduler {
	s := &Scheduler{
		beaconNode:          opts.BeaconNode,
		network:             opts.Network,
		slotTicker:          opts.Ticker,
		executeDuty:         opts.ExecuteDuty,
		validatorController: opts.ValidatorController,
		builderProposals:    opts.BuilderProposals,
		indicesChg:          opts.IndicesChg,
	}
	s.handlers = []dutyHandler{
		NewAttesterHandler(),
		NewProposerHandler(),
		NewSyncCommitteeHandler(),
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

func (s *Scheduler) Run(ctx context.Context, logger *zap.Logger, ready chan<- struct{}) error {
	logger = logger.Named(logging.NameDutyScheduler)
	logger.Info("starting scheduler")

	// Initialize the reorg channel
	s.reorg = make(chan ReorgEvent)

	s.waitCond = sync.NewCond(&s.waitMutex)

	// Subscribe to head events.  This allows us to go early for attestations if a block arrives, as well as
	// re-request duties if there is a change in beacon block.
	if err := s.beaconNode.Events(ctx, []string{"head"}, s.HandleHeadEvent(logger)); err != nil {
		return fmt.Errorf("failed to subscribe to head events: %w", err)
	}

	handlerPool := pool.New().WithContext(ctx)
	var reorgChs []chan<- ReorgEvent
	var indicesChangeChs []chan<- bool
	for _, handler := range s.handlers {
		h := handler
		slotTicker := make(chan phase0.Slot, 32)
		s.slotTicker.Subscribe(slotTicker)

		indicesChangeCh := make(chan bool)
		indicesChangeChs = append(indicesChangeChs, indicesChangeCh)
		reorgCh := make(chan ReorgEvent)
		reorgChs = append(reorgChs, reorgCh)
		h.Setup(s.beaconNode, s.network, s.validatorController, s.ExecuteDuties, slotTicker, reorgCh, indicesChangeCh)

		handlerPool.Go(func(ctx context.Context) error {
			h.HandleDuties(ctx, logger)
			return nil
		})
	}

	go fanOut(ctx, s.indicesChg, indicesChangeChs)
	go fanOut(ctx, s.reorg, reorgChs)

	if ready != nil {
		ready <- struct{}{}
	}
	return handlerPool.Wait()
}

// TODO: use a fan out package or Prysm's event.Feed, because sends in this implementation are blocking
func fanOut[T any](ctx context.Context, in <-chan T, subscribers []chan<- T) {
	for {
		select {
		case <-ctx.Done():
			return
		case item := <-in:
			for _, s := range subscribers {
				// Fan out the message to all subscribers.
				select {
				case s <- item:
				case <-ctx.Done():
				}
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
		delay := s.network.SlotDurationSec() / 3 /* a third of the slot duration */
		slotStartTimeWithDelay := s.network.Beacon.GetSlotStartTime(data.Slot).Add(delay)
		if currentTime.Before(slotStartTimeWithDelay) {
			logger.Debug("🏁 Head event: Block arrived before 1/3 slot", zap.Duration("time_saved", slotStartTimeWithDelay.Sub(currentTime)))
			s.waitMutex.Lock()
			s.waitCond.Broadcast()
			s.waitMutex.Unlock()
		}
	}
}

// ExecuteDuties tries to execute the given duties
func (s *Scheduler) ExecuteDuties(logger *zap.Logger, duties []*spectypes.Duty) {
	for _, duty := range duties {
		duty := duty
		logger := s.loggerWithDutyContext(logger, duty)
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
	s.waitMutex.Lock()
	defer s.waitMutex.Unlock()

	delay := s.network.SlotDurationSec() / 3 /* a third of the slot duration */
	finalTime := s.network.Beacon.GetSlotStartTime(slot).Add(delay)
	waitDuration := time.Until(finalTime)

	if waitDuration <= 0 {
		return
	}

	go func() {
		// Sleep for the specified duration
		time.Sleep(waitDuration)

		// Lock the mutex before broadcasting
		s.waitMutex.Lock()
		s.waitCond.Broadcast()
		s.waitMutex.Unlock()
	}()

	// Wait for the event or signal
	s.waitCond.Wait()
}

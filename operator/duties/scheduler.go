package duties

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	validator2 "github.com/bloxapp/ssv/protocol/v2/ssv/validator"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

type SchedulerOptions struct {
	Ctx                 context.Context
	BeaconClient        beaconprotocol.BeaconNode
	Network             networkconfig.NetworkConfig
	ValidatorController ValidatorController
	Executor            DutyExecutor
	//DutyLimit           uint64
	Ticker           slot_ticker.Ticker
	IndicesChange    chan bool
	BuilderProposals bool
}

type Scheduler struct {
	beaconNode          beaconprotocol.BeaconNode
	network             networkconfig.NetworkConfig
	validatorController ValidatorController
	slotTicker          slot_ticker.Ticker
	executor            DutyExecutor

	reorg chan ReorgEvent

	waitMutex sync.Mutex
	waitCond  *sync.Cond

	lastBlockEpoch            phase0.Epoch
	currentDutyDependentRoot  phase0.Root
	previousDutyDependentRoot phase0.Root
	builderProposals          bool
}

// DutyExecutor represents the component that executes duties
type DutyExecutor interface {
	ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty) error
}

// ValidatorController represents the component that controls validators via the scheduler
type ValidatorController interface {
	ActiveIndices(logger *zap.Logger, epoch phase0.Epoch) []phase0.ValidatorIndex
	GetValidator(pubKey string) (*validator2.Validator, bool)
	IndicesChangeChan() chan bool
	GetOperatorShares() []*types.SSVShare
}

func NewScheduler(opts *SchedulerOptions) Scheduler {
	return Scheduler{
		beaconNode:          opts.BeaconClient,
		network:             opts.Network,
		slotTicker:          opts.Ticker,
		executor:            opts.Executor,
		validatorController: opts.ValidatorController,
		builderProposals:    opts.BuilderProposals,
	}
}

type ReorgEvent struct {
	Slot     phase0.Slot
	Previous bool
	Current  bool
}

func (s *Scheduler) Run(ctx context.Context, logger *zap.Logger) error {
	logger = logger.Named(logging.NameDutyScheduler)
	logger.Info("starting scheduler")

	// Initialize the reorg channel
	s.reorg = make(chan ReorgEvent)

	s.waitCond = sync.NewCond(&s.waitMutex)

	// Subscribe to head events.  This allows us to go early for attestations if a block arrives, as well as
	// re-request duties if there is a change in beacon block.
	if err := s.beaconNode.Events(ctx, []string{"head"}, s.HandleHeadEvent(logger)); err != nil {
		return errors.Wrap(err, "failed to subscribe to head events")
	}

	handlers := []dutyHandler{
		NewAttesterHandler(),
		NewProposerHandler(),
		NewSyncCommitteeHandler(),
	}
	if s.builderProposals {
		handlers = append(handlers, NewValidatorRegistrationHandler())
	}

	var wg sync.WaitGroup
	for _, handler := range handlers {
		slotTicker := make(chan phase0.Slot)
		s.slotTicker.Subscribe(slotTicker)

		indicesChangeCh := make(chan bool)
		reorgCh := make(chan ReorgEvent)
		handler.Setup(s.beaconNode, s.network, s.validatorController, s.ExecuteDuties, slotTicker, reorgCh, indicesChangeCh)

		wg.Add(1)
		go func(h dutyHandler) {
			defer wg.Done()
			h.HandleDuties(ctx, logger)
		}(handler)
	}

	go func() {
		for change := range s.validatorController.IndicesChangeChan() {
			for _, h := range handlers {
				h.IndicesChangeChannel() <- change
			}
		}
	}()

	go func() {
		for event := range s.reorg {
			for _, h := range handlers {
				h.ReorgChannel() <- event
			}
		}
	}()

	wg.Wait()
	return nil
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
		logger := logger.With(zap.String("epoch_slot_sequence", buildStr))
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
		slotStartTimeWithDelay := s.slotStartTime(data.Slot).Add(delay)
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
		loggerWithContext := s.loggerWithDutyContext(logger, duty)

		dutyToExecute := duty
		go func() {
			err := s.ExecuteDuty(loggerWithContext, dutyToExecute)
			if err != nil {
				loggerWithContext.Error("failed to execute duty", zap.Error(err))
			}
		}()
	}
}

func (s *Scheduler) ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty) error {
	if s.executor != nil {
		// enables to work with a custom executor, e.g. readOnlyDutyExec
		return s.executor.ExecuteDuty(logger, duty)
	}

	if duty.Type == spectypes.BNRoleAttester || duty.Type == spectypes.BNRoleSyncCommittee {
		s.waitOneThirdOrValidBlock(duty.Slot)
	}

	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	if v, ok := s.validatorController.GetValidator(hex.EncodeToString(pk[:])); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pk, s.network.Domain)
		if err != nil {
			return err
		}
		dec, err := queue.DecodeSSVMessage(logger, ssvMsg)
		if err != nil {
			return err
		}
		if pushed := v.Queues[duty.Type].Q.TryPush(dec); !pushed {
			logger.Warn("dropping ExecuteDuty message because the queue is full")
		}
		// logger.Debug("📬 queue: pushed message", fields.MessageID(dec.MsgID), fields.MessageType(dec.MsgType))
	} else {
		logger.Warn("could not find validator")
	}

	return nil
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (s *Scheduler) loggerWithDutyContext(logger *zap.Logger, duty *spectypes.Duty) *zap.Logger {
	return logger.
		With(fields.Role(duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(s.network.Beacon)).
		With(fields.Slot(duty.Slot)).
		With(fields.Epoch(s.network.Beacon.EstimatedEpochAtSlot(duty.Slot))).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.StartTimeUnixMilli(s.network.Beacon, duty.Slot))
}

// waitOneThirdOrValidBlock waits until one-third of the slot has transpired (SECONDS_PER_SLOT / 3 seconds after the start of slot)
func (s *Scheduler) waitOneThirdOrValidBlock(slot phase0.Slot) {
	s.waitMutex.Lock()
	defer s.waitMutex.Unlock()

	delay := s.network.SlotDurationSec() / 3 /* a third of the slot duration */
	finalTime := s.slotStartTime(slot).Add(delay)
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

// SlotStartTime returns the start time in terms of its unix epoch value.
func (s *Scheduler) slotStartTime(slot phase0.Slot) time.Time {
	duration := time.Second * time.Duration(uint64(slot)*uint64(s.network.SlotDurationSec().Seconds()))
	startTime := time.Unix(int64(s.network.Beacon.MinGenesisTime()), 0).Add(duration)
	return startTime
}

// CreateDutyExecuteMsg returns ssvMsg with event type of duty execute
func CreateDutyExecuteMsg(duty *spectypes.Duty, pubKey phase0.BLSPubKey, domain spectypes.DomainType) (*spectypes.SSVMessage, error) {
	executeDutyData := types.ExecuteDutyData{Duty: duty}
	edd, err := json.Marshal(executeDutyData)
	if err != nil {
		return nil, errors.Wrap(err, "failed to marshal execute duty data")
	}
	msg := types.EventMsg{
		Type: types.ExecuteDuty,
		Data: edd,
	}
	data, err := msg.Encode()
	if err != nil {
		return nil, errors.Wrap(err, "failed to encode event msg")
	}
	return &spectypes.SSVMessage{
		MsgType: message.SSVEventMsgType,
		MsgID:   spectypes.NewMsgID(domain, pubKey[:], duty.Type),
		Data:    data,
	}, nil
}

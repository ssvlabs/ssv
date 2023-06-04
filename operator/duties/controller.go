package duties

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/cornelk/hashmap"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/operator/slot_ticker"
	"github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	beaconprotocol "github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/message"
	"github.com/bloxapp/ssv/protocol/v2/ssv/queue"
	"github.com/bloxapp/ssv/protocol/v2/types"
)

//go:generate mockgen -package=mocks -destination=./mocks/controller.go -source=./controller.go

// syncCommitteePreparationEpochs is the number of epochs ahead of the sync committee
// period change at which to prepare the relevant duties.
var syncCommitteePreparationEpochs = uint64(2)

const validatorRegistrationEpochInterval = uint64(10)

// DutyExecutor represents the component that executes duties
type DutyExecutor interface {
	ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty) error
}

// DutyController interface for dispatching duties execution according to slot ticker
type DutyController interface {
	Start(logger *zap.Logger)
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Ctx                 context.Context
	BeaconClient        beaconprotocol.BeaconNode
	Network             networkconfig.NetworkConfig
	ValidatorController validator.Controller
	Executor            DutyExecutor
	DutyLimit           uint64
	ForkVersion         forksprotocol.ForkVersion
	Ticker              slot_ticker.Ticker
	BuilderProposals    bool
}

// dutyController internal implementation of DutyController
type dutyController struct {
	ctx     context.Context
	network networkconfig.NetworkConfig
	// executor enables to work with a custom execution
	executor            DutyExecutor
	fetcher             DutyFetcher
	validatorController validator.Controller
	dutyLimit           uint64
	ticker              slot_ticker.Ticker
	builderProposals    bool

	// sync committee duties map [period, map[index, duty]]
	syncCommitteeDutiesMap            *hashmap.Map[uint64, *hashmap.Map[phase0.ValidatorIndex, *eth2apiv1.SyncCommitteeDuty]]
	lastBlockEpoch                    phase0.Epoch
	currentDutyDependentRoot          phase0.Root
	validatorsPassedFirstRegistration map[string]struct{}
}

var secPerSlot int64 = 12

// NewDutyController creates a new instance of DutyController
func NewDutyController(logger *zap.Logger, opts *ControllerOptions) DutyController {
	fetcher := newDutyFetcher(logger, opts.BeaconClient, opts.ValidatorController, opts.Network.Beacon)
	dc := dutyController{
		ctx:                 opts.Ctx,
		network:             opts.Network,
		fetcher:             fetcher,
		validatorController: opts.ValidatorController,
		dutyLimit:           opts.DutyLimit,
		executor:            opts.Executor,
		ticker:              opts.Ticker,
		builderProposals:    opts.BuilderProposals,

		syncCommitteeDutiesMap:            hashmap.New[uint64, *hashmap.Map[phase0.ValidatorIndex, *eth2apiv1.SyncCommitteeDuty]](),
		validatorsPassedFirstRegistration: map[string]struct{}{},
	}
	return &dc
}

// Start listens to slot ticker and dispatches duties execution
func (dc *dutyController) Start(logger *zap.Logger) {
	logger = logger.Named(logging.NameDutyController)
	// warmup
	indices := dc.validatorController.ActiveValidatorIndices(logger)
	logger.Debug("warming up indices", fields.Count(len(indices)))

	// Subscribe to head events.  This allows us to go early for attestations if a block arrives, as well as
	// re-request duties if there is a change in beacon block.
	// This also allows us to re-request duties if the dependent roots change.
	if err := dc.fetcher.Events(dc.ctx, []string{"head"}, dc.HandleHeadEvent(logger)); err != nil {
		logger.Error("failed to subscribe to head events", zap.Error(err))
	}
	dc.warmUpDuties(logger)
	tickerChan := make(chan phase0.Slot, 32)
	dc.ticker.Subscribe(tickerChan)
	dc.listenToTicker(logger, tickerChan)
}

// warmUpDuties preparing duties for all validators when the node starts
// this is done to avoid missing duties for the current epoch when the node starts
// for now we are fetching duties in this way for sync committees only
func (dc *dutyController) warmUpDuties(logger *zap.Logger) {
	indices := dc.validatorController.ActiveValidatorIndices(logger)
	currentEpoch := dc.network.Beacon.EstimatedCurrentEpoch()

	thisSyncCommitteePeriodStartEpoch := dc.network.Beacon.FirstEpochOfSyncPeriod(uint64(currentEpoch) / goclient.EpochsPerSyncCommitteePeriod)
	go dc.scheduleSyncCommitteeMessages(logger, thisSyncCommitteePeriodStartEpoch, indices)

	// next sync committee period.
	// TODO: we should fetch duties for validator indices that became active (active_ongoing) in the next sync committee period as well
	nextSyncCommitteePeriodStartEpoch := dc.network.Beacon.FirstEpochOfSyncPeriod(uint64(currentEpoch)/goclient.EpochsPerSyncCommitteePeriod + 1)
	if uint64(nextSyncCommitteePeriodStartEpoch-currentEpoch) <= syncCommitteePreparationEpochs {
		go dc.scheduleSyncCommitteeMessages(logger, nextSyncCommitteePeriodStartEpoch, indices)
	}
}

// ExecuteDuty tries to execute the given duty
func (dc *dutyController) ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty) error {
	if dc.executor != nil {
		// enables to work with a custom executor, e.g. readOnlyDutyExec
		return dc.executor.ExecuteDuty(logger, duty)
	}

	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	if v, ok := dc.validatorController.GetValidator(hex.EncodeToString(pk[:])); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pk, dc.network.Domain)
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

// HandleHeadEvent handles the "head" events from the beacon node.
func (dc *dutyController) HandleHeadEvent(logger *zap.Logger) func(event *eth2apiv1.Event) {
	return func(event *eth2apiv1.Event) {
		if event.Data == nil {
			return
		}

		var zeroRoot phase0.Root

		data := event.Data.(*eth2apiv1.HeadEvent)
		if data.Slot != dc.network.Beacon.EstimatedCurrentSlot() {
			return
		}

		// check for reorg
		epoch := dc.network.Beacon.EstimatedEpochAtSlot(data.Slot)
		if dc.lastBlockEpoch != 0 && epoch <= dc.lastBlockEpoch {
			if !bytes.Equal(dc.currentDutyDependentRoot[:], zeroRoot[:]) &&
				!bytes.Equal(dc.currentDutyDependentRoot[:], data.CurrentDutyDependentRoot[:]) {
				logger.Debug("Current duty dependent root has changed",
					zap.String("old_dependent_root", fmt.Sprintf("%#x", dc.currentDutyDependentRoot[:])),
					zap.String("new_dependent_root", fmt.Sprintf("%#x", data.CurrentDutyDependentRoot[:])))
				dc.handleCurrentDependentRootChanged(logger)
			}
		}

		dc.lastBlockEpoch = epoch
		dc.currentDutyDependentRoot = data.CurrentDutyDependentRoot
	}
}

// listenToTicker loop over the given slot channel
func (dc *dutyController) listenToTicker(logger *zap.Logger, slots <-chan phase0.Slot) {
	for currentSlot := range slots {
		dc.handleSlot(logger, currentSlot)
	}
}

func (dc *dutyController) handleSlot(logger *zap.Logger, slot phase0.Slot) {
	syncPeriod := uint64(dc.network.Beacon.EstimatedEpochAtSlot(slot)) / goclient.EpochsPerSyncCommitteePeriod
	defer func() {
		if slot == dc.network.Beacon.LastSlotOfSyncPeriod(syncPeriod) {
			// delete duties from map
			dc.syncCommitteeDutiesMap.Del(syncPeriod)
		}
	}()
	// execute duties (attester, proposal)
	duties, err := dc.fetcher.GetDuties(logger, slot)
	if err != nil {
		logger.Warn("failed to get duties", zap.Error(err))
	}
	for i := range duties {
		go dc.onDuty(logger, &duties[i])
	}

	dc.handleSyncCommittee(logger, slot, syncPeriod)
	if dc.builderProposals {
		dc.handleValidatorRegistration(logger, slot)
	}
}

func (dc *dutyController) handleValidatorRegistration(logger *zap.Logger, slot phase0.Slot) {
	shares, err := dc.validatorController.GetOperatorShares(logger)
	if err != nil {
		logger.Warn("failed to get all validators share", zap.Error(err))
		return
	}

	sent := 0
	for _, share := range shares {
		if !share.HasBeaconMetadata() {
			continue
		}

		// if not passed first registration, should be registered within one epoch time in a corresponding slot
		// if passed first registration, should be registered within validatorRegistrationEpochInterval epochs time in a corresponding slot
		registrationSlotInterval := dc.network.SlotsPerEpoch()
		if _, ok := dc.validatorsPassedFirstRegistration[string(share.ValidatorPubKey)]; ok {
			registrationSlotInterval *= validatorRegistrationEpochInterval
		}

		if uint64(share.BeaconMetadata.Index)%registrationSlotInterval != uint64(slot)%registrationSlotInterval {
			continue
		}

		pk := phase0.BLSPubKey{}
		copy(pk[:], share.ValidatorPubKey)

		go dc.onDuty(logger, &spectypes.Duty{
			Type:   spectypes.BNRoleValidatorRegistration,
			PubKey: pk,
			Slot:   slot,
			// no need for other params
		})

		sent++
		dc.validatorsPassedFirstRegistration[string(share.ValidatorPubKey)] = struct{}{}
	}
	logger.Debug("validator registration duties sent", zap.Uint64("slot", uint64(slot)), fields.Count(sent))
}

// handleSyncCommittee preform the following processes -
//  1. execute sync committee duties
//  2. Get next period's sync committee duties, but wait until half-way through the epoch
//     This allows us to set them up at a time when the beacon node should be less busy.
func (dc *dutyController) handleSyncCommittee(logger *zap.Logger, slot phase0.Slot, syncPeriod uint64) {
	// execute sync committee duties
	if syncCommitteeDuties, found := dc.syncCommitteeDutiesMap.Get(syncPeriod); found {
		toSpecDuty := func(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.Duty {
			indices := make([]uint64, len(duty.ValidatorSyncCommitteeIndices))
			for i, index := range duty.ValidatorSyncCommitteeIndices {
				indices[i] = uint64(index)
			}
			return &spectypes.Duty{
				Type:                          role,
				PubKey:                        duty.PubKey,
				Slot:                          slot, // in order for the duty ctrl to execute
				ValidatorIndex:                duty.ValidatorIndex,
				ValidatorSyncCommitteeIndices: indices,
			}
		}
		syncCommitteeDuties.Range(func(index phase0.ValidatorIndex, duty *eth2apiv1.SyncCommitteeDuty) bool {
			go dc.onDuty(logger, toSpecDuty(duty, slot, spectypes.BNRoleSyncCommittee))
			go dc.onDuty(logger, toSpecDuty(duty, slot, spectypes.BNRoleSyncCommitteeContribution))
			return true
		})
	}

	// Get next period's sync committee duties, but wait until half-way through the epoch
	// This allows us to set them up at a time when the beacon node should be less busy.
	if uint64(slot)%dc.network.SlotsPerEpoch() == dc.network.SlotsPerEpoch()/2 {
		currentEpoch := dc.network.Beacon.EstimatedEpochAtSlot(slot)

		// Update the next period if we close to an EPOCHS_PER_SYNC_COMMITTEE_PERIOD boundary.
		if uint64(currentEpoch)%goclient.EpochsPerSyncCommitteePeriod == goclient.EpochsPerSyncCommitteePeriod-syncCommitteePreparationEpochs {
			indices := dc.validatorController.ActiveValidatorIndices(logger)
			go dc.scheduleSyncCommitteeMessages(logger, currentEpoch+phase0.Epoch(syncCommitteePreparationEpochs), indices)
		}
	}
}

func (dc *dutyController) handleCurrentDependentRootChanged(logger *zap.Logger) {
	// We need to refresh the sync committee duties for the next period if we are
	// at the appropriate boundary.
	currentEpoch := dc.network.Beacon.EstimatedCurrentEpoch()
	if uint64(currentEpoch)%goclient.EpochsPerSyncCommitteePeriod == 0 {
		indices := dc.validatorController.ActiveValidatorIndices(logger)
		syncPeriod := dc.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)
		dc.syncCommitteeDutiesMap.Del(syncPeriod)
		go dc.scheduleSyncCommitteeMessages(logger, dc.network.Beacon.EstimatedCurrentEpoch()+phase0.Epoch(syncCommitteePreparationEpochs), indices)
	}
}

// scheduleSyncCommitteeMessages schedules sync committee messages for the given period and validator indices.
func (dc *dutyController) scheduleSyncCommitteeMessages(logger *zap.Logger, epoch phase0.Epoch, indices []phase0.ValidatorIndex) {
	if len(indices) == 0 {
		return
	}

	syncCommitteeDuties, err := dc.fetcher.SyncCommitteeDuties(logger, epoch, indices)
	if err != nil {
		logger.Warn("failed to get sync committee duties", zap.Error(err))
		return
	}

	// populate syncCommitteeDutiesMap
	syncPeriod := dc.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
	periodMap, _ := dc.syncCommitteeDutiesMap.GetOrInsert(syncPeriod, hashmap.New[phase0.ValidatorIndex, *eth2apiv1.SyncCommitteeDuty]())
	for _, duty := range syncCommitteeDuties {
		periodMap.Set(duty.ValidatorIndex, duty)
	}
}

// onDuty handles next duty
func (dc *dutyController) onDuty(logger *zap.Logger, duty *spectypes.Duty) {
	logger = dc.loggerWithDutyContext(logger, duty)
	if dc.shouldExecute(logger, duty) {
		logger.Debug("duty was sent to execution")
		if err := dc.ExecuteDuty(logger, duty); err != nil {
			logger.Warn("could not dispatch duty", zap.Error(err))
			return
		}
		return
	}
	logger.Warn("slot is irrelevant, ignoring duty")
}

func (dc *dutyController) shouldExecute(logger *zap.Logger, duty *spectypes.Duty) bool {
	currentSlot := uint64(dc.network.Beacon.EstimatedCurrentSlot())
	// execute task if slot already began and not pass 1 epoch
	if currentSlot >= uint64(duty.Slot) && currentSlot-uint64(duty.Slot) <= dc.dutyLimit {
		return true
	} else if currentSlot+1 == uint64(duty.Slot) {
		dc.loggerWithDutyContext(logger, duty).Debug("current slot and duty slot are not aligned, "+
			"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", duty.Type.String()))
		return true
	}
	return false
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (dc *dutyController) loggerWithDutyContext(logger *zap.Logger, duty *spectypes.Duty) *zap.Logger {
	return logger.
		With(fields.Role(duty.Type)).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(fields.CurrentSlot(dc.network.Beacon)).
		With(fields.Slot(duty.Slot)).
		With(zap.Uint64("epoch", uint64(duty.Slot)/32)).
		With(fields.PubKey(duty.PubKey[:])).
		With(fields.StartTimeUnixMilli(dc.network.Beacon, duty.Slot))
}

// NewReadOnlyExecutor creates a dummy executor that is used to run in read mode
func NewReadOnlyExecutor() DutyExecutor {
	return &readOnlyDutyExec{}
}

type readOnlyDutyExec struct{}

func (e *readOnlyDutyExec) ExecuteDuty(logger *zap.Logger, duty *spectypes.Duty) error {
	logger.Debug("skipping duty execution",
		zap.Uint64("epoch", uint64(duty.Slot)/32),
		fields.Slot(duty.Slot),
		fields.PubKey(duty.PubKey[:]))
	return nil
}

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
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
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
var validatorRegistrationEpochInterval = uint64(4)

// DutyExecutor represents the component that executes duties
type DutyExecutor interface {
	ExecuteDuty(duty *spectypes.Duty) error
}

// DutyController interface for dispatching duties execution according to slot ticker
type DutyController interface {
	Start()
}

// ControllerOptions holds the needed dependencies
type ControllerOptions struct {
	Logger              *zap.Logger
	Ctx                 context.Context
	BeaconClient        beaconprotocol.Beacon
	EthNetwork          beaconprotocol.Network
	ValidatorController validator.Controller
	Executor            DutyExecutor
	DutyLimit           uint64
	ForkVersion         forksprotocol.ForkVersion
	Ticker              slot_ticker.Ticker
}

// dutyController internal implementation of DutyController
type dutyController struct {
	logger     *zap.Logger
	ctx        context.Context
	ethNetwork beaconprotocol.Network
	// executor enables to work with a custom execution
	executor            DutyExecutor
	fetcher             DutyFetcher
	validatorController validator.Controller
	dutyLimit           uint64
	ticker              slot_ticker.Ticker

	// sync committee duties map [period, map[index, duty]]
	syncCommitteeDutiesMap   *hashmap.Map[uint64, *hashmap.Map[phase0.ValidatorIndex, *eth2apiv1.SyncCommitteeDuty]]
	lastBlockEpoch           phase0.Epoch
	currentDutyDependentRoot phase0.Root
}

var secPerSlot int64 = 12

// NewDutyController creates a new instance of DutyController
func NewDutyController(opts *ControllerOptions) DutyController {
	fetcher := newDutyFetcher(opts.Logger, opts.BeaconClient, opts.ValidatorController, opts.EthNetwork)
	dc := dutyController{
		logger:              opts.Logger,
		ctx:                 opts.Ctx,
		ethNetwork:          opts.EthNetwork,
		fetcher:             fetcher,
		validatorController: opts.ValidatorController,
		dutyLimit:           opts.DutyLimit,
		executor:            opts.Executor,
		ticker:              opts.Ticker,

		syncCommitteeDutiesMap: hashmap.New[uint64, *hashmap.Map[phase0.ValidatorIndex, *eth2apiv1.SyncCommitteeDuty]](),
	}
	return &dc
}

// Start listens to slot ticker and dispatches duties execution
func (dc *dutyController) Start() {
	// warmup
	indices := dc.validatorController.GetValidatorsIndices()
	dc.logger.Debug("warming up indices", zap.Int("count", len(indices)))

	// Subscribe to head events.  This allows us to go early for attestations if a block arrives, as well as
	// re-request duties if there is a change in beacon block.
	// This also allows us to re-request duties if the dependent roots change.
	if err := dc.fetcher.Events(dc.ctx, []string{"head"}, dc.HandleHeadEvent); err != nil {
		dc.logger.Error("failed to subscribe to head events", zap.Error(err))
	}
	dc.warmUpDuties()
	tickerChan := make(chan phase0.Slot, 32)
	dc.ticker.Subscribe(tickerChan)
	dc.listenToTicker(tickerChan)
}

// warmUpDuties preparing duties for all validators when the node starts
// this is done to avoid missing duties for the current epoch when the node starts
// for now we are fetching duties in this way for sync committees only
func (dc *dutyController) warmUpDuties() {
	indices := dc.validatorController.GetValidatorsIndices()
	currentEpoch := dc.ethNetwork.EstimatedCurrentEpoch()

	thisSyncCommitteePeriodStartEpoch := dc.ethNetwork.FirstEpochOfSyncPeriod(uint64(currentEpoch) / goclient.EpochsPerSyncCommitteePeriod)
	go dc.scheduleSyncCommitteeMessages(thisSyncCommitteePeriodStartEpoch, indices)

	// next sync committee period.
	// TODO: we should fetch duties for validator indices that became active (active_ongoing) in the next sync committee period as well
	nextSyncCommitteePeriodStartEpoch := dc.ethNetwork.FirstEpochOfSyncPeriod(uint64(currentEpoch)/goclient.EpochsPerSyncCommitteePeriod + 1)
	if uint64(nextSyncCommitteePeriodStartEpoch-currentEpoch) <= syncCommitteePreparationEpochs {
		go dc.scheduleSyncCommitteeMessages(nextSyncCommitteePeriodStartEpoch, indices)
	}
}

// ExecuteDuty tries to execute the given duty
func (dc *dutyController) ExecuteDuty(duty *spectypes.Duty) error {
	if dc.executor != nil {
		// enables to work with a custom executor, e.g. readOnlyDutyExec
		return dc.executor.ExecuteDuty(duty)
	}
	logger := dc.loggerWithDutyContext(dc.logger, duty)

	// because we're using the same duty for more than 1 duty (e.g. attest + aggregator) there is an error in bls.Deserialize func for cgo pointer to pointer.
	// so we need to copy the pubkey val to avoid pointer
	var pk phase0.BLSPubKey
	copy(pk[:], duty.PubKey[:])

	pubKey := &bls.PublicKey{}
	if err := pubKey.Deserialize(pk[:]); err != nil {
		return errors.Wrap(err, "failed to deserialize pubkey from duty")
	}
	if v, ok := dc.validatorController.GetValidator(pubKey.SerializeToHexStr()); ok {
		ssvMsg, err := CreateDutyExecuteMsg(duty, pubKey)
		if err != nil {
			return err
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			return err
		}
		if pushed := v.Queues[duty.Type].Q.TryPush(dec); !pushed {
			logger.Warn("dropping ExecuteDuty message because the queue is full")
		}
	} else {
		logger.Warn("could not find validator")
	}

	return nil
}

// CreateDutyExecuteMsg returns ssvMsg with event type of duty execute
func CreateDutyExecuteMsg(duty *spectypes.Duty, pubKey *bls.PublicKey) (*spectypes.SSVMessage, error) {
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
		MsgID:   spectypes.NewMsgID(pubKey.Serialize(), duty.Type),
		Data:    data,
	}, nil
}

// HandleHeadEvent handles the "head" events from the beacon node.
func (dc *dutyController) HandleHeadEvent(event *eth2apiv1.Event) {
	if event.Data == nil {
		return
	}

	var zeroRoot phase0.Root

	data := event.Data.(*eth2apiv1.HeadEvent)
	if data.Slot != dc.ethNetwork.EstimatedCurrentSlot() {
		return
	}

	// check for reorg
	epoch := dc.ethNetwork.EstimatedEpochAtSlot(data.Slot)
	if dc.lastBlockEpoch != 0 && epoch <= dc.lastBlockEpoch {
		if !bytes.Equal(dc.currentDutyDependentRoot[:], zeroRoot[:]) &&
			!bytes.Equal(dc.currentDutyDependentRoot[:], data.CurrentDutyDependentRoot[:]) {
			dc.logger.Debug("Current duty dependent root has changed",
				zap.String("old_dependent_root", fmt.Sprintf("%#x", dc.currentDutyDependentRoot[:])),
				zap.String("new_dependent_root", fmt.Sprintf("%#x", data.CurrentDutyDependentRoot[:])))
			dc.handleCurrentDependentRootChanged()
		}
	}

	dc.lastBlockEpoch = epoch
	dc.currentDutyDependentRoot = data.CurrentDutyDependentRoot
}

// listenToTicker loop over the given slot channel
func (dc *dutyController) listenToTicker(slots <-chan phase0.Slot) {
	for currentSlot := range slots {
		dc.handleSlot(currentSlot)
	}
}

func (dc *dutyController) handleSlot(slot phase0.Slot) {
	syncPeriod := uint64(dc.ethNetwork.EstimatedEpochAtSlot(slot)) / goclient.EpochsPerSyncCommitteePeriod
	defer func() {
		if slot == dc.ethNetwork.LastSlotOfSyncPeriod(syncPeriod) {
			// delete duties from map
			dc.syncCommitteeDutiesMap.Del(syncPeriod)
		}
	}()
	// execute duties (attester, proposal)
	duties, err := dc.fetcher.GetDuties(slot)
	if err != nil {
		dc.logger.Warn("failed to get duties", zap.Error(err))
	}
	for i := range duties {
		go dc.onDuty(&duties[i])
	}

	dc.handleSyncCommittee(slot, syncPeriod)
	dc.handleValidatorRegistration(slot)
}

func (dc *dutyController) handleValidatorRegistration(slot phase0.Slot) {
	// push if first time or every 10 epoch at first slot
	epoch := dc.ethNetwork.EstimatedEpochAtSlot(slot)
	firstSlot := dc.ethNetwork.GetEpochFirstSlot(epoch)
	if slot != firstSlot || uint64(epoch)%validatorRegistrationEpochInterval != 0 {
		return
	}
	shares, err := dc.validatorController.GetAllValidatorShares() // TODO better to fetch only active validators
	if err != nil {
		dc.logger.Warn("failed to get all validators share", zap.Error(err))
		return
	}
	for _, share := range shares {
		pk := phase0.BLSPubKey{}
		copy(pk[:], share.ValidatorPubKey)
		go dc.onDuty(&spectypes.Duty{
			Type:   spectypes.BNRoleValidatorRegistration,
			PubKey: pk,
			Slot:   slot,
			// no need for other params
		})
	}
	dc.logger.Debug("validator registration duties sent", zap.Uint64("slot", uint64(slot)))
}

// handleSyncCommittee preform the following processes -
//  1. execute sync committee duties
//  2. Get next period's sync committee duties, but wait until half-way through the epoch
//     This allows us to set them up at a time when the beacon node should be less busy.
func (dc *dutyController) handleSyncCommittee(slot phase0.Slot, syncPeriod uint64) {
	// execute sync committee duties
	if syncCommitteeDuties, found := dc.syncCommitteeDutiesMap.Get(syncPeriod); found {
		toSpecDuty := func(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.Duty {
			return &spectypes.Duty{
				Type:                          role,
				PubKey:                        duty.PubKey,
				Slot:                          slot, // in order for the duty ctrl to execute
				ValidatorIndex:                duty.ValidatorIndex,
				ValidatorSyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
			}
		}
		syncCommitteeDuties.Range(func(index phase0.ValidatorIndex, duty *eth2apiv1.SyncCommitteeDuty) bool {
			go dc.onDuty(toSpecDuty(duty, slot, spectypes.BNRoleSyncCommittee))
			go dc.onDuty(toSpecDuty(duty, slot, spectypes.BNRoleSyncCommitteeContribution))
			return true
		})
	}

	// Get next period's sync committee duties, but wait until half-way through the epoch
	// This allows us to set them up at a time when the beacon node should be less busy.
	if uint64(slot)%dc.ethNetwork.SlotsPerEpoch() == dc.ethNetwork.SlotsPerEpoch()/2 {
		currentEpoch := dc.ethNetwork.EstimatedEpochAtSlot(slot)

		// Update the next period if we close to an EPOCHS_PER_SYNC_COMMITTEE_PERIOD boundary.
		if uint64(currentEpoch)%goclient.EpochsPerSyncCommitteePeriod == goclient.EpochsPerSyncCommitteePeriod-syncCommitteePreparationEpochs {
			indices := dc.validatorController.GetValidatorsIndices()
			go dc.scheduleSyncCommitteeMessages(currentEpoch+phase0.Epoch(syncCommitteePreparationEpochs), indices)
		}
	}
}

func (dc *dutyController) handleCurrentDependentRootChanged() {
	// We need to refresh the sync committee duties for the next period if we are
	// at the appropriate boundary.
	currentEpoch := dc.ethNetwork.EstimatedCurrentEpoch()
	if uint64(currentEpoch)%goclient.EpochsPerSyncCommitteePeriod == 0 {
		indices := dc.validatorController.GetValidatorsIndices()
		syncPeriod := dc.ethNetwork.EstimatedSyncCommitteePeriodAtEpoch(currentEpoch)
		dc.syncCommitteeDutiesMap.Del(syncPeriod)
		go dc.scheduleSyncCommitteeMessages(dc.ethNetwork.EstimatedCurrentEpoch()+phase0.Epoch(syncCommitteePreparationEpochs), indices)
	}
}

// scheduleSyncCommitteeMessages schedules sync committee messages for the given period and validator indices.
func (dc *dutyController) scheduleSyncCommitteeMessages(epoch phase0.Epoch, indices []phase0.ValidatorIndex) {
	if len(indices) == 0 {
		return
	}

	syncCommitteeDuties, err := dc.fetcher.SyncCommitteeDuties(epoch, indices)
	if err != nil {
		dc.logger.Warn("failed to get sync committee duties", zap.Error(err))
		return
	}

	// populate syncCommitteeDutiesMap
	syncPeriod := dc.ethNetwork.EstimatedSyncCommitteePeriodAtEpoch(epoch)
	periodMap, _ := dc.syncCommitteeDutiesMap.GetOrInsert(syncPeriod, hashmap.New[phase0.ValidatorIndex, *eth2apiv1.SyncCommitteeDuty]())
	for _, duty := range syncCommitteeDuties {
		periodMap.Set(duty.ValidatorIndex, duty)
	}
}

// onDuty handles next duty
func (dc *dutyController) onDuty(duty *spectypes.Duty) {
	logger := dc.loggerWithDutyContext(dc.logger, duty)
	if dc.shouldExecute(duty) {
		logger.Debug("duty was sent to execution")
		if err := dc.ExecuteDuty(duty); err != nil {
			logger.Warn("could not dispatch duty", zap.Error(err))
			return
		}
		return
	}
	logger.Warn("slot is irrelevant, ignoring duty")
}

func (dc *dutyController) shouldExecute(duty *spectypes.Duty) bool {
	currentSlot := uint64(dc.ethNetwork.EstimatedCurrentSlot())
	// execute task if slot already began and not pass 1 epoch
	if currentSlot >= uint64(duty.Slot) && currentSlot-uint64(duty.Slot) <= dc.dutyLimit {
		return true
	} else if currentSlot+1 == uint64(duty.Slot) {
		dc.loggerWithDutyContext(dc.logger, duty).Debug("current slot and duty slot are not aligned, "+
			"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", duty.Type.String()))
		return true
	}
	return false
}

// loggerWithDutyContext returns an instance of logger with the given duty's information
func (dc *dutyController) loggerWithDutyContext(logger *zap.Logger, duty *spectypes.Duty) *zap.Logger {
	currentSlot := uint64(dc.ethNetwork.EstimatedCurrentSlot())
	return logger.
		With(zap.String("role", duty.Type.String())).
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(zap.Uint64("current slot", currentSlot)).
		With(zap.Uint64("slot", uint64(duty.Slot))).
		With(zap.Uint64("epoch", uint64(duty.Slot)/32)).
		With(zap.String("pubKey", hex.EncodeToString(duty.PubKey[:]))).
		With(zap.Time("start_time", dc.ethNetwork.GetSlotStartTime(duty.Slot)))
}

// NewReadOnlyExecutor creates a dummy executor that is used to run in read mode
func NewReadOnlyExecutor(logger *zap.Logger) DutyExecutor {
	return &readOnlyDutyExec{logger: logger}
}

type readOnlyDutyExec struct {
	logger *zap.Logger
}

func (e *readOnlyDutyExec) ExecuteDuty(duty *spectypes.Duty) error {
	e.logger.Debug("skipping duty execution",
		zap.Uint64("epoch", uint64(duty.Slot)/32),
		zap.Uint64("slot", uint64(duty.Slot)),
		zap.String("pubKey", hex.EncodeToString(duty.PubKey[:])))
	return nil
}

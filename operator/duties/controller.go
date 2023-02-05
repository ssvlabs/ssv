package duties

import (
	"context"
	"encoding/hex"
	"encoding/json"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
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
	dutyMap             *hashmap.Map[phase0.Slot, []*spectypes.Duty]
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
		//dutyMap:             make(map[phase0.Slot][]*spectypes.Duty),
		dutyMap: hashmap.New[phase0.Slot, []*spectypes.Duty](),
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
	if err := dc.fetcher.Events([]string{"head"}, dc.HandleHeadEvent); err != nil {
		dc.logger.Error("failed to subscribe to head events", zap.Error(err))
	}
	tickerChan := make(chan phase0.Slot, 32)
	dc.initDuties()
	dc.ticker.Subscribe(tickerChan)
	dc.listenToTicker(tickerChan)
}

func (dc *dutyController) initDuties() {
	indices := dc.validatorController.GetValidatorsIndices()
	currentEpoch := dc.ethNetwork.EstimatedCurrentEpoch()

	thisSyncCommitteePeriodStartEpoch := phase0.Epoch((uint64(currentEpoch) / goclient.EpochsPerSyncCommitteePeriod) * goclient.EpochsPerSyncCommitteePeriod)
	go dc.scheduleSyncCommitteeMessages(thisSyncCommitteePeriodStartEpoch, indices)
	nextSyncCommitteePeriodStartEpoch := phase0.Epoch((uint64(currentEpoch)/goclient.EpochsPerSyncCommitteePeriod + 1) * goclient.EpochsPerSyncCommitteePeriod)
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
		ssvMsg, err := createDutyExecuteMsg(duty, pubKey)
		if err != nil {
			return err
		}
		dec, err := queue.DecodeSSVMessage(ssvMsg)
		if err != nil {
			return err
		}
		v.Queues[duty.Type].Q.Push(dec)
	} else {
		logger.Warn("could not find validator")
	}

	return nil
}

// createDutyExecuteMsg returns ssvMsg with event type of duty execute
func createDutyExecuteMsg(duty *spectypes.Duty, pubKey *bls.PublicKey) (*spectypes.SSVMessage, error) {
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
func (dc *dutyController) HandleHeadEvent(event *apiv1.Event) {
	if event.Data == nil {
		return
	}

	data := event.Data.(*apiv1.HeadEvent)
	dc.logger.Debug("received head event", zap.Uint64("slot", uint64(data.Slot)))
	if data.Slot != dc.ethNetwork.EstimatedCurrentSlot() {
		return
	}

	//// check if slot in dc.dutyMap[data.Slot] is not nil
	//if duties := dc.dutyMap[data.Slot]; duties != nil {
	//	for i := range duties {
	//		go dc.onDuty(duties[i])
	//	}
	//}

}

// listenToTicker loop over the given slot channel
func (dc *dutyController) listenToTicker(slots <-chan phase0.Slot) {
	for currentSlot := range slots {
		// check if first slot of epoch
		if dc.ethNetwork.IsFirstSlotOfEpoch(currentSlot) {
			dc.logger.Debug("first slot of epoch", zap.Uint64("slot", uint64(currentSlot)))
			currentEpoch := dc.ethNetwork.EstimatedEpochAtSlot(currentSlot)
			indices := dc.validatorController.GetValidatorsIndices()
			//go dc.scheduleProposals(currentEpoch, indices)

			// Update the _next_ period if we close to an EPOCHS_PER_SYNC_COMMITTEE_PERIOD boundary.
			if uint64(currentEpoch)%goclient.EpochsPerSyncCommitteePeriod == goclient.EpochsPerSyncCommitteePeriod-syncCommitteePreparationEpochs {
				go dc.scheduleSyncCommitteeMessages(currentEpoch+phase0.Epoch(syncCommitteePreparationEpochs), indices)
			}
		}

		//// determine half epoch
		//if uint64(currentSlot)%dc.ethNetwork.SlotsPerEpoch() == dc.ethNetwork.SlotsPerEpoch()/2 {
		//	dc.logger.Debug("half epoch", zap.Uint64("slot", uint64(currentSlot)))
		//	// prepare for next epoch duties
		//	// schedule attestations
		//	dc.prepareForEpoch(dc.ctx, dc.ethNetwork.EstimatedCurrentEpoch()+1)
		//}

		// execute duties
		duties, err := dc.fetcher.GetDuties(currentSlot)
		if err != nil {
			dc.logger.Warn("failed to get duties", zap.Error(err))
		}
		for i := range duties {
			dc.logger.Debug("executing duty", zap.Uint64("slot", uint64(currentSlot)),
				zap.String("pubKey", duties[i].PubKey.String()), zap.String("role", duties[i].Type.String()))
			go dc.onDuty(&duties[i])
		}

		// execute sync committee duties
		syncCommitteeDuties, found := dc.dutyMap.Get(currentSlot)
		if found {
			for _, duty := range syncCommitteeDuties {
				go dc.onDuty(duty)
			}
		}

		// delete duties from map
		dc.dutyMap.Del(currentSlot - 2)
	}
}

// prepareForEpoch fetches duties for the given epoch
func (dc *dutyController) prepareForEpoch(ctx context.Context, epoch phase0.Epoch) {
	dc.logger.Debug("preparing for epoch", zap.Uint64("epoch", uint64(epoch)))
	// TODO: get indices by Epoch
	indices := dc.validatorController.GetValidatorsIndices()
	go dc.scheduleAttestations(epoch, indices)
	// TODO: SubscribeToCommitteeSubnet subscribe to beacon committees
}

// scheduleAttestations schedules attestations for the given epoch and validator indices.
func (dc *dutyController) scheduleAttestations(epoch phase0.Epoch, indices []phase0.ValidatorIndex) {
	if len(indices) == 0 {
		// no validators
		return
	}

	// TODO: Sort the response by slot, then committee index, then validator index.
	attesterDuties, err := dc.fetcher.AttesterDuties(epoch, indices)
	if err != nil {
		return
	}
	// populate dc.dutyMap
	for _, duty := range attesterDuties {
		duties, found := dc.dutyMap.Get(duty.Slot)
		if !found {
			dc.dutyMap.Set(duty.Slot, []*spectypes.Duty{})
		}
		dc.dutyMap.Set(duty.Slot, append(duties, duty))
	}
	// TODO: handle Aggregate
}

// scheduleProposals schedules proposals for the given epoch and validator indices.
func (dc *dutyController) scheduleProposals(epoch phase0.Epoch, indices []phase0.ValidatorIndex) {
	dc.logger.Debug("scheduling proposals", zap.Uint64("epoch", uint64(epoch)), zap.Int("validators", len(indices)))
	if len(indices) == 0 {
		// no validators
		return
	}

	proposerDuties, err := dc.fetcher.ProposerDuties(epoch, indices)
	if err != nil {
		return
	}
	// populate dc.dutyMap
	for _, duty := range proposerDuties {
		duties, found := dc.dutyMap.Get(duty.Slot)
		if !found {
			dc.dutyMap.Set(duty.Slot, []*spectypes.Duty{})
		}
		dc.dutyMap.Set(duty.Slot, append(duties, duty))
	}
}

// scheduleSyncCommitteeMessages schedules sync committee messages for the given period and validator indices.
func (dc *dutyController) scheduleSyncCommitteeMessages(epoch phase0.Epoch, indices []phase0.ValidatorIndex) {
	if len(indices) == 0 {
		// no validators
		return
	}

	syncCommitteeDuties, err := dc.fetcher.SyncCommitteeDuties(epoch, indices)
	if err != nil {
		return
	}
	dc.logger.Debug("sync committee duties", zap.Uint64("epoch", uint64(epoch)), zap.Int("duties", len(syncCommitteeDuties)))

	// populate dc.dutyMap
	for _, duty := range syncCommitteeDuties {
		duties, found := dc.dutyMap.Get(duty.Slot)
		if !found {
			dc.dutyMap.Set(duty.Slot, []*spectypes.Duty{})
		}
		dc.dutyMap.Set(duty.Slot, append(duties, duty))
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

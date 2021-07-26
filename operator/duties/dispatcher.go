package duties

import (
	"encoding/hex"
	"github.com/bloxapp/eth2-key-manager/core"
	"github.com/bloxapp/ssv/beacon"
	"github.com/prysmaticlabs/prysm/shared/slotutil"
	"go.uber.org/zap"
	"time"
)

// DutyExecutor represents the component that executes duties
type DutyExecutor interface {
	ExecuteDuty(duty *beacon.Duty) error
}

// DutyDispatcher interface for dispatching duties execution according to slot ticker
type DutyDispatcher interface {
	ListenToTicker()
	LoggerWithDutyContext(logger *zap.Logger, duty *beacon.Duty) *zap.Logger
}

// dutyDispatcher internal implementation of DutyDispatcher
type dutyDispatcher struct {
	logger       *zap.Logger
	ethNetwork   core.Network
	executor     DutyExecutor
	fetcher      DutyFetcher
	genesisEpoch uint64
	dutyLimit    uint64
}

var secPerSlot int64 = 12

// NewDutyDispatcher creates a new instance of DutyDispatcher
func NewDutyDispatcher(logger *zap.Logger, ethNetwork core.Network, executor DutyExecutor, fetcher DutyFetcher,
	genesisEpoch uint64, dutyLimit uint64) DutyDispatcher {
	ds := dutyDispatcher{logger: logger, ethNetwork: ethNetwork, executor: executor, fetcher: fetcher,
		genesisEpoch: genesisEpoch, dutyLimit: dutyLimit}
	return &ds
}

// ListenToTicker listens to slot ticker and dispatches duties execution
// make sure to warmup indices (updating internal map) by validator controller
func (d *dutyDispatcher) ListenToTicker() {
	genesisTime := time.Unix(int64(d.ethNetwork.MinGenesisTime()), 0)
	slotTicker := slotutil.GetSlotTicker(genesisTime, uint64(d.ethNetwork.SlotDurationSec().Seconds()))
	d.listenToTicker(slotTicker.C())
}

func (d *dutyDispatcher) listenToTicker(slots <-chan uint64) {
	for currentSlot := range slots {
		d.logger.Debug("slot ticker", zap.Uint64("slot", currentSlot))
		duties, err := d.fetcher.GetDuties(currentSlot)
		if err != nil {
			d.logger.Error("failed to get duties", zap.Error(err))
		}
		for i := range duties {
			go func(duty *beacon.Duty) {
				if err := d.dispatch(duty); err != nil {
					d.LoggerWithDutyContext(d.logger, duty).Error("could not dispatch duty", zap.Error(err))
				}
			}(&duties[i])
		}
	}
}

func (d *dutyDispatcher) dispatch(duty *beacon.Duty) error {
	if uint64(duty.Slot) < d.getEpochFirstSlot(d.genesisEpoch) {
		// wait until genesis epoch starts
		d.logger.Debug("skipping slot, lower than genesis",
			zap.Uint64("genesis_slot", d.getEpochFirstSlot(d.genesisEpoch)),
			zap.Uint64("slot", uint64(duty.Slot)))
		return nil
	}

	logger := d.LoggerWithDutyContext(d.logger, duty)
	currentSlot := uint64(d.getCurrentSlot())
	// execute task if slot already began and not pass 1 epoch
	if currentSlot >= uint64(duty.Slot) && currentSlot-uint64(duty.Slot) <= d.dutyLimit {
		logger.Debug("executing duty")
		return d.executor.ExecuteDuty(duty)
	}
	logger.Warn("slot is irrelevant, ignoring duty")
	return nil
}

// LoggerWithDutyContext returns an instance of logger with the given duty's information
func (d *dutyDispatcher) LoggerWithDutyContext(logger *zap.Logger, duty *beacon.Duty) *zap.Logger {
	currentSlot := uint64(d.getCurrentSlot())
	return logger.
		With(zap.Uint64("committee_index", uint64(duty.CommitteeIndex))).
		With(zap.Uint64("current slot", currentSlot)).
		With(zap.Uint64("slot", uint64(duty.Slot))).
		With(zap.Uint64("epoch", uint64(duty.Slot)/32)).
		With(zap.String("pubKey", hex.EncodeToString(duty.PubKey[:]))).
		With(zap.Time("start_time", d.getSlotStartTime(uint64(duty.Slot))))
}

// getSlotStartTime returns the start time for the given slot
func (d *dutyDispatcher) getSlotStartTime(slot uint64) time.Time {
	timeSinceGenesisStart := slot * uint64(d.ethNetwork.SlotDurationSec().Seconds())
	start := time.Unix(int64(d.ethNetwork.MinGenesisTime()+timeSinceGenesisStart), 0)
	return start
}

// getCurrentSlot returns the current beacon node slot
func (d *dutyDispatcher) getCurrentSlot() int64 {
	genesisTime := int64(d.ethNetwork.MinGenesisTime())
	currentTime := time.Now().Unix()
	return (currentTime - genesisTime) / secPerSlot
}

// getEpochFirstSlot returns the beacon node first slot in epoch
func (d *dutyDispatcher) getEpochFirstSlot(epoch uint64) uint64 {
	return epoch * 32
}

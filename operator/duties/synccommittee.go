package duties

import (
	"context"
	"fmt"
	"strings"
	"time"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/beacon/goclient"
	"github.com/bloxapp/ssv/logging/fields"
)

// syncCommitteePreparationEpochs is the number of epochs ahead of the sync committee
// period change at which to prepare the relevant duties.
var syncCommitteePreparationEpochs = uint64(2)

type SyncCommitteeHandler struct {
	baseHandler

	duties             *SyncCommitteeDuties[uint64, *eth2apiv1.SyncCommitteeDuty]
	fetchCurrentPeriod bool
	fetchNextPeriod    bool
}

func NewSyncCommitteeHandler() *SyncCommitteeHandler {
	return &SyncCommitteeHandler{
		duties: NewSyncCommitteeDuties[uint64, *eth2apiv1.SyncCommitteeDuty](),
	}
}

func (h *SyncCommitteeHandler) Name() string {
	return "SYNC_COMMITTEE"
}

type SyncCommitteeDuties[K ~uint64, D any] struct {
	m map[K][]D
}

func NewSyncCommitteeDuties[K ~uint64, D any]() *SyncCommitteeDuties[K, D] {
	return &SyncCommitteeDuties[K, D]{
		m: make(map[K][]D),
	}
}

func (d *SyncCommitteeDuties[K, D]) Add(key K, duty D) {
	if _, ok := d.m[key]; !ok {
		d.m[key] = []D{}
	}
	d.m[key] = append(d.m[key], duty)
}

func (d *SyncCommitteeDuties[K, D]) Reset(key K) {
	if _, ok := d.m[key]; ok {
		d.m[key] = []D{}
	}
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. Fetch duties for the current period.
//  2. If necessary, fetch duties for the next period.
//  3. Execute duties.
//
// On Re-org:
//  1. Execute duties.
//  2. If necessary, fetch duties for the next period.
//
// On Indices Change:
//  1. Execute duties.
//  2. Reset duties for the current period.
//  3. Fetch duties for the current period.
//  4. If necessary, fetch duties for the next period.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the next period.
func (h *SyncCommitteeHandler) HandleDuties(ctx context.Context, logger *zap.Logger) {
	logger = logger.With(zap.String("handler", h.Name()))
	logger.Info("starting duty handler")

	h.fetchCurrentPeriod = true
	currentSlot := h.network.Beacon.EstimatedCurrentSlot()
	currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(currentSlot)
	if h.shouldFetchNextPeriod(currentSlot, currentEpoch) {
		h.fetchNextPeriod = true
	}
	h.fetchFirst = true

	for {
		select {
		case slot := <-h.ticker:
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)

			if h.fetchFirst {
				h.processFetching(ctx, logger, period)
				h.fetchFirst = false
				h.processExecution(logger, period, slot)
			} else {
				h.processExecution(logger, period, slot)
				if h.indicesChanged {
					h.duties.Reset(period)
					h.indicesChanged = false
				}
				h.processFetching(ctx, logger, period)
			}

			// Get next period's sync committee duties, but wait until half-way through the epoch
			// This allows us to set them up at a time when the beacon node should be less busy.
			if uint64(slot)%h.network.Beacon.SlotsPerEpoch() == h.network.Beacon.SlotsPerEpoch()/2-2 &&
				// Update the next period if we close to an EPOCHS_PER_SYNC_COMMITTEE_PERIOD boundary.
				uint64(epoch)%goclient.EpochsPerSyncCommitteePeriod == goclient.EpochsPerSyncCommitteePeriod-syncCommitteePreparationEpochs {
				h.fetchNextPeriod = true
			}

			// last slot of period
			if slot == h.network.Beacon.LastSlotOfSyncPeriod(period) {
				h.duties.Reset(period)
			}

		case reorgEvent := <-h.reorg:
			epoch := h.network.Beacon.EstimatedEpochAtSlot(reorgEvent.Slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)

			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			logger.Info("🔀 reorg event received", zap.String("period_epoch_slot_sequence", buildStr), zap.Any("event", reorgEvent))

			// reset current epoch duties
			if reorgEvent.Current && h.shouldFetchNextPeriod(reorgEvent.Slot, epoch) {
				h.duties.Reset(period + 1)
				h.fetchNextPeriod = true
			}

		case <-h.indicesChange:
			slot := h.network.Beacon.EstimatedCurrentSlot()
			epoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			period := h.network.Beacon.EstimatedSyncCommitteePeriodAtEpoch(epoch)
			buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, epoch, slot, slot%32+1)
			logger.Info("🔁 indices change received", zap.String("period_epoch_slot_sequence", buildStr))

			h.indicesChanged = true
			h.fetchCurrentPeriod = true

			// reset next period duties if in appropriate slot range
			if h.shouldFetchNextPeriod(slot, epoch) {
				h.duties.Reset(period + 1)
				h.fetchNextPeriod = true
			}
		}
	}
}

func (h *SyncCommitteeHandler) processFetching(ctx context.Context, logger *zap.Logger, period uint64) {
	if h.fetchCurrentPeriod {
		if err := h.fetchDuties(ctx, logger, period); err != nil {
			logger.Error("failed to fetch duties for current epoch", zap.Error(err))
			return
		}
		h.fetchCurrentPeriod = false
	}

	if h.fetchNextPeriod {
		if err := h.fetchDuties(ctx, logger, period+1); err != nil {
			logger.Error("failed to fetch duties for next epoch", zap.Error(err))
			return
		}
		h.fetchNextPeriod = false
	}
}

func (h *SyncCommitteeHandler) processExecution(logger *zap.Logger, period uint64, slot phase0.Slot) {
	//currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
	//buildStr := fmt.Sprintf("p%v-e%v-s%v-#%v", period, currentEpoch, slot, slot%32+1)
	//logger.Debug("🛠 process execution", zap.String("period_epoch_slot_sequence", buildStr))

	// range over duties and execute
	if duties, ok := h.duties.m[period]; ok {
		toExecute := make([]*spectypes.Duty, 0, len(duties)*2)
		for _, d := range duties {
			if h.shouldExecute(logger, d) {
				toExecute = append(toExecute, h.toSpecDuty(d, slot, spectypes.BNRoleSyncCommittee))
				toExecute = append(toExecute, h.toSpecDuty(d, slot, spectypes.BNRoleSyncCommitteeContribution))
			}
		}
		h.executeDuties(logger, toExecute)
	}
}

func (h *SyncCommitteeHandler) fetchDuties(ctx context.Context, logger *zap.Logger, period uint64) error {
	start := time.Now()
	firstEpoch := h.network.Beacon.FirstEpochOfSyncPeriod(period)
	currentEpoch := h.network.Beacon.EstimatedCurrentEpoch()
	if firstEpoch < currentEpoch {
		firstEpoch = currentEpoch
	}
	lastEpoch := h.network.Beacon.FirstEpochOfSyncPeriod(period+1) - 1

	indices := h.indicesFetcher.ActiveIndices(logger, firstEpoch)
	duties, err := h.beaconNode.SyncCommitteeDuties(ctx, firstEpoch, indices)
	if err != nil {
		return errors.Wrap(err, "failed to fetch syncCommittee duties")
	}

	for _, d := range duties {
		h.duties.Add(period, d)
	}

	h.prepareDutiesResultLog(logger, period, firstEpoch, lastEpoch, duties, start)

	// lastEpoch + 1 due to the fact that we need to subscribe "until" the end of the period
	syncCommitteeSubscriptions := calculateSubscriptions(lastEpoch+1, duties)
	if len(syncCommitteeSubscriptions) > 0 {
		if err := h.beaconNode.SubmitSyncCommitteeSubscriptions(syncCommitteeSubscriptions); err != nil {
			logger.Warn("failed to subscribe sync committee to subnet", zap.Error(err))
		}
	}

	return nil
}

func (h *SyncCommitteeHandler) prepareDutiesResultLog(logger *zap.Logger, period uint64, firstEpoch, lastEpoch phase0.Epoch, duties []*eth2apiv1.SyncCommitteeDuty, start time.Time) {
	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		tmp := fmt.Sprintf("%v-p%v-e%v-e%v-v%v", h.Name(), period, firstEpoch, lastEpoch, duty.ValidatorIndex)
		b.WriteString(tmp)
	}
	logger.Debug("👥 got duties",
		zap.Int("count", len(duties)),
		zap.Any("duties", b.String()),
		fields.Duration(start))
}

func (h *SyncCommitteeHandler) toSpecDuty(duty *eth2apiv1.SyncCommitteeDuty, slot phase0.Slot, role spectypes.BeaconRole) *spectypes.Duty {
	indices := make([]uint64, len(duty.ValidatorSyncCommitteeIndices))
	for i, index := range duty.ValidatorSyncCommitteeIndices {
		indices[i] = uint64(index)
	}
	return &spectypes.Duty{
		Type:                          role,
		PubKey:                        duty.PubKey,
		Slot:                          slot, // in order for the duty scheduler to execute
		ValidatorIndex:                duty.ValidatorIndex,
		ValidatorSyncCommitteeIndices: indices,
	}
}

func (h *SyncCommitteeHandler) shouldExecute(logger *zap.Logger, duty *eth2apiv1.SyncCommitteeDuty) bool {
	return true
}

// calculateSubscriptions calculates the sync committee subscriptions given a set of duties.
func calculateSubscriptions(endEpoch phase0.Epoch, duties []*eth2apiv1.SyncCommitteeDuty) []*eth2apiv1.SyncCommitteeSubscription {
	subscriptions := make([]*eth2apiv1.SyncCommitteeSubscription, 0, len(duties))
	for _, duty := range duties {
		subscriptions = append(subscriptions, &eth2apiv1.SyncCommitteeSubscription{
			ValidatorIndex:       duty.ValidatorIndex,
			SyncCommitteeIndices: duty.ValidatorSyncCommitteeIndices,
			UntilEpoch:           endEpoch,
		})
	}

	return subscriptions
}

func (h *SyncCommitteeHandler) shouldFetchNextPeriod(slot phase0.Slot, epoch phase0.Epoch) bool {
	return uint64(slot)%h.network.SlotsPerEpoch() >= h.network.SlotsPerEpoch()/2-1 &&
		uint64(epoch)%goclient.EpochsPerSyncCommitteePeriod >= goclient.EpochsPerSyncCommitteePeriod-syncCommitteePreparationEpochs
}

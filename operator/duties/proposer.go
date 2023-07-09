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

	"github.com/bloxapp/ssv/logging/fields"
)

type ProposerHandler struct {
	baseHandler

	duties *Duties[phase0.Epoch, *eth2apiv1.ProposerDuty]
}

func NewProposerHandler() *ProposerHandler {
	return &ProposerHandler{
		duties: NewDuties[phase0.Epoch, *eth2apiv1.ProposerDuty](),
	}
}

func (h *ProposerHandler) Name() string {
	return spectypes.BNRoleProposer.String()
}

// HandleDuties manages the duty lifecycle, handling different cases:
//
// On First Run:
//  1. Fetch duties for the current epoch.
//  2. Execute duties.
//
// On Re-org (current dependent root changed):
//  1. Fetch duties for the current epoch.
//  2. Execute duties.
//
// On Indices Change:
//  1. Execute duties.
//  2. Reset duties for the current epoch.
//  3. Fetch duties for the current epoch.
//
// On Ticker event:
//  1. Execute duties.
//  2. If necessary, fetch duties for the current epoch.
func (h *ProposerHandler) HandleDuties(ctx context.Context, logger *zap.Logger) {
	logger = logger.With(zap.String("handler", h.Name()))
	logger.Info("starting duty handler")

	h.fetchFirst = true

	for {
		select {
		case slot := <-h.ticker:
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(slot)

			if h.fetchFirst {
				h.processFetching(ctx, logger, currentEpoch)
				h.fetchFirst = false
				h.processExecution(logger, currentEpoch, slot)
			} else {
				h.processExecution(logger, currentEpoch, slot)
				if h.indicesChanged {
					h.duties.Reset(currentEpoch)
					h.indicesChanged = false
					h.processFetching(ctx, logger, currentEpoch)
				}
			}

			// last slot of epoch
			if uint64(slot)%h.network.Beacon.SlotsPerEpoch() == h.network.Beacon.SlotsPerEpoch()-1 {
				h.duties.Reset(currentEpoch)
				h.fetchFirst = true
			}

		case reorgEvent := <-h.reorg:
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(reorgEvent.Slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, reorgEvent.Slot, reorgEvent.Slot%32+1)
			logger.Info("🔀 reorg event received", zap.String("epoch_slot_sequence", buildStr), zap.Any("event", reorgEvent))

			// reset current epoch duties
			if reorgEvent.Current {
				h.duties.Reset(currentEpoch)
				h.fetchFirst = true
			}

		case <-h.indicesChange:
			slot := h.network.Beacon.EstimatedCurrentSlot()
			currentEpoch := h.network.Beacon.EstimatedEpochAtSlot(slot)
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, slot, slot%32+1)
			logger.Info("🔁 indices change received", zap.String("epoch_slot_sequence", buildStr))

			h.indicesChanged = true
		}
	}
}

func (h *ProposerHandler) processFetching(ctx context.Context, logger *zap.Logger, epoch phase0.Epoch) {
	if err := h.fetchDuties(ctx, logger, epoch); err != nil {
		logger.Error("failed to fetch duties for current epoch", zap.Error(err))
		return
	}
}

func (h *ProposerHandler) processExecution(logger *zap.Logger, epoch phase0.Epoch, slot phase0.Slot) {
	//buildStr := fmt.Sprintf("e%v-s%v-#%v", epoch, slot, slot%32+1)
	//logger.Debug("🛠️process execution", zap.String("epoch_slot_sequence", buildStr))

	// range over duties and execute
	if slotMap, ok := h.duties.m[epoch]; ok {
		if duties, ok := slotMap[slot]; ok {
			toExecute := make([]*spectypes.Duty, 0, len(duties)*2)
			for _, d := range duties {
				if h.shouldExecute(logger, d) {
					toExecute = append(toExecute, h.toSpecDuty(d, spectypes.BNRoleProposer))
				}
			}
			h.executeDuties(logger, toExecute)
		}
	}
}

func (h *ProposerHandler) fetchDuties(ctx context.Context, logger *zap.Logger, epoch phase0.Epoch) error {
	start := time.Now()
	indices := h.validatorController.ActiveIndices(logger, epoch)
	duties, err := h.beaconNode.ProposerDuties(ctx, epoch, indices)
	if err != nil {
		return errors.Wrap(err, "failed to fetch proposer duties")
	}

	for _, d := range duties {
		h.duties.Add(epoch, d.Slot, d)
	}
	h.prepareDutiesResultLog(logger, epoch, duties, start)

	return nil
}

func (h *ProposerHandler) prepareDutiesResultLog(logger *zap.Logger, epoch phase0.Epoch, duties []*eth2apiv1.ProposerDuty, start time.Time) {
	var b strings.Builder
	for i, duty := range duties {
		if i > 0 {
			b.WriteString(", ")
		}
		tmp := fmt.Sprintf("%v-e%v-s%v-v%v", h.Name(), epoch, duty.Slot, duty.ValidatorIndex)
		b.WriteString(tmp)
	}
	logger.Debug("📚 got duties",
		zap.Int("count", len(duties)),
		fields.Epoch(epoch),
		zap.Any("duties", b.String()),
		fields.Duration(start))
}

func (h *ProposerHandler) toSpecDuty(duty *eth2apiv1.ProposerDuty, role spectypes.BeaconRole) *spectypes.Duty {
	return &spectypes.Duty{
		Type:           role,
		PubKey:         duty.PubKey,
		Slot:           duty.Slot,
		ValidatorIndex: duty.ValidatorIndex,
	}
}

func (h *ProposerHandler) shouldExecute(logger *zap.Logger, duty *eth2apiv1.ProposerDuty) bool {
	currentSlot := h.network.Beacon.EstimatedCurrentSlot()
	// execute task if slot already began and not pass 1 epoch
	if currentSlot == duty.Slot {
		return true
	} else if currentSlot+1 == duty.Slot {
		logger.Debug("current slot and duty slot are not aligned, "+
			"assuming diff caused by a time drift - ignoring and executing duty", zap.String("type", duty.String()))
		return true
	}
	return false
}

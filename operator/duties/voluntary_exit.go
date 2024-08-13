package duties

import (
	"context"
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

const voluntaryExitSlotsToPostpone = phase0.Slot(4)

type ExitDescriptor struct {
	OwnValidator   bool
	PubKey         phase0.BLSPubKey
	ValidatorIndex phase0.ValidatorIndex
	BlockNumber    uint64
}

type VoluntaryExitHandler struct {
	baseHandler
	duties          *dutystore.VoluntaryExitDuties
	validatorExitCh <-chan ExitDescriptor
	dutyQueue       []*spectypes.ValidatorDuty
	blockSlots      map[uint64]phase0.Slot
}

func NewVoluntaryExitHandler(duties *dutystore.VoluntaryExitDuties, validatorExitCh <-chan ExitDescriptor) *VoluntaryExitHandler {
	return &VoluntaryExitHandler{
		duties:          duties,
		validatorExitCh: validatorExitCh,
		dutyQueue:       make([]*spectypes.ValidatorDuty, 0),
		blockSlots:      map[uint64]phase0.Slot{},
	}
}

func (h *VoluntaryExitHandler) Name() string {
	return spectypes.BNRoleVoluntaryExit.String()
}

func (h *VoluntaryExitHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			currentSlot := h.ticker.Slot()
			next = h.ticker.Next()

			h.logger.Debug("🛠 ticker event", fields.Slot(currentSlot))
			h.processExecution(currentSlot)

		case exitDescriptor, ok := <-h.validatorExitCh:
			if !ok {
				return
			}

			blockSlot, err := h.blockSlot(ctx, exitDescriptor.BlockNumber)
			if err != nil {
				h.logger.Warn("failed to get block time from execution client, skipping voluntary exit duty",
					zap.Error(err))
				continue
			}

			dutySlot := blockSlot + voluntaryExitSlotsToPostpone

			duty := &spectypes.ValidatorDuty{
				Type:           spectypes.BNRoleVoluntaryExit,
				PubKey:         exitDescriptor.PubKey,
				Slot:           dutySlot,
				ValidatorIndex: exitDescriptor.ValidatorIndex,
			}

			h.duties.AddDuty(dutySlot, exitDescriptor.PubKey)
			if !exitDescriptor.OwnValidator {
				continue
			}

			h.dutyQueue = append(h.dutyQueue, duty)

			h.logger.Debug("🛠 scheduled duty for execution",
				zap.Uint64("block_slot", uint64(blockSlot)),
				zap.Uint64("duty_slot", uint64(dutySlot)),
				fields.BlockNumber(exitDescriptor.BlockNumber),
			)

		case <-h.indicesChange:
			continue

		case <-h.reorg:
			continue
		}
	}
}

func (h *VoluntaryExitHandler) processExecution(slot phase0.Slot) {
	var dutiesForExecution, pendingDuties []*spectypes.ValidatorDuty

	for _, duty := range h.dutyQueue {
		if duty.Slot <= slot {
			dutiesForExecution = append(dutiesForExecution, duty)
		} else {
			pendingDuties = append(pendingDuties, duty)
		}
	}

	h.dutyQueue = pendingDuties
	h.duties.RemoveSlot(slot - phase0.Slot(h.network.SlotsPerEpoch()))

	if !h.network.PastAlanForkAtEpoch(h.network.Beacon.EstimatedEpochAtSlot(slot)) {
		toExecute := make([]*genesisspectypes.Duty, 0, len(dutiesForExecution))
		for _, d := range dutiesForExecution {
			toExecute = append(toExecute, h.toGenesisSpecDuty(d, genesisspectypes.BNRoleVoluntaryExit))
		}

		if dutyCount := len(dutiesForExecution); dutyCount != 0 {
			h.dutiesExecutor.ExecuteGenesisDuties(h.logger, toExecute)
			h.logger.Debug("executed voluntary exit duties",
				fields.Slot(slot),
				fields.Count(dutyCount))
		}
		return
	}

	if dutyCount := len(dutiesForExecution); dutyCount != 0 {
		h.dutiesExecutor.ExecuteDuties(h.logger, dutiesForExecution)
		h.logger.Debug("executed voluntary exit duties",
			fields.Slot(slot),
			fields.Count(dutyCount))
	}
}

func (h *VoluntaryExitHandler) toGenesisSpecDuty(duty *spectypes.ValidatorDuty, role genesisspectypes.BeaconRole) *genesisspectypes.Duty {
	return &genesisspectypes.Duty{
		Type:           role,
		PubKey:         duty.PubKey,
		Slot:           duty.Slot,
		ValidatorIndex: duty.ValidatorIndex,
	}
}

// blockSlot gets slots happened at the same time as block,
// it prevents calling execution client multiple times if there are several validator exit events on the same block
func (h *VoluntaryExitHandler) blockSlot(ctx context.Context, blockNumber uint64) (phase0.Slot, error) {
	blockSlot, ok := h.blockSlots[blockNumber]
	if ok {
		return blockSlot, nil
	}

	block, err := h.executionClient.BlockByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return 0, err
	}

	blockSlot = h.network.Beacon.EstimatedSlotAtTime(int64(block.Time()))

	h.blockSlots[blockNumber] = blockSlot
	for k, v := range h.blockSlots {
		if v < blockSlot && blockSlot-v >= voluntaryExitSlotsToPostpone {
			delete(h.blockSlots, k)
		}
	}

	return blockSlot, nil
}

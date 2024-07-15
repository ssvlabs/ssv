package duties

import (
	"context"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

const voluntaryExitSlotsToPostpone = phase0.Slot(4)

type ExitDescriptor struct {
	PubKey         phase0.BLSPubKey
	ValidatorIndex phase0.ValidatorIndex
	BlockNumber    uint64
}

type VoluntaryExitHandler struct {
	baseHandler
	duties          *dutystore.VoluntaryExitDuties
	validatorExitCh <-chan ExitDescriptor
	dutyQueue       []*spectypes.BeaconDuty
	blockSlots      map[uint64]phase0.Slot
}

func NewVoluntaryExitHandler(duties *dutystore.VoluntaryExitDuties, validatorExitCh <-chan ExitDescriptor) *VoluntaryExitHandler {
	return &VoluntaryExitHandler{
		duties:          duties,
		validatorExitCh: validatorExitCh,
		dutyQueue:       make([]*spectypes.BeaconDuty, 0),
		blockSlots:      map[uint64]phase0.Slot{},
	}
}

func (h *VoluntaryExitHandler) Name() string {
	return spectypes.BNRoleVoluntaryExit.String()
}

func (h *VoluntaryExitHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	for {
		select {
		case <-ctx.Done():
			return

		case <-h.ticker.Next():
			currentSlot := h.ticker.Slot()

			h.logger.Debug("🛠 ticker event", fields.Slot(currentSlot))

			var dutiesForExecution, pendingDuties []*spectypes.BeaconDuty

			for _, duty := range h.dutyQueue {
				if duty.Slot <= currentSlot {
					dutiesForExecution = append(dutiesForExecution, duty)
				} else {
					pendingDuties = append(pendingDuties, duty)
				}
			}

			h.dutyQueue = pendingDuties

			if dutyCount := len(dutiesForExecution); dutyCount != 0 {
				h.dutiesExecutor.ExecuteDuties(h.logger, dutiesForExecution)
				h.logger.Debug("executed voluntary exit duties",
					fields.Slot(currentSlot),
					fields.Count(dutyCount))
			}

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

			duty := &spectypes.BeaconDuty{
				Type:           spectypes.BNRoleVoluntaryExit,
				PubKey:         exitDescriptor.PubKey,
				Slot:           dutySlot,
				ValidatorIndex: exitDescriptor.ValidatorIndex,
			}

			h.duties.AddDuty(dutySlot, exitDescriptor.PubKey)
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

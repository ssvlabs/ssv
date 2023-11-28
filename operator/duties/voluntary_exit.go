package duties

import (
	"context"
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
)

const voluntaryExitSlotsToPostpone = phase0.Slot(4)

type ExitDescriptor struct {
	PubKey         phase0.BLSPubKey
	ValidatorIndex phase0.ValidatorIndex
	BlockNumber    uint64
}

type VoluntaryExitHandler struct {
	baseHandler
	validatorExitCh <-chan ExitDescriptor
	dutyQueue       []*spectypes.Duty
	blockSlots      map[uint64]phase0.Slot
}

func NewVoluntaryExitHandler(validatorExitCh <-chan ExitDescriptor) *VoluntaryExitHandler {
	return &VoluntaryExitHandler{
		validatorExitCh: validatorExitCh,
		dutyQueue:       make([]*spectypes.Duty, 0),
		blockSlots:      map[uint64]phase0.Slot{},
	}
}

func (h *VoluntaryExitHandler) Name() string {
	return spectypes.BNRoleVoluntaryExit.String()
}

func (h *VoluntaryExitHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")

	for {
		select {
		case <-ctx.Done():
			return

		case <-h.ticker.Next():
			currentSlot := h.ticker.Slot()

			h.logger.Debug("🛠 ticker event", fields.Slot(currentSlot))

			var dutiesForExecution, pendingDuties []*spectypes.Duty

			for _, duty := range h.dutyQueue {
				if duty.Slot <= currentSlot {
					dutiesForExecution = append(dutiesForExecution, duty)
				} else {
					pendingDuties = append(pendingDuties, duty)
				}
			}

			h.dutyQueue = pendingDuties

			if dutyCount := len(dutiesForExecution); dutyCount != 0 {
				h.executeDuties(h.logger, dutiesForExecution)
				h.logger.Debug("executed voluntary exit duties",
					fields.Slot(currentSlot),
					fields.Count(dutyCount))
			}

		case exitDescriptor, ok := <-h.validatorExitCh:
			if !ok {
				return
			}

			blockSlot, ok := h.blockSlots[exitDescriptor.BlockNumber]
			if !ok {
				block, err := h.executionClient.BlockByNumber(ctx, new(big.Int).SetUint64(exitDescriptor.BlockNumber))
				if err != nil {
					h.logger.Warn("failed to get block time from execution client, skipping voluntary exit duty",
						zap.Error(err))
					continue
				}

				blockSlot = h.network.Beacon.EstimatedSlotAtTime(int64(block.Time()))

				h.blockSlots[exitDescriptor.BlockNumber] = blockSlot
				for k, v := range h.blockSlots {
					if v < blockSlot-voluntaryExitSlotsToPostpone {
						delete(h.blockSlots, k)
					}
				}
			}

			dutySlot := blockSlot + voluntaryExitSlotsToPostpone

			duty := &spectypes.Duty{
				Type:           spectypes.BNRoleVoluntaryExit,
				PubKey:         exitDescriptor.PubKey,
				Slot:           dutySlot,
				ValidatorIndex: exitDescriptor.ValidatorIndex,
			}

			h.dutyQueue = append(h.dutyQueue, duty)

			h.logger.Debug("🛠 scheduled duty for execution",
				zap.Uint64("block_slot", uint64(blockSlot)),
				zap.Uint64("duty_slot", uint64(dutySlot)),
				fields.BlockNumber(exitDescriptor.BlockNumber),
			)
		}
	}
}

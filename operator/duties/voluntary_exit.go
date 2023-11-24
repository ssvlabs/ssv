package duties

import (
	"context"
	"sync"

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
	dutyQueueMu     sync.Mutex
}

func NewVoluntaryExitHandler(validatorExitCh <-chan ExitDescriptor) *VoluntaryExitHandler {
	return &VoluntaryExitHandler{
		validatorExitCh: validatorExitCh,
		dutyQueue:       make([]*spectypes.Duty, 0),
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

			var dutiesForExecution []*spectypes.Duty

			h.dutyQueueMu.Lock()
			for _, duty := range h.dutyQueue {
				if duty.Slot > currentSlot {
					break
				}

				dutiesForExecution = append(dutiesForExecution, duty)
			}

			dutyCount := len(dutiesForExecution)
			h.dutyQueue = h.dutyQueue[dutyCount:]
			h.dutyQueueMu.Unlock()

			if dutyCount != 0 {
				h.executeDuties(h.logger, dutiesForExecution)
				h.logger.Debug("executed voluntary exit duties",
					fields.Slot(currentSlot),
					fields.Count(dutyCount))
			}

		case exitDescriptor := <-h.validatorExitCh:
			time, err := h.executionClient.BlockTime(ctx, exitDescriptor.BlockNumber)
			if err != nil {
				h.logger.Warn("failed to get block time from execution client, skipping voluntary exit duty",
					zap.Error(err))
				continue
			}

			slot := h.network.Beacon.EstimatedSlotAtTime(time.Unix()) + voluntaryExitSlotsToPostpone

			duty := &spectypes.Duty{
				Type:           spectypes.BNRoleVoluntaryExit,
				PubKey:         exitDescriptor.PubKey,
				Slot:           slot,
				ValidatorIndex: exitDescriptor.ValidatorIndex,
			}

			h.dutyQueueMu.Lock()
			h.dutyQueue = append(h.dutyQueue, duty)
			h.dutyQueueMu.Unlock()

			h.logger.Debug("🛠 scheduled duty for execution", fields.Slot(duty.Slot))
		}
	}
}

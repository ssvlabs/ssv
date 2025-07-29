package duties

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

// voluntaryExitSlotsToPostpone defines how many slots we want to wait out before
// executing voluntary exit duty.
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
			h.processExecution(ctx, currentSlot)

		case exitDescriptor, ok := <-h.validatorExitCh:
			if !ok {
				return
			}

			// Calculate duty slot in a deterministic manner to ensure every Operator will have the same
			// slot value for this duty. Additionally, add validatorRegistrationSlotsToPostpone slots on
			// top to ensure the duty is scheduled with a slot number never in the past since several slots
			// might have passed by the time we are processing this event here.
			blockSlot, err := h.blockSlot(ctx, exitDescriptor.BlockNumber)
			if err != nil {
				h.logger.Warn(
					"failed to convert block number to slot number, skipping voluntary exit duty",
					zap.Error(err),
				)
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
			h.logger.Debug("🛠 indicesChange event")

		case <-h.reorg:
			h.logger.Debug("🛠 reorg event")
		}
	}
}

func (h *VoluntaryExitHandler) processExecution(ctx context.Context, slot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "voluntary_exit.execute"),
		trace.WithAttributes(observability.BeaconSlotAttribute(slot)))
	defer span.End()

	var dutiesForExecution, pendingDuties []*spectypes.ValidatorDuty

	for _, duty := range h.dutyQueue {
		if duty.Slot <= slot {
			dutiesForExecution = append(dutiesForExecution, duty)
		} else {
			pendingDuties = append(pendingDuties, duty)
		}
	}

	h.dutyQueue = pendingDuties
	h.duties.RemoveSlot(slot - phase0.Slot(h.beaconConfig.GetSlotsPerEpoch()))

	span.SetAttributes(observability.DutyCountAttribute(len(dutiesForExecution)))
	if dutyCount := len(dutiesForExecution); dutyCount != 0 {
		h.dutiesExecutor.ExecuteDuties(ctx, dutiesForExecution)
		h.logger.Debug("executed voluntary exit duties",
			fields.Slot(slot),
			fields.Count(dutyCount))
	}

	span.SetStatus(codes.Ok, "")
}

// blockSlot returns slot that happens (corresponds to) at the same time as block.
// It caches the result to avoid calling execution client multiple times when there are several
// validator exit events present in the same block.
func (h *VoluntaryExitHandler) blockSlot(ctx context.Context, blockNumber uint64) (phase0.Slot, error) {
	blockSlot, ok := h.blockSlots[blockNumber]
	if ok {
		return blockSlot, nil
	}

	header, err := h.executionClient.HeaderByNumber(ctx, new(big.Int).SetUint64(blockNumber))
	if err != nil {
		return 0, fmt.Errorf("request block %d from execution client: %w", blockNumber, err)
	}

	blockSlot = h.beaconConfig.EstimatedSlotAtTime(time.Unix(int64(header.Time), 0)) // #nosec G115

	h.blockSlots[blockNumber] = blockSlot

	// Clean up older cached values since they are not relevant anymore.
	for k, v := range h.blockSlots {
		const recentlyQueriedBlocks = 10
		if blockSlot >= v+recentlyQueriedBlocks {
			delete(h.blockSlots, k)
		}
	}

	return blockSlot, nil
}

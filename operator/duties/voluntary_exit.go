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

	"github.com/ssvlabs/ssv/eth/executionclient"
	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
)

const (
	// voluntaryExitSchedulingSlack absorbs per-operator timing variance once an
	// exit event clears the execution-layer follow distance. Different operators
	// observe the event at slightly different wall-clock times due to EL client
	// differences, head-subscription latency, FilterLogs round-trip, and the
	// phase of the local slot ticker relative to event delivery. The slack gives
	// every operator enough time to compute the same dutySlot and have it still
	// be in the future when their scheduler picks it up, so they all execute on
	// the same target slot rather than firing on the next tick after receipt.
	voluntaryExitSchedulingSlack = phase0.Slot(4)

	// voluntaryExitSlotsToPostpone is the offset added to an exit event's block
	// slot to derive the scheduled duty slot. It must exceed the EL streaming
	// pipeline's worst-case latency from event production to handler delivery,
	// which is dominated by executionclient.FollowDistance (the EL log stream
	// only surfaces an event once the chain head reaches blockSlot+FollowDistance);
	// the remainder is the slack above. If this value were smaller than any
	// operator's effective EL streaming lag, that operator would schedule the
	// duty in the past on receipt and fire immediately, defeating the cross-
	// operator coordination that downstream pre-consensus signing depends on.
	voluntaryExitSlotsToPostpone = phase0.Slot(executionclient.FollowDistance) + voluntaryExitSchedulingSlack
)

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

func (h *VoluntaryExitHandler) WaitShutdown() {}

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
			currentEpoch := h.beaconConfig.EstimatedEpochAtSlot(currentSlot)

			slotNumber := uint64(currentSlot)%h.beaconConfig.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)
			h.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			func() {
				// tickCtx ensures we never take too long to process ticks (otherwise we might not be able to catch up
				// with the latest tick for a while, if ever). Since the ticker always fires at around slot start-time,
				// setting the deadline to currentSlot+1 gives us about ~1 full slot (12s) to process the tick.
				tickCtx, cancel := context.WithDeadline(ctx, h.beaconConfig.SlotStartTime(currentSlot+1))
				defer cancel()

				h.processExecution(tickCtx, currentSlot)
			}()

		case exitDescriptor, ok := <-h.validatorExitCh:
			if !ok {
				return
			}

			// Derive dutySlot deterministically from the EL event's block slot so
			// every operator arrives at the same value regardless of when they
			// personally received the event. This matters because the runner
			// derives VoluntaryExit.Epoch from dutySlot (see
			// VoluntaryExitRunner.calculateVoluntaryExit), and operators sign over
			// the resulting VoluntaryExit object — divergent slots would produce
			// different epochs near an epoch boundary, breaking BLS partial-
			// signature aggregation and silently failing the exit.
			//
			// voluntaryExitSlotsToPostpone keeps that shared slot in the future
			// for every operator: see its docstring for the breakdown.
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

		case <-h.indicesChangeCh:
			h.logger.Debug("🛠 indicesChange event")

		case <-h.reorgEventsCh:
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
	h.duties.RemoveSlot(slot - phase0.Slot(h.beaconConfig.SlotsPerEpoch))

	span.SetAttributes(observability.DutyCountAttribute(len(dutiesForExecution)))
	if dutyCount := len(dutiesForExecution); dutyCount != 0 {
		h.dutiesExecutor.ExecuteDuties(ctx, dutiesForExecution, h.dutyExecutionDeadline(slot))
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

func (h *VoluntaryExitHandler) dutyExecutionDeadline(slot phase0.Slot) time.Time {
	// 1 slot of time since the target slot should be sufficient for this duty-type.
	dutyDeadline := h.beaconConfig.SlotStartTime(slot + 1)
	return dutyDeadline
}

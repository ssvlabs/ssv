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
	// voluntaryExitDutySlotsToPostpone is the offset added to an exit event's
	// block slot to derive duty.Slot — the shared coordination slot used as
	// the dutyStore key (inbound dutyCount validation), as the outbound
	// partial-sig envelope Slot, and as input to the signed VoluntaryExit.Epoch
	// (see VoluntaryExitRunner.calculateVoluntaryExit). Every operator must
	// compute the same value for partial-sigs to validate, route, and
	// aggregate, so it's part of the wire format.
	//
	// Kept at 4 — the value used by pre-#2851 operators — so upgraded nodes
	// remain backwards-compatible in mixed clusters. Do NOT change it without
	// a coordinated network-wide upgrade.
	//
	// This is NOT when this operator broadcasts its own partial-sig; see
	// voluntaryExitExecutionSlotsToPostpone for that.
	voluntaryExitDutySlotsToPostpone = 4

	// voluntaryExitSchedulingSlack absorbs per-operator timing variance once an
	// exit event clears the execution-layer follow distance. Different operators
	// observe the event at slightly different wall-clock times due to EL client
	// differences, head-subscription latency, FilterLogs round-trip, and the
	// phase of the local slot ticker relative to event delivery. The slack
	// ensures that by the time the earliest operator broadcasts its partial-sig
	// (at voluntaryExitExecutionSlotsToPostpone), every other operator has had
	// time to receive the EL event and register the duty in its local dutyStore
	// — which the inbound message-validation path checks via dutyCount.
	//
	// Independent of voluntaryExitDutySlotsToPostpone despite happening to
	// share the same numeric value (4).
	voluntaryExitSchedulingSlack = 4

	// voluntaryExitExecutionSlotsToPostpone is the earliest slot, expressed as
	// an offset from the exit event's block slot, at which this operator may
	// broadcast its own partial-sig. It is a *local-only* scheduling decision:
	// it does not appear on the wire and need not match across operators.
	//
	// The value must exceed the worst-case EL streaming latency from event
	// production to handler delivery, which is dominated by
	// executionclient.FollowDistance (the EL log stream only surfaces an event
	// once the chain head reaches blockSlot + FollowDistance); the remainder is
	// voluntaryExitSchedulingSlack. Broadcasting earlier than this risks racing
	// peers whose EL streaming pipeline hasn't yet delivered the event: their
	// dutyStore returns 0 for the (slot, pk) key, so the dutyCount check fails
	// and our message is dropped (ValidationIgnore on ErrTooManyDutiesPerEpoch).
	voluntaryExitExecutionSlotsToPostpone = executionclient.FollowDistance + voluntaryExitSchedulingSlack
)

type ExitDescriptor struct {
	OwnValidator   bool
	PubKey         phase0.BLSPubKey
	ValidatorIndex phase0.ValidatorIndex
	BlockNumber    uint64
}

// queuedExit holds an own-validator exit duty awaiting its local execution
// gate. duty.Slot is the shared duty slot (voluntaryExitDutySlotsToPostpone);
// earliestExecutionSlot is when this operator may broadcast its partial-sig
// (voluntaryExitExecutionSlotsToPostpone).
type queuedExit struct {
	duty                  *spectypes.ValidatorDuty
	earliestExecutionSlot phase0.Slot
}

type VoluntaryExitHandler struct {
	baseHandler
	duties          *dutystore.VoluntaryExitDuties
	validatorExitCh <-chan ExitDescriptor
	dutyQueue       []*queuedExit
	blockSlots      map[uint64]phase0.Slot
}

func NewVoluntaryExitHandler(duties *dutystore.VoluntaryExitDuties, validatorExitCh <-chan ExitDescriptor) *VoluntaryExitHandler {
	return &VoluntaryExitHandler{
		duties:          duties,
		validatorExitCh: validatorExitCh,
		dutyQueue:       make([]*queuedExit, 0),
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
			currentEpoch := h.netCfg.EstimatedEpochAtSlot(currentSlot)

			slotNumber := uint64(currentSlot)%h.netCfg.SlotsPerEpoch + 1
			buildStr := fmt.Sprintf("e%v-s%v-#%v", currentEpoch, currentSlot, slotNumber)
			h.logger.Debug("🛠 ticker event", zap.String("epoch_slot_pos", buildStr))

			func() {
				// tickCtx ensures we never take too long to process ticks (otherwise we might not be able to catch up
				// with the latest tick for a while, if ever). Since the ticker always fires at around slot start-time,
				// setting the deadline to currentSlot+1 gives us about ~1 full slot (12s) to process the tick.
				tickCtx, cancel := context.WithDeadline(ctx, h.netCfg.SlotStartTime(currentSlot+1))
				defer cancel()

				h.processExecution(tickCtx, currentSlot)
			}()

		case exitDescriptor, ok := <-h.validatorExitCh:
			if !ok {
				return
			}

			// Derive dutySlot deterministically from the EL event's block slot so
			// every operator arrives at the same value regardless of receipt time
			// or code version; earliestExecutionSlot is a separate, local-only gate
			// deferring our own broadcast until peers' EL pipelines have plausibly
			// caught up. See both constants' docstrings for the full rationale.
			blockSlot, err := h.blockSlot(ctx, exitDescriptor.BlockNumber)
			if err != nil {
				h.logger.Warn(
					"failed to convert block number to slot number, skipping voluntary exit duty",
					zap.Error(err),
				)
				continue
			}
			dutySlot := blockSlot + voluntaryExitDutySlotsToPostpone
			earliestExecutionSlot := blockSlot + voluntaryExitExecutionSlotsToPostpone

			duty := &spectypes.ValidatorDuty{
				Type:           spectypes.BNRoleVoluntaryExit,
				PubKey:         exitDescriptor.PubKey,
				Slot:           dutySlot,
				ValidatorIndex: exitDescriptor.ValidatorIndex,
			}

			// Register the duty even for validators that are not ours: inbound p2p message validation consults this
			// store (dutyCount), so without this our node would ignore a p2p message for this validator (and refuse
			// to relay it).
			h.duties.AddDuty(dutySlot, exitDescriptor.PubKey)
			if !exitDescriptor.OwnValidator {
				continue
			}

			h.dutyQueue = append(h.dutyQueue, &queuedExit{
				duty:                  duty,
				earliestExecutionSlot: earliestExecutionSlot,
			})

			h.logger.Debug("🛠 scheduled duty for execution",
				zap.Uint64("block_slot", uint64(blockSlot)),
				zap.Uint64("duty_slot", uint64(dutySlot)),
				zap.Uint64("earliest_execution_slot", uint64(earliestExecutionSlot)),
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

	dutiesForExecution := make([]*spectypes.ValidatorDuty, 0, len(h.dutyQueue))
	pendingItems := make([]*queuedExit, 0, len(h.dutyQueue))

	for _, item := range h.dutyQueue {
		if item.earliestExecutionSlot <= slot {
			dutiesForExecution = append(dutiesForExecution, item.duty)
		} else {
			pendingItems = append(pendingItems, item)
		}
	}

	h.dutyQueue = pendingItems
	h.duties.RemoveSlot(slot - phase0.Slot(h.netCfg.SlotsPerEpoch))

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

	blockSlot = h.netCfg.EstimatedSlotAtTime(time.Unix(int64(header.Time), 0)) // #nosec G115

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
	// 1 wall-clock slot from execution should be sufficient for this duty-type.
	dutyDeadline := h.netCfg.SlotStartTime(slot + 1)
	return dutyDeadline
}

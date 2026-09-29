package duties

import (
	"context"
	"encoding/hex"
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
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/qa/faults"
)

const (
	// frequencyEpochs defines how frequently we want to submit validator-registrations.
	frequencyEpochs = 10

	// validatorRegistrationDutySlotsToPostpone is the offset added to an EL
	// registration event's block slot to derive duty.Slot — the shared
	// coordination slot used as the outbound partial-sig envelope Slot and as
	// input to the signed ValidatorRegistration.Timestamp's epoch (see
	// ValidatorRegistrationRunner.buildValidatorRegistration). Every operator
	// must compute the same value for partial-sigs to validate, route, and
	// aggregate, so it's part of the wire format. Timestamp is epoch-granular,
	// so divergence only matters across an epoch boundary — but the safe
	// stance is identical.
	//
	// Kept at 4 since the constant was introduced — changing it would shift
	// the signed Timestamp's epoch relative to peers on prior code. Do NOT
	// change it without a coordinated network-wide upgrade.
	//
	// This is NOT when this operator broadcasts its own partial-sig; see
	// validatorRegistrationExecutionSlotsToPostpone for that.
	validatorRegistrationDutySlotsToPostpone = 4

	// validatorRegistrationSchedulingSlack absorbs per-operator timing
	// variance once a registration event clears the EL follow distance — see
	// voluntaryExitSchedulingSlack for the full rationale; the same race
	// applies here at the receiver's duty queue, which holds a partial for a
	// not-yet-started duty only until its next duty starts. Independent of
	// validatorRegistrationDutySlotsToPostpone despite happening to share the
	// same numeric value (4).
	validatorRegistrationSchedulingSlack = 4

	// validatorRegistrationExecutionSlotsToPostpone is the earliest slot,
	// expressed as an offset from the registration event's block slot, at
	// which this operator may broadcast its own partial-sig for an
	// event-driven registration. It is a *local-only* scheduling decision and
	// does not appear on the wire.
	//
	// Unlike voluntary-exit, validator-registration's inbound validation does
	// not lock to a per-slot dutyStore key (the dutyLimit is a constant 2);
	// the failure mode if we broadcast too early is that the receiver's duty
	// queue holds our partial until its own duty starts, and drops it as a
	// past slot if that duty is for a later slot. The periodic VRSubmitter
	// loop will eventually resubmit anyway — but the gate mirrors
	// voluntary-exit's pattern for consistency and narrows that race for
	// slow-EL peers.
	validatorRegistrationExecutionSlotsToPostpone = executionclient.FollowDistance + validatorRegistrationSchedulingSlack
)

type RegistrationDescriptor struct {
	ValidatorIndex  phase0.ValidatorIndex
	ValidatorPubkey phase0.BLSPubKey
	FeeRecipient    []byte
	BlockNumber     uint64
}

// queuedRegistration holds an event-driven validator-registration duty
// awaiting its local execution gate. duty.Slot is the shared duty slot
// (validatorRegistrationDutySlotsToPostpone); earliestExecutionSlot is when
// this operator may broadcast its partial-sig
// (validatorRegistrationExecutionSlotsToPostpone).
type queuedRegistration struct {
	duty                  *spectypes.ValidatorDuty
	earliestExecutionSlot phase0.Slot
}

type ValidatorRegistrationHandler struct {
	baseHandler
	validatorRegCh <-chan RegistrationDescriptor
	blockSlots     map[uint64]phase0.Slot
	// eventQueue holds event-driven registrations awaiting their per-event
	// execution gate. Periodic registrations bypass this queue and fire
	// directly on the originating ticker, since their slot is the current
	// slot and there's no race to defer past.
	eventQueue []*queuedRegistration
}

func NewValidatorRegistrationHandler(validatorRegistrationCh <-chan RegistrationDescriptor) *ValidatorRegistrationHandler {
	return &ValidatorRegistrationHandler{
		validatorRegCh: validatorRegistrationCh,
		blockSlots:     map[uint64]phase0.Slot{},
		eventQueue:     make([]*queuedRegistration, 0),
	}
}

func (h *ValidatorRegistrationHandler) Name() string {
	return spectypes.BNRoleValidatorRegistration.String()
}

func (h *ValidatorRegistrationHandler) WaitShutdown() {}

// HandleDuties generates registration duties every N epochs for every participating validator, then
// validator-registrations are aggregated into batches and sent periodically to Beacon node by
// ValidatorRegistrationRunner (sending validator-registrations periodically ensures various
// entities in Ethereum network, such as Relays, are aware of participating validators).
func (h *ValidatorRegistrationHandler) HandleDuties(ctx context.Context) {
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

				h.processExecution(tickCtx, currentEpoch, currentSlot)
			}()

		case regDescriptor, ok := <-h.validatorRegCh:
			if !ok {
				return
			}

			// dutySlot is the deterministic wire slot; earliestExecutionSlot is a
			// separate, local-only broadcast gate. See both constants' docstrings
			// for the full rationale.
			blockSlot, err := h.blockSlot(ctx, regDescriptor.BlockNumber)
			if err != nil {
				h.logger.Warn(
					"failed to convert block number to slot number, skipping validator registration duty",
					zap.Error(err),
				)
				continue
			}
			dutySlot := blockSlot + validatorRegistrationDutySlotsToPostpone
			// Deprecated at the Gloas fork: don't enqueue registrations whose duty slot is Gloas-or-later.
			if gloasVRDeprecated(h.netCfg.IsGloasAtSlot(dutySlot)) {
				continue
			}
			earliestExecutionSlot := blockSlot + validatorRegistrationExecutionSlotsToPostpone

			// No de-dup on enqueue: entries are idempotent and bounded. The duty
			// carries no fee recipient (the runner reads the current one at
			// execution), so entries sharing a (ValidatorIndex, dutySlot) are
			// byte-identical; downstream they're bounded by the receiver's
			// dutyLimit=2/epoch and the periodic VRSubmitter, and the queue drains
			// every slot (the producer also throttles per owner). Any future
			// de-dup MUST key on (ValidatorIndex, dutySlot), never ValidatorIndex
			// alone — collapsing across blocks would diverge dutySlot across
			// operators and break partial-sig aggregation.
			h.eventQueue = append(h.eventQueue, &queuedRegistration{
				duty: &spectypes.ValidatorDuty{
					Type:           spectypes.BNRoleValidatorRegistration,
					ValidatorIndex: regDescriptor.ValidatorIndex,
					PubKey:         regDescriptor.ValidatorPubkey,
					Slot:           dutySlot,
				},
				earliestExecutionSlot: earliestExecutionSlot,
			})
			h.logger.Debug("🛠 scheduled validator registration duty for execution",
				zap.Uint64("block_slot", uint64(blockSlot)),
				zap.Uint64("duty_slot", uint64(dutySlot)),
				zap.Uint64("earliest_execution_slot", uint64(earliestExecutionSlot)),
				zap.Uint64("validator_index", uint64(regDescriptor.ValidatorIndex)),
				zap.String("validator_pubkey", regDescriptor.ValidatorPubkey.String()),
				zap.String("validator_fee_recipient", hex.EncodeToString(regDescriptor.FeeRecipient)))

		case <-h.indicesChangeCh:
			h.logger.Debug("🛠 indicesChange event")

		case <-h.reorgEventsCh:
			h.logger.Debug("🛠 reorg event")
		}
	}
}

// gloasVRDeprecated reports whether the validator-registration heartbeat must stop, given whether
// the slot or epoch in question is Gloas. Validator registration is deprecated from the Gloas fork
// (SIP #94 §5): fee recipient and gas limit travel in the proposer preferences instead. FOUR gates
// must all agree to stop it — the next reader touching any one of them should check the others:
//  1. HandleDuties (this file) — the event-driven enqueue gate, keyed on the duty slot.
//  2. processExecution (this file) — the periodic scheduling gate, keyed on the current epoch.
//  3. ValidatorRegistrationRunner.StartNewDuty
//     (protocol/v2/ssv/runner/validator_registration.go) — the runner-side belt matching
//     ssv-spec, keyed on the duty slot, before the duty starts.
//  4. VRSubmitter.start (same runner file) — BN submission of registrations reconstructed after a
//     pre-consensus quorum, keyed on the current epoch. DOWNSTREAM of the wire: it neither
//     schedules duties nor broadcasts partials.
//
// Gates 3 and 4 live in a different package, so they mirror this function's fault check
// (vrDeprecatedAtSlot) rather than calling it.
//
// QA fault menu MSG-02 (vr-postfork) needs gates 1-3 suppressed at once to keep role-4 messages
// reaching the wire past the fork — the honest operators must refuse them there. Missing any one of
// them keeps the heartbeat scheduled but throws the message away before the wire, which looks like
// a fired fault with no wire evidence behind it. Gate 4 is suppressed too, for consistency only:
// honest operators reject role 4 post-fork, so the faulted node never reconstructs a quorum and
// gate 4 has nothing to submit. The cadence under the fault is set by vrPostForkCadenceShare.
func gloasVRDeprecated(isGloas bool) bool {
	if faults.Is(faults.VRPostFork) {
		return false
	}
	return isGloas
}

// vrPostForkCadenceShare returns the single self share that QA fault vr-postfork registers at a
// Gloas epoch, or nil (stock behaviour) when the fault is off or the epoch is pre-Gloas.
//
// Why (decision D4, 2026-09-28): the stock periodic schedule fires each validator once per
// SlotsPerEpoch*frequencyEpochs = 320 slots (~64 min), at slot%320 == validatorIndex%320, which
// can fall wholly outside a 2-epoch M3 window — the likely cause of 0 vr-postfork injections in
// the 2026-09 run (errata #137 blamed VRSubmitter.start, which is downstream of the wire and so
// could not have held them at 0). Under the fault the modulus becomes SlotsPerEpoch, so the chosen
// validator fires once every epoch. Only the lowest-index attesting share is used, to keep the
// honest operators' REJECT volume low (#139) and to stay within the receivers' dutyLimit of 2.
func vrPostForkCadenceShare(isGloas bool, shares []*types.SSVShare, epoch phase0.Epoch) *types.SSVShare {
	if !isGloas || !faults.Is(faults.VRPostFork) {
		return nil
	}
	var chosen *types.SSVShare
	for _, share := range shares {
		if !share.IsAttesting(epoch + phase0.Epoch(frequencyEpochs)) {
			continue
		}
		if chosen == nil || share.ValidatorIndex < chosen.ValidatorIndex {
			chosen = share
		}
	}
	return chosen
}

func (h *ValidatorRegistrationHandler) processExecution(ctx context.Context, epoch phase0.Epoch, slot phase0.Slot) {
	ctx, span := tracer.Start(ctx,
		observability.InstrumentName(observabilityNamespace, "validator_registration.execute"),
		trace.WithAttributes(observability.BeaconSlotAttribute(slot)))
	defer span.End()

	// Validator registration is deprecated at the Gloas fork — superseded by proposer preferences (§5).
	// Drop any entries that didn't drain before the fork; nothing more is enqueued past it.
	//
	// QA fault menu MSG-02 (vr-postfork): faults.Fired for this fault is emitted from
	// ValidatorRegistrationRunner.executeDuty (protocol/v2/ssv/runner/validator_registration.go),
	// once the role-4 message is signed and handed to the network — not here, where the duty is
	// merely scheduled. A fired-here line would outrun the wire: the runner has its own copy of this
	// gate (see gloasVRDeprecated's doc comment) and used to still throw the message away even when
	// the scheduler let it through, so the honest side's silence proved nothing. Note the broadcast
	// is asynchronous, so even the runner's Fired does not prove the message left the node: without
	// FAULT_NO_LOCAL_VALIDATION the sender-side evidence is this node's own
	// "own outbound message rejected by local validation" Warn.
	if gloasVRDeprecated(h.netCfg.IsGloas(epoch)) {
		h.eventQueue = nil
		return
	}

	shares := h.validatorProvider.SelfValidators()
	duties := make([]*spectypes.ValidatorDuty, 0, len(h.eventQueue)+len(shares))

	// Drain the event-driven queue.
	pendingItems := make([]*queuedRegistration, 0, len(h.eventQueue))
	for _, item := range h.eventQueue {
		if item.earliestExecutionSlot <= slot {
			duties = append(duties, item.duty)
		} else {
			pendingItems = append(pendingItems, item)
		}
	}
	eventDrivenDispatched := len(duties)
	h.eventQueue = pendingItems

	// validator should be registered within frequencyEpochs epochs time in a corresponding slot
	registrationSlots := h.netCfg.SlotsPerEpoch * frequencyEpochs

	// QA fault menu MSG-02 (vr-postfork), decision D4: at Gloas epochs register ONE validator per
	// epoch (see vrPostForkCadenceShare). nil on every other path, so FAULT=none is unchanged.
	qaShare := vrPostForkCadenceShare(h.netCfg.IsGloas(epoch), shares, epoch)
	if qaShare != nil {
		registrationSlots = h.netCfg.SlotsPerEpoch
	}

	for _, share := range shares {
		if !share.IsAttesting(epoch + phase0.Epoch(frequencyEpochs)) {
			// Only attesting validators are eligible for registration duties.
			continue
		}
		if qaShare != nil && share != qaShare {
			continue
		}
		if uint64(share.ValidatorIndex)%registrationSlots != uint64(slot)%registrationSlots {
			continue
		}

		pk := phase0.BLSPubKey(share.ValidatorPubKey)

		duties = append(duties, &spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleValidatorRegistration,
			ValidatorIndex: share.ValidatorIndex,
			PubKey:         pk,
			Slot:           slot,
		})

		h.logger.Debug("validator registration duty sent",
			zap.Uint64("slot", uint64(slot)),
			zap.Uint64("validator_index", uint64(share.ValidatorIndex)),
			zap.String("validator_pubkey", pk.String()))
	}

	span.SetAttributes(observability.DutyCountAttribute(len(duties)))

	if len(duties) > 0 {
		h.dutiesExecutor.ExecuteDuties(ctx, duties, h.dutyExecutionDeadline(slot))
	}

	if eventDrivenDispatched > 0 {
		// Counterpart to the per-duty enqueue log above — confirms dispatch at
		// the gate and keeps the deferred-broadcast flow greppable (mirrors
		// voluntary-exit). Placed after ExecuteDuties so "dispatched" is accurate.
		h.logger.Debug("dispatched event-driven validator registration duties",
			fields.Slot(slot),
			fields.Count(eventDrivenDispatched))
	}

	span.SetStatus(codes.Ok, "")
}

// blockSlot returns slot that happens (corresponds to) at the same time as block.
// It caches the result to avoid calling execution client multiple times when there are several
// validator registration events present in the same block.
func (h *ValidatorRegistrationHandler) blockSlot(ctx context.Context, blockNumber uint64) (phase0.Slot, error) {
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

func (h *ValidatorRegistrationHandler) dutyExecutionDeadline(slot phase0.Slot) time.Time {
	// 1 wall-clock slot from execution should be sufficient for this duty-type.
	dutyDeadline := h.netCfg.SlotStartTime(slot + 1)
	return dutyDeadline
}

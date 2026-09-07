package duties

import (
	"context"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/operator/duties/dutystore"
	"github.com/ssvlabs/ssv/protocol/v2/types/gloas"
)

// PTCAttestationHandler schedules the Gloas (ePBS) Payload Timeliness Committee attestation duty
// (SIP #94 §3): each epoch it fetches the PTC assignments and, for each slot holding one of this
// node's duties, executes it at the 75%-of-slot cutoff so the runner observes payload presence at
// that point and runs its partial-signature round in the otherwise-free [75%, 100%] window.
//
// Like the proposer and sync-committee handlers, it records every participating validator's duty
// (not just this node's) in the shared duty store — so the message validator can reject PTC messages
// from validators with no such assignment — and runs in both operator and exporter modes. InCommittee
// marks this node's own duties, which only operator mode executes.
//
// From mid-epoch on, the next epoch is fetched ahead of time — across the Gloas fork boundary too, so
// the fork epoch's first slots are covered — and that look-ahead view is reconciled against the
// beacon node's settled answer at the epoch's first tick (see reconcileDuties).
type PTCAttestationHandler struct {
	baseHandler

	duties       *dutystore.Duties[gloas.PTCDuty]
	exporterMode bool

	// lookahead maps each epoch whose cached duties came from a fetch made before the epoch began to
	// that answer's dependent_root; the epoch awaits reconciliation at its first tick.
	lookahead map[phase0.Epoch]phase0.Root
}

func NewPTCAttestationHandler(duties *dutystore.Duties[gloas.PTCDuty], exporterMode bool) *PTCAttestationHandler {
	return &PTCAttestationHandler{
		duties:       duties,
		exporterMode: exporterMode,
		lookahead:    make(map[phase0.Epoch]phase0.Root),
	}
}

func (h *PTCAttestationHandler) Name() string {
	return spectypes.BNRolePTCAttester.String()
}

func (h *PTCAttestationHandler) WaitShutdown() {}

func (h *PTCAttestationHandler) HandleDuties(ctx context.Context) {
	h.logger.Info("starting duty handler")
	defer h.logger.Info("duty handler exited")

	next := h.ticker.Next()
	for {
		select {
		case <-ctx.Done():
			return

		case <-next:
			slot := h.ticker.Slot()
			next = h.ticker.Next()
			h.handleTick(ctx, slot)

		case <-h.indicesChangeCh:
			h.invalidateDuties()
		case reorgEvent := <-h.reorgEventsCh:
			h.handleReorg(reorgEvent)
		}
	}
}

// handleTick brings the store up to date for the slot and schedules this node's duties in it. Fetches
// are bounded by the slot so a hung beacon node cannot stall the loop; execution keeps the handler's
// context.
func (h *PTCAttestationHandler) handleTick(ctx context.Context, slot phase0.Slot) {
	epoch := h.netCfg.EstimatedEpochAtSlot(slot)
	fetchCtx, cancel := context.WithDeadline(ctx, h.netCfg.SlotStartTime(slot+1))
	defer cancel()

	if !h.netCfg.IsGloas(epoch) {
		// The epoch before the fork only runs the look-ahead: the fork epoch's duties are due from its
		// first slot, and a fetch made only then races the cutoff.
		if h.shouldFetchNextEpoch(slot) && h.netCfg.IsGloas(epoch+1) {
			h.fetchLookahead(fetchCtx, epoch+1)
		}
		return
	}

	h.reconcileDuties(ctx, slot, epoch)
	h.fetchDuties(fetchCtx, epoch)
	if h.shouldFetchNextEpoch(slot) {
		h.fetchLookahead(fetchCtx, epoch+1)
	}
	h.eraseBefore(epoch)

	// Exporter records duties for message validation but does not execute them.
	if h.exporterMode {
		return
	}
	if ptcDuties := h.duties.CommitteeSlotDuties(epoch, slot); len(ptcDuties) > 0 {
		specDuties := make([]*spectypes.ValidatorDuty, 0, len(ptcDuties))
		for _, d := range ptcDuties {
			specDuties = append(specDuties, h.toSpecDuty(d))
		}
		h.scheduleExecution(ctx, slot, specDuties)
	}
}

// HandleInitialDuties populates the store on startup, before the first tick, so the message validator
// can check assignments right away: the current epoch once Gloas is active and, near an epoch
// boundary, the next one — so the ticker can't miss the rollover, nor the fork itself.
func (h *PTCAttestationHandler) HandleInitialDuties(ctx context.Context) {
	ctx, cancel := context.WithTimeout(ctx, h.netCfg.SlotDuration)
	defer cancel()

	slot := h.netCfg.EstimatedCurrentSlot()
	epoch := h.netCfg.EstimatedEpochAtSlot(slot)
	if h.netCfg.IsGloas(epoch) {
		h.fetchDuties(ctx, epoch)
	}
	if h.shouldFetchNextEpoch(slot) && h.netCfg.IsGloas(epoch+1) {
		h.fetchLookahead(ctx, epoch+1)
	}
}

// handleReorg drops the cached epochs whose dependent_root changed so the next tick re-fetches them.
// An epoch's PTC duties depend on the block closing the epoch two before it — the same root as its
// attester duties — so the "previous" root covers the current epoch and the "current" root the next.
func (h *PTCAttestationHandler) handleReorg(event ReorgEvent) {
	epoch := h.netCfg.EstimatedCurrentEpoch()
	h.logger.Debug("🔀 reorg event received", fields.Epoch(epoch), zap.Any("event", event))
	if event.PreviousDutyDependentRootChanged {
		h.forget(epoch)
	}
	if event.CurrentDutyDependentRootChanged {
		h.forget(epoch + 1)
	}
}

// invalidateDuties drops every cached epoch after a validator-set change so the next tick re-fetches
// them: the authoritative response replaces the cache rather than merging into it (SIP #94 §3).
func (h *PTCAttestationHandler) invalidateDuties() {
	h.logger.Debug("re-fetching PTC duties on next tick after indices change")
	h.duties.Clear()
	clear(h.lookahead)
}

// forget drops one cached epoch.
func (h *PTCAttestationHandler) forget(epoch phase0.Epoch) {
	h.duties.EraseEpochData(epoch)
	delete(h.lookahead, epoch)
}

// eraseBefore drops the cached epochs earlier than the given one, bounding the cache.
func (h *PTCAttestationHandler) eraseBefore(epoch phase0.Epoch) {
	h.duties.EraseBefore(epoch)
	for cached := range h.lookahead {
		if cached < epoch {
			delete(h.lookahead, cached)
		}
	}
}

// fetchDuties records an epoch's PTC duties once; an epoch already in the store is left as is.
func (h *PTCAttestationHandler) fetchDuties(ctx context.Context, epoch phase0.Epoch) {
	if h.duties.IsEpochSet(epoch) {
		return
	}
	h.fetchAndStore(ctx, epoch)
}

// fetchLookahead records the duties of an epoch that has not begun yet and marks the epoch for
// reconciliation at its first tick.
func (h *PTCAttestationHandler) fetchLookahead(ctx context.Context, epoch phase0.Epoch) {
	if h.duties.IsEpochSet(epoch) {
		return
	}
	if _, dependentRoot, ok := h.fetchAndStore(ctx, epoch); ok {
		h.lookahead[epoch] = dependentRoot
	}
}

// reconcileDuties replaces an epoch's look-ahead view with the beacon node's settled one at the
// epoch's first tick. The spec precomputes the next epoch's committees into the state, so a compliant
// beacon node answers both fetches the same in steady state — but its pre-fork answer for the fork
// epoch can only be a projection (the fork upgrade itself initializes the state's PTC window), and
// beacon nodes have been seen to shift assignments between the two answers regardless (ssv#3027).
// A shift under an unchanged dependent_root is the beacon node's own and is logged as a warning; one
// that comes with a new root is a reorg's. The fetch is bounded by the slot's cutoff so a slow answer
// cannot push this slot's execution past it; on failure the look-ahead view stays in use and the
// next tick retries.
func (h *PTCAttestationHandler) reconcileDuties(ctx context.Context, slot phase0.Slot, epoch phase0.Epoch) {
	lookaheadRoot, pending := h.lookahead[epoch]
	if !pending {
		return
	}
	ctx, cancel := context.WithDeadline(ctx, h.netCfg.PayloadAttestationCutoff(slot))
	defer cancel()

	cached := h.duties.EpochDuties(epoch)
	settled, settledRoot, ok := h.fetchAndStore(ctx, epoch)
	if !ok {
		return
	}
	delete(h.lookahead, epoch)

	added, removed := diffDuties(cached, settled)
	if added == 0 && removed == 0 {
		h.logger.Debug("PTC look-ahead duties confirmed", fields.Epoch(epoch))
		return
	}
	changes := []zap.Field{fields.Epoch(epoch), zap.Int("added", added), zap.Int("removed", removed)}
	if settledRoot != lookaheadRoot {
		h.logger.Info("PTC duties changed along with their dependent_root since the look-ahead fetch", changes...)
		return
	}
	h.logger.Warn("PTC duties changed under an unchanged dependent_root since the look-ahead fetch", changes...)
}

// fetchAndStore fetches every participating validator's PTC duties for the epoch and stores them, this
// node's own marked InCommittee for execution. It returns the stored duties and their dependent_root,
// or ok=false when nothing was stored.
func (h *PTCAttestationHandler) fetchAndStore(ctx context.Context, epoch phase0.Epoch) (stored []dutystore.StoreDuty[gloas.PTCDuty], dependentRoot phase0.Root, ok bool) {
	var eligible []phase0.ValidatorIndex
	for _, share := range h.validatorProvider.Validators() {
		if share.IsParticipating(h.netCfg.Beacon, epoch) {
			eligible = append(eligible, share.ValidatorIndex)
		}
	}
	if len(eligible) == 0 {
		return nil, phase0.Root{}, false
	}

	ptcDuties, err := h.beaconNode.PayloadAttestationDuties(ctx, epoch, eligible)
	if err != nil {
		h.logger.Warn("failed to fetch PTC duties", fields.Epoch(epoch), zap.Error(err))
		return nil, phase0.Root{}, false
	}

	self := make(map[phase0.ValidatorIndex]struct{})
	for _, idx := range h.selfParticipatingIndices(epoch) {
		self[idx] = struct{}{}
	}

	stored = make([]dutystore.StoreDuty[gloas.PTCDuty], 0, len(ptcDuties.Duties))
	for _, d := range ptcDuties.Duties {
		_, inCommittee := self[d.ValidatorIndex]
		stored = append(stored, dutystore.StoreDuty[gloas.PTCDuty]{
			Slot:           d.Slot,
			ValidatorIndex: d.ValidatorIndex,
			Duty:           d,
			InCommittee:    inCommittee,
		})
	}
	h.duties.Set(epoch, stored)

	h.logger.Debug("fetched PTC duties", fields.Epoch(epoch), zap.Int("duties", len(stored)))
	return stored, ptcDuties.DependentRoot, true
}

// diffDuties counts the (slot, validator) assignments present in only one of the two views.
func diffDuties(before, after []dutystore.StoreDuty[gloas.PTCDuty]) (added, removed int) {
	type assignment struct {
		slot  phase0.Slot
		index phase0.ValidatorIndex
	}
	unmatched := make(map[assignment]struct{}, len(before))
	for _, d := range before {
		unmatched[assignment{d.Slot, d.ValidatorIndex}] = struct{}{}
	}
	for _, d := range after {
		a := assignment{d.Slot, d.ValidatorIndex}
		if _, ok := unmatched[a]; ok {
			delete(unmatched, a)
		} else {
			added++
		}
	}
	return added, len(unmatched)
}

// scheduleExecution fires the duty at the payload-attestation cutoff, with a deadline at slot end.
func (h *PTCAttestationHandler) scheduleExecution(ctx context.Context, slot phase0.Slot, duties []*spectypes.ValidatorDuty) {
	executeAt := h.netCfg.PayloadAttestationCutoff(slot)
	deadline := h.netCfg.SlotStartTime(slot + 1)
	time.AfterFunc(time.Until(executeAt), func() {
		h.dutiesExecutor.ExecuteDuties(ctx, duties, deadline)
	})
}

func (h *PTCAttestationHandler) toSpecDuty(duty *gloas.PTCDuty) *spectypes.ValidatorDuty {
	return &spectypes.ValidatorDuty{
		Type:           spectypes.BNRolePTCAttester,
		PubKey:         duty.PubKey,
		ValidatorIndex: duty.ValidatorIndex,
		Slot:           duty.Slot,
	}
}

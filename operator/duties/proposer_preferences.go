package duties

import (
	"context"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

// ProposerPreferencesHandler schedules the Gloas (ePBS) proposer-preferences duty (SIP #94 §5): for
// each upcoming proposal slot a local validator holds within the proposer lookahead, it emits one
// duty so the runner broadcasts that validator's fee recipient and target gas limit ahead of the
// slot. Unlike slot-bound duties it emits in advance — duty.Slot is the future proposal slot, and the
// duty executes (the runner signs and broadcasts) as soon as the assignment is known.
type ProposerPreferencesHandler struct {
	baseHandler

	// emitted records the dependent_root last emitted for each epoch (SIP #94 §5). An epoch emits once
	// per dependent_root: a steady-state tick skips an already-emitted epoch, while a post-reorg recheck
	// re-emits only if the root actually changed. Accessed only from the HandleDuties goroutine.
	emitted map[phase0.Epoch]phase0.Root

	// recheckLookahead is set by a reorg so the next tick re-evaluates each lookahead epoch's
	// dependent_root instead of trusting its emitted marker. Accessed only from the HandleDuties goroutine.
	recheckLookahead bool

	// gloasForkRechecked is set once the first Gloas tick has forced a lookahead recheck (see
	// emitForTick). Accessed only from the HandleDuties goroutine.
	gloasForkRechecked bool
}

func NewProposerPreferencesHandler() *ProposerPreferencesHandler {
	return &ProposerPreferencesHandler{
		emitted: map[phase0.Epoch]phase0.Root{},
	}
}

func (h *ProposerPreferencesHandler) Name() string {
	return spectypes.BNRoleProposerPreferences.String()
}

func (h *ProposerPreferencesHandler) WaitShutdown() {}

// HandleDuties emits proposer-preferences duties across the proposer lookahead (current + next epoch,
// MIN_SEED_LOOKAHEAD=1). In the epoch immediately before the Gloas fork it pre-emits the first Gloas
// epoch's preferences (SIP #94 §5) so builders have them before the fork. Reorg/indices-change re-emission
// is handled below.
func (h *ProposerPreferencesHandler) HandleDuties(ctx context.Context) {
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
			h.emitForTick(ctx, slot)

		case <-h.indicesChangeCh:
			// New local validators may hold proposal slots in an already-emitted epoch, so drop the
			// markers to re-emit the full lookahead for them on the next tick.
			h.logger.Debug("🔀 re-emitting proposer preferences on indices change")
			clear(h.emitted)

		case <-h.reorgEventsCh:
			// A reorg may have changed a proposal slot's dependent_root; recheck the lookahead on the next
			// tick and re-emit only the epochs whose root actually changed (SIP #94 §5).
			h.logger.Debug("🔀 rechecking proposer-preferences dependent roots on reorg")
			h.recheckLookahead = true
		}
	}
}

// emitForTick emits the lookahead's preferences for the tick's slot: the current epoch (plus the next,
// once it's a good time to fetch) in steady state, or the first Gloas epoch when in the pre-fork
// window. Outside both it does nothing (pre-Gloas, no preferences yet). A reorg recheck flagged since
// the last tick is consumed here, forcing the lookahead's dependent roots to be re-evaluated.
func (h *ProposerPreferencesHandler) emitForTick(ctx context.Context, slot phase0.Slot) {
	recheck := h.recheckLookahead
	h.recheckLookahead = false

	epoch := h.netCfg.EstimatedEpochAtSlot(slot)
	switch {
	case h.netCfg.IsGloas(epoch):
		// The first Gloas tick forces a recheck, like a reorg would: the pre-fork window emitted the
		// fork epoch under the pre-fork view of its dependent_root, and a CL whose reported root
		// shifts at the fork transition would otherwise leave the boundary epoch pinned to the stale
		// root (an epoch re-emits only on a root change). With an unchanged root this is a no-op.
		if !h.gloasForkRechecked {
			h.gloasForkRechecked = true
			recheck = true
		}
		h.emitForEpoch(ctx, epoch, slot, recheck)
		if h.shouldFetchNextEpoch(slot) {
			h.emitForEpoch(ctx, epoch+1, slot, recheck)
		}
		h.evictOutdated(epoch)
	case h.netCfg.InGloasPriorWindow(slot):
		// epoch+1 is GLOAS_FORK_EPOCH throughout the prior window (MIN_SEED_LOOKAHEAD=1).
		h.emitForEpoch(ctx, epoch+1, slot, recheck)
	}
}

// emitForEpoch emits one proposer-preferences duty per still-upcoming local proposal assignment in
// the epoch, to be broadcast immediately. It emits once per (epoch, dependent_root): a steady-state tick skips an
// already-emitted epoch, and a post-reorg recheck re-emits only when the epoch's dependent_root changed —
// re-emitting under an unchanged root would just duplicate the preference and get the operator
// gossip-penalized (SIP #94 §5).
func (h *ProposerPreferencesHandler) emitForEpoch(ctx context.Context, epoch phase0.Epoch, currentSlot phase0.Slot, recheck bool) {
	if _, done := h.emitted[epoch]; done && !recheck {
		return
	}

	indices := h.selfParticipatingIndices(epoch)
	if len(indices) == 0 {
		return // no local validators yet; retry on the next tick
	}

	dependentRoot, err := h.beaconNode.ProposerDutiesDependentRoot(ctx, epoch)
	if err != nil {
		h.logger.Warn("failed to fetch proposer-duties dependent root", fields.Epoch(epoch), zap.Error(err))
		return // retry on the next tick
	}
	if prev, done := h.emitted[epoch]; done && prev == dependentRoot {
		return // dependent_root unchanged; a re-emission would only duplicate the preference
	}

	duties, err := h.beaconNode.ProposerDuties(ctx, epoch, indices)
	if err != nil {
		h.logger.Warn("failed to fetch proposer duties", fields.Epoch(epoch), zap.Error(err))
		return // retry on the next tick
	}

	preferenceDuties := make([]*spectypes.ValidatorDuty, 0, len(duties))
	for _, d := range duties {
		if d.Slot <= currentSlot {
			// The epoch fetch returns every assignment in the epoch, including proposal slots already
			// reached: a preference for those is moot (builders needed it beforehand) and peers would
			// reject its partials as late, so emitting it could only produce a doomed duty.
			continue
		}
		preferenceDuties = append(preferenceDuties, &spectypes.ValidatorDuty{
			Type:           spectypes.BNRoleProposerPreferences,
			PubKey:         d.PubKey,
			Slot:           d.Slot, // proposal slot — the self-identifying duty.Slot
			ValidatorIndex: d.ValidatorIndex,
		})
	}
	h.emitted[epoch] = dependentRoot

	if len(preferenceDuties) == 0 {
		return
	}

	// Emit now: the runner builds, signs, and broadcasts immediately. duty.Slot is the (future)
	// proposal slot, and operators emit at their own ticks (registration/event timing), so §5
	// convergence may legitimately span the slots up to it — give each duty an execution window
	// running to the end of its own proposal slot, matching the runner's §5 outcome horizon.
	for _, d := range preferenceDuties {
		h.dutiesExecutor.ExecuteDuties(ctx, []*spectypes.ValidatorDuty{d}, h.netCfg.SlotStartTime(d.Slot+1))
	}

	h.logger.Debug("emitted proposer preferences duties",
		fields.Epoch(epoch),
		fields.Count(len(preferenceDuties)),
		zap.String("dependent_root", dependentRoot.String()),
	)
}

// evictOutdated drops emitted-epoch markers for epochs before the current one.
func (h *ProposerPreferencesHandler) evictOutdated(currentEpoch phase0.Epoch) {
	evictEpochsBefore(h.emitted, currentEpoch)
}

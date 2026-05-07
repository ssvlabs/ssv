package obft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// RunProposerSlot drives one proposer-duty slot end-to-end through the
// OBFT flow:
//
//  1. Start a new OBFT instance for the slot.
//  2. For each layer where the local op is the leader: schedule a goroutine
//     that fetches at the layer's FetchAt and broadcasts a Phase-1 bundle.
//  3. At T_commit: BuildAndBroadcastCommit (single emission per spec
//     §Phase 2; carries both σ partials and NR partials).
//  4. From T_commit + Δ_2 onward: poll Resolve opportunistically until
//     σ-quorum reaches (output → SubmitOutput, broadcast Certificate),
//     a peer's Certificate arrives (Certificate → SubmitOutput), or the
//     slot's submission deadline (ctx) is reached.
//  5. EndInstance.
//
// Per spec §Phase 3: T_commit + Δ_2 + Δ_3 (= RoundEndOffset) is a SOFT
// per-operator target, not a hard cluster-wide deadline; reconstruction
// running past it can spill into the submission slack, and a faster peer's
// Certificate gossip lets an operator that hasn't completed local
// reconstruction submit (V, S) directly. Late KindCommit arrivals can
// also be incorporated by re-running the reconstruction walk — Pigeonhole
// semantics still hold.
//
// Returns the result of ResolveAndSubmitOpportunistically (or the first
// non-recoverable error encountered earlier). Phase-1 fetch errors are
// non-fatal: they're surfaced via the Scheduler's hooks but don't abort
// the slot, since other layers may still succeed.
//
// This is the canonical reference for what `protocol/v2/ssv/runner/proposer.go`'s
// runner methods need to do per-slot when integrating OBFT.
func RunProposerSlot(
	ctx context.Context,
	sched *Scheduler,
	slot phase0.Slot,
	slotStart time.Time,
) (err error) {
	ctrl := sched.Controller()
	inst, err := ctrl.StartNewInstance(slot)
	if err != nil {
		return fmt.Errorf("obft runner: start instance: %w", err)
	}
	defer ctrl.EndInstance(slot)

	// Replay any envelopes that arrived before StartNewInstance — peers with
	// shorter pre-consensus may have broadcast Phase-1 bundles or Commits
	// before this operator's instance was ready. Without replay, those
	// messages would be silently dropped (gossipsub doesn't re-deliver to
	// existing subscribers).
	sched.DrainPending(ctx, slot)

	cfg := inst.Config

	// Phase 1 — for each layer the local op leads, schedule a concurrent
	// fetch+broadcast at the layer's FetchAt offset.
	var fetchWG sync.WaitGroup
	for _, layer := range inst.LeaderAtLayers {
		fetchWG.Add(1)
		go func(layer int) {
			defer fetchWG.Done()
			fetchAt := slotStart.Add(cfg.Layers[layer].FetchAt)
			if !sleepUntil(ctx, fetchAt) {
				return
			}
			// Fetch errors are NOT fatal — other layers may still succeed,
			// and operators that didn't fetch fall through via NR-quorum.
			_ = sched.FetchAndBroadcastBundle(ctx, slot, layer)
		}(layer)
	}

	// Phase 2 begins at TCommit. Each operator emits a single KindCommit
	// carrying their per-layer σ partials and NR partials.
	tCommit := slotStart.Add(cfg.TCommit)
	if !sleepUntil(ctx, tCommit) {
		fetchWG.Wait()
		return ctx.Err()
	}
	if _, err := sched.BuildAndBroadcastCommit(ctx, slot); err != nil {
		fetchWG.Wait()
		return fmt.Errorf("obft runner: phase-2 commit broadcast: %w", err)
	}

	// Phase 3 — Resolve opportunistically from T_commit + Δ_2 onward.
	// Per spec §Phase 3, reconstruction starts at T_commit + Δ_2 (when
	// in-envelope KindCommits have propagated) and runs until σ-quorum
	// reaches or the relay-submission deadline (ctx) forces termination.
	// Late KindCommits arriving past T_commit + Δ_2 can be incorporated by
	// re-running the walk; Pigeonhole semantics ensure at most one V can
	// reconstruct cluster-wide regardless of timing.
	phase3Start := slotStart.Add(cfg.PhaseTwoEndOffset())
	if !sleepUntil(ctx, phase3Start) {
		fetchWG.Wait()
		return ctx.Err()
	}

	err = sched.ResolveAndSubmitOpportunistically(ctx, slot)
	fetchWG.Wait()
	return err
}

// sleepUntil blocks until `t` or context cancellation. Returns true if it
// reached `t`, false if canceled.
func sleepUntil(ctx context.Context, t time.Time) bool {
	d := time.Until(t)
	if d <= 0 {
		select {
		case <-ctx.Done():
			return false
		default:
			return true
		}
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return true
	case <-ctx.Done():
		return false
	}
}

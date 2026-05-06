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
//  3. At T_commit: BuildAndBroadcastOnion (covers σ-state operators).
//  4. At T_commit + Δ_2: PhaseTwoEnd + BuildAndBroadcastNR (also re-emits
//     Onion if any Defer-partition resolved at PhaseTwoEnd).
//  5. At T_round_end: Resolve + SubmitOutput (or peer-Certificate fallback).
//  6. EndInstance.
//
// Returns the result of ResolveAndSubmit (or the first non-recoverable
// error encountered earlier). Phase-1 fetch errors are non-fatal: they're
// surfaced via the Scheduler's hooks but don't abort the slot, since other
// layers may still succeed.
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

	// Phase 2 begins at TCommit. σ-state operators emit immediately.
	tCommit := slotStart.Add(cfg.TCommit)
	if !sleepUntil(ctx, tCommit) {
		fetchWG.Wait()
		return ctx.Err()
	}
	if _, err := sched.BuildAndBroadcastOnion(ctx, slot); err != nil {
		fetchWG.Wait()
		return fmt.Errorf("obft runner: phase-2 onion broadcast: %w", err)
	}

	// End of Phase 2 at TCommit + Delta2 — force-commit + emit NR.
	tPhaseTwoEnd := slotStart.Add(cfg.PhaseTwoEndOffset())
	if !sleepUntil(ctx, tPhaseTwoEnd) {
		fetchWG.Wait()
		return ctx.Err()
	}
	if _, err := sched.PhaseTwoEndAndBroadcastNR(ctx, slot); err != nil {
		fetchWG.Wait()
		return fmt.Errorf("obft runner: phase-2 end + NR broadcast: %w", err)
	}

	// Phase 3 — wait until T_round_end before Resolve so all NR partials
	// have had time to propagate.
	tRoundEnd := slotStart.Add(cfg.RoundEndOffset())
	if !sleepUntil(ctx, tRoundEnd) {
		fetchWG.Wait()
		return ctx.Err()
	}

	err = sched.ResolveAndSubmit(ctx, slot)
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

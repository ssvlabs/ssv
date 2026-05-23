package twoab

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// RunProposerSlot drives one proposer-duty slot end-to-end through the 2abOBFT
// flow:
//
//  1. Start a new instance for the slot (+ defer EndInstance).
//  2. Replay any envelopes buffered before the instance started.
//  3. Spawn the host-validation drain goroutine (peer-reflood-V harvest path).
//  4. Phase 1 — for each layer the local op leads, a goroutine fetches the
//     freshest candidate from slot start up to the layer's broadcast deadline
//     (in 2abOBFT FetchAt is the broadcast deadline) and broadcasts a Phase-1
//     bundle (self-fed so the leader's L0ReadyCh can close).
//  5. Phase 2a fire — at the earlier of L0ReadyCh closing (early-fire: the op's
//     L_0 emission is determinable) or the TPhase2a backstop (the NoValue
//     path), fire the one-shot Phase-2a emission. There is no T_commit wall.
//  6. Phase 2b/3 — the resolve loop broadcasts cascade-fired emissions on each
//     state delta (dynamic Phase 2b) and resolves opportunistically until
//     σ-quorum, a peer certificate, or the relay-submission deadline (ctx).
//  7. EndInstance (deferred).
//
// Phase-1 fetch errors are non-fatal: surfaced via the Scheduler's hooks but
// they don't abort the slot (other layers / NR fall-through may still succeed).
// Returns the result of ResolveAndSubmitOpportunistically (or the first
// non-recoverable earlier error).
func RunProposerSlot(
	ctx context.Context,
	sched *Scheduler,
	slot phase0.Slot,
	slotStart time.Time,
) (err error) {
	ctrl := sched.Controller()
	inst, err := ctrl.StartNewInstance(slot)
	if err != nil {
		return fmt.Errorf("twoab runner: start instance: %w", err)
	}
	defer ctrl.EndInstance(slot)

	// Replay envelopes that arrived before StartNewInstance.
	sched.DrainPending(ctx, slot)

	// Drain host-validation requests (peer-reflood-V harvest path): each verdict
	// fed back via ApplyHostValidity can close L0ReadyCh / cascade an emission.
	// Exits when the channel closes (EndInstance) or ctx fires.
	go sched.RunHostValidationDrain(ctx, slot)

	cfg := inst.Config

	// Phase 1 — concurrent per-leader-layer fetch + broadcast. Unlike bare OBFT
	// there's no sleep-until-fetch-start: FetchAt is the broadcast deadline, so
	// the leader polls the freshest candidate from now up to that deadline.
	var fetchWG sync.WaitGroup
	for _, layer := range inst.LeaderAtLayers {
		fetchWG.Add(1)
		go func(layer int) {
			defer fetchWG.Done()
			deadline := slotStart.Add(cfg.Layers[layer].FetchAt)
			// Floor to a live window so a past/zero deadline (deepest layer
			// FetchAt=0 → broadcast at slot start) still fetches + broadcasts
			// under a non-expired ctx.
			if minDeadline := time.Now().Add(defaultFetchBuildBuffer); deadline.Before(minDeadline) {
				deadline = minDeadline
			}
			fetchCtx, cancel := context.WithDeadline(ctx, deadline)
			defer cancel()
			// Fetch errors are non-fatal — other layers / NR fall-through cover it.
			_ = sched.FetchAndBroadcastBundle(fetchCtx, slot, layer)
		}(layer)
	}

	// Phase 2a fire — early-fire on L0ReadyCh, else the TPhase2a backstop.
	tPhase2a := slotStart.Add(cfg.TPhase2a)
	l0Ready := ctrl.L0ReadyCh(slot)
	backstop := time.NewTimer(time.Until(tPhase2a))
	select {
	case <-l0Ready:
	case <-backstop.C:
	case <-ctx.Done():
		backstop.Stop()
		fetchWG.Wait()
		return ctx.Err()
	}
	backstop.Stop()
	if err := sched.FirePhase2a(slot); err != nil {
		fetchWG.Wait()
		return fmt.Errorf("twoab runner: fire phase 2a: %w", err)
	}

	// Phase 2b/3 — dynamic emissions + opportunistic resolve until σ-quorum, a
	// peer certificate, or the relay-submission deadline (ctx).
	err = sched.ResolveAndSubmitOpportunistically(ctx, slot)
	fetchWG.Wait()
	return err
}

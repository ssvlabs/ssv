package tbft

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// DefaultGossipWindow is the default time between Phase-2 broadcast and
// Phase-3 resolve, allowing peers' onions and non-receipts to propagate
// before each operator runs the local decryption walk.
const DefaultGossipWindow = 500 * time.Millisecond

// RunProposerSlot drives one proposer-duty slot end-to-end through the
// TBFT flow:
//
//  1. Start a new TBFT instance for the slot.
//  2. For each layer where the local operator is the designated leader,
//     wait until the layer's `FetchAt` offset, then fetch the candidate
//     value (via the Scheduler's hooks) and broadcast it.
//  3. At the deadline, broadcast the local operator's onion + non-receipts.
//  4. After a gossip window, run Resolve and submit the output.
//  5. End the instance.
//
// Returns the result of `Scheduler.ResolveAndSubmit` (or the first
// non-recoverable error encountered earlier). Phase-1 fetch errors are
// non-fatal: they're surfaced via the Scheduler's hooks but don't abort
// the slot, since other layers may still succeed.
//
// This is the canonical reference for what `protocol/v2/ssv/runner/proposer.go`'s
// runner methods need to do per-slot when integrating TBFT in place of
// QBFT. Production runners will inline this logic into their existing
// flow rather than calling this function directly, because the SSV runner
// has additional concerns (RANDAO pre-consensus, doppelganger checks,
// metrics, tracing, etc.) that interleave with the timing here.
func RunProposerSlot(
	ctx context.Context,
	ctrl *Controller,
	sched *Scheduler,
	slot phase0.Slot,
	slotStart time.Time,
) error {
	return RunProposerSlotWithGossipWindow(ctx, ctrl, sched, slot, slotStart, DefaultGossipWindow)
}

// RunProposerSlotWithGossipWindow is RunProposerSlot with a configurable
// post-broadcast wait. Useful for tests that want sub-second turnaround
// without waiting the full default 500ms.
func RunProposerSlotWithGossipWindow(
	ctx context.Context,
	ctrl *Controller,
	sched *Scheduler,
	slot phase0.Slot,
	slotStart time.Time,
	gossipWindow time.Duration,
) (err error) {
	inst, err := ctrl.StartNewInstance(slot)
	if err != nil {
		return fmt.Errorf("tbft runner: start instance: %w", err)
	}
	defer ctrl.EndInstance(slot)

	cfg := inst.Config

	// Phase 1 — for each layer the local operator leads, schedule a
	// concurrent fetch+broadcast at the layer's FetchAt offset.
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
			// and operators that didn't fetch fall through via non-receipt.
			// The Scheduler's FetchCandidate hook is responsible for any
			// retry/logging; we just record-and-continue here.
			_ = sched.FetchAndBroadcastCandidate(ctx, slot, layer)
		}(layer)
	}

	// Phase 2 — at the deadline, broadcast our onion + non-receipts.
	deadline := slotStart.Add(cfg.Deadline)
	if !sleepUntil(ctx, deadline) {
		fetchWG.Wait() // let any in-flight fetches finish before returning
		return ctx.Err()
	}
	if err := sched.BuildAndBroadcastOnion(ctx, slot); err != nil {
		fetchWG.Wait()
		return fmt.Errorf("tbft runner: phase 2 broadcast: %w", err)
	}

	// Phase 3 — wait for gossip propagation, then resolve + submit.
	resolveAt := deadline.Add(gossipWindow)
	if !sleepUntil(ctx, resolveAt) {
		fetchWG.Wait()
		return ctx.Err()
	}

	err = sched.ResolveAndSubmit(ctx, slot)
	fetchWG.Wait() // ensure all fetch goroutines have exited before instance teardown
	return err
}

// sleepUntil blocks until `t` or context cancellation. Returns true if it
// reached `t`, false if cancelled.
func sleepUntil(ctx context.Context, t time.Time) bool {
	d := time.Until(t)
	if d <= 0 {
		// Already past `t` — return immediately, but still respect a
		// cancelled context.
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

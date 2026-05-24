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
//  4. Immediately from T_commit onward: ResolveAndSubmitOpportunistically
//     blocks on the controller's per-slot state-delta signal, calling
//     Resolve on each inbound KindCommit / KindCertificate observation
//     until σ-quorum reaches (output → SubmitOutput, broadcast
//     Certificate), a peer's Certificate arrives (Certificate →
//     SubmitOutput), or the slot's submission deadline (ctx) is reached.
//  5. EndInstance.
//
// Per spec §Phase 3: T_commit + Δ_2 + ε_3 (= RoundEndOffset) is a SOFT
// per-operator target, not a hard cluster-wide deadline; reconstruction
// running past it can spill into the submission slack, and a faster peer's
// Certificate gossip lets an operator that hasn't completed local
// reconstruction submit (V, S) directly. Late KindCommit arrivals can
// also be incorporated by re-running the reconstruction walk — Pigeonhole
// semantics still hold. Resolve is idempotent (re-running on partial state
// returns ErrNoQuorum without mutation), so the opportunistic call at
// T_commit is safe even before any peer's commit has arrived.
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
	// EndInstance seals the instance (sets the ended flag) and removes it from
	// the controller's tracking map atomically.
	defer ctrl.EndInstance(slot)

	// Replay any envelopes that arrived before StartNewInstance — peers with
	// shorter pre-consensus may have broadcast Phase-1 bundles or Commits
	// before this operator's instance was ready. Without replay, those
	// messages would be silently dropped (gossipsub doesn't re-deliver to
	// existing subscribers).
	sched.DrainPending(ctx, slot)

	// Spec §Phase 2 / Peer-reflood V via early commit: the Instance enqueues
	// host-validity requests on WantsHostValidationCh when V is first-
	// observed via a peer's σ-onion entry at L_0 (V-drop receiver path).
	// Spawn a per-slot drain goroutine that dispatches HostValidate and
	// feeds the verdict back via ApplyHostValidity, which closes the
	// L_0-ready channel on the peer-V σ branch and lets the V-drop
	// receiver early-emit σ_i^V on the harvested V. The goroutine exits
	// when the channel is closed (EndInstance → Finalize) or ctx fires.
	go drainHostValidationRequests(ctx, sched, slot)

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
			// Spec §Failure modes / Late deepest-layer leader broadcast:
			// host-side hard deadline at T_broadcast_max_k =
			// max(0, T_commit − B_k). Past this point, receivers
			// reject the bundle as "first observed past T_commit".
			// Aborting the fetch instead converts the pathology into
			// the cleanly-handled "silent leader" mode (NR-quorum →
			// fall-through). Defense-in-depth on top of the K ≥ f+2
			// minimum that already provides the recovery layer.
			//
			// `B_k` is a target (spec §Setting). When `B_k ≥ T_commit`
			// the per-layer target clamps to 0 — best-effort broadcast
			// from slot start. This is the default for the deepest layer
			// (`B_{K-1} = T_commit` by default). The host-side deadline
			// falls back to T_commit, the protocol-wide hard wall (peers
			// reject past T_commit regardless), so the leader still
			// attempts the fetch instead of going silent.
			deadlineOffset := cfg.BroadcastMaxOffsetForLayer(layer)
			if deadlineOffset == 0 {
				deadlineOffset = cfg.TCommit
			}
			broadcastMax := slotStart.Add(deadlineOffset)
			fetchCtx, cancel := context.WithDeadline(ctx, broadcastMax)
			defer cancel()
			// Fetch errors are NOT fatal — other layers may still succeed,
			// and operators that didn't fetch fall through via NR-quorum.
			_ = sched.FetchAndBroadcastBundle(fetchCtx, slot, layer)
		}(layer)
	}

	// Phase 2 emit: per spec §Phase 2 emission-timing, fire at T_emit =
	// min(L_0-observed-and-validated, T_commit fallback). Healthy mesh hits
	// the early trigger ~1·BTT before T_commit; silent-L_0 falls back to
	// the T_commit deadline. Either path emits the operator's single
	// KindCommit carrying per-layer σ/NR partials based on observations at
	// emit time.
	tCommit := slotStart.Add(cfg.TCommit)
	l0Ready := ctrl.L0ReadyCh(slot)
	tCommitTimer := time.NewTimer(time.Until(tCommit))
	select {
	case <-l0Ready:
	case <-tCommitTimer.C:
	case <-ctx.Done():
		tCommitTimer.Stop()
		fetchWG.Wait()
		return ctx.Err()
	}
	tCommitTimer.Stop()
	if _, err := sched.BuildAndBroadcastCommit(ctx, slot); err != nil {
		fetchWG.Wait()
		return fmt.Errorf("obft runner: phase-2 commit broadcast: %w", err)
	}

	// Phase 3 — Resolve opportunistically from T_commit onward. The previous
	// Δ_2 pre-wait was conservative-by-tradition: Resolve is idempotent and
	// returns ErrNoQuorum cleanly on incomplete state. Calling immediately
	// after Commit emission lets σ-quorum trigger submission as soon as the
	// last needed partial arrives — saving ~Δ_2 + ticker latency per slot in
	// the average case. The scheduler blocks on the controller's per-slot
	// state-delta channel; each KindCommit / KindCertificate observation
	// wakes a single Resolve attempt. Late KindCommits arriving past
	// RoundEndOffset are still incorporated (Pigeonhole keeps cluster-wide
	// uniqueness regardless of timing).
	err = sched.ResolveAndSubmitOpportunistically(ctx, slot)
	fetchWG.Wait()
	return err
}

// drainHostValidationRequests is the per-slot goroutine that services the
// Instance's WantsHostValidationCh per spec §Phase 2 / Peer-reflood V via
// early commit. Each request asks the host to validate a V first-observed
// via a peer's σ-onion entry at some layer; the verdict is fed back via
// ApplyHostValidity, which drives L_0 readiness on the σ-via-peer-V branch.
//
// Exits when (a) the channel is closed by Finalize/EndInstance, or (b) ctx
// fires. Per-request HostValidate failures are logged-and-skipped: a
// transient host error doesn't poison subsequent requests. ApplyHostValidity
// errors are similarly non-fatal at this layer — the worst case is the
// operator misses the early-emit opportunity for this V and falls back to
// the T_commit deadline (existing degraded path).
func drainHostValidationRequests(ctx context.Context, sched *Scheduler, slot phase0.Slot) {
	ctrl := sched.Controller()
	ch := ctrl.WantsHostValidationCh(slot)
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-ch:
			if !ok {
				return // channel closed via Finalize
			}
			valid, err := sched.hooks.HostValidate(ctx, slot, req.Layer, []byte(req.Value))
			if err != nil {
				// Transient host error; skip. The receiver will fall back
				// to the T_commit deadline if this was the only V path.
				continue
			}
			// Non-fatal at runner level: a stale slot or already-validated
			// V both produce errors here that we don't need to surface.
			_ = ctrl.ApplyHostValidity(slot, req.Layer, []byte(req.Value), valid)
		}
	}
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

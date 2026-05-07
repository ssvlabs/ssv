package obft

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft"
	"github.com/ssvlabs/ssv/protocol/v2/obft/wire"
)

// LifecycleHooks are callbacks the runner provides to the Scheduler. The
// runner controls *when* phases fire; the Scheduler executes the protocol
// mechanics inside each phase and calls the appropriate hook for runner
// concerns (fetching, broadcasting, submitting).
type LifecycleHooks struct {
	// FetchCandidate is invoked when the local operator is the designated
	// leader for `layer` of `slot` and the layer's `FetchAt` has arrived.
	// Returns the candidate-value bytes the leader signs and broadcasts.
	//
	// Required.
	FetchCandidate func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error)

	// HostValidate is invoked at receiver-side after a Phase-1 bundle has
	// been observed, to obtain the host application's valid/not-valid
	// verdict on the bundled candidate value. Returns (valid bool, err).
	//
	// Required. Implementors should run a stable head snapshot validation
	// (parent_root match, fork/domain, etc.) and lock the verdict for the
	// remainder of the slot.
	HostValidate func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error)

	// Broadcast is invoked to gossip an OBFT wire-envelope-wrapped message
	// (Phase1Bundle / Commit / Certificate) to peer operators.
	//
	// Required.
	Broadcast func(ctx context.Context, slot phase0.Slot, data []byte) error

	// SubmitOutput is invoked when Resolve produces a signed Output.
	//
	// Required.
	SubmitOutput func(ctx context.Context, slot phase0.Slot, output *obftcore.Output) error

	// BroadcastCertificate is invoked to gossip a final Certificate after
	// Resolve succeeds. Optional; if nil, the certificate is not gossiped
	// (the runner submits its own Output but doesn't help peers without
	// local reconstruction).
	BroadcastCertificate func(ctx context.Context, slot phase0.Slot, data []byte) error

	// OnMissedSlot is invoked when Resolve returns ErrNoQuorum or a related
	// failure. Optional.
	OnMissedSlot func(ctx context.Context, slot phase0.Slot, reason error)
}

func (h *LifecycleHooks) validate() error {
	if h == nil {
		return errors.New("obft adapter: nil LifecycleHooks")
	}
	if h.FetchCandidate == nil {
		return errors.New("obft adapter: LifecycleHooks.FetchCandidate is required")
	}
	if h.HostValidate == nil {
		return errors.New("obft adapter: LifecycleHooks.HostValidate is required")
	}
	if h.Broadcast == nil {
		return errors.New("obft adapter: LifecycleHooks.Broadcast is required")
	}
	if h.SubmitOutput == nil {
		return errors.New("obft adapter: LifecycleHooks.SubmitOutput is required")
	}
	return nil
}

// Scheduler combines a Controller with LifecycleHooks to expose per-slot
// phase mechanics as procedural methods. The runner calls these at the
// right slot offsets — the Scheduler does NOT manage time itself.
type Scheduler struct {
	controller *Controller
	hooks      *LifecycleHooks
}

// NewScheduler constructs a Scheduler.
func NewScheduler(controller *Controller, hooks *LifecycleHooks) (*Scheduler, error) {
	if controller == nil {
		return nil, errors.New("obft adapter: nil Controller")
	}
	if err := hooks.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{controller: controller, hooks: hooks}, nil
}

// Controller returns the underlying Controller.
func (s *Scheduler) Controller() *Controller {
	return s.controller
}

// FetchAndBroadcastBundle executes Phase 1 for one layer where the local
// operator is the designated leader:
//
//  1. Call hooks.FetchCandidate to obtain the candidate value.
//  2. Build the Phase-1 bundle (locks σ-V on the value via EKM-style
//     enforcement; self-observes into the σ-pool).
//  3. Apply host validity verdict at the leader's own instance (host
//     validated the value at fetch time).
//  4. Broadcast the bundle via hooks.Broadcast.
func (s *Scheduler) FetchAndBroadcastBundle(ctx context.Context, slot phase0.Slot, layer int) error {
	value, err := s.hooks.FetchCandidate(ctx, slot, layer)
	if err != nil {
		return fmt.Errorf("obft scheduler: fetch candidate (slot=%d layer=%d): %w", slot, layer, err)
	}

	bundle, err := s.controller.BuildPhase1Bundle(slot, layer, value)
	if err != nil {
		return fmt.Errorf("obft scheduler: build phase-1 bundle: %w", err)
	}
	if err := s.controller.ApplyHostValidity(slot, layer, value, true); err != nil {
		return fmt.Errorf("obft scheduler: apply host validity (own bundle): %w", err)
	}

	data, err := wire.WrapPhase1Bundle(bundle)
	if err != nil {
		return fmt.Errorf("obft scheduler: wrap phase-1 bundle: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return fmt.Errorf("obft scheduler: broadcast phase-1 bundle: %w", err)
	}
	return nil
}

// DrainPending replays any envelopes buffered on the Controller for `slot`
// (typically because peers broadcast before this operator's instance had
// started). Errors from individual replays are logged via the OnMissedSlot
// hook is NOT invoked — replay errors are best-effort, not slot-failing.
func (s *Scheduler) DrainPending(ctx context.Context, slot phase0.Slot) {
	for _, env := range s.controller.DrainPending(slot) {
		switch {
		case env.Bundle != nil:
			_ = s.HandlePeerPhase1Bundle(ctx, env.Bundle, env.ObservedOffset)
		case env.Commit != nil:
			_ = s.controller.ProcessCommit(env.Commit)
		case env.Certificate != nil:
			_ = s.controller.ProcessCertificate(env.Certificate)
		}
	}
}

// HandlePeerPhase1Bundle records a peer's Phase-1 bundle and runs host
// validity for the candidate value. Called by the dispatch layer when a
// KindPhase1Bundle envelope arrives from the network.
func (s *Scheduler) HandlePeerPhase1Bundle(ctx context.Context, b *obftcore.Phase1Bundle, observedOffset time.Duration) error {
	if b == nil {
		return errors.New("obft scheduler: nil phase-1 bundle")
	}
	if err := s.controller.ObservePhase1Bundle(b, observedOffset); err != nil {
		return fmt.Errorf("obft scheduler: observe phase-1 bundle: %w", err)
	}
	valid, err := s.hooks.HostValidate(ctx, phase0.Slot(b.Height), b.Layer, []byte(b.Value))
	if err != nil {
		return fmt.Errorf("obft scheduler: host validate (slot=%d layer=%d): %w", b.Height, b.Layer, err)
	}
	if err := s.controller.ApplyHostValidity(phase0.Slot(b.Height), b.Layer, []byte(b.Value), valid); err != nil {
		return fmt.Errorf("obft scheduler: apply host validity: %w", err)
	}
	return nil
}

// BuildAndBroadcastCommit executes the Phase-2 σ/NR commit step. Returns
// the Commit that was broadcast (so the caller may inspect it for tests).
//
// Per spec §Phase 2, called exactly once per slot at T_commit. The Commit
// bundles the operator's σ-side onion (one entry per σ-state layer) and
// NR-side partials (one per NR-state layer in [0, K-1)).
func (s *Scheduler) BuildAndBroadcastCommit(ctx context.Context, slot phase0.Slot) (*obftcore.Commit, error) {
	c, err := s.controller.BuildOwnCommit(slot)
	if err != nil {
		return nil, fmt.Errorf("obft scheduler: build own commit: %w", err)
	}
	data, err := wire.WrapCommit(c)
	if err != nil {
		return nil, fmt.Errorf("obft scheduler: wrap commit: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return nil, fmt.Errorf("obft scheduler: broadcast commit: %w", err)
	}
	return c, nil
}

// resolvePollInterval is how often Phase-3 Resolve is retried while waiting
// for σ-quorum to materialize. The spec describes Phase 3 as opportunistic
// from T_commit + Δ_2 onward — re-running on late KindCommit arrivals to
// salvage slots where partials trickled in past Δ_2. A 25ms interval is
// well below ε_3 (~100ms local CPU at Config A) and BTT (~200ms gossipsub),
// so polling overhead is negligible relative to the protocol's natural
// time-scales but tight enough to land submissions promptly when σ-quorum
// reaches.
const resolvePollInterval = 25 * time.Millisecond

// ResolveAndSubmit runs Phase-3 Resolve once (single attempt). On success:
// hands the Output to hooks.SubmitOutput, then (if BroadcastCertificate is
// set) broadcasts the final-certificate gossip message.
//
// On failure: tries falling back to a peer's Certificate (if RetainedCertificate
// returns one); else calls hooks.OnMissedSlot.
//
// This is the single-shot resolution path; production runners should prefer
// ResolveAndSubmitOpportunistically (which polls until σ-quorum reaches or
// the context is canceled) per spec §Phase 3.
func (s *Scheduler) ResolveAndSubmit(ctx context.Context, slot phase0.Slot) error {
	out, err := s.controller.Resolve(slot)
	if err != nil {
		// Local reconstruction failed — try peer-certificate fallback.
		if submitted := s.tryCertFastPath(ctx, slot); submitted {
			return nil
		}
		if s.hooks.OnMissedSlot != nil {
			s.hooks.OnMissedSlot(ctx, slot, err)
		}
		return fmt.Errorf("obft scheduler: resolve: %w", err)
	}

	return s.submitAndBroadcastCert(ctx, slot, out)
}

// tryCertFastPath attempts to submit a peer-broadcast Certificate as the
// fallback path. Per spec §Final-certificate gossip, receivers SHOULD re-run
// host application validity on V before submitting downstream — so we call
// HostValidate before SubmitOutput. Returns true if a cert was submitted
// successfully; false if there's no retained cert, host rejects V, or submit
// failed.
func (s *Scheduler) tryCertFastPath(ctx context.Context, slot phase0.Slot) bool {
	cert, err := s.controller.RetainedCertificate(slot)
	if err != nil || cert == nil {
		return false
	}
	// Re-run host validity on the cert's V. Layer is unknown for cert-decided
	// outputs (the cert carries V+sig but not the originating layer); pass
	// -1 so HostValidate hooks can short-circuit / treat as "any layer".
	valid, vErr := s.hooks.HostValidate(ctx, slot, -1, []byte(cert.Value))
	if vErr != nil || !valid {
		return false
	}
	fallbackOut := &obftcore.Output{
		Layer:     -1,
		Value:     []byte(cert.Value),
		Signature: []byte(cert.Signature),
	}
	return s.hooks.SubmitOutput(ctx, slot, fallbackOut) == nil
}

// ResolveAndSubmitOpportunistically polls Phase-3 Resolve until σ-quorum
// reaches (the operator's local reconstruction succeeds), a peer's
// Certificate arrives (alternative submission path via gossip), or ctx is
// canceled (the slot's relay-submission deadline is reached and the slot
// misses).
//
// Per spec §Phase 3 ("Re-running on late KindCommit arrivals"): late
// KindCommit messages can push σ-pool past qV at a layer that didn't
// reach on the initial walk, or push NR-pool past qEnc to unlock the next
// layer's chained decryption. Pigeonhole semantics still hold (at most one
// V reconstructs cluster-wide regardless of timing), so re-running is safe.
//
// This is the production reconstruction path. RoundEndOffset (= T_commit +
// Δ_2 + Δ_3) is a SOFT per-operator target — not a hard deadline; the hard
// wall is ctx (the relay-submission deadline).
func (s *Scheduler) ResolveAndSubmitOpportunistically(ctx context.Context, slot phase0.Slot) error {
	// Optimistic first attempt before allocating the ticker — the common
	// healthy path is "all KindCommits arrived within Δ_2 → σ-quorum on the
	// initial walk".
	if out, err := s.controller.Resolve(slot); err == nil {
		return s.submitAndBroadcastCert(ctx, slot, out)
	} else if !errors.Is(err, obftcore.ErrNoQuorum) {
		// Non-recoverable internal error (crypto failure, etc.).
		if s.hooks.OnMissedSlot != nil {
			s.hooks.OnMissedSlot(ctx, slot, err)
		}
		return fmt.Errorf("obft scheduler: resolve: %w", err)
	}

	// σ-quorum hasn't reached yet. Poll until either (a) Resolve succeeds,
	// (b) a peer's Certificate arrives, or (c) ctx fires.
	ticker := time.NewTicker(resolvePollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			// Submission deadline reached without σ-quorum — slot misses
			// for this operator. Try one final cert fallback before giving
			// up.
			if s.tryCertFastPath(ctx, slot) {
				return nil
			}
			if s.hooks.OnMissedSlot != nil {
				s.hooks.OnMissedSlot(ctx, slot, ctx.Err())
			}
			return fmt.Errorf("obft scheduler: resolve: %w", ctx.Err())

		case <-ticker.C:
			// Try peer-certificate fast path before re-running local
			// reconstruction. If a peer broadcasted (V, S), submit directly.
			if s.tryCertFastPath(ctx, slot) {
				return nil
			}

			out, err := s.controller.Resolve(slot)
			if err == nil {
				return s.submitAndBroadcastCert(ctx, slot, out)
			}
			if !errors.Is(err, obftcore.ErrNoQuorum) {
				// Non-recoverable internal error.
				if s.hooks.OnMissedSlot != nil {
					s.hooks.OnMissedSlot(ctx, slot, err)
				}
				return fmt.Errorf("obft scheduler: resolve: %w", err)
			}
			// Still no quorum — wait for the next tick, late KindCommit
			// arrival, or ctx.
		}
	}
}

// submitAndBroadcastCert hands a successfully-reconstructed Output to
// hooks.SubmitOutput and (if BroadcastCertificate is set) broadcasts the
// final-certificate gossip message. Shared between ResolveAndSubmit and
// ResolveAndSubmitOpportunistically.
func (s *Scheduler) submitAndBroadcastCert(ctx context.Context, slot phase0.Slot, out *obftcore.Output) error {
	if err := s.hooks.SubmitOutput(ctx, slot, out); err != nil {
		return fmt.Errorf("obft scheduler: submit output: %w", err)
	}

	if s.hooks.BroadcastCertificate != nil {
		cert, err := s.controller.BuildCertificate(slot, out)
		if err != nil {
			return fmt.Errorf("obft scheduler: build certificate: %w", err)
		}
		data, err := wire.WrapCertificate(cert)
		if err != nil {
			return fmt.Errorf("obft scheduler: wrap certificate: %w", err)
		}
		if err := s.hooks.BroadcastCertificate(ctx, slot, data); err != nil {
			return fmt.Errorf("obft scheduler: broadcast certificate: %w", err)
		}
	}
	return nil
}


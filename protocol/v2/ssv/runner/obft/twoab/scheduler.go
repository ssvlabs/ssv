package twoab

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"

	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	wire "github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// LifecycleHooks are callbacks the runner provides to the Scheduler. The runner
// controls *when* phases fire; the Scheduler executes the protocol mechanics and
// calls the appropriate hook for runner concerns (fetch / broadcast / submit).
// Mirrors the bare-OBFT LifecycleHooks; the only signature delta is
// OnReplayError's kind (twoab's own wire.MessageKind).
type LifecycleHooks struct {
	// FetchCandidate returns the candidate-value bytes the leader signs and
	// broadcasts for `layer` of `slot`. Required.
	FetchCandidate func(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error)

	// HostValidate returns the host application's valid/not-valid verdict on a
	// candidate value. Called at multiple points (Phase-1 bundle observe, the
	// peer-reflood-V harvest drain, and submit-time re-validation); the verdict
	// MUST be locked per (slot, layer, V) for the slot. Required.
	HostValidate func(ctx context.Context, slot phase0.Slot, layer int, value []byte) (bool, error)

	// Broadcast gossips a 2ab wire-envelope-wrapped message. Required.
	Broadcast func(ctx context.Context, slot phase0.Slot, data []byte) error

	// SubmitOutput is invoked when Resolve produces a signed Output. Required.
	SubmitOutput func(ctx context.Context, slot phase0.Slot, output *twoabcore.Output) error

	// BroadcastCertificate gossips a final Certificate after Resolve succeeds.
	// Optional.
	BroadcastCertificate func(ctx context.Context, slot phase0.Slot, data []byte) error

	// OnMissedSlot is invoked when the slot misses (no σ-quorum by deadline, or
	// a non-recoverable Resolve error). Optional.
	OnMissedSlot func(ctx context.Context, slot phase0.Slot, reason error)

	// OnReplayError is invoked once per pre-instance-replay envelope that
	// errored. Non-fatal telemetry. Optional.
	OnReplayError func(ctx context.Context, slot phase0.Slot, kind wire.MessageKind, err error)
}

func (h *LifecycleHooks) validate() error {
	if h == nil {
		return errors.New("twoab adapter: nil LifecycleHooks")
	}
	if h.FetchCandidate == nil {
		return errors.New("twoab adapter: LifecycleHooks.FetchCandidate is required")
	}
	if h.HostValidate == nil {
		return errors.New("twoab adapter: LifecycleHooks.HostValidate is required")
	}
	if h.Broadcast == nil {
		return errors.New("twoab adapter: LifecycleHooks.Broadcast is required")
	}
	if h.SubmitOutput == nil {
		return errors.New("twoab adapter: LifecycleHooks.SubmitOutput is required")
	}
	return nil
}

// Scheduler combines a Controller with LifecycleHooks to expose per-slot phase
// mechanics as procedural methods. The runner (RunProposerSlot) calls these at
// the right slot offsets — the Scheduler does NOT manage time itself.
type Scheduler struct {
	controller *Controller
	hooks      *LifecycleHooks

	fetchPollInterval time.Duration
	fetchBuildBuffer  time.Duration
}

const (
	defaultFetchPollInterval = 200 * time.Millisecond
	defaultFetchBuildBuffer  = 10 * time.Millisecond
)

// NewScheduler constructs a Scheduler.
func NewScheduler(controller *Controller, hooks *LifecycleHooks) (*Scheduler, error) {
	if controller == nil {
		return nil, errors.New("twoab adapter: nil Controller")
	}
	if err := hooks.validate(); err != nil {
		return nil, err
	}
	return &Scheduler{controller: controller, hooks: hooks}, nil
}

// SetFetchTiming overrides the iterative-fetch poll cadence and end-of-window
// build buffer. Zero keeps the defaults (200ms / 10ms). Negative is rejected.
func (s *Scheduler) SetFetchTiming(pollInterval, buildBuffer time.Duration) error {
	if pollInterval < 0 {
		return fmt.Errorf("twoab scheduler: pollInterval must be non-negative, got %v", pollInterval)
	}
	if buildBuffer < 0 {
		return fmt.Errorf("twoab scheduler: buildBuffer must be non-negative, got %v", buildBuffer)
	}
	s.fetchPollInterval = pollInterval
	s.fetchBuildBuffer = buildBuffer
	return nil
}

// Controller returns the underlying Controller.
func (s *Scheduler) Controller() *Controller { return s.controller }

// FetchAndBroadcastBundle executes Phase 1 for a layer where the local operator
// is the designated leader: iteratively poll the freshest candidate, build the
// Phase-1 bundle, self-feed it, apply host validity, and broadcast.
//
// Delta vs bare OBFT: twoab's BuildPhase1Bundle does NOT self-retain (it is
// retention-driven, not σ-lock-driven), so the runner MUST observe the leader's
// own bundle to seed retention and close its L0ReadyCh — otherwise the leader
// silently falls to the TPhase2a backstop instead of early-firing.
func (s *Scheduler) FetchAndBroadcastBundle(ctx context.Context, slot phase0.Slot, layer int) error {
	freshest, err := s.iterativeFetch(ctx, slot, layer)
	if err != nil {
		return err
	}

	bundle, err := s.controller.BuildPhase1Bundle(slot, layer, freshest)
	if err != nil {
		return fmt.Errorf("twoab scheduler: build phase-1 bundle: %w", err)
	}
	// Leader self-feed (see method doc): observe own bundle + apply host
	// validity so retention is seeded and L0ReadyCh closes.
	if err := s.controller.ObservePhase1Bundle(bundle, 0); err != nil {
		return fmt.Errorf("twoab scheduler: self-observe phase-1 bundle: %w", err)
	}
	if err := s.controller.ApplyHostValidity(slot, layer, freshest, true); err != nil {
		return fmt.Errorf("twoab scheduler: apply host validity (own bundle): %w", err)
	}

	data, err := wire.WrapPhase1Bundle(bundle)
	if err != nil {
		return fmt.Errorf("twoab scheduler: wrap phase-1 bundle: %w", err)
	}
	if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
		return fmt.Errorf("twoab scheduler: broadcast phase-1 bundle: %w", err)
	}
	return nil
}

// iterativeFetch polls hooks.FetchCandidate throughout the available window,
// returning the freshest non-error candidate. Honors ctx cancellation and the
// build-buffer reservation at the deadline. (Mirror of the bare-OBFT scheduler.)
func (s *Scheduler) iterativeFetch(ctx context.Context, slot phase0.Slot, layer int) ([]byte, error) {
	pollInterval := s.fetchPollInterval
	if pollInterval <= 0 {
		pollInterval = defaultFetchPollInterval
	}
	buildBuffer := s.fetchBuildBuffer
	if buildBuffer <= 0 {
		buildBuffer = defaultFetchBuildBuffer
	}

	deadline, hasDeadline := ctx.Deadline()
	var pollEnd time.Time
	if hasDeadline {
		pollEnd = deadline.Add(-buildBuffer)
	}

	var freshest []byte
	var lastErr error
	for {
		cand, err := s.hooks.FetchCandidate(ctx, slot, layer)
		if err == nil && cand != nil {
			freshest = cand
			lastErr = nil
		} else if err != nil {
			lastErr = err
		}

		if !hasDeadline {
			break
		}
		nextPoll := time.Now().Add(pollInterval)
		if !nextPoll.Before(pollEnd) {
			break
		}
		select {
		case <-ctx.Done():
			if freshest != nil {
				return freshest, nil
			}
			return nil, ctx.Err()
		case <-time.After(pollInterval):
		}
	}

	if freshest != nil {
		return freshest, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("twoab scheduler: iterative fetch (slot=%d layer=%d): all polls failed: %w", slot, layer, lastErr)
	}
	return nil, fmt.Errorf("twoab scheduler: iterative fetch (slot=%d layer=%d): no candidate after polling", slot, layer)
}

// FirePhase2a triggers the one-shot Phase-2a emission for the slot. It does NOT
// broadcast: the Controller signals a state delta and the resolve loop (the sole
// broadcaster) sends the resulting emission. The runner calls this once, at the
// earlier of L0ReadyCh closing or the TPhase2a backstop. A no-op if the op is
// not yet fireable or the slot is gone.
func (s *Scheduler) FirePhase2a(slot phase0.Slot) error {
	if _, _, _, err := s.controller.FirePhase2a(slot); err != nil {
		if errors.Is(err, ErrNoActiveInstance) {
			return nil
		}
		return fmt.Errorf("twoab scheduler: fire phase 2a: %w", err)
	}
	return nil
}

// emissionMarks tracks which of the op's own emissions have already been
// broadcast this slot. Owned by the resolve loop (the sole broadcaster); not
// safe for concurrent use.
//
// Set-once contract: each Own{ValueMsg,NoValueMsg,Commit} accessor returns
// exactly one envelope per slot. The mark-to-true-on-first-broadcast pattern
// in broadcastNewEmissions relies on this — see `MaybeBuildAndBroadcastCommit`
// at phase2b.go for the `ownCommit != nil → return nil, nil` idempotency
// guard that enforces it on the Instance side. A future "rebuild on state
// change" path that reassigns `ownCommit` to a different envelope would
// under-broadcast (the rebuild would never reach the wire because
// `marks.commit` is already true). If that pattern is ever needed, marks
// would have to track the envelope identity (content hash, generation
// counter, etc.) rather than a flat bool — adjust both this struct and the
// Instance-side guard at the same time.
type emissionMarks struct {
	value   bool
	noValue bool
	commit  bool
}

// broadcastNewEmissions broadcasts any of the op's own emissions — the Phase-2a
// terminal emission and any cascade-fired Phase-2a-late upgrade / Phase-2b
// Commit — that have not yet been broadcast, marking each on success. This is
// the dynamic-Phase-2b mechanism: there is no scheduled commit-build; emissions
// surface via the Instance's afterStateDelta cascade and are detected here by
// polling OwnValueMsg / OwnNoValueMsg / OwnCommit.
//
// Called ONLY from the resolve loop, so `marks` needs no synchronization. On a
// torn-down slot the OwnX accessors return ErrNoActiveInstance and this returns
// nil (nothing to broadcast).
func (s *Scheduler) broadcastNewEmissions(ctx context.Context, slot phase0.Slot, marks *emissionMarks) error {
	if !marks.value {
		v, err := s.controller.OwnValueMsg(slot)
		if err != nil {
			return nil // slot torn down
		}
		if v != nil {
			data, err := wire.WrapValueMsg(v)
			if err != nil {
				return fmt.Errorf("twoab scheduler: wrap value msg: %w", err)
			}
			if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
				return fmt.Errorf("twoab scheduler: broadcast value msg: %w", err)
			}
			marks.value = true
		}
	}
	if !marks.noValue {
		nv, err := s.controller.OwnNoValueMsg(slot)
		if err != nil {
			return nil
		}
		if nv != nil {
			data, err := wire.WrapNoValueMsg(nv)
			if err != nil {
				return fmt.Errorf("twoab scheduler: wrap no-value msg: %w", err)
			}
			if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
				return fmt.Errorf("twoab scheduler: broadcast no-value msg: %w", err)
			}
			marks.noValue = true
		}
	}
	if !marks.commit {
		c, err := s.controller.OwnCommit(slot)
		if err != nil {
			return nil
		}
		if c != nil {
			data, err := wire.WrapCommit(c)
			if err != nil {
				return fmt.Errorf("twoab scheduler: wrap commit: %w", err)
			}
			if err := s.hooks.Broadcast(ctx, slot, data); err != nil {
				return fmt.Errorf("twoab scheduler: broadcast commit: %w", err)
			}
			marks.commit = true
		}
	}
	return nil
}

// DrainPending replays envelopes buffered on the Controller for `slot` (peers
// that broadcast before this operator's instance started). Replays all five 2ab
// kinds in arrival order; replay errors are non-fatal and surface via the
// optional OnReplayError hook. Called pre-fire, so no own emissions result.
func (s *Scheduler) DrainPending(ctx context.Context, slot phase0.Slot) {
	for _, env := range s.controller.DrainPending(slot) {
		var (
			err  error
			kind wire.MessageKind
		)
		switch {
		case env.Bundle != nil:
			err = s.HandlePeerPhase1Bundle(ctx, env.Bundle, env.ObservedOffset)
			kind = wire.KindPhase1Bundle
		case env.ValueMsg != nil:
			err = s.controller.ProcessValueMsg(env.ValueMsg)
			kind = wire.KindValue
		case env.NoValueMsg != nil:
			err = s.controller.ProcessNoValueMsg(env.NoValueMsg)
			kind = wire.KindNoValue
		case env.Commit != nil:
			err = s.controller.ProcessCommit(env.Commit)
			kind = wire.KindCommit
		case env.Certificate != nil:
			err = s.controller.ProcessCertificate(env.Certificate)
			kind = wire.KindCertificate
		}
		if err != nil && s.hooks.OnReplayError != nil {
			s.hooks.OnReplayError(ctx, slot, kind, err)
		}
	}
}

// HandlePeerPhase1Bundle records a peer's Phase-1 bundle and runs host validity
// for the candidate value. Called by the dispatch layer on a KindPhase1Bundle
// arrival.
func (s *Scheduler) HandlePeerPhase1Bundle(ctx context.Context, b *twoabcore.Phase1Bundle, observedOffset time.Duration) error {
	if b == nil {
		return errors.New("twoab scheduler: nil phase-1 bundle")
	}
	if err := s.controller.ObservePhase1Bundle(b, observedOffset); err != nil {
		return fmt.Errorf("twoab scheduler: observe phase-1 bundle: %w", err)
	}
	valid, err := s.hooks.HostValidate(ctx, phase0.Slot(b.Height), b.Layer, []byte(b.Value))
	if err != nil {
		return fmt.Errorf("twoab scheduler: host validate (slot=%d layer=%d): %w", b.Height, b.Layer, err)
	}
	if err := s.controller.ApplyHostValidity(phase0.Slot(b.Height), b.Layer, []byte(b.Value), valid); err != nil {
		return fmt.Errorf("twoab scheduler: apply host validity: %w", err)
	}
	return nil
}

// The Phase-2a/2b/cert kinds (KindValue / KindNoValue / KindCommit /
// KindCertificate) have no scheduler-side concern (no host-validate, unlike
// Phase-1 bundles), so the dispatcher routes them straight to the Controller's
// Process* methods — mirroring the bare-OBFT dispatcher. See dispatch.go.

// RunHostValidationDrain drains the Instance's host-validation request channel
// (the peer-reflood-V harvest path) until ctx is done or the channel closes
// (EndInstance). For each request it runs HostValidate and ApplyHostValidity;
// ApplyHostValidity signals a state delta so a cascade emission triggered by the
// verdict is broadcast by the resolve loop. Intended to run as a per-slot
// goroutine alongside ResolveAndSubmitOpportunistically.
func (s *Scheduler) RunHostValidationDrain(ctx context.Context, slot phase0.Slot) {
	ch := s.controller.WantsHostValidationCh(slot)
	for {
		select {
		case <-ctx.Done():
			return
		case req, ok := <-ch:
			if !ok {
				return
			}
			valid, err := s.hooks.HostValidate(ctx, slot, req.Layer, []byte(req.Value))
			if err != nil {
				continue
			}
			if err := s.controller.ApplyHostValidity(slot, req.Layer, []byte(req.Value), valid); err != nil {
				// Slot torn down or verdict locked — drop; the loop continues
				// (or exits on the next ctx/closed-channel signal).
				continue
			}
		}
	}
}

// ResolveAndSubmitOpportunistically is the per-slot observer-mode loop and the
// sole broadcaster of the op's own emissions. On each state delta it (1)
// broadcasts any newly-fired own emission (dynamic Phase 2b), (2) tries the
// peer-certificate fast path, and (3) re-runs Resolve. Exits on σ-quorum (local
// reconstruction submitted), a peer certificate, or ctx cancellation (the
// relay-submission deadline → slot misses).
//
// Pre-submit re-broadcast (see the two `broadcastNewEmissions` calls immediately
// before `submitAndBroadcastCert`): the iteration body releases `instanceMu`
// between `broadcastNewEmissions`, `tryCertFastPath`, and `Resolve` (each
// acquires the lock separately via the Controller helpers). Under heavy
// concurrency (`-race` × heavy GOMAXPROCS) a peer dispatch can squeeze a
// cascade-fired Phase-2b NR-Commit into the gap: the iteration's first
// `broadcastNewEmissions` reads `OwnCommit == nil`, then between that read and
// `Resolve`'s lock acquisition the cascade sets `ownCommit` + grows
// `nrTagPool`, and subsequent peer Commit observations push the pool to qEnc.
// `Resolve` then decides without ever broadcasting our own Commit, peers
// stall one NR-partial short, and the slot misses. Re-running
// `broadcastNewEmissions` immediately before `submitAndBroadcastCert` closes
// the window: if the cascade fired during the iteration body, `ownCommit` is
// set and the re-broadcast emits it before submission; on decision paths that
// never set `ownCommit` (e.g. L_0 σ-quorum), the re-broadcast is a harmless
// no-op.
func (s *Scheduler) ResolveAndSubmitOpportunistically(ctx context.Context, slot phase0.Slot) error {
	marks := &emissionMarks{}
	// Acquire the delta channel before the optimistic attempt so no arrival is
	// lost between the attempt and the first blocking receive.
	deltas := s.controller.StateDeltaChan(slot)

	// Broadcast anything already fired (e.g. the Phase-2a emission), then make
	// the optimistic first Resolve attempt.
	if err := s.broadcastNewEmissions(ctx, slot, marks); err != nil {
		return err
	}
	if out, err := s.controller.Resolve(slot); err == nil {
		if err := s.broadcastNewEmissions(ctx, slot, marks); err != nil {
			return err
		}
		return s.submitAndBroadcastCert(ctx, slot, out)
	} else if !errors.Is(err, twoabcore.ErrNoQuorum) {
		if s.hooks.OnMissedSlot != nil {
			s.hooks.OnMissedSlot(ctx, slot, err)
		}
		return fmt.Errorf("twoab scheduler: resolve: %w", err)
	}

	for {
		select {
		case <-ctx.Done():
			if s.tryCertFastPath(ctx, slot) {
				return nil
			}
			if s.hooks.OnMissedSlot != nil {
				s.hooks.OnMissedSlot(ctx, slot, ctx.Err())
			}
			return fmt.Errorf("twoab scheduler: resolve: %w", ctx.Err())

		case _, ok := <-deltas:
			if !ok {
				// Channel closed by EndInstance — only ctx.Done() remains.
				deltas = nil
				continue
			}
			if err := s.broadcastNewEmissions(ctx, slot, marks); err != nil {
				return err
			}
			if s.tryCertFastPath(ctx, slot) {
				return nil
			}
			out, err := s.controller.Resolve(slot)
			if err == nil {
				if err := s.broadcastNewEmissions(ctx, slot, marks); err != nil {
					return err
				}
				return s.submitAndBroadcastCert(ctx, slot, out)
			}
			if !errors.Is(err, twoabcore.ErrNoQuorum) {
				if s.hooks.OnMissedSlot != nil {
					s.hooks.OnMissedSlot(ctx, slot, err)
				}
				return fmt.Errorf("twoab scheduler: resolve: %w", err)
			}
		}
	}
}

// tryCertFastPath submits a peer-broadcast Certificate as a fallback. Re-runs
// host validity on the cert's V before submitting (layer unknown → -1). Returns
// true iff a cert was submitted.
func (s *Scheduler) tryCertFastPath(ctx context.Context, slot phase0.Slot) bool {
	cert, err := s.controller.RetainedCertificate(slot)
	if err != nil || cert == nil {
		return false
	}
	valid, vErr := s.hooks.HostValidate(ctx, slot, -1, []byte(cert.Value))
	if vErr != nil || !valid {
		return false
	}
	fallbackOut := &twoabcore.Output{
		Layer:     -1,
		Value:     []byte(cert.Value),
		Signature: []byte(cert.Signature),
	}
	return s.hooks.SubmitOutput(ctx, slot, fallbackOut) == nil
}

// submitAndBroadcastCert hands a reconstructed Output to SubmitOutput and (if
// BroadcastCertificate is set) gossips the final certificate. Re-runs
// HostValidate on the decided V before submitting (reorg between σ-quorum and
// submit wastes the slot).
func (s *Scheduler) submitAndBroadcastCert(ctx context.Context, slot phase0.Slot, out *twoabcore.Output) error {
	if out == nil {
		return fmt.Errorf("twoab scheduler: nil output")
	}
	valid, err := s.hooks.HostValidate(ctx, slot, out.Layer, []byte(out.Value))
	if err != nil {
		return fmt.Errorf("twoab scheduler: host validate at submit (slot=%d): %w", slot, err)
	}
	if !valid {
		reason := fmt.Errorf("twoab scheduler: decided V no longer host-valid (slot=%d)", slot)
		if s.hooks.OnMissedSlot != nil {
			s.hooks.OnMissedSlot(ctx, slot, reason)
		}
		return reason
	}

	if err := s.hooks.SubmitOutput(ctx, slot, out); err != nil {
		return fmt.Errorf("twoab scheduler: submit output: %w", err)
	}

	if s.hooks.BroadcastCertificate != nil {
		cert, err := s.controller.BuildCertificate(slot, out)
		if err != nil {
			return fmt.Errorf("twoab scheduler: build certificate: %w", err)
		}
		data, err := wire.WrapCertificate(cert)
		if err != nil {
			return fmt.Errorf("twoab scheduler: wrap certificate: %w", err)
		}
		if err := s.hooks.BroadcastCertificate(ctx, slot, data); err != nil {
			return fmt.Errorf("twoab scheduler: broadcast certificate: %w", err)
		}
	}
	return nil
}

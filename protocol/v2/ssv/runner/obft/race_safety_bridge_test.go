package obft

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftcore "github.com/ssvlabs/ssv/protocol/v2/obft/base"
	"github.com/ssvlabs/ssv/protocol/v2/obft/base/wire"
)

// Race-safety bridge: wraps an existing OBFT runner test-cluster setup
// with a wire-tap on the broadcast bus + a post-slot reconstruction of
// a ct.Outcome, so consensustest's 10 safety invariants can be applied
// to the wire-state produced by real-goroutine scheduling of the
// production runner.
//
// Design: docs/RUNNER-RACE-SAFETY-PLAN.md § Architecture.
//
// This file implements the bridge foundation (commit 1 of the plan) +
// a single-cell Healthy smoke test that validates the architecture end-
// to-end at n=4 / K=4. Matrix parameterization (commit 2), additional
// scenarios (commit 3), and the Makefile target (commit 7) come in
// subsequent commits.

// recordingBroadcastBus wraps an existing *broadcastBus, decoding +
// capturing every emission's typed envelope before forwarding to the
// inner bus unchanged. Thread-safe — the inner bus's goroutine fan-out
// for delivery is preserved; the recording side has its own mutex
// protecting the captured slice.
//
// `from` in the broadcast call is the genuine network-level emitter
// (every node's hook routes its own emissions here via n.op). Stays
// authoritative even under future byz-like patterns that might forge
// the wire envelope's claimed-sender field — the production runner
// doesn't have such patterns today, but the bridge's by-emitter
// recording uses `from` so we'd be ready.
type recordingBroadcastBus struct {
	inner    *broadcastBus
	mu       sync.Mutex
	captured []capturedEmission
}

// capturedEmission records one wire emission. The envelope's Kind
// discriminates which of the three typed body fields is populated.
type capturedEmission struct {
	from     spectypes.OperatorID
	envelope *wire.Envelope
}

func newRecordingBroadcastBus(inner *broadcastBus) *recordingBroadcastBus {
	return &recordingBroadcastBus{inner: inner}
}

// broadcast intercepts every emission: decode via wire.Unwrap, append
// (from, envelope) to captured, then delegate to the inner bus
// unchanged. Decode failures (malformed envelope) are silently
// skipped — the inner bus will fail dispatch on its own and the test
// assertion catches it via the "no error" expectation.
func (rb *recordingBroadcastBus) broadcast(from spectypes.OperatorID, data []byte) {
	if env, err := wire.Unwrap(data); err == nil && env != nil {
		rb.mu.Lock()
		rb.captured = append(rb.captured, capturedEmission{
			from:     from,
			envelope: env,
		})
		rb.mu.Unlock()
	}
	rb.inner.broadcast(from, data)
}

// snapshot returns a copy of the captured emissions. Safe to call
// post-slot (after the inner bus's stop() drains in-flight goroutines).
// The copy is shallow — envelopes themselves are immutable typed
// pointers shared with capturing-time observers, fine for the bridge's
// read-only reconstruction path.
func (rb *recordingBroadcastBus) snapshot() []capturedEmission {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]capturedEmission, len(rb.captured))
	copy(out, rb.captured)
	return out
}

// recordPhase1BundleWire mirrors consensustest/obft/events.go's leader-
// broadcast aggregator-observation logic for the production wire's
// *obftcore.Phase1Bundle. Both record the same paths:
//   - Claimed-sender path (ObserveSigma): keyed on bundle.OperatorID
//     (the configured leader at this layer).
//   - By-emitter path (ObserveSigmaByEmitter): keyed on the genuine
//     network-level emitter, regardless of any claimed-sender forgery
//     (the production runner doesn't forge today; the by-emitter
//     recording is forward-looking).
func recordPhase1BundleWire(agg *ct.OfflineAggregator, emitter spectypes.OperatorID, b *obftcore.Phase1Bundle) {
	if b == nil {
		return
	}
	agg.ObserveSigma(ct.OperatorID(b.OperatorID), b.Layer, b.Value)
	agg.ObserveSigmaByEmitter(ct.OperatorID(emitter), b.Layer, b.Value)
}

// recordCommitWire mirrors consensustest/obft/events.go's
// recordCommitToAggregator on the production wire's *obftcore.Commit.
// The two implementations are intentionally aligned — both consume the
// same underlying obftcore.Commit type and apply the same observation
// rules (plaintext σ at L_0, encrypted-claim σ at L_k>0, NR partials
// at any layer < K-1, witnesses keyed on the witnessed-leader identity
// in the claimed-sender path only — by-emitter intentionally skips
// witnesses since they're peer-forwards, not the emitter's own σ-side
// commitment).
func recordCommitWire(agg *ct.OfflineAggregator, emitter spectypes.OperatorID, c *obftcore.Commit) {
	if c == nil {
		return
	}
	from := ct.OperatorID(c.OperatorID)
	em := ct.OperatorID(emitter)
	for layer, el := range c.Layers {
		if len(el.Value) == 0 {
			continue
		}
		if layer == 0 {
			agg.ObserveSigma(from, layer, el.Value)
		} else {
			agg.ObserveEncryptedClaim(from, layer, el.Value)
		}
		agg.ObserveSigmaByEmitter(em, layer, el.Value)
	}
	for _, nr := range c.NRPartials {
		agg.ObserveNR(from, nr.Layer)
		agg.ObserveNRByEmitter(em, nr.Layer)
	}
	for _, w := range c.Witnesses {
		agg.ObserveSigmaByValueRoot(ct.OperatorID(w.Leader), w.Layer, w.ValueRoot)
	}
}

// extractInstanceTrace pulls Instance.LastResolveLayerAttempts via
// internal Controller access. Holds both the Controller's mu (for the
// instances-map lookup) and the RunningInstance's instanceMu (so the
// LastResolveLayerAttempts read is serialized against any in-progress
// Resolve goroutine — defensive under -race even after wg.Wait()).
// Returns nil if no Instance was ever created for this slot (e.g.,
// non-leader op in a scenario that never received Phase-1).
func extractInstanceTrace(node *runnerNode, slot phase0.Slot) []obftcore.LayerAttempt {
	node.ctrl.mu.Lock()
	ri, ok := node.ctrl.instances[slot]
	node.ctrl.mu.Unlock()
	if !ok || ri == nil {
		return nil
	}
	ri.instanceMu.Lock()
	defer ri.instanceMu.Unlock()
	if ri.instance == nil {
		return nil
	}
	return ri.instance.LastResolveLayerAttempts()
}

// reconstructOutcome translates the captured wire trace + per-Instance
// state into a ct.Outcome that consensustest.ComputeSafetyReport can
// consume. Mirrors the DES adapter's outcome() in spirit:
//
//   - Replay captured emissions into a fresh OfflineAggregator.
//   - Per-op: read submittedOutput() and LastResolveLayerAttempts().
//   - Distinguish cert-gossip-decide (oo.Round = -1) from local-decide
//     by checking the trace — if local Resolve never reached a Decided
//     layer but the op still submitted, it was a cert-driven submission
//     (matches the DES adapter's Round=-1 convention).
//
// Byz is intentionally zero-valued: production has no byz-pattern
// injection, so every op is treated as honest by the per-op invariant
// checks (B1/B2/D1/C3).
func reconstructOutcome(nodes []*runnerNode, slot phase0.Slot, captured []capturedEmission) ct.Outcome {
	n := len(nodes)
	agg := ct.NewOfflineAggregator(n)
	for _, em := range captured {
		switch em.envelope.Kind {
		case wire.KindPhase1Bundle:
			recordPhase1BundleWire(agg, em.from, em.envelope.Phase1Bundle)
		case wire.KindCommit:
			recordCommitWire(agg, em.from, em.envelope.Commit)
		case wire.KindCertificate:
			// Certs propagate the already-reconstructed full signature;
			// they don't add to σ/NR pools. No aggregator update.
		}
	}

	perOp := make(map[ct.OperatorID]ct.OperatorOutcome, n)
	for _, node := range nodes {
		oo := ct.OperatorOutcome{
			Round: -1,
		}
		if out := node.submittedOutput(); out != nil {
			oo.Decided = true
			oo.Value = append([]byte(nil), out.Value...)
			oo.Round = out.Layer
		}
		if trace := extractInstanceTrace(node, slot); len(trace) > 0 {
			oo.ResolveLayerAttempts = convertLayerAttempts(trace)
			// If local Resolve never produced a Decided layer but the op
			// still submitted, the submit came via the cert-gossip path.
			// Mirror the DES adapter's Round=-1 convention so D1's
			// case-(a) cert-gossip branch + clusterLocalDecidedOn fires
			// correctly.
			if oo.Decided && !traceHasDecided(trace) {
				oo.Round = -1
			}
		} else if oo.Decided {
			// No trace at all but submitted = cert-gossip-decide before
			// any local Resolve ran. Same Round=-1 convention.
			oo.Round = -1
		}
		perOp[ct.OperatorID(node.op)] = oo
	}

	return ct.Outcome{
		Decided:      anyDecided(perOp),
		DecidedValue: pickClusterValue(perOp),
		DecidedRound: pickClusterRound(perOp),
		PerOp:        perOp,
		OfflineAgg:   agg.AttemptAll(),
		Byz:          ct.ByzPattern{},
	}
}

func traceHasDecided(trace []obftcore.LayerAttempt) bool {
	for _, la := range trace {
		if la.Decided {
			return true
		}
	}
	return false
}

func convertLayerAttempts(in []obftcore.LayerAttempt) []ct.LayerAttempt {
	out := make([]ct.LayerAttempt, len(in))
	for i, la := range in {
		out[i] = ct.LayerAttempt{
			Layer:         la.Layer,
			SigmaPoolSize: la.SigmaPoolSize,
			QV:            la.QV,
			SigmaReached:  la.SigmaReached,
			Decided:       la.Decided,
			NRPoolSize:    la.NRPoolSize,
			QEnc:          la.QEnc,
			NRReached:     la.NRReached,
		}
	}
	return out
}

func anyDecided(perOp map[ct.OperatorID]ct.OperatorOutcome) bool {
	for _, oo := range perOp {
		if oo.Decided {
			return true
		}
	}
	return false
}

// pickClusterValue picks any decided op's Value as the cluster-wide
// decided V. Prefers local-decider (Round >= 0) for the trace-aligned
// reconstruction; falls back to cert-gossip-decider if no local
// decider exists.
func pickClusterValue(perOp map[ct.OperatorID]ct.OperatorOutcome) []byte {
	for _, oo := range perOp {
		if oo.Decided && oo.Round >= 0 {
			return append([]byte(nil), oo.Value...)
		}
	}
	for _, oo := range perOp {
		if oo.Decided {
			return append([]byte(nil), oo.Value...)
		}
	}
	return nil
}

func pickClusterRound(perOp map[ct.OperatorID]ct.OperatorOutcome) int {
	for _, oo := range perOp {
		if oo.Decided && oo.Round >= 0 {
			return oo.Round
		}
	}
	return -1
}

// TestSafetyBridge_OBFT_Healthy_n4_K4 — commit-1 smoke test. Runs the
// healthy scenario through the production runner cluster, captures the
// wire trace + per-Instance state, reconstructs a ct.Outcome, asserts
// the 10 consensustest safety invariants hold under real-goroutine
// scheduling.
//
// Validates the bridge end-to-end at the canonical single cell
// (n=4 / K=4) before commit 2 scales it across the (n, K) matrix and
// commit 3 adds the SilentL0Leader_NRFallThrough scenario.
func TestSafetyBridge_OBFT_Healthy_n4_K4(t *testing.T) {
	budget, fetchAt := compressedTestSchedule(t)
	overrides := &ConfigOverrides{
		K:               4,
		tCommitOverride: 200 * time.Millisecond,
		delta2Override:  60 * time.Millisecond,
		eps3Override:    60 * time.Millisecond,
		BTT:             30 * time.Millisecond,
		FetchAt:         fetchAt,
		BroadcastBudget: budget,
	}

	nodes := buildCluster(t, 4, overrides)

	const slot = phase0.Slot(17)
	slotStart := time.Now()

	bus := newBroadcastBus(nodes, slotStart)
	recordingBus := newRecordingBroadcastBus(bus)
	defer bus.stop()
	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			recordingBus.broadcast(n.op, data)
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := RunProposerSlot(ctx, n.sched, slot, slotStart)
			require.NoErrorf(t, err, "op %d RunProposerSlot", n.op)
		}()
	}
	wg.Wait()

	// Sanity: every op submitted an output. The bridge's per-op
	// reconstruction depends on this; a missing op would mean the
	// scenario didn't complete and the safety check would test an
	// incomplete state.
	for _, n := range nodes {
		require.NotNilf(t, n.submittedOutput(), "op %d submitted no output", n.op)
	}

	// Bridge entry-point: reconstruct + assert.
	outcome := reconstructOutcome(nodes, slot, recordingBus.snapshot())
	rep := ct.ComputeSafetyReport(outcome)
	require.Falsef(t, rep.IsViolation(),
		"safety violation under real-runner scheduling: %s", rep)
}

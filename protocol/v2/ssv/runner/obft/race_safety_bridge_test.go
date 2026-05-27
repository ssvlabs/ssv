package obft

import (
	"context"
	"fmt"
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

// matrixCell — (n, K) parameterization of a runner cluster test.
type matrixCell struct {
	n int
	K int
}

// obftMatrixCells returns the canonical OBFT-family cluster matrix:
// n ∈ {4, 7} × K ∈ {f+1..N} = 8 cells. Spans the BFT-liveness floor
// (K=f+1) through the maximum-fall-through depth (K=n) for each
// cluster size.
func obftMatrixCells() []matrixCell {
	return []matrixCell{
		{4, 2}, {4, 3}, {4, 4},
		{7, 3}, {7, 4}, {7, 5}, {7, 6}, {7, 7},
	}
}

// compressedTestScheduleForK is the K-parameterized variant of the
// existing compressedTestSchedule. Uses the same compressed timing
// (TCommit=200ms, BTT=30ms, SafetyBuffer=0) but produces (FetchAt,
// BroadcastBudget) slices of the requested length. L_0 fetches at
// 100ms; backups L_1..L_{K-1} fetch at slot start (0).
func compressedTestScheduleForK(t *testing.T, K int) (broadcastBudget, fetchAt []time.Duration) {
	t.Helper()
	var err error
	broadcastBudget, err = DefaultBroadcastBudgetSchedule(K, 30*time.Millisecond, 0, 200*time.Millisecond)
	require.NoError(t, err)
	fetchAt = make([]time.Duration, K)
	if K > 0 {
		fetchAt[0] = 100 * time.Millisecond
	}
	return
}

// lateCommitDelayPredicate returns a delayFn that delays KindCommit
// arrivals at the victim op from the highest-numbered (n - qV + 1)
// peers, leaving the victim's timely σ-pool short of qV until the
// delayed commits arrive. Mirrors the regression-shape of the existing
// hardcoded "from == 3 || from == 4" predicate (n=4 / qV=3 → delay 2
// peers) but parameterized over (n, qV) so it works at any cell.
//
// At n=4 / qV=3: delays ops 3, 4 (2 peers); leaves op2 timely + own
// self-observation = 2 partials, < qV=3 — opportunistic resolve must
// salvage when the delayed commits arrive.
//
// At n=7 / qV=5: delays ops 5, 6, 7 (3 peers); leaves ops 2-4 timely
// + own = 4 partials, < qV=5 — same regression shape, salvage path
// must still work.
func lateCommitDelayPredicate(n int, victim spectypes.OperatorID, delay time.Duration) delayFn {
	f := (n - 1) / 3
	qV := 2*f + 1
	delayCount := n - qV + 1
	firstDelayedOp := spectypes.OperatorID(n - delayCount + 1)
	return func(from, to spectypes.OperatorID, kind byte) time.Duration {
		if kind == byte(wire.KindCommit) && to == victim && from >= firstDelayedOp && int(from) <= n {
			return delay
		}
		return 0
	}
}

// scenarioConfig parameterizes a runner scenario for the bridge:
// runner overrides (per (n, K) cell), bus shape (with or without
// delay), and ctx timeout. The bridge's runScenarioWithSafetyCheck
// runs the scenario at the given cell and asserts safety.
type scenarioConfig struct {
	name      string
	timeout   time.Duration
	overrides func(t *testing.T, cell matrixCell) *ConfigOverrides
	// delayFor returns the bus's delayFn for the given cell; nil → no
	// delay (use newBroadcastBus instead of newBroadcastBusWithDelay).
	delayFor func(cell matrixCell) delayFn
}

// healthyScenarioConfig: nominal-conditions cluster. All ops receive
// L_0 bundle on time, all hosts valid. Bridge asserts safety on the
// resulting wire. Subsumes the OpportunisticTiming wire-shape (same
// config + no delay) — the OpportunisticTiming scenario's distinct
// regression class is exercised by its dedicated TestRunProposerSlot
// test which asserts the elapsed-time property; the bridge variant
// only adds the safety-check overlay.
func healthyScenarioConfig() scenarioConfig {
	return scenarioConfig{
		name:    "Healthy",
		timeout: 3 * time.Second,
		overrides: func(t *testing.T, cell matrixCell) *ConfigOverrides {
			budget, fetchAt := compressedTestScheduleForK(t, cell.K)
			return &ConfigOverrides{
				K:               cell.K,
				tCommitOverride: 200 * time.Millisecond,
				delta2Override:  60 * time.Millisecond,
				eps3Override:    60 * time.Millisecond,
				BTT:             30 * time.Millisecond,
				FetchAt:         fetchAt,
				BroadcastBudget: budget,
			}
		},
	}
}

// opportunisticTimingScenarioConfig: same wire shape as Healthy.
// Wrapped under a distinct test name so the bridge's coverage matrix
// explicitly documents that safety invariants hold under the
// observer-mode timing config (no regression cascades safety into
// scope here, but the symmetric scenario set is the right shape for
// future regression classes that might).
func opportunisticTimingScenarioConfig() scenarioConfig {
	cfg := healthyScenarioConfig()
	cfg.name = "OpportunisticTiming"
	return cfg
}

// lateCommitScenarioConfig: delays a (n, qV)-parameterized subset of
// KindCommit arrivals at op1 (the canonical victim) past
// RoundEndOffset. Exercises the runner's opportunistic poll path —
// safety must hold even when the cluster reaches σ-quorum strictly
// after the soft deadline.
func lateCommitScenarioConfig() scenarioConfig {
	return scenarioConfig{
		name:    "LateCommit",
		timeout: 3 * time.Second,
		overrides: func(t *testing.T, cell matrixCell) *ConfigOverrides {
			budget, fetchAt := compressedTestScheduleForK(t, cell.K)
			return &ConfigOverrides{
				K:               cell.K,
				tCommitOverride: 200 * time.Millisecond,
				delta2Override:  60 * time.Millisecond,
				eps3Override:    60 * time.Millisecond,
				BTT:             30 * time.Millisecond,
				FetchAt:         fetchAt,
				BroadcastBudget: budget,
			}
		},
		delayFor: func(cell matrixCell) delayFn {
			const lateCommitDelay = 500 * time.Millisecond
			const victim spectypes.OperatorID = 1
			return lateCommitDelayPredicate(cell.n, victim, lateCommitDelay)
		},
	}
}

// runScenarioWithSafetyCheck — bridge entry-point. Builds the cluster
// at (n, K), runs the scenario via the existing RunProposerSlot
// fixture but with the recordingBroadcastBus wire-tap installed,
// reconstructs the Outcome post-slot, asserts
// ComputeSafetyReport.IsViolation() == false.
//
// Slot id is derived from the cell + scenario name to avoid any
// cross-test state collision under -count=N.
func runScenarioWithSafetyCheck(t *testing.T, cell matrixCell, cfg scenarioConfig) {
	t.Helper()
	overrides := cfg.overrides(t, cell)
	nodes := buildCluster(t, cell.n, overrides)

	slot := phase0.Slot(100 + cell.n*10 + cell.K)
	slotStart := time.Now()

	var bus *broadcastBus
	if cfg.delayFor != nil {
		bus = newBroadcastBusWithDelay(nodes, slotStart, cfg.delayFor(cell))
	} else {
		bus = newBroadcastBus(nodes, slotStart)
	}
	recordingBus := newRecordingBroadcastBus(bus)
	defer bus.stop()
	for _, n := range nodes {
		n := n
		n.hooks.broadcastFn = func(ctx context.Context, slot phase0.Slot, data []byte) error {
			recordingBus.broadcast(n.op, data)
			return nil
		}
	}

	ctx, cancel := context.WithTimeout(context.Background(), cfg.timeout)
	defer cancel()

	var wg sync.WaitGroup
	for _, n := range nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := RunProposerSlot(ctx, n.sched, slot, slotStart)
			require.NoErrorf(t, err, "op %d RunProposerSlot at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
		}()
	}
	wg.Wait()

	for _, n := range nodes {
		require.NotNilf(t, n.submittedOutput(),
			"op %d submitted no output at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
	}

	outcome := reconstructOutcome(nodes, slot, recordingBus.snapshot())
	rep := ct.ComputeSafetyReport(outcome)
	require.Falsef(t, rep.IsViolation(),
		"safety violation at %s n=%d K=%d: %s", cfg.name, cell.n, cell.K, rep)
}

// TestSafetyBridge_OBFT_Healthy iterates the (n, K) matrix and runs
// the Healthy scenario at each cell with the safety-bridge overlay.
// Subsumes the commit-1 single-cell smoke test (n=4 / K=4 is the
// first matrix entry).
func TestSafetyBridge_OBFT_Healthy(t *testing.T) {
	cfg := healthyScenarioConfig()
	for _, cell := range obftMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

// TestSafetyBridge_OBFT_OpportunisticTiming iterates the matrix with
// the OpportunisticTiming config. Wire shape identical to Healthy
// under non-regression code; the named scenario documents that safety
// holds at the observer-mode timing point.
func TestSafetyBridge_OBFT_OpportunisticTiming(t *testing.T) {
	cfg := opportunisticTimingScenarioConfig()
	for _, cell := range obftMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

// TestSafetyBridge_OBFT_LateCommit iterates the matrix with the
// LateCommit config (delays a parameterized subset of KindCommits to
// op1 past RoundEndOffset). Safety must hold under the runner's
// opportunistic-poll re-resolve path.
func TestSafetyBridge_OBFT_LateCommit(t *testing.T) {
	cfg := lateCommitScenarioConfig()
	for _, cell := range obftMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

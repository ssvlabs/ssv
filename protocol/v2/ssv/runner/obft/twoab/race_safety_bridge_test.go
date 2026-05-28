package twoab

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	twoabcore "github.com/ssvlabs/ssv/protocol/v2/obft/twoab"
	"github.com/ssvlabs/ssv/protocol/v2/obft/twoab/wire"
)

// Race-safety bridge for the 2abOBFT runner. Mirror of OBFT base's
// race_safety_bridge_test.go: wraps an existing 2abOBFT runner test-
// cluster setup with a wire-tap on the blsBus + a post-slot
// reconstruction of a ct.Outcome, so consensustest's 10 safety
// invariants can be applied to the wire-state produced by real-
// goroutine scheduling of the production runner. See the Makefile's
// runner-safety-stress target for stress amplification.
//
// File contents:
//
//   - recordingBlsBus + capturedEmission: wire-tap that decodes
//     every emission via wire.Unwrap and records the typed envelope
//     before forwarding to the inner blsBus.
//   - recordValueMsgWire / recordNoValueMsgWire / recordCommitWire:
//     production-wire-side translators mirroring
//     consensustest/twoab/events.go's OfflineAggregator observations.
//     KindPhase1Bundle is INTENTIONALLY NOT recorded — see
//     reconstructOutcome's docstring for the rationale (avoid double-
//     counting the leader's σ contribution, which already rides in
//     the leader's own KindValue.L0Partial).
//   - extractInstanceTrace + reconstructOutcome: produce a ct.Outcome
//     suitable for ComputeSafetyReport from the captured wire +
//     per-Instance LastResolveLayerAttempts.
//   - Three scenarios at every (n, K) cell, each exercising a
//     distinct runner-side path:
//       * TestSafetyBridge_2abOBFT_Healthy — nominal path (decides at L_0).
//       * TestSafetyBridge_2abOBFT_LateCommit — opportunistic-poll
//         re-resolve path (late KindValues past Phase-2a fire-time).
//       * TestSafetyBridge_2abOBFT_SilentL0Leader — NR-quorum unlock +
//         fall-through to L_1 (L_0 leader's outbound suppressed).
//
// Matrix helpers (matrixCell, twoabMatrixCells,
// compressedTestOverridesForK) + LateCommit predicate
// (lateValueDelayPredicate) live in cluster_matrix_test.go; this file
// consumes them.

// recordingBlsBus wraps an existing *blsBus, decoding + capturing
// every emission's typed envelope before forwarding to the inner bus
// unchanged. Thread-safe — the inner bus's goroutine fan-out for
// delivery is preserved; the recording side has its own mutex
// protecting the captured slice.
//
// `from` in the broadcast call is the genuine network-level emitter
// (every node's hook routes its own emissions here via n.op). Stays
// authoritative even under future byz-like patterns that might forge
// the wire envelope's claimed-sender field — the production runner
// doesn't have such patterns today, but the bridge's by-emitter
// recording uses `from` so we'd be ready.
//
// Mirror of OBFT base's recordingBroadcastBus.
type recordingBlsBus struct {
	inner      *blsBus
	mu         sync.Mutex
	captured   []capturedEmission
	deliveries []deliveryEvent
}

// capturedEmission records one wire emission. The envelope's Kind
// discriminates which of the typed body fields is populated.
//
// broadcastAt is the slot-relative time at which the inner bus accepted
// the emission for fan-out (close enough to the source-side send time
// for diagnostic purposes). Matches the broadcastAt threaded into each
// resulting deliveryEvent so the dump can group (emission, deliveries)
// by exact equality.
type capturedEmission struct {
	from        spectypes.OperatorID
	envelope    *wire.Envelope
	broadcastAt time.Duration
}

// deliveryEvent records one per-recipient delivery completion: when the
// recipient's DispatchBytes returned (i.e., when the message had been
// parsed, verified, and pooled into the recipient's Instance state).
// Emitted by blsBus.deliveryHook. broadcastAt matches the originating
// capturedEmission's broadcastAt for grouping.
type deliveryEvent struct {
	from        spectypes.OperatorID
	to          spectypes.OperatorID
	kind        wire.MessageKind
	broadcastAt time.Duration
	deliveredAt time.Duration
}

func newRecordingBlsBus(inner *blsBus) *recordingBlsBus {
	rb := &recordingBlsBus{inner: inner}
	inner.deliveryHook = rb.recordDelivery
	return rb
}

// broadcast intercepts every emission: decode via wire.Unwrap, append
// (from, envelope, broadcastAt) to captured, then delegate to the inner
// bus via broadcastAt with the SAME broadcastAt value so capturedEmission
// and every resulting deliveryEvent share one exact-equal timestamp (the
// dump groups deliveries to emissions by (from, kind, broadcastAt) exact
// match — using two independent time.Since calls would diverge by µs).
// Decode failures (malformed envelope) are silently skipped — the inner
// bus's DispatchBytes path will reject malformed envelopes on its own.
func (rb *recordingBlsBus) broadcast(from spectypes.OperatorID, data []byte) {
	broadcastAt := time.Since(rb.inner.slotStart)
	if env, err := wire.Unwrap(data); err == nil && env != nil {
		rb.mu.Lock()
		rb.captured = append(rb.captured, capturedEmission{
			from:        from,
			envelope:    env,
			broadcastAt: broadcastAt,
		})
		rb.mu.Unlock()
	}
	rb.inner.broadcastAt(from, data, broadcastAt)
}

// recordDelivery is wired into blsBus.deliveryHook by newRecordingBlsBus.
// Called by each per-recipient delivery goroutine after DispatchBytes
// returns. Thread-safe via rb.mu.
func (rb *recordingBlsBus) recordDelivery(from, to spectypes.OperatorID, kind byte, broadcastAt, deliveredAt time.Duration) {
	rb.mu.Lock()
	rb.deliveries = append(rb.deliveries, deliveryEvent{
		from:        from,
		to:          to,
		kind:        wire.MessageKind(kind),
		broadcastAt: broadcastAt,
		deliveredAt: deliveredAt,
	})
	rb.mu.Unlock()
}

// snapshot returns a copy of the captured emissions. Safe to call
// post-slot (after the inner bus's stop() drains in-flight goroutines).
// The copy is shallow — envelopes themselves are immutable typed
// pointers shared with capturing-time observers, fine for the bridge's
// read-only reconstruction path.
func (rb *recordingBlsBus) snapshot() []capturedEmission {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]capturedEmission, len(rb.captured))
	copy(out, rb.captured)
	return out
}

// deliveriesSnapshot returns a copy of recorded deliveries. Safe to call
// post-slot after the inner bus's stop() drains in-flight delivery
// goroutines (all deliveryHook callbacks have fired by then).
func (rb *recordingBlsBus) deliveriesSnapshot() []deliveryEvent {
	rb.mu.Lock()
	defer rb.mu.Unlock()
	out := make([]deliveryEvent, len(rb.deliveries))
	copy(out, rb.deliveries)
	return out
}

// recordValueMsgWire mirrors consensustest/twoab/events.go's
// recordValueMsgToAggregator on the production wire's *twoabcore.ValueMsg.
// The two implementations are intentionally aligned — both record:
//   - L0Partial (the emitter's plaintext σ partial on V at L_0) →
//     ObserveSigma at L_0.
//   - LayerEntries with Kind=SigmaChained → ObserveEncryptedClaim at
//     L_k>0 keyed by the plaintext V (not the encrypted Payload).
//   - LayerEntries with Kind=NRPlaintext → ObserveNR at L_k>0.
//
// All observations are recorded under both the claimed-sender path
// (keyed on vm.OperatorID) and the by-emitter path (keyed on the
// genuine network-level emitter); the production runner doesn't forge
// today but the dual recording is forward-looking.
func recordValueMsgWire(agg *ct.OfflineAggregator, emitter spectypes.OperatorID, vm *twoabcore.ValueMsg) {
	if vm == nil {
		return
	}
	from := ct.OperatorID(vm.OperatorID)
	em := ct.OperatorID(emitter)
	if len(vm.L0Partial) > 0 {
		agg.ObserveSigma(from, 0, vm.V)
		agg.ObserveSigmaByEmitter(em, 0, vm.V)
	}
	for _, e := range vm.LayerEntries {
		switch e.Kind {
		case twoabcore.LayerEntrySigmaChained:
			agg.ObserveEncryptedClaim(from, e.Layer, e.V)
			agg.ObserveSigmaByEmitter(em, e.Layer, e.V)
		case twoabcore.LayerEntryNRPlaintext:
			agg.ObserveNR(from, e.Layer)
			agg.ObserveNRByEmitter(em, e.Layer)
		}
	}
}

// recordNoValueMsgWire mirrors consensustest/twoab/events.go's
// recordNoValueMsgToAggregator on *twoabcore.NoValueMsg. NoValueMsg
// has the same K-1 LayerEntries shape as ValueMsg but carries no
// L_0 payload (the emitter has no V_0 retained or host invalid at
// fire-time).
func recordNoValueMsgWire(agg *ct.OfflineAggregator, emitter spectypes.OperatorID, nv *twoabcore.NoValueMsg) {
	if nv == nil {
		return
	}
	from := ct.OperatorID(nv.OperatorID)
	em := ct.OperatorID(emitter)
	for _, e := range nv.LayerEntries {
		switch e.Kind {
		case twoabcore.LayerEntrySigmaChained:
			agg.ObserveEncryptedClaim(from, e.Layer, e.V)
			agg.ObserveSigmaByEmitter(em, e.Layer, e.V)
		case twoabcore.LayerEntryNRPlaintext:
			agg.ObserveNR(from, e.Layer)
			agg.ObserveNRByEmitter(em, e.Layer)
		}
	}
}

// recordCommitWire mirrors consensustest/twoab/events.go's
// recordCommitToAggregator on *twoabcore.Commit. Commit is NR-side
// only — KindValue carries the σ partial directly at Phase-2a, so
// there is no σ-side commit.
//   - Side=NR: NR tag partial at L_0 → ObserveNR at L_0.
//   - Side=NRDirect: NR tag partial at L_0 + K-1 LayerEntries (the
//     NRDirect emission bundles the L_k>0 commitments with the L_0
//     emission).
func recordCommitWire(agg *ct.OfflineAggregator, emitter spectypes.OperatorID, c *twoabcore.Commit) {
	if c == nil {
		return
	}
	from := ct.OperatorID(c.OperatorID)
	em := ct.OperatorID(emitter)
	switch c.Side {
	case twoabcore.CommitSideNR:
		agg.ObserveNR(from, 0)
		agg.ObserveNRByEmitter(em, 0)
	case twoabcore.CommitSideNRDirect:
		agg.ObserveNR(from, 0)
		agg.ObserveNRByEmitter(em, 0)
		for _, e := range c.LayerEntries {
			switch e.Kind {
			case twoabcore.LayerEntrySigmaChained:
				agg.ObserveEncryptedClaim(from, e.Layer, e.V)
				agg.ObserveSigmaByEmitter(em, e.Layer, e.V)
			case twoabcore.LayerEntryNRPlaintext:
				agg.ObserveNR(from, e.Layer)
				agg.ObserveNRByEmitter(em, e.Layer)
			}
		}
	}
}

// extractInstanceTrace pulls Instance.LastResolveLayerAttempts via the
// *RunningInstance pointer captured by runScenarioWithSafetyCheck's
// per-node capture goroutine. The captured pointer survives EndInstance
// (the Controller deletes its instances-map entry, but the RunningInstance
// and its Instance live on, and LastResolveLayerAttempts is preserved
// across Finalize — see TestLastResolveLayerAttempts_PreservedAcrossEndedInstance).
//
// Holds RunningInstance.instanceMu so the read is serialized against any
// in-progress Resolve goroutine — defensive under -race even after wg.Wait().
// Returns nil if the capture goroutine never observed a live instance for
// this slot (e.g., RunProposerSlot returned before StartNewInstance completed).
//
// Mirror of OBFT base's extractInstanceTrace.
func extractInstanceTrace(node *blsNode) []twoabcore.LayerAttempt {
	node.mu.Lock()
	ri := node.capturedRI
	node.mu.Unlock()
	if ri == nil {
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
// consume. Mirrors the 2abOBFT DES adapter's outcome() in spirit:
//
//   - Replay captured ValueMsg / NoValueMsg / Commit emissions into a
//     fresh OfflineAggregator. KindPhase1Bundle and KindCertificate
//     are intentionally NOT recorded: Phase1Bundle's LeaderSigma is
//     the leader's σ contribution that already rides in the leader's
//     own KindValue.L0Partial (recording both would double-count);
//     Certificates propagate the already-reconstructed full signature
//     and add no σ/NR pool contributions. This matches
//     consensustest/twoab/events.go's recorders (no
//     recordPhase1BundleToAggregator function exists there for the
//     same reason).
//   - Per-op: read submittedOutput() and LastResolveLayerAttempts().
//   - Distinguish cert-gossip-decide (oo.Round = -1) from local-decide
//     by checking the trace — if local Resolve never reached a Decided
//     layer but the op still submitted, it was a cert-driven submission
//     (matches the DES adapter's Layer=-1 convention; see
//     consensustest/twoab/events.go evtCertArrival).
//
// Byz is intentionally zero-valued: production has no byz-pattern
// injection, so every op is treated as honest by the per-op invariant
// checks (B1/B2/D1/C3).
func reconstructOutcome(nodes []*blsNode, captured []capturedEmission) ct.Outcome {
	n := len(nodes)
	agg := ct.NewOfflineAggregator(n)
	for _, em := range captured {
		switch em.envelope.Kind {
		case wire.KindValue:
			recordValueMsgWire(agg, em.from, em.envelope.ValueMsg)
		case wire.KindNoValue:
			recordNoValueMsgWire(agg, em.from, em.envelope.NoValueMsg)
		case wire.KindCommit:
			recordCommitWire(agg, em.from, em.envelope.Commit)
		case wire.KindPhase1Bundle, wire.KindCertificate:
			// Intentionally not recorded — see function docstring.
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
		if trace := extractInstanceTrace(node); len(trace) > 0 {
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

// traceHasDecided reports whether any LayerAttempt in the resolve trace
// reached the Decided state. Used by reconstructOutcome to distinguish
// local-decide (trace has a Decided layer) from cert-gossip-decide
// (submit happened but no local decision).
func traceHasDecided(trace []twoabcore.LayerAttempt) bool {
	for _, la := range trace {
		if la.Decided {
			return true
		}
	}
	return false
}

func convertLayerAttempts(in []twoabcore.LayerAttempt) []ct.LayerAttempt {
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

// committeeFromBLSNodes returns the cluster's operator IDs in ascending
// (rotation-stable) order — matching the production runner's leader-
// rotation convention (leaderForLayer in config.go).
func committeeFromBLSNodes(nodes []*blsNode) []spectypes.OperatorID {
	out := make([]spectypes.OperatorID, len(nodes))
	for i, n := range nodes {
		out[i] = n.op
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// l0Leader computes the L_0 leader at (slot, committee) under the
// production runner's leader-rotation rule: idx = (height + 0) % n on
// the ascending-sorted committee. Matches leaderForLayer at layer=0.
func l0Leader(committee []spectypes.OperatorID, slot phase0.Slot) spectypes.OperatorID {
	if len(committee) == 0 {
		return 0
	}
	return committee[uint64(slot)%uint64(len(committee))]
}

// scenarioConfig parameterizes a runner scenario for the bridge:
// per-cell ConfigOverrides, bus shape (with or without delay), silent
// set (for SilentL0Leader), and ctx timeout. The bridge's
// runScenarioWithSafetyCheck builds the cluster at the given cell and
// asserts safety on the reconstructed wire.
//
// Mirror of OBFT base's scenarioConfig; the silent-set field uses a
// boolean map (matching 2abOBFT's existing blsBus.silent convention)
// rather than OBFT base's predicate function — 2abOBFT's
// SilentL0Leader scenario suppresses ALL outbound from the L_0 leader,
// not just specific message kinds, so a simple set suffices.
type scenarioConfig struct {
	name      string
	timeout   time.Duration
	overrides func(t *testing.T, cell matrixCell) *ConfigOverrides
	// delayFor returns the bus's delayFn for the given cell; nil → no
	// delay (use plain blsBus instead of newBlsBusWithDelay).
	delayFor func(cell matrixCell) delayFn
	// silentFor returns the set of ops whose outbound is dropped at the
	// bus layer (crashed-sender sim). nil → no silent ops. Used by
	// SilentL0Leader to suppress the L_0 leader's outbound. Per-cell
	// context (which op is L_0 leader for the cell's slot) is captured
	// via closure.
	silentFor func(cell matrixCell, slot phase0.Slot, committee []spectypes.OperatorID) map[spectypes.OperatorID]bool
}

// healthyScenarioConfig: nominal-conditions cluster. All ops receive
// L_0 bundle on time, all hosts valid. Bridge asserts safety on the
// resulting wire.
func healthyScenarioConfig() scenarioConfig {
	return scenarioConfig{
		name:    "Healthy",
		timeout: 5 * time.Second,
		overrides: func(_ *testing.T, cell matrixCell) *ConfigOverrides {
			return compressedTestOverridesForK(cell.K)
		},
	}
}

// lateCommitScenarioConfig: delays a (n, qV)-parameterized subset of
// KindValue arrivals at op1 past Phase-2a fire time. Exercises the
// runner's opportunistic-resolve poll path — safety must hold even
// when the cluster reaches σ-quorum strictly after the soft deadline.
//
// Reuses the lateValueDelayPredicate helper from cluster_matrix_test.go.
// Helper named for the wire kind it delays (KindValue is 2abOBFT's
// σ-side terminal carrying ValueMsg.L0Partial — see
// lateValueDelayPredicate's docstring for the protocol note); the
// scenario name "LateCommit" is kept for parity with OBFT base's
// scenario of the same name, even though OBFT base delays KindCommit
// (its own σ-side terminal) rather than KindValue.
func lateCommitScenarioConfig() scenarioConfig {
	return scenarioConfig{
		name: "LateCommit",
		// 60s timeout — generous budget for failure-signal sharpness.
		// Typical wall ~330ms (300ms late-value delay + ~30ms crypto);
		// the crypto portion can balloon 25-50× under -race × heavy
		// GOMAXPROCS. 60s gives ~180× total margin so any timeout
		// failure is a real bug rather than a tail-variance flake.
		// (Cost is only paid on failure paths; passing tests complete
		// in typical wall.)
		timeout: 60 * time.Second,
		overrides: func(_ *testing.T, cell matrixCell) *ConfigOverrides {
			return compressedTestOverridesForK(cell.K)
		},
		delayFor: func(cell matrixCell) delayFn {
			const lateValueDelay = 300 * time.Millisecond
			const victim spectypes.OperatorID = 1
			return lateValueDelayPredicate(cell.n, victim, lateValueDelay)
		},
	}
}

// silentL0LeaderScenarioConfig: suppresses ALL outbound from the L_0
// leader (crashed-sender sim — leader's Instance still self-observes
// internally, but no peer ever sees its emissions). The surviving f+1
// operators emit NoValueMsg → Commit-NR at L_0, aggregate qEnc NR
// partials to unlock the L_0→L_1 chain key, decrypt the L_1
// SigmaChained σ entries from peer ValueMsgs, and decide at L_1.
//
// At K=2 (n=4) this is the canonical f+1=K case — only one fall-
// through layer. At K>2 the cluster could in principle fall further
// (if L_1 was also silent), but the scenario keeps only L_0 silent so
// every cell deterministically decides at L_1.
//
// Safety must hold under real concurrent goroutine scheduling at
// every matrix cell — that's the bridge assertion.
func silentL0LeaderScenarioConfig() scenarioConfig {
	return scenarioConfig{
		name: "SilentL0Leader_NRFallThrough",
		// 60s (vs 5s on Healthy/LateCommit): the silent-leader path does the
		// most sequential crypto work of any scenario — NR-quorum reconstruction
		// at L_0 (threshold-IBE chain-key derivation), chain-key decryption
		// of L_1 SigmaChained entries from each peer, σ-quorum reconstruction
		// at L_1 (BLS threshold aggregate). Under -race × high GOMAXPROCS
		// (32+), the per-op crypto cost can balloon ~25-50× — enough to
		// occasionally exhaust the old 5s budget.
		//
		// 60s gives ~315× margin over typical 190ms wall. Cost is paid only
		// on failure paths (passing tests complete in ~190ms regardless of
		// timeout). The intentionally generous budget makes the failure
		// signal sharp: a hit at 60s is almost certainly a real bug (no
		// reasonable scheduling delay lasts a minute), not a long-tail
		// timing artifact. Avoids re-investigating the same flake repeatedly
		// at smaller bumps (5s → 10s → 20s → ...).
		timeout: 60 * time.Second,
		overrides: func(_ *testing.T, cell matrixCell) *ConfigOverrides {
			return compressedTestOverridesForK(cell.K)
		},
		silentFor: func(_ matrixCell, slot phase0.Slot, committee []spectypes.OperatorID) map[spectypes.OperatorID]bool {
			leader := l0Leader(committee, slot)
			return map[spectypes.OperatorID]bool{leader: true}
		},
	}
}

// runScenarioWithSafetyCheck — bridge entry-point. Builds the cluster
// at (n, K), runs the scenario via the existing RunProposerSlot
// fixture but with the recordingBlsBus wire-tap installed,
// reconstructs the Outcome post-slot, asserts
// ComputeSafetyReport.IsViolation() == false.
//
// Slot id is derived from the cell + scenario name to avoid any
// cross-test state collision under -count=N.
func runScenarioWithSafetyCheck(t *testing.T, cell matrixCell, cfg scenarioConfig) {
	t.Helper()
	overrides := cfg.overrides(t, cell)
	cl := buildBLSCluster(t, cell.n, overrides)

	slot := phase0.Slot(500 + cell.n*10 + cell.K)
	slotStart := time.Now()

	var bus *blsBus
	if cfg.delayFor != nil {
		bus = newBlsBusWithDelay(cl.nodes, slotStart, cfg.delayFor(cell))
	} else {
		bus = &blsBus{nodes: cl.nodes, slotStart: slotStart}
	}
	if cfg.silentFor != nil {
		committee := committeeFromBLSNodes(cl.nodes)
		bus.silent = cfg.silentFor(cell, slot, committee)
	}
	recordingBus := newRecordingBlsBus(bus)
	defer bus.stop()

	for _, n := range cl.nodes {
		n := n
		n.broadcastFn = func(data []byte) {
			// Detect Phase-2a fire: the first non-Phase1Bundle emission
			// from this op corresponds to the FirePhase2a moment (the
			// runner's Phase-1 bundles are KindPhase1Bundle and are not
			// LayerEntries-bearing; the Phase-2a fire emits exactly one
			// of KindValue / KindNoValue / KindCommit-NRDirect with the
			// full K-1 LayerEntries committed). Capturing this time per
			// op lets the diagnostic dump correlate against per-emission
			// delivery times to localize bundle-delivery races.
			if env, err := wire.Unwrap(data); err == nil && env != nil && env.Kind != wire.KindPhase1Bundle {
				n.mu.Lock()
				if n.phase2aFireAt == 0 {
					n.phase2aFireAt = time.Since(slotStart)
				}
				n.mu.Unlock()
			}
			recordingBus.broadcast(n.op, data)
		}
	}

	// sleepAwareTimeoutCtx (see sleepctx_test.go): consumes budget by ACTIVE
	// wall time only, so the test survives laptop-sleep cycles regardless of
	// how the host platform's monotonic clock behaves during suspend. A
	// failure at the cfg.timeout boundary is therefore strictly about
	// active-running time — much sharper signal of a real bug.
	ctx, cancel := sleepAwareTimeoutCtx(context.Background(), cfg.timeout)
	defer cancel()

	// Capture goroutines: poll each node's Controller for the slot's
	// RunningInstance and stash the pointer on n.capturedRI. The pointer
	// survives RunProposerSlot's deferred EndInstance (which only deletes
	// the controller's instances-map entry — the Instance object itself
	// remains reachable via the captured pointer, and the resolve trace is
	// preserved across Finalize). Without this capture, extractInstanceTrace
	// always returns nil post-slot because EndInstance has cleared the map
	// by the time wg.Wait() returns, which would (a) blank the diagnostic
	// dump on failures and (b) feed empty ResolveLayerAttempts into the
	// safety report via reconstructOutcome.
	captureCtx, captureCancel := context.WithCancel(context.Background())
	defer captureCancel()
	var captureWg sync.WaitGroup
	for _, n := range cl.nodes {
		n := n
		captureWg.Add(1)
		go func() {
			defer captureWg.Done()
			ticker := time.NewTicker(100 * time.Microsecond)
			defer ticker.Stop()
			for {
				select {
				case <-captureCtx.Done():
					return
				case <-ticker.C:
					if ri, ok := n.ctrl.GetInstance(slot); ok && ri != nil {
						n.mu.Lock()
						n.capturedRI = ri
						n.mu.Unlock()
						return
					}
				}
			}
		}()
	}

	var wg sync.WaitGroup
	for _, n := range cl.nodes {
		n := n
		wg.Add(1)
		go func() {
			defer wg.Done()
			n.runErr = RunProposerSlot(ctx, n.sched, slot, slotStart)
		}()
	}
	wg.Wait()
	captureCancel()
	captureWg.Wait()

	// Drain the bus BEFORE the dump (vs the post-function defer): ensures
	// every delivery goroutine's deliveryHook has fired so deliveriesSnapshot
	// reflects the complete wire-timing picture. Idempotent via sync.Once,
	// so the deferred bus.stop above is harmless.
	bus.stop()

	// If any op failed (runErr or no submitted output), dump full cluster
	// state BEFORE the assertions t.Fatalf bail out. The dump captures the
	// per-op resolve-trace pool sizes (σ-pool, NR-pool) at each layer +
	// per-op Phase-2a fire time + per-emission wire timing (broadcast →
	// per-recipient delivery) — load-bearing diagnostics for understanding
	// rare race-stress failures where convergence stalls.
	if anyOpFailed(cl.nodes) {
		dumpClusterDiagnostic(t, cl.nodes, slot, recordingBus.snapshot(), recordingBus.deliveriesSnapshot(), cfg.name, cell)
	}

	for _, n := range cl.nodes {
		require.NoErrorf(t, n.runErr, "op %d RunProposerSlot at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
		require.NotNilf(t, n.submittedOutput(),
			"op %d submitted no output at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
	}

	// Cluster-wide value-agreement sanity check (cheap to keep — would
	// fail fast on a clear regression before the safety invariants
	// catch it).
	var ref *twoabcore.Output
	for _, n := range cl.nodes {
		out := n.submittedOutput()
		if ref == nil {
			ref = out
			continue
		}
		require.Truef(t, bytes.Equal(ref.Value, out.Value),
			"op %d decided a different Value at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
	}

	outcome := reconstructOutcome(cl.nodes, recordingBus.snapshot())
	rep := ct.ComputeSafetyReport(outcome)
	require.Falsef(t, rep.IsViolation(),
		"safety violation at %s n=%d K=%d: %s", cfg.name, cell.n, cell.K, rep)
}

// anyOpFailed reports whether any op had a runErr or failed to submit
// an Output. Used by runScenarioWithSafetyCheck to decide whether to
// emit a diagnostic dump before the test's failing assertions bail out.
func anyOpFailed(nodes []*blsNode) bool {
	for _, n := range nodes {
		if n.runErr != nil || n.submittedOutput() == nil {
			return true
		}
	}
	return false
}

// dumpClusterDiagnostic emits a per-op state dump for failing scenarios:
// runErr, submittedOutput, and the per-layer resolve-trace pool sizes
// (σ-pool / NR-pool against qV / qEnc). Plus the captured-wire-emission
// count, per-op Phase-2a fire time, and per-emission wire timing
// (broadcast → per-recipient delivery, with each recipient's deliveredAt
// compared against the recipient's Phase-2a fire time). Called only on
// failure to keep passing-test logs quiet.
//
// Interpreting the dump when investigating a failure:
//   - All ops decided except one + the failing op's σ-pool sizes are
//     short of qV at every layer → the failing op's local convergence
//     stalled (lost message, locked-out scheduler, etc.).
//   - All ops decided + only the failing op's submit was nil → submit
//     path bug (host-validate? SubmitOutput hook?).
//   - No op decided + low captured-emission count → bus delivery stuck.
//   - No op decided + high captured-emission count → protocol-level
//     non-convergence (would also surface as repro-able outside -race).
//   - Cluster stuck at L_k with mixed σ/NR distribution (σ < qV AND
//     NR < qEnc) → look at the wire-timing block: did the L_k leader's
//     KindPhase1Bundle arrive at each recipient BEFORE that recipient's
//     phase2aFireAt? A "late" marker on the bundle's deliveries means
//     the recipient committed NRPlaintext at L_k irrevocably because
//     the bundle hadn't been pooled by the Phase-2a fire instant.
func dumpClusterDiagnostic(t *testing.T, nodes []*blsNode, slot phase0.Slot, captured []capturedEmission, deliveries []deliveryEvent, scenarioName string, cell matrixCell) {
	t.Helper()
	t.Logf("=== DIAGNOSTIC DUMP: scenario=%s n=%d K=%d slot=%d ===", scenarioName, cell.n, cell.K, slot)
	t.Logf("Captured wire emissions: %d, delivery events: %d", len(captured), len(deliveries))

	// Per-op Phase-2a fire times, snapshot under each node's mu.
	fireAt := make(map[spectypes.OperatorID]time.Duration, len(nodes))
	for _, n := range nodes {
		n.mu.Lock()
		fireAt[n.op] = n.phase2aFireAt
		n.mu.Unlock()
	}

	for _, n := range nodes {
		var runErrStr string
		if n.runErr != nil {
			runErrStr = n.runErr.Error()
		} else {
			runErrStr = "<nil>"
		}
		out := n.submittedOutput()
		var outStr string
		if out == nil {
			outStr = "<nil>"
		} else {
			outStr = fmt.Sprintf("Layer=%d Value=%x", out.Layer, out.Value)
		}
		var fireAtStr string
		if fireAt[n.op] == 0 {
			fireAtStr = "<not-fired>"
		} else {
			fireAtStr = fireAt[n.op].String()
		}
		t.Logf("  op %d: runErr=%s submittedOutput=%s phase2aFireAt=%s", n.op, runErrStr, outStr, fireAtStr)
		trace := extractInstanceTrace(n)
		if trace == nil {
			t.Logf("    resolveTrace: <none — capture goroutine never observed a live Instance, or LastResolveLayerAttempts returned nil>")
			continue
		}
		t.Logf("    resolveTrace (%d layers):", len(trace))
		for _, la := range trace {
			t.Logf("      L_%d: σ-pool=%d/qV=%d σ-reached=%v decided=%v | NR-pool=%d/qEnc=%d NR-reached=%v",
				la.Layer, la.SigmaPoolSize, la.QV, la.SigmaReached, la.Decided,
				la.NRPoolSize, la.QEnc, la.NRReached)
		}
	}

	// Wire-timing block: for each captured emission (in capture order),
	// list its per-recipient delivery times alongside each recipient's
	// Phase-2a fire time. A '*' marks deliveredAt ≤ fireAt (in time for
	// the recipient's Phase-2a LayerEntries choice); a '!' marks late.
	t.Logf("Wire timing (broadcast → per-recipient delivery, * = before recipient phase2aFire, ! = after):")
	for i, em := range captured {
		t.Logf("  #%d from=op%d kind=%s broadcastAt=%s", i, em.from, em.envelope.Kind, em.broadcastAt)
		// Find deliveries that match this emission by (from, kind, broadcastAt-exact).
		for _, d := range deliveries {
			if d.from != em.from || d.kind != em.envelope.Kind || d.broadcastAt != em.broadcastAt {
				continue
			}
			marker := "*"
			recipFire := fireAt[d.to]
			if recipFire != 0 && d.deliveredAt > recipFire {
				marker = "!"
			} else if recipFire == 0 {
				marker = "?" // recipient never fired; can't classify
			}
			t.Logf("    %s op%d deliveredAt=%s (delay=%s, recipient.phase2aFireAt=%s)",
				marker, d.to, d.deliveredAt, d.deliveredAt-d.broadcastAt, recipFire)
		}
	}
	t.Logf("=== END DIAGNOSTIC DUMP ===")
}

// TestSafetyBridge_2abOBFT_Healthy iterates the (n, K) matrix and runs
// the Healthy scenario at each cell with the safety-bridge overlay.
// Mirror of OBFT base's TestSafetyBridge_OBFT_Healthy.
func TestSafetyBridge_2abOBFT_Healthy(t *testing.T) {
	cfg := healthyScenarioConfig()
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

// TestSafetyBridge_2abOBFT_LateCommit iterates the matrix with the
// LateCommit config (delays a parameterized subset of KindValue
// arrivals at op1 past Phase-2a fire time). Safety must hold under
// the runner's opportunistic-poll re-resolve path.
//
// Mirror of OBFT base's TestSafetyBridge_OBFT_LateCommit; both
// scenarios exercise opportunistic resolve, but the wire-kind being
// delayed differs by protocol — see lateValueDelayPredicate's
// docstring for the σ-side-terminal asymmetry between the protocols.
func TestSafetyBridge_2abOBFT_LateCommit(t *testing.T) {
	cfg := lateCommitScenarioConfig()
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

// TestSafetyBridge_2abOBFT_SilentL0Leader iterates the matrix with the
// SilentL0Leader_NRFallThrough config. L_0 leader's outbound is
// suppressed on the wire; cluster falls through to L_1 via NR-quorum
// unlock + chain decryption. Safety must hold across the deeper-layer
// recovery path under real concurrent scheduling.
//
// Mirror of OBFT base's TestSafetyBridge_OBFT_SilentL0Leader.
func TestSafetyBridge_2abOBFT_SilentL0Leader(t *testing.T) {
	cfg := silentL0LeaderScenarioConfig()
	for _, cell := range twoabMatrixCells() {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

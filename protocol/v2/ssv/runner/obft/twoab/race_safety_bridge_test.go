package twoab

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
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

// extractInstanceTrace returns the instance's final resolve trace, snapshot
// deterministically at teardown by the Controller.onInstanceEnd hook wired in
// buildBLSCluster (see blsNode.capturedTrace). Read after wg.Wait(), so the
// node.mu lock only guards against the data race the race detector would
// otherwise flag — the value is already final.
//
// Returns nil if no instance ran for the slot (RunProposerSlot returned before
// StartNewInstance) or the instance resolved no layer.
func extractInstanceTrace(node *blsNode) []twoabcore.LayerAttempt {
	node.mu.Lock()
	defer node.mu.Unlock()
	return node.capturedTrace
}

// formatBroadcastCounts renders a per-op `broadcastCounts[op]` map as a
// stable, compact one-liner for the diagnostic dump: keys are sorted by
// MessageKind so the same scenario always logs the same column order
// regardless of map iteration order, and the all-zero case (op had no
// captured emissions at all — e.g. an op that returned before its Phase-2a
// fire completed) prints as `none` to distinguish it from a missing-from-
// map miss.
func formatBroadcastCounts(counts map[wire.MessageKind]int) string {
	if len(counts) == 0 {
		return "none"
	}
	kinds := make([]wire.MessageKind, 0, len(counts))
	for k := range counts {
		kinds = append(kinds, k)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i] < kinds[j] })
	parts := make([]string, 0, len(kinds))
	for _, k := range kinds {
		parts = append(parts, fmt.Sprintf("%s=%d", k, counts[k]))
	}
	return strings.Join(parts, " ")
}

// extractInstanceCascadeErrors returns the instance's CascadeErrors, snapshot
// alongside the resolve trace by the same Controller.onInstanceEnd hook (see
// blsNode.capturedCascadeErrs / extractInstanceTrace). CascadeErrors is the
// accumulator of silent failures from afterStateDelta's cascade
// (MaybeBuildAndBroadcastUpgrade / MaybeBuildAndBroadcastCommit). Typically
// empty; non-empty in a failing scenario indicates a signer/EKM hiccup
// swallowed by recordCascadeError — the kind of bug that's invisible on the
// wire (no emission to point at) and would otherwise take an instrumentation
// session to find. Surfacing it in the diagnostic dump lets a future
// "missing emission" investigation start from the right hypothesis.
//
// Returns nil if no instance ran for the slot.
func extractInstanceCascadeErrors(node *blsNode) []error {
	node.mu.Lock()
	defer node.mu.Unlock()
	return node.capturedCascadeErrs
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
//   - Per-op: the submitted Output's Layer is the authoritative
//     local-vs-cert signal — a local σ-quorum decode submits
//     Output{Layer: k>=0}, the cert fast-path submits Output{Layer: -1}
//     (matches the DES adapter's Layer=-1 convention; see
//     consensustest/twoab/events.go evtCertArrival). oo.Round is taken
//     straight from out.Layer; the resolve trace (snapshot
//     deterministically at teardown — see extractInstanceTrace) is
//     attached as ResolveLayerAttempts purely as enrichment for D1's
//     per-op walk-consistency check. We deliberately do NOT reconcile
//     Round against the trace here: D1 already asserts their consistency
//     (a local-decide with no σ-reached trace entry, or one whose σ
//     layer disagrees with Round, is a violation), so silently coercing
//     a mismatch would mask exactly the kind of bug D1 exists to catch.
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

// pickStarvedSurvivors selects the survivors to deny V_1 so the L_1 fall-through
// deadlocks in a (qV-1)σ / (rest)NR split (used by BundleAtFireSplit). It starves
// the lowest-id survivors — excluding the silent L_0 leader (contributes nothing
// anyway) and the L_1 leader (must stay σ so V_1 exists) — until exactly
// (#survivors - (qV-1)) are starved. The result: σ-pool[1] caps at qV-1 < qV and
// NR-pool[1] = #starved < qEnc, a permanent quorum-short stop at L_1. `committee`
// is ascending-sorted (committeeFromBLSNodes).
func pickStarvedSurvivors(committee []spectypes.OperatorID, slot phase0.Slot, cell matrixCell, l1Leader spectypes.OperatorID) map[spectypes.OperatorID]bool {
	f := (cell.n - 1) / 3
	qV := 2*f + 1
	l0 := l0Leader(committee, slot)
	// Deny V_1 to (n - (qV-1)) ops so exactly qV-1 σ-emitters remain — one short
	// of σ-quorum at L_1 for EVERY op's local view. This INCLUDES the silent L_0
	// leader: it is only outbound-silenced (it still receives inbound), so if it
	// kept V_1 it would self-σ at L_1 and reach σ-quorum locally (its own partial
	// + the qV-1 it receives), deciding on its own — a timing-dependent wildcard
	// that reintroduces flakiness. The L_1 leader is excluded (it self-feeds V_1,
	// which must exist for the σ side).
	toStarve := cell.n - (qV - 1)
	starved := make(map[spectypes.OperatorID]bool, toStarve)
	starved[l0] = true // silent L_0 leader → deterministically NRs at L_1
	toStarve--
	for _, op := range committee {
		if toStarve <= 0 {
			break
		}
		if op == l0 || op == l1Leader {
			continue
		}
		starved[op] = true
		toStarve--
	}
	return starved
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
	// delayFor returns the bus's delayFn for the given cell. Slot + the
	// ascending-sorted committee are passed so a predicate can target a
	// specific layer leader (e.g. the L_1 leader's bundle). nil → no delay
	// (use a plain blsBus instead of newBlsBusWithDelay).
	delayFor func(cell matrixCell, slot phase0.Slot, committee []spectypes.OperatorID) delayFn
	// silentFor returns the set of ops whose outbound is dropped at the
	// bus layer (crashed-sender sim). nil → no silent ops. Used by
	// SilentL0Leader to suppress the L_0 leader's outbound. Per-cell
	// context (which op is L_0 leader for the cell's slot) is captured
	// via closure.
	silentFor func(cell matrixCell, slot phase0.Slot, committee []spectypes.OperatorID) map[spectypes.OperatorID]bool
	// outcome controls how the bridge judges per-op decide/miss results
	// (liveness). Safety (ComputeSafetyReport) is asserted separately and
	// always. nil → must converge (Healthy, LateCommit).
	outcome *outcomePolicy
}

// outcomePolicy is the per-scenario liveness verdict for the safety bridge.
// A within-spec clean miss (the silent-leader fall-through losing its timing
// race under -race, or a deliberately-forced σ/NR split) is NOT a bug — safety
// still holds — so some scenarios tolerate or require it instead of demanding
// convergence.
type outcomePolicy struct {
	// mustMiss: convergence is itself a failure; the scenario MUST end in a
	// clean miss (the deterministic forced-split scenario). When false, a
	// clean miss is *tolerated* alongside convergence (SilentL0Leader).
	mustMiss bool
	// missLayer: the resolve-trace layer a clean miss must stop at. < 0 means
	// "any fall-through layer (>= 1)".
	missLayer int
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
		delayFor: func(cell matrixCell, _ phase0.Slot, _ []spectypes.OperatorID) delayFn {
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
		// Approach C: a silent L_0 leader makes the fall-through-to-L_1 decision
		// race V_1's arrival against the Phase-2a fire. Under -race that race can
		// land on a σ/NR split → a clean, within-spec ErrNoQuorum miss (safety
		// holds; nobody decides). Tolerate it at any fall-through layer; a real
		// wedge (non-ctx error or a non-quorum-short trace) still fails. The
		// fall-through *convergence* itself is asserted deterministically by the
		// DES (TestCrash_L0LeaderDirect and the matrix-cell crash coverage).
		outcome: &outcomePolicy{missLayer: -1},
	}
}

// bundleAtFireSplitScenarioConfig deterministically reproduces the no-man's-land
// σ/NR split that SilentL0Leader hits only flakily under -race: a silent L_0
// leader (fall-through to L_1) plus selective starvation of V_1 (the L_1 leader's
// Phase-1 bundle) at (n - (qV-1)) ops — including the silent leader itself, which
// still receives inbound — so exactly qV-1 σ-emitters remain and σ-pool[1] is one
// short of quorum for every op's local view, with NR-pool[1] also short: a
// permanent quorum-short stop at L_1. The bridge asserts safety holds in this
// split (the whole point) and that the miss is a clean deadlock at L_1.
//
// Determinism comes from *which* survivors receive V_1 (bus-injected, per
// recipient), not fire-timing jitter — so it holds under -race. Denying the L_1
// leader's KindPhase1Bundle alone suffices: every survivor is NoValue at L_0 (no
// KindValue is emitted), and NoValueMsg carries no forwarded witnesses, so the
// direct bundle is the only V_1 vector.
func bundleAtFireSplitScenarioConfig() scenarioConfig {
	return scenarioConfig{
		name: "BundleAtFireSplit",
		// Short timeout: the run is engineered to deadlock at L_1, so it always
		// waits this out (nothing to converge to). 5s clears the time to reach
		// the stable deadlock under -race (~1s) with margin, and is kept small
		// because it is paid on every run.
		timeout: 5 * time.Second,
		overrides: func(_ *testing.T, cell matrixCell) *ConfigOverrides {
			return compressedTestOverridesForK(cell.K)
		},
		silentFor: func(_ matrixCell, slot phase0.Slot, committee []spectypes.OperatorID) map[spectypes.OperatorID]bool {
			return map[spectypes.OperatorID]bool{l0Leader(committee, slot): true}
		},
		delayFor: func(cell matrixCell, slot phase0.Slot, committee []spectypes.OperatorID) delayFn {
			l1Leader := spectypes.OperatorID(leaderForLayer(committee, twoabcore.Height(slot), 1))
			starved := pickStarvedSurvivors(committee, slot, cell, l1Leader)
			return func(from, to spectypes.OperatorID, kind byte) time.Duration {
				if from == l1Leader && kind == byte(wire.KindPhase1Bundle) && starved[to] {
					// Withhold V_1 from the starved ops until ~run-end: long enough
					// that they NR-lock at L_1 first (the Phase-2a fire is ≤ ~1s
					// even under -race), but below the 5s timeout so the bus
					// delivery goroutine finishes before bus.stop()'s wg.Wait().
					// (A time.Hour delay would hang stop() for an hour.) By the time
					// V_1 lands the split is locked; the late arrival can't upgrade
					// the NR-side (no upgrade above L_0), so the deadlock holds.
					return 4 * time.Second
				}
				return 0
			}
		},
		// Forced clean miss at L_1: convergence here would mean the starvation
		// didn't take (a test-setup bug). Safety must still hold in the split.
		outcome: &outcomePolicy{mustMiss: true, missLayer: 1},
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

	committee := committeeFromBLSNodes(cl.nodes)

	var bus *blsBus
	if cfg.delayFor != nil {
		bus = newBlsBusWithDelay(cl.nodes, slotStart, cfg.delayFor(cell, slot, committee))
	} else {
		bus = &blsBus{nodes: cl.nodes, slotStart: slotStart}
	}
	if cfg.silentFor != nil {
		bus.silent = cfg.silentFor(cell, slot, committee)
	}
	recordingBus := newRecordingBlsBus(bus)
	defer bus.stop()

	for _, n := range cl.nodes {
		n := n
		n.broadcastFn = func(data []byte) {
			// Capture first-emit time: the first non-Phase1Bundle emission
			// is this op's Phase-2a emission being *broadcast* (the runner's
			// Phase-1 bundles are KindPhase1Bundle and not LayerEntries-bearing;
			// the Phase-2a emission is exactly one of KindValue / KindNoValue /
			// KindCommit-NRDirect with the full K-1 LayerEntries). This is the
			// emit instant, NOT the backstop-select or MaybeFirePhase2a commit
			// instant — see phase2aFirstEmitAt's doc. Capturing it lets the
			// diagnostic dump correlate against per-emission delivery times.
			if env, err := wire.Unwrap(data); err == nil && env != nil && env.Kind != wire.KindPhase1Bundle {
				n.mu.Lock()
				if n.phase2aFirstEmitAt == 0 {
					n.phase2aFirstEmitAt = time.Since(slotStart)
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

	// Per-op resolve trace + cascade errors are captured deterministically at
	// instance teardown via the Controller.onInstanceEnd hook wired in
	// buildBLSCluster — no polling, no race. Each node runs its slot to
	// completion on its own goroutine; the deferred EndInstance fires the hook
	// (writing n.capturedTrace / n.capturedCascadeErrs) before that goroutine
	// returns, so the snapshot is published happens-before the wg.Wait() below.
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

	// Drain the bus BEFORE the dump (vs the post-function defer): ensures
	// every delivery goroutine's deliveryHook has fired so deliveriesSnapshot
	// reflects the complete wire-timing picture. Idempotent via sync.Once,
	// so the deferred bus.stop above is harmless.
	bus.stop()

	// Dump full cluster state only if the test actually FAILS: on any assertion
	// failure below, require.* calls FailNow → runtime.Goexit, which runs this
	// deferred check (t.Failed() is then true). A tolerated/forced clean miss is
	// a PASS, so it produces no dump — and the dump's formatting work is skipped
	// on the happy path. The dump captures per-op resolve-trace pool sizes (σ/NR
	// vs qV/qEnc), first-emit times, and per-emission wire timing — the
	// load-bearing diagnostics for rare race-stress failures.
	defer func() {
		if t.Failed() {
			dumpClusterDiagnostic(t, cl.nodes, slot, overrides.tPhase2a(), recordingBus.snapshot(), recordingBus.deliveriesSnapshot(), cfg.name, cell)
		}
	}()

	// Safety is the bridge's core assertion — always computed, regardless of
	// whether the cluster converged or cleanly missed. A clean miss is safe by
	// construction: nobody decided, so no two ops can disagree (ComputeSafetyReport
	// excludes Terminated from IsViolation). Compute it before the liveness verdict
	// so a real violation surfaces even on a failing-convergence path.
	outcome := reconstructOutcome(cl.nodes, recordingBus.snapshot())
	rep := ct.ComputeSafetyReport(outcome)
	require.Falsef(t, rep.IsViolation(),
		"safety violation at %s n=%d K=%d: %s", cfg.name, cell.n, cell.K, rep)

	// Deterministic-capture guard — gives the onInstanceEnd refactor teeth.
	//
	// Every local-decide submit (Output{Layer >= 0}) MUST have a captured
	// resolve trace whose final entry is exactly that deciding layer, σ-reached
	// and decided. This is deterministically guaranteed: the only Resolve caller
	// is the synchronous ResolveAndSubmitOpportunistically on the RunProposerSlot
	// goroutine, a deciding Resolve appends its σ-reached layer as the LAST trace
	// entry then returns, and EndInstance's hook snapshots the trace on that same
	// goroutine after it returns — no concurrent Resolve can reset it.
	//
	// The guard exists because D1 (HonestWalkConsistent) *gracefully skips* ops
	// with an empty trace (safety.go's ResolveLayerAttempts default). So a future
	// capture regression that silently reintroduced empty traces — exactly the
	// pre-refactor polling-miss bug — would make D1 pass vacuously for fast local
	// deciders. This assertion fails loudly instead, keeping the capture honest.
	//
	// Cert-gossip submits (Layer == -1) are exempt: a peer certificate can arrive
	// before this op's first local Resolve, so an empty/non-deciding trace is
	// legitimate there.
	for _, n := range cl.nodes {
		out := n.submittedOutput()
		if out == nil || out.Layer < 0 {
			continue
		}
		trace := extractInstanceTrace(n)
		require.NotEmptyf(t, trace,
			"op %d submitted local Output{Layer:%d} but captured no resolve trace (deterministic-capture regression?) at %s n=%d K=%d",
			n.op, out.Layer, cfg.name, cell.n, cell.K)
		last := trace[len(trace)-1]
		require.Truef(t, last.Decided && last.SigmaReached && last.Layer == out.Layer,
			"op %d local Output{Layer:%d} but final trace entry is L_%d decided=%v σ-reached=%v at %s n=%d K=%d",
			n.op, out.Layer, last.Layer, last.Decided, last.SigmaReached, cfg.name, cell.n, cell.K)
	}

	// Liveness verdict, gated by the scenario's outcome policy (safety above is
	// already asserted). Must-converge (default), tolerant (SilentL0Leader), or
	// forced-miss (BundleAtFireSplit) — see assertOutcome.
	assertOutcome(t, cl.nodes, cfg, cell)
}

// assertOutcome judges the per-op decide/miss results against the scenario's
// outcome policy. Safety (ComputeSafetyReport) and the deterministic-capture
// guard are asserted by the caller; this is the liveness verdict.
//
//   - nil policy (Healthy, LateCommit): every op must decide and agree.
//   - tolerant (SilentL0Leader): full convergence is fine; otherwise every op
//     that did NOT decide must be a clean within-spec miss. Deciders are allowed
//     even on the miss path — e.g. the outbound-silenced L_0 leader still
//     receives inbound, so it can self-σ a local quorum at L_1 and decide alone
//     while peers stay quorum-short; that is safe (a lone decider can't disagree,
//     and the safety report + capture guard already vetted it).
//   - forced-miss (BundleAtFireSplit): NObody may decide — the V_1 starvation
//     (incl. the silent leader) is engineered so no op can reconstruct.
//
// Every non-deciding op's resolve trace must end in a real quorum-short stop —
// an empty / never-fired / wrong-state trace fails loudly, so a genuine runner
// wedge under -race is still caught.
func assertOutcome(t *testing.T, nodes []*blsNode, cfg scenarioConfig, cell matrixCell) {
	t.Helper()

	if cfg.outcome == nil {
		requireConverged(t, nodes, cfg, cell)
		return
	}

	decided := 0
	for _, n := range nodes {
		if n.submittedOutput() != nil {
			decided++
		}
	}

	switch {
	case cfg.outcome.mustMiss:
		// Engineered so no op can reconstruct; any decider means the setup
		// (V_1 starvation, incl. the silent leader) didn't take.
		require.Zerof(t, decided,
			"%s n=%d K=%d: %d op(s) decided but the scenario forces an all-ops clean miss",
			cfg.name, cell.n, cell.K, decided)
	case decided == len(nodes):
		// Tolerant policy, common case: full convergence.
		requireConverged(t, nodes, cfg, cell)
		return
	}

	// Forced-miss, or tolerant-with-misses: every op that did NOT decide must be
	// a clean quorum-short miss. Deciders (possible under tolerant) are already
	// vetted by ComputeSafetyReport (agreement) + the deterministic-capture guard.
	missed := 0
	for _, n := range nodes {
		if n.submittedOutput() != nil {
			continue
		}
		missed++
		require.Truef(t, errors.Is(n.runErr, context.Canceled) || errors.Is(n.runErr, context.DeadlineExceeded),
			"op %d clean-miss expected a ctx error, got %v at %s n=%d K=%d", n.op, n.runErr, cfg.name, cell.n, cell.K)
		trace := extractInstanceTrace(n)
		require.NotEmptyf(t, trace,
			"op %d clean-miss but empty resolve trace (a wedge, not a within-spec miss) at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
		last := trace[len(trace)-1]
		// A genuine quorum-short stop: didn't decide, σ-pool < qV, and either
		// NR-pool < qEnc (deadlock) or the deepest layer (exhaustion).
		deepest := last.Layer == cell.K-1
		require.Truef(t, !last.Decided && !last.SigmaReached && (deepest || !last.NRReached),
			"op %d clean-miss trace not quorum-short: L_%d decided=%v σ-reached=%v NR-reached=%v at %s n=%d K=%d",
			n.op, last.Layer, last.Decided, last.SigmaReached, last.NRReached, cfg.name, cell.n, cell.K)
		if cfg.outcome.missLayer >= 0 {
			require.Equalf(t, cfg.outcome.missLayer, last.Layer,
				"op %d clean-miss at L_%d, expected L_%d at %s n=%d K=%d", n.op, last.Layer, cfg.outcome.missLayer, cfg.name, cell.n, cell.K)
		} else {
			require.GreaterOrEqualf(t, last.Layer, 1,
				"op %d clean-miss at L_%d, expected a fall-through layer (>= 1) at %s n=%d K=%d", n.op, last.Layer, cfg.name, cell.n, cell.K)
		}
	}
	require.NotZerof(t, missed,
		"%s n=%d K=%d: expected at least one clean miss but every op decided", cfg.name, cell.n, cell.K)
	t.Logf("%s n=%d K=%d: %d of %d ops cleanly missed (quorum-short at a fall-through layer; within-spec, safety verified)",
		cfg.name, cell.n, cell.K, missed, len(nodes))
}

// requireConverged asserts every op decided without error. Value agreement is
// NOT re-checked here: the caller runs ComputeSafetyReport first, whose SingleV /
// HonestAgreement invariants already fail if any two ops decide distinct values
// (reconstructOutcome records each decider's Value).
func requireConverged(t *testing.T, nodes []*blsNode, cfg scenarioConfig, cell matrixCell) {
	t.Helper()
	for _, n := range nodes {
		require.NoErrorf(t, n.runErr, "op %d RunProposerSlot at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
		require.NotNilf(t, n.submittedOutput(),
			"op %d submitted no output at %s n=%d K=%d", n.op, cfg.name, cell.n, cell.K)
	}
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
//     firstEmitAt? A "late" marker on the bundle's deliveries means
//     the recipient likely committed NRPlaintext at L_k irrevocably because
//     the bundle hadn't been pooled by its Phase-2a commit. (firstEmitAt is
//     the emit, not the commit — a proxy; see phase2aFirstEmitAt's doc.)
func dumpClusterDiagnostic(t *testing.T, nodes []*blsNode, slot phase0.Slot, backstop time.Duration, captured []capturedEmission, deliveries []deliveryEvent, scenarioName string, cell matrixCell) {
	t.Helper()
	t.Logf("=== DIAGNOSTIC DUMP: scenario=%s n=%d K=%d slot=%d ===", scenarioName, cell.n, cell.K, slot)
	t.Logf("Captured wire emissions: %d, delivery events: %d", len(captured), len(deliveries))
	t.Logf("TPhase2a backstop=%s (Phase-2a fires at the earlier of L0Ready or this; firstEmitAt below is when the *emission was broadcast* — lock contention can push it well past the backstop, so do not read it as the fire/commit instant)", backstop)

	// Per-op first-emit times (first non-Phase1Bundle broadcast), snapshot under mu.
	firstEmitAt := make(map[spectypes.OperatorID]time.Duration, len(nodes))
	for _, n := range nodes {
		n.mu.Lock()
		firstEmitAt[n.op] = n.phase2aFirstEmitAt
		n.mu.Unlock()
	}

	// Per-op broadcast counts by wire.MessageKind, derived from `captured`.
	// Cheap "did op X actually call Broadcast for kind Y?" check that
	// short-circuits wire-vs-state mystery investigations: e.g. trace says
	// op 2 set ownCommit but no commit emission appears in the wire dump →
	// broadcastCounts[op2][KindCommit] == 0 confirms the gap is between
	// in-memory state and the scheduler's broadcastNewEmissions poll, not
	// elsewhere in the broadcast pipeline. broadcastCounts[op][kind] counts
	// only emissions captured by recordingBlsBus, which is every emission
	// the test fixture's Broadcast / BroadcastCertificate hooks route — a
	// proxy for hook-entry counts that's sufficient for diagnostic intent.
	broadcastCounts := make(map[spectypes.OperatorID]map[wire.MessageKind]int, len(nodes))
	for _, em := range captured {
		opCounts, ok := broadcastCounts[em.from]
		if !ok {
			opCounts = make(map[wire.MessageKind]int)
			broadcastCounts[em.from] = opCounts
		}
		opCounts[em.envelope.Kind]++
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
		var firstEmitStr string
		if firstEmitAt[n.op] == 0 {
			firstEmitStr = "<not-emitted>"
		} else {
			firstEmitStr = firstEmitAt[n.op].String()
		}
		t.Logf("  op %d: runErr=%s submittedOutput=%s firstEmitAt=%s broadcasts=%s",
			n.op, runErrStr, outStr, firstEmitStr, formatBroadcastCounts(broadcastCounts[n.op]))
		trace := extractInstanceTrace(n)
		if trace == nil {
			t.Logf("    resolveTrace: <none — no instance ran for this slot, or it resolved no layer>")
		} else {
			t.Logf("    resolveTrace (%d layers):", len(trace))
			for _, la := range trace {
				t.Logf("      L_%d: σ-pool=%d/qV=%d σ-reached=%v decided=%v | NR-pool=%d/qEnc=%d NR-reached=%v",
					la.Layer, la.SigmaPoolSize, la.QV, la.SigmaReached, la.Decided,
					la.NRPoolSize, la.QEnc, la.NRReached)
			}
		}
		// CascadeErrors: silent failures of afterStateDelta's cascade. Empty
		// in the healthy case; non-empty fingerprint a signer/EKM hiccup that
		// swallowed a Phase-2a-late upgrade or Phase-2b NR-Commit and would
		// otherwise be invisible on the wire.
		if cascadeErrs := extractInstanceCascadeErrors(n); len(cascadeErrs) > 0 {
			t.Logf("    cascadeErrors (%d):", len(cascadeErrs))
			for j, err := range cascadeErrs {
				t.Logf("      [%d] %s", j, err)
			}
		}
	}

	// Wire-timing block: for each captured emission (in capture order),
	// list its per-recipient delivery times alongside each recipient's
	// first-emit time. A '*' marks deliveredAt ≤ firstEmitAt (the emission
	// likely landed before the recipient broadcast its own Phase-2a choice);
	// a '!' marks late. firstEmitAt is the emit, not the commit, so this is a
	// heuristic — see phase2aFirstEmitAt's doc.
	t.Logf("Wire timing (broadcast → per-recipient delivery, * = before recipient firstEmitAt, ! = after):")
	for i, em := range captured {
		t.Logf("  #%d from=op%d kind=%s broadcastAt=%s", i, em.from, em.envelope.Kind, em.broadcastAt)
		// Find deliveries that match this emission by (from, kind, broadcastAt-exact).
		for _, d := range deliveries {
			if d.from != em.from || d.kind != em.envelope.Kind || d.broadcastAt != em.broadcastAt {
				continue
			}
			marker := "*"
			recipEmit := firstEmitAt[d.to]
			if recipEmit != 0 && d.deliveredAt > recipEmit {
				marker = "!"
			} else if recipEmit == 0 {
				marker = "?" // recipient never emitted; can't classify
			}
			t.Logf("    %s op%d deliveredAt=%s (delay=%s, recipient.firstEmitAt=%s)",
				marker, d.to, d.deliveredAt, d.deliveredAt-d.broadcastAt, recipEmit)
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

// TestForcedSplitSafety_2abOBFT deterministically forces the no-man's-land σ/NR
// split at the L_1 fall-through layer (silent L_0 leader + selective V_1
// starvation) and asserts safety holds in it — the "Phase-1-during-Phase-2a
// (bundle at backstop-fire instant)" race window the Makefile flags as
// known-but-untested, now covered on purpose rather than incidentally (and
// flakily) via SilentL0Leader under -race.
//
// Named OUTSIDE the `^TestSafetyBridge_` pattern on purpose: the stress target
// amplifies that pattern (-race × count=320 × cpu={1,4,8,16,32}), but this
// scenario is deterministic (the split is bus-injected, not timing-raced) and
// *always* waits out its timeout (it can never decide). Amplifying it would add
// nothing but hours of timeout-waiting. It runs once per cell in `make unit-test`
// (which is -race), which is sufficient.
//
// A small representative set, not the full matrix: the split is the same shape
// everywhere, so we cover only the two distinct trace shapes — {4,2} stops at
// the deepest layer (exhaustion), {7,7} stops mid-walk (deadlock).
func TestForcedSplitSafety_2abOBFT(t *testing.T) {
	cfg := bundleAtFireSplitScenarioConfig()
	for _, cell := range []matrixCell{{4, 2}, {7, 7}} {
		cell := cell
		t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
			runScenarioWithSafetyCheck(t, cell, cfg)
		})
	}
}

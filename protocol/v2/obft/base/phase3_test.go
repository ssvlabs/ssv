package base

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestSelectWinningGroup verifies the σ-group selection helper:
// determinism-load-bearing lex-tiebreak on V when partial counts are
// equal. Without the tiebreak, equal-count groups would resolve based
// on slice-iteration order (here always input order, but the helper is
// agnostic), producing nondeterministic Output across operators on
// transient pre-quorum states.
func TestSelectWinningGroup(t *testing.T) {
	mkPartials := func(ops ...OperatorID) map[OperatorID]Signature {
		out := make(map[OperatorID]Signature, len(ops))
		for _, op := range ops {
			out[op] = Signature{byte(op)}
		}
		return out
	}

	t.Run("empty groups returns nil", func(t *testing.T) {
		require.Nil(t, selectWinningGroup(nil))
		require.Nil(t, selectWinningGroup([]*sigGroup{}))
	})

	t.Run("single group wins", func(t *testing.T) {
		g := &sigGroup{value: Value("V0"), partials: mkPartials(1, 2, 3)}
		require.Equal(t, g, selectWinningGroup([]*sigGroup{g}))
	})

	t.Run("higher count wins regardless of V order", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(1, 2, 3)}
		require.Equal(t, gB, selectWinningGroup([]*sigGroup{gA, gB}))
		require.Equal(t, gB, selectWinningGroup([]*sigGroup{gB, gA}))
	})

	t.Run("equal count: lex-smaller V wins (forward order)", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(3, 4)}
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gA, gB}))
	})

	t.Run("equal count: lex-smaller V wins (reverse input order)", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(3, 4)}
		// Same V's, reversed input order — winner must still be V_a.
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gB, gA}))
	})

	t.Run("equal count three-way: smallest V wins", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(2)}
		gC := &sigGroup{value: Value("V_c"), partials: mkPartials(3)}
		// Permutations should all yield gA.
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gA, gB, gC}))
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gC, gB, gA}))
		require.Equal(t, gA, selectWinningGroup([]*sigGroup{gB, gC, gA}))
	})

	t.Run("byte-level lex order (not lexicographic-by-length)", func(t *testing.T) {
		// "AB" < "AC" byte-wise.
		gAB := &sigGroup{value: Value("AB"), partials: mkPartials(1)}
		gAC := &sigGroup{value: Value("AC"), partials: mkPartials(2)}
		require.Equal(t, gAB, selectWinningGroup([]*sigGroup{gAB, gAC}))
		require.Equal(t, gAB, selectWinningGroup([]*sigGroup{gAC, gAB}))
	})
}

// TestLastResolveLayerAttempts_DecidedAtL0 locks in the trace shape for
// the common-case "decided at L_0 σ-quorum" walk: a single entry at
// layer 0 with SigmaReached=true and Decided=true. The consensustest
// framework's bucket-3 D1 invariant relies on this shape to detect
// regressions where Resolve advances past a σ-decidable layer or
// decides without σ-source.
func TestLastResolveLayerAttempts_DecidedAtL0(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	s.runPhase2(nil)

	out, err := s.instances[2].Resolve()
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, 0, out.Layer, "healthy cluster decides at L_0")

	trace := s.instances[2].LastResolveLayerAttempts()
	require.Len(t, trace, 1, "decided at L_0 should produce a single-entry trace")
	require.Equal(t, 0, trace[0].Layer)
	require.True(t, trace[0].SigmaReached, "σ-pool reached qV at L_0")
	require.True(t, trace[0].Decided, "Output returned at L_0")
	require.GreaterOrEqual(t, trace[0].SigmaPoolSize, trace[0].QV,
		"SigmaPoolSize must be ≥ QV when SigmaReached=true")
	require.Equal(t, 3, trace[0].QV, "QV = 2f+1 = 3 at n=4")
}

// TestLastResolveLayerAttempts_DecidedAtFallthrough locks in the trace
// shape for the NR-fall-through walk: L_0 σ-pool short → NR-quorum at
// L_0 unlocks chain → σ-quorum at L_1. Trace contains both layers'
// attempts with the deciding-layer's Decided=true. This is the
// critical shape for the bucket-3 D1 case (b) check
// (oo.Round == sigmaReachedAt[0]).
func TestLastResolveLayerAttempts_DecidedAtFallthrough(t *testing.T) {
	s := newSim(t, 4)
	// Skip L_0 bundle delivery → all ops silent-leader-NR at L_0.
	// Deliver L_1..L_{K-1} bundles so σ-quorum reaches at L_1.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	s.runPhase2(nil)

	out, err := s.instances[2].Resolve()
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, 1, out.Layer, "L_0 missing bundle → fall through to L_1")

	trace := s.instances[2].LastResolveLayerAttempts()
	require.GreaterOrEqual(t, len(trace), 2, "fallthrough trace must include L_0 + deciding layer")

	// L_0: σ-pool short, NR-pool reached qEnc (chain unlocked).
	require.Equal(t, 0, trace[0].Layer)
	require.False(t, trace[0].SigmaReached, "L_0 σ-pool empty without bundle")
	require.False(t, trace[0].Decided, "L_0 didn't decide")
	require.True(t, trace[0].NRReached, "L_0 NR-pool reached qEnc → chain unlocked")
	require.Equal(t, 3, trace[0].QEnc, "QEnc = 2f+1 = 3 at n=4")

	// Deciding layer (L_1): σ-pool reached, Decided.
	deciding := trace[len(trace)-1]
	require.Equal(t, 1, deciding.Layer)
	require.True(t, deciding.SigmaReached)
	require.True(t, deciding.Decided)
	require.GreaterOrEqual(t, deciding.SigmaPoolSize, deciding.QV)
}

// TestLastResolveLayerAttempts_ExhaustionWalk locks in the trace shape
// when Resolve walks every layer without producing σ-quorum anywhere.
// Construct via runPhase2 with no Phase-1 bundles delivered at all —
// all ops silent-leader-NR at every layer they lead, NR-pool reaches
// qEnc at all layers 0..K-2 (chain unlocks each), but σ-pool never
// reaches at any layer. Walk completes with ResolveFailureExhaustion.
// Trace has all K entries with appropriate flags.
func TestLastResolveLayerAttempts_ExhaustionWalk(t *testing.T) {
	s := newSim(t, 4)
	// No bundles delivered → every layer's leader is silent → all ops
	// NR-emit at every layer they lead.
	s.runPhase2(nil)

	out, err := s.instances[2].Resolve()
	require.Error(t, err, "no σ-quorum anywhere → walk exhausts")
	require.Nil(t, out)

	// Confirm the error class (exhaustion vs deadlock).
	var rerr *ResolveError
	require.True(t, errors.As(err, &rerr), "Resolve error must wrap *ResolveError")
	require.Equal(t, ResolveFailureExhaustion, rerr.Reason,
		"walked all K layers with NR-quorum at each → exhaustion, not deadlock")

	trace := s.instances[2].LastResolveLayerAttempts()
	require.Len(t, trace, s.K, "exhaustion walk must record one attempt per visited layer")
	for k := 0; k < s.K; k++ {
		require.Equal(t, k, trace[k].Layer)
		require.False(t, trace[k].SigmaReached, "L_%d σ-pool empty (no σ-side emissions)", k)
		require.False(t, trace[k].Decided)
	}
}

// TestLastResolveLayerAttempts_PreservedAcrossEndedInstance verifies the
// docstring claim: a Resolve call on an already-Finalize'd Instance
// returns ErrInstanceEnded BEFORE the per-call trace reset, so the
// prior trace survives. The consensustest framework relies on this
// (the adapter snapshots the trace at end-of-sim outcome construction,
// which may happen after Finalize).
func TestLastResolveLayerAttempts_PreservedAcrossEndedInstance(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	s.runPhase2(nil)

	// Populate the trace via a successful Resolve.
	_, err := s.instances[2].Resolve()
	require.NoError(t, err)
	traceBefore := s.instances[2].LastResolveLayerAttempts()
	require.NotEmpty(t, traceBefore)

	// Finalize → ended=true. Subsequent Resolve must return
	// ErrInstanceEnded and leave the trace untouched.
	s.instances[2].Finalize()
	out, err := s.instances[2].Resolve()
	require.ErrorIs(t, err, ErrInstanceEnded)
	require.Nil(t, out)

	traceAfter := s.instances[2].LastResolveLayerAttempts()
	require.Equal(t, len(traceBefore), len(traceAfter),
		"ended-instance Resolve must not clear prior trace")
	for i := range traceBefore {
		require.Equal(t, traceBefore[i], traceAfter[i],
			"trace entry %d mutated across ended-instance Resolve", i)
	}
}

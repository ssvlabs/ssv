package twoab

import (
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestResolve_HealthyClusterReachesSigmaAtL0: all ops emit KindValue
// (σ partial inline); Resolve reconstructs at L_0 with V_0.
func TestResolve_HealthyClusterReachesSigmaAtL0(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "σ-quorum reached at L_0")
	require.Equal(t, Value("V0"), out.Value)
}

// TestResolve_FallThroughToL1WhenNoOneHasV0: h_V_honest=0 → all emit NR
// → nr_tag_0-pool reaches qEnc → fall-through to L_1.
func TestResolve_FallThroughToL1WhenNoOneHasV0(t *testing.T) {
	s := newSim(t, 4)
	// Nobody has V_0 at L_0. But L_1 leader delivers and host says valid.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer)
	require.Equal(t, s.candidates[1], out.Value)
}

// TestResolve_NoVAtAnyLayerReturnsNoQuorum.
func TestResolve_NoVAtAnyLayerReturnsNoQuorum(t *testing.T) {
	s := newSim(t, 4)
	// Nothing at L_0, nothing at L_1.
	s.firePhase2aAll()
	_, errs := s.resolveAll()
	for op, err := range errs {
		require.True(t, errors.Is(err, ErrNoQuorum), "op %d should get NoQuorum, got %v", op, err)
	}
}

// TestResolve_LeaderEquivocationFallsThrough: leader equivocates at L_0
// → equivocation trigger fires for all → all emit NR-direct or Phase-2b
// NR → fall-through to L_1.
func TestResolve_LeaderEquivocationFallsThrough(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		s.allOperators(), s.allOperators(), observedEarly)
	// L_1 also delivers (so fall-through has a target).
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer)
}

// TestResolve_Idempotent.
func TestResolve_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	out1, err := s.instances[OperatorID(1)].Resolve()
	require.NoError(t, err)
	out2, err := s.instances[OperatorID(1)].Resolve()
	require.NoError(t, err)
	require.Equal(t, out1.Layer, out2.Layer)
	require.Equal(t, out1.Value, out2.Value)
}

func TestBuildCertificate_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	cert, err := s.instances[OperatorID(1)].BuildCertificate(&Output{
		Layer:     0,
		Value:     Value("V0"),
		Signature: Signature("sig"),
	})
	require.NoError(t, err)
	require.Equal(t, Value("V0"), cert.Value)
}

func TestBuildCertificate_RejectsNil(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[OperatorID(1)].BuildCertificate(nil)
	require.Error(t, err)
}

func TestObserveCertificate_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	out, err := s.instances[OperatorID(1)].Resolve()
	require.NoError(t, err)
	cert, err := s.instances[OperatorID(1)].BuildCertificate(out)
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(2)].ObserveCertificate(cert))
	require.NotNil(t, s.instances[OperatorID(2)].RetainedCertificate())
}

func TestRetainedCertificate_NilBeforeObserve(t *testing.T) {
	s := newSim(t, 4)
	require.Nil(t, s.instances[OperatorID(1)].RetainedCertificate())
}

// TestResolve_K3_MultiLayerFallThrough: at K=3 with no L_0 or L_1
// retention (only L_2 has a healthy leader broadcast), the cluster
// walks Phase 3 through TWO consecutive NR-quorum unlocks (L_0 →
// L_1 → L_2) before reaching σ-quorum at L_2. This exercises the
// chain-decryption walk: nr_tag_0-pool unlocks L_1; nr_tag_1-pool
// (from Phase-2a L_1 NRPlaintext entries) unlocks L_2; L_2
// SigmaChained entries decrypt and aggregate to σ-quorum.
func TestResolve_K3_MultiLayerFallThrough(t *testing.T) {
	s := newSimWithK(t, 4, 3)
	// Only L_2 leader broadcasts. Apply host validity for V_2 on all ops.
	v2 := s.candidates[2]
	s.deliverPhase1(2, v2, s.allOperators(), observedEarly)
	s.applyHostValidityAll(2, v2, true)
	// L_0 and L_1 have no retention — all ops will emit KindNoValue at
	// Phase 2a with L_1 NRPlaintext (no V_1) + L_2 SigmaChained (V_2 +
	// host valid).
	s.firePhase2aAll()
	// Resolve at each op should walk all three layers and decide at L_2.
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 2, out.Layer, "σ-quorum reaches at L_2 via two consecutive NR-quorum unlocks")
	require.Equal(t, v2, out.Value)
}

// TestResolve_FakeEncryptedPresenceFiresRule4: at L_k>0, a peer's
// SigmaChained LayerEntry whose Payload doesn't decrypt to a valid σ
// partial fires Rule 4. We trigger this by:
//  1. Engineering an h_V_honest=0 fall-through at L_0 (no L_0 V delivery
//     → all honest emit KindCommit-NR → nr_tag_0-pool reaches qEnc →
//     chain unlocks for L_1 decryption).
//  2. Injecting a peer KindNoValue with a SigmaChained L_1 entry whose
//     Payload is malformed bytes (not a valid stub-IBE ciphertext).
//  3. Driving Resolve, which derives nr_tag_0 and attempts to decrypt
//     the malformed L_1 entry → stub IBE Decrypt returns an error →
//     Rule 4 fires.
func TestResolve_FakeEncryptedPresenceFiresRule4(t *testing.T) {
	s := newSim(t, 4)
	// "byz" op1 stays silent at Phase 2a but injects a fake KindNoValue
	// (with a malformed L_1 SigmaChained entry) directly into op2's view.
	op2 := s.instances[OperatorID(2)]
	fake := &NoValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1, // claim it's from op1
		Height:     s.cfg.Height,
		LayerEntries: []LayerEntry{
			{
				Layer:   1,
				Kind:    LayerEntrySigmaChained,
				V:       Value("fake-V1"),
				Payload: []byte("garbage-not-a-valid-ibe-ciphertext-bytes"),
			},
		},
	}
	require.NoError(t, op2.ObserveNoValueMsg(fake))
	// Ops 2, 3, 4 fire Phase 2a → KindNoValue (none has V_0).
	for _, op := range []OperatorID{2, 3, 4} {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Cross-broadcast NoValues among ops 2, 3, 4 (op1 is silent). After
	// each Observe*, the cascade may fire NR-eligibility for the receiver
	// once their noValuePool reaches qEnc=3.
	honest := []OperatorID{2, 3, 4}
	for _, op := range honest {
		nv, ok := s.instances[op].OwnNoValueMsg()
		require.True(t, ok)
		for _, peer := range honest {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveNoValueMsg(nv))
		}
	}
	// Now each of ops 2, 3, 4 has noValuePool with their own + 2 peers + 1
	// fake (op2 only) = 3-4 entries, ≥ qEnc. NR-eligibility fired and each
	// emitted KindCommit-NR. Cross-broadcast commits from ops 3, 4 to op2
	// so op2's nr_tag_0-pool collects ≥ qEnc partials.
	for _, op := range []OperatorID{3, 4} {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok, "op %d should have emitted Commit-NR via cascade", op)
		require.Equal(t, CommitSideNR, c.Side)
		require.NoError(t, op2.ObserveCommit(c))
	}
	// Now op2 has nr_tag_0-pool with {op2 (self), op3, op4} = 3 = qEnc.
	// Resolve walks L_0 (σ-pool empty, NR-aggregates), then L_1: tries to
	// decrypt the fake's malformed SigmaChained payload → fails → Rule 4.
	_, err := op2.Resolve()
	require.Error(t, err, "no σ-quorum at any layer; expect ErrNoQuorum")
	var foundRule4 bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceFakeEncryptedPresence {
			foundRule4 = true
			require.NotNil(t, e.FakeEncryptedPresence)
		}
	}
	require.True(t, foundRule4, "malformed L_1 SigmaChained should fire Rule 4 during Resolve")
}

// TestSelectWinningGroup verifies the σ-group selection helper:
// determinism-load-bearing lex-tiebreak on V when partial counts are
// equal. Without the tiebreak, equal-count groups would resolve based
// on map-iteration order (groups is built from sigmaPool which is a
// V_root-keyed map), producing nondeterministic Output across operators
// on transient pre-quorum states.
func TestSelectWinningGroup(t *testing.T) {
	mkPartials := func(ops ...OperatorID) map[OperatorID]Signature {
		out := make(map[OperatorID]Signature, len(ops))
		for _, op := range ops {
			out[op] = Signature{byte(op)}
		}
		return out
	}
	mkGroups := func(gs ...*sigGroup) map[[32]byte]*sigGroup {
		out := make(map[[32]byte]*sigGroup, len(gs))
		for _, g := range gs {
			out[ValueRoot(g.value)] = g
		}
		return out
	}

	t.Run("empty groups returns nil", func(t *testing.T) {
		require.Nil(t, selectWinningGroup(nil))
		require.Nil(t, selectWinningGroup(map[[32]byte]*sigGroup{}))
	})

	t.Run("single group wins", func(t *testing.T) {
		g := &sigGroup{value: Value("V0"), partials: mkPartials(1, 2, 3)}
		require.Equal(t, g, selectWinningGroup(mkGroups(g)))
	})

	t.Run("higher count wins", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(1, 2, 3)}
		require.Equal(t, gB, selectWinningGroup(mkGroups(gA, gB)))
	})

	t.Run("equal count: lex-smaller V wins", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1, 2)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(3, 4)}
		// Run many times to amortize map-iteration nondeterminism.
		// The result must be deterministic regardless.
		for i := 0; i < 100; i++ {
			require.Equal(t, gA, selectWinningGroup(mkGroups(gA, gB)),
				"iteration %d: equal-count lex-tiebreak must be deterministic", i)
		}
	})

	t.Run("equal count three-way: smallest V wins", func(t *testing.T) {
		gA := &sigGroup{value: Value("V_a"), partials: mkPartials(1)}
		gB := &sigGroup{value: Value("V_b"), partials: mkPartials(2)}
		gC := &sigGroup{value: Value("V_c"), partials: mkPartials(3)}
		for i := 0; i < 100; i++ {
			require.Equal(t, gA, selectWinningGroup(mkGroups(gA, gB, gC)),
				"iteration %d: equal-count 3-way lex-tiebreak must be deterministic", i)
		}
	})

	t.Run("byte-level lex order (not lexicographic-by-length)", func(t *testing.T) {
		gAB := &sigGroup{value: Value("AB"), partials: mkPartials(1)}
		gAC := &sigGroup{value: Value("AC"), partials: mkPartials(2)}
		require.Equal(t, gAB, selectWinningGroup(mkGroups(gAB, gAC)))
	})
}

// TestRecoverV_KGt0_FallsBackToRetainedBundle verifies the recoverV
// fallback at k>0: a σ-pool[k] entry seeded purely from the leader's
// plaintext LWitness (no peer SigmaChained entry carrying V_k observed yet)
// can still recover V's bytes from the retained bundle — so the witness
// head-start counts toward σ-quorum during reconstruction.
func TestRecoverV_KGt0_FallsBackToRetainedBundle(t *testing.T) {
	s := newSimWithK(t, 7, 3)
	const k = 2
	leader := s.leaderAt(k)
	v := s.candidates[k]
	b, err := s.instances[leader].BuildPhase1Bundle(k, v)
	require.NoError(t, err)
	recv := s.instances[OperatorID(5)]
	require.NoError(t, recv.ObservePhase1Bundle(b, observedEarly))
	// recv has the leader's witness in σ-pool[k] but has observed NO Phase-2a
	// SigmaChained entry carrying V_k. recoverV must still find V via the
	// retained-bundle fallback (mirrors the L_0 fallback).
	root := ValueRoot(v)
	got, ok := recv.recoverV(k, root)
	require.True(t, ok, "recoverV must fall back to the retained bundle at k>0")
	require.Equal(t, v, got)
}

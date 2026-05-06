package obft

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Protocol-level tests covering scenarios from docs/OBFT.md:
//
//   1. Healthy — all honest receive V_{L_0}, σ-quorum reaches at L_0.
//   2. Silent leader at L_0 — NR-quorum at L_0 → fall-through to L_1.
//   3. Equivocation, 2-1 split where leader's σ_L^V completes a 2-honest
//      σ-pool to qV → succeeds at L_0 on the majority V.
//   4. Equivocation, all-Defer outcome — every honest retains both V's
//      before σ-emit time → all force-NR at end of Phase 2 → NR-quorum at
//      L_0 → fall-through to L_1.
//   5. Equivocation, σ-locked split (1-1-Defer) — slot misses at L_0; no
//      fall-through (σ-locked operators can't NR).
//   6. Beyond f offline — neither σ nor NR quorum reaches; ErrNoQuorum.
//   7. Multi-failure fall-through — multiple silent layers; eventual σ at
//      first honest leader.
//   8. Late re-flood within Phase 2 — Defer-partition resolves on late V
//      delivery → late σ-emit → σ-pool reaches qV.
//   9. Cross-signing detection (Rule 1).
//  10. Validity-divergence — host returns NV; operator joins NR pool.

const observedEarly = 1200 * time.Millisecond // before T_commit (1500ms)

// ---- Healthy --------------------------------------------------------------

func TestObft_Healthy_n4(t *testing.T) {
	s := newSim(t, 4)
	// Every layer's leader broadcasts; every operator receives all bundles
	// well before T_commit and host-validates them.
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)

	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "should decide at L_0 in healthy case")
	require.True(t, bytes.Equal(s.candidates[0], out.Value))
}

// ---- Silent leader at L_0 -------------------------------------------------

func TestObft_SilentLeaderL0_n4(t *testing.T) {
	s := newSim(t, 4)
	// Layers 1..K-1 broadcast normally; L_0 leader is silent.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)

	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "should fall through to L_1")
	require.True(t, bytes.Equal(s.candidates[1], out.Value))
}

// ---- Multi-layer silent leaders (K=4 with L_0+L_1+L_2 silent) -------------

func TestObft_MultiSilent_n4(t *testing.T) {
	s := newSim(t, 4)
	// Only L_3 broadcasts.
	s.deliverPhase1(3, s.candidates[3], s.allOperators(), observedEarly, true)

	s.runPhase2(nil)

	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 3, out.Layer, "K=4 fall-through should reach L_3")
	require.True(t, bytes.Equal(s.candidates[3], out.Value))
}

// ---- One honest offline (within f-bound) ---------------------------------

func TestObft_OneOperatorOffline_n4(t *testing.T) {
	s := newSim(t, 4)
	// Operator 4 is offline — does not receive Phase-1 bundles, does not
	// emit Phase-2 messages. Within f=1 byzantine bound; cluster should
	// still reach σ-quorum at L_0 with the leader's σ + 2 honest non-leader
	// σ = 3 = qV.
	online := []OperatorID{1, 2, 3}
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], online, observedEarly, true)
	}

	excluded := map[OperatorID]bool{4: true}
	s.runPhase2(excluded)

	outputs := s.resolveAll(excluded)
	require.Len(t, outputs, 3) // only the 3 online operators report
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
}

// ---- Two operators offline (beyond f) ------------------------------------

func TestObft_TwoOperatorsOffline_n4_NoQuorum(t *testing.T) {
	s := newSim(t, 4)
	online := []OperatorID{1, 2}
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], online, observedEarly, true)
	}

	excluded := map[OperatorID]bool{3: true, 4: true}
	s.runPhase2(excluded)

	outputs := s.resolveAll(excluded)
	for _, out := range outputs {
		require.Nil(t, out, "no operator should reach quorum")
	}
}

// ---- Equivocation: all-Defer outcome → fall-through to L_1 ---------------

func TestObft_EquivocationAllDefer_n4(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader (op1) equivocates: V_a to {2, 3}, V_b to {3, 4}. Each
	// non-leader (2, 3, 4) thus sees BOTH V's (overlapping coverage):
	//   op2 sees V_a only ❌ (misses V_b)
	//   op3 sees V_a + V_b → Defer-equivocation
	//   op4 sees V_b only ❌
	// Hmm — to get all-Defer, all 3 non-leaders must see both V's. Let me
	// deliver V_a to {2,3,4} and V_b to {2,3,4}.
	vA := []byte("L_0-V_a")
	vB := []byte("L_0-V_b")
	s.deliverPhase1Equivocation(0, vA, vB,
		[]OperatorID{1, 2, 3, 4}, []OperatorID{1, 2, 3, 4},
		observedEarly, true)
	// L_1, L_2, L_3: normal broadcasts so fall-through finds an honest leader.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)

	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "all-Defer at L_0 should fall through to L_1")
	require.True(t, bytes.Equal(s.candidates[1], out.Value))

	// Each non-leader op should have recorded leader-equivocation evidence.
	for _, op := range []OperatorID{2, 3, 4} {
		ev := s.instances[op].Evidence()
		foundEquiv := false
		for _, e := range ev {
			if e.Rule == EvidenceLeaderEquivocation && e.Layer == 0 && e.OperatorID == 1 {
				foundEquiv = true
			}
		}
		require.True(t, foundEquiv, "op %d should have observed leader-equivocation evidence", op)
	}
}

// ---- Equivocation: σ-locked split (1-1-Defer) — slot misses at L_0 -------

func TestObft_EquivocationSigmaLockedSplit_n4(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader (op1, byzantine) delivers V_a only to {op2}, V_b only to
	// {op3}, nothing to {op4}. Op2 σ-emits on V_a (sole retained); op3
	// σ-emits on V_b; op4 has no V → Defer-partition (no peer σ-emit observed
	// at L_0 in the no-V fallback... wait, op4 sees op2 and op3's onions
	// later, but op4 has no retained V at L_0, so under the L_0 fallback
	// any auth-signed σ-claim counts as observed → op4 stays Defer).
	//
	// Final pools at L_0:
	//   σ-pool on V_a = {op2, op1's σ_L^V(V_a)} = 2 < qV=3
	//   σ-pool on V_b = {op3, op1's σ_L^V(V_b)} = 2 < qV=3
	//   NR-pool at L_0: op4 (force-NR at end of Phase 2). op2/op3 σ-locked.
	//                   = 1 < qEnc=3
	// → no quorum, no fall-through; slot misses at L_0.
	vA := []byte("L_0-V_a")
	vB := []byte("L_0-V_b")
	s.deliverPhase1Equivocation(0, vA, vB,
		[]OperatorID{1, 2}, []OperatorID{1, 3},
		observedEarly, true)
	// L_1, L_2, L_3 broadcast normally — but cluster should still miss
	// because L_0 σ-locked operators can't NR-emit at L_0, blocking
	// fall-through.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)

	outputs := s.resolveAll(nil)
	for op, out := range outputs {
		require.Nilf(t, out, "op %d should not reach quorum (σ-locked split)", op)
	}
}

// ---- Late re-flood within Phase 2 ---------------------------------------

func TestObft_LateRefloodWithinPhase2_n4(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader (op1) delivers V_{L_0} to op2 only at observedEarly. Op3
	// and op4 receive V_{L_0} late (within the receiver acceptance window
	// past T_commit) — modeled by passing a later observedOffset on those
	// ObservePhase1Bundle calls.
	//
	// Path: op2 σ-eligible at T_commit → emits σ. Op3, op4 in
	// Defer-partition at T_commit. Late re-flood delivers V to them within
	// [T_commit, T_accept_max]. They σ-emit late. At end of Phase 2,
	// σ-pool on V_{L_0} = {op1's σ_L^V, op2, op3, op4} = 4, includes 3
	// honest non-leader + 1 leader = qV. Resolve at L_0 succeeds.
	leader := s.leaderAt(0)
	leaderInst := s.instances[leader]
	v0 := s.candidates[0]
	bundle, err := leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)
	require.NoError(t, leaderInst.ApplyHostValidity(0, v0, true))

	// op2 sees on time.
	require.NoError(t, s.instances[2].ObservePhase1Bundle(bundle, observedEarly))
	require.NoError(t, s.instances[2].ApplyHostValidity(0, v0, true))

	// op3, op4 see late (but within accept window). T_commit = 1500ms,
	// Delta2 = 300ms, D+δ = 150ms → T_accept_max = 1500 + 300 - 150 = 1650ms.
	lateButOK := 1600 * time.Millisecond
	require.NoError(t, s.instances[3].ObservePhase1Bundle(bundle, lateButOK))
	require.NoError(t, s.instances[3].ApplyHostValidity(0, v0, true))
	require.NoError(t, s.instances[4].ObservePhase1Bundle(bundle, lateButOK))
	require.NoError(t, s.instances[4].ApplyHostValidity(0, v0, true))

	// L_1+ broadcast normally so fall-through has a backup if needed.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)
	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "late re-flood within accept window should still succeed at L_0")
	require.True(t, bytes.Equal(v0, out.Value))
}

// ---- Phase-1 bundle past T_accept_max → rejected -------------------------

func TestObft_Phase1BundlePastAcceptWindow(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	leaderInst := s.instances[leader]
	v0 := s.candidates[0]
	bundle, err := leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)

	// T_accept_max at the simulator config = 1650ms. Past that → rejected.
	pastAccept := 1700 * time.Millisecond
	err = s.instances[2].ObservePhase1Bundle(bundle, pastAccept)
	require.ErrorIs(t, err, ErrLatePhase1Bundle)
}

// ---- NV / validity divergence ------------------------------------------

func TestObft_HostNV_OneOperator_StillReachesQuorum(t *testing.T) {
	s := newSim(t, 4)
	// All 4 receive V_{L_0}, but op4 returns NV. Cluster has 3 σ-emitters
	// + leader's σ_L^V; that's 4 (includes leader's own contribution at
	// op1's role, but at the σ-pool we count distinct operators) → 3 distinct
	// σ partials = qV. Should still succeed at L_0.
	leader := s.leaderAt(0)
	leaderInst := s.instances[leader]
	v0 := s.candidates[0]
	bundle, err := leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)
	require.NoError(t, leaderInst.ApplyHostValidity(0, v0, true))

	for _, rcp := range []OperatorID{2, 3} {
		require.NoError(t, s.instances[rcp].ObservePhase1Bundle(bundle, observedEarly))
		require.NoError(t, s.instances[rcp].ApplyHostValidity(0, v0, true))
	}
	// op4 says NV.
	require.NoError(t, s.instances[4].ObservePhase1Bundle(bundle, observedEarly))
	require.NoError(t, s.instances[4].ApplyHostValidity(0, v0, false))

	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)
	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.True(t, bytes.Equal(v0, out.Value))
	// op4 should be in NV state at layer 0.
	require.Equal(t, CommitNV, s.instances[4].LocalState(0))
}

// ---- Local-state inspection -----------------------------------------------

func TestObft_LeaderAtLayers(t *testing.T) {
	s := newSim(t, 4)
	for op, inst := range s.instances {
		ll := inst.LeaderAtLayers()
		// At K=4 = n, every operator leads exactly one layer.
		require.Lenf(t, ll, 1, "op %d should lead exactly one layer at K=n=4", op)
	}
}

// ---- BuildPhase1Bundle EKM enforcement -----------------------------------

func TestObft_BuildPhase1Bundle_RejectsSecondV(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	leaderInst := s.instances[leader]
	v0 := s.candidates[0]
	_, err := leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)
	// Second call with a different V at the same (slot, layer) must be
	// rejected by EKM-style enforcement.
	_, err = leaderInst.BuildPhase1Bundle(0, []byte("different-V"))
	require.ErrorIs(t, err, ErrSigmaLocked)
	// Idempotent on same V.
	_, err = leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)
}

// ---- BuildPhase1Bundle rejects non-leader -------------------------------

func TestObft_BuildPhase1Bundle_RejectsNonLeader(t *testing.T) {
	s := newSim(t, 4)
	// op2 is the leader at L_1, not L_0. They cannot BuildPhase1Bundle for L_0.
	_, err := s.instances[2].BuildPhase1Bundle(0, []byte("V"))
	require.Error(t, err)
}

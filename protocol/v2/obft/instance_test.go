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
//   4. Equivocation, all-honest-NR outcome — every honest retains both V's
//      before T_commit → all NR per equivocation rule → NR-quorum at L_0
//      → fall-through to L_1.
//   5. Equivocation, σ-locked split (1-1) — slot misses at L_0; no
//      fall-through (σ-locked operators can't NR).
//   6. Beyond f offline — neither σ nor NR quorum reaches; ErrNoQuorum.
//   7. Multi-failure fall-through — multiple silent layers; eventual σ at
//      first honest leader.
//   8. Asymmetric L_0 propagation — bundle dropped on the wire → cluster
//      falls through to L_1.
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
	// σ = 3 = qV. Every online operator must reconstruct LOCALLY (not via
	// certificate gossip from the leader) — this is the regression test for
	// the M1 finding that own σ/NR contributions were excluded from the
	// local Resolve pool.
	online := []OperatorID{1, 2, 3}
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], online, observedEarly, true)
	}

	excluded := map[OperatorID]bool{4: true}
	s.runPhase2(excluded)

	outputs := s.resolveAll(excluded)
	require.Len(t, outputs, 3) // only the 3 online operators report
	out := requireAllReconstruct(t, outputs)
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

// ---- Equivocation: all-honest-NR outcome → fall-through to L_1 -----------

func TestObft_EquivocationAllNR_n4(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader (op1) equivocates: deliver V_a to {2,3,4} and V_b to
	// {2,3,4}. All non-leaders retain ≥ 2 distinct V's by T_commit; per
	// the equivocation rule (no winner-picking under f=1), they NR at L_0.
	// NR-quorum at L_0 reaches; cluster falls through to L_1.
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
	require.Equal(t, 1, out.Layer, "all-honest-NR at L_0 should fall through to L_1")
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

// ---- Equivocation: σ-locked split (1-1) — slot misses at L_0 ------------

func TestObft_EquivocationSigmaLockedSplit_n4(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader (op1, byzantine) delivers V_a only to {op2}, V_b only to
	// {op3}, nothing to {op4}. Op2 σ-emits on V_a (sole retained); op3
	// σ-emits on V_b; op4 has no V at T_commit → NR per silent-leader rule.
	//
	// Final pools at L_0:
	//   σ-pool on V_a = {op2, op1's σ_L^V(V_a)} = 2 < qV=3
	//   σ-pool on V_b = {op3, op1's σ_L^V(V_b)} = 2 < qV=3
	//   NR-pool at L_0: op4 (silent-leader NR). op2/op3 σ-locked.
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

// ---- Asymmetric propagation past T_commit → fall-through to L_1 ---------

func TestObft_AsymmetricPropagation_FallsThroughToL1_n4(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader (op1) builds and self-observes a Phase-1 bundle but the
	// bundle never reaches any non-leader peer (modeled by NOT calling
	// ObservePhase1Bundle on receivers). op2/op3/op4 NR at L_0 per the
	// silent-leader rule; op1 is σ-locked from Phase 1 so cross-phase
	// exclusivity prevents them from NR-ing. NR-pool at L_0 = 3 = qEnc;
	// cluster falls through to L_1 (where op2 broadcasts honestly).
	leader := s.leaderAt(0)
	leaderInst := s.instances[leader]
	v0 := s.candidates[0]
	_, err := leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)
	require.NoError(t, leaderInst.ApplyHostValidity(0, v0, true))
	// Don't deliver to peers — simulating bundle dropped on the wire.

	// L_1+ broadcast normally so fall-through has a backup.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	s.runPhase2(nil)
	outputs := s.resolveAll(nil)
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "asymmetric L_0 propagation falls through to L_1")
	require.True(t, bytes.Equal(s.candidates[1], out.Value))
}

// ---- Phase-1 bundle past T_commit → rejected ----------------------------

func TestObft_Phase1BundlePastTCommit(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	leaderInst := s.instances[leader]
	v0 := s.candidates[0]
	bundle, err := leaderInst.BuildPhase1Bundle(0, v0)
	require.NoError(t, err)

	// T_commit at the simulator config = 1500ms. Past that → rejected.
	pastTCommit := 1600 * time.Millisecond
	err = s.instances[2].ObservePhase1Bundle(bundle, pastTCommit)
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

// ---- Evidence: leader cross-signing via Phase-1 σ + KindCommit NR (E1) ---

func TestObft_Evidence_Rule1_LeaderPhase1SigmaPlusNR(t *testing.T) {
	s := newSim(t, 4)
	// op1 is L_0 leader. They σ-emit via Phase-1 bundle, then byzantinely
	// emit an NR partial at L_0 in their KindCommit. An honest peer
	// observing both should record Rule 1 evidence against op1.
	v0 := s.candidates[0]
	all := s.allOperators()
	s.deliverPhase1(0, v0, all, observedEarly, true)

	// Construct a byzantine KindCommit from op1: empty σ at L_0, NR partial at L_0.
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := signer.SignPartial(tag)
	require.NoError(t, err)
	byzCommit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{{Layer: 0, PartialSig: nrSig}},
	}

	// op2 (honest) observes; should record Rule 1 against op1.
	require.NoError(t, s.instances[2].ObserveCommit(byzCommit))
	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceCrossSigning && e.OperatorID == 1 && e.Layer == 0 {
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 1 evidence against op1 at L_0; got %+v", ev)
}

// ---- Evidence: leader cross-onion equivocation via Phase-1 σ + onion (E2) ---

func TestObft_Evidence_Rule3_LeaderPhase1SigmaPlusOnion(t *testing.T) {
	s := newSim(t, 4)
	// op1 is L_0 leader. They σ-emit V_a via Phase-1 bundle, then send a
	// distinct σ-side onion entry on V_b in their KindCommit at L_0 — single-σ-V
	// exclusivity violation across phases.
	vA := []byte("V-a")
	vB := []byte("V-b")
	s.deliverPhase1(0, vA, []OperatorID{1, 2, 3, 4}, observedEarly, true)

	// Construct byzantine KindCommit from op1 with σ on V_b at L_0.
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	sigB, err := signer.SignPartial(vB)
	require.NoError(t, err)
	layers := make([]EncryptedLayer, s.K)
	layers[0] = EncryptedLayer{Value: append(Value{}, vB...), Ciphertext: sigB}
	byzCommit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     layers,
	}

	require.NoError(t, s.instances[2].ObserveCommit(byzCommit))
	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceCrossOnionEquivocation && e.OperatorID == 1 && e.Layer == 0 &&
			e.CrossOnionEquivocation != nil &&
			!bytes.Equal(e.CrossOnionEquivocation.ValueA, e.CrossOnionEquivocation.ValueB) {
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 3 evidence (V_a vs V_b) at L_0 op1; got %+v", ev)
}

// ---- Evidence: second distinct KindCommit per operator (E3) ---

func TestObft_Evidence_Rule3_SecondKindCommit(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]
	s.deliverPhase1(0, v0, []OperatorID{1, 2, 3, 4}, observedEarly, true)

	// op1 emits a legitimate KindCommit, then a structurally-distinct second
	// one. The receiver records cross-onion-equivocation evidence (Layer = -1
	// indicating "spans the whole commit").
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	sigA, err := signer.SignPartial(v0)
	require.NoError(t, err)
	commit1 := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
	}
	commit1.Layers[0] = EncryptedLayer{Value: append(Value{}, v0...), Ciphertext: sigA}

	// Distinct second commit: same content but with an extra NR partial at L_1.
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 1)
	nrSig, err := signer.SignPartial(tag)
	require.NoError(t, err)
	commit2 := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     append([]EncryptedLayer{}, commit1.Layers...),
		NRPartials: []NRPartial{{Layer: 1, PartialSig: nrSig}},
	}

	require.NoError(t, s.instances[2].ObserveCommit(commit1))
	require.NoError(t, s.instances[2].ObserveCommit(commit2))

	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceCrossOnionEquivocation && e.OperatorID == 1 && e.Layer == -1 {
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 3 (second-distinct-commit) against op1; got %+v", ev)

	// Identical re-broadcast of commit2 must not record additional evidence.
	beforeCount := len(ev)
	require.NoError(t, s.instances[2].ObserveCommit(commit2))
	require.Len(t, s.instances[2].Evidence(), beforeCount, "identical re-broadcast must be a no-op")
}

// ---- Witnesses (M2): leader-σ_L^V re-broadcast in KindCommit ---

// TestObft_Witness_BuildOwnCommit_PacksRetainedBundles — every retained
// (layer, leader, V, σ_V) appears as a witness in the operator's KindCommit.
func TestObft_Witness_BuildOwnCommit_PacksRetainedBundles(t *testing.T) {
	s := newSim(t, 4)
	all := s.allOperators()
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], all, observedEarly, true)
	}

	// op2 (non-leader at L_0; leader at L_1) builds their commit. They
	// retained Phase-1 bundles for all K leaders — one witness per layer.
	c, err := s.instances[2].BuildOwnCommit()
	require.NoError(t, err)
	require.Len(t, c.Witnesses, s.K, "expected one witness per retained layer")

	for layer := 0; layer < s.K; layer++ {
		expectedLeader := s.leaderAt(layer)
		expectedV := s.candidates[layer]
		found := false
		for _, w := range c.Witnesses {
			if w.Layer == layer && w.Leader == expectedLeader && bytes.Equal(w.Value, expectedV) {
				require.NotEmpty(t, w.SigmaV, "witness at L_%d has empty SigmaV", layer)
				found = true
				break
			}
		}
		require.Truef(t, found, "missing witness for L_%d leader %d", layer, expectedLeader)
	}
}

// TestObft_Witness_RehydratesMissedPhase1 — a single receiver who missed the
// L_0 Phase-1 broadcast (gossipsub drop) still rehydrates the leader's σ_V
// from a peer's KindCommit witness and reconstructs locally at L_0.
//
// Setup: op4 misses the L_0 bundle; op1/op2/op3 receive it. In Phase 2
// op1/op2/op3 σ at L_0; op4 NRs at L_0 (no retained V locally). Without M2,
// op4's L_0 σ-pool tops out at 2 (op2's σ + op3's σ) — qV=3 unreachable.
// With M2, op2/op3's KindCommits carry witnesses for op1's bundle, which
// rehydrate op4's bundles map; op4 then has 3 partials in the L_0 σ-pool
// and reconstructs locally.
func TestObft_Witness_RehydratesMissedPhase1(t *testing.T) {
	s := newSim(t, 4)

	// Partition L_0: only op1, op2, op3 receive the bundle. op4 (= L_3 leader,
	// non-leader at L_0) misses.
	s.deliverPhase1(0, s.candidates[0], []OperatorID{1, 2, 3}, observedEarly, true)
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	// Pre-Phase-2 sanity: op4 has no L_0 bundle.
	require.Empty(t, s.instances[4].bundles[0][1], "op4 should not yet have L_0 bundle from op1")

	// Phase 2: every op builds and broadcasts their KindCommit. op2 and op3
	// retained op1's L_0 bundle and pack it as a witness.
	s.runPhase2(nil)

	// op4 rehydrated op1's L_0 bundle from at least one peer's witness.
	rehydrated := s.instances[4].bundles[0][1]
	require.NotEmpty(t, rehydrated, "op4 should have rehydrated op1's L_0 bundle from witness")
	require.True(t, bytes.Equal(rehydrated[0].Value, s.candidates[0]))

	// op4 successfully reconstructs at L_0 locally. Pool: op1 σ (rehydrated)
	// + op2 σ + op3 σ = 3 = qV. (op4 itself NR'd at L_0, so contributes no σ.)
	out, err := s.instances[4].Resolve()
	require.NoError(t, err)
	require.Equal(t, 0, out.Layer)
	require.True(t, bytes.Equal(out.Value, s.candidates[0]))
}

// TestObft_Witness_BadSigmaDropped — a witness whose σ doesn't verify is
// silently dropped (doesn't poison the bundles map; doesn't error out the
// whole ObserveCommit).
func TestObft_Witness_BadSigmaDropped(t *testing.T) {
	s := newSim(t, 4)
	all := s.allOperators()
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], all, observedEarly, true)
	}

	// op2 builds a commit, then we corrupt one witness's SigmaV.
	c, err := s.instances[2].BuildOwnCommit()
	require.NoError(t, err)
	require.NotEmpty(t, c.Witnesses)

	// Find the L_0 witness and corrupt it.
	for i := range c.Witnesses {
		if c.Witnesses[i].Layer == 0 {
			c.Witnesses[i].SigmaV = []byte("garbage-sig-not-the-real-one")
			break
		}
	}

	// op3 observes the corrupted commit. The bad witness must be dropped
	// without affecting the rest of the commit's processing.
	preCount := len(s.instances[3].bundles[0][1])
	require.NoError(t, s.instances[3].ObserveCommit(c))
	postCount := len(s.instances[3].bundles[0][1])
	// op3 had already directly observed op1's L_0 bundle (via deliverPhase1
	// to all); the corrupted witness shouldn't add a duplicate or override.
	require.Equal(t, preCount, postCount, "corrupted witness must not enter bundles")
}

// TestObft_Witness_RejectsBadLeaderClaim — ValidateCommit rejects a witness
// whose Leader doesn't match the layer's expected leader.
func TestObft_Witness_RejectsBadLeaderClaim(t *testing.T) {
	s := newSim(t, 4)
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		Witnesses: []LeaderSigmaWitness{
			{Layer: 0, Leader: 2 /* wrong: L_0 leader is op1 */, Value: []byte("V"), SigmaV: []byte("sig")},
		},
	}
	err := ValidateCommit(c, s.cfg)
	require.ErrorContains(t, err, "leader")
}

// TestObft_Witness_TriggersRule2OnDistinctV — observing two witnesses with
// distinct V's at same (layer, leader) triggers Rule 2 evidence.
func TestObft_Witness_TriggersRule2OnDistinctV(t *testing.T) {
	s := newSim(t, 4)
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	vA := []byte("V-a")
	vB := []byte("V-b")
	sigA, err := signer.SignPartial(vA)
	require.NoError(t, err)
	sigB, err := signer.SignPartial(vB)
	require.NoError(t, err)

	// Two commits from different ops, each carrying a witness for op1 at
	// L_0 with a distinct V — modeling op1 having equivocated their Phase-1
	// bundle, with each peer retaining a different V.
	c2 := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		Witnesses:  []LeaderSigmaWitness{{Layer: 0, Leader: 1, Value: vA, SigmaV: sigA}},
	}
	c3 := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 3,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		Witnesses:  []LeaderSigmaWitness{{Layer: 0, Leader: 1, Value: vB, SigmaV: sigB}},
	}

	receiver := s.instances[4]
	require.NoError(t, receiver.ObserveCommit(c2))
	require.NoError(t, receiver.ObserveCommit(c3))

	ev := receiver.Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceLeaderEquivocation && e.OperatorID == 1 && e.Layer == 0 {
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 2 evidence from witnesses with distinct V's; got %+v", ev)
}

// ---- G2: ClusterID binding in inner messages ---

// TestObft_ClusterID_Phase1BundleSetByBuilder — BuildPhase1Bundle stamps the
// instance's ClusterID into the emitted bundle.
func TestObft_ClusterID_Phase1BundleSetByBuilder(t *testing.T) {
	s := newSim(t, 4)
	bundle, err := s.instances[1].BuildPhase1Bundle(0, []byte("V"))
	require.NoError(t, err)
	require.Equal(t, s.cfg.ClusterID, bundle.ClusterID)
}

// TestObft_ClusterID_CommitSetByBuilder — BuildOwnCommit stamps the
// instance's ClusterID into the emitted commit.
func TestObft_ClusterID_CommitSetByBuilder(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	c, err := s.instances[2].BuildOwnCommit()
	require.NoError(t, err)
	require.Equal(t, s.cfg.ClusterID, c.ClusterID)
}

// TestObft_ClusterID_RejectsMismatchedPhase1Bundle — a bundle with the wrong
// ClusterID is rejected by ValidatePhase1Bundle and never enters bundles.
func TestObft_ClusterID_RejectsMismatchedPhase1Bundle(t *testing.T) {
	s := newSim(t, 4)
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	v := []byte("V")
	sig, err := signer.SignPartial(v)
	require.NoError(t, err)
	bundle := &Phase1Bundle{
		ClusterID:  [32]byte{0xDE, 0xAD, 0xBE, 0xEF}, // wrong cluster
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layer:      0,
		Value:      v,
		SigmaV:     sig,
	}
	err = ValidatePhase1Bundle(bundle, s.cfg)
	require.ErrorContains(t, err, "cluster id")
	// Defense-in-depth: also rejected at the Instance API.
	err = s.instances[2].ObservePhase1Bundle(bundle, observedEarly)
	require.ErrorContains(t, err, "cluster id")
}

// TestObft_ClusterID_RejectsMismatchedCommit — a Commit with wrong ClusterID
// is rejected at ObserveCommit (cross-cluster replay defense).
func TestObft_ClusterID_RejectsMismatchedCommit(t *testing.T) {
	s := newSim(t, 4)
	c := &Commit{
		ClusterID:  [32]byte{0xDE, 0xAD},
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
	}
	err := ValidateCommit(c, s.cfg)
	require.ErrorContains(t, err, "cluster id")
	err = s.instances[3].ObserveCommit(c)
	require.ErrorContains(t, err, "cluster id")
}

// TestObft_ClusterID_RejectsMismatchedCertificate — a Certificate with wrong
// ClusterID is rejected.
func TestObft_ClusterID_RejectsMismatchedCertificate(t *testing.T) {
	s := newSim(t, 4)
	c := &Certificate{
		ClusterID: [32]byte{0xCA, 0xFE},
		Height:    s.cfg.Height,
		Value:     []byte("V"),
		Signature: []byte("sig"),
	}
	err := ValidateCertificate(c, s.cfg)
	require.ErrorContains(t, err, "cluster id")
}

// TestObft_ClusterID_NRTagBinding — NR partials are signed over
// NoQuorumTag(ClusterID, Height, Layer); a partial signed for cluster A
// fails verification under cluster B's tag construction. This is what
// prevents cross-cluster replay of NR partials independently of the
// ClusterID structural check on the carrier Commit.
func TestObft_ClusterID_NRTagBinding(t *testing.T) {
	clusterA := [32]byte{0x11}
	clusterB := [32]byte{0x22}
	const height = 100
	const layer = 0

	tagA := NoQuorumTag(clusterA, height, layer)
	tagB := NoQuorumTag(clusterB, height, layer)
	require.NotEqual(t, tagA, tagB, "NR tags for different clusters must differ")

	// Sign tag_A with op2's share, attempt to verify against tag_B → must fail.
	signer := NewStubSigner(3, []byte{2})
	partial, err := signer.SignPartial(tagA)
	require.NoError(t, err)
	require.True(t, signer.VerifyPartial([]byte{2}, tagA, partial),
		"tag_A partial verifies against tag_A")
	require.False(t, signer.VerifyPartial([]byte{2}, tagB, partial),
		"tag_A partial must NOT verify against tag_B (cross-cluster replay rejection)")
}

// TestObft_ClusterID_BlocksCrossClusterReplay — a complete cross-cluster
// replay scenario: cluster A produces a Phase1Bundle; an attacker forwards
// it to cluster B (same Height by accident); cluster B operators reject it.
func TestObft_ClusterID_BlocksCrossClusterReplay(t *testing.T) {
	clusterA := newSim(t, 4)
	clusterB := newSim(t, 4)
	clusterB.cfg.ClusterID = [32]byte{0x11, 0x22, 0x33}
	// Re-create cluster B's instances with the new ClusterID so their cfg
	// matches; we only test the validation gate, not full reconstruction.
	bInst, err := NewInstance(clusterB.cfg, 2, NewStubSigner(clusterB.cfg.QV(), []byte{2}), nil, NewStubIBE(clusterB.cfg.QV()), nil, clusterB.pubKeyShares, nil)
	require.NoError(t, err)

	bundle, err := clusterA.instances[1].BuildPhase1Bundle(0, []byte("V"))
	require.NoError(t, err)
	require.Equal(t, clusterA.cfg.ClusterID, bundle.ClusterID)
	require.NotEqual(t, clusterA.cfg.ClusterID, clusterB.cfg.ClusterID)

	// Replay clusterA's bundle into clusterB's instance — must be rejected
	// by ClusterID check (independent of σ verification, which would also
	// fail since the V-key shares differ).
	err = bInst.ObservePhase1Bundle(bundle, observedEarly)
	require.ErrorContains(t, err, "cluster id")
}

// ---- M3: end-to-end with per-layer staggered broadcast deadlines ---

// TestObft_BroadcastBudget_HealthyEndToEnd — the simulator runs a healthy
// scenario through a config built with the spec-recommended staggered
// schedule (B_0 < B_1 < B_2 < B_3, deepest ≥ BFT-min). All ops reconstruct
// at L_0 just like the uniform-cap healthy test.
func TestObft_BroadcastBudget_HealthyEndToEnd(t *testing.T) {
	s := newSimWithStaggeredBudgets(t, 4)
	all := s.allOperators()
	// observedAt is just before T_commit, satisfying observedTimeOK regardless
	// of the (rebased) T_commit chosen by newSimWithStaggeredBudgets.
	observedAt := s.cfg.TCommit - 10*time.Millisecond
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], all, observedAt, true)
	}
	s.runPhase2(nil)
	out := requireAllReconstruct(t, s.resolveAll(nil))
	require.Equal(t, 0, out.Layer)
}

package base

import (
	"bytes"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/protocol/v2/obft"
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
	tag := obft.NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
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

// TestObft_Evidence_Rule1_LeaderNRBeforePhase1Bundle is the reverse-order
// twin of TestObft_Evidence_Rule1_LeaderPhase1SigmaPlusNR: the byzantine
// leader's KindCommit (carrying NR at their own layer) is observed BEFORE
// their Phase-1 bundle. Per spec §Cross-signing detection, Rule 1 detection
// is "Immediate (dual partials on the wire)" — order-independent.
func TestObft_Evidence_Rule1_LeaderNRBeforePhase1Bundle(t *testing.T) {
	s := newSim(t, 4)
	v0 := s.candidates[0]

	// 1) Build the byzantine NR-at-own-layer KindCommit OUTSIDE the leader's
	// instance (bypassing EKM). op2 (honest receiver) observes it.
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	tag := obft.NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrSig, err := signer.SignPartial(tag)
	require.NoError(t, err)
	byzCommit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{{Layer: 0, PartialSig: nrSig}},
	}
	require.NoError(t, s.instances[2].ObserveCommit(byzCommit))

	// At this point Rule 1 cannot have fired yet — op2 hasn't seen the
	// leader's Phase-1 σ.
	for _, e := range s.instances[2].Evidence() {
		require.NotEqualf(t, EvidenceCrossSigning, e.Rule,
			"Rule 1 must not fire before Phase-1 bundle is observed")
	}

	// 2) Now the leader's Phase-1 bundle arrives at op2. The bundle's σ_V
	// signs v0. Rule 1 should fire from ObservePhase1Bundle now that both
	// sides of the cross-signing pair are present.
	leaderBundle, err := s.instances[1].BuildPhase1Bundle(0, v0)
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObservePhase1Bundle(leaderBundle, observedEarly))

	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceCrossSigning && e.OperatorID == 1 && e.Layer == 0 {
			require.NotNil(t, e.CrossSigning, "Rule 1 evidence must carry payload")
			require.True(t, bytes.Equal(e.CrossSigning.SigmaValue, v0),
				"Rule 1 SigmaValue must match the leader's σ_V target")
			require.True(t, bytes.Equal(e.CrossSigning.NRPartial, nrSig),
				"Rule 1 NRPartial must match the byzantine NR partial")
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 1 evidence against op1 at L_0 after reverse-order delivery; got %+v", ev)
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

// TestObft_Evidence_Rule3_LeaderOnionBeforePhase1Bundle is the reverse-order
// twin of TestObft_Evidence_Rule3_LeaderPhase1SigmaPlusOnion: the byzantine
// leader's L_0 onion entry on V_b is observed BEFORE their Phase-1 bundle on
// V_a. Per spec §Cross-onion partial-sig equivocation, detection is
// "Immediate (two σ partials on different V)" — order-independent.
func TestObft_Evidence_Rule3_LeaderOnionBeforePhase1Bundle(t *testing.T) {
	s := newSim(t, 4)
	vA := []byte("V-a")
	vB := []byte("V-b")

	// 1) op1 (L_0 leader) byzantinely emits a KindCommit with σ on V_b at L_0.
	// op2 (honest) observes this first.
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

	// At this point Rule 3 cannot have fired yet — op2 hasn't seen the
	// leader's Phase-1 σ on V_a. The L_0 onion entry should sit in
	// peerOnions[0][op1] (verified σ on V_b against op1's share — verdict =
	// l0SigmaUnknownV, retained but no evidence).
	for _, e := range s.instances[2].Evidence() {
		require.NotEqualf(t, EvidenceCrossOnionEquivocation, e.Rule,
			"Rule 3 must not fire before Phase-1 bundle is observed; got %+v", e)
	}

	// 2) Leader's Phase-1 bundle on V_a now arrives at op2. The retroactive
	// Rule 3 check in reevaluateL0Sigmas should fire.
	leaderBundle, err := s.instances[1].BuildPhase1Bundle(0, vA)
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObservePhase1Bundle(leaderBundle, observedEarly))

	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceCrossOnionEquivocation && e.OperatorID == 1 && e.Layer == 0 &&
			e.CrossOnionEquivocation != nil &&
			!bytes.Equal(e.CrossOnionEquivocation.ValueA, e.CrossOnionEquivocation.ValueB) {
			// Bundle V (vA) vs onion V (vB) should be the pair recorded.
			va, vb := e.CrossOnionEquivocation.ValueA, e.CrossOnionEquivocation.ValueB
			require.True(t,
				(bytes.Equal(va, vA) && bytes.Equal(vb, vB)) ||
					(bytes.Equal(va, vB) && bytes.Equal(vb, vA)),
				"Rule 3 evidence must pair vA and vB; got ValueA=%q ValueB=%q", va, vb)
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 3 evidence (vA vs vB) at L_0 op1 after reverse-order delivery; got %+v", ev)
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
	tag := obft.NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 1)
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
	var topLevel *Evidence
	for idx := range ev {
		if ev[idx].Rule == EvidenceCrossOnionEquivocation && ev[idx].OperatorID == 1 && ev[idx].Layer == -1 {
			topLevel = &ev[idx]
			break
		}
	}
	require.NotNil(t, topLevel, "expected Rule 3 (second-distinct-commit) against op1; got %+v", ev)

	// Slashable payload: both Commit bodies must be present and structurally
	// distinct (different content hashes).
	require.NotNil(t, topLevel.CommitEquivocation, "top-level Rule 3 must carry CommitEquivocation payload")
	require.NotNil(t, topLevel.CommitEquivocation.CommitA)
	require.NotNil(t, topLevel.CommitEquivocation.CommitB)
	hashA := commitContentHash(topLevel.CommitEquivocation.CommitA)
	hashB := commitContentHash(topLevel.CommitEquivocation.CommitB)
	require.NotEqual(t, hashA, hashB, "evidence payload must carry two structurally-distinct Commits")

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
		expectedRoot := ValueRoot(expectedV)
		for _, w := range c.Witnesses {
			if w.Layer == layer && w.Leader == expectedLeader && w.ValueRoot == expectedRoot {
				require.NotEmpty(t, w.SigmaV, "witness at L_%d has empty SigmaV", layer)
				found = true
				break
			}
		}
		require.Truef(t, found, "missing witness for L_%d leader %d", layer, expectedLeader)
	}
}

// TestObft_Witness_HarvestsLeaderSigmaWhenVKnownViaPeerOnion — a receiver
// who missed the L_0 Phase-1 broadcast directly but learned V through a
// peer's σ-onion entry CAN recover the leader's σ_V via the peer's witness
// section.
//
// Per spec §Phase 2 wire format: witnesses ship value_root + σ_V (plaintext
// at every layer). When the receiver has V locally — from any source,
// including a peer's σ-onion entry carrying V — they can cross-reference
// value_root, verify σ_V against the leader's pubshare on V, and harvest
// the leader's partial into their σ-pool. Reaches qV in cases where the
// leader's bundle never reached this operator directly.
//
// Setup: op4 misses the L_0 bundle; op1/op2/op3 receive it. In Phase 2,
// op2/op3 emit σ-onion entries at L_0 carrying V plus witness sections
// with op1's σ_V. op4 receives both: V from op2/op3's onion entries +
// op1's σ_V from witnesses. Local σ-pool: op2 + op3 + harvested op1 = qV=3.
func TestObft_Witness_HarvestsLeaderSigmaWhenVKnownViaPeerOnion(t *testing.T) {
	s := newSim(t, 4)

	// Partition L_0: only op1, op2, op3 receive the bundle. op4 misses.
	s.deliverPhase1(0, s.candidates[0], []OperatorID{1, 2, 3}, observedEarly, true)
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	// Pre-Phase-2 sanity: op4 has no L_0 bundle.
	require.Empty(t, s.instances[4].bundles[0][1], "op4 should not yet have L_0 bundle from op1")

	// Phase 2: every op builds and broadcasts their KindCommit. op2/op3 pack
	// witnesses for op1's L_0 bundle (value_root + σ_V).
	s.runPhase2(nil)

	// Bundle retention stays empty — witnesses don't rehydrate the full bundle.
	require.Empty(t, s.instances[4].bundles[0][1],
		"witnesses must not synthesize a Phase-1 bundle (no V transport)")

	// But the witnessed σ_V SHOULD have been harvested into
	// witnessedLeaderSigma — op2/op3 carried V in their σ-onion entries,
	// giving op4 a local copy of V to verify witnesses against.
	root := ValueRoot(s.candidates[0])
	bucket := s.instances[4].witnessedLeaderSigma[0]
	require.NotEmpty(t, bucket, "op4 should harvest leader σ_V from peer witnesses")
	_, ok := bucket[root]
	require.True(t, ok, "witnessedLeaderSigma must contain entry for L_0's V root")

	// And op4's local Resolve at L_0 now reaches qV: op2 + op3 (from
	// peerOnions) + op1 leader's σ_V (from witness) = 3 = qV.
	out, err := s.instances[4].Resolve()
	require.NoError(t, err, "op4 reaches σ-quorum at L_0 via witness harvest")
	require.NotNil(t, out)
	require.Equal(t, 0, out.Layer)
	require.True(t, bytes.Equal(out.Value, s.candidates[0]))
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
			{Layer: 0, Leader: 2 /* wrong: L_0 leader is op1 */, ValueRoot: ValueRoot([]byte("V")), SigmaV: []byte("sig")},
		},
	}
	err := ValidateCommit(c, s.cfg)
	require.ErrorContains(t, err, "leader")
}

// TestObft_Witness_DoesNotTriggerRule2 — observing two witnesses with
// distinct value_roots at same (layer, leader) does NOT trigger Rule 2
// evidence at the receiver.
//
// Per spec §Phase 2 wire format, witnesses ship value_root + σ_V (no full
// V). Rule 2 evidence requires the BUNDLE pair (V_a/σ_V_a, V_b/σ_V_b) —
// witnesses don't carry V, so they cannot independently introduce V's into
// retention. Cluster-wide Rule 2 attribution requires either (a) at least
// one honest receiver to have observed both V's via Phase-1 bundles
// directly (gossipsub re-flood scenario), or (b) a future Appendix-style
// ship-full-V witness variant. Per spec §Slashing evidence, the
// honest-operator MUST-log requirement covers the per-operator detection
// surface; out-of-band log aggregation across operators handles the
// asymmetric-retention case at the cluster level.
//
// The trade-off is documented: dropping full V from witnesses saves ~10×
// bandwidth at the cost of losing cross-receiver Rule 2 attribution in
// natural-recovery scenarios where each receiver only sees one V.
func TestObft_Witness_DoesNotTriggerRule2(t *testing.T) {
	s := newSim(t, 4)
	signer := NewStubSigner(s.cfg.QV(), []byte{1})
	vA := []byte("V-a")
	vB := []byte("V-b")
	sigA, err := signer.SignPartial(vA)
	require.NoError(t, err)
	sigB, err := signer.SignPartial(vB)
	require.NoError(t, err)

	// Two commits from different ops, each carrying a witness for op1 at
	// L_0 with a distinct value_root.
	c2 := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		Witnesses:  []LeaderSigmaWitness{{Layer: 0, Leader: 1, ValueRoot: ValueRoot(vA), SigmaV: sigA}},
	}
	c3 := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 3,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		Witnesses:  []LeaderSigmaWitness{{Layer: 0, Leader: 1, ValueRoot: ValueRoot(vB), SigmaV: sigB}},
	}

	receiver := s.instances[4]
	require.NoError(t, receiver.ObserveCommit(c2))
	require.NoError(t, receiver.ObserveCommit(c3))

	ev := receiver.Evidence()
	for _, e := range ev {
		require.NotEqualf(t, EvidenceLeaderEquivocation, e.Rule,
			"witnesses must not trigger Rule 2 (receiver lacks V to make slashable evidence); got %+v", ev)
	}
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
// obft.NoQuorumTag(ClusterID, Height, Layer); a partial signed for cluster A
// fails verification under cluster B's tag construction. This is what
// prevents cross-cluster replay of NR partials independently of the
// ClusterID structural check on the carrier Commit.
func TestObft_ClusterID_NRTagBinding(t *testing.T) {
	clusterA := [32]byte{0x11}
	clusterB := [32]byte{0x22}
	const height = 100
	const layer = 0

	tagA := obft.NoQuorumTag(clusterA, height, layer)
	tagB := obft.NoQuorumTag(clusterB, height, layer)
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
	bInst, err := NewInstance(clusterB.cfg, 2, NewStubSigner(clusterB.cfg.QV(), []byte{2}), nil, NewStubIBE(clusterB.cfg.QV()), nil, clusterB.pubKeyShares, nil, nil)
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

// ---- Reviewer-identified fixes ---

// TestObft_NRPartial_RejectedUnderOptionA — under Option A (ibePubKeyShares
// nil), a corrupt NR partial must be rejected by ObserveCommit instead of
// silently entering peerNR (which would corrupt Lagrange aggregation at
// Phase 3 and kill fall-through cluster-wide). Defense-in-depth even though
// the validation layer also rejects.
func TestObft_NRPartial_RejectedUnderOptionA(t *testing.T) {
	s := newSim(t, 4)
	// Op2 silent at L_0 (no V retained) → would NR at L_0. Forge a Commit
	// from op2 with a garbage NR partial.
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{{Layer: 0, PartialSig: []byte("garbage-NR-padded-to-look-like-bls-sig-bytes-here-here-here-here")}},
	}
	err := s.instances[3].ObserveCommit(c)
	require.ErrorContains(t, err, "NR partial")
	// Confirm peerNR did not get poisoned.
	require.Empty(t, s.instances[3].peerNR[0])
}

// TestObft_UnknownV_Rule5_FiresWhenVNotRetained — per spec §Slashing
// evidence Rule 5, "a plaintext σ partial that does not verify against any
// retained candidate V at that layer (where the receiver has retained at
// least one such V) … is a slashable byzantine fault". The unknownV
// variant — σ verifies against op's own share on the claimed V, but
// claimed V is not in the receiver's retained set — fires Rule 5
// immediately once the receiver has at least one V to compare against.
//
// Per spec MUST-log framing, per-receiver-view evidence is logged
// regardless of whether other receivers might have retained the V via a
// different path (e.g. leader equivocation); out-of-band aggregation
// reconciles. See TestObft_Rule5_NoFireWhenBothEquivocatedVsRetained for
// the negative case where the receiver retained BOTH equivocated V's.
func TestObft_UnknownV_Rule5_FiresWhenVNotRetained(t *testing.T) {
	s := newSim(t, 4)

	// Op2 signs a V the cluster never broadcast. The signature verifies
	// against op2's own share but doesn't match any V the leader signed.
	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	fakeV := []byte("never-broadcast-V")
	sigOnFakeV, err := signer.SignPartial(fakeV)
	require.NoError(t, err)
	layers := make([]EncryptedLayer, s.K)
	layers[0] = EncryptedLayer{Value: fakeV, Ciphertext: sigOnFakeV}
	byzCommit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     layers,
	}

	receiver := s.instances[3]

	// Pre-retention: receiver has no V at L_0. peerSigmaAtL0Verdict
	// returns inconclusive — Rule 5 cannot fire yet.
	require.NoError(t, receiver.ObserveCommit(byzCommit))
	for _, e := range receiver.Evidence() {
		require.NotEqualf(t, EvidenceFakePlaintextSigma, e.Rule,
			"Rule 5 must NOT fire before any V is retained")
	}

	// Leader's bundle on V_a arrives. reevaluateL0Sigmas re-checks the
	// peerOnion entry: verdict is now unknownV (fakeV doesn't match V_a).
	// Rule 5 fires retroactively.
	s.deliverPhase1(0, s.candidates[0], []OperatorID{1, 2, 3, 4}, observedEarly, true)
	found := false
	for _, e := range receiver.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 2 && e.Layer == 0 {
			require.NotNil(t, e.FakePlaintextSigma)
			require.True(t, bytes.Equal(e.FakePlaintextSigma.OnionValue, fakeV),
				"Rule 5 evidence must carry the offending OnionValue")
			found = true
			break
		}
	}
	require.True(t, found, "expected Rule 5 evidence against op2 at L_0 after V_a retention; got %+v", receiver.Evidence())
}

// TestObft_Rule5_NoFireWhenBothEquivocatedVsRetained — when an L_0 leader
// equivocates (V_a then V_b), and the receiver retains BOTH V's (per the
// 2-distinct retention rule), an honest peer's σ on V_b verifies against
// the retained V_b. Verdict = l0SigmaVerified, no Rule 5 fires. The
// leader still gets Rule 2 (LeaderEquivocation) evidence — the slot's
// byzantine attribution targets the leader, not the honest equivocation-
// reactor.
func TestObft_Rule5_NoFireWhenBothEquivocatedVsRetained(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.cfg.Layers[0].Leader
	vA := s.candidates[0]
	vB := []byte("equivocated-V_b")

	receiver := s.instances[3]

	// Step 1: receiver retains BOTH leader bundles (Rule 2 fires at
	// retention of the second). deliverPhase1Equivocation routes V_a and
	// V_b to disjoint sets but both reach op3 via the recipientsA + B
	// arguments.
	s.deliverPhase1Equivocation(0, vA, vB,
		[]OperatorID{1, 2, 3, 4}, []OperatorID{3},
		observedEarly, true)
	require.GreaterOrEqualf(t, len(receiver.bundles[0][leaderID]), 2,
		"setup: receiver must retain both equivocated V's")

	// Step 2: honest op2 σ-signs V_b (received it from the leader's
	// equivocation broadcast). Op2's commit arrives at receiver. Verdict
	// = l0SigmaVerified (matches retained V_b). Rule 5 must NOT fire.
	op2Signer := NewStubSigner(s.cfg.QV(), []byte{2})
	op2SigOnVb, err := op2Signer.SignPartial(vB)
	require.NoError(t, err)
	layers := make([]EncryptedLayer, s.K)
	layers[0] = EncryptedLayer{Value: vB, Ciphertext: op2SigOnVb}
	op2Commit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     layers,
	}
	require.NoError(t, receiver.ObserveCommit(op2Commit))

	for _, e := range receiver.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 2 {
			t.Fatalf("Rule 5 must NOT fire against op2 when V_b is retained: %+v", e)
		}
	}
	// Leader still attributed via Rule 2.
	rule2Found := false
	for _, e := range receiver.Evidence() {
		if e.Rule == EvidenceLeaderEquivocation && e.OperatorID == leaderID {
			rule2Found = true
			break
		}
	}
	require.True(t, rule2Found, "Rule 2 (leader equivocation) must still fire against the leader")
}

// TestObft_Finalize_Idempotent — Finalize is safe to call repeatedly.
// First call flips Ended() from false to true; subsequent calls are no-ops
// (no panic, no Evidence() mutation).
func TestObft_Finalize_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	inst := s.instances[2]

	require.False(t, inst.Ended(), "fresh Instance should not be ended")
	preEv := append([]Evidence{}, inst.Evidence()...)

	inst.Finalize()
	require.True(t, inst.Ended(), "Finalize must set Ended()")
	postFirstEv := inst.Evidence()
	require.Len(t, postFirstEv, len(preEv), "Finalize must not mutate Evidence accumulator")

	// Second call: still ended, still no Evidence mutation, no panic.
	inst.Finalize()
	require.True(t, inst.Ended(), "Ended() must remain true after second Finalize")
	require.Len(t, inst.Evidence(), len(preEv), "second Finalize must not mutate Evidence")
}

// TestObft_Rule5_FiresOnMinorityEquivocationView — when a leader equivocates
// V_a/V_b and a receiver retains only V_a (V_b's bundle never arrived to
// this receiver), an honest peer who σ-signed V_b is logged Rule 5 from
// the receiver's local view. Per spec MUST-log framing this is expected:
// out-of-band aggregation reconciles minority views against the majority
// (receivers who retained V_b log no Rule 5 against the peer; leader still
// attributed via Rule 2 from any receiver who retained both V's).
func TestObft_Rule5_FiresOnMinorityEquivocationView(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.cfg.Layers[0].Leader
	V_a := s.candidates[0]
	V_b := []byte("equivocating-leader-V_b")

	op2Signer := NewStubSigner(s.cfg.QV(), []byte{byte(2)})
	op2SigOnVb, err := op2Signer.SignPartial(V_b)
	require.NoError(t, err)
	layers := make([]EncryptedLayer, s.K)
	layers[0] = EncryptedLayer{Value: V_b, Ciphertext: op2SigOnVb}
	op2Commit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     layers,
	}

	receiver := s.instances[3]

	// Receiver retains leader's V_a (the V_b bundle never reaches this
	// receiver; the equivocation is invisible from their local view).
	s.deliverPhase1(0, V_a, []OperatorID{1, 2, 3, 4}, observedEarly, true)
	require.NotZero(t, len(receiver.bundles[0][leaderID]),
		"setup: receiver must have V_a retained")

	// Op2's commit on V_b arrives. peerSigmaAtL0Verdict = l0SigmaUnknownV
	// (verifies on V_b, but V_b is not retained). Per spec literal reading
	// of Rule 5 ("does not verify against any retained candidate V"),
	// receiver logs Rule 5 against op2.
	require.NoError(t, receiver.ObserveCommit(op2Commit))
	rule5Found := false
	for _, e := range receiver.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 2 {
			rule5Found = true
			break
		}
	}
	require.True(t, rule5Found, "Rule 5 must fire against op2 from minority-view receiver")
}

// TestObft_Rule5_CryptoFakeSilentLeader — a byzantine's L_0 σ partial that
// fails BLS verify against their own share is cryptoFake regardless of whether
// the L_0 leader has been silent. Without firing here, the byzantine could
// emit unverifiable bytes early in the slot (before any L_0 retention) and
// never get attributed: reevaluateL0Sigmas only fires on retentions, and
// the unknownV path doesn't fire Rule 5 at all (see Instance.Finalize).
func TestObft_Rule5_CryptoFakeSilentLeader(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[3]

	// Byzantine op2 emits an L_0 σ entry whose Ciphertext is garbage that
	// won't verify against op2's pubshare on the claimed Value. No L_0
	// bundle has been retained at this point.
	garbageSig := []byte("garbage-not-a-valid-bls-sig-padded-to-bls-length-bytes-bytes-bytes")
	require.NotEqual(t, l0SigmaInconclusive, // sanity: would have been the buggy verdict
		receiver.peerSigmaAtL0Verdict(2, EncryptedLayer{Value: []byte("V_x"), Ciphertext: garbageSig}),
		"verdict must not return inconclusive when sig fails verify, even without retentions")

	layers := make([]EncryptedLayer, s.K)
	layers[0] = EncryptedLayer{Value: []byte("V_x"), Ciphertext: garbageSig}
	byzCommit := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers:     layers,
	}
	require.NoError(t, receiver.ObserveCommit(byzCommit))

	// Rule 5 must fire at observe time even though no L_0 bundle has been
	// retained — the cryptoFake check is signature-self-contained.
	found := false
	for _, e := range receiver.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 2 {
			found = true
			break
		}
	}
	require.True(t, found, "Rule 5 must fire on cryptoFake even when leader is silent")
}

// TestObft_PeerCommitHashes_CappedPerOp — under abuse (a byzantine emitting
// many distinct Commits), peerCommitHashes accumulation is bounded by
// MaxCommitHashesPerOp. Beyond the cap, further distinct emissions are
// dropped silently — the operator is already flagged byzantine many times
// over, accepting more variants is just memory pressure.
func TestObft_PeerCommitHashes_CappedPerOp(t *testing.T) {
	s := newSim(t, 4)
	signer := NewStubSigner(s.cfg.QV(), []byte{2})
	receiver := s.instances[3]

	// Emit MaxCommitHashesPerOp+5 structurally-distinct Commits from op2.
	// Each carries one distinct NR partial layer to vary content.
	for k := 0; k < MaxCommitHashesPerOp+5; k++ {
		layer := k % (s.K - 1) // layers in [0, K-1) for NR
		tag := obft.NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, layer)
		nrSig, err := signer.SignPartial(tag)
		require.NoError(t, err)
		// Vary the partial sig to make each commit structurally distinct.
		varied := append([]byte{byte(k)}, nrSig...)
		c := &Commit{
			ClusterID:  s.cfg.ClusterID,
			OperatorID: 2,
			Height:     s.cfg.Height,
			Layers:     make([]EncryptedLayer, s.K),
			NRPartials: []NRPartial{{Layer: layer, PartialSig: varied}},
		}
		// Errors past the cap are not surfaced (silent drop), but earlier
		// distinct hashes will fire NR-partial verification failure (the
		// varied sig won't verify). Skip error checks; we're testing the
		// hash-cap not the partial verification.
		_ = receiver.ObserveCommit(c)
	}

	// peerCommitHashes for op2 must be capped exactly at MaxCommitHashesPerOp.
	// Loop ran MaxCommitHashesPerOp+5 distinct emissions, so we expect to
	// have hit the cap (not just be under it — that would be a sign the cap
	// is too aggressive).
	hashes := receiver.peerCommitHashes[2]
	require.Equal(t, MaxCommitHashesPerOp, len(hashes),
		"peerCommitHashes must hit the cap exactly under sustained abuse")
}

// TestObft_NoQuorumTag_RejectsNegativeLayer — out-of-range layer must
// panic rather than silently corrupt the tag (replay vector). Verified by
// recovering the panic.
func TestObft_NoQuorumTag_RejectsNegativeLayer(t *testing.T) {
	defer func() {
		r := recover()
		require.NotNil(t, r, "expected panic on negative layer")
		require.Contains(t, r.(string), "out of range")
	}()
	obft.NoQuorumTag([32]byte{1}, 1, -1)
}

// TestObft_NewInstance_RequiresLeaderPubShares — a misconfigured instance
// (one of the layer leaders has no registered pub-key share) must be
// rejected at construction rather than silently degrading at Phase 3.
func TestObft_NewInstance_RequiresLeaderPubShares(t *testing.T) {
	cfg := validBaseConfig()
	pubShares := map[OperatorID][]byte{
		1: {1}, 2: {2}, 3: {3},
		// op4 (L_3 leader) intentionally missing.
	}
	signer := NewStubSigner(cfg.QV(), []byte{1})
	ibe := NewStubIBE(cfg.QV())
	_, err := NewInstance(cfg, 1, signer, signer, ibe, []byte{0xCC}, pubShares, nil, nil)
	require.ErrorContains(t, err, "no pub-key share")
}

// TestObft_PeerSigmaAtL0_MissingPubkeyNotSlashable — a peer with no
// registered pub-share must NOT be flagged as Rule 5; it's a config issue,
// not a slashable fault.
func TestObft_PeerSigmaAtL0_MissingPubkeyNotSlashable(t *testing.T) {
	s := newSim(t, 4)
	// Deliver L_0 to populate retained V at L_0.
	s.deliverPhase1(0, s.candidates[0], []OperatorID{1, 2, 3, 4}, observedEarly, true)

	// Construct a Commit from a non-cluster operator (op99) — its share isn't
	// registered. A receiver running the L_0 σ-check on it must NOT fire Rule 5.
	signer := NewStubSigner(s.cfg.QV(), []byte{99})
	sig, err := signer.SignPartial(s.candidates[0])
	require.NoError(t, err)
	layers := make([]EncryptedLayer, s.K)
	layers[0] = EncryptedLayer{Value: s.candidates[0], Ciphertext: sig}
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 99,
		Height:     s.cfg.Height,
		Layers:     layers,
	}
	// Bypass ValidateCommit (which would reject op99 as non-cluster) by
	// calling the Verdict function directly.
	verdict := s.instances[3].peerSigmaAtL0Verdict(99, layers[0])
	require.Equal(t, l0SigmaInconclusive, verdict, "missing pub-share → inconclusive, not fake")
	_ = c
}

// TestObft_EvidenceObserver_FiresOncePerTuple verifies the spec's MUST-log
// surface: an EvidenceObserver fires once per (Rule, OperatorID, Layer)
// tuple on first observation, regardless of how many redundant detections
// trigger. This is the per-operator logging cap; cluster-wide attribution
// happens out-of-band via log aggregation.
func TestObft_EvidenceObserver_FiresOncePerTuple(t *testing.T) {
	s := newSim(t, 4)

	// Build a sibling instance for op 2 with an observer wired in at
	// construction. (newSim's instances are observer-less; constructing a
	// fresh one is the cleanest way to exercise the observer path now that
	// SetEvidenceObserver is gone — the observer is immutable post-NewInstance.)
	var observed []Evidence
	signer := NewStubSigner(s.cfg.QV(), []byte{byte(2)})
	ibe := NewStubIBE(s.cfg.QV())
	inst, err := NewInstance(
		s.cfg, OperatorID(2),
		signer, signer, ibe,
		[]byte{0xCC, 0xDD}, s.pubKeyShares, nil,
		func(e Evidence) { observed = append(observed, e) },
	)
	require.NoError(t, err)

	// Inject the same evidence twice; the observer should fire only once.
	inst.recordEvidence(Evidence{Rule: EvidenceFakePlaintextSigma, OperatorID: 3, Layer: 0})
	inst.recordEvidence(Evidence{Rule: EvidenceFakePlaintextSigma, OperatorID: 3, Layer: 0})
	require.Len(t, observed, 1, "observer must fire ONCE per (rule, op, layer); got %d fires", len(observed))

	// Different layer for same op+rule → distinct tuple → observer fires again.
	inst.recordEvidence(Evidence{Rule: EvidenceFakePlaintextSigma, OperatorID: 3, Layer: 1})
	require.Len(t, observed, 2, "different layer = distinct tuple; observer should fire again")

	// Different op same rule+layer → distinct tuple → observer fires again.
	inst.recordEvidence(Evidence{Rule: EvidenceFakePlaintextSigma, OperatorID: 4, Layer: 0})
	require.Len(t, observed, 3, "different op = distinct tuple; observer should fire again")

	// Different rule same op+layer → distinct tuple.
	inst.recordEvidence(Evidence{Rule: EvidenceCrossSigning, OperatorID: 3, Layer: 0})
	require.Len(t, observed, 4, "different rule = distinct tuple; observer should fire again")

	// Recording does not affect Evidence() accumulator behavior — it still
	// records every entry (observer dedup is independent of evidence storage).
	require.Len(t, inst.Evidence(), 5, "Evidence() should retain all 5 records")
}

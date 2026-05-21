package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Worked-cases catalog per docs/2abOBFT-REDESIGN-PLAN.md §Liveness worked
// cases. Each scenario corresponds to a row in the worked-cases table —
// see the plan for the full state-transition narrative.

// Healthy h_V=4: all 4 ops retain V_0, all host-valid. L_0 σ-quorum.
func TestScenario_HealthyL0Success(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// Healthy h_V=3: 3 ops retain V_0, 1 doesn't. σ-eligibility reaches qV=3
// from 3 ops; L_0 σ-quorum on V_0.
func TestScenario_HV3SucceedsAtL0(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// h_V_honest=1 (only leader has V_0 at Phase 1) — V-drops upgrade via
// Phase-1 reflood arriving BEFORE noValuePool reaches qEnc (modeled here
// by orchestrating reflood + host-validity early in the cross-broadcast
// sequence; in real gossipsub, the leader's KindValue propagates
// alongside other ops' KindNoValues, and timing determines whether each
// V-drop processes the leader's bundle before reaching the NR-eligibility
// quorum).
//
// This is the headline case from §Liveness worked cases line 781. The
// test demonstrates the spec-guaranteed-recovery path: 2 honest V-drops
// upgrade, σ-eligibility fires with valuePool ≥ qV, all upgraded ops
// emit Signed → L_0 σ-quorum.
func TestScenario_HV1RecoversViaUpgrade(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 has V_0; ops 2, 3, 4 don't (yet).
	s.deliverPhase1(0, Value("V0"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), true)
	// Op 1 fires Phase 2a → emits KindValue.
	vmLeader, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vmLeader)
	// Ops 2, 3, 4 fire Phase 2a → emit KindNoValue. But before their
	// NoValues propagate to peers, simulate the Phase-1 reflood reaching
	// ops 2, 3 (op 4 stays drop).
	for _, op := range []OperatorID{2, 3, 4} {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Pre-NoValue-propagation reflood: deliver V_0 + host validity to
	// ops 2, 3 (modeling the leader's KindValue or Phase-1 reflood
	// arriving before peer NoValues).
	leader := s.leaderAt(0)
	bundle, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	for _, op := range []OperatorID{2, 3} {
		require.NoError(t, s.instances[op].ObservePhase1Bundle(bundle, observedAfterPhase2a))
		require.NoError(t, s.instances[op].ApplyHostValidity(0, Value("V0"), true))
	}
	// The afterStateDelta cascade inside ApplyHostValidity should have
	// fired the A1 upgrades for ops 2, 3 (preconditions met: ownNoValueMsg
	// set, V_0 retained, host valid).
	for _, op := range []OperatorID{2, 3} {
		_, hadUpgrade := s.instances[op].OwnValueMsg()
		require.True(t, hadUpgrade, "op %d should have emitted upgrade KindValue", op)
	}
	// Cross-broadcast everything: leader's KindValue, V-drops' NoValues,
	// upgrade KindValues from ops 2, 3, and any Phase-2b Commits.
	s.propagatePostPhase2aEmissions()
	// Also cross-broadcast the original Phase-2a emissions (leader's
	// KindValue + NoValues from ops 2, 3, 4) which haven't propagated yet.
	for _, op := range s.allOperators() {
		vm, _ := s.instances[op].OwnValueMsg()
		nv, _ := s.instances[op].OwnNoValueMsg()
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			if vm != nil {
				require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
			}
			if nv != nil {
				require.NoError(t, s.instances[peer].ObserveNoValueMsg(nv))
			}
		}
	}
	// Propagate any newly-emitted Phase-2b Commits.
	s.propagatePostPhase2aEmissions()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "L_0 σ-quorum via upgrades")
	require.Equal(t, Value("V0"), out.Value)
}

// h_V_honest=0: nobody has V_0 → all emit KindNoValue → NR-eligibility
// fires → nr_tag_0-pool reaches qEnc → fall-through to L_1.
func TestScenario_HV0FallsThroughToL1(t *testing.T) {
	s := newSim(t, 4)
	// No L_0 delivery. L_1 delivers.
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

// 1-1-1 byz leader equivocation: leader emits 2 distinct V_0 to splitting
// subsets. Receivers observe equivocation → equivocation trigger →
// KindCommit-NR → nr_tag_0-pool reaches qEnc → fall-through to L_1.
func TestScenario_LeaderEquivocationFallsThrough(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		s.allOperators(), s.allOperators(), observedEarly)
	// L_1 has a candidate.
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

// TestScenario_Healthy_N7_F2: algebra generalizes to n=7 / f=2 (qV=qEnc=5).
// All 7 ops have V_0 + host valid → all emit KindValue → cluster σ-quorum
// reaches at L_0 with 7 σ-side partials (well above qV=5).
func TestScenario_Healthy_N7_F2(t *testing.T) {
	s := newSimWithFK(t, 7, 2, 3) // K=3 (f+1) BFT-liveness minimum at f=2
	require.Equal(t, 5, s.cfg.QV())
	require.Equal(t, 5, s.cfg.QEnc())
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// TestScenario_DualPoolMembershipForA3HostFlipPivot: per spec §Pool
// aggregation rules / Dual-pool membership, an op that emits KindValue
// then KindCommit-NR (A3 host-flip pivot) appears in both
// valuePool[V_0] AND noValuePool[L_0] simultaneously. This is the
// intended pool semantics: claim-pools (value_pool, novalue_pool)
// track CLAIMS (the op's wire emissions), while threshold pools
// (sigma_pool, nr_tag_0-pool) track ACTUAL partials. Pigeonhole 1
// applies to the threshold pools, not the claim pools.
func TestScenario_DualPoolMembershipForA3HostFlipPivot(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	vmA, _ := s.instances[OperatorID(1)].OwnValueMsg()
	vmB, _ := s.instances[OperatorID(2)].OwnValueMsg()
	vmC, _ := s.instances[OperatorID(3)].OwnValueMsg()
	require.NotNil(t, vmA)
	require.NotNil(t, vmB)
	require.NotNil(t, vmC)
	// Op 4's host flips to NV → A3 pivot at commit time.
	require.NoError(t, s.instances[OperatorID(4)].ApplyHostValidity(0, Value("V0"), false))
	// Deliver peer KindValues to drive σ-eligibility at op 4 → A3 pivot.
	for _, vm := range []*ValueMsg{vmA, vmB, vmC} {
		require.NoError(t, s.instances[OperatorID(4)].ObserveValueMsg(vm))
	}
	c4, ok := s.instances[OperatorID(4)].OwnCommit()
	require.True(t, ok)
	require.Equal(t, CommitSideNR, c4.Side)
	// Now check pool membership at op 1 (observing op 4's A3 sequence).
	op1 := s.instances[OperatorID(1)]
	vmD, _ := s.instances[OperatorID(4)].OwnValueMsg()
	require.NotNil(t, vmD, "op 4 emitted KindValue at Phase 2a before host flip")
	require.NoError(t, op1.ObserveValueMsg(vmD))
	require.NoError(t, op1.ObserveCommit(c4))
	// Op 4 should be in BOTH valuePool[V_0] AND noValuePool[L_0] at op 1.
	v0Root := ValueRoot(Value("V0"))
	require.True(t, op1.valuePool[0][v0Root][OperatorID(4)],
		"op 4 should be in valuePool[V_0] from its KindValue")
	require.True(t, op1.noValuePool[0][OperatorID(4)],
		"op 4 should ALSO be in noValuePool[L_0] from its Commit-NR (A3 dual-pool)")
}

// TestObservePhase1Bundle / Phase-2a / Commit re-broadcast dedup: gossipsub
// may deliver the same message multiple times via mesh fanout. The
// protocol layer must silently dedup identical re-broadcasts (no double-
// counting in pools, no spurious evidence). Already covered for Phase-1
// bundles by TestObservePhase1Bundle_IdenticalRebroadcastIsSilentDedup;
// here we cover Value / NoValue / Commit explicitly.
func TestObserveValueMsg_IdenticalRebroadcastIsSilentDedup(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		V:          Value("V0"),
		ValueRoot:  ValueRoot(Value("V0")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryEmpty},
		},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	beforeEvidence := len(op2.Evidence())
	require.NoError(t, op2.ObserveValueMsg(vm))
	require.NoError(t, op2.ObserveValueMsg(vm))
	require.Equal(t, beforeEvidence, len(op2.Evidence()),
		"identical re-broadcast should not produce new evidence")
	// Pool size unchanged.
	require.Equal(t, 1, op2.valuePoolSize(0, ValueRoot(Value("V0"))))
}

func TestObserveNoValueMsg_IdenticalRebroadcastIsSilentDedup(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	nv := &NoValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   1,
		Height:       s.cfg.Height,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	beforeEvidence := len(op2.Evidence())
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	require.Equal(t, beforeEvidence, len(op2.Evidence()))
	require.Equal(t, 1, op2.noValuePoolSize(0))
}

func TestObserveCommit_IdenticalRebroadcastIsSilentDedup(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideSigned,
		L0Value:    Value("V0"),
		L0Partial:  Signature{0x01},
	}
	require.NoError(t, op2.ObserveCommit(c))
	beforeEvidence := len(op2.Evidence())
	require.NoError(t, op2.ObserveCommit(c))
	require.NoError(t, op2.ObserveCommit(c))
	require.Equal(t, beforeEvidence, len(op2.Evidence()))
	require.Equal(t, 1, op2.valuePoolSize(0, ValueRoot(Value("V0"))))
}

// TestObserveCommit_KindCommitNRPopulatesNoValuePool: per inference rules,
// KindCommit-NR observation adds the op to noValuePool[L_0] AND to
// nrTagPool[L_0] (the partial is extracted). This is the NR-side
// counterpart to TestObserveCommit_KindCommitSignedInfersKindValue.
func TestObserveCommit_KindCommitNRPopulatesNoValuePool(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  Signature{0x01, 0x02},
	}
	require.NoError(t, op2.ObserveCommit(c))
	require.True(t, op2.noValuePool[0][OperatorID(1)],
		"KindCommit-NR observation should add op to noValuePool[L_0]")
	require.NotNil(t, op2.nrTagPool[0][OperatorID(1)],
		"KindCommit-NR observation should add op to nrTagPool[L_0]")
}

// TestEKM_TransitionToSigma_RejectsCrossV: at the same layer, a second
// transitionToSigma call with a different V must fail with ErrSigmaLocked
// per single-σ-V EKM invariant. Defense-in-depth — the public API path
// gates against this at higher levels (ownCommit-already-set early
// return), but EKM is the cryptographic backstop.
func TestEKM_TransitionToSigma_RejectsCrossV(t *testing.T) {
	s := newSim(t, 4)
	inst := s.instances[OperatorID(1)]
	require.NoError(t, inst.transitionToSigma(0, Value("V_a")))
	// Same V → idempotent (no error).
	require.NoError(t, inst.transitionToSigma(0, Value("V_a")))
	// Different V at same layer → rejected.
	err := inst.transitionToSigma(0, Value("V_b"))
	require.ErrorIs(t, err, ErrSigmaLocked)
}

// TestEKM_TransitionToSigma_RejectsAfterNRLock: at the same layer,
// transitionToSigma after transitionToNR must fail with ErrNRLocked
// per σ-XOR-NR EKM invariant.
func TestEKM_TransitionToSigma_RejectsAfterNRLock(t *testing.T) {
	s := newSim(t, 4)
	inst := s.instances[OperatorID(1)]
	require.NoError(t, inst.transitionToNR(0))
	err := inst.transitionToSigma(0, Value("V_a"))
	require.ErrorIs(t, err, ErrNRLocked)
}

// TestEKM_TransitionToNR_RejectsAfterSigmaLock: at the same layer,
// transitionToNR after transitionToSigma must fail with ErrSigmaLocked
// per σ-XOR-NR EKM invariant (the other half of cross-phase exclusivity).
func TestEKM_TransitionToNR_RejectsAfterSigmaLock(t *testing.T) {
	s := newSim(t, 4)
	inst := s.instances[OperatorID(1)]
	require.NoError(t, inst.transitionToSigma(0, Value("V_a")))
	err := inst.transitionToNR(0)
	require.ErrorIs(t, err, ErrSigmaLocked)
}

// TestEKM_LockingIsPerLayer: EKM locks at one layer don't affect other
// layers. An op σ-locked at L_0 can still NR-lock at L_1 (or σ-lock on
// a different V at L_1).
func TestEKM_LockingIsPerLayer(t *testing.T) {
	s := newSim(t, 4)
	inst := s.instances[OperatorID(1)]
	require.NoError(t, inst.transitionToSigma(0, Value("V_a")))
	// L_1 is independent.
	require.NoError(t, inst.transitionToNR(1))
	require.True(t, inst.sigmaLocked[0])
	require.True(t, inst.nrLocked[1])
}

// Non-uniform mesh-tail at L_0: 3 honest have V_0 + host valid, 1 honest
// is a V-drop. A specific peer's KindValue is delayed in delivery to one
// "slow-view" op (modeling a mesh-tail latency outlier). Under v1's
// T_commit hard wall this would force the slow-view op to NR-default at
// the wall; v4 waits (no protocol-level Phase-2b deadline) and recovers
// once the delayed KindValue arrives — σ-eligibility trigger fires, the
// slow-view op emits KindCommit-Signed, L_0 σ-quorum reaches.
//
// This is the headline case for v4's "closes the non-uniform mesh-tail
// boundary" claim from the redesign plan §Liveness worked cases
// (line 783): the protocol absorbs slow-edge propagation up to the slot
// deadline without forcing premature NR-default. The test orchestrates
// delayed delivery manually rather than porting gossipsub mesh machinery
// into the unit-test sim — the underlying protocol behavior (wait for
// pool to reach qV; fire commit when it does) is independent of how
// messages get delayed/refloded at the transport layer.
func TestScenario_NonUniformMeshTailRecovery(t *testing.T) {
	s := newSim(t, 4)
	// Ops 1, 2, 3 have V_0 + host valid. Op 4 is a V-drop (no V_0).
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)

	// All 4 ops fire Phase 2a. Ops 1/2/3 → KindValue; op 4 → KindNoValue.
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	vmA, _ := s.instances[OperatorID(1)].OwnValueMsg()
	vmB, _ := s.instances[OperatorID(2)].OwnValueMsg()
	vmC, _ := s.instances[OperatorID(3)].OwnValueMsg()
	nvD, _ := s.instances[OperatorID(4)].OwnNoValueMsg()
	require.NotNil(t, vmA)
	require.NotNil(t, vmB)
	require.NotNil(t, vmC)
	require.NotNil(t, nvD)

	// Initial propagation: op 1 (the slow-view op) misses op 3's KindValue.
	// All others see everyone's full Phase-2a set.
	for _, op := range s.allOperators() {
		for _, msg := range []struct {
			from OperatorID
			vm   *ValueMsg
			nv   *NoValueMsg
		}{
			{1, vmA, nil}, {2, vmB, nil}, {3, vmC, nil}, {4, nil, nvD},
		} {
			if msg.from == op {
				continue
			}
			// Delay op 3's KindValue to op 1 specifically (mesh-tail).
			if op == OperatorID(1) && msg.from == OperatorID(3) {
				continue
			}
			if msg.vm != nil {
				require.NoError(t, s.instances[op].ObserveValueMsg(msg.vm))
			}
			if msg.nv != nil {
				require.NoError(t, s.instances[op].ObserveNoValueMsg(msg.nv))
			}
		}
	}

	// At this point op 1's value_pool[V_0] = {op1, op2} = 2 < qV=3.
	// noValuePool = {op4} = 1 < qEnc=3. The cannot-σ gate on NR-eligibility
	// blocks op 1 from defaulting to NR (op 1 has V_local + host valid).
	// Op 1 has NOT yet emitted a commit — it's waiting for the slow message.
	_, op1HasCommit := s.instances[OperatorID(1)].OwnCommit()
	require.False(t, op1HasCommit, "slow-view op 1 should be waiting, not yet committed")

	// Meanwhile, ops 2 and 3 saw the full set (op 1's + 2's + 3's = 3
	// KindValues ≥ qV); σ-eligibility fired for them. They've emitted
	// KindCommit-Signed via the cascade.
	for _, op := range []OperatorID{2, 3} {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok, "op %d (full-view) should have emitted Commit-Signed", op)
		require.Equal(t, CommitSideSigned, c.Side)
	}

	// Now simulate the mesh-tail recovery: op 3's KindValue finally
	// arrives at op 1 (e.g., via gossipsub IHAVE/IWANT after the lazy-
	// push HeartbeatInterval). The afterStateDelta cascade fires
	// σ-eligibility and op 1 emits KindCommit-Signed.
	require.NoError(t, s.instances[OperatorID(1)].ObserveValueMsg(vmC))
	c, ok := s.instances[OperatorID(1)].OwnCommit()
	require.True(t, ok, "op 1 should have committed once the slow KindValue arrived")
	require.Equal(t, CommitSideSigned, c.Side, "op 1 should be σ-side (has V_local + host valid)")

	// Cross-broadcast all Commits so the cluster can reach σ-quorum.
	for _, op := range []OperatorID{1, 2, 3} {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveCommit(c))
		}
	}
	// Op 4 (V-drop) cascade-fires NR-eligibility once it sees the cluster's
	// noValuePool fail to inflate while valuePool reaches qV elsewhere.
	// Actually at op 4: value_pool grows to qV=3 once it observes the
	// three KindValues → σ-eligibility fires → side decision: op 4 has
	// no V_local → emits KindCommit-NR. Already happened during cross-
	// broadcast. Just broadcast it.
	if cNR, ok := s.instances[OperatorID(4)].OwnCommit(); ok {
		for _, peer := range []OperatorID{1, 2, 3} {
			require.NoError(t, s.instances[peer].ObserveCommit(cNR))
		}
	}

	// Resolve. L_0 σ-pool[V_0] should have {1, 2, 3} = 3 = qV.
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "L_0 σ-quorum via mesh-tail recovery")
	require.Equal(t, Value("V0"), out.Value)
}

// h_V_honest=2: 2 honest have V_0 at Phase 1, 2 honest are V-drops. Per
// the redesign plan §Liveness worked cases (line 780): both V-drops
// receive V_0 via Phase-1 reflood + host valid → upgrade. After upgrades,
// value_pool reaches qV=3 (2 original + 2 upgrades = 4 ops on V_0); all
// 4 ops emit KindCommit-Signed; L_0 σ-quorum.
func TestScenario_HV2RecoversViaUpgrades(t *testing.T) {
	s := newSim(t, 4)
	// Ops 1, 2 have V_0 + host valid initially. Ops 3, 4 are V-drops.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	// All fire Phase 2a. 1/2 → KindValue; 3/4 → KindNoValue.
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Reflood V_0 to V-drops + apply host validity → upgrades fire via cascade.
	leader := s.leaderAt(0)
	bundle, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	for _, op := range []OperatorID{3, 4} {
		require.NoError(t, s.instances[op].ObservePhase1Bundle(bundle, observedAfterPhase2a))
		require.NoError(t, s.instances[op].ApplyHostValidity(0, Value("V0"), true))
		_, ok := s.instances[op].OwnValueMsg()
		require.True(t, ok, "op %d should have emitted A1 upgrade", op)
	}
	// Cross-broadcast all Phase-2a emissions + upgrade KindValues.
	for _, op := range s.allOperators() {
		vm, _ := s.instances[op].OwnValueMsg()
		nv, _ := s.instances[op].OwnNoValueMsg()
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			if vm != nil {
				require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
			}
			if nv != nil {
				require.NoError(t, s.instances[peer].ObserveNoValueMsg(nv))
			}
		}
	}
	s.propagatePostPhase2aEmissions()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer)
	require.Equal(t, Value("V0"), out.Value)
}

// Host re-org mid-slot, 3-σV vs 1-NV: 4 ops fire KindValue at Phase 2a;
// one op's host flips to NV before its σ-eligibility commit fires.
// σ-eligibility trigger fires for all 4 (value_pool=4≥qV). The flipped
// op's side decision routes to NR (A3 host-flip pivot). σ-pool=3≥qV at
// L_0; slot succeeds at L_0. Per redesign plan §Liveness worked cases
// (line 786).
func TestScenario_HostFlipMidSlot_3v1_SucceedsAtL0(t *testing.T) {
	s := newSim(t, 4)
	// All 4 ops have V_0 + host valid initially.
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	// All fire KindValue at Phase 2a.
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	vmA, _ := s.instances[OperatorID(1)].OwnValueMsg()
	vmB, _ := s.instances[OperatorID(2)].OwnValueMsg()
	vmC, _ := s.instances[OperatorID(3)].OwnValueMsg()
	vmD, _ := s.instances[OperatorID(4)].OwnValueMsg()
	// Deliver only ONE peer KindValue to op 4 so its value_pool stays
	// below qV; then flip its host; then deliver the rest. By the time
	// σ-eligibility fires at op 4, the host re-check says NV → Commit-NR.
	require.NoError(t, s.instances[OperatorID(4)].ObserveValueMsg(vmA))
	require.NoError(t, s.instances[OperatorID(4)].ApplyHostValidity(0, Value("V0"), false))
	require.NoError(t, s.instances[OperatorID(4)].ObserveValueMsg(vmB))
	// At this point value_pool at op 4 = {self, op1, op2} = 3 = qV →
	// σ-eligibility fired during ObserveValueMsg(vmB)'s cascade. Side
	// decision: host re-check says NV → Commit-NR.
	c4, ok := s.instances[OperatorID(4)].OwnCommit()
	require.True(t, ok)
	require.Equal(t, CommitSideNR, c4.Side, "op 4 should be NR-side via A3 host-flip pivot")
	// Deliver op 3's KindValue to op 4 for completeness (no effect on commit).
	require.NoError(t, s.instances[OperatorID(4)].ObserveValueMsg(vmC))
	// Ops 1/2/3 see the full set; σ-eligibility fires; Commit-Signed via cascade.
	for from, vm := range map[OperatorID]*ValueMsg{1: vmA, 2: vmB, 3: vmC, 4: vmD} {
		for _, peer := range []OperatorID{1, 2, 3} {
			if peer == from {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
		}
	}
	// Cross-broadcast all Commits to all peers.
	s.propagatePostPhase2aEmissions()
	for _, op := range []OperatorID{1, 2, 3} {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok)
		require.Equal(t, CommitSideSigned, c.Side, "op %d should be σ-side", op)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveCommit(c))
		}
	}
	for _, peer := range []OperatorID{1, 2, 3} {
		require.NoError(t, s.instances[peer].ObserveCommit(c4))
	}
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "L_0 σ-quorum reaches with 3 σ-side ops")
}

// Host re-org mid-slot, 4-NV (all 4 ops' hosts flip post-Phase-2a-fire):
// σ-eligibility fires for all 4 (value_pool=4≥qV). All 4 side-decision
// to NR (host re-check NV). nr_tag_0-pool = 4 = qEnc → fall-through to
// L_1. Per redesign plan §Liveness worked cases (line 789).
func TestScenario_HostFlipMidSlot_4NV_FallsThroughToL1(t *testing.T) {
	s := newSim(t, 4)
	// L_0 delivery + host-valid at all 4 ops.
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	// L_1 has a healthy delivery too — fall-through has a target.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	// All fire KindValue at Phase 2a.
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Flip ALL 4 hosts to NV before σ-eligibility fires.
	for _, op := range s.allOperators() {
		require.NoError(t, s.instances[op].ApplyHostValidity(0, Value("V0"), false))
	}
	// Cross-broadcast KindValues. Each receiver's cascade fires
	// σ-eligibility (cluster value_pool = qV via peer KindValues), but
	// the side decision routes to NR (host re-check NV).
	for _, op := range s.allOperators() {
		vm, _ := s.instances[op].OwnValueMsg()
		require.NotNil(t, vm)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
		}
	}
	// All 4 ops emitted KindCommit-NR via A3 host-flip pivot.
	for _, op := range s.allOperators() {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok)
		require.Equal(t, CommitSideNR, c.Side, "op %d should be NR-side (host-flipped)", op)
	}
	s.propagatePostPhase2aEmissions()
	// Cross-broadcast commits.
	for _, op := range s.allOperators() {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveCommit(c))
		}
	}
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "fall-through to L_1 via L_0 NR-quorum")
}

// Host re-org mid-slot, 2-σV vs 2-NV (assumption-3 violation): 2 ops'
// hosts flip; 2 stay valid. σ-pool=2<qV; nr_tag_0-pool=2<qEnc. Both
// pools short → slot misses at L_0, NO fall-through (no T_commit hard
// wall to default the remaining ops). Inherited algebraic limit. Per
// redesign plan §Liveness worked cases (line 787).
func TestScenario_HostFlipMidSlot_2v2_StallsAtL0(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	// L_1 also has a healthy delivery (so we can verify fall-through DOESN'T happen).
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	// All fire KindValue.
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Flip ops 3, 4 to NV before σ-eligibility fires.
	for _, op := range []OperatorID{3, 4} {
		require.NoError(t, s.instances[op].ApplyHostValidity(0, Value("V0"), false))
	}
	// Cross-broadcast KindValues.
	for _, op := range s.allOperators() {
		vm, _ := s.instances[op].OwnValueMsg()
		require.NotNil(t, vm)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
		}
	}
	// Ops 1, 2 → Commit-Signed. Ops 3, 4 → Commit-NR (A3 pivot).
	s.propagatePostPhase2aEmissions()
	for _, op := range s.allOperators() {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveCommit(c))
		}
	}
	// σ-pool[V_0] = {op1, op2} = 2 < qV. nr_tag_0-pool = {op3, op4} = 2 < qEnc.
	// Neither σ-quorum nor NR-quorum reaches at L_0. Resolve returns deadlock.
	for _, op := range s.allOperators() {
		_, err := s.instances[op].Resolve()
		require.Error(t, err, "op %d Resolve should miss (2-2 split)", op)
		var rerr *ResolveError
		require.ErrorAs(t, err, &rerr)
		require.Equal(t, ResolveFailureDeadlock, rerr.Reason,
			"L_0 deadlock — both quorums short, no fall-through")
	}
}

// Validity-divergence 2-σV vs 2-NV AT PHASE 1 (algebraic limit, distinct
// from the host-flip case above — here divergence starts before Phase 2a
// fires, so 2 ops emit KindValue and 2 emit KindNoValue). value_pool=2 <
// qV; noValuePool=2 < qEnc. Neither trigger fires; cluster stalls until
// slot deadline. Per redesign plan §Liveness worked cases (line 785).
func TestScenario_ValidityDivergence2v2_AtPhase1_StallsAtL0(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	// 2 host-valid, 2 host-NV at Phase 2a fire-time.
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	s.applyHostValidityFor([]OperatorID{3, 4}, 0, Value("V0"), false)
	// L_1 healthy for completeness.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	// Ops 1/2 fire KindValue; ops 3/4 fire KindNoValue (host NV at fire).
	s.firePhase2aAll()
	// value_pool = 2 < qV; noValuePool = 2 < qEnc. No trigger fires. Slot misses.
	for _, op := range s.allOperators() {
		_, ok := s.instances[op].OwnCommit()
		require.False(t, ok, "op %d should not have committed (2-2 algebraic limit)", op)
	}
	for _, op := range s.allOperators() {
		_, err := s.instances[op].Resolve()
		require.Error(t, err)
	}
}

// 1-1-1 byz leader equivocation, recovery via Phase-1 reflood. Each
// honest gets a different V from byz leader → no equivocation observed
// locally initially. After reflood delivers all V's to each honest
// (modeled here by manual delivery), each retainedBundles[L_0][leader]
// has ≥ 2 V's → equivocation trigger fires for each honest → all emit
// KindCommit-NR (A4 pivot). nr_tag_0-pool = 3 = qEnc → fall-through.
// Per redesign plan §Liveness worked cases (line 784).
func TestScenario_Equivocation111_ViaReflood_FallsThrough(t *testing.T) {
	s := newSim(t, 4)
	// Byz leader = op 1 (L_0 leader). Initially deliver V_a to op 2, V_b
	// to op 3, V_c to op 4. Each honest sees 1 V from the leader.
	leader := s.leaderAt(0)
	bA, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	bB, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_b"))
	require.NoError(t, err)
	bC, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_c"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(2)].ObservePhase1Bundle(bA, observedEarly))
	require.NoError(t, s.instances[OperatorID(3)].ObservePhase1Bundle(bB, observedEarly))
	require.NoError(t, s.instances[OperatorID(4)].ObservePhase1Bundle(bC, observedEarly))
	s.applyHostValidityFor([]OperatorID{2}, 0, Value("V_a"), true)
	s.applyHostValidityFor([]OperatorID{3}, 0, Value("V_b"), true)
	s.applyHostValidityFor([]OperatorID{4}, 0, Value("V_c"), true)
	// L_1 has a healthy honest leader for fall-through.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)
	s.applyHostValidityAll(1, s.candidates[1], true)
	// Honest 2, 3, 4 fire Phase 2a → each KindValue on their own V.
	// (op 1 is byz; silent at Phase 2.)
	for _, op := range []OperatorID{2, 3, 4} {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Reflood: deliver the alternative V's to each honest. After reflood,
	// each honest has 2 retained V's from the leader → equivocation
	// trigger ready to fire on next cascade.
	require.NoError(t, s.instances[OperatorID(2)].ObservePhase1Bundle(bB, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(3)].ObservePhase1Bundle(bA, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(4)].ObservePhase1Bundle(bA, observedAfterPhase2a))
	// Each ObservePhase1Bundle's cascade fires equivocation trigger →
	// A4 pivot from prior KindValue → Commit-NR.
	for _, op := range []OperatorID{2, 3, 4} {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok, "op %d should have pivoted to Commit-NR via equivocation trigger", op)
		require.Equal(t, CommitSideNR, c.Side)
	}
	// Cross-broadcast everything for completion.
	for _, op := range []OperatorID{2, 3, 4} {
		vm, _ := s.instances[op].OwnValueMsg()
		c, _ := s.instances[op].OwnCommit()
		for _, peer := range []OperatorID{2, 3, 4} {
			if peer == op {
				continue
			}
			if vm != nil {
				_ = s.instances[peer].ObserveValueMsg(vm) // may fire Rule 6a / cross-V; ignore for this test
			}
			require.NoError(t, s.instances[peer].ObserveCommit(c))
		}
	}
	outputs, errs := s.resolveAll()
	// Op 1 (byz) won't resolve since it never fired Phase 2a. Filter.
	delete(outputs, OperatorID(1))
	delete(errs, OperatorID(1))
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "fall-through to L_1 via L_0 NR-quorum (3 honest NRs reach qEnc)")
}

// Slot-miss case (companion to TestScenario_NonUniformMeshTailRecovery):
// same setup, but the late KindValue NEVER arrives at the slow-view op.
// Op 1 stays waiting indefinitely; without a T_commit hard wall there's
// nothing to force a default. Resolve returns NoQuorum (deadlock at L_0).
//
// This demonstrates the wait-or-fail-cleanly semantics: v4 has no
// premature NR-default; if recovery doesn't happen within the slot
// deadline, the runner abandons the slot at relay-cutoff and the slot
// misses cleanly. No safety violation.
func TestScenario_MeshTail_NoRecovery_MissesCleanly(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	// All fire Phase 2a.
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	vmA, _ := s.instances[OperatorID(1)].OwnValueMsg()
	vmB, _ := s.instances[OperatorID(2)].OwnValueMsg()
	vmC, _ := s.instances[OperatorID(3)].OwnValueMsg()
	nvD, _ := s.instances[OperatorID(4)].OwnNoValueMsg()
	_ = vmC // intentionally undelivered to ops 1, 2 in the "no recovery" scenario
	// Op 1 misses op 3's KindValue and op 2 misses op 3's KindValue too —
	// neither reaches qV; cluster can't σ-commit. Op 4's NoValue reaches
	// all but noValuePool stays at 1 < qEnc. Stalemate.
	require.NoError(t, s.instances[OperatorID(1)].ObserveValueMsg(vmB))
	require.NoError(t, s.instances[OperatorID(1)].ObserveNoValueMsg(nvD))
	require.NoError(t, s.instances[OperatorID(2)].ObserveValueMsg(vmA))
	require.NoError(t, s.instances[OperatorID(2)].ObserveNoValueMsg(nvD))
	require.NoError(t, s.instances[OperatorID(3)].ObserveValueMsg(vmA))
	require.NoError(t, s.instances[OperatorID(3)].ObserveValueMsg(vmB))
	require.NoError(t, s.instances[OperatorID(3)].ObserveNoValueMsg(nvD))
	require.NoError(t, s.instances[OperatorID(4)].ObserveValueMsg(vmA))
	require.NoError(t, s.instances[OperatorID(4)].ObserveValueMsg(vmB))
	// No op has committed (value_pool < qV at the σ-eligible ops; cannot-σ
	// gate blocks NR-eligibility; equivocation not observed).
	for _, op := range []OperatorID{1, 2} {
		_, ok := s.instances[op].OwnCommit()
		require.False(t, ok, "op %d (σ-eligible but pool short) should be waiting", op)
	}
	// Now if op 3 also doesn't see op 1's full set... let's check op 3
	// — it saw 2 peer KindValues, so value_pool = self + op1 + op2 = 3 =
	// qV → σ-eligibility fired → op 3 emitted Commit-Signed.
	c3, ok := s.instances[OperatorID(3)].OwnCommit()
	require.True(t, ok)
	require.Equal(t, CommitSideSigned, c3.Side)
	// Resolve at op 1 → no σ-quorum (sigmaPool has only op3's partial via
	// inference but op 3's Commit-Signed hasn't been broadcast to op 1).
	// Actually inference happens via ObserveCommit. Let me check more
	// carefully: op 3's Commit-Signed hasn't been delivered to op 1 yet.
	// So sigmaPool[op1's view] = empty (op 1 self isn't σ-side either —
	// op 1 didn't emit Commit-Signed because value_pool was short).
	_, err := s.instances[OperatorID(1)].Resolve()
	require.Error(t, err, "op 1 misses cleanly with no σ-quorum")
}

// Validity-divergence: 3-σV vs 1-NV. 3 ops host-valid on V_0, 1 op
// host-NV. σ-eligibility fires with valuePool=3 ≥ qV. NV-side op's
// side-decision routes to NR (host re-check says NV). 3 σ + 1 NR =
// L_0 σ-quorum on V_0.
func TestScenario_ValidityDivergence3of4Succeeds(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	s.applyHostValidityFor([]OperatorID{4}, 0, Value("V0"), false)
	s.firePhase2aAll()
	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "3-σ + 1-NR reaches L_0 σ-quorum")
	require.Equal(t, Value("V0"), out.Value)
}

package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Worked-cases catalog per docs/2abOBFT.md §Liveness (synchrony-conditional).
// Each scenario corresponds to a row in the worked-cases table — see the
// spec's Liveness section for the full state-transition narrative.

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

// TestScenario_DualPoolMembershipForA3HostFlipPivot is obsolete post Op5:
// the A3 host-flip pivot is removed (KindValue is σ-locking at L_0; an
// op cannot transition to KindCommit-NR after emitting KindValue). Pool
// semantics still distinguish claim pools from threshold pools, but the
// dual-pool case this test exercised no longer arises from honest ops.
// (Byz cross-signing — KindValue + KindCommit-NR from same op — is now
// Rule 1 / Rule 6a slashable; covered in phase2b_test.)
func TestScenario_DualPoolMembershipForA3HostFlipPivot(t *testing.T) {
	t.Skip("Op5 removes A3 host-flip pivot; obsolete scenario — see plan §Op5 line 1253")
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
		Witnesses:  l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:  Signature{0x01},                         // Op5: arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
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
	// Post Op5: Commit is NR-side only; KindCommit-NR with verified
	// nr_tag_0 partial. Re-broadcasts of the identical commit dedup
	// silently (no new evidence, no double-pool growth).
	nrTag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrPartial, err := s.instances[OperatorID(1)].tagSigner.SignPartial(nrTag)
	require.NoError(t, err)
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  nrPartial,
	}
	require.NoError(t, op2.ObserveCommit(c))
	beforeEvidence := len(op2.Evidence())
	require.NoError(t, op2.ObserveCommit(c))
	require.NoError(t, op2.ObserveCommit(c))
	require.Equal(t, beforeEvidence, len(op2.Evidence()))
	require.Equal(t, 1, op2.noValuePoolSize(0))
}

// TestObserveCommit_KindCommitNRPopulatesNoValuePool: per inference rules,
// KindCommit-NR observation adds the op to noValuePool[L_0] AND to
// nrTagPool[L_0] (the partial is extracted), gated by NR partial
// verification. This is the NR-side counterpart to
// TestObserveCommit_KindCommitSignedInfersKindValue.
func TestObserveCommit_KindCommitNRPopulatesNoValuePool(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Build a properly-signed NR partial on nr_tag_0 from op 1's tagSigner.
	tag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	op1TagSigner := NewStubSigner(s.cfg.QV(), s.pubShares[OperatorID(1)])
	sig, err := op1TagSigner.SignPartial(tag)
	require.NoError(t, err)
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  sig,
	}
	require.NoError(t, op2.ObserveCommit(c))
	require.True(t, op2.noValuePool[0][OperatorID(1)],
		"KindCommit-NR observation should add op to noValuePool[L_0]")
	require.NotNil(t, op2.nrTagPool[0][OperatorID(1)],
		"KindCommit-NR observation should add op to nrTagPool[L_0]")
}

// TestProcessObservedLayerEntries_GarbageNRPlaintextIsSkipped: an L_k>0
// NRPlaintext entry with a payload that fails nr_tag_k verification is
// skipped — the byz contribution doesn't poison nrTagPool[k] or
// noValuePool[k]. Companion to the L_0 NR partial verification: closes
// the pool-poisoning vector at deeper layers.
func TestProcessObservedLayerEntries_GarbageNRPlaintextIsSkipped(t *testing.T) {
	// K=3: L_1 is a middle layer where NRPlaintext is structurally valid
	// (NRPlaintext at the deepest layer L_{K-1} is rejected by message
	// validation, so we need K ≥ 3 to exercise the verification gate).
	s := newSimWithK(t, 4, 3)
	op2 := s.instances[OperatorID(2)]
	nv := &NoValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryNRPlaintext, Payload: []byte("garbage")},
			{Layer: 2, Kind: LayerEntryEmpty},
		},
	}
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	require.False(t, op2.noValuePool[1][OperatorID(1)],
		"garbage NRPlaintext at L_1 should be skipped — no noValuePool entry")
	require.Nil(t, op2.nrTagPool[1][OperatorID(1)],
		"garbage NRPlaintext at L_1 should not poison nrTagPool")
}

// TestObserveCommit_KindCommitNRWithGarbagePartialIsRejected: byz crafted
// NR partial that doesn't verify against op's IBE share → ObserveCommit
// silently skips the pool update (the L_0 claim has no valid signature
// backing it). Closes the pool-poisoning vector for nr_tag_0 partials
// that the previous unconditional addToNrTagPool allowed.
func TestObserveCommit_KindCommitNRWithGarbagePartialIsRejected(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  Signature("garbage-not-a-valid-nr-partial"),
	}
	require.NoError(t, op2.ObserveCommit(c))
	require.False(t, op2.noValuePool[0][OperatorID(1)],
		"garbage NR partial should be rejected; op should not be in noValuePool")
	require.Nil(t, op2.nrTagPool[0][OperatorID(1)],
		"garbage NR partial should not poison nrTagPool")
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

// TestCascadeErrors_HappyPathIsEmpty: in the typical no-error flow, no
// errors accumulate in cascadeErrors. Sanity check that the
// ErrUpgradeNotAvailable sentinel is correctly filtered out (it's the
// preconditions-not-met no-op, not a genuine failure).
func TestCascadeErrors_HappyPathIsEmpty(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	for _, op := range s.allOperators() {
		require.Empty(t, s.instances[op].CascadeErrors(),
			"op %d: healthy path should produce no cascade errors", op)
	}
}

// TestCascadeErrors_NoValueFlowIsEmpty exercises the ErrUpgradeNotAvailable
// filter path explicitly. With no V delivered to anyone, every op fires
// KindNoValue at Phase 2a. The cascade then attempts
// MaybeBuildAndBroadcastUpgrade for each op — which returns
// ErrUpgradeNotAvailable (no V_0 retained at L_0). The filter MUST
// recognize this sentinel and skip accumulating it; if the filter
// regressed to always-record, this test would flag a non-empty
// CascadeErrors despite no genuine signer failure occurring.
func TestCascadeErrors_NoValueFlowIsEmpty(t *testing.T) {
	s := newSim(t, 4)
	// Do NOT call deliverPhase1 → no V retained at L_0 anywhere.
	// host-validity not applied either → ops can't σ-eligibility-fire
	// even if V arrived. Phase-2a fire produces KindNoValue for all
	// (no V_local).
	s.firePhase2aAll()
	for _, op := range s.allOperators() {
		require.Empty(t, s.instances[op].CascadeErrors(),
			"op %d: NoValue path with no signer failure should produce no cascade errors (ErrUpgradeNotAvailable must be filtered)", op)
		_, hadNV := s.instances[op].OwnNoValueMsg()
		require.True(t, hadNV,
			"op %d: should have fired KindNoValue at Phase 2a (sanity)", op)
	}
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

	// At this point op 1's σ-pool[V_0] contains {self, op2} via direct
	// KindValue observations (Op5: σ partial in KindValue.L0Partial).
	// op 3's σ partial is missing → σ-pool size = 2 < qV=3.
	// No Phase-2b Commit-Signed under Op5 — the σ-side terminal emission
	// IS the KindValue itself. The cluster simply waits for the missing
	// KindValue to arrive.
	root := ValueRoot(Value("V0"))
	require.Equal(t, 2, len(s.instances[OperatorID(1)].sigmaPool[0][root]),
		"slow-view op 1's σ-pool has 2 partials, awaiting the delayed peer KindValue")

	// Now simulate the mesh-tail recovery: op 3's KindValue finally
	// arrives at op 1. σ-pool grows to qV via direct observation.
	require.NoError(t, s.instances[OperatorID(1)].ObserveValueMsg(vmC))
	require.GreaterOrEqual(t, len(s.instances[OperatorID(1)].sigmaPool[0][root]), s.cfg.QV(),
		"σ-pool[V_0] reaches qV at op 1 once the delayed KindValue arrives (1-hop Op5 cascade)")

	// Cross-broadcast all Commits so the cluster can reach σ-quorum.
	// (Under Op5 ops 1/2/3 didn't emit any σ-side Commit — there's no
	// commit to broadcast. The σ-pool grew directly via KindValue.)
	for _, op := range []OperatorID{1, 2, 3} {
		c, ok := s.instances[op].OwnCommit()
		if !ok {
			continue // no Commit under Op5 σ-side path
		}
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

// Host re-org mid-slot, post-Op5: 4 ops fire KindValue at Phase 2a; one
// op's host flips to NV mid-slot. Under v4 (pre-Op5), this would have
// pivoted that op to KindCommit-NR via A3 host-flip pivot at Phase 2b.
// Post-Op5, the σ-lock is acquired at KindValue emit time — the op is
// already σ-committed to V_0 and cannot pivot. The host-flip is a no-op
// at the protocol level (the op stays σ-locked; the validator bears the
// chain-reorg risk normally). Cluster σ-pool[V_0] still reaches qV via
// direct KindValue observations (1-hop Op5 cascade); slot succeeds at
// L_0 with V_0.
//
// Trade-off: pre-Op5's A3 safety net (avoiding signing a re-orged V) is
// lost. See plan §Op5 line 1253 — explicit structural cost. Assumption 3
// (host validity best-effort unanimous at decision time) makes mid-slot
// flips rare; when they occur, validators take normal reorg-slashing
// risk.
func TestScenario_HostFlipMidSlot_3v1_SucceedsAtL0(t *testing.T) {
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
	vmD, _ := s.instances[OperatorID(4)].OwnValueMsg()
	require.NotNil(t, vmA)
	require.NotNil(t, vmB)
	require.NotNil(t, vmC)
	require.NotNil(t, vmD)
	require.NotEmpty(t, vmD.L0Partial, "op 4 emitted KindValue σ-locked at L_0 on V_0")
	// Host flips mid-slot to NV at op 4. Under Op5 this has no protocol
	// effect — op 4 is already σ-locked.
	require.NoError(t, s.instances[OperatorID(4)].ApplyHostValidity(0, Value("V0"), false))
	// Op 4 stays σ-committed; no Commit-NR pivot fires.
	_, op4HasCommit := s.instances[OperatorID(4)].OwnCommit()
	require.False(t, op4HasCommit, "Op5: σ-locked op cannot pivot to NR on host flip (A3 removed)")
	// Cross-broadcast all KindValues. Cluster σ-pool reaches qV at every
	// honest receiver.
	for from, vm := range map[OperatorID]*ValueMsg{1: vmA, 2: vmB, 3: vmC, 4: vmD} {
		for _, peer := range s.allOperators() {
			if peer == from {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveValueMsg(vm))
		}
	}
	s.propagatePostPhase2aEmissions()
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
//
// NOTE (sub-chunk #1 / Op3): the L_0 leader now σ-locks at Phase-1 build
// time via L_0 witness signing, so the A3 host-flip pivot is structurally
// no longer available to the leader. Under Op3-only intermediate state,
// the leader emits Commit-Signed despite host-NV; non-leaders pivot to
// Commit-NR. Σ-pool[V_0] = {leader} = 1 < qV=3; nr_tag_0-pool = 3 = qEnc
// → fall-through to L_1 still succeeds. Once sub-chunk #3 (Op5) lands,
// no op can pivot (KindValue is σ-direction terminal), and the test
// becomes "all-σ at L_0 despite host-flips, slot decides at L_0 with V"
// — entirely different shape. Skipping with TODO instead of constantly
// re-fixing across intermediate states.
func TestScenario_HostFlipMidSlot_4NV_FallsThroughToL1(t *testing.T) {
	t.Skip("test reframed by sub-chunk #3 (Op5) — A3 host-flip pivot obsolete under Op5; rewrite when Op5 lands")
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

// TestScenario_HostFlipMidSlot_2v2_StallsAtL0 is obsolete post Op5: the
// A3 host-flip pivot is removed. Under Op5 all 4 ops emit KindValue
// σ-locked at L_0 BEFORE the host flip can re-route them. The σ partials
// pool cluster-wide; σ-quorum reaches at L_0 with V_0 — the cluster
// commits to V_0 regardless of the late host flip. The "stall on 2-2
// host flip" scenario depended on A3 pivot — that path is gone.
func TestScenario_HostFlipMidSlot_2v2_StallsAtL0(t *testing.T) {
	t.Skip("Op5 removes A3 host-flip pivot; the 2-2 host-flip stall scenario doesn't arise under Op5 (cluster σ-locks at Phase 2a fire)")
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

// 1-1-1 byz leader equivocation, post Op5: each honest gets a different
// V from byz leader → emits KindValue σ-locked on their own V at Phase 2a.
// After reflood delivers all V's to each honest, each retainedBundles[
// L_0][leader] grows to ≥ 2 V's → Rule 2 evidence fires. But under Op5
// no NR pivot is available post-KindValue-σ-lock. σ-pool fragments (each
// V has 1 partial cluster-wide); no V reaches qV. NR-pool empty (no NR
// commits). Slot misses at L_0 with no fall-through. Pre-Op5 this
// scenario succeeded at L_1 via A4 pivot + NR-fall-through; the loss is
// a documented Op5 trade-off (plan §Op6 line 1276: equivocation
// observed POST-KindValue-emission has no recovery path). Pigeonhole 2
// safety holds: no V reaches qV, no cluster decision on any V.
func TestScenario_Equivocation111_PostKindValue_MissesUnderOp5(t *testing.T) {
	s := newSim(t, 4)
	// Byz leader = op 1 (L_0 leader). Initially deliver V_a to op 2, V_b
	// to op 3, V_c to op 4. Each honest sees 1 V from the leader.
	// Byz-equivocation simulation: first bundle uses the legitimate path
	// (acquires σ-lock on V_a); subsequent bundles bypass EKM via direct
	// signer access — exactly as a byz leader would.
	leader := s.leaderAt(0)
	bA, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	bC := s.buildByzEquivocatingBundle(leader, 0, Value("V_c"))
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
	// each honest has 2 retained V's from the leader → Rule 2 evidence
	// fires (leader equivocation observed).
	require.NoError(t, s.instances[OperatorID(2)].ObservePhase1Bundle(bB, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(3)].ObservePhase1Bundle(bA, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(4)].ObservePhase1Bundle(bA, observedAfterPhase2a))
	// Post Op5: ops are σ-locked at L_0 from their Phase 2a KindValue
	// emission; equivocation observed post-emission does NOT pivot to
	// Commit-NR (A4 path is removed). Each op stays σ-committed on
	// their respective V. Rule 2 evidence still fires on the equivocation
	// observation (slashable signal preserved).
	for _, op := range []OperatorID{2, 3, 4} {
		var foundRule2 bool
		for _, e := range s.instances[op].Evidence() {
			if e.Rule == EvidenceLeaderEquivocation {
				foundRule2 = true
			}
		}
		require.True(t, foundRule2,
			"Op5: equivocation post-KindValue fires Rule 2 (no NR pivot, but slashable signal preserved)")
		_, hadCommit := s.instances[op].OwnCommit()
		require.False(t, hadCommit,
			"Op5: post-KindValue equivocation does NOT pivot to Commit-NR; op stays σ-locked")
	}
	// Cluster cannot reach σ-quorum (each op σ-locked on a DIFFERENT V;
	// σ-pool fragments at 1 partial per V). Cluster cannot reach
	// NR-quorum (no NR commits). Slot misses — by Pigeonhole 2 safety
	// holds (no V reaches qV). Documented Op5 trade-off (§Op6 line 1276).
	outputs, errs := s.resolveAll()
	delete(outputs, OperatorID(1)) // byz didn't emit Phase 2a
	delete(errs, OperatorID(1))
	for op, err := range errs {
		require.Error(t, err, "op %d should miss (σ-fragmented post-Op5)", op)
	}
	// No agreement reachable.
	_ = outputs
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
	// Post Op5: ops 1, 2, 3 all σ-locked at L_0 at Phase 2a fire (KindValue
	// carries σ partial). Op 4 is on KindNoValue path. No Commit fires on
	// σ-side. Op 4's NR-eligibility gate isn't satisfied (noValuePool=1
	// at op 4 since only its own KindNoValue arrived; the 2 σ-honest
	// peers didn't emit NR). So op 4 also waits, no Commit fires
	// anywhere.
	for _, op := range []OperatorID{1, 2, 3, 4} {
		_, ok := s.instances[op].OwnCommit()
		require.False(t, ok, "op %d should be waiting (no Commit fires under Op5 σ-side; NR-eligibility gate unsatisfied at op 4)", op)
	}
	// Op 1's σ-pool[V_0] = {self, op2} (its own L0Partial + op2's
	// observed L0Partial via vmB) = 2 < qV=3. No σ-quorum, no
	// NR-quorum → Resolve fails at op 1.
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

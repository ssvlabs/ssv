package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestApplyHostValidity_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), true))
	valid, recorded := op.HostValidity(0, Value("V0"))
	require.True(t, recorded)
	require.True(t, valid)
}

func TestApplyHostValidity_NotValid(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), false))
	valid, recorded := op.HostValidity(0, Value("V0"))
	require.True(t, recorded)
	require.False(t, valid)
}

func TestApplyHostValidity_OverwritePreservesLatest(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), true))
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), false))
	valid, _ := op.HostValidity(0, Value("V0"))
	require.False(t, valid)
}

func TestApplyHostValidity_RejectsBadLayer(t *testing.T) {
	s := newSim(t, 4)
	require.Error(t, s.instances[OperatorID(1)].ApplyHostValidity(99, Value("V0"), true))
}

func TestApplyHostValidity_RejectsEmptyValue(t *testing.T) {
	s := newSim(t, 4)
	require.Error(t, s.instances[OperatorID(1)].ApplyHostValidity(0, Value{}, true))
}

// TestMaybeFirePhase2a_HealthyValuePath: every op has V_0 + host valid →
// emits KindValue at Phase 2a fire.
func TestMaybeFirePhase2a_HealthyValuePath(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	for _, op := range s.allOperators() {
		vm, nv, c, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
		require.NotNil(t, vm, "op %d should emit KindValue", op)
		require.Nil(t, nv)
		require.Nil(t, c)
		require.Equal(t, Value("V0"), vm.V)
	}
}

// TestMaybeFirePhase2a_NoValueWhenNoRetention: op without V_0 emits NoValue.
func TestMaybeFirePhase2a_NoValueWhenNoRetention(t *testing.T) {
	s := newSim(t, 4)
	// Deliver V_0 only to ops 1,2,3. Op 4 has nothing retained.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	vm, nv, c, err := s.instances[OperatorID(4)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.NotNil(t, nv, "op without V_0 should emit KindNoValue")
	require.Nil(t, c)
}

// TestMaybeFirePhase2a_NoValueWhenHostInvalid: op has V_0 but host says NV.
func TestMaybeFirePhase2a_NoValueWhenHostInvalid(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	// Op 1: host says NV.
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), false)
	vm, nv, c, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.NotNil(t, nv)
	require.Nil(t, c)
}

// TestMaybeFirePhase2a_NRDirectWhenEquivocationObserved: op observed ≥2
// distinct V's at L_0 → emits Commit-NRDirect (A8 path).
func TestMaybeFirePhase2a_NRDirectWhenEquivocationObserved(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		[]OperatorID{1}, []OperatorID{1}, observedEarly)
	// Op 1 has both V_a and V_b retained.
	vm, nv, c, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.Nil(t, nv)
	require.NotNil(t, c)
	require.Equal(t, CommitSideNRDirect, c.Side)
}

func TestMaybeFirePhase2a_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), true)
	vm1, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	vm2, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Same(t, vm1, vm2, "second MaybeFirePhase2a should return cached emission")
}

// TestMaybeBuildAndBroadcastUpgrade_A1: NoValue-path op receives V_0 +
// host valid → emits upgrade KindValue.
func TestMaybeBuildAndBroadcastUpgrade_A1(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 doesn't receive V_0 initially.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2, 3, 4}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2, 3, 4}, 0, Value("V0"), true)
	vm, nv, c, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.NotNil(t, nv)
	require.Nil(t, c)
	// Late delivery of V_0 + host valid to op 1.
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(1)].ObservePhase1Bundle(b, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(1)].ApplyHostValidity(0, Value("V0"), true))
	// Upgrade should have fired automatically via the afterStateDelta cascade.
	upgrade, ok := s.instances[OperatorID(1)].OwnValueMsg()
	require.True(t, ok, "op 1 should have emitted upgrade KindValue")
	require.Equal(t, Value("V0"), upgrade.V)
}

// TestMaybeBuildAndBroadcastUpgrade_NotAvailableWhenNoNoValueMsg.
func TestMaybeBuildAndBroadcastUpgrade_NotAvailableWhenNoNoValueMsg(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 hasn't fired Phase 2a yet (so no ownNoValueMsg).
	_, err := s.instances[OperatorID(1)].MaybeBuildAndBroadcastUpgrade()
	require.ErrorIs(t, err, ErrUpgradeNotAvailable)
}

// ---------- Op11 peer-reflood-V harvest tests ----------

// TestMaybeFirePhase2a_ValuePath_PopulatesL0Witness verifies that Op11
// builder forwards the L_0 leader's L0Witness from the retained Phase-1
// bundle into the emitted KindValue (so receivers can verify and harvest
// V via peer-reflood).
func TestMaybeFirePhase2a_ValuePath_PopulatesL0Witness(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	vm, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vm)
	require.NotEmpty(t, vm.L0Witness,
		"Op11: KindValue must carry the L_0 leader's L0Witness forwarded from the retained bundle")
	// The forwarded witness should match what the leader signs on V_0.
	expected, err := s.instances[leader].signer.SignPartial(Value("V0"))
	require.NoError(t, err)
	require.Equal(t, expected, vm.L0Witness,
		"forwarded L0Witness must equal the leader's σ partial bytes (deterministic stub signer)")
}

// TestMaybeBuildAndBroadcastUpgrade_PopulatesL0Witness verifies that the
// A1 upgrade path also forwards L0Witness from the retained bundle.
func TestMaybeBuildAndBroadcastUpgrade_PopulatesL0Witness(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 starts on KindNoValue path, then harvests V via late bundle arrival.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2, 3, 4}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2, 3, 4}, 0, Value("V0"), true)
	_, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(1)].ObservePhase1Bundle(b, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(1)].ApplyHostValidity(0, Value("V0"), true))
	upgrade, ok := s.instances[OperatorID(1)].OwnValueMsg()
	require.True(t, ok)
	require.NotEmpty(t, upgrade.L0Witness,
		"Op11: A1 upgrade KindValue must also carry the leader's L0Witness")
}

// TestObserveValueMsg_HarvestSeedsRetentionAndSigmaPool verifies the core
// Op11 harvest: a V-drop receiver observing a peer KindValue with a
// verifying L0Witness establishes retention[0][leader] = {V} AND seeds
// σ-pool[0][V_root][leader] with the leader's partial (effectively as
// if the receiver had observed the leader's Phase-1 bundle directly).
func TestObserveValueMsg_HarvestSeedsRetentionAndSigmaPool(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// V-recipient (op2) gets the bundle; V-drops (op3, op4) do NOT.
	s.deliverPhase1(0, Value("V0"), []OperatorID{leader, OperatorID(2)}, observedEarly)
	s.applyHostValidityFor([]OperatorID{leader, OperatorID(2)}, 0, Value("V0"), true)
	// op2 fires KindValue with the leader's L0Witness forwarded.
	vm, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vm)
	// op3 (V-drop) observes op2's KindValue → harvests V.
	op3 := s.instances[OperatorID(3)]
	require.Empty(t, op3.RetainedBundles(0, leader), "op3 has no Phase-1 bundle before harvest")
	require.NoError(t, op3.ObserveValueMsg(vm))
	require.Len(t, op3.RetainedBundles(0, leader), 1,
		"Op11: peer KindValue with valid L0Witness should harvest V into retention")
	require.Equal(t, Value("V0"), op3.RetainedBundles(0, leader)[0].Bundle.Value)
	root := ValueRoot(Value("V0"))
	require.NotEmpty(t, op3.sigmaPool[0][root][leader],
		"Op11: σ-pool[V_0][leader] should be seeded with the forwarded L0Witness")
}

// TestObserveValueMsg_HarvestEnqueuesValidationRequest verifies that the
// first-time harvest enqueues a ValidationRequest on WantsHostValidationCh.
// Mirrors OBFT's WantsHostValidationCh pattern.
func TestObserveValueMsg_HarvestEnqueuesValidationRequest(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	s.deliverPhase1(0, Value("V0"), []OperatorID{leader, OperatorID(2)}, observedEarly)
	s.applyHostValidityFor([]OperatorID{leader, OperatorID(2)}, 0, Value("V0"), true)
	vm, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	op3 := s.instances[OperatorID(3)]
	require.NoError(t, op3.ObserveValueMsg(vm))
	select {
	case req := <-op3.WantsHostValidationCh():
		require.Equal(t, 0, req.Layer)
		require.Equal(t, Value("V0"), req.Value)
	default:
		t.Fatal("Op11: harvest should enqueue ValidationRequest on WantsHostValidationCh")
	}
}

// TestObserveValueMsg_HarvestDoesNotEnqueueOnDirectRetention verifies
// that the harvest path skips the validation-request enqueue when the
// op already retained V via direct Phase-1 observation — the host has
// already been queried by the runner at bundle arrival.
func TestObserveValueMsg_HarvestDoesNotEnqueueOnDirectRetention(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	vm, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	op3 := s.instances[OperatorID(3)]
	// op3 ALREADY has V_0 retained via direct Phase-1.
	require.Len(t, op3.RetainedBundles(0, s.leaderAt(0)), 1)
	require.NoError(t, op3.ObserveValueMsg(vm))
	select {
	case req := <-op3.WantsHostValidationCh():
		t.Fatalf("op3 already had V retained directly; harvest should NOT enqueue (got %+v)", req)
	default:
	}
}

// TestObserveValueMsg_FakeL0WitnessSilentlyDiscarded verifies the
// anti-framing guarantee: a peer KindValue with a bogus L0Witness does
// not harvest V into retention, does not seed σ-pool, and does NOT
// enqueue a validation request OR fire Rule 5 (the leader would be
// falsely accused if Rule 5 fired on the emitter-signed envelope).
func TestObserveValueMsg_FakeL0WitnessSilentlyDiscarded(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Forge a KindValue from op 2 with V claim but bogus L0Witness bytes.
	bogus := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V_fake"),
		ValueRoot:    ValueRoot(Value("V_fake")),
		L0Witness:    Signature{0xde, 0xad, 0xbe, 0xef},
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	op3 := s.instances[OperatorID(3)]
	require.NoError(t, op3.ObserveValueMsg(bogus))
	require.Empty(t, op3.RetainedBundles(0, leader),
		"bogus L0Witness must not harvest V into retention")
	select {
	case req := <-op3.WantsHostValidationCh():
		t.Fatalf("bogus L0Witness must not enqueue validation (got %+v)", req)
	default:
	}
	for _, e := range op3.Evidence() {
		require.NotEqualf(t, EvidenceFakePlaintextSigma, e.Rule,
			"bogus L0Witness in peer KindValue must NOT fire Rule 5 (framing-attack guard)")
	}
}

// TestRequestHostValidation_DedupesAgainstExistingVerdict verifies that
// the Instance's request-side dedup skips enqueue when the host has
// already validated the (layer, V) pair.
func TestRequestHostValidation_DedupesAgainstExistingVerdict(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	require.NoError(t, op.ApplyHostValidity(0, Value("V0"), true))
	op.requestHostValidation(0, Value("V0"))
	select {
	case req := <-op.WantsHostValidationCh():
		t.Fatalf("already-validated V should dedup (got %+v)", req)
	default:
	}
}

// TestRequestHostValidation_DedupesInFlightRequest verifies that two
// consecutive requestHostValidation calls for the same (layer, V) only
// enqueue one ValidationRequest until the verdict arrives.
func TestRequestHostValidation_DedupesInFlightRequest(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	op.requestHostValidation(0, Value("V0"))
	op.requestHostValidation(0, Value("V0"))
	count := 0
drainLoop:
	for {
		select {
		case <-op.WantsHostValidationCh():
			count++
		default:
			break drainLoop
		}
	}
	require.Equal(t, 1, count, "in-flight dedup: should only enqueue one request per (layer, V_root)")
}

// TestFinalize_ClosesWantsHostValidationCh verifies that Finalize closes
// the channel so runners draining via range terminate cleanly.
func TestFinalize_ClosesWantsHostValidationCh(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	op.Finalize()
	// Reading from a closed channel returns zero value + ok=false.
	_, ok := <-op.WantsHostValidationCh()
	require.False(t, ok, "channel should be closed after Finalize")
	// Idempotent: second Finalize must not panic on close-of-closed.
	require.NotPanics(t, op.Finalize, "Finalize must be idempotent")
}

// TestObserveValueMsg_HarvestSecondDistinctVFiresRule2 verifies that
// observing two peer KindValues with valid L0Witnesses on distinct V's
// from the same leader causes retention to grow to 2 via the harvest
// path → Rule 2 (leader equivocation) fires. Rule 5 must NOT fire
// (the framing-attack guard applies symmetrically to second-witness
// observation).
func TestObserveValueMsg_HarvestSecondDistinctVFiresRule2(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Build two distinct byz-equivocating bundles (the byz leader signs
	// L0Witnesses on both V_a and V_b — direct signer access bypasses
	// the σ-lock).
	bA := s.buildByzEquivocatingBundle(leader, 0, Value("V_a"))
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	// Craft two peer KindValues from op 2 and op 3 forwarding the byz
	// leader's L0Witnesses on V_a and V_b respectively. (In reality
	// these would be op2/op3's own Phase-2a emissions after they each
	// retained a different bundle; we just need the wire shape.)
	vmA := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V_a"),
		ValueRoot:    ValueRoot(Value("V_a")),
		L0Witness:    bA.L0Witness,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	vmB := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   3,
		Height:       s.cfg.Height,
		V:            Value("V_b"),
		ValueRoot:    ValueRoot(Value("V_b")),
		L0Witness:    bB.L0Witness,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	op4 := s.instances[OperatorID(4)]
	require.NoError(t, op4.ObserveValueMsg(vmA))
	require.NoError(t, op4.ObserveValueMsg(vmB))
	require.Len(t, op4.RetainedBundles(0, leader), 2,
		"two distinct harvests should grow retention to 2")
	var foundRule2, foundRule5 bool
	for _, e := range op4.Evidence() {
		if e.Rule == EvidenceLeaderEquivocation {
			foundRule2 = true
		}
		if e.Rule == EvidenceFakePlaintextSigma {
			foundRule5 = true
		}
	}
	require.True(t, foundRule2,
		"Op11: harvest of second distinct V must fire Rule 2 (leader equivocation)")
	require.False(t, foundRule5,
		"Op11: harvest path must NOT fire Rule 5 (anti-framing guarantee)")
}

// TestObserveValueMsg_HarvestThenDirectConvergesToSameState verifies the
// docstring claim that direct-then-harvest and harvest-then-direct
// converge to the same retention state. The two paths populate
// retainedBundles via different routes (peer KindValue vs ObservePhase1Bundle);
// the dedup-by-Value check inside retainPhase1Bundle should ensure
// idempotent convergence.
func TestObserveValueMsg_HarvestThenDirectConvergesToSameState(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Build a Phase-1 bundle from the leader.
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	// Craft op2's KindValue forwarding the same L0Witness (as op2 would
	// have done after retaining the bundle).
	vm := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V0"),
		ValueRoot:    ValueRoot(Value("V0")),
		L0Witness:    b.L0Witness,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	// Path A: harvest-then-direct at op3.
	op3 := s.instances[OperatorID(3)]
	require.NoError(t, op3.ObserveValueMsg(vm))
	require.Len(t, op3.RetainedBundles(0, leader), 1)
	require.NoError(t, op3.ObservePhase1Bundle(b, observedAfterPhase2a))
	require.Len(t, op3.RetainedBundles(0, leader), 1,
		"direct after harvest must dedup (same V)")
	// Path B: direct-then-harvest at op4.
	op4 := s.instances[OperatorID(4)]
	require.NoError(t, op4.ObservePhase1Bundle(b, observedEarly))
	require.Len(t, op4.RetainedBundles(0, leader), 1)
	require.NoError(t, op4.ObserveValueMsg(vm))
	require.Len(t, op4.RetainedBundles(0, leader), 1,
		"harvest after direct must dedup (same V)")
	// Final retained V should match across both paths.
	require.Equal(t, op3.RetainedBundles(0, leader)[0].Bundle.Value,
		op4.RetainedBundles(0, leader)[0].Bundle.Value)
}

// TestObserveValueMsg_HarvestAtRetentionCapSilentDrop verifies that when
// retention is already at the 2-V cap, a third harvest attempt for a
// distinct V silent-drops (the retainPhase1Bundle len >= 2 branch).
func TestObserveValueMsg_HarvestAtRetentionCapSilentDrop(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Seed op4's retention with two distinct V's via direct equivocation.
	bA := s.buildByzEquivocatingBundle(leader, 0, Value("V_a"))
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	op4 := s.instances[OperatorID(4)]
	require.NoError(t, op4.ObservePhase1Bundle(bA, observedEarly))
	require.NoError(t, op4.ObservePhase1Bundle(bB, observedEarly))
	require.Len(t, op4.RetainedBundles(0, leader), 2, "precondition: retention at 2-V cap")
	beforeEvidence := len(op4.Evidence())
	// Now craft a peer KindValue with a third distinct V.
	bC := s.buildByzEquivocatingBundle(leader, 0, Value("V_c"))
	vmC := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   3,
		Height:       s.cfg.Height,
		V:            Value("V_c"),
		ValueRoot:    ValueRoot(Value("V_c")),
		L0Witness:    bC.L0Witness,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op4.ObserveValueMsg(vmC))
	require.Len(t, op4.RetainedBundles(0, leader), 2,
		"harvest at retention cap must silent-drop (no third distinct V retained)")
	// No NEW evidence should fire on the silently-dropped harvest.
	require.Equal(t, beforeEvidence, len(op4.Evidence()),
		"silently-dropped harvest must not add new evidence")
}

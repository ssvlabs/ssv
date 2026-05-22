package twoab

import (
	"fmt"
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
// builder forwards the L_0 leader's L_0 witness from the retained Phase-1
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
	require.NotEmpty(t, l0WitnessOf(vm),
		"Op11: KindValue must carry the L_0 leader's witness forwarded from the retained bundle")
	// The forwarded witness should match what the leader signs on V_0.
	expected, err := s.instances[leader].signer.SignPartial(Value("V0"))
	require.NoError(t, err)
	require.Equal(t, expected, l0WitnessOf(vm),
		"forwarded L_0 witness must equal the leader's σ partial bytes (deterministic stub signer)")
}

// TestMaybeBuildAndBroadcastUpgrade_PopulatesL0Witness verifies that the
// A1 upgrade path also forwards L_0 witness from the retained bundle.
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
	require.NotEmpty(t, l0WitnessOf(upgrade),
		"Op11: A1 upgrade KindValue must also carry the leader's L_0 witness")
}

// TestObserveValueMsg_HarvestSeedsRetentionAndSigmaPool verifies the core
// Op11 harvest: a V-drop receiver observing a peer KindValue with a
// verifying L_0 witness establishes retention[0][leader] = {V} AND seeds
// σ-pool[0][V_root][leader] with the leader's partial (effectively as
// if the receiver had observed the leader's Phase-1 bundle directly).
func TestObserveValueMsg_HarvestSeedsRetentionAndSigmaPool(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// V-recipient (op2) gets the bundle; V-drops (op3, op4) do NOT.
	s.deliverPhase1(0, Value("V0"), []OperatorID{leader, OperatorID(2)}, observedEarly)
	s.applyHostValidityFor([]OperatorID{leader, OperatorID(2)}, 0, Value("V0"), true)
	// op2 fires KindValue with the leader's L_0 witness forwarded.
	vm, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vm)
	// op3 (V-drop) observes op2's KindValue → harvests V.
	op3 := s.instances[OperatorID(3)]
	require.Empty(t, op3.RetainedBundles(0, leader), "op3 has no Phase-1 bundle before harvest")
	require.NoError(t, op3.ObserveValueMsg(vm))
	require.Len(t, op3.RetainedBundles(0, leader), 1,
		"Op11: peer KindValue with valid L_0 witness should harvest V into retention")
	require.Equal(t, Value("V0"), op3.RetainedBundles(0, leader)[0].Bundle.Value)
	root := ValueRoot(Value("V0"))
	require.NotEmpty(t, op3.sigmaPool[0][root][leader],
		"Op11: σ-pool[V_0][leader] should be seeded with the forwarded L_0 witness")
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
// anti-framing guarantee: a peer KindValue with a bogus L_0 witness does
// not harvest V into retention, does not seed σ-pool, and does NOT
// enqueue a validation request OR fire Rule 5 (the leader would be
// falsely accused if Rule 5 fired on the emitter-signed envelope).
func TestObserveValueMsg_FakeL0WitnessSilentlyDiscarded(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Forge a KindValue from op 2 with V claim but bogus L_0 witness bytes.
	// L0Partial is also bogus (different attribution — Rule 5 will fire
	// against op 2 for the bad self-partial; the L_0 witness verify is the
	// path we want to exercise; Rule 5 firing for emitter L0Partial is
	// orthogonal and won't be checked here).
	bogus := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V_fake"),
		ValueRoot:    ValueRoot(Value("V_fake")),
		Witnesses:    l0Witness(Value("V_fake"), Signature{0xde, 0xad, 0xbe, 0xef}),
		L0Partial:    Signature{0xfe, 0xed},
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	op3 := s.instances[OperatorID(3)]
	require.NoError(t, op3.ObserveValueMsg(bogus))
	require.Empty(t, op3.RetainedBundles(0, leader),
		"bogus L_0 witness must not harvest V into retention")
	select {
	case req := <-op3.WantsHostValidationCh():
		t.Fatalf("bogus L_0 witness must not enqueue validation (got %+v)", req)
	default:
	}
	// Anti-framing guarantee: Rule 5 must NOT fire against the LEADER.
	// (Post Op5, Rule 5 may fire against the EMITTER for the bogus
	// L0Partial — that's correct behavior since L0Partial is the
	// emitter's own signing artifact, not a forwarded leader artifact.)
	for _, e := range op3.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma {
			require.NotEqualf(t, leader, e.OperatorID,
				"bogus L_0 witness in peer KindValue must NOT fire Rule 5 against the LEADER (framing-attack guard)")
		}
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

// TestRequestHostValidation_BufferFullRollsBackPendingFlag verifies the
// buffer-full rollback semantic: when wantsHostValidationCh is full and
// a new enqueue hits the `default` branch in select, the helper rolls
// back the pendingValidation flag for that (layer, V_root). Without the
// rollback, the (layer, V) pair would be permanently silenced — the
// dedup-by-in-flight check would skip every subsequent harvest, but the
// drop-on-full prevented the channel from ever receiving the request.
// Load-bearing for "later harvest retries succeed" behavior under
// transient buffer-full conditions.
func TestRequestHostValidation_BufferFullRollsBackPendingFlag(t *testing.T) {
	s := newSim(t, 4)
	op := s.instances[OperatorID(1)]
	K := op.Config().K()
	// Fill the channel to capacity (the buffer is K-deep). Use distinct
	// (layer, V) pairs across the K layers so the dedup-by-(layer, V_root)
	// doesn't suppress any of them.
	for k := 0; k < K; k++ {
		op.requestHostValidation(k, Value(fmt.Sprintf("V%d", k)))
	}
	// Sanity-check the introspection: K pending entries summed across
	// all layers.
	require.Equal(t, K, op.Stats().PendingValidationCount,
		"after K successful enqueues, K (layer, V_root) pairs should be pending")
	// Channel is now full. Subsequent enqueue must hit the `default`
	// branch and roll back its pendingValidation entry.
	const droppedLayer = 0
	droppedV := Value("V-dropped")
	op.requestHostValidation(droppedLayer, droppedV)
	// Inspect the rollback through the public introspection API: the
	// pending count should remain K — the rollback erased the just-set
	// flag for the dropped (layer, V) pair, leaving only the original
	// K pending entries.
	require.Equal(t, K, op.Stats().PendingValidationCount,
		"buffer-full path MUST roll back the pendingValidation flag so a later harvest can re-attempt; pending count should not grow on the drop")
	// Drain ALL slots from the channel so the re-attempt lands cleanly
	// in an empty channel (avoiding FIFO ordering noise with the
	// earlier fill).
	for k := 0; k < K; k++ {
		<-op.WantsHostValidationCh()
	}
	// Now re-attempt the dropped request — it should succeed (and
	// enqueue) because the rollback left the dedup state in a re-
	// attemptable position.
	op.requestHostValidation(droppedLayer, droppedV)
	select {
	case req := <-op.WantsHostValidationCh():
		require.Equal(t, droppedLayer, req.Layer)
		require.Equal(t, droppedV, req.Value)
	default:
		t.Fatal("after rollback + headroom freed, re-attempt MUST enqueue")
	}
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

// TestFinalize_RefusesMutators verifies that after Finalize is called,
// every state-mutating public Instance method returns ErrInstanceEnded
// and does not mutate internal state. Mirrors
// `base/instance_test.go::TestObft_Finalize_RefusesMutators`; load-bearing
// for the Phase 3 convergence (twoab adopting base's lifecycle-guard
// pattern uniformly across all mutators).
//
// Excludes BuildCertificate — that method is a pure value-constructor
// without an i.ended guard (matches base); it's expected to keep
// working post-Finalize.
func TestFinalize_RefusesMutators(t *testing.T) {
	s := newSim(t, 4)
	receiver := s.instances[OperatorID(3)]
	leaderID := s.leaderAt(0)
	leaderInst := s.instances[leaderID]

	// Build a peer bundle + ValueMsg + Commit pre-Finalize so we can
	// replay them post-Finalize. Sourced from non-receiver instances to
	// avoid mutating receiver state during setup.
	bundle, err := leaderInst.BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	src2 := s.instances[OperatorID(2)]
	require.NoError(t, src2.ObservePhase1Bundle(bundle, observedEarly))
	require.NoError(t, src2.ApplyHostValidity(0, Value("V0"), true))
	srcValueMsg, _, _, err := src2.MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, srcValueMsg)

	// Snapshot pre-Finalize observables via the public Stats() API.
	preStats := receiver.Stats()
	preEv := append([]Evidence{}, receiver.Evidence()...)

	receiver.Finalize()
	require.True(t, receiver.Ended())

	// Each mutator must return ErrInstanceEnded.
	_, err = receiver.BuildPhase1Bundle(0, Value("V0"))
	require.ErrorIs(t, err, ErrInstanceEnded, "BuildPhase1Bundle")
	require.ErrorIs(t, receiver.ObservePhase1Bundle(bundle, observedEarly),
		ErrInstanceEnded, "ObservePhase1Bundle")
	require.ErrorIs(t, receiver.ApplyHostValidity(0, Value("V0"), true),
		ErrInstanceEnded, "ApplyHostValidity")
	_, _, _, err = receiver.MaybeFirePhase2a()
	require.ErrorIs(t, err, ErrInstanceEnded, "MaybeFirePhase2a")
	_, err = receiver.MaybeBuildAndBroadcastUpgrade()
	require.ErrorIs(t, err, ErrInstanceEnded, "MaybeBuildAndBroadcastUpgrade")
	_, err = receiver.MaybeBuildAndBroadcastCommit()
	require.ErrorIs(t, err, ErrInstanceEnded, "MaybeBuildAndBroadcastCommit")
	require.ErrorIs(t, receiver.ObserveValueMsg(srcValueMsg),
		ErrInstanceEnded, "ObserveValueMsg")
	require.ErrorIs(t, receiver.ObserveNoValueMsg(&NoValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}), ErrInstanceEnded, "ObserveNoValueMsg")
	require.ErrorIs(t, receiver.ObserveCommit(&Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  Signature{0x11, 0x22, 0x33},
	}), ErrInstanceEnded, "ObserveCommit")
	_, err = receiver.Resolve()
	require.ErrorIs(t, err, ErrInstanceEnded, "Resolve")
	require.ErrorIs(t, receiver.ObserveCertificate(&Certificate{
		ClusterID: s.cfg.ClusterID,
		Height:    s.cfg.Height,
		Value:     Value("V0"),
		Signature: Signature{0xaa},
	}), ErrInstanceEnded, "ObserveCertificate")

	// State must be unchanged. Verify via the public Stats() snapshot
	// + Evidence() — neither should grow post-Finalize.
	postStats := receiver.Stats()
	require.Equal(t, preStats.PendingValidationCount, postStats.PendingValidationCount,
		"PendingValidationCount must not change")
	require.Equal(t, preStats.VerifiedWitnessesCount, postStats.VerifiedWitnessesCount,
		"VerifiedWitnessesCount must not change")
	require.Equal(t, preStats.EvidenceCount, postStats.EvidenceCount,
		"EvidenceCount must not change")
	require.True(t, postStats.Ended, "Ended must be true post-Finalize")
	require.Equal(t, len(preEv), len(receiver.Evidence()),
		"Evidence accumulator must not grow")

	// BuildCertificate is intentionally NOT guarded (pure value-
	// constructor, no Instance mutation). Verify it still succeeds with
	// a valid Output so the API contract holds post-Finalize.
	cert, err := receiver.BuildCertificate(&Output{
		Layer:     0,
		Value:     Value("V0"),
		Signature: Signature{0xbb},
	})
	require.NoError(t, err, "BuildCertificate must remain callable post-Finalize")
	require.NotNil(t, cert)
}

// countingSigner wraps a real Signer and counts VerifyPartial calls
// (filtered by a matcher predicate so we can isolate the L_0 witness
// verifies from incidental L0Partial verifies that happen on the same
// peer KindValue). Used by TestObserveValueMsg_HarvestCacheDedupsVerify.
type countingSigner struct {
	inner    Signer
	matchMsg func(msg []byte) bool
	count    int
}

func (c *countingSigner) SignPartial(msg []byte) (Signature, error) {
	return c.inner.SignPartial(msg)
}

func (c *countingSigner) AggregatePartials(partials map[OperatorID]Signature) (Signature, error) {
	return c.inner.AggregatePartials(partials)
}

func (c *countingSigner) VerifyPartial(pubKeyShare []byte, msg []byte, partial Signature) bool {
	if c.matchMsg(msg) {
		c.count++
	}
	return c.inner.VerifyPartial(pubKeyShare, msg, partial)
}

func (c *countingSigner) VerifyAggregate(clusterPubKey []byte, msg []byte, sig Signature) bool {
	return c.inner.VerifyAggregate(clusterPubKey, msg, sig)
}

// TestObserveValueMsg_HarvestCacheDedupsVerify locks in the Op11
// verify-cost dedup: when multiple peers forward the same V (with the
// same leader L_0 witness inside their KindValues), the receiver verifies
// the L_0 witness exactly ONCE via BLS, then short-circuits subsequent
// observations via verifiedWitnesses[layer][V_root]. Without the
// cache, n peers re-flooding the same V would each trigger a fresh BLS
// partial-verify (~1ms in production) on the receiver, stacking against
// the slot budget.
//
// Setup: build a custom op-3 instance whose signer is wrapped with a
// counter matching the (V, leaderID) verify message. Observe two
// distinct peer KindValues from op-2 and op-4 both carrying the same V
// + leader L_0 witness. Assert exactly one matching verify call.
func TestObserveValueMsg_HarvestCacheDedupsVerify(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Seed leader + op-2 + op-4 with V_0 directly; op-3 is the V-drop
	// receiver that will harvest V via the peer-KindValue path.
	v0 := Value("V0")
	s.deliverPhase1(0, v0, []OperatorID{leader, OperatorID(2), OperatorID(4)}, observedEarly)
	s.applyHostValidityFor([]OperatorID{leader, OperatorID(2), OperatorID(4)}, 0, v0, true)
	vm2, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	vm4, _, _, err := s.instances[OperatorID(4)].MaybeFirePhase2a()
	require.NoError(t, err)

	// Build a fresh op-3 instance with a counting signer wrapping the
	// stub. The counter matches the (V, op-id) verify path that Op11's
	// L_0 witness check exercises.
	v0Bytes := []byte(v0)
	innerSigner := NewStubSigner(s.cfg.QV(), s.pubShares[OperatorID(3)])
	counter := &countingSigner{
		inner:    innerSigner,
		matchMsg: func(msg []byte) bool { return string(msg) == string(v0Bytes) },
	}
	op3, err := NewInstance(
		s.cfg, OperatorID(3),
		counter,
		NewStubSigner(s.cfg.QV(), s.pubShares[OperatorID(3)]),
		NewStubIBE(s.cfg.QEnc()),
		[]byte{0xCC, 0xDD}, // clusterPubKey placeholder (stub ignores value)
		s.pubShares,
		nil,
		nil,
	)
	require.NoError(t, err)

	// First observation: cache cold, real verify runs.
	require.NoError(t, op3.ObserveValueMsg(vm2))
	require.Len(t, op3.RetainedBundles(0, leader), 1,
		"first harvest must establish retention")
	firstCount := counter.count

	// Second observation: cache hit MUST short-circuit the L_0 witness
	// verify. The verify call counter should not increment (modulo any
	// L0Partial verify, which happens with msg = V_0 too — so we'll
	// quantify the structural minimum).
	require.NoError(t, op3.ObserveValueMsg(vm4))
	require.Len(t, op3.RetainedBundles(0, leader), 1,
		"second harvest dedups against the first (same V); retention stays at 1")
	// Net new verify calls between the two observations:
	//   - Op2 path (first obs):  1 L_0 witness verify + 1 L0Partial verify = 2
	//   - Op4 path (second obs): 0 L_0 witness verify (cache hit) +
	//                            1 L0Partial verify (different signer/share) = 1
	// So secondDelta should be exactly 1 (the L0Partial verify), NOT 2.
	// Without the cache, secondDelta would be 2.
	secondDelta := counter.count - firstCount
	require.Equal(t, 1, secondDelta,
		"Op11 verify-cost dedup: second observation should incur exactly 1 verify (L0Partial; L_0 witness short-circuits via cache). Got %d total, %d after first observation, delta %d",
		counter.count, firstCount, secondDelta)
}

// TestObserveValueMsg_HarvestSecondDistinctVFiresRule2 verifies that
// observing two peer KindValues with valid L_0 witnesses on distinct V's
// from the same leader causes retention to grow to 2 via the harvest
// path → Rule 2 (leader equivocation) fires. Rule 5 must NOT fire
// (the framing-attack guard applies symmetrically to second-witness
// observation).
func TestObserveValueMsg_HarvestSecondDistinctVFiresRule2(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Build two distinct byz-equivocating bundles (the byz leader signs
	// L_0 witnesses on both V_a and V_b — direct signer access bypasses
	// the σ-lock).
	bA := s.buildByzEquivocatingBundle(leader, 0, Value("V_a"))
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	// Craft two peer KindValues from op 2 and op 3 forwarding the byz
	// leader's L_0 witnesses on V_a and V_b respectively. (In reality
	// these would be op2/op3's own Phase-2a emissions after they each
	// retained a different bundle; we just need the wire shape.)
	// Real σ partials from emitters (op2, op3) on their respective V's
	// so ValidateValueMsg's L0Partial-non-empty + Op5 verify-pool logic
	// at the receiver works. Note: with real-signing partials, Rule 5
	// will NOT fire against op2/op3 (they actually signed). The test
	// asserts Rule 2 fires (leader equivocation) and Rule 5 does NOT
	// fire (anti-framing guarantee on L_0 witness verify failure path is
	// preserved — leader equivocation is via the L_0 witnesses, but the
	// L_0 witness verifies because the byz leader DID sign both).
	op2Sig, err := s.instances[OperatorID(2)].signer.SignPartial(Value("V_a"))
	require.NoError(s.t, err)
	op3Sig, err := s.instances[OperatorID(3)].signer.SignPartial(Value("V_b"))
	require.NoError(s.t, err)
	vmA := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V_a"),
		ValueRoot:    ValueRoot(Value("V_a")),
		Witnesses:    l0Witness(Value("V_a"), bA.LWitness),
		L0Partial:    op2Sig,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	vmB := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   3,
		Height:       s.cfg.Height,
		V:            Value("V_b"),
		ValueRoot:    ValueRoot(Value("V_b")),
		Witnesses:    l0Witness(Value("V_b"), bB.LWitness),
		L0Partial:    op3Sig,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	op4 := s.instances[OperatorID(4)]
	require.NoError(t, op4.ObserveValueMsg(vmA))
	require.NoError(t, op4.ObserveValueMsg(vmB))
	require.Len(t, op4.RetainedBundles(0, leader), 2,
		"two distinct harvests should grow retention to 2")
	var foundRule2, foundRule5OnLeader bool
	for _, e := range op4.Evidence() {
		if e.Rule == EvidenceLeaderEquivocation {
			foundRule2 = true
		}
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == leader {
			foundRule5OnLeader = true
		}
	}
	require.True(t, foundRule2,
		"Op11: harvest of second distinct V must fire Rule 2 (leader equivocation)")
	require.False(t, foundRule5OnLeader,
		"Op11: harvest path must NOT fire Rule 5 against the leader (anti-framing guarantee)")
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
	// Craft op2's KindValue forwarding the same L_0 witness (as op2 would
	// have done after retaining the bundle).
	op2Sig, err := s.instances[OperatorID(2)].signer.SignPartial(Value("V0"))
	require.NoError(t, err)
	vm := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V0"),
		ValueRoot:    ValueRoot(Value("V0")),
		Witnesses:    l0Witness(Value("V0"), b.LWitness),
		L0Partial:    op2Sig,
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
	op3Sig, err := s.instances[OperatorID(3)].signer.SignPartial(Value("V_c"))
	require.NoError(t, err)
	vmC := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   3,
		Height:       s.cfg.Height,
		V:            Value("V_c"),
		ValueRoot:    ValueRoot(Value("V_c")),
		Witnesses:    l0Witness(Value("V_c"), bC.LWitness),
		L0Partial:    op3Sig,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op4.ObserveValueMsg(vmC))
	require.Len(t, op4.RetainedBundles(0, leader), 2,
		"harvest at retention cap must silent-drop (no third distinct V retained)")
	// No NEW evidence should fire on the silently-dropped harvest. (The
	// L0Partial verify+pool still runs for the emitter — op 3 — but
	// that's a separate path that doesn't add EVIDENCE; the assertion
	// is that no NEW Rule-firing fires from the harvest itself.)
	gotEvidence := op4.Evidence()
	require.Equal(t, beforeEvidence, len(gotEvidence),
		"silently-dropped harvest must not add new evidence")
}

// TestRetentionSource_DirectPath verifies that a bundle retained via
// ObservePhase1Bundle (the direct gossipsub Phase-1 channel) carries
// Source = RetentionDirect.
func TestRetentionSource_DirectPath(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	op := s.instances[OperatorID(3)]
	require.NoError(t, op.ObservePhase1Bundle(b, observedEarly))
	retained := op.RetainedBundles(0, leader)
	require.Len(t, retained, 1)
	require.Equal(t, RetentionDirect, retained[0].Source,
		"direct Phase-1 bundle observation must produce Source=RetentionDirect")
}

// TestRetentionSource_HarvestPath verifies that a bundle retained via
// Op11 peer-reflood-V harvest (synthesized inside ObserveValueMsg)
// carries Source = RetentionHarvest.
func TestRetentionSource_HarvestPath(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Seed op2 with V0 directly so its KindValue forwards a real
	// leader L_0 witness.
	s.deliverPhase1(0, Value("V0"), []OperatorID{leader, OperatorID(2)}, observedEarly)
	s.applyHostValidityFor([]OperatorID{leader, OperatorID(2)}, 0, Value("V0"), true)
	vm, _, _, err := s.instances[OperatorID(2)].MaybeFirePhase2a()
	require.NoError(t, err)
	op3 := s.instances[OperatorID(3)]
	require.NoError(t, op3.ObserveValueMsg(vm))
	retained := op3.RetainedBundles(0, leader)
	require.Len(t, retained, 1, "harvest path must establish retention")
	require.Equal(t, RetentionHarvest, retained[0].Source,
		"harvest-path retention must produce Source=RetentionHarvest")
}

// TestRetentionSource_OrderDependence locks in the documented "FIRST
// observation wins" semantic: the Source field on a retainedBundle
// reflects which path observed the (leader, V) FIRST, regardless of
// whether the other path later observes the same V (and silently
// dedups). This is a deliberate property of the dedup-by-V check in
// retainPhase1Bundle.
func TestRetentionSource_OrderDependence(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	b, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	op2Sig, err := s.instances[OperatorID(2)].signer.SignPartial(Value("V0"))
	require.NoError(t, err)
	vm := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V0"),
		ValueRoot:    ValueRoot(Value("V0")),
		Witnesses:    l0Witness(Value("V0"), b.LWitness),
		L0Partial:    op2Sig,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}

	// Path A: direct-then-harvest at op3. Direct arrives first; harvest
	// dedups silently. Source MUST stay RetentionDirect.
	op3 := s.instances[OperatorID(3)]
	require.NoError(t, op3.ObservePhase1Bundle(b, observedEarly))
	require.NoError(t, op3.ObserveValueMsg(vm))
	retA := op3.RetainedBundles(0, leader)
	require.Len(t, retA, 1)
	require.Equal(t, RetentionDirect, retA[0].Source,
		"direct-then-harvest: Source reflects FIRST observation (Direct)")

	// Path B: harvest-then-direct at op4. Harvest arrives first; direct
	// dedups silently. Source MUST stay RetentionHarvest. Note: the
	// runner's mcache may still hold the direct envelope from path B's
	// later ObservePhase1Bundle call — but the Instance's Source field
	// reflects how it FIRST learned of the (leader, V), not which paths
	// have the envelope downstream.
	op4 := s.instances[OperatorID(4)]
	require.NoError(t, op4.ObserveValueMsg(vm))
	require.NoError(t, op4.ObservePhase1Bundle(b, observedAfterPhase2a))
	retB := op4.RetainedBundles(0, leader)
	require.Len(t, retB, 1)
	require.Equal(t, RetentionHarvest, retB[0].Source,
		"harvest-then-direct: Source reflects FIRST observation (Harvest)")
}

// TestLeaderEquivocationEvidence_SurfacesSourcePerBundle verifies that
// when Rule 2 fires on two distinct V's reaching retention via
// different paths, the resulting LeaderEquivocationEvidence carries
// SourceA / SourceB reflecting each bundle's actual arrival path.
func TestLeaderEquivocationEvidence_SurfacesSourcePerBundle(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// First V via direct path.
	bA := s.buildByzEquivocatingBundle(leader, 0, Value("V_a"))
	// Second V via harvest path: build a peer's KindValue forwarding the
	// byz leader's L_0 witness on V_b.
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	op2Sig, err := s.instances[OperatorID(2)].signer.SignPartial(Value("V_b"))
	require.NoError(t, err)
	vmB := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   2,
		Height:       s.cfg.Height,
		V:            Value("V_b"),
		ValueRoot:    ValueRoot(Value("V_b")),
		Witnesses:    l0Witness(Value("V_b"), bB.LWitness),
		L0Partial:    op2Sig,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	op4 := s.instances[OperatorID(4)]
	// Direct first → V_a.
	require.NoError(t, op4.ObservePhase1Bundle(bA, observedEarly))
	// Harvest second → V_b → triggers Rule 2.
	require.NoError(t, op4.ObserveValueMsg(vmB))
	require.Len(t, op4.RetainedBundles(0, leader), 2)

	var rule2 *LeaderEquivocationEvidence
	for _, e := range op4.Evidence() {
		if e.Rule == EvidenceLeaderEquivocation {
			rule2 = e.LeaderEquivocation
			break
		}
	}
	require.NotNil(t, rule2, "Rule 2 must fire on the second distinct V")
	require.Equal(t, RetentionDirect, rule2.SourceA,
		"SourceA reflects bundle A's arrival path (direct, observed first)")
	require.Equal(t, RetentionHarvest, rule2.SourceB,
		"SourceB reflects bundle B's arrival path (harvest, observed second)")
}

// ---------- Op8 σ-lock-aware buildLayerEntry ----------

// TestBuildLayerEntry_LeaderStaysSigmaOnHostFlip verifies the Op8
// σ-lock-aware branch in buildLayerEntry: an L_k leader that σ-locked at
// witness-sign time (BuildPhase1Bundle) stays σ-committed — it emits
// SigmaChained on the locked V even if the host later flips that value
// invalid. Flipping to NR would publish both a σ witness (Phase-1 bundle)
// and an NR partial at the same layer = self-equivocation. This mirrors the
// post-Op5 L_0 principle that a host re-validate-NV verdict has no protocol
// effect once the op is σ-locked.
func TestBuildLayerEntry_LeaderStaysSigmaOnHostFlip(t *testing.T) {
	s := newSimWithK(t, 7, 3)
	const k = 1
	leader := s.leaderAt(k)
	v := s.candidates[k]
	inst := s.instances[leader]
	// Leader builds its layer-k bundle → σ-locks layer k on V_k.
	_, err := inst.BuildPhase1Bundle(k, v)
	require.NoError(t, err)
	require.True(t, inst.sigmaLocked[k], "BuildPhase1Bundle at k must σ-lock")
	// Host flips V_k invalid at the leader (e.g. mid-slot re-evaluation).
	require.NoError(t, inst.ApplyHostValidity(k, v, false))
	// buildLayerEntry must STILL emit SigmaChained on V_k (not NRPlaintext).
	entry, err := inst.buildLayerEntry(k)
	require.NoError(t, err)
	require.Equal(t, LayerEntrySigmaChained, entry.Kind,
		"Op8: σ-locked leader must stay σ-side at layer k despite host-flip-invalid")
	require.Equal(t, v, entry.V)
}

// ---------- Op12 per-layer forwarded witnesses ----------

// TestMaybeFirePhase2a_ForwardsDeeperLayerWitness verifies the Op12 builder:
// an op σ-side at both L_0 and L_1 emits a KindValue whose Witnesses[] carries
// BOTH the L_0 and L_1 leader witnesses (the deeper one rides alongside its
// SigmaChained entry).
func TestMaybeFirePhase2a_ForwardsDeeperLayerWitness(t *testing.T) {
	s := newSimWithK(t, 7, 3) // K=3 → layer 1 exists
	op := OperatorID(4)
	v0, v1 := s.candidates[0], s.candidates[1]
	// Deliver L_0 + L_1 bundles to op and validate both → op is σ at both.
	s.deliverPhase1(0, v0, []OperatorID{op}, observedEarly)
	s.deliverPhase1(1, v1, []OperatorID{op}, observedEarly)
	require.NoError(t, s.instances[op].ApplyHostValidity(0, v0, true))
	require.NoError(t, s.instances[op].ApplyHostValidity(1, v1, true))

	vm, _, _, err := s.instances[op].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vm)

	var haveL0, haveL1 bool
	for _, w := range vm.Witnesses {
		switch w.Layer {
		case 0:
			haveL0 = true
			require.Equal(t, ValueRoot(v0), w.ValueRoot)
		case 1:
			haveL1 = true
			require.Equal(t, ValueRoot(v1), w.ValueRoot, "Op12: L_1 witness root must match V_1 (rides with the SigmaChained entry)")
		}
	}
	require.True(t, haveL0, "KindValue must forward the L_0 witness")
	require.True(t, haveL1, "Op12: KindValue must forward the L_1 witness when σ-at-L_1")
}

// TestObserveValueMsg_HarvestsDeeperLayerWitness verifies the Op12 harvest: a
// receiver that missed the L_1 bundle seeds σ-pool[1][V_1][L_1-leader] from a
// peer KindValue's Layer-1 witness (V_1 recovered from the colocated
// SigmaChained entry — "root + σ-colocation").
func TestObserveValueMsg_HarvestsDeeperLayerWitness(t *testing.T) {
	s := newSimWithK(t, 7, 3)
	const k = 1
	l0Leader, l1Leader := s.leaderAt(0), s.leaderAt(k)
	v0, v1 := s.candidates[0], s.candidates[k]

	// Real leader witnesses (so they BLS-verify at the receiver).
	w0, err := s.instances[l0Leader].signer.SignPartial(v0)
	require.NoError(t, err)
	w1, err := s.instances[l1Leader].signer.SignPartial(v1)
	require.NoError(t, err)

	emitter := OperatorID(3)
	emitterSig, err := s.instances[emitter].signer.SignPartial(v0)
	require.NoError(t, err)
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: emitter,
		Height:     s.cfg.Height,
		V:          v0,
		ValueRoot:  ValueRoot(v0),
		Witnesses: []LayerWitness{
			{Layer: 0, ValueRoot: ValueRoot(v0), Witness: w0},
			{Layer: k, ValueRoot: ValueRoot(v1), Witness: w1},
		},
		L0Partial: emitterSig,
		LayerEntries: []LayerEntry{
			{Layer: k, Kind: LayerEntrySigmaChained, V: v1, Payload: []byte("ct")},
			{Layer: 2, Kind: LayerEntryEmpty}, // K-1 entries required (K=3)
		},
	}

	recv := s.instances[OperatorID(5)] // missed the L_1 bundle
	require.NoError(t, recv.ObserveValueMsg(vm))

	require.NotEmpty(t, recv.sigmaPool[k][ValueRoot(v1)][l1Leader],
		"Op12: deeper-layer witness must seed σ-pool[k] with the L_k leader's partial")
	require.Len(t, recv.RetainedBundles(k, l1Leader), 1,
		"Op12: deeper-layer harvest must retain V_k under the L_k leader")
}

// TestObserveValueMsg_DeeperWitnessSkippedWithoutColocatedV verifies the
// graceful skip when a Layer-k witness has no colocated V (no matching
// SigmaChained entry) — the witness can't be verified, so it's silently
// dropped (defensive; an honest emitter never forwards such a witness).
func TestObserveValueMsg_DeeperWitnessSkippedWithoutColocatedV(t *testing.T) {
	s := newSimWithK(t, 7, 3)
	const k = 1
	l0Leader, l1Leader := s.leaderAt(0), s.leaderAt(k)
	v0, v1 := s.candidates[0], s.candidates[k]

	w0, err := s.instances[l0Leader].signer.SignPartial(v0)
	require.NoError(t, err)
	w1, err := s.instances[l1Leader].signer.SignPartial(v1)
	require.NoError(t, err)

	emitterSig, err := s.instances[OperatorID(3)].signer.SignPartial(v0)
	require.NoError(t, err)
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: OperatorID(3),
		Height:     s.cfg.Height,
		V:          v0,
		ValueRoot:  ValueRoot(v0),
		Witnesses: []LayerWitness{
			{Layer: 0, ValueRoot: ValueRoot(v0), Witness: w0},
			{Layer: k, ValueRoot: ValueRoot(v1), Witness: w1},
		},
		L0Partial: emitterSig,
		// No SigmaChained entry at layer k → no colocated V_1. (K-1 entries
		// still required for K=3.)
		LayerEntries: []LayerEntry{
			{Layer: k, Kind: LayerEntryEmpty},
			{Layer: 2, Kind: LayerEntryEmpty},
		},
	}

	recv := s.instances[OperatorID(5)]
	require.NoError(t, recv.ObserveValueMsg(vm))
	require.Empty(t, recv.sigmaPool[k][ValueRoot(v1)][l1Leader],
		"Op12: a deeper witness without a colocated V must be skipped (not poolable)")
	require.Empty(t, recv.RetainedBundles(k, l1Leader),
		"Op12: no colocated V → no deeper-layer retention")
}

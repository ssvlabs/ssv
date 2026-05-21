package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMaybeBuildAndBroadcastCommit_SigmaEligibilityFires: cluster reaches
// value_pool ≥ qV → σ-eligibility trigger fires → KindCommit-Signed.
func TestMaybeBuildAndBroadcastCommit_SigmaEligibilityFires(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	// After firePhase2aAll, all 4 ops should have emitted KindCommit-Signed.
	for _, op := range s.allOperators() {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok, "op %d should have emitted KindCommit", op)
		require.Equal(t, CommitSideSigned, c.Side, "op %d should be Signed side", op)
		require.Equal(t, Value("V0"), c.L0Value)
	}
}

// TestMaybeBuildAndBroadcastCommit_NREligibilityFiresWhenNoValuePathOpsConverge:
// h_V_honest=0 case — no op has V_0 → all emit KindNoValue → NR-eligibility
// fires for all → all emit KindCommit-NR.
func TestMaybeBuildAndBroadcastCommit_NREligibilityFiresOnAllNoValuePath(t *testing.T) {
	s := newSim(t, 4)
	// Nobody has V_0; nobody applies host validity.
	s.firePhase2aAll()
	for _, op := range s.allOperators() {
		c, ok := s.instances[op].OwnCommit()
		require.True(t, ok, "op %d should have emitted KindCommit", op)
		require.Equal(t, CommitSideNR, c.Side, "op %d should be NR side", op)
	}
}

// TestMaybeBuildAndBroadcastCommit_GateBlocksSigmaEligibleNRFire: the
// cannot-σ gate prevents a σ-eligible op from prematurely emitting
// KindCommit-NR via NR-eligibility. With V_0 + host valid, the op should
// wait for the σ-eligibility trigger, not the NR-eligibility trigger.
func TestMaybeBuildAndBroadcastCommit_GateBlocksSigmaEligibleNRFire(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 (L_0 leader) has V_0. Ops 2, 3, 4 are V-drops.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), true)
	// Op 1 fires Phase 2a → emits KindValue.
	vmA, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vmA)
	// Ops 2, 3, 4 fire Phase 2a → each emits KindNoValue.
	var nvs []*NoValueMsg
	for _, op := range []OperatorID{2, 3, 4} {
		_, nv, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
		require.NotNil(t, nv)
		nvs = append(nvs, nv)
	}
	// Cross-broadcast: Op 1 observes 3 KindNoValues. noValuePool reaches qEnc=3.
	for _, nv := range nvs {
		require.NoError(t, s.instances[OperatorID(1)].ObserveNoValueMsg(nv))
	}
	// Op 1 should NOT have emitted KindCommit-NR — the cannot-σ gate
	// blocks NR-eligibility for a σ-eligible op. Op 1 has V_local + host
	// valid → cannot-σ gate fails → no NR-eligibility fire.
	_, ok := s.instances[OperatorID(1)].OwnCommit()
	require.False(t, ok, "op 1 (σ-eligible) should wait, not fire NR commit")
}

// TestMaybeBuildAndBroadcastCommit_EquivocationFires: op observes ≥ 2
// distinct V_0 from L_0 leader → equivocation trigger fires →
// KindCommit-NR (A4 pivot if op had emitted KindValue).
func TestMaybeBuildAndBroadcastCommit_EquivocationFires(t *testing.T) {
	s := newSim(t, 4)
	// Op 1 emits KindValue on V_a. Then observes V_b — equivocation.
	s.deliverPhase1(0, Value("V_a"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V_a"), true)
	vm, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vm)
	// Now leader emits a second distinct bundle observed by op 1.
	leader := s.leaderAt(0)
	bB, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_b"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(1)].ObservePhase1Bundle(bB, observedAfterPhase2a))
	// Op 1's equivocation trigger fires → emits KindCommit-NR (A4 pivot).
	c, ok := s.instances[OperatorID(1)].OwnCommit()
	require.True(t, ok)
	require.Equal(t, CommitSideNR, c.Side)
}

// TestObserveValueMsg_FromPeerUpdatesValuePool.
func TestObserveValueMsg_FromPeerUpdatesValuePool(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{1}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1}, 0, Value("V0"), true)
	vm, _, _, err := s.instances[OperatorID(1)].MaybeFirePhase2a()
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(2)].ObserveValueMsg(vm))
	require.Equal(t, 1, s.instances[OperatorID(2)].valuePoolSize(0, ValueRoot(Value("V0"))))
}

// TestObserveValueMsg_DistinctFromSameOpFiresRule6a.
func TestObserveValueMsg_DistinctFromSameOpFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Craft two distinct KindValues from same op on different V's.
	vmA := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V_a"), ValueRoot: ValueRoot(Value("V_a")),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	vmB := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V_b"), ValueRoot: ValueRoot(Value("V_b")),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vmA))
	require.NoError(t, op2.ObserveValueMsg(vmB))
	// Rule 6a fires.
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a, "cross-V KindValue should fire Rule 6a")
}

// TestObserveCommit_CrossSideFiresRule1.
func TestObserveCommit_CrossSideFiresRule1(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Craft Signed + NR commits from same op.
	cSigned := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:      CommitSideSigned,
		L0Value:   Value("V0"),
		L0Partial: Signature{0x01}, // arbitrary
	}
	cNR := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:      CommitSideNR,
		L0Partial: Signature{0x02},
	}
	require.NoError(t, op2.ObserveCommit(cSigned))
	require.NoError(t, op2.ObserveCommit(cNR))
	var foundRule1, foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceCrossSigning {
			foundRule1 = true
		}
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule1, "cross-side commit should fire Rule 1")
	require.True(t, foundRule6a, "second distinct commit should also fire Rule 6a")
}

// TestObserveCommit_PostNRDirectAnyEmissionFiresRule6a.
//
// Per A8 (KindCommit-NRDirect is sole-emission per slot), any other
// Phase-2 emission from the same op is slashable. This holds regardless
// of gossipsub arrival order — the receiver detects the unauthorized
// pair on whichever message arrives second.
func TestObserveCommit_PostNRDirectAnyEmissionFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// NRDirect first.
	cNRDirect := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:         CommitSideNRDirect,
		L0Partial:    Signature{0x01},
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveCommit(cNRDirect))
	// Then a KindValue from same op — slashable per A8.
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V0"), ValueRoot: ValueRoot(Value("V0")),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a)
}

// TestObserveCommit_PreNRDirectValueMsgThenNRDirectFiresRule6a covers
// the gossipsub-reorder companion to PostNRDirect: KindValue observed
// FIRST, then NRDirect from the same op. Same slashable sequence (A8
// violation), different observation order. The detection lives in
// ObserveCommit's `Side==NRDirect && (hadValue != nil || hadNoValue !=
// nil)` branch.
func TestObserveCommit_PreNRDirectValueMsgThenNRDirectFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// KindValue first.
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V0"), ValueRoot: ValueRoot(Value("V0")),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	// Then NRDirect from same op — slashable per A8.
	cNRDirect := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:         CommitSideNRDirect,
		L0Partial:    Signature{0x01},
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveCommit(cNRDirect))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a)
}

// TestObserveValueMsg_A1UpgradeMismatchedLayerEntriesFiresRule6a: a byz
// emits KindNoValue with one set of L_k>0 entries and then an "upgrade"
// KindValue with DIFFERENT L_k>0 entries. Per spec §Phase 2a-late
// upgrade, the L_k entries MUST be identical across the pair. The
// mismatch fires Rule 6a; the upgrade's entries are NOT processed (the
// prior NoValueMsg's pool contributions stand, preventing cross-pool
// injection).
func TestObserveValueMsg_A1UpgradeMismatchedLayerEntriesFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Prior KindNoValue from op1 with L_1 = NRPlaintext.
	nv := &NoValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryEmpty},
		},
	}
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	// Crafted "upgrade" KindValue with L_1 = SigmaChained (mismatch).
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		V:          Value("V0"),
		ValueRoot:  ValueRoot(Value("V0")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntrySigmaChained, V: Value("V_b"), Payload: []byte("ct")},
		},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a, "mismatched L_k entries between NoValue and upgrade Value should fire Rule 6a")
}

// TestObserveNoValueMsg_ReorderMismatchedLayerEntriesFiresRule6a: same
// as above, but observation order is reversed (Value first, then
// NoValue arrives late with mismatched entries).
func TestObserveNoValueMsg_ReorderMismatchedLayerEntriesFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Upgrade KindValue arrives first.
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		V:          Value("V0"),
		ValueRoot:  ValueRoot(Value("V0")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntrySigmaChained, V: Value("V_b"), Payload: []byte("ct")},
		},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	// Then a mismatched KindNoValue arrives late.
	nv := &NoValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryEmpty},
		},
	}
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a, "reorder with mismatched L_k entries should also fire Rule 6a")
}

// TestObserveCommit_SignedCrossVAfterValueMsgFiresRule6a: byz emits
// KindValue(V_a), then KindCommit-Signed(V_b) with V_b ≠ V_a. Per A2,
// the σ-eligibility commit MUST be on the V the op claimed σ-eligibility
// on; cross-V is an unauthorized A1-A8 sequence. Should fire Rule 6a.
func TestObserveCommit_SignedCrossVAfterValueMsgFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		V:          Value("V_a"),
		ValueRoot:  ValueRoot(Value("V_a")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryEmpty},
		},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideSigned,
		L0Value:    Value("V_b"), // ≠ V_a
		L0Partial:  Signature("partial"),
	}
	require.NoError(t, op2.ObserveCommit(c))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a, "KindValue(V_a) → Commit-Signed(V_b) should fire Rule 6a")
}

// TestObserveValueMsg_PostCommitSignedCrossVFiresRule6a: same as above
// but reverse order — Commit-Signed(V_b) arrives first, then ValueMsg(V_a)
// where V_a ≠ V_b. Symmetric Rule 6a fire from the ObserveValueMsg side.
func TestObserveValueMsg_PostCommitSignedCrossVFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideSigned,
		L0Value:    Value("V_b"),
		L0Partial:  Signature("partial"),
	}
	require.NoError(t, op2.ObserveCommit(c))
	vm := &ValueMsg{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		V:          Value("V_a"), // ≠ V_b
		ValueRoot:  ValueRoot(Value("V_a")),
		LayerEntries: []LayerEntry{
			{Layer: 1, Kind: LayerEntryEmpty},
		},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a, "Commit-Signed(V_b) → ValueMsg(V_a) should fire Rule 6a")
}

// TestObserveCommit_FakePlaintextSigmaFiresRule5: a peer Commit-Signed
// carrying an L_0 σ partial that doesn't verify against the op's
// pubshare on the claimed V triggers Rule 5 (fake plaintext sigma at
// L_0). The op is still added to value_pool via inference (KindCommit-
// Signed implies a prior KindValue existed), but NOT to sigma-pool —
// the fake partial can't contribute to σ-quorum reconstruction.
func TestObserveCommit_FakePlaintextSigmaFiresRule5(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Craft a Commit-Signed from op 1 with garbage L0Partial.
	c := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideSigned,
		L0Value:    Value("V0"),
		L0Partial:  Signature("garbage-not-a-valid-partial"),
	}
	require.NoError(t, op2.ObserveCommit(c))
	var foundRule5 bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma {
			foundRule5 = true
			require.NotNil(t, e.FakePlaintextSigma)
			require.Equal(t, Value("V0"), e.FakePlaintextSigma.OnionValue)
		}
	}
	require.True(t, foundRule5, "fake L_0 σ partial should fire Rule 5")
}

// TestObserveCommit_PreNRDirectNoValueMsgThenNRDirectFiresRule6a is the
// NoValueMsg variant of the above — KindNoValue first, then NRDirect.
func TestObserveCommit_PreNRDirectNoValueMsgThenNRDirectFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// KindNoValue first.
	nv := &NoValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	// Then NRDirect from same op — slashable per A8.
	cNRDirect := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:         CommitSideNRDirect,
		L0Partial:    Signature{0x01},
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveCommit(cNRDirect))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a)
}

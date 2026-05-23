package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestMaybeFirePhase2a_HealthyClusterSigmaQuorumViaKindValue: every
// σ-eligible op emits KindValue at Phase 2a with its own L0Partial.
// No Phase-2b Commit follows on the σ-side (there is no σ-eligibility
// trigger). Cluster σ-pool[V_0] reaches qV via direct KindValue
// observations — a 1-hop cascade.
func TestMaybeFirePhase2a_HealthyClusterSigmaQuorumViaKindValue(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.firePhase2aAll()
	// Every op emitted KindValue with σ partial; no σ-side Commit fires.
	for _, op := range s.allOperators() {
		vm, ok := s.instances[op].OwnValueMsg()
		require.True(t, ok, "op %d should have emitted KindValue", op)
		require.NotEmpty(t, vm.L0Partial, "KindValue must carry the emitter's σ partial")
		require.Equal(t, Value("V0"), vm.V)
		_, hadCommit := s.instances[op].OwnCommit()
		require.False(t, hadCommit, "op %d should NOT emit Commit on σ-side", op)
	}
	// Each op's σ-pool[V_0] should contain qV partials via direct
	// KindValue observation (own + peers).
	root := ValueRoot(Value("V0"))
	for _, op := range s.allOperators() {
		count := len(s.instances[op].sigmaPool[0][root])
		require.GreaterOrEqual(t, count, s.cfg.QV(),
			"op %d σ-pool[V_0] should reach qV via direct KindValue observations", op)
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
// KindCommit-NR via NR-eligibility. With V_0 + host valid, the op is
// σ-side (it emits KindValue carrying its σ partial inline); the cannot-σ
// gate keeps it off the NR-eligibility path even after noValuePool reaches
// qEnc.
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

// TestEquivocationPostKindValue_NoRecovery: an op that has
// emitted KindValue (σ-locked at L_0) cannot pivot to NR on observing
// equivocation post-emission. EKM enforces σ-XOR-NR — the op stays
// σ-committed to its V. The σ-lock is acquired at KindValue emit time,
// so equivocation observed post-emission has no recovery path (a
// documented leader-witness trade-off; see docs/2abOBFT.md §Failure
// modes).
func TestEquivocationPostKindValue_NoRecovery(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	bA, err := s.instances[leader].BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	op2 := s.instances[OperatorID(2)]
	require.NoError(t, op2.ObservePhase1Bundle(bA, observedEarly))
	require.NoError(t, op2.ApplyHostValidity(0, Value("V_a"), true))
	vm, _, _, err := op2.MaybeFirePhase2a()
	require.NoError(t, err)
	require.NotNil(t, vm, "op 2 emits KindValue σ-locked on V_a")
	// Byz leader equivocates with V_b post-σ-lock.
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	require.NoError(t, op2.ObservePhase1Bundle(bB, observedAfterPhase2a))
	// Op 2 retains both V_a and V_b (Rule 2 evidence fires) but cannot
	// pivot — ownCommit stays nil because MaybeBuildAndBroadcastCommit
	// gates on ownNoValueMsg != nil (KindValue-path ops are σ-locked).
	_, hadCommit := op2.OwnCommit()
	require.False(t, hadCommit, "post-KindValue equivocation does NOT fire NR pivot")
	var foundRule2 bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceLeaderEquivocation {
			foundRule2 = true
		}
	}
	require.True(t, foundRule2, "Rule 2 still fires on equivocation observation (slashable signal preserved)")
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
	// Craft two distinct KindValues from same op on different V's. The
	// forwarded-witness bytes are arbitrary non-empty — they fail leader-
	// verify (so harvest silently discards), but ValidateValueMsg requires
	// a Layer-0 witness (the forwarded-witness invariant). Rule 6a fires
	// from the cross-V emission, not the witness verify failure.
	vmA := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V_a"), ValueRoot: ValueRoot(Value("V_a")),
		Witnesses:    l0Witness(Value("V_a"), Signature{0xff}),
		L0Partial:    Signature{0x01}, // arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	vmB := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V_b"), ValueRoot: ValueRoot(Value("V_b")),
		Witnesses:    l0Witness(Value("V_b"), Signature{0xff}),
		L0Partial:    Signature{0x01}, // arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
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

// TestObserveValueMsgThenCommitNR_FiresRule1: the cross-σ-NR
// (cross-signing) detection is on KindValue↔Commit-NR. The σ partial
// sits in KindValue.L0Partial; the NR partial sits in Commit.L0Partial.
// Both must verify against their respective key shares for Rule 1 to fire
// cryptographically. Rule 6a also fires on the sequence violation
// (KindValue + KindCommit-NR is outside the authorized set
// {NoValue→Value, NoValue→Commit-NR, Commit-NRDirect-alone}).
func TestObserveValueMsgThenCommitNR_FiresRule1(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Real σ partial on V_0 signed by op 1's V-share signer.
	sigmaPartial, err := s.instances[OperatorID(1)].signer.SignPartial(Value("V0"))
	require.NoError(t, err)
	// Real nr_tag_0 partial signed by op 1's IBE-share signer (= same
	// signer under Option A).
	nrTag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrPartial, err := s.instances[OperatorID(1)].tagSigner.SignPartial(nrTag)
	require.NoError(t, err)
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V0"), ValueRoot: ValueRoot(Value("V0")),
		Witnesses:    l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:    sigmaPartial,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	cNR := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:      CommitSideNR,
		L0Partial: nrPartial,
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
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
	require.True(t, foundRule1, "KindValue σ + KindCommit-NR cross-signing should fire Rule 1")
	require.True(t, foundRule6a, "sequence violation should fire Rule 6a")
}

// TestObserveCommit_PostNRDirectAnyEmissionFiresRule6a.
//
// KindCommit-NRDirect is sole-emission per slot, so any other Phase-2
// emission from the same op is slashable. This holds regardless of
// gossipsub arrival order — the receiver detects the unauthorized pair
// on whichever message arrives second.
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
	// Then a KindValue from same op — slashable (NRDirect is sole-emission).
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V0"), ValueRoot: ValueRoot(Value("V0")),
		Witnesses:    l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:    Signature{0x01},                         // arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
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
// FIRST, then NRDirect from the same op. Same slashable sequence
// (NRDirect is sole-emission), different observation order. The
// detection lives in ObserveCommit's `Side==NRDirect && (hadValue != nil
// || hadNoValue != nil)` branch.
func TestObserveCommit_PreNRDirectValueMsgThenNRDirectFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// KindValue first.
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V0"), ValueRoot: ValueRoot(Value("V0")),
		Witnesses:    l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:    Signature{0x01},                         // arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	// Then NRDirect from same op — slashable (NRDirect is sole-emission).
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

// TestObserveValueMsg_UpgradeAfterPriorCommitNRFiresRule6a:
// covers the three-message byz sequence
// `KindNoValue → KindCommit-NR → KindValue(V_a)` where the final KindValue
// arrives in-order (after both prior messages were observed). The op
// NR-committed and then σ-claims V_a — a σ-XOR-NR sequence violation. The
// hadNoValue branch handles upgrade processing; an additional nested
// check fires Rule 6a on the KindValue-after-KindCommit-NR violation.
// (The σ partial lives in KindValue, so the slashable triple is the
// NR-then-σ sequence above.)
func TestObserveValueMsg_UpgradeAfterPriorCommitNRFiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	// Prior KindNoValue from op 1.
	nv := &NoValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   1,
		Height:       s.cfg.Height,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveNoValueMsg(nv))
	// The triple-message slashable sequence is
	// `KindNoValue → KindCommit-NR → KindValue(V_a)` — the op committed
	// NR but then σ-claims V_a in a later KindValue. The Rule 6a check
	// in ObserveValueMsg's hadCommit branch fires on the sequence
	// violation; Rule 1 cross-signing also fires if both partials
	// verify.
	cNR := &Commit{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Side:       CommitSideNR,
		L0Partial:  Signature{0x01}, // unverified — sequence violation fires regardless
	}
	require.NoError(t, op2.ObserveCommit(cNR))
	vm := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   1,
		Height:       s.cfg.Height,
		V:            Value("V_a"),
		ValueRoot:    ValueRoot(Value("V_a")),
		Witnesses:    l0Witness(Value("V_a"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:    Signature{0x02},                          // arbitrary
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	var foundRule6a bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
	}
	require.True(t, foundRule6a, "KindNoValue → KindCommit-NR → KindValue should fire Rule 6a")
}

// TestPhase2aFire_NRDirectOnEquivocationBeforeKindValue: the
// equivocation trigger fires only when the op is still in EKM
// coordination state at L_0 — i.e., hasn't yet emitted KindValue. The
// canonical path is Phase-2a fire-time NRDirect: an op observing ≥ 2
// distinct V's from the leader at Phase-2a fire-time emits Commit-NRDirect
// (sole emission for the slot). This test pins that path.
func TestPhase2aFire_NRDirectOnEquivocationBeforeKindValue(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	// Two distinct bundles retained at op 2 BEFORE Phase 2a fires.
	bA := s.buildByzEquivocatingBundle(leader, 0, Value("V_a"))
	bB := s.buildByzEquivocatingBundle(leader, 0, Value("V_b"))
	op2 := s.instances[OperatorID(2)]
	require.NoError(t, op2.ObservePhase1Bundle(bA, observedEarly))
	require.NoError(t, op2.ObservePhase1Bundle(bB, observedEarly))
	// Phase 2a fire-time decision: equivocation observed → NRDirect.
	vm, nv, c, err := op2.MaybeFirePhase2a()
	require.NoError(t, err)
	require.Nil(t, vm)
	require.Nil(t, nv)
	require.NotNil(t, c)
	require.Equal(t, CommitSideNRDirect, c.Side, "Phase 2a NRDirect on equivocation observed pre-emission")
}

// TestAfterStateDelta_UpgradeIsTerminalSigmaEmission: the upgrade
// KindValue IS the σ-side terminal emission — it carries the emitter's
// σ partial at L_0 (L0Partial). There is no follow-up σ commit. Verifies
// the upgrade fires when preconditions are met AND that no second σ-side
// emission happens after.
func TestAfterStateDelta_UpgradeIsTerminalSigmaEmission(t *testing.T) {
	s := newSim(t, 4)
	// Ops 1, 2, 3 have V_0 + host valid; op 4 is a V-drop at Phase 2a.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2, 3}, 0, Value("V0"), true)
	for _, op := range s.allOperators() {
		_, _, _, err := s.instances[op].MaybeFirePhase2a()
		require.NoError(t, err)
	}
	// Late delivery of V_0 + host valid to op 4 → the upgrade fires
	// inside ApplyHostValidity's afterStateDelta cascade.
	leader := s.leaderAt(0)
	bundle, err := s.instances[leader].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[OperatorID(4)].ObservePhase1Bundle(bundle, observedAfterPhase2a))
	require.NoError(t, s.instances[OperatorID(4)].ApplyHostValidity(0, Value("V0"), true))
	upgrade, hadUpgrade := s.instances[OperatorID(4)].OwnValueMsg()
	require.True(t, hadUpgrade, "the upgrade should fire via afterStateDelta")
	require.Equal(t, Value("V0"), upgrade.V)
	require.NotEmpty(t, upgrade.L0Partial, "the upgrade carries σ partial directly")
	// No σ-side Commit follows.
	_, hadCommit := s.instances[OperatorID(4)].OwnCommit()
	require.False(t, hadCommit, "the upgrade is the terminal σ-side emission; no σ commit follows")
}

// TestObserveValueMsg_UpgradeMismatchedLayerEntriesFiresRule6a: a byz
// emits KindNoValue with one set of L_k>0 entries and then an "upgrade"
// KindValue with DIFFERENT L_k>0 entries. Per spec §Phase 2a-late
// upgrade, the L_k entries MUST be identical across the pair. The
// mismatch fires Rule 6a; the upgrade's entries are NOT processed (the
// prior NoValueMsg's pool contributions stand, preventing cross-pool
// injection).
func TestObserveValueMsg_UpgradeMismatchedLayerEntriesFiresRule6a(t *testing.T) {
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
		Witnesses:  l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:  Signature{0x01},                         // arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
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
		Witnesses:  l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:  Signature{0x01},                         // arbitrary; verify fails (Rule 5 OK for these tests focused on Rule 6a)
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

// TestObserveCommitNR_AfterKindValueFiresRule6aAndRule1: KindValue(V_a)
// then KindCommit-NR from same op is σ-XOR-NR violation. Rule 6a fires
// on sequence; Rule 1 fires if partials verify cryptographically.
func TestObserveCommitNR_AfterKindValueFiresRule6aAndRule1(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	sigmaPartial, err := s.instances[OperatorID(1)].signer.SignPartial(Value("V_a"))
	require.NoError(t, err)
	nrTag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrPartial, err := s.instances[OperatorID(1)].tagSigner.SignPartial(nrTag)
	require.NoError(t, err)
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V_a"), ValueRoot: ValueRoot(Value("V_a")),
		Witnesses:    l0Witness(Value("V_a"), Signature{0xff}),
		L0Partial:    sigmaPartial,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	c := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:      CommitSideNR,
		L0Partial: nrPartial,
	}
	require.NoError(t, op2.ObserveCommit(c))
	var foundRule6a, foundRule1 bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
		if e.Rule == EvidenceCrossSigning {
			foundRule1 = true
		}
	}
	require.True(t, foundRule6a, "KindValue → KindCommit-NR should fire Rule 6a (sequence)")
	require.True(t, foundRule1, "KindValue σ + KindCommit-NR should fire Rule 1 (cross-signing)")
}

// TestObserveValueMsg_AfterCommitNRFiresRule6aAndRule1: reorder of the
// above — KindCommit-NR arrives first, then KindValue. Same evidence
// fires symmetrically from the ObserveValueMsg side (the hadCommit
// branch checks for cross-signing + sequence violation).
func TestObserveValueMsg_AfterCommitNRFiresRule6aAndRule1(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	sigmaPartial, err := s.instances[OperatorID(1)].signer.SignPartial(Value("V_a"))
	require.NoError(t, err)
	nrTag := NoQuorumTag(s.cfg.ClusterID, s.cfg.Height, 0)
	nrPartial, err := s.instances[OperatorID(1)].tagSigner.SignPartial(nrTag)
	require.NoError(t, err)
	c := &Commit{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		Side:      CommitSideNR,
		L0Partial: nrPartial,
	}
	require.NoError(t, op2.ObserveCommit(c))
	vm := &ValueMsg{
		ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height,
		V: Value("V_a"), ValueRoot: ValueRoot(Value("V_a")),
		Witnesses:    l0Witness(Value("V_a"), Signature{0xff}),
		L0Partial:    sigmaPartial,
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	var foundRule6a, foundRule1 bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidencePhase2Equivocation {
			foundRule6a = true
		}
		if e.Rule == EvidenceCrossSigning {
			foundRule1 = true
		}
	}
	require.True(t, foundRule6a, "KindCommit-NR → KindValue should fire Rule 6a (reorder)")
	require.True(t, foundRule1, "KindCommit-NR + KindValue σ should fire Rule 1 (cross-signing reorder)")
}

// TestObserveValueMsg_FakeL0PartialFiresRule5: a peer's KindValue carrying
// an L0Partial that doesn't verify against the emitter's pubKeyShare on
// V triggers Rule 5 (fake plaintext σ) keyed on the EMITTER (the L0Partial
// is the emitter's own signing artifact, not a forwarded leader artifact
// — Rule 5 attribution differs from the forwarded L_0 witness handling). The
// emitter is still added to value_pool via the V-claim (claim pool), but
// NOT to σ-pool — the fake partial can't contribute to threshold
// reconstruction.
func TestObserveValueMsg_FakeL0PartialFiresRule5(t *testing.T) {
	s := newSim(t, 4)
	op2 := s.instances[OperatorID(2)]
	vm := &ValueMsg{
		ClusterID:    s.cfg.ClusterID,
		OperatorID:   1,
		Height:       s.cfg.Height,
		V:            Value("V0"),
		ValueRoot:    ValueRoot(Value("V0")),
		Witnesses:    l0Witness(Value("V0"), Signature{0xff}), // arbitrary; harvest silently discards
		L0Partial:    Signature("garbage-not-a-valid-partial"),
		LayerEntries: []LayerEntry{{Layer: 1, Kind: LayerEntryEmpty}},
	}
	require.NoError(t, op2.ObserveValueMsg(vm))
	var foundRule5 bool
	for _, e := range op2.Evidence() {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 1 {
			foundRule5 = true
			require.NotNil(t, e.FakePlaintextSigma)
			require.Equal(t, Value("V0"), e.FakePlaintextSigma.OnionValue)
		}
	}
	require.True(t, foundRule5, "fake L0Partial in KindValue should fire Rule 5 against emitter")
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
	// Then NRDirect from same op — slashable (NRDirect is sole-emission).
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

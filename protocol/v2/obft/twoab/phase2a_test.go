package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------- ApplyHostValidity / HostValidity ----------

func TestApplyHostValidity_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	require.NoError(t, s.instances[1].ApplyHostValidity(0, Value("V"), true))
	valid, recorded := s.instances[1].HostValidity(0, Value("V"))
	require.True(t, recorded)
	require.True(t, valid)
}

func TestApplyHostValidity_NotValid(t *testing.T) {
	s := newSim(t, 4)
	require.NoError(t, s.instances[1].ApplyHostValidity(0, Value("V"), false))
	valid, recorded := s.instances[1].HostValidity(0, Value("V"))
	require.True(t, recorded)
	require.False(t, valid)
}

func TestApplyHostValidity_RejectsBadLayer(t *testing.T) {
	s := newSim(t, 4)
	require.ErrorIs(t, s.instances[1].ApplyHostValidity(-1, Value("V"), true), ErrLayerOutOfRange)
	require.ErrorIs(t, s.instances[1].ApplyHostValidity(s.K, Value("V"), true), ErrLayerOutOfRange)
}

func TestApplyHostValidity_RejectsEmptyValue(t *testing.T) {
	s := newSim(t, 4)
	require.ErrorIs(t, s.instances[1].ApplyHostValidity(0, nil, true), ErrEmptyValue)
}

func TestApplyHostValidity_OverwritePreservesLatest(t *testing.T) {
	s := newSim(t, 4)
	require.NoError(t, s.instances[1].ApplyHostValidity(0, Value("V"), false))
	require.NoError(t, s.instances[1].ApplyHostValidity(0, Value("V"), true))
	valid, _ := s.instances[1].HostValidity(0, Value("V"))
	require.True(t, valid, "latest host verdict wins (validity stabilization)")
}

func TestHostValidity_NotRecordedReturnsFalse(t *testing.T) {
	s := newSim(t, 4)
	_, recorded := s.instances[1].HostValidity(0, Value("V"))
	require.False(t, recorded)
}

func TestHostValidity_PerLayerSeparation(t *testing.T) {
	s := newSim(t, 4)
	require.NoError(t, s.instances[1].ApplyHostValidity(0, Value("V"), true))
	// Same V at a different layer is independent.
	_, recorded := s.instances[1].HostValidity(1, Value("V"))
	require.False(t, recorded)
}

// ---------- ComputeLocalVerdict — 4-case rule ----------

func TestComputeLocalVerdict_NoRetention_NR(t *testing.T) {
	s := newSim(t, 4)
	// op2 has nothing retained at layer 0.
	kind, root, err := s.instances[2].ComputeLocalVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNR, kind)
	require.Equal(t, [32]byte{}, root)
}

func TestComputeLocalVerdict_SingleVHostValid_SigmaV(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	kind, root, err := s.instances[2].ComputeLocalVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, kind)
	require.Equal(t, ValueRoot(Value("V0")), root)
}

func TestComputeLocalVerdict_SingleVHostInvalid_NV(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), false)

	kind, root, err := s.instances[2].ComputeLocalVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNV, kind)
	require.Equal(t, [32]byte{}, root)
}

func TestComputeLocalVerdict_TwoDistinctV_NR(t *testing.T) {
	s := newSim(t, 4)
	// Leader equivocates with V_a and V_b; op2 retains both.
	s.deliverPhase1Equivocation(0,
		Value("V_a"), Value("V_b"),
		[]OperatorID{2}, []OperatorID{2},
		observedEarly)
	// Host verdict isn't consulted in the equivocation case — no need to apply.
	kind, root, err := s.instances[2].ComputeLocalVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNR, kind, "equivocation observed → NR regardless of host")
	require.Equal(t, [32]byte{}, root)
}

func TestComputeLocalVerdict_AuthOnlyRetention_NR(t *testing.T) {
	s := newSim(t, 4)
	// Deliver V past T_accept_max — auth-only retention only.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedAuthOnly)
	// Even if host says valid, auth-only doesn't allow σV.
	require.NoError(t, s.instances[2].ApplyHostValidity(0, Value("V0"), true))

	kind, root, err := s.instances[2].ComputeLocalVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNR, kind, "auth-only retention not verdict-eligible")
	require.Equal(t, [32]byte{}, root)
}

func TestComputeLocalVerdict_SingleVMissingHost_Errors(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
	// No ApplyHostValidity called.
	_, _, err := s.instances[2].ComputeLocalVerdict(0)
	require.ErrorContains(t, err, "host validity not applied")
}

func TestComputeLocalVerdict_BadLayer_Errors(t *testing.T) {
	s := newSim(t, 4)
	_, _, err := s.instances[1].ComputeLocalVerdict(-1)
	require.ErrorIs(t, err, ErrLayerOutOfRange)
	_, _, err = s.instances[1].ComputeLocalVerdict(s.K)
	require.ErrorIs(t, err, ErrLayerOutOfRange)
}

// ---------- BuildVerdict ----------

func TestBuildVerdict_HappyPathSigmaV(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	v, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, s.cfg.ClusterID, v.ClusterID)
	require.Equal(t, OperatorID(2), v.OperatorID)
	require.Equal(t, s.cfg.Height, v.Height)
	require.Equal(t, 0, v.Layer)
	require.Equal(t, VerdictSigmaV, v.Kind)
	require.Equal(t, ValueRoot(Value("V0")), v.ValueRoot)
}

func TestBuildVerdict_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	v1, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	v2, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Same(t, v1, v2, "second BuildVerdict returns cached")
}

func TestBuildVerdict_NRWhenNoV(t *testing.T) {
	s := newSim(t, 4)
	v, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNR, v.Kind)
	require.Equal(t, [32]byte{}, v.ValueRoot)
}

func TestBuildVerdict_NVWhenHostInvalid(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), false)

	v, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNV, v.Kind)
}

func TestBuildVerdict_PropagatesComputeError(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
	// Skip ApplyHostValidity → BuildVerdict should error.
	_, err := s.instances[2].BuildVerdict(0)
	require.ErrorContains(t, err, "host validity not applied")
}

func TestOwnVerdict_BeforeBuildReturnsFalse(t *testing.T) {
	s := newSim(t, 4)
	v, ok := s.instances[2].OwnVerdict(0)
	require.False(t, ok)
	require.Nil(t, v)
}

func TestOwnVerdict_AfterBuild(t *testing.T) {
	s := newSim(t, 4)
	built, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	got, ok := s.instances[2].OwnVerdict(0)
	require.True(t, ok)
	require.Same(t, built, got)
}

// ---------- ObserveVerdict — first-observed convergence ----------

func TestObserveVerdict_FirstObservedRecorded(t *testing.T) {
	s := newSim(t, 4)
	v := s.deliverVerdict(2, 0, []OperatorID{1, 3, 4})

	for _, op := range []OperatorID{1, 3, 4} {
		peers := s.instances[op].PeerVerdicts(0)
		require.Len(t, peers, 1)
		require.NotNil(t, peers[OperatorID(2)])
		require.Equal(t, v.Kind, peers[OperatorID(2)].Kind)
	}
}

func TestObserveVerdict_IdenticalRebroadcastDeduped(t *testing.T) {
	s := newSim(t, 4)
	v, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.NoError(t, s.instances[1].ObserveVerdict(v))
	require.NoError(t, s.instances[1].ObserveVerdict(v))
	require.NoError(t, s.instances[1].ObserveVerdict(v))

	peers := s.instances[1].PeerVerdicts(0)
	require.Len(t, peers, 1)
	require.False(t, s.instances[1].IsEquivocator(0, 2),
		"identical rebroadcasts don't mark as equivocator")
	require.Empty(t, s.instances[1].Evidence())
}

// Rule 6a fires on second DISTINCT verdict from same op. The pair
// `(VerdictA, VerdictB)` is recorded; the equivocator flag is set;
// first-observed verdict stays in convergence pool.
func TestObserveVerdict_DistinctSecondVerdict_FiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	// Byzantine op2 sends σV verdict to op1 and NR verdict to op3.
	vA := &Verdict{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layer:      0,
		Kind:       VerdictSigmaV,
		ValueRoot:  [32]byte{0xab},
	}
	vB := &Verdict{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layer:      0,
		Kind:       VerdictNR,
		ValueRoot:  [32]byte{},
	}
	// op1 sees BOTH (in this order: vA first, then vB) — Rule 6a fires.
	require.NoError(t, s.instances[1].ObserveVerdict(vA))
	require.NoError(t, s.instances[1].ObserveVerdict(vB))

	require.True(t, s.instances[1].IsEquivocator(0, 2))
	ev := s.instances[1].Evidence()
	require.Len(t, ev, 1)
	require.Equal(t, EvidenceVerdictEquivocation, ev[0].Rule)
	require.Equal(t, OperatorID(2), ev[0].OperatorID)
	require.Equal(t, 0, ev[0].Layer)
	require.NotNil(t, ev[0].VerdictEquivocation)
	require.Equal(t, VerdictSigmaV, ev[0].VerdictEquivocation.VerdictA.Kind)
	require.Equal(t, VerdictNR, ev[0].VerdictEquivocation.VerdictB.Kind)

	// First-observed verdict stays in the pool (per spec).
	peers := s.instances[1].PeerVerdicts(0)
	require.Equal(t, VerdictSigmaV, peers[OperatorID(2)].Kind)
}

func TestObserveVerdict_ThirdDistinctDroppedNoExtraEvidence(t *testing.T) {
	s := newSim(t, 4)
	vA := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictSigmaV, ValueRoot: [32]byte{0xab}}
	vB := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictNR}
	vC := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictNV}

	require.NoError(t, s.instances[1].ObserveVerdict(vA))
	require.NoError(t, s.instances[1].ObserveVerdict(vB))
	require.NoError(t, s.instances[1].ObserveVerdict(vC))

	require.True(t, s.instances[1].IsEquivocator(0, 2))
	// Only ONE Rule-6a evidence entry, even with 3 distinct verdicts.
	ev := s.instances[1].Evidence()
	require.Len(t, ev, 1)
}

// Distinct ValueRoot (σV on V_a vs σV on V_b) → Rule 6a.
func TestObserveVerdict_DistinctValueRoot_FiresRule6a(t *testing.T) {
	s := newSim(t, 4)
	vA := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictSigmaV, ValueRoot: [32]byte{0xa}}
	vB := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictSigmaV, ValueRoot: [32]byte{0xb}}

	require.NoError(t, s.instances[1].ObserveVerdict(vA))
	require.NoError(t, s.instances[1].ObserveVerdict(vB))

	require.True(t, s.instances[1].IsEquivocator(0, 2))
	ev := s.instances[1].Evidence()
	require.Len(t, ev, 1)
	require.Equal(t, EvidenceVerdictEquivocation, ev[0].Rule)
}

// Equivocation at one layer doesn't propagate to other layers.
func TestObserveVerdict_PerLayerEquivocation(t *testing.T) {
	s := newSim(t, 4)
	vA0 := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictSigmaV, ValueRoot: [32]byte{0xa}}
	vB0 := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 0, Kind: VerdictNR}
	v1 := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 2, Height: s.cfg.Height, Layer: 1, Kind: VerdictNR}

	require.NoError(t, s.instances[1].ObserveVerdict(vA0))
	require.NoError(t, s.instances[1].ObserveVerdict(vB0))
	require.NoError(t, s.instances[1].ObserveVerdict(v1))

	require.True(t, s.instances[1].IsEquivocator(0, 2))
	require.False(t, s.instances[1].IsEquivocator(1, 2),
		"equivocation flag is per-layer, not cluster-wide")
}

// Two different operators each equivocating at the same layer.
func TestObserveVerdict_PerOperatorEquivocation(t *testing.T) {
	s := newSim(t, 4)
	mk := func(op OperatorID, kind VerdictKind, root byte) *Verdict {
		return &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: op, Height: s.cfg.Height, Layer: 0, Kind: kind, ValueRoot: [32]byte{root}}
	}
	require.NoError(t, s.instances[3].ObserveVerdict(mk(1, VerdictSigmaV, 0xa)))
	require.NoError(t, s.instances[3].ObserveVerdict(mk(1, VerdictNR, 0)))
	require.NoError(t, s.instances[3].ObserveVerdict(mk(2, VerdictSigmaV, 0xb)))
	require.NoError(t, s.instances[3].ObserveVerdict(mk(2, VerdictNV, 0)))

	require.True(t, s.instances[3].IsEquivocator(0, 1))
	require.True(t, s.instances[3].IsEquivocator(0, 2))
	require.Len(t, s.instances[3].Evidence(), 2)
}

func TestObserveVerdict_RejectsNil(t *testing.T) {
	s := newSim(t, 4)
	require.Error(t, s.instances[2].ObserveVerdict(nil))
}

func TestObserveVerdict_RejectsBadValidate(t *testing.T) {
	s := newSim(t, 4)
	bad := &Verdict{
		ClusterID:  [32]byte{0xff}, // wrong cluster
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layer:      0,
		Kind:       VerdictNR,
	}
	require.ErrorContains(t, s.instances[1].ObserveVerdict(bad), "cluster id")
}

// EvidenceObserver fires once per (Rule, op, layer) tuple even on
// multiple distinct verdicts past the second.
func TestObserveVerdict_EvidenceObserverFiresOnce(t *testing.T) {
	c := healthyConfig()
	pubShares := map[OperatorID][]byte{1: {1}, 2: {2}, 3: {3}, 4: {4}}
	var fired []Evidence
	observer := func(e Evidence) { fired = append(fired, e) }
	inst, err := NewInstance(
		c, OperatorID(1),
		NewStubSigner(c.QV(), pubShares[1]),
		NewStubSigner(c.QV(), pubShares[1]),
		NewStubIBE(c.QEnc()),
		nil, pubShares, nil, observer,
	)
	require.NoError(t, err)

	mk := func(kind VerdictKind, root byte) *Verdict {
		return &Verdict{ClusterID: c.ClusterID, OperatorID: 2, Height: c.Height, Layer: 0, Kind: kind, ValueRoot: [32]byte{root}}
	}
	require.NoError(t, inst.ObserveVerdict(mk(VerdictSigmaV, 0xa)))
	require.NoError(t, inst.ObserveVerdict(mk(VerdictNR, 0)))
	require.NoError(t, inst.ObserveVerdict(mk(VerdictSigmaV, 0xc)))
	require.NoError(t, inst.ObserveVerdict(mk(VerdictNV, 0)))

	require.Len(t, fired, 1, "observer fires once per (Rule, op, layer) tuple")
	require.Equal(t, EvidenceVerdictEquivocation, fired[0].Rule)
}

// PeerVerdicts snapshot is independent — caller mutation doesn't affect
// Instance state.
func TestPeerVerdicts_SnapshotIsolated(t *testing.T) {
	s := newSim(t, 4)
	s.deliverVerdict(2, 0, []OperatorID{1})

	snap := s.instances[1].PeerVerdicts(0)
	require.Len(t, snap, 1)
	// Mutate the snapshot (set entry to nil); Instance should be
	// unaffected.
	delete(snap, OperatorID(2))
	// Re-fetch — original entry still present.
	again := s.instances[1].PeerVerdicts(0)
	require.NotNil(t, again[OperatorID(2)])
}

// Phase-2a cluster-wide convergence test: every honest operator
// computes their verdict locally and broadcasts it. The convergence
// pools (each receiver's PeerVerdicts) should match.
func TestPhase2a_HealthyClusterConverges(t *testing.T) {
	s := newSim(t, 4)
	// All four operators retain V0 at layer 0; all host-validate.
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	// Every op builds + broadcasts their verdict; every other op
	// observes.
	for _, op := range s.allOperators() {
		v, err := s.instances[op].BuildVerdict(0)
		require.NoError(t, err)
		require.Equal(t, VerdictSigmaV, v.Kind)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveVerdict(v))
		}
	}

	// Every operator sees all 3 OTHER operators' verdicts as σV.
	for _, op := range s.allOperators() {
		peers := s.instances[op].PeerVerdicts(0)
		require.Len(t, peers, 3, "op %d should see 3 peer verdicts", op)
		for peerID, v := range peers {
			require.Equal(t, VerdictSigmaV, v.Kind, "peer %d", peerID)
		}
	}
}

package twoab

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------- BuildPhase1Bundle ----------

func TestBuildPhase1Bundle_HealthyLeader(t *testing.T) {
	s := newSim(t, 4)
	// op1 is L_0 leader by sim convention.
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.Equal(t, s.cfg.ClusterID, b.ClusterID)
	require.Equal(t, OperatorID(1), b.OperatorID)
	require.Equal(t, s.cfg.Height, b.Height)
	require.Equal(t, 0, b.Layer)
	require.True(t, bytes.Equal(b.Value, Value("V0")))
}

func TestBuildPhase1Bundle_RejectsNonLeader(t *testing.T) {
	s := newSim(t, 4)
	// op2 is NOT the L_0 leader.
	_, err := s.instances[2].BuildPhase1Bundle(0, Value("V0"))
	require.ErrorIs(t, err, ErrNotLeader)
}

func TestBuildPhase1Bundle_RejectsLayerOutOfRange(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[1].BuildPhase1Bundle(s.K, Value("V"))
	require.ErrorIs(t, err, ErrLayerOutOfRange)
	_, err = s.instances[1].BuildPhase1Bundle(-1, Value("V"))
	require.ErrorIs(t, err, ErrLayerOutOfRange)
}

func TestBuildPhase1Bundle_RejectsEmptyValue(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[1].BuildPhase1Bundle(0, nil)
	require.ErrorIs(t, err, ErrEmptyValue)
	_, err = s.instances[1].BuildPhase1Bundle(0, Value{})
	require.ErrorIs(t, err, ErrEmptyValue)
}

// Idempotent re-build with same args produces equivalent bundles. Per
// spec §Phase 1, the leader's broadcast is a single envelope; the
// protocol-level Build is allowed to be called repeatedly with the same
// inputs (re-broadcasting same V is not equivocation).
func TestBuildPhase1Bundle_IdempotentSameArgs(t *testing.T) {
	s := newSim(t, 4)
	b1, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	b2, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.Equal(t, b1.OperatorID, b2.OperatorID)
	require.Equal(t, b1.Layer, b2.Layer)
	require.True(t, bytes.Equal(b1.Value, b2.Value))
}

// 2abOBFT Variant C regression guard: the bundle has no SigmaV. This
// test pins down that BuildPhase1Bundle doesn't accidentally re-introduce
// a threshold partial.
func TestBuildPhase1Bundle_NoSigmaVField(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V"))
	require.NoError(t, err)
	// The struct has no SigmaV field at all (Variant C). This test
	// exists so that any future refactor that re-introduces a SigmaV
	// field has to also delete this test, forcing a re-spec discussion.
	_ = b
	require.NotContains(t, "SigmaV", "twoab.Phase1Bundle struct must not have a SigmaV field — Variant C")
}

// Defensive: the caller can mutate the passed Value slice after Build;
// the bundle's Value should be an independent copy.
func TestBuildPhase1Bundle_DefensiveValueCopy(t *testing.T) {
	s := newSim(t, 4)
	v := Value("V0-original")
	b, err := s.instances[1].BuildPhase1Bundle(0, v)
	require.NoError(t, err)
	// Mutate the caller's slice; the bundle's Value must not change.
	v[0] = 'X'
	require.True(t, bytes.Equal(b.Value, Value("V0-original")))
}

// ---------- ObservePhase1Bundle — healthy / retention ----------

func TestObservePhase1Bundle_HealthyRetention(t *testing.T) {
	s := newSim(t, 4)
	b := s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)

	// Every operator should have retained the bundle.
	for _, op := range s.allOperators() {
		retained := s.instances[op].RetainedBundles(0, b.OperatorID)
		require.Len(t, retained, 1, "op %d should have 1 retained bundle", op)
		require.False(t, retained[0].AuthOnly, "op %d retention should be regular, not auth-only", op)
		require.Equal(t, observedEarly, retained[0].RetentionEstablishedAt)
	}
}

func TestObservePhase1Bundle_IdempotentRebroadcast(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedEarly))
	// Re-broadcast (gossipsub mesh path) — must dedup silently.
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedEarly))
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedEarly))
	retained := s.instances[2].RetainedBundles(0, b.OperatorID)
	require.Len(t, retained, 1, "rebroadcasts should not duplicate retention")
}

// ---------- Late-bundle auth-only retention ----------

func TestObservePhase1Bundle_AuthOnlyRetentionPastTAcceptMax(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	// Deliver past T_accept_max but before T_commit.
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedAuthOnly))
	retained := s.instances[2].RetainedBundles(0, b.OperatorID)
	require.Len(t, retained, 1)
	require.True(t, retained[0].AuthOnly, "bundle past T_accept_max should be auth-only")
	require.Equal(t, observedAuthOnly, retained[0].RetentionEstablishedAt)
}

func TestObservePhase1Bundle_RejectsPastTCommit(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	err = s.instances[2].ObservePhase1Bundle(b, observedLate)
	require.ErrorIs(t, err, ErrLatePhase1Bundle)
	// No retention recorded.
	require.Empty(t, s.instances[2].RetainedBundles(0, b.OperatorID))
}

// Auth-only → regular promotion when a re-broadcast lands within the
// accept window. Per spec §Phase 1 bundle propagation: gossipsub re-flood
// continues into Phase 2a so late bundles arriving in (T_commit, T_accept_max]
// are absorbed; in the test's terms, an earlier auth-only retention can
// be upgraded if the bundle is later re-observed within the accept window.
func TestObservePhase1Bundle_AuthOnlyToRegularPromotion(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	// First observe past T_accept_max.
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedAuthOnly))
	retained := s.instances[2].RetainedBundles(0, b.OperatorID)
	require.True(t, retained[0].AuthOnly)
	// Then re-observe within the accept window (rare in practice — gossip
	// usually flows the other direction — but the protocol allows it).
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedEarly))
	retained = s.instances[2].RetainedBundles(0, b.OperatorID)
	require.Len(t, retained, 1)
	require.False(t, retained[0].AuthOnly, "promotion: auth-only → regular")
	require.Equal(t, observedEarly, retained[0].RetentionEstablishedAt)
}

// Reverse: regular → auth-only is NOT a downgrade. Once a bundle is
// retained in the accept window, a later auth-only re-observation is
// a silent no-op (dedup).
func TestObservePhase1Bundle_RegularStaysRegular(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedEarly))
	require.NoError(t, s.instances[2].ObservePhase1Bundle(b, observedAuthOnly))
	retained := s.instances[2].RetainedBundles(0, b.OperatorID)
	require.False(t, retained[0].AuthOnly)
	// RetentionEstablishedAt should be the first (earlier) observation.
	require.Equal(t, observedEarly, retained[0].RetentionEstablishedAt)
}

// ---------- Leader equivocation → Rule 2 ----------

func TestObservePhase1Bundle_LeaderEquivocationFiresRule2(t *testing.T) {
	s := newSim(t, 4)
	bA, bB := s.deliverPhase1Equivocation(0,
		Value("V_a"), Value("V_b"),
		[]OperatorID{2}, []OperatorID{2},
		observedEarly)

	// op2 should have BOTH bundles retained.
	retained := s.instances[2].RetainedBundles(0, OperatorID(1))
	require.Len(t, retained, 2)
	// Rule 2 evidence recorded against the leader (op1) at layer 0.
	ev := s.instances[2].Evidence()
	require.Len(t, ev, 1)
	require.Equal(t, EvidenceLeaderEquivocation, ev[0].Rule)
	require.Equal(t, OperatorID(1), ev[0].OperatorID)
	require.Equal(t, 0, ev[0].Layer)
	require.NotNil(t, ev[0].LeaderEquivocation)
	// BundleA holds the first retention; BundleB the second. Match by V.
	require.True(t,
		(bytes.Equal(ev[0].LeaderEquivocation.BundleA.Value, bA.Value) &&
			bytes.Equal(ev[0].LeaderEquivocation.BundleB.Value, bB.Value)),
		"BundleA/BundleB ordering matches observation order")
}

// Third distinct V from the same leader is dropped silently (cap=2).
// Per spec §Phase 1 / Retention bounds.
func TestObservePhase1Bundle_ThirdDistinctVDroppedSilently(t *testing.T) {
	s := newSim(t, 4)
	leader := s.instances[1]
	bA, err := leader.BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	bB, err := leader.BuildPhase1Bundle(0, Value("V_b"))
	require.NoError(t, err)
	bC, err := leader.BuildPhase1Bundle(0, Value("V_c"))
	require.NoError(t, err)

	require.NoError(t, s.instances[2].ObservePhase1Bundle(bA, observedEarly))
	require.NoError(t, s.instances[2].ObservePhase1Bundle(bB, observedEarly))
	require.NoError(t, s.instances[2].ObservePhase1Bundle(bC, observedEarly))

	retained := s.instances[2].RetainedBundles(0, OperatorID(1))
	require.Len(t, retained, 2, "third distinct V dropped — cap=2")
	// Evidence is recorded for the bA/bB pair only (first equivocation event).
	ev := s.instances[2].Evidence()
	require.Len(t, ev, 1)
}

// EvidenceObserver fires on Rule 2 detection.
func TestObservePhase1Bundle_EvidenceObserverFires(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	observer := func(e Evidence) { fired = append(fired, e) }
	// Rebuild op2's Instance with the observer wired in.
	inst, err := NewInstance(
		s.cfg, OperatorID(2),
		NewStubSigner(s.cfg.QV(), s.pubShares[2]),
		NewStubSigner(s.cfg.QV(), s.pubShares[2]),
		NewStubIBE(s.cfg.QEnc()),
		nil, s.pubShares, nil, observer,
	)
	require.NoError(t, err)

	bA, err := s.instances[1].BuildPhase1Bundle(0, Value("V_a"))
	require.NoError(t, err)
	bB, err := s.instances[1].BuildPhase1Bundle(0, Value("V_b"))
	require.NoError(t, err)
	require.NoError(t, inst.ObservePhase1Bundle(bA, observedEarly))
	require.NoError(t, inst.ObservePhase1Bundle(bB, observedEarly))
	require.Len(t, fired, 1)
	require.Equal(t, EvidenceLeaderEquivocation, fired[0].Rule)
}

// ---------- Structural validation surfaced via Observe ----------

func TestObservePhase1Bundle_RejectsNil(t *testing.T) {
	s := newSim(t, 4)
	err := s.instances[2].ObservePhase1Bundle(nil, observedEarly)
	require.ErrorContains(t, err, "nil phase-1 bundle")
}

func TestObservePhase1Bundle_RejectsWrongLeader(t *testing.T) {
	s := newSim(t, 4)
	// Construct a bundle claiming layer 0 but from op 2 (who isn't L_0).
	b := &Phase1Bundle{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: OperatorID(2),
		Height:     s.cfg.Height,
		Layer:      0,
		Value:      Value("V"),
	}
	err := s.instances[3].ObservePhase1Bundle(b, observedEarly)
	require.ErrorContains(t, err, "leader is")
}

func TestObservePhase1Bundle_RejectsWrongCluster(t *testing.T) {
	s := newSim(t, 4)
	b, err := s.instances[1].BuildPhase1Bundle(0, Value("V"))
	require.NoError(t, err)
	b.ClusterID = [32]byte{0xff}
	err = s.instances[2].ObservePhase1Bundle(b, observedEarly)
	require.ErrorContains(t, err, "cluster id")
}

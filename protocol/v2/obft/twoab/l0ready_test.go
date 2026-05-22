package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Unit tests for the L_0 early-fire signal (l0ReadyCh / maybeSignalL0Ready /
// l0DecisionReady). The DES/runner selects on L0ReadyCh() to async-fire
// MaybeFirePhase2a before the cluster-wide TPhase2a backstop (Op6).
//
// These pin twoab's L0Ready semantics, which DIVERGE from base
// (protocol/v2/obft/base/early_commit_test.go) in two load-bearing ways:
//
//   - NoValue does NOT close L0Ready. A host-NV verdict or a V-drop (no
//     retained V_0) leaves the channel OPEN — the op waits for the
//     TPhase2a backstop so V keeps its full reflood window before the op
//     declares NV. In base, host-NV is itself a commitment and closes
//     L0Ready. (See l0DecisionReady / the l0ReadyCh field doc.)
//
//   - The leader closes L0Ready via self-observe (ObservePhase1Bundle +
//     ApplyHostValidity), NOT via BuildPhase1Bundle. twoab's
//     computeLocalValueState is retention-driven, and BuildPhase1Bundle
//     σ-locks without self-retaining — unlike base, whose l0DecisionReady
//     checks sigmaLocked[0] and so fires straight from the build. (See
//     maybeSignalL0Ready's doc on the two-and-only-two signal sites.)
//
// Completeness: computeLocalValueState reads only retainedBundles (written
// solely by retainPhase1Bundle) and hostVerdict (written solely by
// ApplyHostValidity); both call maybeSignalL0Ready after mutating, so these
// two sites cover every state change that can flip fire-readiness.

// requireL0Open fails if op's L0ReadyCh has closed.
func requireL0Open(t *testing.T, inst *Instance, msg string) {
	t.Helper()
	select {
	case <-inst.L0ReadyCh():
		t.Fatalf("L0ReadyCh closed unexpectedly: %s", msg)
	default:
	}
}

// requireL0Closed fails if op's L0ReadyCh is still open.
func requireL0Closed(t *testing.T, inst *Instance, msg string) {
	t.Helper()
	select {
	case <-inst.L0ReadyCh():
	default:
		t.Fatalf("L0ReadyCh did not close: %s", msg)
	}
}

// firstNonLeader returns an operator that is not the L_0 leader.
func (s *sim) firstNonLeader() OperatorID {
	leader := s.leaderAt(0)
	for _, op := range s.allOperators() {
		if op != leader {
			return op
		}
	}
	s.t.Fatalf("no non-leader operator in cluster of size %d", s.n)
	return 0
}

func TestL0Ready_InitiallyOpen(t *testing.T) {
	s := newSim(t, 4)
	for _, inst := range s.instances {
		requireL0Open(t, inst, "before any Phase-1 observation")
	}
}

// Primary σ path: a non-leader that retains the unique V_0 AND records a
// valid host verdict becomes σ-eligible (Value) → ready. Mirrors base's
// TestL0Ready_NonLeader_BundleObservedAndValid.
func TestL0Ready_NonLeader_BundleAndValid_Ready(t *testing.T) {
	s := newSim(t, 4)
	op := s.firstNonLeader()
	V := s.candidates[0]

	bundle, err := s.instances[s.leaderAt(0)].BuildPhase1Bundle(0, V)
	require.NoError(t, err)

	requireL0Open(t, s.instances[op], "before bundle observation")

	require.NoError(t, s.instances[op].ObservePhase1Bundle(bundle, observedEarly))
	// Retained but host verdict not yet recorded → NoValue (defensive) → open.
	requireL0Open(t, s.instances[op], "bundle retained, host verdict not yet recorded")

	require.NoError(t, s.instances[op].ApplyHostValidity(0, V, true))
	requireL0Closed(t, s.instances[op], "unique retained V_0 + valid host verdict")
}

// DIVERGENCE FROM base: a host NOT-valid verdict on the unique retained
// V_0 yields NoValue, which does NOT close L0Ready (the op waits for the
// TPhase2a backstop before declaring NV). In base the same scenario fires.
func TestL0Ready_NonLeader_HostNotValid_StaysOpen(t *testing.T) {
	s := newSim(t, 4)
	op := s.firstNonLeader()
	V := s.candidates[0]

	bundle, err := s.instances[s.leaderAt(0)].BuildPhase1Bundle(0, V)
	require.NoError(t, err)
	require.NoError(t, s.instances[op].ObservePhase1Bundle(bundle, observedEarly))
	require.NoError(t, s.instances[op].ApplyHostValidity(0, V, false))

	requireL0Open(t, s.instances[op],
		"host NOT-valid → NoValue must NOT close L0Ready (twoab divergence from base)")
}

// A V-drop op (no retained V_0) is in the NoValue state and stays open,
// even after a host verdict is recorded for some other value.
func TestL0Ready_NoBundle_StaysOpen(t *testing.T) {
	s := newSim(t, 4)
	op := s.firstNonLeader()

	requireL0Open(t, s.instances[op], "no retained V_0 at L_0")

	// Recording a verdict for a value the op never retained must not flip it.
	require.NoError(t, s.instances[op].ApplyHostValidity(0, s.candidates[0], true))
	requireL0Open(t, s.instances[op], "host verdict for an unretained V must not close L0Ready")
}

// Equivocation: ≥ 2 distinct V_0 retained from the leader → NRDirect →
// ready, independent of any host verdict. Mirrors base's
// TestL0Ready_Equivocation_ForcesReady.
func TestL0Ready_Equivocation_Ready(t *testing.T) {
	s := newSim(t, 4)
	op := s.firstNonLeader()
	vA := Value("L0-equiv-A")
	vB := Value("L0-equiv-B")

	s.deliverPhase1Equivocation(0, vA, vB, []OperatorID{op}, []OperatorID{op}, observedEarly)

	requireL0Closed(t, s.instances[op],
		"two distinct V_0 retained (equivocation) forces NRDirect → ready")
}

// Leader path: BuildPhase1Bundle ALONE does not close L0Ready (it σ-locks
// but does not self-retain); the leader becomes ready only after it
// self-observes its own bundle and records its host verdict. This pins the
// divergence from base (where the build itself fires via sigmaLocked[0]).
func TestL0Ready_Leader_BuildAloneStaysOpen_SelfObserveCloses(t *testing.T) {
	s := newSim(t, 4)
	leader := s.leaderAt(0)
	V := s.candidates[0]

	b, err := s.instances[leader].BuildPhase1Bundle(0, V)
	require.NoError(t, err)
	requireL0Open(t, s.instances[leader],
		"BuildPhase1Bundle σ-locks but does not self-retain → leader L0Ready stays open")

	// Self-observe (as the DES/runner does at fetch time) + host verdict.
	require.NoError(t, s.instances[leader].ObservePhase1Bundle(b, observedEarly))
	require.NoError(t, s.instances[leader].ApplyHostValidity(0, V, true))

	requireL0Closed(t, s.instances[leader], "leader self-observed unique V_0 + valid host verdict")
}

// L0Ready is an L_0-only signal: retaining + validating a bundle at a
// deeper layer must not close it. Mirrors base's
// TestL0Ready_DeeperLayerObservation_NoTrigger.
func TestL0Ready_DeeperLayerObservation_StaysOpen(t *testing.T) {
	s := newSim(t, 4) // K=2, so layer 1 exists
	op := s.firstNonLeader()
	V1 := s.candidates[1]

	b, err := s.instances[s.leaderAt(1)].BuildPhase1Bundle(1, V1)
	require.NoError(t, err)
	require.NoError(t, s.instances[op].ObservePhase1Bundle(b, observedEarly))
	require.NoError(t, s.instances[op].ApplyHostValidity(1, V1, true))

	requireL0Open(t, s.instances[op], "L_1 retention/validation must not close the L_0 signal")
}

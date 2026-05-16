package base

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the L_0-trigger early-commit signal (spec docs/OBFT.md §Phase 2
// emission-timing). L0ReadyCh closes when the operator can determine their
// L_0 commitment: uniquely-retained host-validated V, or host not-valid on
// the unique retained V, or ≥ 2 distinct V's retained (equivocation forcing
// NR), or for an L_0 leader the moment they've σ-locked via BuildPhase1Bundle.

func TestL0Ready_InitiallyOpen(t *testing.T) {
	s := newSim(t, 4)
	for _, inst := range s.instances {
		select {
		case <-inst.L0ReadyCh():
			t.Fatalf("L0ReadyCh closed before any Phase-1 observation")
		default:
		}
	}
}

func TestL0Ready_NonLeader_BundleObservedAndValid(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0)
	bundle, err := s.instances[leaderID].BuildPhase1Bundle(0, s.candidates[0])
	require.NoError(t, err)

	// Pick a non-leader operator to test the trigger.
	var nonLeaderID OperatorID
	for _, op := range s.allOperators() {
		if op != leaderID {
			nonLeaderID = op
			break
		}
	}
	nonLeader := s.instances[nonLeaderID]

	// Before bundle observation: not ready.
	select {
	case <-nonLeader.L0ReadyCh():
		t.Fatalf("L0ReadyCh closed before bundle observation")
	default:
	}

	require.NoError(t, nonLeader.ObservePhase1Bundle(bundle, observedEarly))

	// Bundle retained but host validity not yet recorded: still not ready.
	select {
	case <-nonLeader.L0ReadyCh():
		t.Fatalf("L0ReadyCh closed before host validity recorded")
	default:
	}

	require.NoError(t, nonLeader.ApplyHostValidity(0, s.candidates[0], true))

	// Both retained + host-validated: ready.
	select {
	case <-nonLeader.L0ReadyCh():
	default:
		t.Fatalf("L0ReadyCh did not close after bundle+host-validity recorded")
	}
}

func TestL0Ready_NonLeader_HostNotValid(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0)
	bundle, err := s.instances[leaderID].BuildPhase1Bundle(0, s.candidates[0])
	require.NoError(t, err)

	var nonLeaderID OperatorID
	for _, op := range s.allOperators() {
		if op != leaderID {
			nonLeaderID = op
			break
		}
	}
	nonLeader := s.instances[nonLeaderID]
	require.NoError(t, nonLeader.ObservePhase1Bundle(bundle, observedEarly))
	require.NoError(t, nonLeader.ApplyHostValidity(0, s.candidates[0], false))

	// Host returned not-valid → operator's L_0 decision is NV (NR-equivalent).
	// The trigger should fire — the decision is determinable.
	select {
	case <-nonLeader.L0ReadyCh():
	default:
		t.Fatalf("L0ReadyCh did not close when host returned not-valid on retained V")
	}
}

func TestL0Ready_Equivocation_ForcesReady(t *testing.T) {
	s := newSim(t, 4)
	vA := []byte("L_0 candidate A")
	vB := []byte("L_0 candidate B")
	leaderID := s.leaderAt(0)

	var nonLeaderID OperatorID
	for _, op := range s.allOperators() {
		if op != leaderID {
			nonLeaderID = op
			break
		}
	}
	nonLeader := s.instances[nonLeaderID]
	// Build both bundles via the equivocation helper to bypass leader's EKM.
	s.deliverPhase1Equivocation(0, vA, vB, []OperatorID{nonLeaderID}, []OperatorID{nonLeaderID}, observedEarly, true)

	// ≥ 2 distinct V's retained → forced NR per cross-phase exclusivity →
	// trigger fires regardless of host validity.
	select {
	case <-nonLeader.L0ReadyCh():
	default:
		t.Fatalf("L0ReadyCh did not close when equivocation observed at L_0")
	}
}

func TestL0Ready_L0Leader_BuildPhase1Bundle(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0)
	leader := s.instances[leaderID]

	// Before BuildPhase1Bundle: not ready.
	select {
	case <-leader.L0ReadyCh():
		t.Fatalf("L0ReadyCh closed before L_0 leader built their bundle")
	default:
	}

	_, err := leader.BuildPhase1Bundle(0, s.candidates[0])
	require.NoError(t, err)

	// L_0 leader's Phase-1 σ_V counts as their σ-commit at L_0
	// (sigmaLocked[0] = true via transitionToSigma in BuildPhase1Bundle).
	// Trigger should fire even without ApplyHostValidity — the leader
	// implicitly validated V via their fetch path.
	select {
	case <-leader.L0ReadyCh():
	default:
		t.Fatalf("L0ReadyCh did not close after L_0 leader's BuildPhase1Bundle")
	}
}

func TestL0Ready_DeeperLayerObservation_NoTrigger(t *testing.T) {
	s := newSim(t, 4)
	// Deliver L_1 bundle to all — should NOT close L0ReadyCh on the L_1
	// non-leaders (only L_0 observations gate the L_0 trigger). The L_1
	// leader's BuildPhase1Bundle for L_1 sets sigmaLocked[1], not [0]; the
	// L_0 leader and non-L_1-leader operators observe L_1's bundle, also
	// not affecting L_0 state.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly, true)
	for _, inst := range s.instances {
		// Skip the L_0 leader: their L_0 trigger fires only when they
		// build their own L_0 bundle, which this test doesn't do.
		if inst.cfg.Layers[0].Leader == inst.ownOperatorID {
			continue
		}
		select {
		case <-inst.L0ReadyCh():
			t.Fatalf("L0ReadyCh closed after L_1 (not L_0) observation")
		default:
		}
	}
}

package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Tests for the L_0-trigger early-commit signals (spec docs/2abOBFT.md §Phase 2a
// emission-timing + §Phase 2b emission). L0VerdictReadyCh closes when the
// operator can determine their L_0 Phase-2a verdict; L0SigmaEligibilityCh
// closes when their local verdict-pool at L_0 has ≥ qV σV verdicts for some V.

func TestL0VerdictReady_InitiallyOpen(t *testing.T) {
	s := newSim(t, 4)
	for _, inst := range s.instances {
		select {
		case <-inst.L0VerdictReadyCh():
			t.Fatalf("L0VerdictReadyCh closed before any Phase-1 observation")
		default:
		}
		select {
		case <-inst.L0SigmaEligibilityCh():
			t.Fatalf("L0SigmaEligibilityCh closed before any verdict observation")
		default:
		}
	}
}

func TestL0VerdictReady_NonLeader_BundleAndValidity(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader is op1; pick op2 as the non-leader subject.
	const subject = OperatorID(2)
	nonLeader := s.instances[subject]

	bundle := s.deliverPhase1(0, Value("V0"), []OperatorID{subject}, observedEarly)
	require.NotNil(t, bundle)

	// Bundle retained but host validity not yet recorded: not ready.
	select {
	case <-nonLeader.L0VerdictReadyCh():
		t.Fatalf("L0VerdictReadyCh closed before host validity recorded")
	default:
	}

	require.NoError(t, nonLeader.ApplyHostValidity(0, Value("V0"), true))

	select {
	case <-nonLeader.L0VerdictReadyCh():
	default:
		t.Fatalf("L0VerdictReadyCh did not close after bundle+host-validity recorded")
	}
}

func TestL0VerdictReady_AuthOnlyDoesNotTrigger(t *testing.T) {
	s := newSim(t, 4)
	const subject = OperatorID(2)
	nonLeader := s.instances[subject]

	// Deliver bundle past T_accept_max → auth-only retention. Per spec
	// §Phase 1 / "Auth-only retention does not allow the operator to issue
	// a KindVerdict based on this bundle" — trigger must NOT fire.
	s.deliverPhase1(0, Value("V0"), []OperatorID{subject}, observedAuthOnly)
	require.NoError(t, nonLeader.ApplyHostValidity(0, Value("V0"), true))

	select {
	case <-nonLeader.L0VerdictReadyCh():
		t.Fatalf("L0VerdictReadyCh closed on auth-only retention — verdict-eligibility requires regular retention")
	default:
	}
}

func TestL0VerdictReady_L0Leader_StandardPath(t *testing.T) {
	s := newSim(t, 4)
	// L_0 leader is op1 in the default sim layout. 2abOBFT has no Phase-1
	// σ_V (Variant C), so the L_0 leader's trigger fires via the standard
	// retention + host-validity path: build bundle, self-observe via
	// ObservePhase1Bundle, ApplyHostValidity on own V.
	leaderID := s.leaderAt(0)
	leader := s.instances[leaderID]

	// Build + self-observe (the runner pattern: leader observes own bundle
	// so it gets retained and contributes to verdict computation).
	bundle, err := leader.BuildPhase1Bundle(0, Value("V0"))
	require.NoError(t, err)
	require.NoError(t, leader.ObservePhase1Bundle(bundle, observedEarly))

	// Before ApplyHostValidity: not ready (bundle retained but no host
	// verdict yet).
	select {
	case <-leader.L0VerdictReadyCh():
		t.Fatalf("L0VerdictReadyCh closed before host validity recorded")
	default:
	}

	require.NoError(t, leader.ApplyHostValidity(0, Value("V0"), true))

	// Now both retained + host-validated for L_0 leader.
	select {
	case <-leader.L0VerdictReadyCh():
	default:
		t.Fatalf("L0VerdictReadyCh did not close for L_0 leader after self-observe + validity")
	}
}

func TestL0VerdictReady_Equivocation(t *testing.T) {
	s := newSim(t, 4)
	const subject = OperatorID(2)
	nonLeader := s.instances[subject]

	// Deliver two distinct V's from the L_0 leader to subject — equivocation
	// observed → forced NR per cross-phase exclusivity → trigger fires
	// regardless of host validity.
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		[]OperatorID{subject}, []OperatorID{subject}, observedEarly)

	select {
	case <-nonLeader.L0VerdictReadyCh():
	default:
		t.Fatalf("L0VerdictReadyCh did not close when leader equivocation observed at L_0")
	}
}

func TestL0SigmaEligibility_QVσVVerdicts(t *testing.T) {
	s := newSim(t, 4)
	// Subject is op4 (not L_0 leader = op1). Build subject's own L_0
	// verdict + 2 peer σV verdicts on the same V → qV=3 σV count met.
	const subject = OperatorID(4)
	subjectInst := s.instances[subject]

	// Subject observes V at L_0 and validates it (so subject's own verdict
	// will be σV(V0)).
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	// Subject builds own verdict (σV on V0).
	ownVerdict, err := subjectInst.BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, ownVerdict.Kind)

	// Build + deliver 2 peer σV verdicts to subject.
	peer1Verdict, err := s.instances[OperatorID(1)].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, peer1Verdict.Kind)
	peer2Verdict, err := s.instances[OperatorID(2)].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, peer2Verdict.Kind)

	require.NoError(t, subjectInst.ObserveVerdict(peer1Verdict))

	// After 2 σV verdicts (own + peer1), σ-count = 2 < qV=3 — not ready.
	select {
	case <-subjectInst.L0SigmaEligibilityCh():
		t.Fatalf("L0SigmaEligibilityCh closed before qV σV verdicts observed")
	default:
	}

	require.NoError(t, subjectInst.ObserveVerdict(peer2Verdict))

	// Now 3 σV verdicts on V0 (own + peer1 + peer2) = qV → trigger fires.
	select {
	case <-subjectInst.L0SigmaEligibilityCh():
	default:
		t.Fatalf("L0SigmaEligibilityCh did not close after qV σV verdicts in pool")
	}
}

func TestL0SigmaEligibility_NRVerdictsDoNotCount(t *testing.T) {
	s := newSim(t, 4)
	const subject = OperatorID(4)
	subjectInst := s.instances[subject]

	// Subject builds own NR verdict (no bundle observed at L_0).
	ownVerdict, err := subjectInst.BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNR, ownVerdict.Kind)

	// Deliver 3 peer NR verdicts to subject — NR count grows but σ-count
	// stays at 0. Trigger should NOT fire.
	for _, op := range []OperatorID{1, 2, 3} {
		peerVerdict, err := s.instances[op].BuildVerdict(0)
		require.NoError(t, err)
		require.Equal(t, VerdictNR, peerVerdict.Kind)
		require.NoError(t, subjectInst.ObserveVerdict(peerVerdict))
	}

	select {
	case <-subjectInst.L0SigmaEligibilityCh():
		t.Fatalf("L0SigmaEligibilityCh closed on NR-only verdict-pool")
	default:
	}
}

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

// Spec §Phase 2 / Peer-reflood V via early commit: a V-drop receiver
// (no Phase-1 bundle locally at L_0) can early-emit σ_i^V on a V learned
// from a peer's KindCommit when the leader's σ_L^V witness verifies
// against that V. Closes the h_V=1 selective-delivery deadlock for bare
// OBFT.
//
// Setup pattern across the peer-V tests:
//   - L_0 leader builds + self-observes its Phase-1 bundle.
//   - One honest non-leader observes the bundle directly (Phase-1 receipt)
//     and host-validates, then builds + self-emits a KindCommit carrying
//     V at L_0 plaintext + σ_L^V witness.
//   - The V-drop receiver (didn't observe the Phase-1 bundle) receives
//     that peer commit and drives the peer-reflood-V path.

// TestL0Ready_PeerVOnly_TriggersReady — V-drop receiver gets V via peer
// commit + host validates → L0ReadyCh fires.
func TestL0Ready_PeerVOnly_TriggersReady(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0) // op 1
	// op 2 = honest forwarder (gets bundle directly).
	// op 3 = V-drop receiver (we test this one).
	const forwarder OperatorID = 2
	const vDrop OperatorID = 3

	// Leader builds + self-observes; forwarder observes + host-validates.
	s.deliverPhase1(0, s.candidates[0], []OperatorID{leaderID, forwarder}, observedEarly, true)
	// Make sure the leader and forwarder also have host validity on the
	// other layers' bundles so BuildOwnCommit doesn't err on deeper layers.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	// V-drop receiver: no L_0 bundle locally, so L0ReadyCh stays open.
	vDropInst := s.instances[vDrop]
	select {
	case <-vDropInst.L0ReadyCh():
		t.Fatalf("V-drop receiver's L0ReadyCh closed before peer-V observation")
	default:
	}

	// Forwarder builds + observes its own Commit so peerOnions[0][forwarder]
	// has V_0 plaintext + Witnesses carries (L_0, leader, ValueRoot(V_0), σ_L^V).
	forwarderCommit, err := s.instances[forwarder].BuildOwnCommit()
	require.NoError(t, err)

	// V-drop observes the peer commit.
	require.NoError(t, vDropInst.ObserveCommit(forwarderCommit))

	// V-drop's L0ReadyCh STILL stays open at this point — peer-V is observed
	// but host hasn't validated yet. The Instance should have enqueued a
	// validation request.
	select {
	case <-vDropInst.L0ReadyCh():
		t.Fatalf("V-drop's L0ReadyCh closed before host-validity on peer-V")
	default:
	}
	select {
	case req := <-vDropInst.WantsHostValidationCh():
		require.Equal(t, 0, req.Layer)
		require.Equal(t, Value(s.candidates[0]), req.Value)
	default:
		t.Fatalf("V-drop didn't enqueue a host-validation request on peer-V")
	}

	// Runner-equivalent: apply host validity. Now L0ReadyCh fires on the
	// σ-via-peer-V path.
	require.NoError(t, vDropInst.ApplyHostValidity(0, s.candidates[0], true))
	select {
	case <-vDropInst.L0ReadyCh():
	default:
		t.Fatalf("V-drop's L0ReadyCh did not close after host-validates peer-V")
	}
}

// TestL0Ready_PeerVMissingWitness_NoTrigger — peer commit carries V at
// L_0 plaintext but no valid σ_L^V witness. V-drop receiver should NOT
// trigger σ-via-peer-V (witness gate blocks byz-fabricated V).
func TestL0Ready_PeerVMissingWitness_NoTrigger(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0)
	const forwarder OperatorID = 2
	const vDrop OperatorID = 3

	// Forwarder observes deeper layers' bundles + host-validates so they
	// can BuildOwnCommit. Crucially we DON'T deliver the L_0 bundle to
	// forwarder — so forwarder's commit has no L_0 σ-onion entry (NR-side)
	// and no L_0 witness.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	// Leader self-builds L_0 (forwarder doesn't see it).
	_, err := s.instances[leaderID].BuildPhase1Bundle(0, s.candidates[0])
	require.NoError(t, err)

	// Forwarder commits (NR on L_0 because no V observed; no L_0 witness
	// because no L_0 bundle retained). Crucially the wire format will
	// carry V from the LEADER's commit only — not from this forwarder's.
	forwarderCommit, err := s.instances[forwarder].BuildOwnCommit()
	require.NoError(t, err)
	// Sanity-check the commit has no L_0 σ-entry (empty Value).
	require.Equal(t, 0, len(forwarderCommit.Layers[0].Value),
		"forwarder should have NR'd L_0 (no V observed)")

	vDropInst := s.instances[vDrop]
	require.NoError(t, vDropInst.ObserveCommit(forwarderCommit))
	select {
	case <-vDropInst.L0ReadyCh():
		t.Fatalf("V-drop L0ReadyCh fired without any peer-V available")
	default:
	}
	select {
	case req := <-vDropInst.WantsHostValidationCh():
		t.Fatalf("V-drop unexpectedly enqueued validation request: %+v", req)
	default:
	}
}

// TestL0Ready_CrossSourceEquivocation_NoSigma — operator has Phase-1
// bundle V_a locally retained + host-validated, AND observes a peer
// commit with V_b at L_0 (via leader equivocation that reached different
// honest operators). Cross-phase exclusivity forces NR even though
// chosenVForLayer's primary path would otherwise return V_a — the
// equivocation gate detects V_a + V_b across sources.
func TestL0Ready_CrossSourceEquivocation_NoSigma(t *testing.T) {
	s := newSim(t, 4)
	vA := []byte("L_0 V_a")
	vB := []byte("L_0 V_b")
	leaderID := s.leaderAt(0)
	// op 2 gets V_a (retains bundle); op 3 gets V_b (retains bundle).
	// op 4 is our test subject: gets V_a directly + observes op 3's commit
	// (which carries V_b at L_0 plaintext + σ_L^V_b witness).
	const opA OperatorID = 2
	const opB OperatorID = 3
	const subject OperatorID = 4
	s.deliverPhase1Equivocation(0, vA, vB,
		[]OperatorID{leaderID, opA, subject}, []OperatorID{opB},
		observedEarly, true)
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	commitB, err := s.instances[opB].BuildOwnCommit()
	require.NoError(t, err)

	subjectInst := s.instances[subject]
	require.NoError(t, subjectInst.ObserveCommit(commitB))

	// Subject's bundles[0] has V_a (1 entry); peerOnions[0] has V_b from
	// op_B; witnessedLeaderSigma[0] has V_b (harvested from op_B's commit
	// witness, verified against the V_b plaintext in op_B's σ-onion). The
	// local bundle's σ_V on V_a stays in bundles[0][leader] (not in
	// witnessedLeaderSigma — that map only holds peer-harvested σ_L^V's).
	// distinctVCountAtLayer counts {V_a, V_b} = 2 → equivocation gate
	// fires in chosenVForLayer, forcing NR.
	subjectCommit, err := subjectInst.BuildOwnCommit()
	require.NoError(t, err)
	require.Equal(t, 0, len(subjectCommit.Layers[0].Value),
		"subject must NR on L_0 under cross-source equivocation, not σ on V_a")
	// L0Ready also closes (equivocation predicate).
	select {
	case <-subjectInst.L0ReadyCh():
	default:
		t.Fatalf("L0Ready did not close on cross-source equivocation")
	}
}

// TestL0Ready_PeerVEquivocation_ForcesReady — two peers send commits with
// distinct V's at L_0 (leader equivocation). L0ReadyCh fires immediately
// on the equivocation branch (forced NR).
func TestL0Ready_PeerVEquivocation_ForcesReady(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0) // op 1
	vA := []byte("L_0 V_a")
	vB := []byte("L_0 V_b")
	// op 2 observes V_a; op 3 observes V_b. Both build commits.
	// op 4 is the V-drop receiver.
	const fA OperatorID = 2
	const fB OperatorID = 3
	const vDrop OperatorID = 4
	s.deliverPhase1Equivocation(0, vA, vB,
		[]OperatorID{leaderID, fA}, []OperatorID{fB},
		observedEarly, true)
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	commitA, err := s.instances[fA].BuildOwnCommit()
	require.NoError(t, err)
	commitB, err := s.instances[fB].BuildOwnCommit()
	require.NoError(t, err)

	vDropInst := s.instances[vDrop]
	require.NoError(t, vDropInst.ObserveCommit(commitA))
	require.NoError(t, vDropInst.ObserveCommit(commitB))

	// Two distinct V's observable across peerOnions/witnesses →
	// equivocation → NR ready.
	select {
	case <-vDropInst.L0ReadyCh():
	default:
		t.Fatalf("V-drop L0ReadyCh did not close on peer-V equivocation (NR ready)")
	}
}

// TestHV1SelectiveDelivery_PeerVRecovery is the end-to-end demonstration
// that §1's peer-reflood-V mechanism closes the h_V=1 selective-delivery
// deadlock. Setup mirrors the consensustest HV1SelectiveDelivery scenario:
// byz L_0 leader delivers V to exactly 1 honest non-leader (n=4 f=1).
// Pre-§1 baseline: σ-pool at L_0 = 2 (leader σ_L^V + 1 honest σ_i^V) <
// qV=3, NR-pool = 2 < qEnc=3, slot misses at L_0 with no fall-through.
// Post-§1: V-drop receivers σ on peer-harvested V → σ-pool reaches qV.
//
// Drives the event ordering manually (the consensustest framework's
// sync-emit model doesn't yet exercise early-emit + peer-V; that's a
// follow-up framework enhancement).
func TestHV1SelectiveDelivery_PeerVRecovery(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0)       // op 1 = byz leader
	const fwdID OperatorID = 2      // honest who got V
	const drop3 OperatorID = 3      // V-drop receiver
	const drop4 OperatorID = 4      // V-drop receiver
	v0 := s.candidates[0]

	// Leader builds + self-σ-V (Phase-1 σ_V is leader's σ commitment).
	// Selective delivery: only fwd observes the Phase-1 bundle.
	s.deliverPhase1(0, v0, []OperatorID{leaderID, fwdID}, observedEarly, true)
	// Backups: deliver to all so deeper layers can build their commits
	// (their σ on L_k for k>0 is orthogonal to the L_0 σ-pool).
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}

	// Phase-2 emit ordering matching early-emit semantics: the L_0-V-holder
	// (fwd) emits FIRST; V-drop receivers observe peer commit → drain
	// validation request → emit later.
	fwdCommit, err := s.instances[fwdID].BuildOwnCommit()
	require.NoError(t, err)

	// V-drops observe fwd's commit.
	for _, drop := range []OperatorID{drop3, drop4} {
		dropInst := s.instances[drop]
		require.NoError(t, dropInst.ObserveCommit(fwdCommit))
		// Drain the validation request and apply verdict (mirrors runner).
		select {
		case req := <-dropInst.WantsHostValidationCh():
			require.Equal(t, 0, req.Layer)
			require.NoError(t, dropInst.ApplyHostValidity(req.Layer, req.Value, true))
		default:
			t.Fatalf("op %d did not enqueue validation request on peer-V", drop)
		}
	}

	// V-drops now build their commits. Per §1, they σ-emit on the
	// peer-harvested V at L_0.
	drop3Commit, err := s.instances[drop3].BuildOwnCommit()
	require.NoError(t, err)
	drop4Commit, err := s.instances[drop4].BuildOwnCommit()
	require.NoError(t, err)
	require.Equal(t, Value(v0), drop3Commit.Layers[0].Value, "drop3 should σ-emit on peer-V at L_0")
	require.Equal(t, Value(v0), drop4Commit.Layers[0].Value, "drop4 should σ-emit on peer-V at L_0")

	// Byz leader emits no commit (silent at L_0). Cross-observe the three
	// honest commits.
	allCommits := map[OperatorID]*Commit{fwdID: fwdCommit, drop3: drop3Commit, drop4: drop4Commit}
	for receiver, inst := range s.instances {
		for sender, c := range allCommits {
			if sender == receiver {
				continue
			}
			require.NoError(t, inst.ObserveCommit(c))
		}
	}

	// Resolve at fwd (any of the three honest works). σ-pool at L_0 =
	// leader σ_L^V (harvested via fwd's witness) + 3 honest σ_i^V = 4 ≥
	// qV=3. Slot decides at L_0.
	out, err := s.instances[fwdID].Resolve()
	require.NoError(t, err, "h_V=1 should recover via peer-V at L_0")
	require.Equal(t, 0, out.Layer, "decision should be at L_0 (peer-reflood-V recovery)")
	require.Equal(t, Value(v0), out.Value)
}

// TestBuildOwnCommit_PeerVPath_EmitsSigma — full end-to-end path: V-drop
// receiver harvests V from peer commit, host-validates, then BuildOwnCommit
// emits σ_i^V at L_0 on the peer-harvested V.
func TestBuildOwnCommit_PeerVPath_EmitsSigma(t *testing.T) {
	s := newSim(t, 4)
	leaderID := s.leaderAt(0) // op 1
	const forwarder OperatorID = 2
	const vDrop OperatorID = 3

	s.deliverPhase1(0, s.candidates[0], []OperatorID{leaderID, forwarder}, observedEarly, true)
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly, true)
	}
	forwarderCommit, err := s.instances[forwarder].BuildOwnCommit()
	require.NoError(t, err)

	vDropInst := s.instances[vDrop]
	require.NoError(t, vDropInst.ObserveCommit(forwarderCommit))
	// Drain the validation request (mirrors runner behavior).
	select {
	case req := <-vDropInst.WantsHostValidationCh():
		require.NoError(t, vDropInst.ApplyHostValidity(req.Layer, req.Value, true))
	default:
		t.Fatalf("expected validation request on V-drop")
	}

	// V-drop BuildOwnCommit should include σ_i^V at L_0 (plaintext Value =
	// the peer-harvested V).
	vDropCommit, err := vDropInst.BuildOwnCommit()
	require.NoError(t, err)
	require.Equal(t, Value(s.candidates[0]), vDropCommit.Layers[0].Value,
		"V-drop should σ-emit on the peer-harvested V at L_0")
	require.NotEmpty(t, vDropCommit.Layers[0].Ciphertext,
		"V-drop's L_0 σ-onion entry should have a partial-sig ciphertext")
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

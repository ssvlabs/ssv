package twoab

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// runHealthySlot drives a full healthy slot: every op retains V_k at
// every layer, host-validates all, broadcasts verdicts, broadcasts
// Onion2b. After this helper, every Instance is in a state where
// Resolve should produce an Output at L_0.
func runHealthySlot(t *testing.T, s *sim) {
	t.Helper()
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()
}

// ---------- Healthy reconstruction at L_0 ----------

func TestResolve_HealthyClusterReachesSigmaAtL0(t *testing.T) {
	s := newSim(t, 4)
	runHealthySlot(t, s)

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d Resolve", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 0, out.Layer, "should reach σ-quorum at L_0")
	require.True(t, bytes.Equal(out.Value, s.candidates[0]))
	require.NotEmpty(t, out.Signature, "aggregate signature populated")
}

// Resolve is idempotent — re-running returns the same Output.
func TestResolve_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	runHealthySlot(t, s)

	out1, err := s.instances[2].Resolve()
	require.NoError(t, err)
	out2, err := s.instances[2].Resolve()
	require.NoError(t, err)
	require.Equal(t, out1.Layer, out2.Layer)
	require.True(t, bytes.Equal(out1.Value, out2.Value))
	require.True(t, bytes.Equal(out1.Signature, out2.Signature))
}

// ---------- Fall-through scenarios ----------

// h_V = 0 at L_0 (no operator retains V0). All ops verdict NR at L_0
// → NR-quorum at L_0 → walk advances to L_1, where everyone σ's.
func TestResolve_FallsThroughL0ToL1WhenNoV0(t *testing.T) {
	s := newSim(t, 4)
	// Skip L_0 delivery entirely — no operator retains V0.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "fall-through to L_1")
	require.True(t, bytes.Equal(out.Value, s.candidates[1]))
}

// All layers silent → walk exhausts → ErrNoQuorum.
func TestResolve_NoVAtAnyLayer_NoQuorum(t *testing.T) {
	s := newSim(t, 4)
	// No deliveries at any layer.
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op := range errs {
		require.Nil(t, outputs[op], "op %d should not have an output", op)
		require.ErrorIs(t, errs[op], ErrNoQuorum)
	}
}

// Equivocation at L_0 (all-honest-NR shape: every honest retains 2 V's,
// every honest σ-locked-short→ NR) → fall-through to L_1.
func TestResolve_EquivocationAtL0FallsThrough(t *testing.T) {
	s := newSim(t, 4)
	// Byzantine op1 (L_0 leader) sends V_a + V_b to all non-leaders →
	// every honest retains 2 distinct V's → row 1 of convergence rule
	// fires (equivocation observed → NR) for every honest at L_0.
	s.deliverPhase1Equivocation(0,
		Value("V_a"), Value("V_b"),
		s.allOperators(), s.allOperators(),
		observedEarly)
	// L_1, L_2, L_3 healthy.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer,
		"equivocation at L_0 → all honest NR → NR-quorum → fall-through")
}

// Multi-layer fall-through: L_0 + L_1 + L_2 all NR, σ at L_3 (deepest).
func TestResolve_MultiLayerFallThrough(t *testing.T) {
	s := newSim(t, 4)
	// Only L_3 broadcasts.
	s.deliverPhase1(3, s.candidates[3], s.allOperators(), observedEarly)
	s.applyHostValidityAll(3, s.candidates[3], true)
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 3, out.Layer, "K=4 fall-through reaches L_3")
}

// h_V = 2 at L_0 (2-of-4 retain V0): σ-pool = 2 < qV = 3, NR-pool = 2 <
// qEnc = 3 at L_0. Per Phase G convergence row 5 → all honest NR. NR-pool
// at Phase 2b = 4 = qEnc → walk advances to L_1.
func TestResolve_HV2FallsThroughViaNRQuorum(t *testing.T) {
	s := newSim(t, 4)
	// Only op1 and op2 retain V0; op3/op4 don't.
	s.deliverPhase1(0, s.candidates[0], []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, s.candidates[0], true)
	// L_1 onwards healthy.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	outputs, errs := s.resolveAll()
	for op, err := range errs {
		require.NoError(t, err, "op %d", op)
	}
	out := requireAllAgree(t, outputs)
	require.Equal(t, 1, out.Layer, "h_V=2 falls through to L_1 (no σ_V head-start in 2ab)")
}

// ---------- Rule 4 (fake encrypted-presence) ----------

// Byzantine op emits a chained-encrypted σ entry at L_1 with garbage
// ciphertext that decrypts to non-σ-partial bytes. After NR-quorum at
// L_0 unlocks decryption, Rule 4 fires.
func TestResolve_Rule4FakeEncryptedPresenceAtL1(t *testing.T) {
	s := newSim(t, 4)
	// Healthy at L_0 → eventually σ-quorum should reach. But to force
	// the walk past L_0, set up scenario where L_0 doesn't reach
	// σ-quorum but NR-quorum advances; then Rule 4 fires at L_1.
	//
	// Strategy: no L_0 delivery → all-NR at L_0 → NR-quorum advances
	// → L_1 onions get decrypted; byzantine's L_1 entry decrypts to
	// garbage.

	// L_1 — only op2/3/4 retain V_1 (3-of-4 → σ-quorum).
	s.deliverPhase1(1, s.candidates[1], []OperatorID{2, 3, 4}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2, 3, 4}, 1, s.candidates[1], true)
	// Add some retention at L_2/L_3 so they don't block.
	for k := 2; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}

	s.runPhase2aCanonical()

	// op1 doesn't σ-emit at L_1 (no V_1 retained). Construct op1's
	// onion manually to inject garbage σ entry at L_1.
	o1Genuine, err := s.instances[1].BuildOwnOnion2b()
	require.NoError(t, err)
	// op1 normally has NR at L_0 / L_1 / L_2 in this scenario. Tamper:
	// replace L_1 entry with a σ-claim using garbage ciphertext, AND
	// drop op1's NR partial at L_1 so the same layer doesn't fire Rule 1.
	tampered := deepCopyOnion2b(o1Genuine)
	tampered.Layers[1] = EncryptedLayer{
		Value:      s.candidates[1],
		Ciphertext: []byte("garbage-that-wont-decrypt-to-σ"),
	}
	// Remove op1's NR partial at L_1 (so Rule 1 doesn't co-fire and obscure Rule 4).
	filtered := tampered.NRPartials[:0]
	for _, p := range tampered.NRPartials {
		if p.Layer != 1 {
			filtered = append(filtered, p)
		}
	}
	tampered.NRPartials = filtered

	// Other ops build normally.
	for _, op := range []OperatorID{2, 3, 4} {
		_, err := s.instances[op].BuildOwnOnion2b()
		require.NoError(t, err)
	}

	// Deliver: op2 observes everyone's onions. op2 observes op1's
	// tampered onion (not the genuine one).
	for _, op := range []OperatorID{2, 3, 4} {
		o, _ := s.instances[op].OwnOnion2b()
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveOnion2b(o))
		}
	}
	for _, peer := range s.allOperators() {
		if peer == 1 {
			continue
		}
		require.NoError(t, s.instances[peer].ObserveOnion2b(tampered))
	}

	// op2 Resolves. Walk: L_0 σ-pool short (no V_0); NR-quorum at L_0
	// → advance to L_1. At L_1, op1's encrypted entry decrypts to
	// garbage → Rule 4 fires for op 1 / L_1.
	out, err := s.instances[2].Resolve()
	// Honest ops 2/3/4 σ-quorum at L_1 — slot succeeds.
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, 1, out.Layer)

	// Rule 4 evidence recorded.
	foundRule4 := false
	for _, e := range s.instances[2].Evidence() {
		if e.Rule == EvidenceFakeEncryptedPresence && e.OperatorID == 1 && e.Layer == 1 {
			foundRule4 = true
			require.NotNil(t, e.FakeEncryptedPresence)
			break
		}
	}
	require.True(t, foundRule4, "Rule 4 fires when byzantine's encrypted entry decrypts to garbage")
}

// ---------- BuildCertificate / ObserveCertificate ----------

func TestBuildCertificate_HappyPath(t *testing.T) {
	s := newSim(t, 4)
	runHealthySlot(t, s)
	out, err := s.instances[2].Resolve()
	require.NoError(t, err)

	cert, err := s.instances[2].BuildCertificate(out)
	require.NoError(t, err)
	require.Equal(t, s.cfg.ClusterID, cert.ClusterID)
	require.Equal(t, s.cfg.Height, cert.Height)
	require.True(t, bytes.Equal(cert.Value, out.Value))
	require.True(t, bytes.Equal(cert.Signature, out.Signature))
}

func TestBuildCertificate_RejectsNilOutput(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[2].BuildCertificate(nil)
	require.ErrorContains(t, err, "nil output")
}

func TestBuildCertificate_RejectsEmptyValueOrSignature(t *testing.T) {
	s := newSim(t, 4)
	_, err := s.instances[2].BuildCertificate(&Output{Layer: 0, Value: nil, Signature: Signature("sig")})
	require.ErrorContains(t, err, "empty")
	_, err = s.instances[2].BuildCertificate(&Output{Layer: 0, Value: Value("V"), Signature: nil})
	require.ErrorContains(t, err, "empty")
}

func TestObserveCertificate_HealthyRoundTrip(t *testing.T) {
	s := newSim(t, 4)
	runHealthySlot(t, s)
	out, err := s.instances[2].Resolve()
	require.NoError(t, err)
	cert, err := s.instances[2].BuildCertificate(out)
	require.NoError(t, err)

	// op1 observes op2's certificate.
	require.NoError(t, s.instances[1].ObserveCertificate(cert))
	retained := s.instances[1].RetainedCertificate()
	require.NotNil(t, retained)
	require.True(t, bytes.Equal(retained.Value, cert.Value))
	require.True(t, bytes.Equal(retained.Signature, cert.Signature))
}

func TestObserveCertificate_RejectsBadSignature(t *testing.T) {
	s := newSim(t, 4)
	cert := &Certificate{
		ClusterID: s.cfg.ClusterID,
		Height:    s.cfg.Height,
		Value:     Value("V"),
		Signature: Signature("not-a-valid-aggregate"),
	}
	err := s.instances[1].ObserveCertificate(cert)
	require.ErrorContains(t, err, "does not verify")
}

// Second valid cert observation is silent dedup.
func TestObserveCertificate_FirstWinsDedup(t *testing.T) {
	s := newSim(t, 4)
	runHealthySlot(t, s)
	out, err := s.instances[2].Resolve()
	require.NoError(t, err)
	cert, err := s.instances[2].BuildCertificate(out)
	require.NoError(t, err)

	require.NoError(t, s.instances[1].ObserveCertificate(cert))
	require.NoError(t, s.instances[1].ObserveCertificate(cert)) // dedup
	require.NoError(t, s.instances[1].ObserveCertificate(cert)) // dedup
	retained := s.instances[1].RetainedCertificate()
	require.NotNil(t, retained)
}

func TestRetainedCertificate_NilBeforeObserve(t *testing.T) {
	s := newSim(t, 4)
	require.Nil(t, s.instances[1].RetainedCertificate())
}

// ---------- Late-onion incorporation ----------

// An operator who initially fails to Resolve (lacks enough partials)
// can re-run Resolve after a late peer's onion arrives.
func TestResolve_LateOnionIncorporated(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()

	// Build every op's onion but only deliver to op1 from 3 ops (not yet
	// enough for σ-quorum at op1 if we count own + 2 peers = 3 = qV...
	// actually that IS enough. Need to deliver from only 1 peer: own + 1
	// peer = 2 < qV = 3).
	o2, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	_, err = s.instances[3].BuildOwnOnion2b()
	require.NoError(t, err)
	_, err = s.instances[4].BuildOwnOnion2b()
	require.NoError(t, err)
	o1, err := s.instances[1].BuildOwnOnion2b()
	require.NoError(t, err)

	// Deliver only op2's onion to op1 first.
	require.NoError(t, s.instances[1].ObserveOnion2b(o2))
	out, err := s.instances[1].Resolve()
	require.ErrorIs(t, err, ErrNoQuorum, "own + 1 peer = 2 partials < qV = 3")
	require.Nil(t, out)

	// Now op3's onion arrives — should reach σ-quorum.
	o3, _ := s.instances[3].OwnOnion2b()
	require.NoError(t, s.instances[1].ObserveOnion2b(o3))
	out, err = s.instances[1].Resolve()
	require.NoError(t, err)
	require.NotNil(t, out)
	require.Equal(t, 0, out.Layer)

	// Use o1 + o4 to silence unused-var warnings.
	_ = o1
	_, _ = s.instances[4].OwnOnion2b()
}

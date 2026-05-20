package twoab

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

// ---------- ConvergenceDecision — 5-row decision table ----------

// Row 1: equivocation observed → NR.
func TestConvergence_Row1_EquivocationObserved_NR(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1Equivocation(0, Value("V_a"), Value("V_b"),
		[]OperatorID{2}, []OperatorID{2}, observedEarly)

	choice, v, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice)
	require.Nil(t, v)
}

// Row 2: nr_eligibility_quorum reached → NR (regardless of own verdict).
func TestConvergence_Row2_NRQuorum_NR(t *testing.T) {
	s := newSim(t, 4)
	// Make op2 σ-eligible on V0 (host valid, V retained), but other
	// operators broadcast NR — pushing nr_pool to qEnc.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	// op1 / op3 / op4 broadcast NR verdicts (no V retained at op1/3/4).
	mkNR := func(op OperatorID) *Verdict {
		return &Verdict{
			ClusterID:  s.cfg.ClusterID,
			OperatorID: op,
			Height:     s.cfg.Height,
			Layer:      0,
			Kind:       VerdictNR,
		}
	}
	require.NoError(t, s.instances[2].ObserveVerdict(mkNR(1)))
	require.NoError(t, s.instances[2].ObserveVerdict(mkNR(3)))
	require.NoError(t, s.instances[2].ObserveVerdict(mkNR(4)))

	// op2's own verdict is σV (1 V retained + host valid). But nr_pool
	// from op1/3/4 = 3 = qEnc → row 2 fires → NR.
	choice, v, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice,
		"row 2: nr_eligibility_quorum overrides own σV eligibility")
	require.Nil(t, v)
}

// Row 3: σ_eligibility_quorum + own V matches + host re-validates → σ.
func TestConvergence_Row3_SigmaQuorumOnOwnV_Sigma(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()

	choice, v, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceSigma, choice)
	require.True(t, bytes.Equal(v, Value("V0")))
}

// Row 4: σ_eligibility on V but operator doesn't have V_local = V → NR.
func TestConvergence_Row4_SigmaQuorumWithoutOwnV_NR(t *testing.T) {
	s := newSim(t, 4)
	// op1, op3, op4 retain V0 and broadcast σV; op2 doesn't retain V0.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 3, 4}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 3, 4}, 0, Value("V0"), true)

	// All four operators build + broadcast verdicts.
	for _, op := range []OperatorID{1, 3, 4} {
		v, err := s.instances[op].BuildVerdict(0)
		require.NoError(t, err)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveVerdict(v))
		}
	}
	v2, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictNR, v2.Kind, "op2 has no V → NR verdict")
	for _, peer := range []OperatorID{1, 3, 4} {
		require.NoError(t, s.instances[peer].ObserveVerdict(v2))
	}

	// op2's verdict_pool[V0] from op1/3/4 = 3 = qV. op2 has no V_local → NR.
	choice, _, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice,
		"row 4: σ-eligibility met but op2 doesn't have V_local")
}

// Row 4 variant: σ-eligibility on V but host re-validates as NV at
// Phase-2b sign time → NR.
func TestConvergence_Row4_HostFlippedNV_NR(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()

	// Host re-evaluates at Phase-2b sign time and flips to not-valid
	// (e.g., further re-org).
	require.NoError(t, s.instances[2].ApplyHostValidity(0, Value("V0"), false))

	choice, _, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice,
		"host flipped to NV at Phase-2b → row 4 fires")
}

// Row 5: neither quorum reaches → NR.
func TestConvergence_Row5_NeitherQuorum_NR(t *testing.T) {
	s := newSim(t, 4)
	// h_V = 2: only op1 and op2 retain V0; op3/op4 don't.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)

	// op1/op2 broadcast σV, op3/op4 broadcast NR (but they only have
	// 0 V retained → NR by default).
	for _, op := range []OperatorID{1, 2} {
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
	// Don't deliver verdicts from op3/op4 — neither quorum can form.

	// op2's view: verdict_pool[V0] = {op1, op2 (self)} = 2 < qV = 3.
	// nr_pool = 0 < qEnc = 3. Row 5 → NR.
	choice, _, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice)
}

// Auth-only retention doesn't make the operator eligible for σ.
func TestConvergence_AuthOnlyRetention_NR(t *testing.T) {
	s := newSim(t, 4)
	// op2 sees V0 past T_accept_max → auth-only retention.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2}, observedAuthOnly)
	s.applyHostValidityAll(0, Value("V0"), true)

	// op2's own verdict will be NR (auth-only doesn't enable σV). Others
	// might σV on V0, pushing verdict_pool[V0] to ≥ qV. op2 would then
	// fall into row 4 (σ-eligibility but no V_local).
	choice, _, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice)
}

// Equivocators are excluded from convergence pools (treat-as-null).
func TestConvergence_EquivocatorExcluded(t *testing.T) {
	s := newSim(t, 4)
	// 4 operators total. op2 has V retained, host says valid → would σV.
	// Suppose op1 equivocates with two verdicts: σV(V0) and NR. From
	// op2's perspective, op1 is marked as equivocator → null contributor.
	s.deliverPhase1(0, Value("V0"), []OperatorID{2, 3}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2, 3}, 0, Value("V0"), true)

	// op3 and op2 broadcast σV.
	for _, op := range []OperatorID{2, 3} {
		v, err := s.instances[op].BuildVerdict(0)
		require.NoError(t, err)
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveVerdict(v))
		}
	}

	// op1 (byzantine) equivocates at op2's view.
	root := ValueRoot(Value("V0"))
	vA := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height, Layer: 0, Kind: VerdictSigmaV, ValueRoot: root}
	vB := &Verdict{ClusterID: s.cfg.ClusterID, OperatorID: 1, Height: s.cfg.Height, Layer: 0, Kind: VerdictNR}
	require.NoError(t, s.instances[2].ObserveVerdict(vA))
	require.NoError(t, s.instances[2].ObserveVerdict(vB))
	require.True(t, s.instances[2].IsEquivocator(0, 1))

	// op4 sends nothing — abstain.

	// op2's verdict_pool[V0]: own (op2) + op3 + (op1 EXCLUDED) = 2 < qV = 3.
	// nr_pool: (op1 EXCLUDED) = 0 < qEnc. Neither quorum → row 5 → NR.
	choice, _, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice,
		"equivocator's σV contribution excluded → quorum-short → NR")
}

// Self-observation regression: if a runner chooses to ObserveVerdict its
// own verdict (mirroring the spec's "first-observed" semantics uniformly
// across own and peer), the convergence pool must NOT count own's verdict
// twice. Same property as Phase 3's tryReconstructLayer self-skip.
func TestConvergence_SelfObservedVerdictNotDoubleCounted(t *testing.T) {
	s := newSim(t, 4)
	// h_V = 2 honest only: op2 is among the σ-eligible. Without the
	// self-skip, op2 self-observing its own σV verdict would inflate
	// verdict_pool[V0] from 2 → 3 and incorrectly trigger σ-eligibility.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)

	// op1 + op2 build verdicts; deliver op1's verdict to op2.
	v1, err := s.instances[1].BuildVerdict(0)
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObserveVerdict(v1))
	v2, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, v2.Kind)
	// op2 self-observes its own verdict.
	require.NoError(t, s.instances[2].ObserveVerdict(v2))

	// op2's verdict_pool[V0] = {op1, op2} = 2 (NOT 3). qV = 3 → row 5 → NR.
	choice, _, err := s.instances[2].ConvergenceDecision(0)
	require.NoError(t, err)
	require.Equal(t, CommitChoiceNR, choice,
		"self-observed own verdict must not be double-counted (2 < qV → row 5 NR)")
}

// Self-observation regression for the L_0 σ-eligibility channel: if a
// runner self-observes its own σV verdict at L_0, the channel must NOT
// fire on the inflated count.
func TestL0SigmaEligibility_SelfObservedVerdictNotDoubleCounted(t *testing.T) {
	s := newSim(t, 4)
	// h_V = 2: op1, op2 retain V0 + valid. Only op1 delivers to op2.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)

	v1, err := s.instances[1].BuildVerdict(0)
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObserveVerdict(v1))
	v2, err := s.instances[2].BuildVerdict(0)
	require.NoError(t, err)
	// op2 self-observes own σV.
	require.NoError(t, s.instances[2].ObserveVerdict(v2))

	// Without the self-skip, the L_0 σ-eligibility tally for V0 would be
	// {op1, own (via own), own (via peerVerdicts)} = 3 ≥ qV → channel
	// closes spuriously. With the skip, count is 2 < qV and the channel
	// stays open.
	select {
	case <-s.instances[2].L0SigmaEligibilityCh():
		t.Fatalf("L0SigmaEligibilityCh closed at h_V=2: own self-observed verdict was double-counted")
	default:
	}
}

// Self-observation regression for Rule 6b: own's verdict-vs-action
// mismatch (the Case B convergence-rule σ-eligibility-quorum-short flip
// — own σV verdict followed by NR action) is the expected honest path
// and must not record evidence against self even when the runner
// self-observes its own Onion2b.
func TestRule6b_SkippedForOwnOnion(t *testing.T) {
	s := newSim(t, 4)
	// h_V = 2 honest at L_0: op1, op2 retain V0; op3, op4 don't. op2 will
	// verdict σV(V0); cluster verdict_pool[V0] = 2 < qV → row 5 → op2
	// emits NR at Phase-2b. Own verdict (σV) and own action (NR) mismatch
	// — the honest Case B flip.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	// Deeper layers: ensure no σ eligibility there either.
	for k := 1; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], []OperatorID{2}, observedEarly)
		s.applyHostValidityFor([]OperatorID{2}, k, s.candidates[k], true)
	}

	// op1 verdicts σV(V0); op2 verdicts σV(V0). op3/op4 don't deliver
	// anything (silent).
	for _, op := range []OperatorID{1, 2} {
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
	// op2 self-observes its own verdict at L_0.
	v2, _ := s.instances[2].OwnVerdict(0)
	require.NoError(t, s.instances[2].ObserveVerdict(v2))

	// op2 builds its onion — row 5 → NR at L_0 (verdict-quorum short).
	o, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	require.Empty(t, o.Layers[0].Value, "L_0 should be NR (no σ entry)")
	// op2 self-observes its own onion.
	require.NoError(t, s.instances[2].ObserveOnion2b(o))

	// No Rule 6b evidence against op2 itself.
	for _, e := range s.instances[2].Evidence() {
		if e.Rule == EvidenceVerdictAction && e.OperatorID == 2 {
			t.Fatalf("Rule 6b fired against own self on Case B honest convergence flip: %+v", e)
		}
	}
}

// ---------- BuildOwnOnion2b — happy paths ----------

func TestBuildOwnOnion2b_HealthyClusterSigmaAtL0(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()

	o, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	require.Equal(t, OperatorID(2), o.OperatorID)
	require.Len(t, o.Layers, s.K)
	// Layer 0: σ on V (plaintext partial).
	require.True(t, bytes.Equal(o.Layers[0].Value, s.candidates[0]))
	require.NotEmpty(t, o.Layers[0].Ciphertext, "L_0 σ partial present")
	// Layers 1, 2, 3: σ too (chained-IBE-encrypted) since all-σ-eligible.
	for k := 1; k < s.K; k++ {
		require.True(t, bytes.Equal(o.Layers[k].Value, s.candidates[k]),
			"σ at L_%d", k)
		require.NotEmpty(t, o.Layers[k].Ciphertext)
	}
	// No NR partials (all layers are σ-side).
	require.Empty(t, o.NRPartials)
}

func TestBuildOwnOnion2b_Idempotent(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()

	o1, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	o2, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	require.Same(t, o1, o2, "second BuildOwnOnion2b returns cached")
}

// At L_{K-1} (deepest), NR produces no on-wire emission per spec
// §Convergence rule.
func TestBuildOwnOnion2b_DeepestNRNoEmission(t *testing.T) {
	s := newSim(t, 4)
	// No V retained at any layer → ConvergenceDecision returns NR for
	// every layer (row 5). NR partials produced at layers 0, 1, 2; no
	// partial at K-1 = 3.
	o, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	require.Len(t, o.NRPartials, s.K-1, "NR partials only for layers [0, K-1)")
	for _, p := range o.NRPartials {
		require.Less(t, p.Layer, s.K-1, "NR partial layer < K-1")
	}
	// All σ layers empty.
	for k, el := range o.Layers {
		require.Empty(t, el.Value, "L_%d σ entry empty (NR)", k)
		require.Empty(t, el.Ciphertext)
	}
}

// EKM σ-XOR-NR enforcement: if local state somehow already has NR
// locked, σ-emission at that layer must error.
func TestBuildOwnOnion2b_EKMRejectsSigmaAfterNRLock(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()

	// Force NR-lock at L_0 directly (simulating a bug where NR was
	// committed before σ was attempted).
	require.NoError(t, s.instances[2].transitionToNR(0))

	_, err := s.instances[2].BuildOwnOnion2b()
	require.ErrorIs(t, err, ErrNRLocked)
}

func TestOwnOnion2b_BeforeBuildReturnsFalse(t *testing.T) {
	s := newSim(t, 4)
	o, ok := s.instances[2].OwnOnion2b()
	require.False(t, ok)
	require.Nil(t, o)
}

// ---------- ObserveOnion2b — happy paths ----------

func TestObserveOnion2b_HealthyClusterAllSigma(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()
	onions := s.runPhase2bCanonical()
	require.Len(t, onions, s.n)

	// op1's view: σ entries from each op at each layer.
	for k := 0; k < s.K; k++ {
		peers := s.instances[1].peerOnions[k]
		require.Len(t, peers, s.n-1, "L_%d", k) // peers exclude self
	}
}

func TestObserveOnion2b_IdenticalRebroadcastDeduped(t *testing.T) {
	s := newSim(t, 4)
	for k := 0; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()

	o, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)
	require.NoError(t, s.instances[1].ObserveOnion2b(o))
	require.NoError(t, s.instances[1].ObserveOnion2b(o)) // duplicate
	require.NoError(t, s.instances[1].ObserveOnion2b(o)) // triplicate

	// Only one set of entries retained per layer.
	require.Len(t, s.instances[1].peerOnions[0][OperatorID(2)], 1)
	// No equivocation evidence.
	require.Empty(t, s.instances[1].Evidence())
}

// ---------- ObserveOnion2b — Rule 3 cross-onion equivocation ----------

func TestObserveOnion2b_Rule3TopLevelOnTwoDistinctOnions(t *testing.T) {
	s := newSim(t, 4)
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()

	// Build the canonical onion for op2.
	o1, err := s.instances[2].BuildOwnOnion2b()
	require.NoError(t, err)

	// Construct a structurally-distinct second onion from op2 (different
	// L_0 V). This wouldn't pass EKM in practice (single-σ-V) — we
	// fabricate the bytes directly to simulate a byzantine.
	o2 := deepCopyOnion2b(o1)
	o2.Layers[0].Value = Value("V0-different")

	// op1 observes both.
	require.NoError(t, s.instances[1].ObserveOnion2b(o1))
	require.NoError(t, s.instances[1].ObserveOnion2b(o2))

	// Top-level Rule 3 evidence at Layer=-1.
	ev := s.instances[1].Evidence()
	require.NotEmpty(t, ev)
	foundLayer := -2
	for _, e := range ev {
		if e.Rule == EvidenceCrossOnionEquivocation && e.Layer == -1 {
			foundLayer = -1
			require.NotNil(t, e.OnionEquivocation)
			require.NotNil(t, e.OnionEquivocation.OnionA)
			require.NotNil(t, e.OnionEquivocation.OnionB)
			break
		}
	}
	require.Equal(t, -1, foundLayer, "expected Rule 3 evidence at Layer=-1")
}

// ---------- ObserveOnion2b — Rule 1 cross-signing ----------

func TestObserveOnion2b_Rule1SigmaPlusNRSameLayer(t *testing.T) {
	s := newSim(t, 4)
	// Use L_1 (deeper layer) so Rule-5 cryptoFake doesn't drop the
	// σ entry before Rule-1 cross-check runs. At deeper layers σ
	// partials are IBE-encrypted (opaque to Rule 5).
	//
	// Byzantine op2 emits BOTH σ at L_1 (chained-IBE wrapped opaque
	// bytes) AND NR at L_1. EKM would block this locally; we construct
	// the onion directly.
	o := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 2,
		Height:     s.cfg.Height,
		Layers: []EncryptedLayer{
			{}, // L_0 empty
			{Value: Value("V1"), Ciphertext: []byte("op2-σ-ibe-wrapped")},
			{}, {}, // L_2, L_3 empty
		},
		NRPartials: []NRPartial{
			{Layer: 1, PartialSig: Signature("op2-nr-at-L1")},
		},
	}
	require.NoError(t, s.instances[1].ObserveOnion2b(o))

	ev := s.instances[1].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceCrossSigning && e.OperatorID == 2 && e.Layer == 1 {
			found = true
			require.NotNil(t, e.CrossSigning)
			require.True(t, bytes.Equal(e.CrossSigning.SigmaValue, Value("V1")))
			break
		}
	}
	require.True(t, found, "expected Rule 1 cross-signing evidence at op 2 / L_1")
}

// ---------- ObserveOnion2b — Rule 5 fake plaintext σ at L_0 ----------

func TestObserveOnion2b_Rule5CryptoFakeAtL0(t *testing.T) {
	s := newSim(t, 4)
	// Retain V0 at L_0 (so the stub signer's verification context exists).
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)

	// Byzantine op3 emits an L_0 σ with bogus partial bytes that don't
	// verify against op3's share for the claimed V.
	o := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 3,
		Height:     s.cfg.Height,
		Layers: []EncryptedLayer{
			{Value: Value("V0"), Ciphertext: []byte("garbage-partial-fails-verify")},
			{}, {}, {},
		},
	}
	require.NoError(t, s.instances[1].ObserveOnion2b(o))

	ev := s.instances[1].Evidence()
	foundRule5 := false
	for _, e := range ev {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 3 && e.Layer == 0 {
			foundRule5 = true
			require.NotNil(t, e.FakePlaintextSigma)
			require.NotEmpty(t, e.FakePlaintextSigma.RetainedValueHashes,
				"evidence carries the receiver's retained-V hashes")
			break
		}
	}
	require.True(t, foundRule5, "expected Rule 5 cryptoFake at op 3 / L_0")
}

// ---------- ObserveOnion2b — Rule 6b verdict-vs-action ----------

func TestObserveOnion2b_Rule6bSigmaVerdictNRAction(t *testing.T) {
	s := newSim(t, 4)
	// op1 broadcasts σV verdict at L_0 (op1 host says valid on V0).
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)

	v1, err := s.instances[1].BuildVerdict(0)
	require.NoError(t, err)
	require.Equal(t, VerdictSigmaV, v1.Kind)
	require.NoError(t, s.instances[2].ObserveVerdict(v1))

	// op1's actual Phase-2b action is NR at L_0 (contradicts the σV
	// verdict). Construct the onion manually.
	o := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{
			{Layer: 0, PartialSig: Signature("op1-nr-instead-of-σ")},
		},
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(o))

	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceVerdictAction && e.OperatorID == 1 && e.Layer == 0 {
			found = true
			require.NotNil(t, e.VerdictAction)
			require.Equal(t, VerdictSigmaV, e.VerdictAction.Verdict.Kind)
			require.NotEmpty(t, e.VerdictAction.NRPartial)
			break
		}
	}
	require.True(t, found, "expected Rule 6b σV-verdict + NR-action evidence")
}

func TestObserveOnion2b_Rule6bNRVerdictSigmaAction(t *testing.T) {
	s := newSim(t, 4)
	// op1 broadcasts NR verdict (no V retained at op1).
	v1 := &Verdict{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layer:      0,
		Kind:       VerdictNR,
	}
	require.NoError(t, s.instances[2].ObserveVerdict(v1))

	// op1's actual Phase-2b action is σ on V at L_0.
	o := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers: []EncryptedLayer{
			{Value: Value("V"), Ciphertext: []byte("σ-after-NR-verdict")},
			{}, {}, {},
		},
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(o))

	ev := s.instances[2].Evidence()
	found := false
	for _, e := range ev {
		if e.Rule == EvidenceVerdictAction && e.OperatorID == 1 && e.Layer == 0 {
			found = true
			require.Equal(t, VerdictNR, e.VerdictAction.Verdict.Kind)
			require.NotEmpty(t, e.VerdictAction.SigmaValue)
			require.NotEmpty(t, e.VerdictAction.SigmaPartial)
			break
		}
	}
	require.True(t, found, "expected Rule 6b NR-verdict + σ-action evidence")
}

func TestObserveOnion2b_Rule6bNotFiredOnHonestRevisionCase(t *testing.T) {
	s := newSim(t, 4)
	// Healthy: op1 verdicts σV, emits σ. No Rule 6b.
	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)
	s.applyHostValidityAll(0, Value("V0"), true)
	s.runPhase2aCanonical()
	s.runPhase2bCanonical()

	for _, e := range s.instances[2].Evidence() {
		require.NotEqual(t, EvidenceVerdictAction, e.Rule,
			"healthy σ-emission must not trigger Rule 6b")
	}
}

// Validation errors propagate from ObserveOnion2b.
func TestObserveOnion2b_RejectsNil(t *testing.T) {
	s := newSim(t, 4)
	require.Error(t, s.instances[2].ObserveOnion2b(nil))
}

func TestObserveOnion2b_RejectsWrongCluster(t *testing.T) {
	s := newSim(t, 4)
	o := &Onion2b{
		ClusterID: [32]byte{0xff},
		Height:    s.cfg.Height,
		Layers:    make([]EncryptedLayer, s.K),
	}
	o.OperatorID = 2
	require.ErrorContains(t, s.instances[1].ObserveOnion2b(o), "cluster id")
}

// ---------- chained IBE round-trip via stub ----------

func TestChainEncryptForLayer_L0IsPlaintext(t *testing.T) {
	s := newSim(t, 4)
	partial := []byte("σ_at_L0")
	ct, err := s.instances[2].chainEncryptForLayer(0, partial)
	require.NoError(t, err)
	require.True(t, bytes.Equal(ct, partial), "L_0 chain encryption is identity")
}

func TestChainEncryptForLayer_DeeperLayerProducesCiphertext(t *testing.T) {
	s := newSim(t, 4)
	partial := []byte("σ_at_L_K-1")
	// Encrypt at deepest layer (K-1 = 3) — 3 IBE wraps.
	ct, err := s.instances[2].chainEncryptForLayer(3, partial)
	require.NoError(t, err)
	require.False(t, bytes.Equal(ct, partial), "deeper-layer entry is encrypted")
	// chainDecryptForLayer round-trip uses real aggregated NR-partial
	// signatures as decryption keys; that flow is exercised in Phase H's
	// reconstruction tests where NR partials are produced by the stub
	// signer.
}

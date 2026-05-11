package twoab

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// Phase-I evidence-observer wiring: confirm every rule's detection path
// fires the EvidenceObserver exactly once per (Rule, OperatorID, Layer)
// tuple, matching the dedup contract documented on
// instance.recordEvidence and on the EvidenceObserver type.
//
// Rules 2 and 6a have their primary observer test in phase1_test.go and
// phase2a_test.go respectively (they share file with the rule's
// detection tests for narrative). This file consolidates observer
// coverage for the other five rules (1, 3, 4, 5, 6b) plus a multi-rule
// sanity scenario that confirms cross-rule dedup keys don't collide.

// ---------- Rule 1 (cross-signing) ----------

// Byzantine op emits σ + NR at the same layer within one Onion2b. The
// detection path runs twice (σ-side branch + NR-side branch in
// ObserveOnion2b); observer must fire exactly once.
func TestEvidenceObserver_Rule1FiresOnce(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	s.withObserver(2, func(e Evidence) { fired = append(fired, e) })

	// L_1 retention so Rule 5 cryptoFake doesn't drop the σ entry at L_0.
	s.deliverPhase1(1, s.candidates[1], s.allOperators(), observedEarly)

	// Byzantine op3 emits σ on V_1 at layer 1 AND NR at layer 1.
	o := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 3,
		Height:     s.cfg.Height,
		Layers: []EncryptedLayer{
			{}, // L_0 — empty
			{Value: s.candidates[1], Ciphertext: Signature{3}}, // L_1 σ on V_1
			{}, {}, // L_2, L_3
		},
		NRPartials: []NRPartial{
			{Layer: 1, PartialSig: Signature("op3-nr-at-L1")},
		},
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(o))

	require.Len(t, fired, 1, "Rule 1 observer fires once per (op, layer)")
	require.Equal(t, EvidenceCrossSigning, fired[0].Rule)
	require.Equal(t, OperatorID(3), fired[0].OperatorID)
	require.Equal(t, 1, fired[0].Layer)
}

// ---------- Rule 3 (cross-onion equivocation, top-level Layer=-1) ----------

// Byzantine op emits two structurally-distinct Onion2b messages. Top-
// level Rule 3 fires at Layer=-1 once; subsequent identical re-broadcasts
// are silent dedup; a THIRD distinct onion still must not re-fire the
// observer (peerFirstOnion2b is cleared after the first second-distinct).
func TestEvidenceObserver_Rule3TopLevelFiresOnce(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	s.withObserver(2, func(e Evidence) { fired = append(fired, e) })

	mk := func(payload byte) *Onion2b {
		return &Onion2b{
			ClusterID:  s.cfg.ClusterID,
			OperatorID: 3,
			Height:     s.cfg.Height,
			Layers:     make([]EncryptedLayer, s.K),
			NRPartials: []NRPartial{
				{Layer: 0, PartialSig: Signature{payload}},
			},
		}
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(mk(0xa)))
	require.NoError(t, s.instances[2].ObserveOnion2b(mk(0xb))) // distinct → fires
	require.NoError(t, s.instances[2].ObserveOnion2b(mk(0xc))) // distinct again → must NOT re-fire

	gotRule3Top := 0
	for _, e := range fired {
		if e.Rule == EvidenceCrossOnionEquivocation && e.Layer == -1 {
			gotRule3Top++
		}
	}
	require.Equal(t, 1, gotRule3Top, "Rule 3 top-level fires once per op even with 3+ distinct onions")
}

// ---------- Rule 4 (fake encrypted-presence at k > 0) ----------

// Byzantine op's chained-encrypted L_1 entry decrypts to garbage. Detected
// during Phase-3 walk after NR-quorum at L_0 unlocks decryption.
// Observer fires once for (Rule 4, byzantine, layer 1); a second Resolve
// must not re-fire.
func TestEvidenceObserver_Rule4FiresOnce(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	s.withObserver(2, func(e Evidence) { fired = append(fired, e) })

	// Mirror TestResolve_Rule4FakeEncryptedPresenceAtL1's setup.
	s.deliverPhase1(1, s.candidates[1], []OperatorID{2, 3, 4}, observedEarly)
	s.applyHostValidityFor([]OperatorID{2, 3, 4}, 1, s.candidates[1], true)
	for k := 2; k < s.K; k++ {
		s.deliverPhase1(k, s.candidates[k], s.allOperators(), observedEarly)
		s.applyHostValidityAll(k, s.candidates[k], true)
	}
	s.runPhase2aCanonical()

	o1Genuine, err := s.instances[1].BuildOwnOnion2b()
	require.NoError(t, err)
	tampered := deepCopyOnion2b(o1Genuine)
	tampered.Layers[1] = EncryptedLayer{
		Value:      s.candidates[1],
		Ciphertext: []byte("garbage-wont-decrypt-to-σ"),
	}
	// Drop op1's NR partial at L_1 so Rule 1 doesn't co-fire and confuse dedup.
	filtered := tampered.NRPartials[:0]
	for _, p := range tampered.NRPartials {
		if p.Layer != 1 {
			filtered = append(filtered, p)
		}
	}
	tampered.NRPartials = filtered

	for _, op := range []OperatorID{2, 3, 4} {
		_, err := s.instances[op].BuildOwnOnion2b()
		require.NoError(t, err)
	}
	for _, op := range []OperatorID{2, 3, 4} {
		o, _ := s.instances[op].OwnOnion2b()
		for _, peer := range s.allOperators() {
			if peer == op {
				continue
			}
			require.NoError(t, s.instances[peer].ObserveOnion2b(o))
		}
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(tampered))

	_, err = s.instances[2].Resolve()
	require.NoError(t, err)
	// Idempotent re-Resolve must NOT re-fire Rule 4 on the same peer/layer.
	_, err = s.instances[2].Resolve()
	require.NoError(t, err)

	gotRule4 := 0
	for _, e := range fired {
		if e.Rule == EvidenceFakeEncryptedPresence && e.OperatorID == 1 && e.Layer == 1 {
			gotRule4++
		}
	}
	require.Equal(t, 1, gotRule4, "Rule 4 observer fires once even on repeat Resolve")
}

// ---------- Rule 5 (fake plaintext σ at L_0) ----------

// Byzantine op emits a cryptoFake σ at L_0 in two distinct Onion2b
// messages (top-level Rule 3 also fires). The Phase-I dedup fix ensures
// Rule 5 fires only once even when re-evaluated on the second distinct
// onion's per-layer pass.
func TestEvidenceObserver_Rule5CryptoFakeFiresOnce(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	s.withObserver(2, func(e Evidence) { fired = append(fired, e) })

	s.deliverPhase1(0, Value("V0"), s.allOperators(), observedEarly)

	// Byzantine op3 emits σ at L_0 with bogus partial; then a distinct
	// second onion (different NR payload) so top-level Rule 3 also fires.
	mkOnion := func(nrPayload byte) *Onion2b {
		return &Onion2b{
			ClusterID:  s.cfg.ClusterID,
			OperatorID: 3,
			Height:     s.cfg.Height,
			Layers: []EncryptedLayer{
				{Value: Value("V0"), Ciphertext: []byte("garbage-fails-verify")},
				{}, {}, {},
			},
			NRPartials: []NRPartial{
				{Layer: 0, PartialSig: Signature{nrPayload}},
			},
		}
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(mkOnion(0xa)))
	require.NoError(t, s.instances[2].ObserveOnion2b(mkOnion(0xb)))

	gotRule5 := 0
	for _, e := range fired {
		if e.Rule == EvidenceFakePlaintextSigma && e.OperatorID == 3 && e.Layer == 0 {
			gotRule5++
		}
	}
	require.Equal(t, 1, gotRule5, "Rule 5 cryptoFake observer fires once even under top-level equivocation")
}

// ---------- Rule 6b (verdict-vs-action) ----------

// Byzantine op broadcasts σV verdict at L_0 but emits NR action at L_0.
// Observer fires once for (Rule 6b, op, L_0).
func TestEvidenceObserver_Rule6bFiresOnce(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	s.withObserver(2, func(e Evidence) { fired = append(fired, e) })

	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	v1, err := s.instances[1].BuildVerdict(0)
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObserveVerdict(v1))

	o := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{
			{Layer: 0, PartialSig: Signature("op1-nr-action")},
		},
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(o))
	// Same onion re-observed → top-level dedup; no re-fire.
	require.NoError(t, s.instances[2].ObserveOnion2b(o))

	gotRule6b := 0
	for _, e := range fired {
		if e.Rule == EvidenceVerdictAction && e.OperatorID == 1 && e.Layer == 0 {
			gotRule6b++
		}
	}
	require.Equal(t, 1, gotRule6b)
}

// ---------- Cross-rule sanity: distinct rules don't collide on dedup ----------

// Single receiver observes two byzantine ops, one for Rule 5 (cryptoFake
// at L_0) and one for Rule 6b (verdict-vs-action at L_0). Observer must
// fire for each independent (Rule, op, layer) tuple — the dedup key
// includes Rule, so different rules don't suppress each other.
func TestEvidenceObserver_DistinctRulesFireIndependently(t *testing.T) {
	s := newSim(t, 4)
	var fired []Evidence
	s.withObserver(2, func(e Evidence) { fired = append(fired, e) })

	// Setup: V0 retained at L_0 by op2 (the observer). Verdicts from op1
	// + op3 already in place.
	s.deliverPhase1(0, Value("V0"), []OperatorID{1, 2}, observedEarly)
	s.applyHostValidityFor([]OperatorID{1, 2}, 0, Value("V0"), true)
	v1, err := s.instances[1].BuildVerdict(0)
	require.NoError(t, err)
	require.NoError(t, s.instances[2].ObserveVerdict(v1))

	// op1 commits Rule 6b (σV verdict + NR action at L_0).
	o1 := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 1,
		Height:     s.cfg.Height,
		Layers:     make([]EncryptedLayer, s.K),
		NRPartials: []NRPartial{{Layer: 0, PartialSig: Signature("op1-nr-action")}},
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(o1))

	// op3 commits Rule 5 (cryptoFake σ at L_0).
	o3 := &Onion2b{
		ClusterID:  s.cfg.ClusterID,
		OperatorID: 3,
		Height:     s.cfg.Height,
		Layers: []EncryptedLayer{
			{Value: Value("V0"), Ciphertext: []byte("garbage")},
			{}, {}, {},
		},
	}
	require.NoError(t, s.instances[2].ObserveOnion2b(o3))

	// Observer fired twice — once per distinct rule — and the rule codes
	// are independent.
	rules := map[EvidenceRule]int{}
	for _, e := range fired {
		rules[e.Rule]++
	}
	require.Equal(t, 1, rules[EvidenceVerdictAction], "Rule 6b fires once for op1/L_0")
	require.Equal(t, 1, rules[EvidenceFakePlaintextSigma], "Rule 5 fires once for op3/L_0")
}

package consensustest

// ---- Fake encrypted presence (OBFT-specific Rule 4) -------------------

var scenarioFakeEncryptedPresence = Scenario{
	Name:  "FakeEncryptedPresence",
	Title: "Forged encrypted-presence at L_1 (Rule 4)",
	Group: "OBFT-specific attacks",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:         ByzFakeEncryptedPresence,
			ByzOperators: []OperatorID{1},
			Layer:        1,
		}
	},
	Expect: map[string]ExpectClass{
		// OBFT: byz silent at L_0 → NR-quorum unlocks L_1 decryption →
		// honest L_1 leader's bundle reconstructs at L_1; byz's L_1
		// garbage triggers Rule 4 evidence (recorded but not asserted here
		// — adapter tracks via PerOp.EvidenceByRule).
		"OBFT": ExpectSuccessFallThrough,
		// 2abOBFT: same mechanism — byz silent at L_0 + injected L_1
		// garbage. NR-quorum at L_0 unlocks L_1 decryption; honest L_1
		// leader reconstructs σ; byz's garbage entry fires Rule 4.
		"2abOBFT": ExpectSuccessFallThrough,
		"QBFT":    ExpectNotApplicable,
		"PSigs":   ExpectNotApplicable, // OBFT-specific (no chained encryption in PSigs)
	},
	Note: "OBFT-specific (no chained encryption in QBFT). Verifies Rule 4 detection path under real-BLS or stub-IBE.",
}

// ---- Rule 1 (cross-signing) evidence (Phase 3) ------------------------

var scenarioCrossSigningRule1 = Scenario{
	Name:  "CrossSigning_Rule1",
	Title: "Cross-signing evidence (Rule 1: σ + NR exclusivity)",
	Group: "OBFT-specific attacks",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		// byz=op2 (L_1 leader by default rotation): silent at L_1 → real NR at L_1;
		// adapter forges σ at L_1 in commit → Rule 1 fires at honest receivers.
		cfg.Byz = ByzPattern{Kind: ByzCrossSigning, ByzOperators: []OperatorID{2}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // L_0 leader op1 still broadcasts; healthy
		"2abOBFT": ExpectSuccessFastest, // same — L_0 still healthy
		"QBFT":    ExpectNotApplicable,  // OBFT-specific Rule 1 (σ + NR exclusivity)
		"PSigs":   ExpectNotApplicable,  // OBFT-specific Rule 1
	},
	Note: "Rule 1 evidence: byz emits σ + NR at same layer. L_0 leader unaffected → healthy decides.",
}

// ---- Rule 5 (fake plaintext σ at L_0) evidence (Phase 3) --------------

var scenarioFakePlaintextSigmaRule5 = Scenario{
	Name:  "FakePlaintextSigma_Rule5",
	Title: "Forged plaintext σ at L_0 (Rule 5)",
	Group: "OBFT-specific attacks",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzFakePlaintextSigma, ByzOperators: []OperatorID{2}}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // L_0 still healthy via the 3 honest σ partials
		"2abOBFT": ExpectSuccessFastest, // same — 3 honest σ partials at L_0 reach qV
		"QBFT":    ExpectNotApplicable,  // OBFT-specific Rule 5 (cryptoFake at L_0)
		"PSigs":   ExpectNotApplicable,  // OBFT-specific Rule 5
	},
	Note: "Rule 5 evidence: byz emits forged plaintext σ at L_0; honest receivers detect cryptoFake immediately. Cluster decides via 3 honest σ partials at L_0.",
}

// ---- Rule 3 (cross-onion equivocation) evidence (Phase 3) -------------

var scenarioCrossOnionEquivocationRule3 = Scenario{
	Name:  "CrossOnionEquivocation_Rule3",
	Title: "Cross-onion equivocation (Rule 3)",
	Group: "OBFT-specific attacks",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{
			Kind:         ByzCrossOnionEquivocation,
			ByzOperators: []OperatorID{2},
			Layer:        0,
		}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest, // 3 honest σ partials at L_0 still reach quorum
		"2abOBFT": ExpectSuccessFastest, // same — 3 honest σ partials reach qV at L_0
		"QBFT":    ExpectNotApplicable,  // OBFT-specific Rule 3 (cross-onion σ)
		"PSigs":   ExpectNotApplicable,  // OBFT-specific Rule 3
	},
	Note: "Rule 3 evidence: byz emits two structurally-distinct Commits with different σ at L_0. Honest receivers fire top-level + per-layer Rule 3.",
}

// ---- Rule 6a (Phase-2 equivocation, 2abOBFT-specific) -----------------

// scenarioPhase2EquivocateCrossV_Rule6a exercises the Phase-2 equivocation
// detection path at the catalog level: byz=op2 emits its natural KindValue
// on V (the leader's honest value) AND injects an extra KindValue on a
// distinct V_b. Each honest receiver fires Rule 6a (Phase2Equivocation)
// when the cross-V second ValueMsg arrives. The cluster's honest majority
// (op1 leader + op3 + op4 = 3 ops at n=4 f=1) still contributes to
// value_pool[V] reaching qV=3, so σ-eligibility fires and the slot
// succeeds at L_0.
//
// This is the catalog complement to the unit-level Rule-6a tests in
// protocol/v2/obft/twoab/phase2b_test.go (the multi-message cross-V cases)
// and scenarios_test.go (the mismatched-L_k-entries and post-NRDirect
// cases). At catalog level we verify the end-to-end cluster behavior:
// cluster decides cleanly + receiver-side evidence collection records
// the Rule 6a violation.
//
// Byz count is fixed at 1 regardless of cluster size (ByzOperators :=
// {op2}); at larger n the single-byz pattern still produces the same
// SuccessFastest outcome. The scenario stays valid across the
// FullCatalog_LargerN sweep without n-aware Apply scaling.
var scenarioPhase2EquivocateCrossV_Rule6a = Scenario{
	Name:  "Phase2EquivocateCrossV_Rule6a",
	Title: "Phase-2 cross-V equivocation (Rule 6a, 2abOBFT-specific)",
	Group: "OBFT-specific attacks",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzPhase2EquivocateCrossV, ByzOperators: []OperatorID{2}}
	},
	Expect: map[string]ExpectClass{
		// 2abOBFT: byz=op2 emits KindValue(V) + extra KindValue(V_b).
		// Honest receivers fire Rule 6a on cross-V; cluster value_pool[V]
		// reaches qV via 3 honest contributions → L_0 σ-quorum → fastest.
		"2abOBFT": ExpectSuccessFastest,
		// Bare OBFT: no Phase-2a coordination layer; no Rule-6a-equivalent
		// detection path exists in the bare OBFT instance.
		"OBFT":  ExpectNotApplicable,
		"QBFT":  ExpectNotApplicable, // QBFT's PREPARE pool uses different equivocation semantics
		"PSigs": ExpectNotApplicable, // PSigs has no leader/coordination phase
	},
	Note: "Rule 6a (Phase-2 equivocation) evidence: byz emits two distinct ValueMsgs on different V's. 2abOBFT receivers detect cross-V; cluster still decides at L_0 via honest majority. 2abOBFT-specific — no analog in bare OBFT / QBFT / PSigs.",
}

// scenarioPhase2DowngradeValueNoValue_Rule6a exercises the cross-KIND
// Phase-2 equivocation path: byz=op2 emits its natural KindValue on V
// (the leader's honest value) AND an extra KindNoValue from the same op.
// The sequence "KindValue → KindNoValue" is unauthorized per spec (only
// the reverse, A1 upgrade, is allowed). Receivers fire Rule 6a when they
// observe both kinds from the same op. Cluster decides at L_0 via the
// honest majority on value_pool[V] ≥ qV=3.
//
// Companion to Phase2EquivocateCrossV_Rule6a: that scenario covers
// same-kind cross-V double emission; this one covers cross-kind
// downgrade. Together they exercise both major Rule-6a violation
// categories at the catalog level (the unit-level tests in
// scenarios_test.go cover several more specific sequences: post-NRDirect
// reorderings, mismatched L_k upgrade entries, three-message in-order
// sequences).
//
// Byz count is fixed at 1 regardless of cluster size (ByzOperators :=
// {op2}); at larger n the single-byz pattern still produces the same
// SuccessFastest outcome (Rule 6a fires at receivers, cluster decides
// via the larger honest majority). The scenario stays valid across the
// FullCatalog_LargerN sweep without n-aware Apply scaling.
var scenarioPhase2DowngradeValueNoValue_Rule6a = Scenario{
	Name:  "Phase2DowngradeValueNoValue_Rule6a",
	Title: "Phase-2 Value→NoValue downgrade (Rule 6a, 2abOBFT-specific)",
	Group: "OBFT-specific attacks",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzPhase2DowngradeValueNoValue, ByzOperators: []OperatorID{2}}
	},
	Expect: map[string]ExpectClass{
		// 2abOBFT: byz=op2 emits natural KindValue(V) + extra KindNoValue.
		// Honest receivers fire Rule 6a (cross-kind unauthorized sequence
		// per spec §Authorized Phase-2 emission pairs). 3 honest contribute
		// to value_pool[V] → qV=3 → L_0 σ-quorum → fastest.
		"2abOBFT": ExpectSuccessFastest,
		"OBFT":    ExpectNotApplicable, // No Phase-2a coordination layer
		"QBFT":    ExpectNotApplicable, // No equivalent equivocation-detection path
		"PSigs":   ExpectNotApplicable, // No leader/coordination phase
	},
	Note: "Rule 6a (Phase-2 equivocation) evidence: byz emits KindValue(V) + KindNoValue downgrade from same op. 2abOBFT receivers detect the unauthorized cross-kind sequence (only A1 reverse direction is allowed); cluster still decides at L_0 via honest majority. Companion to Phase2EquivocateCrossV_Rule6a — together they cover both major Rule-6a categories (same-kind cross-V and cross-kind downgrade).",
}

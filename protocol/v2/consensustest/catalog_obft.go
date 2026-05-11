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
		"QBFT": ExpectNotApplicable,
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
		"OBFT": ExpectSuccessFastest, // L_0 leader op1 still broadcasts; healthy
		"QBFT": ExpectNotApplicable,  // OBFT-specific Rule 1 (σ + NR exclusivity)
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
		"OBFT": ExpectSuccessFastest, // L_0 still healthy via the 3 honest σ partials
		"QBFT": ExpectNotApplicable,  // OBFT-specific Rule 5 (cryptoFake at L_0)
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
		"OBFT": ExpectSuccessFastest, // 3 honest σ partials at L_0 still reach quorum
		"QBFT": ExpectNotApplicable,  // OBFT-specific Rule 3 (cross-onion σ)
	},
	Note: "Rule 3 evidence: byz emits two structurally-distinct Commits with different σ at L_0. Honest receivers fire top-level + per-layer Rule 3.",
}

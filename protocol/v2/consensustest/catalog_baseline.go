package consensustest

// ---- Healthy ------------------------------------------------------------

var scenarioHealthy = Scenario{
	Name:  "Healthy",
	Title: "Normal operations (all-honest healthy path)",
	Group: "Baseline",
	Modes: []Mode{ModeCorrectness, ModeStress},
	Apply: func(cfg *SimConfig) {
		cfg.Byz = ByzPattern{Kind: ByzNone}
		cfg.Host = HostAllValid{}
	},
	Expect: map[string]ExpectClass{
		"OBFT":    ExpectSuccessFastest,
		"2abOBFT": ExpectSuccessFastest,
		"QBFT":    ExpectSuccessFastest,
	},
	Note: "Protocols complete at fastest path under all-honest, in-budget propagation. BFT-comparison.md Table 1.",
}

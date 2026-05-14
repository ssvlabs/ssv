package consensustest

// ---- Healthy ------------------------------------------------------------

var scenarioHealthy = Scenario{
	Name:  "Healthy",
	Title: "Normal operations (all-honest healthy path)",
	Group: "Baseline",
	Modes: []Mode{ModeCorrectness, ModeStress},
	// Healthy is the only catalog scenario that opts into the mesh
	// transport: the all-honest baseline is where libp2p-shaped
	// re-flood + dedup adds modelling value. Adversarial scenarios
	// (selective broadcast, asymmetric byz patterns, host-flip cases)
	// stay on DeliveryDirect because their per-(from, to) primitives
	// lose meaning under reflood. WrapBaselineForInstability inherits
	// this flag so the instability-wrapped Healthy points also run
	// through mesh.
	Delivery: DeliveryMesh,
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

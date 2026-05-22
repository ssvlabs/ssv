package consensustest

// Faulty-nodes axis — the Healthy "faulty_nodes" knob. Mirrors the
// instability axis (instability.go): a Baseline-group-only wrap that the
// p2p_baseline sweep crosses with every other axis, surfaced in the UI as
// a picker over 0..f crashed operators. There is no dedicated faulty_nodes
// chart (per design) — the picker drives the main heatmap's Healthy cells.

// FaultyNodesRange returns the faulty_nodes axis values for cluster size n:
// 0..f where f = (n-1)/3. n=4 → {0,1}; n=7 → {0,1,2}; n=10 → {0,1,2,3}.
// 0 reproduces the pre-crash behavior (the wrap is a no-op); the upper
// bound is the BFT fault budget, at which the surviving 2f+1 operators are
// exactly the σ-quorum.
func FaultyNodesRange(n int) []int {
	f := (n - 1) / 3
	out := make([]int, 0, f+1)
	for c := 0; c <= f; c++ {
		out = append(out, c)
	}
	return out
}

// WrapBaselineForFaultyNodes returns a Scenario whose Apply additionally
// crashes `count` operators, when the scenario is in the Baseline group AND
// count > 0. Non-Baseline scenarios and count == 0 return the scenario
// unmodified — the knob is opt-in via Group + count, exactly like
// WrapBaselineForInstability.
//
// The victim set is left as a COUNT (Byz.CrashedCount); SimConfig.Validate
// resolves it to concrete operator IDs per-seed, so a deep-sampled Baseline
// cell averages over crash positions (leader and non-leader alike) rather
// than pinning one slice. Crashed operators are fully offline — no emission,
// no delivery, absent from the mesh topology — and reported Decided=false /
// Err="offline". Composes with the instability wrap (that mutates the
// network model; this sets a Byz field), so a cell can carry both.
func WrapBaselineForFaultyNodes(s Scenario, count int) Scenario {
	if !IsBaselineGroup(s) || count <= 0 {
		return s
	}
	return CloneScenarioWith(s, func(cfg *SimConfig) {
		cfg.Byz.CrashedCount = count
	})
}

// wrapAllForFaultyNodes applies WrapBaselineForFaultyNodes to every scenario
// in the slice; non-Baseline scenarios pass through unchanged.
func wrapAllForFaultyNodes(scenarios []Scenario, count int) []Scenario {
	out := make([]Scenario, len(scenarios))
	for i, s := range scenarios {
		out[i] = WrapBaselineForFaultyNodes(s, count)
	}
	return out
}

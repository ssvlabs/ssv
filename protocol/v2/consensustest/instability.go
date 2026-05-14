package consensustest

import (
	"math"
	"time"
)

// InstabilityLevel encodes a "p2p_instability" picker value. The
// Baseline-group scenarios (currently just Healthy) wrap their Network
// with MarkovianSlowness + LossyNetwork at the chosen level, modeling
// the production reality that real meshes aren't perfectly jittery:
// some operators are intermittently slow, and a few percent of
// packets drop in bursts (mesh-churn windows / peer-score events).
// Non-Baseline scenarios are intentionally unaffected — they already
// model specific named failure modes whose semantics would be muddied
// by extra randomness.
//
// Levels span "barely visible" (low) to "near-breaking-point"
// (extreme); see InstabilityLevels for the calibrated parameter
// values. Level=0 ("none") is the no-wrap pass-through that
// reproduces the pre-instability behavior.
//
// Composition order, inside Apply, is:
//
//	inner Network  →  MarkovianSlowness  →  LossyNetwork
//
// matching the real-world causal chain ("packet got hung up by a slow
// op, then was lossily dropped on the wire"). Both wrappers are
// stateful and constructed fresh per Scenario.Apply call so per-sim
// determinism is preserved (each iter gets its own Markov chain).
type InstabilityLevel struct {
	Name string
	// Level is what flows through SweepPoint.Fields["Instability"] —
	// numeric so it fits map[string]float64; the UI maps it back to
	// Name for picker labels.
	Level int

	LossRate    float64 // network-wide bursty stochastic loss
	BurstFactor int     // mean dwell time in the lossy state, in messages
	// SlowOpsFraction is the share of the cluster flagged as
	// markov-slow. Effective count is ceil(SlowOpsFraction × N),
	// capped at N−1 so the leader (op1) stays fast. Using a fraction
	// rather than a raw count keeps "extreme" qualitatively-similar
	// across cluster sizes: SlowOps=3 at n=4 means 3-of-4 slow (only
	// leader fast), but at n=13 it means just 3-of-13 — well within
	// the byzantine bound, qualitatively a much milder configuration.
	// Cross-cluster comparison via the n-picker is one of the matrix
	// mode's selling points, so scaling preserves that comparison.
	SlowOpsFraction float64
	SlowMul         float64 // slow op's ExtraDelay = SlowMul × cfg.BTT
	PersistP        float64 // symmetric chain P(stay) in each state
}

// slowCountForN returns the actual number of markov-slow operators
// for this level at cluster size n. Always leaves at least the leader
// (op1) fast (cap at n−1), and yields zero for level=none (fraction=0).
func (l InstabilityLevel) slowCountForN(n int) int {
	count := int(math.Ceil(l.SlowOpsFraction * float64(n)))
	if count > n-1 {
		count = n - 1
	}
	if count < 0 {
		count = 0
	}
	return count
}

// InstabilityLevels are the 5 calibrated picker values. SlowOpsFraction
// was originally anchored to the n=4 raw-count calibration
// (low=1, moderate=2, high=2, extreme=3); high and extreme were
// subsequently dialled back to leave a clearer gap between adjacent
// levels at the larger cluster sizes (and below). Counts at each n
// come from ceil(SlowOpsFraction × n), capped at n−1:
//
//	level     fraction  n=4  n=7  n=10  n=13
//	low       0.25       1    2    3     4
//	moderate  0.50       2    4    5     7
//	high      0.35       2    3    4     5
//	extreme   0.55       3    4    6     8
//
// (moderate and high collide at n=4 — by design, anchored to the
// original n=4 calibration — but diverge for larger n. LossRate,
// BurstFactor, SlowMul, and PersistP further separate adjacent
// levels.)
//
// Tuning notes (expect a round of empirical adjustment per cluster
// size if you change ranges):
//   - none      pass-through; reproduces pre-instability stats.
//   - low       very rare instabilities; Healthy success rate should
//               be ≥ 99% — basically indistinguishable from "none"
//               unless the user looks carefully.
//   - moderate  occasional disruption; success rate drops a few %.
//   - high      sustained-but-recoverable instability; success rate
//               drops 10-30%.
//   - extreme   clearly worse than high but still informative —
//               success rate should land in roughly the 0-30% range
//               so per-protocol differences stay visible (going all
//               the way to 0% just collapses every protocol to the
//               same flat line).
var InstabilityLevels = []InstabilityLevel{
	{Name: "none", Level: 0},
	{Name: "low", Level: 1, LossRate: 0.005, BurstFactor: 5, SlowOpsFraction: 0.25, SlowMul: 1.5, PersistP: 0.5},
	{Name: "moderate", Level: 2, LossRate: 0.02, BurstFactor: 5, SlowOpsFraction: 0.50, SlowMul: 2.0, PersistP: 0.7},
	{Name: "high", Level: 3, LossRate: 0.07, BurstFactor: 6, SlowOpsFraction: 0.35, SlowMul: 2.1, PersistP: 0.55},
	{Name: "extreme", Level: 4, LossRate: 0.10, BurstFactor: 6, SlowOpsFraction: 0.55, SlowMul: 2.8, PersistP: 0.60},
}

// IsBaselineGroup reports whether `s` is one of the Group=="Baseline"
// scenarios that the instability wrap applies to. Currently just
// Healthy; extracted as a predicate so adding new baseline scenarios
// later doesn't require touching the wrap logic.
func IsBaselineGroup(s Scenario) bool { return s.Group == "Baseline" }

// WrapBaselineForInstability returns a Scenario whose Apply composes
// the original Apply with the per-level MarkovianSlowness + LossyNetwork
// wrap when the scenario is in the Baseline group AND the level is
// non-zero. Non-Baseline scenarios and level=0 return the scenario
// unmodified — the wrap is opt-in via Group + Level.
//
// Slow ops are picked as op2..op{SlowOps+1} so the leader (op1) stays
// fast (matches the convention in p2pNodeSlownessSweep).
//
// CAVEAT: the pinned-low-index choice biases the slow set toward the
// shallow-layer leaders under SSV's op-id-ordered leader rotation
// (op_k typically leads L_{k-1}). That's the intended worst case for
// stress, but it means a single sim/seed exercises only one slice of
// "which ops are slow". Cross-sim variety comes from the LossyNetwork
// + MarkovianSlowness Markov chains rerolling per seed within a fixed
// op set, not from rerolling the op set itself. If a future sweep
// wants to surface "average-case slow-set" rather than "leader-biased
// worst case", randomize SlowOps selection per sim before changing
// this convention.
func WrapBaselineForInstability(s Scenario, level InstabilityLevel) Scenario {
	if !IsBaselineGroup(s) || level.Level == 0 {
		return s
	}
	inner := s
	return Scenario{
		Name:     s.Name,
		Title:    s.Title,
		Group:    s.Group,
		Modes:    s.Modes,
		Delivery: s.Delivery,
		Apply: func(cfg *SimConfig) {
			if inner.Apply != nil {
				inner.Apply(cfg)
			}
			slowCount := level.slowCountForN(cfg.N)
			slowOps := make([]OperatorID, 0, slowCount)
			for i := 0; i < slowCount; i++ {
				slowOps = append(slowOps, OperatorID(i+2))
			}
			base := cfg.Network
			if base == nil {
				base = ConstantDelay{D: cfg.BTT}
			}
			withSlow := NewMarkovianSlowness(base, slowOps,
				time.Duration(level.SlowMul*float64(cfg.BTT)), level.PersistP)
			cfg.Network = NewLossyNetwork(withSlow, level.LossRate, level.BurstFactor)
		},
		Expect: s.Expect,
		Note:   s.Note,
	}
}

// filterBaselineScenarios returns just the Baseline-group entries of
// `scenarios`. Used by sweep builders to keep instability variants
// from re-running every non-Baseline scenario at every level
// (non-Baseline scenarios are instability-invariant by construction,
// so their data is computed once at level=none and the UI reuses it
// regardless of picker position).
func filterBaselineScenarios(scenarios []Scenario) []Scenario {
	out := make([]Scenario, 0)
	for _, s := range scenarios {
		if IsBaselineGroup(s) {
			out = append(out, s)
		}
	}
	return out
}

// wrapAllForInstability applies WrapBaselineForInstability to every
// scenario in the slice. Non-Baseline scenarios pass through unchanged
// (the wrap is a no-op for them). Caller decides whether to pass in
// the full scenario list (level=0 case — non-Baseline scenarios run
// once here) or just the Baseline subset (level>0 case — only
// Baseline scenarios re-run with the wrap).
func wrapAllForInstability(scenarios []Scenario, level InstabilityLevel) []Scenario {
	out := make([]Scenario, len(scenarios))
	for i, s := range scenarios {
		out[i] = WrapBaselineForInstability(s, level)
	}
	return out
}

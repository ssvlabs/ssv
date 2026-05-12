package consensustest

import "time"

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
	SlowOps     int     // number of operators flagged as "markov-slow"
	SlowMul     float64 // slow op's ExtraDelay = SlowMul × cfg.BTT
	PersistP    float64 // symmetric chain P(stay) in each state
}

// InstabilityLevels are the 5 calibrated picker values. Calibration
// notes (likely to need 1-2 rounds of empirical tuning):
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
	{Name: "low", Level: 1, LossRate: 0.005, BurstFactor: 5, SlowOps: 1, SlowMul: 1.5, PersistP: 0.5},
	{Name: "moderate", Level: 2, LossRate: 0.02, BurstFactor: 5, SlowOps: 2, SlowMul: 2.0, PersistP: 0.7},
	{Name: "high", Level: 3, LossRate: 0.10, BurstFactor: 8, SlowOps: 2, SlowMul: 3.0, PersistP: 0.8},
	{Name: "extreme", Level: 4, LossRate: 0.15, BurstFactor: 8, SlowOps: 3, SlowMul: 4.0, PersistP: 0.85},
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
func WrapBaselineForInstability(s Scenario, level InstabilityLevel) Scenario {
	if !IsBaselineGroup(s) || level.Level == 0 {
		return s
	}
	inner := s
	return Scenario{
		Name:  s.Name,
		Title: s.Title,
		Group: s.Group,
		Modes: s.Modes,
		Apply: func(cfg *SimConfig) {
			if inner.Apply != nil {
				inner.Apply(cfg)
			}
			slowCount := level.SlowOps
			if slowCount > cfg.N-1 {
				// keep the leader (op1) out of the slow set
				slowCount = cfg.N - 1
			}
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

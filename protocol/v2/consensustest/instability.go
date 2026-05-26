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
// Levels span "barely visible" (low) to the harshest modeled mesh
// stress (extreme); see InstabilityLevels for the calibrated parameter
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
	// Level is what flows through SweepPoint.Fields[FieldInstability] —
	// numeric so it fits map[FieldKey]float64; the UI maps it back to
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
	// SlowMul scales the network's slow-op disruption reference:
	// ExtraDelay = SlowMul × Network.SlowOpAnchor(). Anchored to the
	// network model (not cfg.BTT) so empirical profiles get realistic
	// per-environment slow-op magnitudes (hundreds-of-ms CPU stalls)
	// regardless of the protocol-budget BTT axis. Synthetic models
	// (ConstantDelay, LogNormalDelay) still produce BTT-coupled
	// disruption — their SlowOpAnchor tracks the configured delay.
	SlowMul  float64
	PersistP float64 // symmetric chain P(stay) in each state
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

// InstabilityLevels are the 5 calibrated picker values. Every
// severity-relevant knob is monotonically non-decreasing none →
// extreme, so a higher level is at least as harsh on every axis.
// Slow-op counts come from ceil(SlowOpsFraction × n), capped at n−1 so
// the leader (op1) always stays fast:
//
//	level     fraction  n=4  n=7  n=10  n=13
//	low       0.25       1    2    3     4
//	moderate  0.40       2    3    4     6
//	high      0.50       2    4    5     7
//	extreme   0.60       3    5    6     8
//
// Counts are strictly ordered at the larger supported sizes (n = 7, 10,
// 13). At n = 4 the n−1 cap leaves only three distinct counts for four
// non-zero levels, so moderate and high tie there (2 of 4 slow) and
// separate via the intensity knobs (LossRate, BurstFactor, SlowMul,
// PersistP), which are themselves monotonic.
//
// Tuning notes. Every knob is monotonic none → extreme, so the Healthy
// success curve is monotonically non-increasing:
//   - none      pass-through; reproduces pre-instability stats.
//   - low       mild instability; the gentlest step above none.
//   - moderate  occasional disruption.
//   - high      sustained-but-recoverable instability.
//   - extreme   the harshest informative point — deliberately short of
//     a total wipeout so per-protocol differences stay visible.
//
// Absolute degradation is modest in Healthy's mesh config: it keeps its
// recovery features on (gossip backstop + SafetyBuffer=700ms), which
// absorb most instability-induced misses, so success stays high for most
// protocols and the levels separate them mainly at the harsh end
// (high/extreme, more so at larger n). Expect a round of empirical
// adjustment per cluster size if you change ranges; while the recovery
// features dominate, raising the knobs alone yields diminishing returns.
var InstabilityLevels = []InstabilityLevel{
	{Name: "none", Level: 0},
	{Name: "low", Level: 1, LossRate: 0.0125, BurstFactor: 5, SlowOpsFraction: 0.25, SlowMul: 1.75, PersistP: 0.55},
	{Name: "moderate", Level: 2, LossRate: 0.02, BurstFactor: 5, SlowOpsFraction: 0.40, SlowMul: 2.0, PersistP: 0.6},
	{Name: "high", Level: 3, LossRate: 0.08, BurstFactor: 6, SlowOpsFraction: 0.50, SlowMul: 2.4, PersistP: 0.65},
	{Name: "extreme", Level: 4, LossRate: 0.10, BurstFactor: 6, SlowOpsFraction: 0.60, SlowMul: 2.8, PersistP: 0.70},
}

// IsBaselineGroup reports whether `s` is one of the Group=="Baseline"
// scenarios that the instability wrap applies to. Currently just
// Healthy; extracted as a predicate so adding new baseline scenarios
// later doesn't require touching the wrap logic.
func IsBaselineGroup(s Scenario) bool { return s.Group == "Baseline" }

// slowOpExtraDelay computes the per-hop slow-op tax for `model` at
// multiplier `mul`: mul × model.SlowOpAnchor(), with the float ↔
// Duration cast centralized. Used by both the cluster-wide (direct)
// and mesh paths of WrapBaselineForInstability, and by the
// p2pNodeSlownessSweep slow-ops wrap. The two paths read the anchor
// from their respective underlying models (cfg.Network vs
// cfg.Mesh.HopDelay) so a mismatched direct/mesh setup (e.g. mesh
// uses BTT/3 LogNormal while direct uses a calibrated mixture) gets
// each anchor independently.
func slowOpExtraDelay(model NetworkModel, mul float64) time.Duration {
	return time.Duration(mul * float64(model.SlowOpAnchor()))
}

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
	return CloneScenarioWith(s, func(cfg *SimConfig) {
		slowCount := level.slowCountForN(cfg.N)
		slowOps := make([]OperatorID, 0, slowCount)
		for i := 0; i < slowCount; i++ {
			slowOps = append(slowOps, OperatorID(i+2))
		}
		// Cluster-wide (direct path): wrap cfg.Network with markov-slow
		// + bursty loss, both anchored to the current level's params.
		// ExtraDelay derives from the underlying NetworkModel's
		// SlowOpAnchor (per-impl: D for ConstantDelay, 2×Median for
		// LogNormalDelay, hand-tuned 250-450ms for the empirical
		// profiles) so empirical profiles see realistic slow-op taxes
		// independent of cfg.BTT, while synthetic models keep tracking
		// the BTT axis. See the NetworkModel interface comment for
		// per-impl details.
		base := cfg.Network
		if base == nil {
			base = ConstantDelay{D: cfg.BTT}
		}
		directExtra := slowOpExtraDelay(base, level.SlowMul)
		withSlow := NewMarkovianSlowness(base, slowOps, directExtra, level.PersistP)
		cfg.Network = NewLossyNetwork(withSlow, level.LossRate, level.BurstFactor)
		// Mesh hop (mesh path): wrap cfg.Mesh.HopDelay with FRESH
		// per-mesh-edge instances of the same markov-slow + lossy
		// wrappers. The mesh transport feeds them mesh-endpoint
		// OperatorIDs (cluster + relay synthetic IDs), so the chains
		// key per mesh edge — relay-to-relay edges don't share state
		// with op-to-relay edges, so Healthy in mesh-mode responds to
		// the instability axis.
		// Per-hop anchor read separately (mesh hop model may differ
		// from cfg.Network, e.g. mesh-mode uses BTT/3 LogNormal anchor
		// while direct may use a calibrated mixture).
		meshInner := cfg.Mesh.HopDelay
		if meshInner == nil {
			meshInner = LogNormalDelay{Median: cfg.BTT / 3, Sigma: 0.3}
		}
		meshExtra := slowOpExtraDelay(meshInner, level.SlowMul)
		meshWithSlow := NewMarkovianSlowness(meshInner, slowOps, meshExtra, level.PersistP)
		cfg.Mesh.HopDelay = NewLossyNetwork(meshWithSlow, level.LossRate, level.BurstFactor)
	})
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

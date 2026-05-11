package consensustest

import "time"

// Mode identifies which test tier a scenario participates in. Scenarios
// declare their supported modes via Scenario.Modes; test entry points
// (TestCorrectness, TestStress) filter the Catalog by mode.
//
// See docs/CONSENSUSTEST-SPLIT-PLAN.md.
type Mode string

const (
	// ModeCorrectness — deterministic params, fixed seed, ConstantDelay,
	// hard assertions per scenario via Scenario.Expect. Fast, runs in CI.
	// One operating point per scenario; no sweeps.
	ModeCorrectness Mode = "correctness"

	// ModeStress — varied seeds, jittered network, many iterations.
	// Soft assertions: only framework-level safety invariants. Emits the
	// consensustest-report.
	ModeStress Mode = "stress"
)

// AssertLevel governs how aggressively the runner reacts to outcome
// mismatches. Safety invariants always panic regardless of level.
type AssertLevel int

const (
	// AssertHard fails the test on any scenario-expectation mismatch
	// (used by the correctness profile).
	AssertHard AssertLevel = iota

	// AssertSafetyOnly skips per-scenario expectation matching; only
	// safety invariants fail the test (used by the stress profile).
	AssertSafetyOnly
)

// SeedStrategy describes how iteration seeds are derived from a batch's
// SeedStart. Both strategies are deterministic at the batch level: same
// SeedStart → same Seed sequence.
type SeedStrategy int

const (
	// SeedFixed reuses the batch's SeedStart for every iteration (used by
	// the correctness profile, which runs each scenario once). With
	// Iterations=1 this is identical to SeedDerived.
	SeedFixed SeedStrategy = iota

	// SeedDerived produces a distinct seed per iteration as
	// `SeedStart + iterIndex`. Iterations within the batch are uncorrelated
	// while remaining reproducible (used by the stress profile).
	SeedDerived
)

// Profile captures the runtime shape for a tier. The two presets
// (CorrectnessProfile, StressProfile) are what test entry points pass to
// the runner; ad-hoc tests can build a Profile inline.
//
// Profile is intentionally orthogonal to Scenario: a scenario describes
// WHAT goes wrong; a profile describes HOW we run it. The same catalog
// scenario can be executed under either profile (assuming it opts in via
// Scenario.Modes).
//
// Today the framework consumes BaseConfig directly. The other fields
// document the tier's intended runtime shape for callers / future
// runner-side wiring (e.g. a Profile-aware batch driver that honors
// Iterations / Seed / Assertions instead of carrying those knobs
// separately on BatchConfig).
type Profile struct {
	Name       string
	Mode       Mode
	Iterations int
	Seed       SeedStrategy
	Assertions AssertLevel
	BaseConfig SimConfig // template; the runner copies + applies scenario.Apply per sim
}

// CorrectnessProfile is the deterministic preset used by TestCorrectness.
// One iteration per scenario, ConstantDelay, hard assertions.
func CorrectnessProfile(btt time.Duration) Profile {
	base := DefaultProposerDutyConfig(btt)
	base.Network = ConstantDelay{D: btt}
	return Profile{
		Name:       "correctness",
		Mode:       ModeCorrectness,
		Iterations: 1,
		Seed:       SeedFixed,
		Assertions: AssertHard,
		BaseConfig: base,
	}
}

// StressProfile is the stochastic preset used by TestStress. Many
// iterations, varied seeds, safety-only assertions. Default network is
// JitteredDelay (±25% jitter around BTT) — modelling real-world
// propagation variance. Callers can override via `net` when running a
// sweep that has its own network model (e.g. LogNormal heavy-tail,
// LossyNetwork) — pass nil for the default jittered shape.
func StressProfile(btt time.Duration, iterations int, net NetworkModel) Profile {
	base := DefaultProposerDutyConfig(btt)
	if net != nil {
		base.Network = net
	} else {
		base.Network = JitteredDelay{D: btt, Jitter: btt / 4}
	}
	return Profile{
		Name:       "stress",
		Mode:       ModeStress,
		Iterations: iterations,
		Seed:       SeedDerived,
		Assertions: AssertSafetyOnly,
		BaseConfig: base,
	}
}

# Consensustest: correctness vs stress split

## Goal

Split the protocol tests into two complementary tiers that share one catalog of scenarios.

- **Correctness** — deterministic params, fixed seed, `ConstantDelay`, hard assertions per scenario. Fast. Never flaky. Runs in CI. No stats / no report.
- **Stress** — stochastic params, varied seeds, jittered network, many iterations. Soft assertions (framework-level safety only). Slow. Run on-demand / nightly. Emits `consensustest-report`.

Both tiers cover the same kinds of scenarios (normal + failure). The tier-level difference is *how* we run them, not *what* we run.

## Design

A scenario describes WHAT goes wrong; a profile describes HOW we run it.

### Single catalog

One `ct.Catalog` of scenarios — protocol-level fault descriptions, agnostic of network shape and iteration count. Each scenario carries a `Modes` opt-in (`Correctness`, `Stress`, or both). Most apply to both.

### Profile owns runtime shape

```go
type Profile struct {
    Name       string       // "correctness" | "stress"
    Mode       Mode         // ModeCorrectness | ModeStress
    Iterations int          // 1 vs 50–100+
    Seed       SeedStrategy // SeedFixed | SeedDerived (iter seed = batchSeed + iterIndex)
    Assertions AssertLevel  // AssertHard | AssertSafetyOnly
    BaseConfig SimConfig    // template — runner copies + applies scenario.Apply per sim
}
```

The presets `CorrectnessProfile(btt)` and `StressProfile(btt, iterations, net)` populate these fields. Today only `BaseConfig` is read by the framework (consumed by `TestCorrectness`); the other fields document the tier's intended runtime shape and are wired in as future Profile-aware runners need them. The stress sweeps consult `BaseConfig.Network` indirectly via the per-sweep templates set in `sweep.go` — Phase 5 made `canonical / cluster_scaling / btt_degradation` use `JitteredDelay`, mirroring `StressProfile`'s default.

### Per-scenario expectations

Each scenario declares its expected outcome per protocol using the existing `Expect map[string]ExpectClass` field — keys are protocol names (`"OBFT"`, `"QBFT"`); values are the canonical outcome buckets (`ExpectSuccessFastest`, `ExpectSuccessFallThrough`, `ExpectMiss`, `ExpectNotApplicable`, `ExpectSuccessOrMiss`). The correctness runner (`RunScenarioOnProtocol`) picks the entry matching the protocol under test; a missing entry fails the test with `scenario %q has no Expect entry for protocol %q`.

`ExpectClass` is a coarse classification — no per-scenario "expected miss reason" string. If a scenario's correctness story really hangs on the specific reason it should miss, the audit captures that in the scenario's `Note` comment rather than in the assertion path; that keeps the runner contract minimal and avoids brittle reason-string matching.

### Assertion layers

- **Safety invariants** — framework-level, always checked, both profiles. Implemented in `safety.go`'s `ComputeSafetyReport` and `SafetyReport.IsViolation()`. A violation panics regardless of profile via `SafetyPanic`. Enumerated below.
- **Correctness expectations** — per-scenario, correctness profile only. Examples: "scenario X decides at fastest path", "scenario Z is n/a for QBFT". Defined on the scenario via `Scenario.Expect map[string]ExpectClass`; invoked only by `RunScenarioOnProtocol` (which both `TestCorrectness` and the spec-conformance smoke tests already use).
- **Stress observations** — per-scenario, stress profile only. Emitted as distributions/stats for the report. No hard assertions. Specifically NOT asserted: success rates, percentile decision times, miss-reason ratios — those belong on the chart, not in the assertion path.

### Safety invariants

Checked on every iteration regardless of profile.

Universal (both OBFT and QBFT):
1. **Agreement** — no two honest validators decide different values for the same instance.
2. **Quorum-backed decision** — any decided state references ≥2f+1 valid commit signatures from distinct operators.
3. **No equivocation accepted** — for any (round, leader), if an honest validator receives two conflicting proposals signed by the same leader, it does not commit based on them.

OBFT-specific:
4. **σ-or-NR commit semantics** — every T_commit transition is preceded by either a proper σ-commit or NR-commit with quorum (no free decisions).
5. **Host-validity respect** — if honest validators reach a decision, the decided value satisfies the host-validity predicate each honest validator computed.

The invariant-checker today is a flat sequence of checks inside `ComputeSafetyReport` (not a registry of plug-in functions). Adding a sixth check is a new bool field on `SafetyReport`, a corresponding adapter-side `*Checked` field on `CommitAttestation` (if it needs adapter introspection), and a few lines in `ComputeSafetyReport` + `IsViolation` + `String`. Adapter migration is non-breaking via the established graceful-degradation pattern: uninstrumented adapters leave their `*Checked` field zero and the framework treats the invariant as no-violation-reportable.

### Test entry points

- `TestCorrectness` (in `correctness_test.go`) — uses `ScenariosWithMode(Catalog, ModeCorrectness)`, runs each with `CorrectnessProfile.BaseConfig` via `RunScenarioOnProtocol` (single operating point per scenario; no sweeps). Asserts `Scenario.Expect` matches the observed outcome class plus safety invariants. Hard fail on any mismatch.
- `TestStress` (in `stress_test.go`) — uses `ScenariosWithMode(Catalog, ModeStress)`, runs each scenario across the curated `DefaultSweeps` set. Safety invariants only (no per-scenario `Expect` enforcement). Always emits `data.js` when `REPORT_DIR` is set.

If a scenario needs behavioral assertions at multiple operating points (e.g., `n=4` AND `n=10`), it is expressed as separate catalog entries — never as a sweep within `TestCorrectness`.

## Phased plan

### Phase 1 — Foundations (additive, behavior unchanged) — DONE

1. Added `ct.Profile` type + `ct.CorrectnessProfile` / `ct.StressProfile` presets in `profile.go`.
2. Extended `SafetyReport` with the four new invariants (QuorumBackedDecision, NoEquivocationAccepted, OBFTCommitKindValid, OBFTHostValidityRespect) + `IsViolation()` helper. `ComputeSafetyReport` reads `Outcome.CommitAttestation` with graceful degradation (matches the existing `NoOfflineDoubleV` pattern). `runCell` and `RunScenarioOnProtocol` both gate on `safety.IsViolation()`.
3. Added `Modes []Mode` field + `HasMode(m)` helper on `Scenario`. Empty slice defaults to `{ModeStress}` for back-compat.
4. *Not done* — the existing `Scenario.Expect map[string]ExpectClass` field already served the per-protocol-expectation role; a richer `Expectation{Kind,Reason}` was not needed.

### Phase 1b — OBFT adapter equivocation instrumentation — DONE

OBFT adapter populates `CommitAttestation.EquivocationChecked` and counts `Rule2 / Rule3` evidence fires into `EquivocationsObserved`. Quorum / OBFTCommitKind / OBFTHostValidityRespect deferred — each needs deeper introspection than the current adapter boundary exposes (`obft.Instance` partial counts, NR-vs-σ commit-path tracking, per-op acceptance-layer plumbing).

### Phase 2 — Scenario refactor — DONE

5. Audited each scenario in `catalog.go`. Findings:
   - All 29 scenarios are pure config mutators (no randomness, no in-body assertions) — `*rand.Rand` threading was unnecessary in practice.
   - All declared `Expect` for both protocols (29 OBFT + 29 QBFT entries). No expansion to `Expectation{Kind,Reason}` was needed.
   - All 29 opted into both modes — annotated with `Modes: []Mode{ModeCorrectness, ModeStress}`.
6. Verified `make consensustest-report` regenerates `data.js` unchanged.

### Phase 3 — Correctness entry point — DONE

Added `TestCorrectness` in `correctness_test.go`. Filters Catalog via `ScenariosWithMode(Catalog, ModeCorrectness)`, runs each with `CorrectnessProfile.BaseConfig` through `RunScenarioOnProtocol`. 29 × 2 = 58 sub-tests at the canonical operating point; ~1s total runtime.

### Phase 4 — Stress entry point reconciliation — DONE

Renamed `TestGenerateBatchReport` → `TestStress` (file: `batch_report_test.go` → `stress_test.go`, via `git mv` to preserve history). Filters Catalog via `ScenariosWithMode(Catalog, ModeStress)`. Makefile's `consensustest-report` target updated. `WriteReportData` is called only by `TestStress` — confirmed by grep.

### Phase 5 — Network model — DONE

`StressProfile`'s default network is now `JitteredDelay{D: btt, Jitter: btt/4}` (±25% relative variance). The non-network-varying sweeps (canonical, cluster_scaling, btt_degradation) build per-point bases with `JitteredDelay`; the network-varying sweeps (heavy_tail, loss) keep their explicit `LogNormalDelay` / `LossyNetwork(ConstantDelay)` models. `CorrectnessProfile` keeps `ConstantDelay`.

### Phase 6 — Cleanup — DONE (this commit)

- Removed unused `Profile.Network` field (it duplicated `BaseConfig.Network`).
- Removed unused `Profile.BatchConfig()` method.
- Fixed `lossSweep` dropping `Modes` when wrapping scenarios.
- Added `ScenariosWithMode(catalog, mode)` helper; both entry points use it.
- Extracted `OBFT/RuleN/*` rule name string literals to constants in the OBFT adapter package.
- Extended `SafetyPanic` to dump `CommitAttestation` diagnostic fields when any new invariant fires.
- Added `safety_test.go` covering all four new invariants (OK + violation cases + the "gated on Decided" semantic).
- Brought sweep descriptions and the catalog header doc in line with the per-tier network model.
- This plan doc updated to reflect what shipped (vs. earlier aspirational language about `Expectation{Kind,Reason}`, `*rand.Rand` threading, and a modular invariant-checker registry).

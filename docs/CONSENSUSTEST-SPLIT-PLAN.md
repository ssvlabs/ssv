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
    Network    NetworkModel // ConstantDelay vs LogNormalDelay / JitteredDelay
    Iterations int          // 1 vs 50–100+
    Seed       SeedStrategy // fixed batch seed; iter seed = batchSeed + iterIndex
    Assertions AssertLevel  // Hard | SafetyOnly
}
```

Scenario bodies receive `*rand.Rand` from the profile. In correctness mode the RNG is seeded → deterministic faults (fixed round, fixed victim). In stress mode the RNG is re-seeded per iteration (`iterSeed = batchSeed + iterIndex`) → stochastic faults, reproducible at the batch level. No mode-conditional `if profile == correctness` logic inside scenarios.

### Per-scenario expectations

Each scenario declares its expected outcome per protocol:

```go
type Expectation struct {
    Kind   ExpectKind // Decide | Miss | NotApplicable
    Reason string     // optional, used by Miss (and Decide with strict reason)
}

type Scenario struct {
    // ... existing fields ...
    Modes             []Mode                  // {Correctness, Stress}
    ExpectByProtocol  map[string]Expectation  // keys: "OBFT", "QBFT"
}
```

The correctness runner picks the entry matching the protocol under test. Missing entry = test fails with "scenario X has no correctness expectation for protocol Y" — forces every catalog scenario to declare its expectations explicitly.

### Assertion layers

- **Safety invariants** — framework-level, always checked, both profiles. Implemented once in the runner. A violation fails the test regardless of profile. Enumerated below.
- **Correctness expectations** — per-scenario, correctness profile only. Examples: "scenario X decides with reason=Y", "scenario Z is n/a for QBFT". Defined on the scenario via `ExpectByProtocol`; invoked only by the correctness runner.
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

The invariant-checker is modular: each invariant is a struct/function the runner calls. Adding a sixth check later is a one-line registration.

### Test entry points

- `TestCorrectness` — iterates `Catalog.WhereMode(Correctness)`, runs each with `CorrectnessProfile` at a single operating point per scenario (no sweeps). Asserts per-scenario `ExpectByProtocol` + safety invariants. Hard fail on any mismatch.
- `TestStress` — iterates `Catalog.WhereMode(Stress)`, runs each scenario across each sweep point with `StressProfile`-derived `BatchConfig`. Safety invariants only. Always emits `data.js`.

If a scenario needs behavioral assertions at multiple operating points (e.g., `n=4` AND `n=10`), it is expressed as separate catalog entries — never as a sweep within `TestCorrectness`.

## Phased plan

### Phase 1 — Foundations (additive, behavior unchanged)

1. Add `ct.Profile` type + `ct.CorrectnessProfile` / `ct.StressProfile` presets.
2. Add `ct.SafetyInvariants` checker covering all 5 invariants (universal + OBFT-specific), invoked by the batch runner after every iteration regardless of profile.
3. Add `Modes []Mode` field on `ct.Scenario`; default all existing entries to `{Stress}` so the current report run stays identical.
4. Add `ExpectByProtocol map[string]Expectation` field on `ct.Scenario`; leave empty for now (filled in Phase 2).

### Phase 2 — Scenario refactor

5. Audit each scenario in `catalog.go`. For each:
   - Decide which modes it supports (`Correctness` / `Stress` / both).
   - Fill in `ExpectByProtocol` for each protocol it applies to.
   - Identify any hard assertions baked into the scenario body; remove (now redundant — handled by runner via `ExpectByProtocol`).
   - If the scenario uses randomness (fault timing, victim selection), take an `*rand.Rand` parameter and thread it through the profile.
6. Run `make consensustest-report` and confirm the resulting `data.js` is unchanged.

### Phase 3 — Correctness entry point

7. Add `TestCorrectness` (file `correctness_test.go`): iterates `Catalog.WhereMode(Correctness)`, runs each with `CorrectnessProfile`, asserts `ExpectByProtocol` + safety invariants.
8. Fold any existing single-shot Go tests that duplicate scenarios into correctness opt-ins on the catalog scenarios themselves.

### Phase 4 — Stress entry point reconciliation

9. Rename `TestGenerateBatchReport` → `TestStress`. It iterates `Catalog.WhereMode(Stress)`, runs each through `StressProfile`-derived `BatchConfig` for each sweep point.
10. Ensure only `TestStress` calls `reporting.WriteReportData`.

### Phase 5 — Network model

11. Switch `StressProfile` default network to a jittered model (LogNormal or Jittered) for sweeps that don't already vary network shape. Existing sigma sweep stays as-is.
12. `CorrectnessProfile` keeps `ConstantDelay`.

### Phase 6 — Cleanup

13. Remove config knobs subsumed by Profile (per-scenario iteration counts, ad-hoc RNG seeds, etc.).
14. Update `docs/CONSENSUS-TEST-PLAN.md` to reference the two-tier model and link this plan.

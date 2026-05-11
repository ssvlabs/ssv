# Broadcast-budget redesign — plan

Two coupled changes, executed in one PR:

1. **OBFT default deepest `B_{K-1}` changes from `5.5·BTT` to `T_commit`** ("earliest possible" — deepest leader broadcasts at slot start). At K=4 the default schedule becomes `[1·BTT, 1.5·BTT, 2.5·BTT, T_commit]`. The clamp-at-0 logic added in the previous change becomes the *normal* operating point for the deepest layer.

2. **2abOBFT gains per-layer staggered broadcast deadlines**, mirroring OBFT. Single cluster-wide `T_broadcast_max = T_verdict_start − 2·BTT` is replaced with per-layer `T_broadcast_max_k = max(0, T_verdict_start − B_k)`. Default schedule `[1·BTT, 1.5·BTT, 2.5·BTT, T_verdict_start]` at K=4. Phase-2a's `Δ_2a + 1·BTT = 3·BTT` re-flood absorption is documented as additive on top of per-layer `B_k`.

Both share one design move: deepest layer of the staggered schedule sits at the protocol's anchor point (`T_commit` for OBFT, `T_verdict_start` for 2abOBFT), so `T_broadcast_max_{K-1} = 0` by default.

## Decisions

| # | Question | Decision |
|---|---|---|
| 1 | Schedule-helper signature: silent malformed output vs. error return when `T_commit < 2.5·BTT`? | **Return error.** Three helpers (`obft.DefaultBroadcastBudget`, `runner/obft.DefaultBroadcastBudgetSchedule`, `consensustest.DefaultBkSchedule`) get `([]time.Duration, error)` return. |
| 2 | `compressedTestBroadcastBudget` helper in [runner_test.go](../protocol/v2/ssv/runner/obft/runner_test.go) — keep with updated comment, or replace with default call now that the default fits? | **Replace with default call inline.** Default `DefaultBroadcastBudgetSchedule(4, 30ms, 200ms)` produces `[30, 45, 75, 200]ms` which fits the test's tight timing; the special-case helper is no longer needed. |
| 3 | [obft-impl.md](obft-impl.md) — historical D6 entry mentions `5.5·BTT`. Update or leave as history? | **Delete the file entirely.** All 8 D-items are marked Fixed (line 20: "All D-items resolved"), no inbound links from anywhere in the repo. The file has fulfilled its purpose. |
| 4 | [OBFT.md §Failure modes Class A bullet on host-side hard deadline](OBFT.md) (with the "degraded case `B_k > T_commit`" qualifier added last change) — keep the qualifier? | **Simplify.** Under the new default the deepest layer always clamps to 0; "degraded case" is no longer a qualifier — it's the normal operating point. Drop the qualifier, describe the `T_commit` fallback as the standard behavior at the deepest layer. |

## Helper API (breaking signature change)

All three return `error` if `T_commit ≤ 2.5·BTT` (default `B_{K-2} = 2.5·BTT` would no longer be strictly less than the deepest):

```go
// protocol/v2/obft/types.go
func DefaultBroadcastBudget(K int, btt, tCommit time.Duration) ([]time.Duration, error)

// protocol/v2/ssv/runner/obft/config.go
func DefaultBroadcastBudgetSchedule(K int, btt, tCommit time.Duration) ([]time.Duration, error)

// protocol/v2/consensustest/schedule.go
func DefaultBkSchedule(K int, btt, tCommit time.Duration) ([]time.Duration, error)
```

All produce `[1·BTT, 1.5·BTT, 2.5·BTT, T_commit]` at K=4. K=3 → `[1·BTT, 2.5·BTT, T_commit]`. K≥5 → interpolate intermediate budgets between `2.5·BTT` and `T_commit` in duration space.

### Internal data-structure changes in [runner/obft/config.go](../protocol/v2/ssv/runner/obft/config.go)

- `defaultLayerSchedules[K].budgetBTT100` shrinks to length `K-1` (shallow layers only); deepest is always `T_commit`.
- `deepestBudgetDefaultBTT100 = 550` constant removed.
- `interpolatedBudgetSchedule` signature: deepest endpoint becomes `time.Duration` (an absolute), not `int` (BTT-hundredths). Interpolation in duration space.

## Sanity-check operating points

Default `[1·BTT, 1.5·BTT, 2.5·BTT, T_commit]` strict-increasing requires `T_commit > 2.5·BTT`:

| Operating point | BTT | T_commit | Schedule (ms) | Valid? |
|---|---|---|---|---|
| SSV proposer-duty | 200 | 3400 | [200, 300, 500, 3400] | ✓ |
| Degraded BTT=600 | 600 | 2600 | [600, 900, 1500, 2600] | ✓ |
| Compressed runner_test | 30 | 200 | [30, 45, 75, 200] | ✓ |
| Edge BTT=1000 | 1000 | 2000 | would be [1000, 1500, 2500, 2000] | ✗ error returned |

The framework's degraded-BTT sweeps (`btt_degradation`) top out at BTT=600ms — well inside the valid range. The compressed-runner test fits comfortably. The error-return path catches genuinely-broken configs (T_commit too tight for staggered schedule).

## Scope — file-by-file

### Impl (code)

| File | Change |
|---|---|
| [protocol/v2/obft/types.go](../protocol/v2/obft/types.go) | `DefaultBroadcastBudget` signature + body; doc comments on `LayerSpec.BroadcastBudget` mention "default deepest = T_commit". |
| [protocol/v2/obft/sim_test.go](../protocol/v2/obft/sim_test.go) | Caller at line 60 passes `tCommit`; handles error. |
| [protocol/v2/obft/config_test.go](../protocol/v2/obft/config_test.go) | `validBaseConfig`, `validStaggeredConfig` updated. `TestConfig_BroadcastBudget_AllowsDeepestBkOverTCommit` reframed: default deepest now equals T_commit, so the "B_k > T_commit" case becomes "B_k = T_commit + 100ms still validates via clamp". Two-layer-clamp rejection test stays. |
| [protocol/v2/ssv/runner/obft/config.go](../protocol/v2/ssv/runner/obft/config.go) | `defaultLayerSchedules` struct, removal of `deepestBudgetDefaultBTT100`, `interpolatedBudgetSchedule` refactor, `DefaultBroadcastBudgetSchedule` signature, doc comments at lines 75, 77, 85, 121, 127, 164, 250, 279, 284. |
| [protocol/v2/ssv/runner/obft/config_test.go](../protocol/v2/ssv/runner/obft/config_test.go) | `TestDefaultBroadcastBudgetSchedule_EndpointConstantsMatchK4` reframed — guards "deepest equals tCommit param" rather than constant; comments at lines 14, 26, 41 updated. |
| [protocol/v2/ssv/runner/obft/runner_test.go](../protocol/v2/ssv/runner/obft/runner_test.go) | Replace `compressedTestBroadcastBudget` helper with inline `DefaultBroadcastBudgetSchedule(4, 30ms, 200ms)` call; remove the obsolete "5.5·BTT exceeds TCommit" comment. |
| [protocol/v2/consensustest/schedule.go](../protocol/v2/consensustest/schedule.go) | `DefaultBkSchedule` signature + body; `ld = 5.5` constant removed. `DefaultFetchSchedule` adjusted to consume new helper. |
| [protocol/v2/consensustest/protocol.go](../protocol/v2/consensustest/protocol.go) | Caller at line 298 passes `tCommit`, handles error in the existing `Validate()` flow. |
| [protocol/v2/consensustest/obft/byz.go](../protocol/v2/consensustest/obft/byz.go) | Comment update at line 625 (B_3 = 5.5 BTT → T_commit). |
| [protocol/v2/consensustest/catalog.go](../protocol/v2/consensustest/catalog.go) | Comment update at line 817 (1100ms timing reference). |

### Spec — OBFT

[docs/OBFT.md](OBFT.md) — every `5.5 BTT`, `1100ms`, `B_3 = 5.5 BTT`, `B_{K-1} = 5.5 BTT` reference revised. ~22 occurrences across these sections:

- Intro (line 7)
- "Not suited for" (line 17)
- §Setting Sizing intuition (line 48)
- §Assumed partial synchrony (line 59)
- §Phase 1 broadcast deadlines (line 147)
- §Slot structure (line 333)
- §Liveness (lines 397, 461)
- §Liveness comparison table (lines 499, 512, 513, 518)
- §Failure modes Class A (lines 593, 600); **simplify Class A "Host-side hard deadline" bullet (line 604)** per decision 4 — drop "degraded case" qualifier.
- §Properties summary (lines 638, 646)
- §Application Timing budget (lines 683, 692, 750)
- §Practical caveats (line 773)
- §A.1 OBFTR comparison table (line 833)
- §Appendix L_Bid table (line 1239)

Concrete substitutions at Config A (T_commit=3400ms, BTT=200ms):
- "B_3 = 5.5 BTT = 1100ms" → "B_3 = T_commit = 3400ms (earliest possible; deepest leader broadcasts at slot start)"
- "L_3 broadcasts by T_commit − 5.5 BTT = 2300ms" → "L_3 broadcasts at slot_start (T_broadcast_max_3 = 0)"
- "V_3 covers real propagation up to 1100ms" → "V_3 covers real propagation up to T_commit = 3400ms (entire slot)"

### Spec — 2abOBFT

[docs/2abOBFT.md](2abOBFT.md) — 9 single-deadline references → per-layer + 1 new §Setting subsection:

- §Setting (lines 44-48): single-deadline definition → per-layer `T_broadcast_max_k = max(0, T_verdict_start − B_k)`. Add `B_k` sizing bullet mirroring OBFT.md §Setting (target-not-cap precision, clamp-at-0 semantics for degraded `B_k > T_verdict_start`).
- New §Setting subsection — **"Per-layer B_k composes with Phase-2a re-flood absorption"**: Phase-2a's `Δ_2a + 1·BTT = 3·BTT` absorption is uniform across layers; per-layer `B_k` adds asymmetric MEV-fetch + extra pre-Phase-2a propagation budget for deep layers. Both mechanisms compose, neither substitutes.
- §Bundle propagation (line 147): `T_broadcast_max` → `T_broadcast_max_k`.
- §Slot structure Phase 1 (line 327): constraint becomes `T_k + Δ_1 ≤ T_broadcast_max_k`.
- §Trust model partial synchrony (lines 394, 396): three-cutoff section generalized; effective absorption window per-layer = `(Δ_2a + 1·BTT) + (B_k − 2·BTT)`.
- §Late-bundle deepest-layer recovery (line 537): `T_broadcast_max` → `T_broadcast_max_{K-1}`.
- Properties summary table (line 682): per-layer formula + target framing.
- Timing budget table (line 697): single "Phase 1 fetch (effective) 1200ms" row expanded into per-layer V_0..V_3 rows mirroring OBFT.md.
- Wire/protocol checklist (line 730): per-layer bullet.

Default 2abOBFT schedule at K=4: `[1·BTT, 1.5·BTT, 2.5·BTT, T_verdict_start]`. Deepest 2abOBFT leader broadcasts at slot start (analogous to OBFT).

### Cross-doc

| File | Change |
|---|---|
| [docs/BFT-comparison.md](BFT-comparison.md) | Line 42 (OBFT row, 5.5 BTT mention), line 223 (OBFT staggered budgets), lines 276-282 (2abOBFT row expands into per-layer V_0..V_3 budgets; OBFT row updated to T_commit). |
| [docs/CONSENSUS-TEST-PLAN.md](CONSENSUS-TEST-PLAN.md) | Line 22 — signature reference for `DefaultBkSchedule`. |
| [docs/obft-impl.md](obft-impl.md) | **Delete** — all deltas resolved, no inbound links, doc is historical. |
| [docs/OBFTR.md](OBFTR.md) | No 5.5 BTT references; no change. |
| [docs/OBFT-formal-verif.md](OBFT-formal-verif.md) | Line 290's `T_broadcast_max_k = T_commit − B_k` uses general form; no change. |

## Sequencing

1. **Impl signatures + bodies**: `obft.DefaultBroadcastBudget` → `runner/obft.DefaultBroadcastBudgetSchedule` → `consensustest.DefaultBkSchedule`. All three return `([]time.Duration, error)`. Internal helpers (`defaultLayerSchedules`, `interpolatedBudgetSchedule`, `ld` constant) updated.
2. **Direct test-file callers**: `config_test.go` × 2, `sim_test.go`, `runner_test.go` (with `compressedTestBroadcastBudget` replacement per decision 2).
3. **Build + test gate**: `go build ./... && go test ./protocol/v2/obft/... ./protocol/v2/ssv/runner/obft/... ./protocol/v2/consensustest/...` green before touching docs.
4. **OBFT.md rewrite** (~22 occurrences).
5. **2abOBFT.md rewrite** + new §Setting subsection.
6. **Cross-doc updates** (BFT-comparison.md, CONSENSUS-TEST-PLAN.md).
7. **Delete obft-impl.md** per decision 3.
8. **Regenerate consensustest report** (`REPORT_DIR=/tmp/redesign ITERATIONS=10 go test -run TestGenerateBatchReport ./protocol/v2/consensustest/`) and confirm no `out of envelope` lines.

## Verification

- `go build ./...` green.
- `go test ./...` green.
- `make lint` green.
- Grep `5\.5\|1100ms` in repo (excluding `.claude/worktrees`): zero remaining occurrences in code or current docs.
- Consensustest report at BTT=600ms produces real OBFT data for all 29 scenarios (the clamp is now the default-schedule normal operating point).

## Out of scope

- 2abOBFT implementation work — there is no production or test code that implements 2abOBFT.
- OBFTR-family schedule changes — uses cross-round retention, not per-layer staggering.
- L_Bid extension schedules — inherits OBFT's primary schedule via `T_0_broadcast_max`; the deepest under L_Bid is still T_commit-anchored, no separate change needed.

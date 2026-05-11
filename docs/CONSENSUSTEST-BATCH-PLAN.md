# `consensustest` batch-comparison framework plan

Follow-on to [`docs/CONSENSUS-TEST-PLAN.md`](CONSENSUS-TEST-PLAN.md) (the original framework build) and [`docs/OBFT-SPEC-ALIGNMENT-PLAN.md`](OBFT-SPEC-ALIGNMENT-PLAN.md) (spec-correspondence pass). This plan extends `consensustest` from per-scenario single-sim correctness checking to multi-sim batch comparison with distribution-aware stats and chart-based reporting — so OBFT and QBFT can be plugged in and compared on success rate, latency distribution, and bandwidth distribution under varying network conditions.

Total estimated effort: ~13–16 hours across 6 phases / ~6-8 commits.

## Goals

- Run each `(scenario, protocol)` cell `N` times with deterministic per-seed reproducibility, aggregate the outcomes into distributions (P50/P90/P99 + min/max/mean/stddev).
- Surface success rate, decision-time distribution, and bandwidth distribution as the three primary headline metrics; preserve per-rule evidence counts and miss-reason taxonomy as secondary.
- Render results as comparison charts using the existing [`reporting/`](../protocol/v2/consensustest/reporting) Chart.js scaffolding — success-rate bars, latency P50/P90/P99 grouped bars, per-kind bandwidth stacked bars, latency-vs-success trade-off scatter, cluster-size scaling lines.
- Sweep over the parameter axes that matter most to OBFT-vs-QBFT comparison: cluster size, BTT, network jitter intensity, loss rate, K. The Cartesian product becomes a tabbed report (one chart set per axis-point).
- Add production-fidelity network models (heavy-tail propagation, stochastic loss with bursts, correlated per-link flakiness) so the batch results approximate real SSV mainnet conditions rather than uniform-jitter idealization.

## Non-goals

- Replace the existing single-sim test paths. Default `go test` runtime stays unchanged; batch reports are gated on env vars and run on demand.
- Full real-network integration (libp2p mocks, multi-hop GossipSub fan-out). Framework remains virtual-time DES — multi-hop is approximated by the per-pair `NetworkModel` abstraction.
- Live calibration against SSV mainnet telemetry. Production-fidelity network models land with sensible defaults; per-deployment calibration is left as separate work (a `// CALIBRATE` comment block on each model points to where data would come from).
- Per-iteration trace storage. Batch runs discard the per-sim event traces and retain only aggregated stats (otherwise memory blows up at N=1000).

## Phase A — Foundation (~3-4 h)

Land first; phases B-F depend on the batch-runner + distribution types.

### Task A1 — Batch-runner API

**Why.** Existing `RunScenarioOnProtocol` runs one sim per call. Aggregating distributions requires invoking it N times with stepped seeds and accumulating results.

**Action.** New `batch.go` in `protocol/v2/consensustest/`:

- `BatchConfig{Iterations int, SeedStart int64, Base SimConfig, Scenarios []Scenario, Protocols []Protocol, Parallelism int}` — input.
- `RunBatch(t *testing.T, cfg BatchConfig) BatchReport` — drives `Iterations × len(Scenarios) × len(Protocols)` sims. Each `(scenario, protocol)` cell runs in its own goroutine; per-iteration seed = `SeedStart + iter` so the (scenario, protocol, iter) triple is reproducible.
- Parallelism via worker pool sized to `cfg.Parallelism` (default `GOMAXPROCS`). Sims within a cell are independent; the framework is already deterministic per seed.
- Safety violations panic per the existing `RunScenarioOnProtocol` contract — they should never occur in a correctly-implemented protocol, so a panic is the right signal.

**Accept.** Canonical-config run (`n=4, BTT=200ms, 100 iterations × 25 scenarios × 2 protocols = 5000 sims`) completes in under 30 seconds on a typical dev machine. Re-running with the same `SeedStart` produces byte-identical `BatchReport` stats.

**Effort.** ~2 h.

### Task A2 — Distribution + BatchReport types

**Why.** Each cell needs distribution-aware aggregation; the existing `Outcome` is single-sample.

**Action.** New `stats.go`:

- `Distribution []float64` with helper methods: `Mean()`, `Median()`, `Percentile(p float64)`, `Min()`, `Max()`, `Stddev()`, `Len()`. Median is just `Percentile(50)`. Use linear interpolation for percentile calculation.
- `BatchCell{Protocol, Scenario string; Iterations int; SuccessRate float64; DecisionTime Distribution; ClusterBandwidth Distribution; PerKindBandwidth map[string]Distribution; EvidenceCounts map[string]Distribution; MissReasons map[string]int; SafetyViolations int}` — cell aggregate. `DecisionTime` includes only successful sims; `ClusterBandwidth` includes all sims (a deadlock still emits bandwidth before timeout).
- `BatchReport{Config BatchConfig; Cells []BatchCell; Wallclock time.Duration; GeneratedAt time.Time}` — top-level result.

Plus a smoke test verifying `Distribution.Percentile(50) == Median()`, percentile interpolation at boundary cases (empty, single element, two elements), and that the same seeded run twice produces identical `BatchCell` stats.

**Accept.** `Distribution` math passes property tests (median bounds, monotone percentiles, mean within stddev of median for log-normal samples).

**Effort.** ~1-2 h.

## Phase B — Production-fidelity network models (~2-3 h)

Adds the three models flagged in the prior analysis to [`network.go`](../protocol/v2/consensustest/network.go). All composable on top of existing `Inner NetworkModel` chains.

### Task B1 — `LogNormalDelay`

`LogNormalDelay{Median, Sigma time.Duration}` — replaces uniform jitter with log-normal-distributed delay (heavy tail). Calibratable defaults: `Median=150ms, Sigma=0.3` for canonical BTT=200ms operating point. Determinism preserved via the seeded `*math/rand.Rand` passed to `Delay`.

Smoke test: across 10,000 draws at `Median=150ms, Sigma=0.3`, verify `P50 ≈ 150ms`, `P99/P50 ≈ 2x`, `Max < 10*Median`.

### Task B2 — `LossyNetwork`

`LossyNetwork{Inner NetworkModel; LossRate float64; BurstFactor int}` — Bernoulli loss with burst correlation via a two-state Markov chain. `LossRate` is the steady-state loss probability; `BurstFactor` is the mean dwell time (in messages) in the "bad" state. Lost messages return a sentinel `1*time.Hour` delay (same convention as `PartitionedNetwork`).

Smoke test: at `LossRate=0.05, BurstFactor=5`, across 10,000 draws, observe loss rate within ±0.5% of `0.05`; lost-burst length distribution should match expected Markov dwell time.

### Task B3 — `CorrelatedLinkDelay`

`CorrelatedLinkDelay{Inner NetworkModel; BadLinkProb float64; BadLinkMultiplier float64; BurstDuration time.Duration}` — per-pair sustained flakiness. Each `(from, to)` pair enters a "bad" state with probability `BadLinkProb` per `BurstDuration` window; while in bad state, all messages on that link have delay multiplied by `BadLinkMultiplier`. State is keyed by `(from, to)` so the same pair stays correlated within a window.

Smoke test: at `BadLinkProb=0.2, BadLinkMultiplier=3, BurstDuration=1s`, observe expected per-pair bad-window fraction.

### Task B4 — Optional: size-weighted delay

(Lower priority; included if Phase B comes in under budget.) `SizedDelay{Inner NetworkModel; BytesPerSec int64}` — adds size-weighted delay component. Requires extending `NetworkModel.Delay` signature with a `bytes int64` arg. Backwards-compatible if callers pass `0` for non-size-aware paths.

**Accept (Phase B).** Each new model has a smoke test confirming distribution shape; existing scenarios still pass with default `ConstantDelay`; switching one scenario to `LogNormalDelay` produces a higher P99 latency in the corresponding batch cell.

**Effort.** ~2-3 h.

## Phase C — Reporting extensions (~4-5 h)

Extends [`reporting/`](../protocol/v2/consensustest/reporting) to render `BatchReport` as charts.

### Task C1 — Batch report types

`reporting/batch_run.go`:
- `BatchRun{Title, Description string; Cells []BatchCell; Config string; GeneratedAt time.Time}` — render-layer struct analogous to existing `Run`.
- `NewBatchRun(title, description string, br consensustest.BatchReport) *BatchRun` — converter.
- `RenderBatchHTML(*BatchRun, path string) error`, `RenderBatchCSV`, `RenderBatchMarkdown`.

### Task C2 — Chart renderers

Extends `reporting/html.go` with five Chart.js chart types:

1. **Success-rate bar chart.** One bar pair per scenario (OBFT in blue, QBFT in orange). Y-axis 0-1.
2. **Decision-time P50/P90/P99 grouped bars.** Per scenario, six bars: OBFT P50, OBFT P90, OBFT P99, QBFT P50/P90/P99. Y-axis log scale option.
3. **Per-kind bandwidth stacked bars.** Per scenario, two bars (one per protocol), each stacked by message kind (LeaderBroadcast / Commit / Certificate / RoundChange).
4. **Trade-off scatter.** X = decision-time P99 (ms), Y = success rate. One dot per `(scenario, protocol)`. Two colors. Hover shows scenario name.
5. **Cluster-size scaling lines.** For the cluster-scaling sweep specifically: X = N, Y = decision-time P99. Two lines (OBFT/QBFT), one chart per scenario class.

Charts use Chart.js core (already CDN-loaded). Box plots avoided — use grouped bars at P50/P90/P99 instead, which Chart.js core handles natively. If box plots become essential, chartjs-chart-boxplot via the same CDN pattern is the upgrade path.

### Task C3 — CSV + Markdown

CSV: per-cell rows with columns `protocol, scenario, iterations, success_rate, decision_p50_ms, decision_p90_ms, decision_p99_ms, decision_max_ms, bandwidth_p50_b, bandwidth_p99_b, evidence_<rule>_total`. Per-iteration rows optionally written to a separate file when `EmitPerIteration=true`.

Markdown: summary tables — one per metric (success rate, P99 latency, P99 bandwidth) — with rows per scenario and columns per protocol. Renders nicely on GitHub.

**Accept.** `RenderBatchHTML` produces a file that opens in a browser with all five chart types populated; chart axes are correctly labeled; protocol legends are visible.

**Effort.** ~4-5 h.

## Phase D — Scenario sweeps (~2 h)

### Task D1 — Sweep DSL

`sweep.go`:
- `SweepAxis` enum: `AxisClusterSize`, `AxisBTT`, `AxisJitterSigma`, `AxisLossRate`, `AxisK`.
- `Sweep{Axes []SweepAxis; Values map[SweepAxis][]any; BaseConfig BatchConfig}` — Cartesian-product axis specification.
- `RunSweep(t, Sweep) []BatchReport` — one `BatchReport` per axis-point.

### Task D2 — Curated default sweeps

`DefaultSweeps()` returns five sweeps that cover the comparison story:

1. **Canonical** — `n=4, BTT=200ms, K=4, ConstantDelay`. The reference point.
2. **Cluster scaling** — `n ∈ {4, 7, 10, 13}`. Shows protocol scaling behavior.
3. **Network degradation** — `BTT ∈ {100ms, 200ms, 400ms, 600ms}`. Envelope-fit curves.
4. **Heavy-tail propagation** — `LogNormalDelay with Sigma ∈ {0.1, 0.3, 0.5, 0.7}`. P99/P50 ratio effect.
5. **Loss** — `LossyNetwork{LossRate ∈ {0, 0.01, 0.05, 0.1}}`. Drop tolerance.

**Accept.** Each default sweep runs without error and produces a `BatchReport` per axis-point.

**Effort.** ~2 h.

## Phase E — Driver test + tooling (~1 h)

### Task E1 — Driver test

`batch_report_test.go`:
- `TestGenerateBatchReport` — analogous to existing `TestGenerateReport`, gated on env var `BATCH_REPORT_DIR`.
- Iterations configurable via `BATCH_ITERATIONS` env (default 100).
- Runs each `DefaultSweeps()` sweep, writes HTML/CSV/MD output per sweep to `$BATCH_REPORT_DIR/<sweep_name>.{html,csv,md}`.

### Task E2 — `make` target

`make consensustest-batch-report` in the project Makefile:
```
consensustest-batch-report:
    BATCH_REPORT_DIR=./reports BATCH_ITERATIONS=100 go test -timeout 30m \
        -run TestGenerateBatchReport ./protocol/v2/consensustest/
```

**Accept.** `make consensustest-batch-report` from a clean checkout produces `./reports/canonical.html`, `./reports/cluster_scaling.html`, etc., each opening in a browser with the comparison charts.

**Effort.** ~1 h.

## Phase F — Documentation + calibration notes (~1 h)

### Task F1 — Framework doc

`docs/CONSENSUSTEST-REPORT.md` — usage guide for the batch framework:
- How to add a new sweep.
- How to interpret each chart type.
- Default iteration count rationale (100 is sufficient for P99 stability at success rates ≥ 50%; bump to 1000 for rare-success scenarios).
- Determinism guarantee statement.

### Task F2 — Calibration anchors

A `// CALIBRATE` comment block on each production-fidelity network model documenting:
- Which observable phenomenon the parameter maps to.
- Default value rationale.
- Where calibration data would come from (mainnet telemetry endpoint, peer-score logs, dashboard panel).

Leave actual calibration as separate follow-on if/when SSV telemetry pipeline is queryable.

**Effort.** ~1 h.

## Sequencing and rollout

Per-phase commit order:

1. **Phase A:** A1 → A2 (one commit per task).
2. **Phase B:** B1, B2, B3 (one commit each); B4 optional.
3. **Phase C:** C1 → C2 → C3 (one commit per task; C2 is the heaviest).
4. **Phase D:** D1 → D2.
5. **Phase E:** E1 → E2.
6. **Phase F:** combined commit.

Per commit: run `go test ./protocol/v2/consensustest/...` to confirm no regression. Driver test gated on env var.

**Highest risk:** C2 Chart.js wiring — first time the framework adds non-trivial chart configurations. Backup plan if Chart.js core proves limiting: fall back to flat HTML tables for the affected chart types until a follow-on iteration upgrades to chartjs-chart-boxplot or similar.

## Trade-offs

1. **Default iteration count.** N=100 gives stable P99 for success-rate ≥ 50% scenarios. Rare-success cases (e.g., `MeshFlakiness` at 0% on OBFT) need more iterations for the failure-mode taxonomy; configurable via env var.
2. **Batch under jitter by default?** Canonical config gives the cleanest baseline. Jitter scenarios are the differentiator — handled by the "Heavy-tail propagation" sweep specifically. Plan defaults to canonical for the matrix view.
3. **Real-BLS in batch?** Stub mode is fine for protocol-shape comparison and Phase A-F default. Real BLS for actual performance benchmarking — gated on a separate `BATCH_REAL_BLS=true` env. Estimated ~30 min for 1000-sim sweep under real BLS.
4. **Calibration data dependency.** Production-fidelity models become genuinely useful once calibrated from observed SSV mainnet propagation telemetry. Without it, defaults are informed estimates. Worth a separate conversation with whoever owns SSV telemetry.

## Net result after execution

- New framework files: `batch.go`, `stats.go`, `sweep.go`, `batch_report_test.go`.
- New network models in `network.go`: `LogNormalDelay`, `LossyNetwork`, `CorrelatedLinkDelay`. Optional: `SizedDelay`.
- New reporting files: `reporting/batch_run.go`, plus chart-renderer additions to `reporting/html.go`.
- New make target: `make consensustest-report` (originally introduced as `consensustest-batch-report`; consolidated post-implementation since this became the canonical comparison report).
- New doc: `docs/CONSENSUSTEST-REPORT.md` (originally `CONSENSUSTEST-BATCH.md`).

Estimated total: ~13–16 hours / 6-8 commits.

## Explicitly out of scope

- Live-network integration (libp2p, GossipSub multi-hop fan-out).
- Calibration of production-fidelity models against mainnet data.
- Cross-language adapters (Rust, JS implementations). Framework stays Go.
- Per-iteration trace storage (would blow up memory at high N).
- Per-validator-duty scenarios (proposer / attestation / sync committee). The plan targets the proposer-duty operating point only — attestation/sync-committee comparison is a follow-on if needed.

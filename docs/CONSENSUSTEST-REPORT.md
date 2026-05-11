# `consensustest` comparison report framework

A multi-sim comparison framework for SSV consensus protocols. Plugs OBFT and QBFT into the same scenario matrix, runs each `(scenario, protocol)` cell N times with deterministic per-seed reproducibility, and renders the aggregated distributions as Chart.js comparison reports.

Built on top of the per-scenario [`consensustest`](../protocol/v2/consensustest/) engine — see [docs/CONSENSUS-TEST-PLAN.md](CONSENSUS-TEST-PLAN.md) for the underlying scenario-execution layer and [docs/CONSENSUSTEST-BATCH-PLAN.md](CONSENSUSTEST-BATCH-PLAN.md) for the design rationale.

## Quick start

```bash
make consensustest-report
open ./consensustest-reports/index.html
```

That runs the five curated sweeps × OBFT/QBFT × 100 iterations per cell (≈ 21k sims total) in ~75 seconds and opens the navigation index in your browser.

Override defaults:

```bash
# Higher iteration count for rare-event scenarios:
ITERATIONS=1000 make consensustest-report

# Different output directory:
REPORT_DIR=/tmp/report-out make consensustest-report
```

## What you get

`./consensustest-reports/index.html` lists 17 per-(sweep, point) reports grouped by sweep:

- **canonical** (1 point) — the baseline. n=4, BTT=200ms, K=4, `ConstantDelay`.
- **cluster_scaling** (4 points) — n ∈ {4, 7, 10, 13} at fixed BTT.
- **btt_degradation** (4 points) — BTT ∈ {100, 200, 400, 600}ms at fixed n=4.
- **heavy_tail** (4 points) — `LogNormalDelay` Sigma ∈ {0.1, 0.3, 0.5, 0.7}.
- **loss** (4 points) — `LossyNetwork` LossRate ∈ {0, 0.01, 0.05, 0.10}.

Each per-point HTML report has five sections:

1. **Summary matrix** — scenario × protocol grid; cells show "N% success · P99 Xms" color-coded.
2. **Success rate** bar chart — bars per scenario, OBFT vs QBFT.
3. **Decision time P50/P90/P99 grouped bars** — six bars per scenario (protocol × percentile).
4. **Bandwidth per cell** stacked bars — one bar per cell, stacked by message kind.
5. **Trade-off scatter** — X = P99 latency, Y = success rate, one dot per cell.

Plus per-point CSV (spreadsheet-ready with all percentile columns) and Markdown (renders cleanly in PR descriptions / GitHub).

## How to interpret each chart

**Success rate.** Closer to 1.0 = the protocol decided in every iteration. Different per-scenario — `Healthy` should always be 1.0; `Equivocate_111` is 0.0 for OBFT (spec-expected slot-miss) and 1.0 for QBFT (round-2 recovers).

**P50/P90/P99 latency.** Only successful sims contribute (decision time is undefined on deadlocks). Compare grouped bars: a protocol's three bars (P50/P90/P99) reveal its tail behavior; wider tail at fixed P50 = less predictable. OBFT's healthy P99 hits ~3.85s (the spec's "Phase 3 complete" anchor); QBFT's ~1.7s on healthy / ~3.9s on fall-through.

**Bandwidth stacked.** Each bar's segments show which message kind dominates. OBFT spends most bandwidth on `Commit` (the onion + witnesses + NR partials); QBFT spreads across `LeaderBroadcast` / `Commit` (PREPARE+COMMIT) / `RoundChange` if R1 fails. Per-protocol totals reveal the bandwidth tradeoff at a glance.

**Trade-off scatter.** Top-left corner is the best (low latency + high success). One color per protocol; dots scattered far apart on the X-axis indicate the protocol behaves very differently across scenarios. Hover for the scenario name.

## How to add a new sweep

Suppose you want a sweep over `K` (the OBFT layer count). Add to `consensustest/sweep.go`:

```go
func kSweep(scenarios []Scenario, protocols []Protocol, iterations int) Sweep {
    Ks := []int{3, 4, 5, 6, 7}
    pts := make([]SweepPoint, 0, len(Ks))
    for _, k := range Ks {
        base := DefaultProposerDutyConfig(200 * time.Millisecond)
        base.K = k
        pts = append(pts, SweepPoint{
            Label: "K=" + itoa(k),
            Config: BatchConfig{
                Iterations: iterations,
                Base:       base,
                Scenarios:  scenarios,
                Protocols:  protocols,
            },
        })
    }
    return Sweep{Name: "K_sweep", Description: "...", AxisLabel: "K", Points: pts}
}
```

Then register it in `DefaultSweeps`:

```go
return []Sweep{
    canonicalSweep(...),
    clusterScalingSweep(...),
    // ... existing ...
    kSweep(scenarios, protocols, iterations),  // new
}
```

Run `make consensustest-report` — the new sweep appears in `index.html` automatically.

## How to use the batch framework directly (not via DefaultSweeps)

For one-off comparisons not worth turning into a curated sweep:

```go
import (
    ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
    obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
    qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
    "github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

cfg := ct.BatchConfig{
    Iterations: 200,
    SeedStart:  1,
    Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
    Scenarios:  ct.Catalog,
    Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
}
report := ct.RunBatch(t, cfg)
br := reporting.NewBatchRun("My experiment", "...", report)
reporting.RenderBatchHTML(br, "/tmp/out.html")
```

## Iteration count rationale

- **100 (default)** — stable P99 estimate for scenarios with success rate ≥ 50%. Binomial 99% CI on success rate at p=0.5, N=100 is ±0.13; tighter for high-success scenarios.
- **1000** — needed when investigating rare-event scenarios (the `MeshFlakiness` deadlock at borderline parameters, low-rate `LossyNetwork` bursts, etc.). At N=1000, ~12-15 min wallclock for the full default sweep matrix.
- **10,000** — only useful for very rare events (P9999 latency tail). Several hours wallclock. Not the framework's intended use case.

The framework auto-parallelizes per `(scenario, protocol)` cell up to `GOMAXPROCS` goroutines; sims within a cell run sequentially.

## Determinism guarantee

`RunBatch` is deterministic: re-running with the same `(BatchConfig.Iterations, BatchConfig.SeedStart, BatchConfig.Base, BatchConfig.Scenarios, BatchConfig.Protocols)` tuple produces byte-identical `BatchReport` stats (Cells, SuccessRate, all distribution percentiles).

Wallclock varies across runs; goroutine scheduling reorders WHEN sims execute but not WHICH outputs they produce (each sim is wholly determined by its `(SimConfig, Seed)` input).

This is the load-bearing guarantee for reproducible reports: any anomaly in a generated chart can be re-investigated by re-running the same batch.

## Production-fidelity network models

The framework ships three models calibratable to real SSV mainnet conditions:

- **`LogNormalDelay`** — log-normal-distributed propagation delay. Real gossipsub has a heavy right tail; uniform jitter underestimates it. Sigma controls tail fatness: 0.3 → P99/P50 ≈ 2×; 0.7 → ≈ 5×.
- **`LossyNetwork`** — bursty stochastic loss via a two-state Markov chain. Captures the "mesh churn / peer-score event" pattern where drops cluster in bursts rather than being independent-Bernoulli.
- **`CorrelatedLinkDelay`** — per-pair sustained flakiness. One specific link can be slow while others behave normally — distinct from network-wide bursts.

All three composable on top of `ConstantDelay` / each other. Each model's docstring has a `CALIBRATE` comment block documenting what mainnet observation each parameter should be fit to.

**Current state: defaults are illustrative.** Once SSV mainnet telemetry is queryable for per-message-kind propagation P50/P99 and observed loss rates, the defaults can be tightened to match production. Until then, the models bracket plausible production conditions but don't claim to predict actual mainnet rates.

## Limitations and caveats

- **Per-iteration trace not retained.** Memory would blow up at high N. If you need to inspect a specific failing sim, re-run that single (scenario, protocol, seed) triple via `RunScenarioOnProtocol` with `TraceEnabled=true`.
- **Real-BLS not in batch by default.** Stub crypto is fine for protocol-shape comparison. For real-BLS performance benchmarking, build with the `real_bls` tag and expect ~30 min for the default sweep matrix.
- **Single-cluster mock — no multi-hop GossipSub.** The framework's `NetworkModel` abstracts to per-pair delay; real GossipSub re-floods via mesh peers (~2-3 hops cluster-wide). For n=4 the mesh is effectively full so single-hop is approximately right; for n=13 with multi-subnet topology the gap widens. Not a calibration concern at SSV's typical operating cluster sizes.
- **Cross-point sweep line charts not yet rendered.** Currently each sweep point gets its own per-point report; a "P99 latency vs cluster size" line chart would need a cross-point renderer. Planned follow-on if useful.

## Files

| File | What it adds |
|---|---|
| [`batch.go`](../protocol/v2/consensustest/batch.go) | `BatchConfig`, `RunBatch`, cell runner |
| [`stats.go`](../protocol/v2/consensustest/stats.go) | `Distribution`, `BatchCell`, `BatchReport` |
| [`sweep.go`](../protocol/v2/consensustest/sweep.go) | `Sweep`, `RunSweep`, `DefaultSweeps` |
| [`network.go`](../protocol/v2/consensustest/network.go) | `LogNormalDelay`, `LossyNetwork`, `CorrelatedLinkDelay` |
| [`batch_report_test.go`](../protocol/v2/consensustest/batch_report_test.go) | `TestGenerateBatchReport` driver |
| [`reporting/batch_run.go`](../protocol/v2/consensustest/reporting/batch_run.go) | `BatchRun`, `NewBatchRun`, `SweepIndexEntry` |
| [`reporting/html.go`](../protocol/v2/consensustest/reporting/html.go) | `RenderBatchHTML`, `RenderSweepIndex` |
| [`reporting/csv.go`](../protocol/v2/consensustest/reporting/csv.go) | `RenderBatchCSV` |
| [`reporting/markdown.go`](../protocol/v2/consensustest/reporting/markdown.go) | `RenderBatchMarkdown` |

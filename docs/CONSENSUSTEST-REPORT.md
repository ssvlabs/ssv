# `consensustest` comparison report framework

A multi-sim comparison framework for SSV consensus protocols. Plugs OBFT and QBFT into the same scenario matrix, runs each `(scenario, protocol)` cell N times with deterministic per-seed reproducibility, and renders the aggregated distributions as a single self-contained HTML page with Chart.js panels.

Built on top of the per-scenario [`consensustest`](../protocol/v2/consensustest/) engine — see [docs/CONSENSUS-TEST-PLAN.md](CONSENSUS-TEST-PLAN.md) for the underlying scenario-execution layer and [docs/CONSENSUSTEST-BATCH-PLAN.md](CONSENSUSTEST-BATCH-PLAN.md) for the design rationale.

## Quick start

```bash
make consensustest-report
open ./consensustest-reports/index.html
```

That runs the five curated sweeps × OBFT/QBFT × 100 iterations per cell (≈ 21k sims total) in ~75 seconds and opens a single scrollable comparison page in your browser.

Override defaults:

```bash
# Higher iteration count for rare-event scenarios:
ITERATIONS=1000 make consensustest-report

# Different output directory:
REPORT_DIR=/tmp/report-out make consensustest-report
```

## What you get

One file: `./consensustest-reports/index.html`. The page is a scrollable SPA with a sticky table-of-contents linking to one section per sweep. Five sweeps in `DefaultSweeps`:

- **canonical** (1 point) — n=4, BTT=200ms, K=4, `ConstantDelay`. Reference operating point.
- **cluster_scaling** (4 points) — n ∈ {4, 7, 10, 13} at fixed BTT=200ms.
- **btt_degradation** (4 points) — BTT ∈ {100, 200, 400, 600}ms at fixed n=4.
- **heavy_tail** (4 points) — `LogNormalDelay` Sigma ∈ {0.1, 0.3, 0.5, 0.7}.
- **loss** (4 points) — `LossyNetwork` LossRate ∈ {0, 0.01, 0.05, 0.10}.

Each section renders one of two layouts:

**Single-point sweep → detail layout** (used for `canonical`):
1. Summary matrix — scenario × protocol grid; cells show "N% success · P99 Xms" color-coded.
2. Success rate per scenario — bar chart, OBFT vs QBFT.
3. Decision time P50/P90/P99 grouped bars — six bars per scenario (protocol × percentile).
4. Bandwidth per cell stacked bars — one bar per (scenario, protocol), stacked by message kind.
5. Trade-off scatter — X = P99 latency, Y = success rate, one dot per (scenario, protocol).

**Multi-point sweep → trend layout** (used for the four parameter sweeps):
1. Success rate vs swept axis — line chart, X = parameter value, one line per (scenario, protocol).
2. Decision time P99 vs swept axis — same shape, Y in ms.
3. Bandwidth median vs swept axis — same shape, Y in bytes.

Trend charts use color = scenario, line style = protocol (solid OBFT, dashed QBFT) with point markers (circle OBFT, triangle QBFT). Datasets that are entirely n/a are omitted from the legend to keep it tractable. Chart.js's legend is click-to-filter — toggle individual scenarios on/off to focus.

## How to interpret each chart

**Success rate.** Closer to 1.0 = the protocol decided in every iteration. Different per-scenario — `Healthy` should always be 1.0; `Equivocate_111` is 0.0 for OBFT (spec-expected slot-miss) and 1.0 for QBFT (round-2 recovers).

**P50/P90/P99 latency.** Only successful sims contribute (decision time is undefined on deadlocks). OBFT's healthy P99 hits ~3.85s (the spec's "Phase 3 complete" anchor); QBFT's ~1.7s on healthy / ~3.9s on fall-through.

**Bandwidth.** Stacked bars in the detail layout show which message kind dominates per cell. OBFT spends most bandwidth on `Commit` (the onion + witnesses + NR partials); QBFT spreads across `LeaderBroadcast` / `Commit` (PREPARE+COMMIT) / `RoundChange` if R1 fails.

**Trade-off scatter.** Top-left corner is the best (low latency + high success). Color = scenario; OBFT uses circle markers, QBFT triangles.

**Trend lines.** Curves close together = parameter doesn't matter much for that (scenario, protocol) pair. Curves diverging = scenario crosses an envelope boundary at some value of the parameter. A line dropping to zero on `success rate vs BTT` reveals the protocol's tolerance ceiling.

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
            Label: "K=" + strconv.Itoa(k),
            Config: BatchConfig{
                Iterations: iterations,
                Base:       base,
                Scenarios:  scenarios,
                Protocols:  protocols,
            },
        })
    }
    return Sweep{
        Name: "K_sweep", Title: "OBFT K-layer sweep",
        Description: "K-sweep: K ∈ {3..7}", AxisLabel: "K", Points: pts,
    }
}
```

Then register it in `DefaultSweeps`. Run `make consensustest-report` — the new sweep appears as a new section (with trend layout if >1 point, detail layout if 1 point) automatically.

## How to use the framework directly (not via DefaultSweeps)

For one-off comparisons not worth turning into a curated sweep:

```go
import (
    ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
    obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
    qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
    "github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
sweep := ct.Sweep{
    Name: "experiment", Title: "My experiment",
    Description: "...", AxisLabel: "BTT",
    Points: []ct.SweepPoint{
        {Label: "BTT=200ms", Config: ct.BatchConfig{
            Iterations: 200, SeedStart: 1,
            Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
            Scenarios: ct.Catalog, Protocols: protocols,
        }},
        // ... more points ...
    },
}
result := ct.RunSweep(t, sweep)
reporting.RenderComparison(reporting.Comparison{
    Title: "My experiment", Description: "...",
    Sweeps: []ct.SweepResult{result}, Iterations: 200,
    Wallclock: 0, GeneratedAt: time.Now(),
}, "/tmp/out.html")
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
- **Real-BLS not in default report.** Stub crypto is fine for protocol-shape comparison. For real-BLS performance benchmarking, build with the `real_bls` tag and expect ~30 min for the default sweep matrix.
- **Single-cluster mock — no multi-hop GossipSub.** The framework's `NetworkModel` abstracts to per-pair delay; real GossipSub re-floods via mesh peers (~2-3 hops cluster-wide). For n=4 the mesh is effectively full so single-hop is approximately right; for n=13 with multi-subnet topology the gap widens.

## Files

| File | What it adds |
|---|---|
| [`batch.go`](../protocol/v2/consensustest/batch.go) | `BatchConfig`, `RunBatch`, cell runner |
| [`stats.go`](../protocol/v2/consensustest/stats.go) | `Distribution`, `BatchCell`, `BatchReport` |
| [`sweep.go`](../protocol/v2/consensustest/sweep.go) | `Sweep`, `RunSweep`, `DefaultSweeps` |
| [`scenario.go`](../protocol/v2/consensustest/scenario.go) | `Scenario` with `Title` (human label) + `DisplayTitle()` |
| [`network.go`](../protocol/v2/consensustest/network.go) | `LogNormalDelay`, `LossyNetwork`, `CorrelatedLinkDelay` |
| [`batch_report_test.go`](../protocol/v2/consensustest/batch_report_test.go) | `TestGenerateBatchReport` driver |
| [`reporting/html.go`](../protocol/v2/consensustest/reporting/html.go) | `RenderComparison`, `Comparison`, `Applicable` |

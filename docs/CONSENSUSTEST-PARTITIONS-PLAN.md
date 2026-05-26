# Consensustest Partitions Plan

Design plan for a new `p2p_partitions` stress-test sweep — a sustained per-connection severance axis. Companion to (and depends on) the mesh transport introduced under the (now-removed) `CONSENSUSTEST-MESH-PLAN.md`.

## Goal

Add a sweep that exercises **sustained connectivity loss**: a chosen fraction of peer-to-peer connections are severed for the entire slot, blocking both eager push and gossip lazy-push on those pairs. Measures deadline-miss rate as a function of cut fraction. Distinct from `p2p_packet_loss` (transient burst loss that recovers within the slot and is healed by gossip backstop).

## Why

`make stresstest` currently sweeps latency (BTT), transient loss (`p2p_packet_loss`), correlated slow links (`p2p_correlated_delays`), and slow operators (`p2p_node_slowness`). No sweep exercises *sustained* link failure.

The gap matters because in production, peer-connection failures (NAT/firewall blips, peer-score evictions, regional routing issues) frequently last longer than a single slot. The current loss model recovers transiently and gossip routes around the rest — so the cluster looks more resilient than it is. A sustained-cut knob measures the regime where:

- Failed links *don't* recover within the slot.
- Gossip backstop must route via *surviving* peers, with fewer alternates as cuts accumulate.
- At high cut fractions, deadline misses emerge.

## Decisions (resolved during design)

### Cut model: degradation curve, not isolation

Independent per-pair Bernoulli cuts at `p ∈ {0, 0.05, 0.10, 0.20}`, sampled per sim from the mesh's salted rng. The output is a deadline-miss-rate vs cut-fraction curve.

This is **not a partition-tolerance / split-brain test**. With per-node connectivity `C`, the probability that all of a node's connections are simultaneously cut is `p^C` — `~1e-7` at `p=0.20, C=10`. Independent cuts essentially never isolate. Correlated isolation / cohort splits would (cut all of a node's links, or sever a region from the rest), but at SSV's small `n` those scenarios largely overlap the existing silent/crashed catalogs (`catalog_silent.go`, `catalog_crashed.go`) and are **deferred**.

### Mesh fidelity: keep eager mesh as-is, bound only the gossip pool

The eager mesh degree of 3 is **load-bearing for latency calibration**: it produces the typical 2-hop diameter (`Pᵢ → Rⱼ → Pₖ`) that the per-hop delay model (`LogNormal{Median: BTT/3, σ: 0.3}`) convolves into the cluster-wide BTT envelope. The original design (`CONSENSUSTEST-MESH-PLAN.md`) explicitly scoped this as a "mesh-as-realism" latency layer, *not* a literal gossipsub topology emulator. Bumping eager mesh to D=8 would collapse the diameter to ~1 hop, break the convolution, and force re-fitting every empirical profile (`prod`, `stage*`, …) — not worth it for a degradation-curve feature.

The degree-3 reason (eager *latency*) is **orthogonal** to the partition concern (gossip *connectivity*). They live on different methods — `Neighbors()` for eager, `NonMeshPeers()` for gossip — so we fix gossip connectivity without touching eager.

### Connectivity fidelity: bound the gossip candidate pool

Real SSV per-subnet connectivity is capped at `TopicMaxPeers = 10` (`network/p2p/config.go:51`), with `D = 8` of those being the eager mesh (`network/topics/params/gossipsub.go:11`). Real per-subnet IHAVE candidates ≈ `TopicMaxPeers − D = 2`.

Today's `NonMeshPeers()` returns the *entire* non-eager set, so IHAVE candidates are 2–3× richer than real. Two consequences:

1. The cluster is effectively un-partitionable (sparse cuts get healed by an over-rich gossip layer).
2. Existing loss sweeps (`p2p_packet_loss` especially) likely *understate* miss rates because gossip recovery is too strong.

Fix: introduce a per-node bounded gossip-connection set with expected size `≈ TopicMaxPeers − eagerDegree = 10 − 3 = 7`. A node's total per-sim connectivity ≈ 10, matching real SSV's per-subnet cap. The 3/7 eager/gossip split differs from real gossipsub's 8/2, but matching total connectivity is the right fidelity target — the eager mesh in this model is a *latency abstraction*, not a literal mesh count.

This is also the *foundation* of the partition knob.

### Pair universe: all mesh-node pairs

Severance covers protocol-protocol, protocol-relay, and relay-relay pairs uniformly. Undirected (link down = down both ways). Matches the existing `LossyNetwork` / `CorrelatedLinkDelay` per-pair conventions.

### Mesh-mode only

Direct-fanout has no routing fabric; severing a direct (from,to) pair collapses into "drop those messages", degenerate vs `LossyNetwork`. Healthy / Baseline already runs `DeliveryMesh`; the new sweep wraps those.

### Iteration budget

Bernoulli cuts make each sim a different topology realization, so one sample is meaningless. The new sweep wraps Healthy (`Group == "Baseline"`) so it inherits `ITERATIONS_BASELINE_OPERATIONS` (default 10000) via the existing `CloneScenarioWith` pattern that `p2p_packet_loss` already relies on. No new iteration class needed.

### Subnet size: keep `r = n`

Don't grow the relay pool. At n=4 (total 8 nodes), the non-eager pool is already 4 — smaller than the bound (7) — so the bound trivially doesn't restrict, and the n=4 cell will show near-flat partition signal. That's an honest reflection of "small subnet" reality. At n=7 (pool 10) and n=13 (pool 22) the bound bites and the degradation curve emerges. Document this behaviour in the sweep description.

Growing the relay pool would also risk lengthening the eager `Pᵢ → Rⱼ → Pₖ` path beyond 2 hops if relays-per-protocol-peer must scale up, which would disturb the calibration. Avoid.

## Architecture

### Layer 1 — Bounded gossip connectivity (foundation)

API extension on `MeshGossipConfig` (`protocol/v2/consensustest/mesh.go`) — placed alongside the other lazy-push tunables for cohesion:

```go
type MeshGossipConfig struct {
    ...
    Dlazy           int
    GossipFactor    float64
    // New: cap the per-node IHAVE candidate set. 0 → WithDefaults
    // fills in 7 (= TopicMaxPeers − typical eager degree). Pass an
    // explicit large value to opt out.
    GossipPoolBound int
}
```

A new unexported `meshLinkKey {a, b MeshNode}` (sorted-pair constructor `newMeshLinkKey`) is introduced for keying — `network.go`'s `linkKey` is per-`OperatorID` and shared with direct-mode `NetworkModel` wrappers, but the mesh layer needs per-`MeshNode` keys because relays have synthetic endpoints that wouldn't survive the OperatorID-based keying cleanly.

Construction in `NewMeshTopology`, after eager wiring + the existing `isConnected` check:

```go
if m.gossip.GossipPoolBound > 0 {
    const typicalEager = 3
    poolPerNode := total - 1 - typicalEager
    if poolPerNode > m.gossip.GossipPoolBound {
        pg := float64(m.gossip.GossipPoolBound) / float64(poolPerNode)
        m.gossipConn = make(map[meshLinkKey]struct{})
        // Deterministic upper-triangular walk; each non-eager pair
        // rolled once against the mesh's salted rng.
        //   for a := 0..total-1: for b := a+1..total-1:
        //     skip if eager; else if rng.Float64() < pg, include.
    }
}
```

`NonMeshPeers(node)` consults `m.gossipConn` (when non-nil) as an additional filter on top of the existing self + eager-neighbour exclusions.

When `poolPerNode ≤ GossipPoolBound` (small subnet, e.g. n=4 → pool=4), the inner construction is **skipped entirely**: `m.gossipConn` stays `nil` and `NonMeshPeers` falls through to its pre-bound behaviour. Same outcome as "p_g would clip to 1.0," cleaner mechanics.

### Layer 2 — Sever knob (the feature)

Further extension on `MeshConfig` (top-level — severance affects both transports, not gossip-only):

```go
type MeshConfig struct {
    ...
    Gossip    MeshGossipConfig
    // SeverProb: per-pair Bernoulli severance probability, sampled once
    // per sim from the mesh rng. 0 = no severing (default). See the
    // field's full docstring in mesh.go for the model rationale.
    SeverProb float64
}
```

Construction in `NewMeshTopology`, after Layer 1:

```go
if cfg.SeverProb > 0 {
    m.severed = make(map[meshLinkKey]struct{})
    // Deterministic upper-triangular walk over DELIVERY-PATH pairs
    // only (eager edges ∪ gossip-reachable pairs). Iterating only
    // pairs that actually carry messages keeps SeverProb's meaning
    // user-facing: "X% of connections are down," not "X% of all
    // node-pairs (most of which were never connected anyway)."
    //
    // Gossip-reachable iff (gossipConn ≠ nil and pair in gossipConn)
    //                  OR  (gossipConn == nil and pair is non-eager).
    // Roll rng.Float64() < SeverProb; record into m.severed.
}
```

Filters at access time:

- `Neighbors(node)` — filter out neighbours where `severed[newMeshLinkKey(node, nbr)]` is set. Fast-path: when `m.severed == nil`, returns the aliased internal slice unchanged (no allocation, no overhead on the partition-disabled path).
- `NonMeshPeers(node)` — filter out where `severed[newMeshLinkKey(node, peer)]` is set, in addition to the existing `gossipConn` filter from Layer 1.

Both gates ride on the same `severed` set, so:

- **Eager push & reflood** (all five adapters, via `Neighbors()`): severed pair → no delivery.
- **Lazy IHAVE** (via `NonMeshPeers()` → `PickGossipRecipients()`): severed pair → no IHAVE → no IWANT → no IWANT-response reinject via that pair.

All delivery paths covered; **no adapter changes needed**.

### `isConnected` interaction

The build-time check (`mesh.go:isConnected`) inspects the eager-mesh graph (`m.neighbors`). We don't modify eager wiring at build — severing is an *access-time filter* — so the eager graph remains connected and the panic never fires. The gossip-connection graph and the severed-pair set are independent overlays; we don't require either to be connected (partitions in those are an intended outcome).

### Configuration & sweep wiring

`MeshGossipConfig.WithDefaults` (or the `MeshConfig`-level defaults helper) sets `GossipPoolBound = 7` so all mesh-mode scenarios inherit bounded gossip by default. `SeverProb` defaults to 0; only the new sweep sets it.

New sweep (in `protocol/v2/consensustest/sweep.go`):

```go
func p2pPartitionsSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
    fallback, byGroup := iters.asBatchIterations()
    probs := []float64{0, 0.05, 0.10, 0.20}
    pts := make([]SweepPoint, 0, len(probs))
    for _, prob := range probs {
        prob := prob
        scenariosWithSever := cloneScenariosWithMesh(scenarios, prob > 0, func(cfg *MeshConfig) {
            cfg.SeverProb = prob
        })
        btt := 300 * time.Millisecond
        base := withClusterSize(DefaultProposerDutyConfig(btt), n)
        base.K = k
        base.Network = productionLogNormal(btt)
        pts = append(pts, SweepPoint{
            Label: fmt.Sprintf("n=%d K=%d severProb=%.2f", n, k, prob),
            Fields: map[FieldKey]float64{
                FieldN: float64(n), FieldK: float64(k), FieldSeverProb: prob,
            },
            Config: BatchConfig{
                Iterations:        fallback,
                IterationsByGroup: byGroup,
                Base:              base,
                Scenarios:         scenariosWithSever,
                Protocols:         protocols,
            },
        })
    }
    return Sweep{
        Name:        "p2p_partitions",
        Title:       "Sustained link severance",
        Params: []string{
            "Bernoulli per undirected pair",
            "GossipPoolBound=7 (TopicMaxPeers−eager)",
            "direct: LogNormal{Median: BTT/2, σ: 0.5}",
            "mesh per-hop: LogNormal{Median: BTT/3, σ: 0.3}",
        },
        Description: "Per-pair sustained severance over a production-shaped baseline with bounded gossip connectivity (TopicMaxPeers=10 calibrated). Cuts persist whole slot — gossip recovery uses surviving links only. At n=4 the non-eager pool is below the bound so cells stay near-flat; n=7 and n=13 carry the degradation signal.",
        AxisLabel: "Sever probability",
        Points:    pts,
    }
}
```

New helper `cloneScenariosWithMesh` parallels `wrapScenariosNetwork` but mutates `cfg.Mesh` instead of `cfg.Network`.

New FieldKey alongside the existing block in `sweep.go`:

```go
FieldSeverProb FieldKey = "SeverProb"
```

Register in `DefaultSweeps`. Document in `Makefile` near the existing sweep list. Add a report cell in `stresstest-report/app.js`.

## Implementation order

Two commits. Layers 1 and 2 ship together as a single foundation: Layer 2 reuses Layer 1's `gossipConn` to define its delivery graph, neither has a user-facing entrypoint on its own, and the bias-measurement test (A/B over bounded vs unbounded `p2p_packet_loss` cells) belongs alongside the bound that motivates it.

### Commit 1 — Bounded gossip pool + sustained-cut severance (foundation)

Layer 1 — `MeshGossipConfig.GossipPoolBound`:

- Field + docstring + `WithDefaults` filling 0 → 7 (`TopicMaxPeers − typical eager`).
- `meshLinkKey` type + sorted-pair constructor.
- `m.gossipConn` built in `NewMeshTopology` after the `isConnected` check; deterministic upper-triangular walk, Bernoulli at `p_g = GossipPoolBound / poolPerNode`.
- `NonMeshPeers` filters by `gossipConn`.
- Skip the construction (gossipConn stays nil) when `poolPerNode ≤ GossipPoolBound` — small-subnet fall-through.

Layer 2 — `MeshConfig.SeverProb`:

- Field + docstring + `m.severed` populated after Layer 1.
- Same deterministic walk, but over delivery-path pairs (eager ∪ gossip-reachable) only.
- `Neighbors` filters by `severed` (fast-path alias when `m.severed == nil`).
- `NonMeshPeers` filters by `severed` alongside the existing `gossipConn` filter.

Tests (in `mesh_test.go`, 10 new tests + 2 helpers):

- Bound: restricts-at-n7 (mean degree ≈ 7, ≥4/7 protocol peers strictly restricted), symmetric, deterministic, inactive at n=4 (small subnet), explicit-unbounded path preserves legacy behaviour.
- Severance: surviving fraction ≈ 1 − p over 100 trials at p=0.30 (within ±3%), symmetric, deterministic, filter fires on BOTH eager and gossip layers, `SeverProb=0` behaves identically to the pre-Layer-2 baseline.

Bias-measurement A/B (in `mesh_sweep_test.go`):

- One test, three subtests covering the `p2p_packet_loss` sweep's actual operating range plus one stressed point: `(n=7, loss=0.20)`, `(n=7, loss=0.30)`, `(n=13, loss=0.20)`. 300 iters per side, Healthy + production mesh+gossip stack, OBFT-700 protocol.
- Logs unbounded-vs-bounded decided counts + bias delta. Asserts a soft directional check (`bounded − unbounded ≤ iters/20`) to guard against pathological flips while tolerating low-stress equality.

Measured outcome (recorded in the commit message): bias is essentially zero at the sweep's `LossRate=0.20` ceiling (0% at n=7 and n=13), 0.3% at the past-sweep stress point (LossRate=0.30, n=7). The existing committed `data.js` is unaffected within statistical noise. The bound is therefore justified entirely on its foundational role for the partition sweep, not on correcting past results.

### Commit 2 — Sweep, field, report

- `p2pPartitionsSweep` in `sweep.go`; register in `DefaultSweeps`.
- `FieldSeverProb` key.
- Helper `cloneScenariosWithMesh`.
- `Makefile` sweep-list update + the sweep-name list near the top.
- `stresstest-report/app.js` axis rendering.
- Smoke test: `make stresstest PROTOCOLS=OBFT-700,QBFT-700,PSigs CLUSTER_SIZES_N=7 ITERATIONS_BASELINE_OPERATIONS=500` and verify monotone curve on the `p2p_partitions` cells.

## Validation

### Foundation (Commit 1 — landed)

- **Unit**: 10 new `TestMesh_*` tests cover bound and severance — rate, symmetry, determinism, filter coverage on both layers, and the small-subnet inactive path. All pass; full `./protocol/v2/consensustest/...` suite green.
- **Cross-sweep bias** (the `p2p_packet_loss` re-run): `TestMeshHealthy_GossipPoolBoundShiftsLossRecovery` runs the bounded vs unbounded A/B over `(n=7, loss=0.20)`, `(n=7, loss=0.30)`, `(n=13, loss=0.20)` at 300 iters each. Bias is **0% at the sweep ceiling** and 0.3% at the stressed point — the existing `p2p_baseline` and `p2p_packet_loss` numbers are unaffected within statistical noise. The bound is justified entirely by its foundational role for the partition sweep, not by correcting past results.

### Sweep (Commit 2 — to come)

- `p2p_partitions` monotone in `SeverProb`: miss-rate(0) ≤ miss-rate(0.05) ≤ … ≤ miss-rate(0.20), within statistical noise.
- n=4 cell near-flat (bound inactive at small subnet) — confirm this rather than a bug.
- n=7 and n=13 cells show monotone degradation.

## Out of scope (deferred)

- **Correlated isolation / split-brain** — overlaps existing silent/crashed catalogs at SSV's small `n`. Candidate v2 if real-world partition incidents motivate it.
- **Eager-mesh fidelity rebuild** (D=8 + recalibrate) — breaks the calibrated 2-hop convolution.
- **Mid-slot heal / time-varying cuts** — candidate v2 axis if static cuts under-represent realism.
- **Direct-mode partitions** — degenerate without a routing fabric.
- **`PartitionedNetwork` cleanup** (`protocol/v2/consensustest/network.go`) — unused primitive; leave in place. Remove in a separate cleanup commit if it stays unused after this lands.

## Answered questions

- ✅ Does `p2p_packet_loss` already cover this? **No** — transient burst loss + over-rich gossip backstop make it forgiving; sustained cuts test a distinct regime.
- ✅ Cut model? **Independent Bernoulli per-pair, sustained whole-slot.**
- ✅ Isolation or degradation? **Degradation curve.** Isolation needs correlated cuts (deferred).
- ✅ Fidelity scope (A1/A2)? **A1, reframed** — keep eager mesh degree-3 (calibration), bound only the gossip pool.
- ✅ Pair universe? **Delivery-path pairs (eager + gossip-reachable), undirected.** Non-delivery pairs carry no messages, so rolling them would dilute the user-facing meaning of `SeverProb`.
- ✅ Percentage semantics? **Bernoulli per pair, `p ∈ {0, 0.05, 0.10, 0.20}`.**
- ✅ Subnet size? **Keep `r = n`.** Document n=4 cell as near-flat.
- ✅ Iteration budget? **Inherits Baseline (10000)** via the existing scenario-cloning pattern.
- ✅ Mesh-mode only? **Yes.**
- ✅ `isConnected` panic? **Sidesteps** — sever is access-time filter; build-time graph stays connected.
- ✅ Where do severance gates live? **Inside `MeshTopology` (`Neighbors` + `NonMeshPeers`)** — no adapter changes.

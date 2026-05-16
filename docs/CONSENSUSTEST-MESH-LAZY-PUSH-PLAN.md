# Stress-test mesh: closing the lazy-push + publisher-exclusion gaps

Plan-doc for closing two specific gaps between the consensustest framework's mesh transport ([protocol/v2/consensustest/mesh.go](../protocol/v2/consensustest/mesh.go) + per-adapter `emitMesh` / `evtMeshArrival`) and the real-world libp2p gossipsub behavior that SSV actually runs on. The mesh-transport is used today only by the `Healthy` scenario ([catalog_baseline.go](../protocol/v2/consensustest/catalog_baseline.go)); adversarial scenarios use direct fanout ([mesh.go §DeliveryMesh comment](../protocol/v2/consensustest/mesh.go)) and are unaffected.

The two gaps being closed:

1. **Lazy-push IHAVE/IWANT backstop** — real libp2p delivers messages over non-mesh edges within ~1 heartbeat via IHAVE/IWANT, so single-edge mesh failures self-heal inside the slot. The sim has no such recovery, so its tail-latency / partial-coverage failure rates on the Healthy path are more pessimistic than reality.
2. **Mesh reflood excludes original publisher** — real `Publish` skips both the immediate relay sender AND `msg.GetFrom()` on each hop. The sim only skips the immediate sender, so messages can loop back through the publisher's mesh and slightly over-count edges.

Out of scope: mesh churn (graft/prune driven by heartbeats + score), IDONTWANT, peer scoring, mesh degree mismatch (SSV uses `D=8`, sim uses fixed degree 3 with relay peers as a steady-state approximation). The sim's degree-3 fixed topology is an intentional approximation of the cluster + immediate libp2p neighborhood; within a 12 s slot the lack of churn is empirically minor.

## SSV's actual libp2p configuration

The plan's defaults match SSV's real overrides from [network/topics/params/gossipsub.go](../network/topics/params/gossipsub.go), not the upstream libp2p defaults. Concretely:

| Parameter | libp2p default | SSV value | Used in sim? |
| --- | --- | --- | --- |
| `HeartbeatInterval` | 1000 ms | **700 ms** | Yes |
| `HistoryLength` (mcache slots) | 5 | **6** (= 4.2 s) | Yes |
| `HistoryGossip` (IHAVE slots) | 3 | **4** (= 2.8 s) | Yes |
| `Dlazy` (min IHAVE recipients per heartbeat) | 6 | 6 (no override) | Yes |
| `GossipFactor` | 0.25 | 0.25 (no override) | Yes |
| `D` (eager mesh degree) | 6 | 8 | No — sim uses fixed degree 3 (deferred under gap #2) |
| `Dlo` / `Dhi` | 5 / 12 | 6 / 12 | No — no mesh churn |
| `MaxIHaveLength` | 5000 | 1500 | No — sim doesn't rate-limit |
| `MaxIHaveMessages` (per peer per heartbeat) | 10 | 32 | No — sim doesn't rate-limit |
| `msgIDCacheTTL` (seen cache) | 120 s | 700 ms × 550 = ~385 s | Implicitly yes (sim's per-node seen map is effectively infinite-TTL within a sub-second-to-12 s sim) |

The msgIDCacheTTL ~6 minute setting in SSV is much longer than the simulator's wall-clock per-iter, so the existing per-node `seen map[MsgID]struct{}` ([mesh.go](../protocol/v2/consensustest/mesh.go)) without TTL is already a faithful model of SSV's seen cache.

`MaxIHaveLength` / `MaxIHaveMessages` are rate limits that matter under bandwidth pressure; the sim's payload counts per slot are well below 1500 mids, so we don't need to model these.

# Phase A — exclude original publisher on reflood (gap #4)

**Status: landed.**

Small, mechanical. Independent of Phase B.

## What changed

Thread the publisher's `MeshNode` through every hop of the reflood walk and skip it in the forward loop. Per-adapter `evtMeshArrival.handle` previously skipped only the immediate sender; now also skips the original publisher, matching go-libp2p-pubsub's `Publish` (which excludes both the relay sender and `msg.GetFrom()`).

Before:

```go
for _, neighbor := range mesh.Neighbors(e.to) {
    if neighbor == e.from {
        continue  // skip immediate sender
    }
    ...
}
```

After:

```go
for _, neighbor := range mesh.Neighbors(e.to) {
    if neighbor == e.from || neighbor == e.publisher {
        continue
    }
    ...
}
```

## What landed

1. **`publisher ct.MeshNode` field** added to each adapter's `evtMeshArrival` struct (lowercase — the type is unexported in each adapter package).
2. **`publisher: fromNode`** set at first-hop emission in each adapter's `emitMesh`.
3. **`publisher: e.publisher`** propagated unchanged on each subsequent reflood hop in `handle`.
4. **Skip condition** `if neighbor == e.from || neighbor == e.publisher { continue }` added to the forward loop.
5. **`describe()` collapsed to a single-line call** to a new shared helper, `ct.FormatMeshArrival(from, to, publisher MeshNode, msgID MsgID, kind MsgKind) string`, in `protocol/v2/consensustest/mesh.go`. Produces the canonical `MeshArrival[from=X to=Y publisher=Z msg=N kind=K]` string used by all four adapters — single-sourcing the format keeps producer and parser aligned.
6. **Shared trace-parsing helper** `ct.ParseMeshArrivalTrace(entry string) (from, to, publisher MeshNode, ok bool)` added next to `FormatMeshArrival`. Used by all four adapter regression tests.
7. **PSigs file split** brought into line with OBFT / 2abOBFT — `emitToAll` / `emitDirect` / `emitMesh` moved from `psigs/events.go` to `psigs/des.go` so the "core sim + emit helpers in `des.go`, event types in `events.go`" convention is uniform across all four adapters.

Edge case: when the publisher is also the immediate sender (the first hop), the publisher-skip is redundant with the sender-skip — harmless.

## Files touched

- [`protocol/v2/consensustest/mesh.go`](../protocol/v2/consensustest/mesh.go) — added `FormatMeshArrival` + `ParseMeshArrivalTrace` helpers and the shared `meshArrivalRE` regex; imports `regexp` + `strconv`.
- [`protocol/v2/consensustest/obft/events.go`](../protocol/v2/consensustest/obft/events.go) — field + skip + propagate; `describe()` delegates to `ct.FormatMeshArrival`.
- [`protocol/v2/consensustest/obft/des.go`](../protocol/v2/consensustest/obft/des.go) — set field at emit.
- [`protocol/v2/consensustest/twoab/events.go`](../protocol/v2/consensustest/twoab/events.go) — same as OBFT.
- [`protocol/v2/consensustest/twoab/des.go`](../protocol/v2/consensustest/twoab/des.go) — set field at emit.
- [`protocol/v2/consensustest/qbft/events.go`](../protocol/v2/consensustest/qbft/events.go) — same (uses `round` instead of `layer`).
- [`protocol/v2/consensustest/qbft/network.go`](../protocol/v2/consensustest/qbft/network.go) — set field at emit.
- [`protocol/v2/consensustest/psigs/events.go`](../protocol/v2/consensustest/psigs/events.go) — field + skip + propagate; `describe()` delegates to `ct.FormatMeshArrival`; emit helpers moved out.
- [`protocol/v2/consensustest/psigs/des.go`](../protocol/v2/consensustest/psigs/des.go) — receives `emitToAll` / `emitDirect` / `emitMesh` (gains `slices` import); matches OBFT / 2abOBFT structure.
- [`protocol/v2/consensustest/{obft,twoab,qbft,psigs}/adapter_test.go`](../protocol/v2/consensustest/obft/adapter_test.go) — new `TestMeshArrival_NoRefloodToPublisher` per adapter, sharing `ct.ParseMeshArrivalTrace`.

## Tests

One regression test per adapter (`TestMeshArrival_NoRefloodToPublisher`): runs healthy mesh with `TraceEnabled=true`, scans `out.Trace`, asserts no `MeshArrival` entry has `to == publisher`. Demonstrably catches the bug — verified by temporarily reverting the publisher-skip in OBFT and watching the test fail with `mesh arrival scheduled with to=publisher (loop-back): "MeshArrival[from=7 to=2 publisher=2 msg=2 kind=LeaderBroadcast]"`.

The OBFT test carries the canonical docstring; twoab / qbft / psigs reference back to it.

## Size

~200 LoC net (≈30 of mechanical struct/handler changes across 8 emit/forward sites, ~40 of helpers in `mesh.go`, ~140 of tests across four adapter files; PSigs file split is move-only). Full consensustest suite remains green.

# Phase B — lazy-push IHAVE/IWANT backstop (gap #1)

**Status: landed.**

Heartbeat-driven gossip layer on top of the existing eager-push mesh. Closes the gap where a mesh-only delivery failure (slow link, partial coverage) had no recovery within the slot.

## Model

Each mesh node (protocol + relay alike — SSV's actual mesh treats relay nodes as topic peers) maintains:

- An **mcache**: per-node sliding window of `(MsgID → reinject closure)`, rotated every heartbeat. The closure is opaque from `mesh.go`'s perspective — it captures the adapter-specific `evtMeshArrival` builder + payload and schedules a fresh arrival when invoked.
- The existing per-node `seen` map as the dedup gate, unchanged.

Every `HeartbeatInterval`, each node:

1. Rotates its mcache (evicts the oldest slot).
2. Collects mids from the last `HistoryGossip` slots.
3. Picks `max(Dlazy, ceil(GossipFactor × |non-mesh peers|))` recipients from `(all_cluster_nodes − mesh_neighbors − self)`.
4. Emits one `evtMeshIHave{from, to, mids}` per recipient via single-hop direct path (`cfg.Network.Delay`), not the mesh hop chain — matches real gossipsub's use of direct TCP connections for control RPCs.

When a node receives `evtMeshIHave`:
- For each advertised mid where `!IsSeen(self, mid)`, accumulate.
- If any unseen, schedule a single `evtMeshIWant` back to the IHAVE sender carrying the unseen list.

When a node receives `evtMeshIWant`:
- For each requested mid still in the mcache, invoke its reinject closure with `requester = e.from`. The closure schedules a fresh `evtMeshArrival` from this node to the requester, preserving the original publisher.
- Mids no longer in mcache are silently dropped (the requester will hear about them via a later heartbeat from another peer).

The reinjected `evtMeshArrival` flows through the existing handler, gated on the requester's `seen` map — a body that arrived via the mesh between IHAVE-out and IWANT-back is correctly deduped.

## Parameters (sim defaults = SSV's gossipsub overrides)

`MeshGossipConfig.WithDefaults()` fills any zero field with SSV's [network/topics/params/gossipsub.go](../network/topics/params/gossipsub.go) values:

| Field | Default | Source |
| --- | --- | --- |
| `Enabled` | `false` | Opt-in; existing mesh tests don't pick up gossip implicitly |
| `HeartbeatInterval` | 700 ms | SSV `HeartbeatInterval` |
| `HistoryLength` | 6 slots (≈ 4.2 s mcache window) | SSV `gsMcacheLen` |
| `HistoryGossip` | 4 slots (≈ 2.8 s IHAVE window) | SSV `gsMcacheGossip` |
| `Dlazy` | 6 | libp2p default (SSV does not override) |
| `GossipFactor` | 0.25 | libp2p default (SSV does not override) |

`MeshTopology` snapshots the config at construction via `cfg.Gossip.WithDefaults()` and exposes it as `mesh.Gossip()` so adapters and event handlers read the same values without recomputing defaults.

## What landed

### Framework (`mesh.go`, `network.go`)

- New `MsgKind` values: `KindGossipIHave`, `KindGossipIWant` (extends the existing `MsgKind` enum + `String()` switch).
- `MeshGossipConfig` struct + `WithDefaults()`; embedded as `MeshConfig.Gossip`.
- `MeshTopology.gossip` retained at construction time.
- Per-node `nodeMcache` (entries map + slot ring buffer + head index); allocated lazily on first `MCacheInsert`.
- Methods: `IsSeen`, `TotalNodes`, `Gossip`, `MCacheInsert`, `MCacheLookup`, `MCacheRotate`, `MCacheGossipMids`, `NonMeshPeers`, `PickGossipRecipients`.
- Constants + helper: `GossipBaseBytes = 32`, `GossipPerMidBytes = 32`, `GossipRPCSize(numMids)`.
- `MCacheReinjectFunc = func(requester MeshNode)` and `MCacheEntry` (kind, bytes, reinject closure).

### Per-adapter (OBFT / 2abOBFT / QBFT / PSigs)

Identical pattern in each:

- Three new event types in `events.go`: `evtMeshHeartbeat`, `evtMeshIHave`, `evtMeshIWant`, each with `describe()` + `handle()`. They are per-adapter because they need to implement the adapter's `event` interface; the bodies are nearly identical across the four.
- `cacheArrivalForGossip` method on `*sim` (in `des.go` for OBFT/2abOBFT/PSigs, in `network.go` for QBFT): inserts an MCacheEntry whose reinject closure builds a fresh `evtMeshArrival` typed to that adapter's builder signature. No-op when gossip is disabled.
- `scheduleInitialHeartbeats` method on `*sim`: pre-schedules a finite sequence of heartbeats per mesh node over `[0, RelayCutoff]`, staggered by `node × HeartbeatInterval / TotalNodes`. **Pre-scheduling rather than self-rescheduling inside the handler** is what guarantees the event queue is finite-by-construction.
- `cacheArrivalForGossip` called from two sites: `emitMesh` (publisher inserts after self-MarkSeen) and `evtMeshArrival.handle` (receiver inserts after MarkSeen succeeds). Both stash a reinject closure preserving the original publisher.
- `RelayCutoff time.Duration` added to each adapter's `desConfig`, plumbed from `cfg.RelayCutoff` at adapter Run time.
- `scheduleInitialHeartbeats()` called at the end of each adapter's `sim.start()`.

### Single-hop control delivery

IHAVE / IWANT delays use `cfg.Network.Delay(rng, fromEP, toEP, KindGossipI...)`, not `cfg.Mesh.HopDelay`. Matches real gossipsub: control RPCs ride direct peer connections, not multi-hop through the mesh.

### Bandwidth accounting

Reuses the existing `RecordMeshHop` dispatch (Emission / EmissionToRelay / EmissionFromRelay / EmissionRelayToRelay) with the new `KindGossipIHave` / `KindGossipIWant` kinds. No new tracker methods or distribution buckets needed.

Body re-send on IWANT goes through `RecordMeshHop` with the original message's kind. Per-slot total bytes shift upward by the IHAVE / IWANT chatter when gossip is enabled; relative protocol comparisons are unaffected.

### Determinism

Recipient selection uses the sim's existing RNG via `rng.Shuffle`. The event queue's `(when, seq)` tie-break ordering keeps per-iter trace byte-identical across runs at the same seed.

### Byz behavior

Adversarial scenarios use `DeliveryDirect` and never enter the mesh path, so byz primitives never reach IHAVE / IWANT under the current scenario catalog. The gossip layer is honest-only by design (no `AllowDelivery` / `OverrideDelay` checks on the gossip events). A future byz-on-mesh scenario would need to add byz hooks; flag as out-of-scope for now.

### Relays

Relays participate identically to protocol peers: tick heartbeats, advertise IHAVE, answer IWANT. They have no protocol-delivery side-effects (`mesh.IsProtocol` gate in `evtMeshArrival.handle` is unchanged) but they do fully participate in gossip, matching real libp2p.

## Tests

Framework-level unit tests in [`mesh_gossip_test.go`](../protocol/v2/consensustest/mesh_gossip_test.go):

- `TestMeshGossipConfig_WithDefaults` — defaults match SSV values; partial overrides are preserved.
- `TestMeshGossip_MCacheLifecycle` — insert → lookup, idempotence in msgID, rotate, eviction after HistoryLength rotations.
- `TestMeshGossip_GossipMids_Window` — `MCacheGossipMids(window)` returns the union of the last `window` slots' mids; oversized window is clamped to HistoryLength.
- `TestMeshGossip_NonMeshPeers` — pool excludes self + mesh neighbors at every node.
- `TestMeshGossip_PickRecipients_DeterministicAndCapped` — same RNG seed → same selection; Dlazy > pool caps at pool size.

Integration tests in [`obft/adapter_test.go`](../protocol/v2/consensustest/obft/adapter_test.go):

- `TestMeshGossip_SmokeOBFT` — gossip enabled on healthy mesh, decision lands, trace contains both `MeshHeartbeat` and `MeshIHave` entries (46 heartbeats, 136 IHAVE events in the captured run).
- `TestMeshGossip_SlowMeshRescue_OBFT` — proves the value-prop: `HopDelay = 5s` (mesh broken past the 4s deadline), `Network = 50ms` direct, `HeartbeatInterval = 100ms`. Without gossip the cluster misses; with gossip enabled the cluster decides at 3.59 s — recovered entirely through IHAVE / IWANT on the fast direct path.

(Plan-list items "dedup-on-reinject" and "explicit relay-participation" are structurally guaranteed by the implementation — `MarkSeen` gates the reinjected arrival, and relays use the same code path as protocol nodes — and didn't need bespoke regression tests on top of the integration coverage.)

Existing-test regressions: none. Full `consensustest/...` suite stays green.

## Files touched

- [`mesh.go`](../protocol/v2/consensustest/mesh.go) — `MeshGossipConfig`, `MCacheEntry`, `nodeMcache`, mcache + gossip methods, `KindGossipIHave/IWant` MsgKinds, `GossipRPCSize`.
- [`network.go`](../protocol/v2/consensustest/network.go) — extended `MsgKind` enum + `String()`.
- [`obft/events.go`](../protocol/v2/consensustest/obft/events.go), [`twoab/events.go`](../protocol/v2/consensustest/twoab/events.go), [`qbft/events.go`](../protocol/v2/consensustest/qbft/events.go), [`psigs/events.go`](../protocol/v2/consensustest/psigs/events.go) — three new event types per adapter; `evtMeshArrival.handle` calls `cacheArrivalForGossip` after MarkSeen succeeds.
- [`obft/des.go`](../protocol/v2/consensustest/obft/des.go), [`twoab/des.go`](../protocol/v2/consensustest/twoab/des.go), [`psigs/des.go`](../protocol/v2/consensustest/psigs/des.go) — `cacheArrivalForGossip`, `scheduleInitialHeartbeats`, `emitMesh` mcache insert, `start()` heartbeat schedule.
- [`qbft/network.go`](../protocol/v2/consensustest/qbft/network.go) — same as above (QBFT keeps its emit-side helpers there rather than in des.go).
- [`obft/adapter.go`](../protocol/v2/consensustest/obft/adapter.go), [`twoab/adapter.go`](../protocol/v2/consensustest/twoab/adapter.go), [`qbft/adapter.go`](../protocol/v2/consensustest/qbft/adapter.go), [`psigs/adapter.go`](../protocol/v2/consensustest/psigs/adapter.go) — `RelayCutoff` field on `desConfig` + propagation at adapter Run.
- [`mesh_gossip_test.go`](../protocol/v2/consensustest/mesh_gossip_test.go) — new framework-level tests (created).

## Size

≈ 950 LoC net: ~200 in `mesh.go` (state + 9 methods + 2 consts + helpers), ~150 per adapter × 4 ≈ 600 (events + helpers + adapter-level `RelayCutoff` plumbing), ~150 in tests.

# Suggested execution order

1. **Phase A** — publisher exclusion. Self-contained; lands first to verify the per-adapter struct/handler-touch pattern.
2. **Phase B step 1** — mcache + heartbeat infrastructure in `mesh.go`, no adapter changes. Compiles, doesn't run yet (no `emitMesh` callers stashing closures), tests only the in-memory structures.
3. **Phase B step 2** — wire OBFT end-to-end, write `mesh_gossip_test.go`. Validates the whole chain on one adapter.
4. **Phase B step 3** — replicate to twoab, qbft, psigs. Mechanical once OBFT is solid.
5. **Verify** — full `consensustest/...` suite green. Spot-check the Healthy CDF in a small `make stresstest` run with `ITERATIONS_BASELINE_OPERATIONS=200` to confirm the expected tail pull-in before committing to the full 46-minute regen.

# Out of scope (gaps explicitly NOT closed)

- **Mesh churn (graft/prune)** — SSV uses `D=8` with heartbeat-driven mesh adjustments. The sim's degree-3 fixed topology is a steady-state approximation. Within a 12 s slot real-world churn is empirically minor; cross-slot effects aren't modeled by sim anyway. Document the assumption.
- **IDONTWANT** — only matters for messages ≥1024 B; SSV consensus payloads are smaller. Skip.
- **Peer scoring** — no misbehavior consequences are modeled. Adversarial scenarios use `DeliveryDirect` so this gap doesn't bite today.
- **Async validation gating** (`PreValidation`, `markSeen` post-validate) — sim's `ValidateDelay` is a fixed per-hop number, sub-resolution at BTT granularity.
- **MaxIHaveLength / MaxIHaveMessages rate limits** — payload counts per slot stay well below SSV's overridden 1500/32 limits.
- **Per-topic mesh / floodsub fallback / fanout** — sim has one logical topic; not relevant.

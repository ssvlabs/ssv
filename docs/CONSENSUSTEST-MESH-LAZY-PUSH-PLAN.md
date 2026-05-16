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

The substantive change. Adds a heartbeat-driven gossip layer on top of the existing eager-push mesh.

## Model summary

Each node (protocol + relay, both participate — SSV's actual mesh treats them as topic peers) maintains:

- an **mcache**: a bounded sliding window of `(MsgID → rebuildArrival)`, rotated every heartbeat. The rebuild closure is the opaque thing that, when invoked, schedules a fresh `evtMeshArrival` on the recipient. This keeps `mesh.go` adapter-agnostic — the adapter's `emitMesh` is the only thing that knows how to construct an arrival, and it stashes a closure into the mcache at emit time.
- the existing per-node `seen map[MsgID]struct{}` ([mesh.go](../protocol/v2/consensustest/mesh.go)) as the dedup gate (unchanged).

Every `HeartbeatInterval`, each node:

1. Picks `max(Dlazy, GossipFactor × |non-mesh topic peers|)` random recipients from `(all_cluster_nodes − mesh_neighbors − self)`.
2. Sends each one an `evtMeshIHave{from, to, mids}` over a single-hop direct path (using `cfg.Network.SampleBTT(from, to)`, NOT the mesh hop chain — IHAVE/IWANT rides direct connections in real libp2p too).
3. The `mids` list is the union of mcache slot ids for the last `HistoryGossip` slots.

When a node receives `evtMeshIHave`:

1. For each advertised mid: if `seen[mid]` is false, accumulate.
2. If any unseen mids: schedule `evtMeshIWant{from: self, to: from, mids: unseen}` back to the sender (one-hop direct, same RTT path).

When a node receives `evtMeshIWant`:

1. For each requested mid: if in mcache (i.e. within the last `HistoryLength` heartbeats), invoke the stashed rebuild closure to schedule a fresh `evtMeshArrival` back to the requester.
2. If not in mcache: drop silently (the requester will hear IHAVE from another peer next heartbeat).

The reinjected `evtMeshArrival` flows through the existing handler, which gates on the requester's `seen` map (so a body that arrived via the mesh in the meantime is still deduped correctly).

## Parameters (sim defaults match SSV)

These belong on `SimConfig` (or a `MeshGossipConfig` sub-struct) with the SSV-real defaults:

| Param | Default | Source |
| --- | --- | --- |
| `HeartbeatInterval` | 700 ms | [network/topics/params/gossipsub.go:30](../network/topics/params/gossipsub.go:30) |
| `HistoryLength` | 6 heartbeats (4.2 s) | [gossipsub.go:25 `gsMcacheLen`](../network/topics/params/gossipsub.go:25) |
| `HistoryGossip` | 4 heartbeats (2.8 s) | [gossipsub.go:27 `gsMcacheGossip`](../network/topics/params/gossipsub.go:27) |
| `Dlazy` | 6 | libp2p default; SSV does not override |
| `GossipFactor` | 0.25 | libp2p default; SSV does not override |

## State + event additions in `mesh.go`

The mcache and heartbeat scheduling are protocol-agnostic and live in `mesh.go`:

```go
// per-node sliding-window of mids-with-rebuild
type mcacheEntry struct {
    insertedAtSlot int                  // heartbeat slot index when added
    rebuild        func(to MeshNode) scheduledEvent  // re-injects evtMeshArrival on `to`
}

type nodeMcache struct {
    entries map[MsgID]mcacheEntry      // active entries
    slots   [][]MsgID                  // ring buffer: most recent HistoryLength slots
    head    int                        // current slot index modulo HistoryLength
}

// On (*MeshTopology) when constructed:
//   - allocate per-node mcache
//   - schedule first evtMeshHeartbeat per node at SlotStart + node-phase-offset
```

Three new event types (protocol-agnostic, defined in `mesh.go` and dispatched by a single shared handler):

| Event | Payload | Behavior |
| --- | --- | --- |
| `evtMeshHeartbeat` | `{node MeshNode, slot int}` | Rotate mcache (advance ring head); collect last `HistoryGossip` slots' mids; pick `max(Dlazy, GossipFactor × non_mesh_pool)` recipients; emit one `evtMeshIHave` per recipient; reschedule self for `now + HeartbeatInterval` |
| `evtMeshIHave` | `{from, to MeshNode, mids []MsgID}` | For each mid where `!seen[to][mid]`, accumulate; emit one `evtMeshIWant` back with the accumulated list |
| `evtMeshIWant` | `{from, to MeshNode, mids []MsgID}` | For each mid in `to`'s mcache, invoke the `rebuild` closure to schedule a fresh `evtMeshArrival` on `from` |

Single-hop delivery: scheduled with `cfg.Network.SampleBTT(from, to)`, mirroring `DeliveryDirect` semantics — IHAVE/IWANT does not multi-hop through the mesh.

## Adapter wiring

Each adapter's `emitMesh` ([obft/des.go:301](../protocol/v2/consensustest/obft/des.go), [twoab/des.go:292](../protocol/v2/consensustest/twoab/des.go), [qbft/network.go:25](../protocol/v2/consensustest/qbft/network.go), [psigs/adapter.go](../protocol/v2/consensustest/psigs/adapter.go)) gains one line: stash a `rebuild` closure into `mesh.MCacheInsert(from, msgID, rebuildArrival)` after marking self seen. The closure is a method value capturing the arrival's builder (the same `e.builder(recipientOp)` pattern that already exists in `evtMeshArrival.handle`).

Nothing else in the adapters changes — `evtMeshArrival` continues to handle arrivals identically whether the source was first-hop eager push or a re-injection from IWANT.

## Determinism

Heartbeat-driven recipient selection RNG: per-call seed = `hash(cfg.Seed, node, heartbeatIdx)`. This matches the seed-salting pattern already in `NewMeshTopology` (`seed ^ 0x6d6573682d76310a`, [mesh.go:123](../protocol/v2/consensustest/mesh.go)). Each call uses its own deterministic source so heartbeats don't interleave RNG state with mesh construction or hop-delay sampling.

Per-node phase offset for the initial heartbeat: `node * HeartbeatInterval / |nodes|`, to avoid lockstep heartbeats hammering the event queue at identical times.

## Bandwidth accounting

The existing `cfg.Bandwidth` tracker has per-kind buckets ([des.go in each adapter](../protocol/v2/consensustest/obft/des.go)). Add two new kinds:

- `Kind = "IHAVE"`: small constant per mid, e.g. 32 B base + 32 B per mid (covers msg-id + small RPC framing).
- `Kind = "IWANT"`: same formula.

Body re-send on IWANT uses the original message's kind (reusing the existing per-kind accounting unchanged).

Per-slot bandwidth distributions in the report will shift upward by a small constant. Absolute byte counts in existing baseline reports may need refreshing; relative protocol comparisons are unaffected.

## Byz behavior

Adversarial scenarios use `DeliveryDirect` and never enter the mesh path ([catalog_baseline.go](../protocol/v2/consensustest/catalog_baseline.go)), so byz primitives never reach IHAVE/IWANT under the current scenario catalog. The plan does NOT add byz hooks for lazy push. If a future scenario wants byz-on-mesh (silent IHAVE, IHAVE spam, IWANT body withholding), that's a follow-up — document the assumption in `mesh.go`'s package docstring.

## Relays

Relays participate identically to protocol peers: tick heartbeats, IHAVE, answer IWANT. They have no protocol delivery (`mesh.IsProtocol(e.to)` gate in `evtMeshArrival.handle` is already correct), but they fully participate in gossip. This matches real libp2p, where all topic subscribers gossip regardless of whether they hold protocol state.

## Tests

A new `protocol/v2/consensustest/mesh_gossip_test.go` covering:

1. **Smoke**: 4-op cluster, mesh enabled, large BTT on one specific edge such that one operator can't receive a Phase 1 message via mesh in time. Without gossip, decision misses; with gossip, IHAVE → IWANT delivers within ~`HeartbeatInterval + 2 × BTT`, decision lands. Assert decision-time delta.
2. **Determinism**: same seed → same IHAVE recipients across runs.
3. **mcache eviction**: a message that arrived `HistoryLength + 1` heartbeats ago is not advertised in IHAVE and not answerable from IWANT.
4. **Dedup-on-reinject**: a message already received via mesh between IHAVE-out and IWANT-back is still deduped (recipient's seen map is consulted on the reinjected arrival).
5. **Relay participation**: a message that reaches only a relay still gets IHAVE'd to its non-mesh neighbors; a protocol peer can receive it via IHAVE→IWANT through the relay.

Existing regressions to expect:
- `TestPhase2_AllSweepPoints_NoSetupErrors` ([sweep_batch_test.go](../protocol/v2/consensustest/sweep_batch_test.go)): more events per sim, should still pass (no envelope changes).
- Baseline Healthy CDFs in the stress-test report: tail should pull in (less pessimism). The 46-minute regen is necessary to update `stresstest-report/data.js`.

## Files touched

| File | Change |
| --- | --- |
| [`mesh.go`](../protocol/v2/consensustest/mesh.go) | mcache state, 3 event types + handlers, heartbeat scheduling, recipient-selection RNG, package docstring |
| [`protocol.go`](../protocol/v2/consensustest/protocol.go) | `MeshGossipConfig` on `SimConfig`, defaults |
| [`stats.go`](../protocol/v2/consensustest/stats.go) | new bandwidth kinds in distributions |
| [`obft/des.go`](../protocol/v2/consensustest/obft/des.go) | `emitMesh` stashes rebuild closure |
| [`obft/events.go`](../protocol/v2/consensustest/obft/events.go) | rebuild closure helper |
| [`twoab/des.go`](../protocol/v2/consensustest/twoab/des.go) | same |
| [`twoab/events.go`](../protocol/v2/consensustest/twoab/events.go) | same |
| [`qbft/network.go`](../protocol/v2/consensustest/qbft/network.go) | same |
| [`qbft/events.go`](../protocol/v2/consensustest/qbft/events.go) | same |
| [`psigs/adapter.go`](../protocol/v2/consensustest/psigs/adapter.go) | same |
| [`psigs/events.go`](../protocol/v2/consensustest/psigs/events.go) | same |
| [`mesh_gossip_test.go`](../protocol/v2/consensustest/mesh_gossip_test.go) | new test file |

## Size

~900 LoC total: 200 in `mesh.go`, ~100/adapter × 4 = 400, ~200 in tests, ~80 in docstrings + comments. 1.5-2 days of careful work.

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

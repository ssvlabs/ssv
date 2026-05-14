# consensustest mesh transport + QBFT post-consensus plan

Plan-doc for two coupled changes to the `consensustest` framework:

1. **Mesh transport** — a per-sim libp2p-pubsub-flavored mesh with dedup + reflood, opt-in per scenario via a `Delivery` flag. Healthy / Baseline-group scenarios opt in; every adversarial scenario stays on the current direct-fanout path.
2. **QBFT post-consensus events** — replace the fixed `PhaseBudget` accounting in [`recordDecided`](../protocol/v2/consensustest/qbft/events.go) with real `KindPostConsensus` broadcast/arrival events through the same transport. QBFT's `Outcome.DecisionTime` semantic shifts from "consensus decided" to "ready to submit", making the comparison with OBFT-family apples-to-apples at the relay boundary.

Both changes converge on the same transport surface, so they're plannable together; sequencing matters (mesh first; otherwise the post-consensus fanout has to be re-wired when mesh lands).

## Why this matters

Today's framework approximates network propagation as a single-hop direct fanout: each `emit()` schedules one arrival per peer using `Network.Delay(...)` (see [OBFT `emitToAll`](../protocol/v2/consensustest/obft/des.go) and [QBFT `virtualNetwork.Broadcast`](../protocol/v2/consensustest/qbft/network.go)). That under-models real SSV mainnet behavior in two ways:

- **Propagation tail shape**: libp2p GossipSub re-floods through D mesh peers per hop, so the cluster-wide P99 is the convolution of multiple per-hop draws, not a single per-pair draw. At n=4 the cluster is too small to see this in a cluster-internal-only mesh, so the model has to include non-cluster relay peers from the same subnet.
- **QBFT post-consensus realism**: today QBFT's `DecisionTime` is "consensus decided"; SSV production then runs a separate ~PhaseBudget-long partial-sig aggregation step before the slot can be submitted. The framework currently swallows that as a fixed budget, so QBFT looks faster than it really is on the submission timeline. OBFT-family `DecisionTime` already means "local σ-cert in hand = ready to submit" (cert relay is informational, not on the critical path) — so the comparison is currently apples-to-oranges by exactly the post-consensus phase length.

## Goals

After this work:

- Healthy baseline runs propagate through a small-world-ish mesh of cluster ops + relay peers, with dedup + reflood per hop. Cluster-wide propagation distribution (mesh-as-realism layer) calibrates to roughly the same per-sim BTT envelope as direct mode.
- QBFT proposer-duty `Outcome.DecisionTime` = earliest cluster op to (a) decide locally **and** (b) receive 2f+1 partial sigs on its decided value. OBFT/2abOBFT semantics unchanged (already ready-to-submit at local decide).
- Adversarial scenarios keep their per-(from, to) primitives (`Byz.AllowDelivery`, `OverrideDelay`, selective broadcast patterns) intact via the unchanged direct-fanout path.

## Non-goals (deferred)

- Full GossipSub fidelity: IHAVE/IWANT, peer scoring, mesh pruning, heartbeat
- Multi-subnet / per-message-kind mesh topologies
- Per-validator-owner aggregation (we model cluster-earliest 2f+1, mirroring the existing earliest-decider rule used everywhere in the framework)
- Strict byte-for-byte cross-run determinism for mesh-mode runs (best-effort only; direct mode keeps its current determinism guarantee)
- Calibrating BTT or adapter timing budgets to mesh hop count (we keep BTT as the cluster-wide propagation anchor; per-hop delays calibrate down so cluster-wide P99 ≈ current direct-mode behavior — "mesh as realism layer")

## Architecture

### Delivery mode flag

New types on the framework boundary:

```go
// protocol.go
type DeliveryMode int
const (
    DeliveryDirect DeliveryMode = iota // current full-fanout transport (default)
    DeliveryMesh                        // small-world mesh with dedup + reflood
)

type SimConfig struct {
    ...
    Delivery DeliveryMode
    // Mesh is consulted only when Delivery == DeliveryMesh; left
    // zero-valued the sim runs in DeliveryDirect mode regardless.
    Mesh MeshConfig
}
```

`MeshConfig` is intentionally narrow — the user-facing knobs are bottled inside the framework defaults, not exposed to scenario authors. Internal-only:

```go
// mesh.go (new file)
type MeshConfig struct {
    // Wiring (hard-coded defaults at sim build time; user-facing
    // configuration not planned for v1):
    //   ProtocolMeshDegree = 1   (in-cluster mesh neighbors per protocol peer)
    //   RelayMeshDegree    = 2   (relay neighbors per protocol peer)
    //   RelayPoolSize      = N   (one relay-only peer per cluster op)
    //   RelayInternalDegree= 2   (relay-to-relay edges per relay)
    // Per-hop delay model — mesh-as-realism: calibrated so the
    // convolution over the average mesh diameter ≈ today's
    // cluster-wide BTT envelope.
    HopDelay      NetworkModel
    ValidateDelay time.Duration // per-hop dedup+validate cost; default 0
}
```

### Scenario opt-in

```go
// scenario.go
type Scenario struct {
    ...
    Delivery DeliveryMode // default Direct
}
```

Mesh opt-in: **just Healthy** (and its instability-wrapped variants, which inherit from the Baseline group). Every adversarial / propagation / equivocation / host-validity / silent-leader scenario stays Direct. Rationale: those scenarios encode behavior via per-(from, to) primitives that lose meaning under reflood (the wire-suppressed receiver still sees the message via a neighbor's re-broadcast). Keeping them Direct preserves the existing scenario semantics with zero catalog rewrites.

`wrapAllForInstability` propagates the Delivery flag onto the wrapped Healthy scenarios so the Baseline-group level>0 points also run in mesh mode.

### Mesh topology (n=4 baseline)

For n=4, the wiring rule is:

- **4 protocol peers** P₁..P₄, paired as (P₁↔P₂) and (P₃↔P₄). Each Pᵢ has exactly 1 in-cluster mesh neighbor.
- **4 relay peers** R₁..R₄. Each Pᵢ has 2 relay neighbors picked from the pool such that every relay is touched by at least one Pᵢ.
- **Relay-to-relay edges**: 2 per relay, fanning out the relay subgraph enough that the overall mesh is connected (assert this at sim build time; panic if disconnected so silent partitions can't slip past).

Total mesh size = 8 nodes, every node at degree 3.

For odd n (only n=7 in the matrix; deferred to nice-to-have): the unpaired protocol op gets `ProtocolMeshDegree=0`, `RelayMeshDegree=3` — i.e. all 3 mesh neighbors are relays. This is the "no co-cluster neighbor in my mesh today" case that does occur in production (the libp2p mesh is randomly assembled; sometimes you happen not to be wired to any same-cluster op).

For general n: `RelayPoolSize = n`; protocol pairing by op-id (P_{2k−1} ↔ P_{2k}); rest of the wiring filled out with random selections from `cfg.Seed`.

### Wiring construction

Deterministic from `cfg.Seed`. Build sequence:

1. Pair protocol peers by op-id (no RNG needed).
2. For each Pᵢ, sample `RelayMeshDegree` relay neighbors uniformly without replacement; rejection-retry until every relay has ≥1 protocol attachment.
3. Fill relay-to-relay edges by sampling `RelayInternalDegree` neighbors per relay, rejection-retry to avoid self-loops / duplicates.
4. Assert connectivity (BFS from P₁; panic if any node is unreachable).

This is a single-shot construction at sim build, ~tens of μs of work — no hot path.

### Per-hop delay (mesh-as-realism)

The framework's `BTT` stays defined as "cluster-wide P99 propagation" — that's the anchor. Per-hop delay is calibrated so the average mesh diameter integrates to roughly the cluster-wide P99.

Defaults to ship:

```go
// At n=4 with the 4P + 4R mesh, typical hop count between protocol
// peers in different pairs is 2 (Pᵢ → Rⱼ → Pₖ). Per-hop median = BTT/3
// gives total median ≈ 2·BTT/3 — close to today's direct-mode median
// (BTT/2 by LogNormal{Median: BTT/2}). Sigma per hop tighter (0.3)
// because the cluster-wide heavy tail emerges from the convolution.
HopDelay: LogNormalDelay{Median: btt / 3, Sigma: 0.3}
```

Calibration in Phase B: empirical check that Healthy mesh-mode cluster-wide P99 lands within ~10% of direct-mode Healthy P99. Tweak `Median` factor if not. The user explicitly accepted this as "mesh-as-realism", not "BTT-as-per-hop reinterpretation" — adapter timing budgets (Δ_2, T_commit, broadcast schedules) unchanged.

### Mesh transport

Per-peer state (cluster ops and relay peers share this):

```go
type meshPeerState struct {
    seenMsgID map[MsgID]struct{} // dedup
    neighbors []meshNodeID
}
```

Each emitted message carries a `MsgID uint64` — assigned at publish time from a per-sim atomic counter (seeded from `cfg.Seed` so direct-mode determinism stays intact; mesh-mode is best-effort deterministic).

**Publish from cluster op A** (replaces today's `emitToAll`):

1. For each mesh neighbor `N ∈ neighbors[A]`: schedule `evtMeshArrival{from: A, to: N, msg, ts: now + HopDelay.Sample(rng, A, N, kind)}`.
2. Bandwidth accounting: one outbound emission per neighbor (instead of one per `n−1`).

**Arrival at peer M** (`evtMeshArrival.handle`):

1. Record inbound bandwidth (mirroring libp2p, which logs the byte regardless of dedup outcome).
2. If `seen[msg.MsgID]` is set → drop, done.
3. Mark `seen[msg.MsgID]`.
4. If `M` is a protocol peer: schedule the existing `evtPhase1Arrival` / `evtCommitArrival` / etc. handler at `now + ValidateDelay`. Relay peers skip the protocol-handler step.
5. Schedule forwards to `neighbors[M] \ {from}` at `now + ValidateDelay + HopDelay.Sample(...)` per outbound edge.

The per-protocol arrival events (`evtPhase1Arrival`, `evtCommitArrival`, `evtCertArrival`, the new `evtPartialSigArrival`) keep their current handler logic — the mesh layer is a pure transport replacement; once a message lands on a protocol peer's local protocol state, processing is identical.

### Byz primitives in mesh mode

`Byz.AllowDelivery(from, to, kind)` and `Byz.OverrideDelay(...)` continue to work at the **publish step** only — the publisher consults them when emitting to each first-hop neighbor. After that, reflood hops use mesh defaults regardless of the byz pattern.

Document clearly in the mesh.go header: byz patterns are only meaningful under `DeliveryDirect`. Under `DeliveryMesh`, a publish-time suppression to peer X still results in X receiving the message via a re-flooding neighbor with one extra hop of delay. The catalog opts adversarial scenarios into Direct already, so this caveat is informational, not load-bearing.

### Bandwidth accounting

Direct mode: unchanged (one inbound + one outbound per (publisher, receiver) edge per emit).

Mesh mode: each hop counts. The cluster-total grows from `O(n × emit_count × msg_size)` to `O(D × hops × emit_count × msg_size)`. This is production-true (libp2p actually re-floods each message D times per hop) and a useful side-output for capacity analysis. `PerKindBandwidth` works the same; mesh just multiplies the per-emit footprint.

### QBFT post-consensus events

Add to [`qbft/events.go`](../protocol/v2/consensustest/qbft/events.go):

```go
type evtPartialSigBroadcast struct {
    from  OperatorID
    value []byte
    sig   []byte // stub or real BLS depending on cfg.BLSKeys
}

type evtPartialSigArrival struct {
    from, to OperatorID
    value    []byte
    sig      []byte
}
```

**Operator-local state additions** (per-sim, per-op):

```go
type qbftState struct {
    ...
    partials  map[valueHash]map[OperatorID][]byte // value → signer → sig
    readyAt   time.Duration                       // first time |partials[v]| ≥ 2f+1; zero if never
}
```

**Wiring**:

1. When QBFT op `i` decides at `T_i` (existing `recordDecided`): emit `evtPartialSigBroadcast{from: i, value: decidedValue, sig: stubSign(i, decidedValue)}` through the broadcast transport (Direct or Mesh per scenario).
2. `evtPartialSigArrival{from: j, to: i, value, sig}` at op `i`: `state[i].partials[hash(value)][j] = sig`.
3. If `len(state[i].partials[hash(value)]) >= 2*F+1` and `state[i].readyAt == 0`: `state[i].readyAt = now`.
4. End-of-sim aggregation in `outcome()`: `out.DecisionTime = min_i(state[i].readyAt)` over ops where `readyAt > 0`. If no op reaches 2f+1: `out.Decided = false`, miss reason `"no_postconsensus_quorum"`.

The existing fixed `PhaseBudget` accounting in `recordDecided` is removed. Bandwidth for partial sigs gets recorded by the transport naturally — no separate budget needed.

**Honest operators emit their partial sig at their own local decide time** (user's clarification). That gives per-op variance: faster ops emit early, slower ops drag the cluster's earliest-2f+1 timestamp.

**Byz operators**: skip the partial-sig emit (existing byz patterns for QBFT already model byz behavior at the consensus layer; the post-consensus layer just inherits "byz doesn't sign"). At byz-count > f, the cluster has < 2f+1 honest partials and the slot misses cleanly.

### Outcome semantic shift (QBFT only)

| Field | Before | After |
| ----- | ------ | ----- |
| `out.DecisionTime` (QBFT, success) | cluster-earliest QBFT-decide + fixed PhaseBudget | cluster-earliest QBFT-decide **and** 2f+1 partial sigs received locally |
| `out.DecisionTime` (OBFT, 2abOBFT) | cluster-earliest local σ-cert in hand | unchanged |
| `out.Decided` (QBFT) | true if any op decided in-time | true if any op reached 2f+1 partial sigs by `RelayCutoff − HeaderSubmitHeadroom` |
| Miss reasons (QBFT) | `consensus_deadlock`, etc. | + `no_postconsensus_quorum` |

This is the apples-to-apples comparison: both protocols' `DecisionTime` now means "earliest cluster op holds a submittable certificate". OBFT's cert relay step stays informational (not on the critical path).

`ClipLateDecision` works the same way — if the new `DecisionTime` exceeds `RelayCutoff − HeaderSubmitHeadroom`, the outcome clips to miss. The reporting layer's `DecidingBroadcastTime` continues to be set by the OBFT/2abOBFT adapters from their broadcast schedules; QBFT leaves it zero (the slot_start filter uses the QBFT pipeline-shift branch for QBFT cells anyway).

## Phases

### Phase A — Mesh transport, Direct preserved

**Code**:

- `protocol/v2/consensustest/mesh.go` (new, ~400 LoC est.): `DeliveryMode`, `MeshConfig`, topology builder, `evtMeshArrival` + dedup/forward, MsgID counter.
- `protocol/v2/consensustest/protocol.go`: add `SimConfig.Delivery`, `SimConfig.Mesh`. Default `DeliveryDirect`.
- `protocol/v2/consensustest/scenario.go`: add `Scenario.Delivery`. Default `DeliveryDirect`. `wrapAllForInstability` propagates the field.
- Adapter shims (`obft/des.go`, `twoab/des.go`, `qbft/network.go`): the existing `emitToAll`/`Broadcast` paths dispatch via a new shared `Transport` interface implemented by `directTransport` (today's code path) and `meshTransport` (new).
- `protocol/v2/consensustest/transport.go` (new, ~100 LoC est.): `Transport` interface, `directTransport` extracted from the three adapters' inline fanout.

**Tests**:

- `mesh_test.go`: builder produces a connected 4P+4R graph at the wiring constraints; reflood reaches every protocol peer; dedup drops re-arrivals; ValidateDelay measurable.
- `mesh_byz_test.go`: byz `AllowDelivery=false` at publish suppresses first-hop but reflood still reaches receiver one hop later (documenting the semantic).
- Existing direct-mode tests pass byte-identically (smoke + determinism unchanged for the still-Direct catalog).

**Acceptance**: every existing test green; mesh smoke shows P₁ → P₃ propagation goes through at least one relay hop.

### Phase B — Healthy opts into mesh + calibration

**Code**:

- Mark `Healthy` (and Healthy via `wrapAllForInstability`) with `Delivery: DeliveryMesh`.
- Plumb the default `MeshConfig` (HopDelay, ValidateDelay) at `BatchConfig.Base` construction in `DefaultProposerDutyConfig` so every Healthy sim runs through the mesh.

**Calibration**:

- Run the stress matrix (small iteration count) with Healthy in mesh mode at n=4.
- Compare cluster-wide propagation P99 vs the prior direct-mode Healthy baseline.
- Tweak `HopDelay.Median` factor (start at BTT/3) until the cluster-wide P99 lands within ~10% of direct-mode P99.
- Bake the chosen factor into the default and add a brief comment in `mesh.go` documenting the calibration rationale.

**Tests**:

- Mesh-mode Healthy success rate at n=4 baseline parameters matches direct-mode within statistical noise (assert with a wide tolerance, say ±2% over 1000 iters — the calibration target).
- Bandwidth assertion: mesh-mode Healthy `ClusterBandwidth` is order-of-magnitude `degree × hops` larger than direct-mode (no point asserting tight numbers; just sanity).

**Acceptance**: Healthy mesh-mode and direct-mode P99 / success-rate within 10%; mesh-mode bandwidth visibly larger (~3-4×) and consistent across runs.

### Phase C — QBFT post-consensus events

**Code**:

- `qbft/events.go`: add `evtPartialSigBroadcast` and `evtPartialSigArrival`.
- `qbft/des.go`: per-op `partials` + `readyAt` state.
- `qbft/adapter.go`: after `runDES`, aggregate `min_i(readyAt[i])` as `out.DecisionTime`; if no op `readyAt > 0` while at least one op decided locally, set `out.Decided = false` with miss reason `no_postconsensus_quorum`.
- Remove the fixed `PhaseBudget` accounting in `recordDecided` (or convert to a hard floor — see Open Q below).
- New unit test for the `2f+1` arithmetic.

**Tests**:

- `qbft/adapter_test.go`: a healthy QBFT sim emits exactly N partial sigs (one per honest op), each op's `readyAt` lands after 2f+1 arrivals.
- A byz-count = f sim still reaches 2f+1 (the f honest ops not byz-flagged sign); byz-count = f+1 fails post-consensus (new miss reason).
- Late-decide sim where local decide is on-time but 2f+1 arrives past the relay clip: outcome clips to miss.
- Determinism (in direct mode): post-consensus event ordering identical across reruns at the same seed.

**Acceptance**: QBFT smoke tests pass with the new semantics; the new `no_postconsensus_quorum` reason fires for the expected scenarios.

### Phase D — Reporting + docs

**Code**:

- Regenerate `data.js` (the user runs `make stresstest` after Phase C lands).
- The reporting layer needs no schema change — `DecisionTime` carries the new semantic transparently.
- Update [`docs/STRESSTEST-REPORT.md`](STRESSTEST-REPORT.md) (already stale per the prior cleanup): document the new QBFT semantic, the mesh-mode opt-in, and clarify that OBFT cert relay is informational.
- Optional: add a short note in the report's description string (rendered in the UI) flagging the QBFT semantic shift so a reviewer doesn't read the new wall-clock as a regression.

**Acceptance**: regenerated report renders; description text mentions the QBFT semantic; STRESSTEST-REPORT.md no longer stale.

## Risks and open questions

1. **`PhaseBudget` after Phase C — delete entirely, or keep as a hard floor?**
   - Delete: post-consensus timing is purely emergent from partial-sig event flight times. Cleanest.
   - Keep as floor: e.g. "earliest 2f+1 ≥ T_decide + 100ms" to model real aggregation cost beyond pure network (BLS verify, deserialization, etc.). Defensive against undercounting.
   - **Recommend**: delete. The mesh transport already models per-hop variance; an extra floor double-counts. Re-add if the data shows QBFT mesh-mode success times unrealistically fast.

2. **Calibration tolerance**.
   - Target ±10% on Healthy P99 vs direct mode. If we can't get there at any reasonable `HopDelay`, options are (a) widen the mesh slightly (add a couple of cross-pair relay-to-relay edges), (b) loosen the tolerance and accept that mesh-mode tail is heavier by design.

3. **Determinism in mesh mode**.
   - User said "not super important". Best-effort: per-sim `MsgID` counter + per-hop delays sampled in DES queue order. Should reproduce within a single Go runtime; cross-machine reproduction is the standard caveat.
   - If we ever need strict determinism: key the per-hop RNG draw off `hash(publisher_id, sender_id, receiver_id, MsgID)`. Worth ~30 LoC; defer until/unless a reproduction need pops up.

4. **What about 2abOBFT post-consensus?**
   - 2abOBFT's `Output` is already a quorum-backed σ-cert (same shape as OBFT). No separate 2f+1 aggregation step. So no analogous change needed — 2abOBFT stays "ready to submit at local decide".

5. **Mesh opt-in beyond Healthy** (re-confirm).
   - Current plan: only Healthy + instability-wrapped Healthy. Every adversarial scenario stays Direct.
   - If a future request comes for, say, the bandwidth comparison scenarios to run under mesh: easy to flag those scenarios `DeliveryMesh` per-scenario without changing the framework. Phase A's plumbing is per-scenario already.

## Estimated diff

| Phase | Files touched | LoC added | LoC removed |
| ----- | ------------- | --------- | ----------- |
| A     | ~6            | ~600      | ~50         |
| B     | ~3            | ~30       | ~5          |
| C     | ~4            | ~250      | ~30         |
| D     | ~2            | ~80       | ~40         |

Total: ~1k LoC added, ~125 removed. Tests included.

## Sequencing summary

A → B → C → D. Each phase ships standalone (own commit, own PR if helpful) without breaking the others. Phase A is the load-bearing infrastructure; Phases B and C are independent applications of it (mesh opt-in for Healthy; post-consensus events for QBFT) and could even be done in parallel after A lands.

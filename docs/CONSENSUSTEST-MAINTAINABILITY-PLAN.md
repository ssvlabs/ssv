# consensustest Maintainability Refactor — Plan

Target: `protocol/v2/consensustest` (the virtual-time discrete-event test framework for SSV consensus) and its protocol sub-packages `qbft/`, `obft/`, `twoab/`, `psigs/`.

This plan is the harness analog of the now-complete protocol-side OBFT shared-core refactor (`protocol/v2/obft`, Phases 1–6, plan retired in 4e5343cb1). It is a **standalone, do-it-now** effort — the protocol work it was once sequenced behind is finished, and the harness is test-only so there is no production blast radius. Where the prior plan's reasoning still applies (the over-DRY guardrails especially), it is carried over; the specifics are re-derived for the harness.

**Scope decisions (from the planning back-and-forth):**
- Go **as deep as makes sense**, but **do not over-DRY** — over-deduplication is its own antipattern. §3 is the binding ruleset; when in doubt, leave it duplicated.
- The operator-ID-type seam that blocks dedup: technique **recommended in §2.3** (the user asked for a concrete proposal to react to).
- Both axes of duplication are in scope: the cross-all-4 engine/transport (clean), and the obft↔twoab twins (delicate — prototype-first).

---

## 1. Honest assessment (where the pain actually is)

**It is not spaghetti — it is copy-paste, plus a few central-type hot-spots.** The architecture is well-layered: an algorithm-agnostic `Protocol` interface (`Name()` / `Run(cfg) (Outcome, error)`) → per-protocol adapters → a per-protocol discrete-event sim; and at the top, `sweep → batch → stats/bandwidth/safety → reporting`. The comments are unusually thorough. The "tight coupling / entanglement" impression comes mostly from **one thing wearing two hats** — hand-maintained structural duplication across the four protocol sub-packages.

### The duplication, concretely (verified by diff + full read, not guessed)

**Hat 1 — the DES engine + mesh/gossip transport is duplicated 4×.** Every sub-package independently defines: the `eventQueue` + heap methods, `runLoop`, `schedule`, the `sim` struct skeleton, and the entire mesh transport — `evtMeshArrival`, `evtMeshHeartbeat`, `evtMeshIHave`, `evtMeshIWant`, `cacheArrivalForGossip`, `scheduleInitialHeartbeats`.

- The mesh handlers are **line-for-line identical** across packages except a struct field named `round` (qbft) vs `layer` (obft/twoab) — same semantic — and the operator-ID type used in one builder closure.
- `qbft/events.go` carries the comment *"Mirrors the OBFT adapter's gossip events; see …obft/events.go evtMeshHeartbeat for the design rationale."* — duplication maintained by hand, by explicit admission. A fix to the gossip dedup/forward logic today must be applied in four places.
- This is roughly **700–800 lines of pure cross-package duplication** (the engine loop + transport + the byz/crash scaffold below).

**Hat 2 — `obft` and `twoab` harness packages are near-twins.** After normalizing identifiers (`twoab`↔`obftbase`, σ-field names) and stripping comments:

| file | obft↔twoab shared | note |
|---|---|---|
| `des.go` | **~84%** | the sim engine + setup is almost entirely shared |
| `adapter.go` | **~77%** | validate / translate-byz / clip / classify scaffold |
| `events.go` | **~70–81%** | shared OBFT-family Phase-1/Phase-3 event flow; twoab adds a genuine Phase-2a coordination layer (~200 extra normalized lines) |
| `byz.go` | **~70%** | shared byz scaffold + OBFT-family kinds; twoab adds Phase-2a (Rule 6a) byz hooks |

This mirrors exactly the protocol-side `base`↔`twoab` twin that the completed refactor collapsed into a shared `obft/` parent.

**The cross-family packages (`qbft`, `psigs`) share only Hat 1** — the engine + transport scaffold — not the protocol handlers. qbft (round-based consensus) and psigs (no consensus, just a threshold counter) have genuinely different protocol logic that must stay separate.

### The blocker is mechanical, not logical

The only reason the identical code can't be shared today: `sim`, `event`, and `scheduledEvent` are **package-local types**, and each package uses a **different operator-ID type** (`spectypes.OperatorID` / `obftbase.OperatorID` / `twoab.OperatorID` / `ct.OperatorID`). The transport is already 95% expressed in shared `ct` types (`ct.MeshNode`, `ct.MsgID`, `ct.MsgKind`, `ct.MeshTopology`, `ct.BandwidthReport`, `ct.NetworkModel`); only the final `builder(recipientOp)` call touches the protocol's operator-ID. Resolve the seam (§2.3) and the shared engine + transport collapse to one copy.

### Secondary issues (real, smaller, independent of the dedup)

- **`SweepPoint.Fields map[string]float64` is a stringly-typed cross-run merge key.** `reporting/data.go`'s `fieldsKey()` joins sorted keys with `%g`-formatted values; a key typo or a last-ULP float drift silently splits one logical data point into two in the report, with no schema validation. Genuine fragility.
- **`sweep.go` repeats three near-identical network-wrap builders** (`p2pPacketLossSweep`, `p2pCorrelatedDelaysSweep`, `p2pNodeSlownessSweep`) — same "wrap `cfg.Network` and `cfg.Mesh.HopDelay` with a fresh stateful model per point" pattern, copied three times.
- **Safety checks are late and terminal:** `batch.go` runs `ComputeSafetyReport` in the single-threaded reduce phase and `SafetyPanic`s on the first violation, discarding the rest of a long stress batch. Correct for correctness mode; awkward for stress.
- `SimConfig` is a ~20-field god-config with heavy per-field docs (likely inherent to a parameterized harness, but worth noting).

### What is genuinely protocol-divergent (must stay per-protocol)

- **Protocol event handlers** — QBFT `evtStartInstance`/`evtMessageArrival`/`evtRoundTimeout`/`evtByzProposal`; OBFT `evtLeaderFetch`/`evtCommitArrival`/`emitOwnCommit`/resolve walk; twoab Phase-2a (`evtPhase2aFire`, `scheduleValueMsg`/`scheduleNoValueMsg`); psigs `evtPSigSign`/`evtPSigArrival`.
- **`newSim` / `start` / `outcome`** — they construct and read protocol `Instance` objects; only the loop they sit inside is shared.
- **Byz kinds** — each protocol's attack surface differs; only the *scaffold* (`byzSet`, `crashOverlay`, the `translateByz` dispatch shape) is shared, not the kind bodies.
- **Timing algebra** — OBFT `T_commit`/`BroadcastBudget`/`FetchAt`; twoab `TPhase2a`/`T0Broadcast` max-form; QBFT per-round RT; psigs flat sign-time. The `BTTMultiplier`/`SafetyBuffer`/variant knobs.

### What is already fine (do not touch — see §7 Non-goals)

The `Protocol` interface and the adapter seam; the top-level layering; the `Scenario`/`Catalog`/`ExpectClass` model; the network models (`network.go`) and mesh topology (`mesh.go`) as shared substrate; the safety-invariant set.

### Why this is a low-risk, high-leverage target

The four sub-packages are **imported only by the parent package's `*_test.go` files** — nothing in production wires them. There are ~100 test functions, including byte-identical-trace **determinism tests** (`TestBatch_Determinism`, `TestSmoke_TraceDeterministic`, `TestMesh_BuildDeterministic`, `TestLossyNetwork_Deterministic`, `TestCorrelatedLinkDelay_Deterministic`). Those act as ready-made golden tests: extract the engine, keep the traces byte-identical, and correctness is verified mechanically.

---

## 2. Target architecture

### 2.1 Where the shared code lives

Mirror the protocol-side outcome (a `obft/` parent holding the shared core, thin `base/`+`twoab/` siblings). Introduce one new sub-package:

```
protocol/v2/consensustest/                 ← abstract framework + shared substrate (unchanged role)
  protocol.go, scenario.go, host.go,       ← Protocol/SimConfig/Outcome/Scenario/Catalog/ExpectClass
  byz.go, faults.go, runner.go, matrix.go
  mesh.go, network.go, bandwidth.go,       ← shared transport substrate (already shared today)
  safety.go, offlineagg.go, stats.go, ...
  catalog_*.go                              ← scenario data

protocol/v2/consensustest/desim/   [NEW]   ← shared DES engine + mesh/gossip transport + byz/crash scaffold
  engine.go      ← eventQueue + heap, runLoop, schedule, the Loop/Host seam
  transport.go   ← evtMeshArrival / evtMeshHeartbeat / evtMeshIHave / evtMeshIWant, cacheArrivalForGossip, scheduleInitialHeartbeats
  byzscaffold.go ← byzSet, crashOverlay, the translateByz dispatch shape
  adapter.go     ← shared Run() scaffold: Validate-wrap, clip + pre-clip snapshot, bandwidth setup (classify stays per-protocol)

protocol/v2/consensustest/qbft/            ← thin: QBFT events + byz kinds + RT timing + outcome/start
protocol/v2/consensustest/psigs/           ← thin: PSigs events + minimal byz + flat timing
protocol/v2/consensustest/obft/    ┐
protocol/v2/consensustest/twoab/   ┘       ← OBFT-family twins; §4 collapses their genuinely-shared core
```

`desim` imports `ct` (parent) for `MeshTopology`/`NetworkModel`/`BandwidthReport`/`OperatorID`/`MsgKind`. The protocol packages import both `ct` and `desim`. `ct` imports neither → no cycle.

**Why this layering (the proper long-term split, resolved):** the dividing line is **contract + substrate vs. mechanism**. `desim` owns the simulation *mechanism* — the DES engine (loop/queue/schedule) plus the shared transport event-handlers and byz/crash scaffold. The transport *substrate* those handlers drive — `MeshTopology`, the `NetworkModel` implementations, `BandwidthReport`, `MeshConfig` — stays in `ct`, because those types are referenced by the framework *contract* (`SimConfig.Network`/`SimConfig.Mesh`, `Outcome.Bandwidth`) and describe *what a sim spec/result is*, not *how it runs*. That gives a stable kernel (`ct` = contract + substrate) that everything depends on, an execution package (`desim` = the code this refactor makes single-source-of-truth), and adapters depending on both — a DAG with no cycles and zero churn to `SimConfig`/`Outcome`. The one asymmetry — `MeshTopology` (runtime state) living in `ct` rather than beside its handlers — is intentional: it is built by `SimConfig.MakeMeshTopology()` and is "the realized network the sim runs on" (sim *environment*), which the handlers operate on through its methods.

Folding the whole engine into parent `ct` was rejected (it conflates the abstract contract with one concrete engine and grows an already-large package). A broader decomposition that *also* moves the orchestration layer (`sweep`/`batch`/`reporting`) out of `ct` is a reasonable longer-term direction but is **out of scope here** — it is not part of the duplication problem and would be churn mixed into this refactor. (`desim` is a placeholder name carried from the prior plan; bikeshed at implementation time.)

### 2.2 The seam

Two small shared types let the per-package `sim` stay the concrete owner of all protocol state while the engine/transport live in `desim`:

```go
// desim
type Event interface {
    handle(Host) []Scheduled   // returns follow-on events to schedule
    describe() string          // for TraceEntry
}
type Scheduled struct { When time.Duration; Ev Event }

// The engine-facing surface a sim must expose. ~6 methods, all already
// present as sim fields/methods today.
type Host interface {
    Now() time.Duration
    Rng() *mrand.Rand
    Mesh() *ct.MeshTopology
    Network() ct.NetworkModel
    Bandwidth() *ct.BandwidthReport
    Schedule(when time.Duration, ev Event)
}
```

- **Shared transport events** (`evtMeshArrival` etc.) live in `desim`, are identical for all protocols, and use **only** `Host` + a `builder func(ct.OperatorID) Event` closure. They never touch protocol state, so they need no type assertion.
- **Protocol-specific events** (e.g. `obft.evtCommitArrival`) implement `desim.Event` and start with one line, `s := h.(*sim)`, to reach their concrete state. The assertion is safe (the engine only ever holds this package's sim) and is the single ergonomic cost of the interface seam.
- The `ct.OperatorID ↔ protocol OperatorID` conversion happens inside the protocol's builder closure — exactly where it already happens today (`obftbase.OperatorID(mesh.OperatorForNode(e.to))`), so no *new* conversions are introduced.

### 2.3 Recommended technique for the operator-ID seam

**Recommendation: interface-first, with `ct.OperatorID` as the transport lingua franca; reserve generics for a local spot only if it is obviously cleaner there.**

Rationale:
- The transport is already in `ct` types; the only protocol touch is the builder boundary, which the closure already converts. Typing `builder` as `func(ct.OperatorID) desim.Event` makes the entire transport protocol-agnostic and **non-generic**.
- The engine loop (queue/seq/now/rng/trace/`runLoop`/`schedule`) touches no operator-ID at all — an `Event` interface is sufficient; no type parameters needed.
- Generics would force `desim.Engine[OP, EV]`, `desim.Sim[OP, EV]`, and `[]desim.Scheduled[*sim]`-style noise through every signature, for no added safety — the operator-ID crossing is a single boundary call. Per the §3 flag/readability guardrails, that is more machinery than the problem warrants.

Tradeoff (stated honestly): the interface seam costs one `h.(*sim)` assertion per protocol-specific handler. If those proliferate unpleasantly during Phase 1, the fallback is a **single** type parameter on the engine (`Engine[S Host]`) to drop the assertion — adopt it only if the asserts measurably hurt readability, not preemptively.

### 2.4 What each thin protocol package keeps

Protocol events + byz kinds + timing derivation + `newSim`/`start`/`outcome` + the per-protocol `classify*Miss` and `computeAttestation`. Everything that reads or constructs an `Instance`, or encodes a protocol decision, stays home.

---

## 3. Guardrails against over-deduplication (binding)

Over-deduplication is itself an antipattern: the *wrong abstraction* costs more than duplication. The aim is to remove **knowledge expressed twice**, not to merge code that merely **looks** alike. These rules govern every extraction; **when in doubt, leave it duplicated.**

- **Litmus test (apply to every extraction).** Share it only if both copies must change *for the same reason*. The gossip dedup logic qualifies (a libp2p-fidelity fix must hit all four). A handler that two protocols happen to write similarly but would tune independently does not.
- **Share mechanism, not policy.** Extract plumbing; leave decisions per-protocol. The mesh forward/dedup loop is mechanism (share it); *when* a protocol emits, *what* it commits, and its miss-classification are policy (keep them).
- **The flag test (hard stop).** If a proposed shared helper needs a `protocol`/`isObft` flag, grows more than ~1 boolean param, or sprouts `if obft {…} else {…}` to serve both callers — the abstraction is wrong. Prefer two clear copies.
- **Readability beats LOC.** Don't extract a 5-line helper to dedup 5 lines. A shared abstraction that forces a reader to chase indirection to understand one call site is a net loss even if it removes lines.
- **Cleaner shape wins, per-case (not a fixed obft- or twoab-direction).** Where the twins genuinely share a part but differ in shape, the shared form follows the simpler/cleaner one, decided case-by-case.
- **Prototype borderline items on ONE case first.** If the abstraction isn't *obviously* cleaner than the duplication, stop and keep them separate. Record the decision so it isn't re-litigated. It is expected — not a failure — that some Phase-4 items come back "kept duplicated on purpose."

**Classification of the proposed extractions:**

| Extraction | Verdict | Note |
|---|---|---|
| `eventQueue` + heap, `runLoop`, `schedule` | **Solid** | pure mechanism, byte-identical across all 4; the loop is one concept |
| Mesh/gossip transport (`evtMesh*`, `cacheArrivalForGossip`, `scheduleInitialHeartbeats`) | **Solid** | line-identical today; the "Mirrors the OBFT adapter" comment is the proof that it's shared knowledge maintained by hand |
| `byzSet` / `crashOverlay` | **Solid** | "crashed = offline in every role" is one invariant; the wrapper is identical mechanism |
| `translateByz` *dispatch shape* | **Borderline** | share the switch/`ErrNotApplicable` skeleton only if it's clean; the **case bodies (byz kinds) stay per-protocol**. Watch the flag test. |
| Adapter `Run` scaffold (Validate-wrap, pre-clip snapshot + `ClipLateDecision`, bandwidth setup) | **Borderline→Solid** | share the obvious glue; **`classify*Miss` and timing derivation stay per-protocol** |
| obft↔twoab Phase-1 build/observe | **Borderline** | prototype first; do NOT force a unified `ObservePhase1Bundle` — the tails differ |
| obft↔twoab Phase-3 resolve walk | **Borderline** | share the for-k skeleton + leaf helpers; if the per-protocol hook turns ugly, keep `resolve` per-package |
| Per-protocol event handlers / byz kind bodies / timing algebra | **Keep duplicated** | genuinely protocol-divergent; this is the seam |
| psigs into OBFT-family abstractions | **Keep separate** | psigs has almost no protocol logic; it joins only the Hat-1 engine/transport, never the obft-family core |
| A unified `Instance`/`sim` interface across all 4 | **No** | method sets are protocol-disjoint (mirrors the protocol-side "no unified Instance interface" decision) — fat union / weak intersection |

---

## 4. Phased execution

Each phase is independently shippable and **must keep every existing test green** — most importantly the determinism tests (byte-identical traces). No phase changes observable behavior or reported metrics.

### Phase 0 — Safety net (no behavior change)
- Confirm the determinism tests cover all four protocols with tracing on; if a protocol is thin on trace coverage, add a `TraceEnabled` golden for one healthy + one adversarial scenario per protocol so Phases 1–4 are guarded end-to-end.
- Optionally snapshot a small `reporting/data.go` output (one sweep) as a golden to guard Phase 5's Fields work.
- No production code touched.

### Phase 1 — Shared DES engine loop (`desim/engine.go`)
- Introduce `desim.Event`/`Scheduled`/`Host` and the `eventQueue` + `runLoop` + `schedule` core.
- Migrate one package first (psigs — smallest, simplest events) end-to-end as the proof of seam; then qbft, obft, twoab.
- Each `sim` implements `Host`; each package's events implement `desim.Event`.
- Gate: determinism tests byte-identical after each package migrates.

### Phase 2 — Shared mesh/gossip transport (`desim/transport.go`)
- Move `evtMeshArrival`/`evtMeshHeartbeat`/`evtMeshIHave`/`evtMeshIWant` + `cacheArrivalForGossip` + `scheduleInitialHeartbeats` into `desim`, typed on `ct.OperatorID` + `builder` closures.
- Unify the `round`/`layer` field as one neutral name (e.g. `phase int` or keep `layer` with qbft passing its round). Delete the four copies.
- This is the single highest-value, lowest-risk chunk — do it immediately after the engine seam exists.
- Gate: determinism tests + the mesh-specific tests (`TestMesh*`, `TestMeshGossip*`) byte-identical.

### Phase 3 — Shared byz/crash + adapter scaffold (`desim/byzscaffold.go`, `desim/adapter.go`)
- Extract `byzSet` + `crashOverlay` (identical mechanism).
- Extract the adapter `Run` glue: the `Validate`→`ErrConfigOutOfEnvelope` wrap, the pre-clip snapshot + `ClipLateDecision`, bandwidth-report setup. Keep `classify*Miss`, `computeAttestation`, and all timing derivation per-protocol (flag test).
- Decide on `translateByz`'s dispatch skeleton per the §3 borderline verdict — share only if clean.

### Phase 4 — obft↔twoab twin consolidation (reference-guided; prototype-and-revert)
- This is the Hat-2 work. **Use the shipped protocol-side split as the authoritative reference for where the genuine divergence lies:** `protocol/v2/obft` (parent shared core + thin `base/` + `twoab/`). The harness `obft`/`twoab` packages wrap exactly those protocols, so the harness cut-lines should mirror the protocol cut-lines. The seam runs through **Phase 2/2a** (message/wire types, protocol timings, Rule 6a); Phase-1 build/observe and the Phase-3 resolve walk are the shared-*candidate* mechanics.
- **Methodology — decided at execution time, not committed up front:** before extracting, re-read the actual `base`/`twoab` divergence to confirm the seam; then prototype one flow (start with Phase-1 build/observe), evaluate against §3, and **keep it only if it is obviously cleaner — otherwise revert and record "kept duplicated on purpose."** Let the prototype outcomes draw the line. Explicitly: do **not** fully skip Phase 4, and do **not** force a total merge.
- **Phase-2a stays entirely in twoab** — the genuine protocol divergence (the seam runs through Phase 2, exactly as on the protocol side).
- Expect a mixed outcome: some flows merge, some stay as two clear copies.

### Phase 5 — Secondary cleanups (independent; any order)
- **Fields typing (decided: enum):** introduce a `FieldKey` enum whose `String()` emits the *same* wire names the report and the `stresstest-report/app.js` UI already consume (`"N"`, `"K"`, `"BTT"`, `"p2p_profile"`, `"Instability"`, `"BFT_start"`), so the data.js / JS-UI contract is unchanged while a key typo becomes a Go compile error. Centralize the `%g` value formatting in one place so sweep-side and report-side can't drift. This is the one cleanup with cross-language (Go↔JS UI) coupling — guard it with the Phase-0 data.js golden.
- **Sweep-builder dedup:** extract one `withFreshNetworkWrap(point, makeModel)` helper for the loss/correlated/slowness sweeps.
- **Safety mode (optional):** add a "collect violations, fail at end" mode for stress runs while keeping correctness mode's fail-fast `SafetyPanic`.

### Cross-cutting — comment hygiene (any time)
- The comments are thorough and mostly excellent — keep that. Only strip dev-history residue (no "vN", "pre/post-X", dangling refs to deleted docs) and update any comment that points at a moved symbol after Phases 1–4. Do **not** gut the genuinely-useful current-state documentation.

---

## 5. Risks & mitigations

- **Determinism drift** (a refactor changes event ordering / RNG draw order) → caught immediately by the byte-identical-trace tests; migrate one package at a time and re-run after each.
- **Generics sprawl** → avoided by the interface-first recommendation (§2.3); generics only as a measured fallback.
- **Over-DRY** → §3 is binding; borderline items are prototyped on one case and may be kept duplicated on purpose.
- **Phase 4 turning into a tar pit** → it is explicitly prototype-gated and can be stopped at any sub-flow; Phases 1–3 + 5 deliver most of the value and stand alone if Phase 4 is deferred.
- **`desim`↔`ct` back-references** → if the engine needs something only the parent has and the parent needs the engine, that's a sign to host the engine in `ct` instead (§2.1 alternative); decide at Phase 1.

## 6. Resolved decisions

1. **Engine home (§2.1):** new `desim` sub-package owns the mechanism (engine + transport handlers + byz scaffold); the transport substrate (`MeshTopology`/`NetworkModel`/`BandwidthReport`/`MeshConfig`) and the abstract contract stay in `ct`. Chosen as the proper contract-+-substrate vs. mechanism layering; a broader orchestration split is out of scope.
2. **Phase 4 extent (§4):** reference-guided by the shipped `protocol/v2/obft` `base`/`twoab` split, executed prototype-and-revert, with the exact extent decided as the work proceeds — neither skipped nor force-merged.
3. **Fields typing (§5):** a `FieldKey` enum that preserves the existing string names (compile-time typo safety, unchanged JS-UI contract).
4. **psigs (§1, §3):** stays a full sub-package; it joins the Hat-1 engine/transport only, never the obft-family core.

The only thing deliberately left to execution time is the precise *extent* of Phase 4 (decision 2) — by design, since it is resolved by prototyping against the real divergence rather than guessed up front.

## 7. Non-goals

- The `Protocol` interface, the adapter seam, the `Scenario`/`Catalog`/`ExpectClass` model, the network/mesh substrate — unchanged.
- No unified cross-protocol `Instance`/`sim` interface.
- No change to observable behavior, reported metrics, or the data.js schema (Phase 5 changes how Fields are *typed*, not their values).
- No forcing psigs (or qbft) into OBFT-family abstractions.
- No production code changes — the harness is test-only.

# OBFT Maintainability Refactor — Shared-Core Restructure Plan

Status: working draft, 2026-05-23. Author: investigation + back-and-forth with iurii.

Scope decisions agreed up front:
- **base and twoab both stay permanent** → the goal is a real *shared core* both build on, not the convergence plan's "keep two hand-aligned copies".
- **Deep restructure** is in scope: extract shared core, introduce subsystem seams, decompose god-objects. (A *single unified `Instance` interface* is not — see Phase 0.)
- **No backward-compatibility constraint** — both protocols are internal-only today (twoab has zero node wiring; base's wire format is not externally frozen for our purposes). Wire/API/behavior may change where it makes the end result better.

This plan supersedes the stance of [docs/OBFT-TWOAB-CONVERGENCE-PLAN.md](OBFT-TWOAB-CONVERGENCE-PLAN.md), which deliberately kept two parallel implementations and aligned their *coding patterns* by hand. That convergence work was the right interim step and it helps us now (the two are close enough that extraction is largely mechanical), but hand-maintained parallelism is the root maintainability tax and this plan removes it.

---

## 1. Honest assessment (where the pain actually is)

The suspected hotspots are mostly **not** where the entanglement lives:

- **Crypto is clean.** [blsbackend](../protocol/v2/obft/blsbackend) hides herumi/kyber/drand behind the parent package's narrow `Signer` / `ThresholdIBE` interfaces; no crypto library leaks into protocol code. (One nit: the BLSSigner-vs-KyberSigner DST distinction is convention, not type-encoded.)
- **The runner → protocol boundary is narrow**, and the **production integration surface is clean** (verified). All seven node-side integration points — [obft_validation.go](../message/validation/obft_validation.go), [setup_obft.go](../operator/validator/setup_obft.go), [proposer_obft.go](../protocol/v2/ssv/runner/proposer_obft.go) + its split half [proposer.go](../protocol/v2/ssv/runner/proposer.go), [validator.go](../protocol/v2/ssv/validator/validator.go), [queue/messages.go](../protocol/v2/ssv/queue/messages.go), [testing/runner.go](../protocol/v2/ssv/testing/runner.go) — touch only the *public* API, and most talk primarily to the runner adapter façade ([runner/obft](../protocol/v2/ssv/runner/obft)) rather than to `base` directly. The symbols escaping into production are stable exported value/identity types: `OperatorID`, `Output`, `Evidence`, `EvidenceObserver`, `Verifier` (+ its `Verify*` methods), the `wire` package (`Envelope`, `Kind*`, `Unwrap`, `MessageKind`), and the `blsbackend` constructors (`New`/`NewTLockIBE`/`NewKyberSigner`, production-wired in [setup_obft.go:16](../operator/validator/setup_obft.go:16)).
- **Neither test harness reaches into protocol internals** — no reflection/unsafe/unexported access. They drive the *same exported lifecycle API* the production runner does.

The real maintainability tax, ranked:

| # | Problem | Evidence | Severity |
|---|---|---|---|
| P1 | **Two large protocol implementations kept in sync by hand** (base ~5.2k / twoab ~6.2k non-test LOC). base and twoab are near-twins outside Phase 2. | [base/instance.go](../protocol/v2/obft/base/instance.go) vs [twoab/instance.go](../protocol/v2/obft/twoab/instance.go); 60-asymmetry catalog in the convergence plan | **Dominant** |
| P2 | **Two test harnesses kept in sync by hand.** twoab's harness near-clones the obft harness's DES engine / transport / byz scaffold; `events.go` genuinely diverges (cascade-driven Phase 2b). | non-test source: [consensustest/obft](../protocol/v2/consensustest/obft) 2780 vs [consensustest/twoab](../protocol/v2/consensustest/twoab) **3145** (twoab is the bigger side); twoab `des.go` comments say "mirrors the OBFT helper" | **High** |
| P3 | **`Instance` god-object** — ~5 candidate subsystems, but only 2 cleanly separable (see Phase 3); the rest stay read-through. | [base/instance.go:146](../protocol/v2/obft/base/instance.go:146) (~30 fields), [twoab/instance.go:77](../protocol/v2/obft/twoab/instance.go:77) | High |
| P4 | **Wire reader/writer primitives 3-way duplicated**; the *drifted* safety bounds are **2-way** (base/wire vs twoab/wire — `MaxLayers` `>=` vs `>`, per-field caps vs coarse 16 MiB). tlock's reader is a 3rd primitives copy but a different domain (no layer caps; extra `readRest`/`remaining`). | [base/wire/wire.go](../protocol/v2/obft/base/wire/wire.go), [twoab/wire/wire.go](../protocol/v2/obft/twoab/wire/wire.go), [tlock_ibe.go:241](../protocol/v2/obft/blsbackend/tlock_ibe.go) | High (correctness) |
| P5 | **Runner `Controller` god-object** + 12 forwarding methods repeating a `lookup→lock→ended-check→delegate` body (the ended-check is real code, in 9 of 12; only its *rationale* is prose). | [controller.go](../protocol/v2/ssv/runner/obft/controller.go) | Medium |
| P6 | **Harness internal debt** (separate from P2): `sim` god-object, a 47-line handler doing 5 jobs. | [des.go:32](../protocol/v2/consensustest/obft/des.go), `evtCommitArrival` in [events.go](../protocol/v2/consensustest/obft/events.go) | Medium |
| X | Cross-cutting: 9-param constructors; doc comments 2–4× the logic. (A unified `Instance` interface is *not* pursued — the method sets are Phase-2-disjoint; see Phase 0.) | `NewInstance` (9 params) in both | Low–Medium |

### The duplication, concretely (verified by full read)

Exists in **both** protocol packages, differing only by an error-string prefix or a trivial shape detail:

- Crypto-deps clump (`signer, tagSigner, ibe, clusterPubKey, pubKeyShares, ibePubKeyShares`) + the `NewInstance` validation of them.
- `chainEncryptForLayer` / `chainDecryptForLayer` (the chained-IBE onion) — effectively identical ([base/instance.go:824](../protocol/v2/obft/base/instance.go:824), [twoab/instance.go:806](../protocol/v2/obft/twoab/instance.go:806)).
- `transitionToSigma` / `transitionToNR` (EKM locks).
- `recordEvidence` + `recordRulePerLayer` + per-rule dedup maps + `EvidenceObserver` + `evidenceObservedKey` — the evidence-engine **mechanics** are identical. The evidence **types** need four reconciliations: Rule 6a (+ a slot-wide dedup variant base lacks), `LeaderEquivocationEvidence`'s `SourceA`/`SourceB` ([twoab/evidence.go:184](../protocol/v2/obft/twoab/evidence.go:184)), the Rule-3 payload rename (`CrossOnion…`→`CrossCommit…`), and base's `CommitEquivocationEvidence` + `Layer=-1` path with no twoab analog. So the engine is shareable; most payload types are not.
- The host-validation channel mechanism (`wantsHostValidationCh`, `pendingValidation`, `requestHostValidation`, `ValidationRequest`).
- The `l0ReadyCh` + `maybeSignalL0Ready` mechanism (predicate differs; plumbing identical).
- `Stats`, `Evidence`, `Finalize`, `RetainedBundles` snapshot helpers.
- **Identical message types** (verified byte-for-byte modulo package): `Certificate`, `Output`, the `ValueRoot` / `writeUint32` / `writeUint64` helpers. And `Phase1Bundle` is the same 6-field struct — base's `SigmaV` and twoab's `LWitness` are the same artifact (the layer leader's σ partial on V), just renamed.
- **`RetainedCertificate`** is character-identical; **`BuildCertificate` / `ObserveCertificate`** are identical *modulo the package error-prefix* (`obft:`/`twoab:`) on each error path (+ a doc-comment).
- **The Phase-3 walk**: `Resolve` + `tryDeriveNextLayerKey` are shared logic, but the reconciliations are real — not a single hook: the `sigGroup` container differs (`[]*sigGroup` vs `map[[32]byte]*sigGroup`), `selectWinningGroup`'s signature follows it, the σ-partial *collection* step differs (base inlines onion-decrypt + witness harvest; twoab pre-populates `sigmaPool` + `aggregatePeerLayerEntries`), and twoab carries an ~86-line `recoverV` with no base analog (base partials carry V plaintext). Shareable, but Phase 5 is non-trivial.
- **`ValidatePhase1Bundle` / `ValidateCertificate`** + the envelope-header checks (ClusterID/Height/OperatorInCluster match) — shared shape.
- **The SSV-boundary `Verifier`** ([base/verify.go](../protocol/v2/obft/base/verify.go)): its key-context + `VerifyPhase1Bundle`/`VerifyCertificate` are shareable. twoab has no `Verifier` at all — the shared `Verifier` core (Phase 5) is the basis for E4; actually wiring twoab message-validation onto it is Phase-L work (§5).
- Config topology + threshold logic (`LayerSpec`, `K/QV/QEnc/Quorum`, member/leader/F/K validation) + the type-alias/re-export block at [base/types.go:37](../protocol/v2/obft/base/types.go:37) and [twoab/config.go:46](../protocol/v2/obft/twoab/config.go:46).

And, on the harness axis (P2): the DES engine (`sim`/`runDES`/`runLoop`/`schedule`/`outcome`), the transport/mesh/gossip layer (`emitDirect`/`emitMesh`/heartbeats/IHave/IWant), and the byz scaffolding (`internalByz`/`broadcastPlan`/`byzSet`/`crashOverlay`/`honestDefaults`/`translateByz` + ~20 `byz*` pattern types) are near-cloned between the two harnesses. `events.go` is the exception — it genuinely diverges (twoab's cascade-driven Phase 2b vs obft's schedule-anchored flow), so this is a *subset* clone, not a whole-harness one.

### What is genuinely protocol-divergent (must stay per-protocol)

The seam runs through **Phase 2**. Keep separate:

- **Phase 2 logic.** base: single `KindCommit` at `T_commit` with a hard `T_commit` acceptance wall. twoab: Phase-2a `KindValue`/`KindNoValue` split + trigger-driven Phase-2b + NoValue→Value upgrade + peer-reflood-V harvest + cascade-on-state-delta + Rule 6a.
- **Phase-2 message types + wire formats.** base: `Commit`, `EncryptedLayer`, `NRPartial`, `LeaderSigmaWitness`. twoab: `ValueMsg`, `NoValueMsg`, `Commit`, `LayerEntry`, `LayerWitness`. (`Phase1Bundle`, `Certificate`, `Output` are NOT divergent — they move to the shared core; see above.)
- **Protocol-specific timing.** base `Config`: `TCommit`/`Delta2`/`Eps3`. twoab `Config`: `TPhase2a`/`SafetyBuffer`.
- **Rule 6a** (Phase-2 equivocation) and twoab's per-message validators (`ValidateValueMsg`/`ValidateNoValueMsg`/`validateLayerEntries`) — twoab-only by construction.
- **Per-protocol `Commit` verification** in the `Verifier` (different `Commit` shape) and the protocol-specific σ-partial collection in Phase 3.

This matches the convergence plan's split (~25 forced / ~35 convergeable). The "convergeable 35" become "share the code", not "align by hand".

---

## 2. Target architecture

The parent `obft` package already owns the right things (`Signer`, `ThresholdIBE`, `NoQuorumTag`, type aliases, `OperatorInCluster`, `ErrInstanceEnded`) and is the natural home. We grow it into a real shared-core, with each protocol package becoming a thin Phase-2-specific layer. Symmetrically, the two harnesses grow a shared DES/transport/byz engine.

```
protocol/v2/obft/                      ← shared protocol core (grows)
  signer.go, ibe.go, tag.go            ← (today) crypto interfaces + stubs
  cluster_config.go    [NEW]           ← ClusterConfig + LayerSpec + topology validation
  crypto_context.go    [NEW]           ← CryptoContext: keys + chained-IBE + partial verify + ctor validation
  ekm.go               [NEW]           ← EKM: σ/NR lock arrays (read-through — l0/evidence query it; see Phase 3)
  evidence.go          [NEW]           ← EvidenceEngine dedup sink + shared payload types + observer (per-rule firing stays on Instance; Phase 3)
  hostgate.go          [NEW]           ← HostValidationGate: wants-channel + dedup
  l0ready.go           [NEW?]          ← L0ReadySignal: close-once channel; extract only if it pays (Phase 3)
  reconstruct.go       [NEW]           ← Phase-3 walk skeleton + sigGroup + selectWinningGroup + cert methods
  message.go           [NEW]           ← shared wire types: Phase1Bundle, Certificate, Output + ValueRoot/writeUintN + shared validation helpers
  verify.go            [NEW]           ← SSV-boundary Verifier: shared key-context + bundle/cert verify
  instance.go          [DEFERRED]       ← only if Phase L proves a real seam; method sets are Phase-2-disjoint, so one union interface is a fat union / weak intersection (see Phase 0)

protocol/v2/wire/                      ← shared wire primitives (grows; already holds framing.go)
  codec.go             [NEW]           ← reader/writer primitives + ONE reconciled set of size caps (lifted from the 3 copies)

protocol/v2/obft/base/                 ← thin: bare-OBFT Phase 2 + its message types + wire format + Commit-verify
protocol/v2/obft/twoab/                ← thin: 2abOBFT Phase 2a/2b + its message types + wire format + Commit-verify

protocol/v2/obft/driver/   [DEFERRED, post-Phase-L] ← shared per-slot orchestrator; only if the real runner's sequence mirrors the sim's (see Phase 7)
protocol/v2/consensustest/desim/ [NEW] ← shared DES engine + transport/mesh/gossip + byz scaffold (generic over protocol)
protocol/v2/ssv/runner/obft/           ← real-transport driver binding + Controller (decomposed)
protocol/v2/consensustest/{obft,twoab} ← thin: protocol-specific events/byz kinds over the shared desim engine
```

Each protocol `Instance` becomes **composition over the shared subsystems** plus its own Phase-2 state:

```go
// illustrative — twoab.Instance after refactor
type Instance struct {
    cfg    *Config            // embeds obft.ClusterConfig + twoab timings
    crypto *obft.CryptoContext
    ekm    *obft.EKM
    ev     *obft.EvidenceEngine[Evidence]
    host   *obft.HostValidationGate
    l0     *obft.L0ReadySignal
    // ^ crypto + host are clean leaves; ekm/ev/l0 hold mechanism only —
    //   firing, predicates, and the pool read-web stay on Instance (see Phase 3).

    // --- twoab-specific Phase-2 state only ---
    retainedBundles map[int]map[OperatorID][]*retainedBundle
    valuePool, sigmaPool, noValuePool, nrTagPool ...
    ownValueMsg, ownNoValueMsg, ownCommit ...
    cascadeErrors []error
    ...
}
```

**Composition over embedding** is the recommendation: explicit `i.ev.Record(...)` / `i.crypto.ChainEncrypt(...)` reads better than promoted methods and avoids method-name collisions, at the cost of a little forwarding. (Open for discussion — embedding is viable for the leaf subsystems if we prefer terseness.)

### Subsystem sketches

- **`ClusterConfig`** — `Height, ClusterID, Operators, F, Layers []LayerSpec, BTT, BFTStart`; methods `K/QV/QEnc/Quorum/BroadcastMaxOffsetForLayer/ValidateTopology`. Protocol `Config` embeds it + adds timings + a `Validate()` that calls `ValidateTopology()` then checks its own timing fields.
- **`CryptoContext`** — the 6-field key clump; methods `ChainEncryptForLayer`, `ChainDecryptForLayer`, `VerifyNRTagPartial`, `VerifySigmaPartial`, and `Validate()` (the `NewInstance` key/share checks). Constructed once, passed in.
- **`EvidenceEngine[E]`** — generic over the per-protocol evidence item (which exposes `Key() (rule int, op OperatorID, layer int)`); holds the accumulator, observer, per-(rule,op,layer) dedup, and a slot-wide dedup variant (Rule 6a). Methods: `Record(E)`, `RecordFirstPerLayer(rule,op,layer) bool`, `RecordFirstPerOp(...) bool`, `Snapshot() []E`, `Count() int`. Only the genuinely-identical payloads (`CrossSigningEvidence`, `FakeEncryptedPresenceEvidence`, `FakePlaintextSigmaEvidence`) move to `obft`; the divergent ones stay per-protocol (`LeaderEquivocationEvidence` has twoab `Source` fields; the Rule-3 payload differs in name `CrossOnion`/`CrossCommit`; base has `CommitEquivocation`+`Layer=-1`, twoab has `Phase2Equivocation`/Rule 6a). Each protocol keeps its own `Evidence` union + rule enum. **The per-rule *firing* stays on `Instance` (it reads pools); only this dedup sink moves.** *(Generics vs. a non-generic `obft.Evidence` with an `any` payload is an open call — see §7.)*
- **`EKM`** — `sigmaLocked []bool`, `sigmaLockedV []Value`, `nrLocked []bool`, `ownPartials map[int]Signature`; `TransitionToSigma`, `TransitionToNR`. (base's `transitionToNR` takes a diagnostic state arg; fold into a small variant.) **Read-through**: `l0DecisionReady`/evidence query `SigmaLocked(k)` back, so this cuts fields, not coupling (Phase 3).
- **`HostValidationGate`** — channel + `pendingValidation` + `RequestHostValidation` + `WantsHostValidationCh` + finalize-close. Predicate-free; pure plumbing.
- **`L0ReadySignal`** — close-once channel + `MaybeSignal(ready func() bool)`. Each protocol passes its own `l0DecisionReady` predicate (the only genuinely different part). **Conditional**: thin enough that it's extracted only if it pays for itself (Phase 3).
- **Shared message types + `Verifier`** — `Phase1Bundle` (unify base `SigmaV` / twoab `LWitness` → one field name, e.g. `LeaderSigma`), `Certificate`, `Output` move to `obft`. The Phase-3 walk skeleton + `sigGroup` + `selectWinningGroup` + `BuildCertificate`/`ObserveCertificate`/`RetainedCertificate` move to `reconstruct.go` with a per-protocol `collectSigmaGroups(layer, chainedKeys) → groups` hook. The `Verifier`'s key-context + `VerifyPhase1Bundle`/`VerifyCertificate` move to `obft`; per-protocol `Commit` verify stays in each package. twoab gains a `Verifier` core in Phase 5 (it has none today); wiring it into message-validation is Phase-L.
- **wire `codec.go`** — `appendUint16/32/64`, the `reader` struct + helpers, and **one** reconciled set of size caps. base/twoab wire packages keep their per-message Encode/Decode + tags + message structs but call these. **Reconcile the drift here** (2-way, base/wire vs twoab/wire): pick the correct `MaxLayers` bound (`>=` vs `>`) and a single cap policy (prefer base's tight per-field caps over twoab's coarse 16 MiB). `tlock_ibe.go`'s reader is a *separate-domain* codec (ciphertext framing, extra `readRest`/`remaining`, no layer caps) — evaluate whether it adopts the shared primitives or stays independent; don't assume a mechanical fold.
- **`consensustest/desim`** — the DES core (`sim`/event loop/schedule/outcome), transport (`emitDirect`/`emitMesh`/heartbeat/IHave/IWant), and byz scaffold (`internalByz`/`broadcastPlan`/`byzSet`/`crashOverlay`/`honestDefaults`/`translateByz`), generic over the protocol's instance + message types. Each `consensustest/{obft,twoab}` keeps only its protocol-specific event handlers and byz kinds.

### Integration-surface contract (must hold throughout)

Restructuring `base` internals is safe **as long as** these stay exported and importable where the node expects them:
- `OperatorID`, `Output`, `Evidence`, `EvidenceObserver` — if they move to parent `obft`, re-export from `base` via type alias.
- `Verifier` + its `Verify{Phase1Bundle,CommitNRPartials,CommitWitnesses,Certificate}` method set — consumed by [obft_validation.go](../message/validation/obft_validation.go).
- The `wire` package: `Envelope`, `Kind*`, `Unwrap`, `MessageKind` — consumed by `message/validation`, `queue`, `validator`.
- `blsbackend`'s constructors (`New`, `NewTLockIBE`, `NewKyberSigner`) — production-wired in [setup_obft.go](../operator/validator/setup_obft.go); the `Signer`/`ThresholdIBE` interfaces they return must keep their signatures.

twoab has zero node wiring, so only the `base` side (+ `blsbackend`) has an integration surface to preserve.

---

## 3. Guardrails against over-deduplication (don't over-DRY)

Over-deduplication is itself an antipattern: the *wrong abstraction* costs more than duplication. The aim is to remove **knowledge expressed twice**, not to merge code that merely **looks** alike. These rules govern every extraction below; when in doubt, leave it duplicated.

**Litmus test (apply to every extraction).** Share it only if both copies must change *for the same reason*. If base and twoab would plausibly evolve the piece independently — a Phase-2-driven change, a protocol-specific tuning — keep it duplicated. Coincidental shape ≠ shared knowledge.

**Share mechanism, not policy.** Extract the plumbing; leave the decision per-protocol. `HostValidationGate` shares the channel + dedup + non-blocking enqueue, but NOT when-to-request or the host-validity semantics (base closes L0Ready on any verdict; twoab deliberately doesn't for NoValue). `L0ReadySignal` shares the close-once channel; the `l0DecisionReady` predicate stays per-protocol.

**Cleaner shape wins (not a fixed protocol direction).** When base and twoab differ on a part that genuinely *is* shared, the shared form follows the *simpler/cleaner* shape — decided per-case, not "twoab-shaped" or "base-shaped" by default. The genuinely-shared parts are mostly where the two already agree; where they differ, it cuts both ways — twoab tends DRYer on code patterns (the convergence pass flowed ~15 patterns twoab→base vs ~10 the other way: pool helpers, `Stats()`, evidence-file org), while base is simpler on protocol surface (the "simpler-spec cousin" — [base/types.go:5](../protocol/v2/obft/base/types.go:5)) and tighter on safety bounds. Precedent: the wire-cap reconciliation (§2 `codec.go`) takes base's tight per-field caps over twoab's coarse 16 MiB because tighter is safer — but a blanket "either-by-default" rule would mispredict the next one. (Both protocols stay permanent regardless; this is a shaping rule, not a bet on which survives.)

**The flag test (hard stop).** If a proposed shared helper needs a `protocol`/`isBase` flag, grows more than ~1 boolean param, or sprouts `if base { … } else { … }` branches to serve both callers — the abstraction is wrong. Prefer two clear copies. (This is exactly why the convergence plan dropped B2 and D3 on self-review; the same judgment governs here.)

**Readability beats LOC.** Don't extract a 5-line helper to dedup 5 lines. A shared abstraction that forces a reader to chase indirection to understand one call site is a net loss even if it removes lines.

**Classification of the proposed extractions:**

| Extraction | Verdict | Note |
|---|---|---|
| CryptoContext / chained-IBE | **Solid** | one cryptographic algorithm; a fix must apply to both |
| Wire codec primitives + caps | **Solid** | byte-level primitives; the drift IS the bug this prevents. Message *types* stay per-protocol. |
| EvidenceEngine *mechanics* | **Solid (sink only)** | the record/dedup *sink* is one concept; the per-rule *firing* reads pools and stays in `Instance`. Rule catalog + payload unions stay per-protocol. |
| Certificate / Output / ValueRoot / writeUintN | **Solid** | identical value types / pure helpers, no protocol meaning |
| ClusterConfig topology validation | **Solid** | "valid cluster+layer topology" is shared knowledge. Timings stay per-protocol. |
| EKM locks | **Solid (mechanism) / read-through** | σ-XOR-NR is a shared invariant; extract the lock arrays + transitions, but `l0DecisionReady`/evidence read `sigmaLocked` back, so EKM becomes a *queried* collaborator — cuts struct fields, not the coupling |
| sigGroup / selectWinningGroup | **Solid** | `sigGroup` struct is identical (→ Phase 1); `selectWinningGroup` bodies match but signatures differ (`[]` vs `map`) → unify the container in Phase 5; `addToGroup` is base-only |
| Phase1Bundle type | **Solid** | same 6-field struct; unify the renamed σ field |
| HostValidationGate | **Borderline** | share plumbing only; semantics differ (see "share mechanism, not policy") |
| L0ReadySignal | **Borderline** | thin (close-once channel). Extract only if it pays for itself; predicate per-protocol |
| Phase-1 build/observe | **Borderline** | share deepCopy + Rule-1/2 firing; do NOT force a unified `ObservePhase1Bundle` — base `reevaluateL0Sigmas` vs twoab harvest are protocol-specific tails |
| Phase-3 walk skeleton + hook | **Borderline** | the for-k loop + `tryDeriveNextLayerKey` are shared; if the `collectSigmaGroups` hook turns ugly, keep `Resolve` per-protocol and share only the leaf helpers |
| EvidenceEngine *generics* | **Watch** | generics can over-engineer; if `E`+`Key()` feels forced, use a non-generic engine + `any` payload (open Q2) |
| Per-slot driver (Phase 7) | **Deferred / default-don't** | real vs simulated transport have different control-flow *and* failure models; can't even be evaluated until Phase L's real runner exists. Two drivers by default; share only small helpers if the sequences happen to match |
| `consensustest/desim` engine | **Borderline→Solid** | the DES loop/transport/byz-scaffold is a true shared engine, but verify the per-protocol event/byz hooks stay thin; don't fold protocol logic into the engine |

**Rule for borderline/risk items:** prototype the abstraction on ONE representative case first; if it isn't *obviously* cleaner than the duplication, stop and keep them separate. Record the decision (mirroring the convergence plan's "dropped on self-review" notes) so it isn't re-litigated. It's fine — expected, even — for some Phase-5/8 items to come back "kept duplicated on purpose."

---

## 4. Phased execution

Each phase is independently shippable and leaves all tests green. Order: low-risk high-leverage extractions first; deep/structural items once the seams exist.

**Scope tranches (read before the phase list).** The phases split by *runner-dependence*, because twoab's production runner ("Phase L") doesn't exist yet and will reshape any runner-facing seam:
- **Committed now — runner-agnostic:** Phases 1–4, plus the Phase-1/Phase-3 *reconstruction leaves* from Phase 5, plus the Controller decomposition (Phase 6, which touches only the *existing* base adapter). None of these touch twoab's future runner surface. This tranche also lands the one hard *correctness* win — the wire `>=`/`>` `MaxLayers` drift (Phase 4).
- **Deferred until after Phase L — runner-facing:** the `Instance` interface, the *twoab* `Verifier` wiring (Phase 5), and the shared driver (Phase 7). Re-derive these from twoab's real runner; don't freeze them against a consumer that doesn't exist.
- **Last, against frozen goldens:** harness consolidation (Phase 8).

Re-justify Phase 5 (sharing) and Phase 8 with real evidence after Phase 4; treat the deferred tranche as out of this pass.

### Phase 0 — Safety net (no behavior change)
- The two consensustest harnesses + unit tests are the regression oracle. Note current coverage gaps before touching code.
- **Freeze golden outputs**: capture the full `SimConfig → Outcome` matrix (both DES suites) as checked-in fixtures, so every later phase validates against a frozen baseline — not against a harness that Phase 8 will itself refactor.
- **No unified `obft.Instance` interface.** The two `Instance` method sets are largely Phase-2-*disjoint* (base `BuildOwnCommit`/`ObserveCommit` vs twoab `MaybeFirePhase2a`/`MaybeBuildAndBroadcastCommit`/`ObserveValueMsg`/`ObserveNoValueMsg`), and `ObserveCommit` has incompatible semantics across the two. A single interface is either a fat union (one-sided methods become `panic` stubs) or a thin intersection that excludes the very Phase-2 surface a driver needs. The harnesses already drive the concrete `Instance` fine. Revisit only if Phase L proves a real seam.
- Acceptance: builds + all tests green; goldens captured; no logic moved yet.

### Phase 1 — Stateless shared extractions (low risk)
- Move to `obft`: `ValueRoot`, `writeUint32/64`; the `sigGroup` struct (byte-identical in both); the chained-IBE pair into the `CryptoContext` skeleton (methods only, keys still on Instance for now); the type-alias/re-export block (delete the duplicate); and the **identical message types** `Certificate`, `Output`, plus `Phase1Bundle` (unifying `SigmaV`/`LWitness`).
- **Not here:** `selectWinningGroup` (signatures differ — `[]*sigGroup` vs `map[[32]byte]*sigGroup` — so unifying it is the Phase-5 container reconciliation, not a zero-risk move) and `addToGroup` (base-only; nothing to dedup).
- Both packages call the shared functions/types; delete the local copies. Re-export from `base` via alias where the integration surface requires it.
- Acceptance: tests green; net LOC down; zero behavior change.

### Phase 2 — Shared `ClusterConfig`
- Extract `obft.ClusterConfig` + `LayerSpec` + `ValidateTopology`. Each `Config` embeds it; `Validate()` delegates topology then checks timings. Extract a shared `validateEnvelopeHeader` (ClusterID/Height/OperatorInCluster) used by both packages' validators.
- Acceptance: config + validation tests green in both packages.

### Phase 3 — Shared stateful subsystems (lead with the clean leaves)
Land as separate commits, one subsystem each. **Two are genuine leaves** (extract first, low risk): `CryptoContext` (pure functions of construction-time immutables) and `HostValidationGate` (self-contained channel + dedup; needs only a read handle to `hostVerdict`).

The other three extract their **mechanism** but not their decision logic — the firing/predicate/read-web stays on `Instance`. They're three different confidence levels, not three maybes:
- `EvidenceEngine` (**extract — real win**): the `recordEvidence`/`recordRulePerLayer`/dedup *sink* is leaf-clean and identical across both — pull it out. Only the per-rule *firing* (twoab's `maybeFireCrossSigmaV` scans `sigmaPool` + `recoverV`) stays in `Instance`. Don't throw the sink out with the firing.
- `EKM` (**extract — read-through**): the lock arrays + transitions extract cleanly as *writers*, but `l0DecisionReady`/evidence read `sigmaLocked` back, so EKM is a *queried collaborator* (exposes `SigmaLocked(k)` accessors). Cuts fields, not coupling.
- `L0ReadySignal` (**conditional**): a thin close-once channel; extract only if it pays for itself (predicate stays per-protocol).

Note the actual center of the tangle — the **pools** (`peerOnions`/`peerNR`/`valuePool`/`sigmaPool`) — stays on `Instance` by design; these extractions cut struct field-count and remove duplicated *mechanics*, they do **not** flatten the pool↔evidence↔EKM read-web. That's expected, not a failure.
- Acceptance: tests green after each subsystem (vs frozen goldens); the shared sinks/mechanics land; `L0ReadySignal` may come back "kept inlined on purpose."

### Phase 4 — Wire primitives unification
- Lift reader/writer + caps into `protocol/v2/wire/codec.go`. Rewire base/wire and twoab/wire. **Reconcile the drifted bounds** (2-way: `MaxLayers` `>=` vs `>`; per-field caps vs 16 MiB) and add a focused test that both protocols reject over-cap inputs identically.
- Evaluate `tlock_ibe.go` separately: it's a ciphertext-framing codec (different domain, extra methods, no layer caps). Fold its reader in only if it genuinely matches; otherwise leave it (per §3 guardrails) — this is not a mechanical fold.
- Acceptance: wire round-trip tests green; base/wire + twoab/wire share primitives.

### Phase 5 — Shared Phase 1 / Phase 3 logic (+ shared Verifier core)
*Runner-agnostic part — re-justify after Phase 4, then proceed if appetite holds.*
- **Non-trivial** (not a thin hook): first unify the `sigGroup` container + `selectWinningGroup` signature and handle twoab's `recoverV` (no base analog), then move the `Resolve` loop + `tryDeriveNextLayerKey` + cert methods to `obft/reconstruct.go` with each package supplying its `collectSigmaGroups` hook. Factor the shared bundle build/observe (σ-lock-on-build, self-observe, Rule-1/Rule-2 firing through `EvidenceEngine`); base's retroactive L_0 evidence (`reevaluateL0Sigmas`) and twoab's harvest stay per-package. Per §3, if the hook turns ugly, keep `Resolve` per-protocol and share only the leaf helpers.
- Extract the shared `Verifier` *core* (key-context + bundle/cert verify) and keep base's existing `Verifier` working on it; per-protocol `Commit` verify stays in each package.
- **Deferred to post-Phase-L:** *wiring a twoab `Verifier`* into message-validation — that's runner-adapter (Phase L) work and should be derived from twoab's real validation surface, not pre-built here.
- Acceptance: the heaviest scenario tests (`scenarios_test.go`, `sim_test.go`) green.

### Phase 6 — Runner `Controller` decomposition
- Extract the pending-buffer LRU and the ended-slot ring into their own small types. Collapse the 12 `lookup→lock→ended-check→delegate` methods behind one `withLiveInstance(slot, fn)` helper; document the 3 deliberate ended-check omissions (read-only accessors) at the call site.
- Acceptance: runner tests green (with `-race`); controller.go materially shorter.

### Phase 7 — Shared per-slot driver — DEFERRED (post-Phase-L; default: don't)
You cannot evaluate this until twoab's real runner exists: real transport (gossipsub, context-cancellation, goroutines, beacon submit) and simulated transport (virtual clock, deterministic host, synchronous mesh) have different control-flow *and* failure models, and a duplicated orchestration *sequence* is exactly the kind of thing that reads clearer copied than abstracted behind an injection surface. **Default to two drivers.** Revisit only if Phase L's real orchestration sequence turns out to mirror the sim's; if so, share small *helpers* (e.g. the resolve-on-arrival predicate), not the whole orchestration.
- If pursued: the per-slot orchestration is fetch→broadcast→observe→resolve→submit, host-validation drain, opportunistic resolve, cert fast-path — parameterized by transport + host hooks + clock. Acceptance would be: real runner tests + DES scenario suites pass on the shared driver.

### Phase 8 — Harness consolidation (P2 + P6) — last, against frozen goldens
- **Do this last, and never in a commit that also changes protocol code.** Phase 8 refactors the very engine (`des.go`/`byz.go`) that earlier phases lean on as the oracle, so it must validate against the Phase-0 golden fixtures, not against itself.
- Extract `consensustest/desim` (DES engine + transport + byz scaffold) shared by both `obft` and `twoab` harnesses; each harness keeps only protocol-specific event handlers + byz kinds.
- Decompose the `sim` god-object (transport state vs protocol-result state); split the multi-job event handlers (`evtCommitArrival`); replace hand-rolled [sizes.go](../protocol/v2/consensustest/obft/sizes.go) byte accounting with `len(wire.Encode...())`.
- Acceptance: both DES suites reproduce the frozen goldens; harness LOC down sharply.

### Cross-cutting (any time) — comment hygiene
- **Strip dev-history from comments** (current-only): keep comments to current facts + live cross-refs; drop version markers, internal op-codenames, pre/post-X phrasings, and dangling refs to deleted docs. Largely done in the protocol code already; verify completeness across code *and* tests.
- **Doc-comment diet** *(optional, low priority)*: relocate the long spec-derivation essays to the `docs/` spec with anchors; keep code comments to the non-obvious WHY + a doc pointer. **Relocate, don't delete** — traceability is preserved; flagged only because files are 2–4× their logic.

---

## 5. Bears on these deferred questions from the convergence plan

Category E ("open architectural questions, deferred") interacts with this restructure, but two of the four are **Phase-L decisions, not resolved here**:
- **E1 snapshot-at-Finalize** → *still a Phase-L call.* This restructure puts the `Evidence`/`Stats` snapshot helpers in one place, which makes the eventual snapshot-at-Finalize cleaner to land — but the decision itself is runner-facing and waits for Phase L (the convergence plan defers it there deliberately). This plan does **not** resolve E1.
- **E2 unified pool-removal API** → *largely not shareable.* The pools are the divergent center (base's σ-onion `peerOnions`/`peerNR` vs twoab's claim-pools), they stay on `Instance` (Phase 3), and base `removeOnionEntry` vs twoab `removeFromNoValuePool` are different shapes for different reasons (convergence-plan E2). At most an incidental helper shares; the API shape is Phase-L-constrained.
- **E3 cascade error model** → one decision can be applied in the shared core (Phase 3) — runner-agnostic, resolvable here.
- **E4 verifier abstraction** → the shared `Verifier` *core* lands here (Phase 5); *wiring a twoab `Verifier`* is Phase-L work (deferred).

---

## 6. Risks & mitigations

- **R1 — Subtle behavior drift during extraction.** The DES + scenario suites are a strong differential oracle for Phases 1–6 (the scenario *assertions* stay fixed while protocol code moves); extract one subsystem per commit and run the full suite + the Phase-0 goldens each time. Caveat: the oracle is only trustworthy while the harness itself is unchanged — hence Phase 8 (which refactors the engine) runs last and validates against the frozen goldens, never against itself.
- **R2 — Generics vs. interface ergonomics for `EvidenceEngine`.** Prototype both on the evidence engine in Phase 3 and pick before proceeding.
- **R3 — Wire bound reconciliation changes accept/reject behavior.** Phase 4 adds cross-protocol cap tests; nothing externally frozen, so choosing the stricter bound is safe.
- **R4 — Concurrency contract regressions in the Controller.** `withLiveInstance` *centralizes* the contract that's currently copy-pasted; add a `-race` run to the runner tests.
- **R5 — Breaking the production integration surface.** Honor the §2 integration-surface contract: keep `OperatorID`/`Output`/`Evidence`/`EvidenceObserver`/`Verifier`/`wire.*` exported where the node expects them (alias on move). Phases 1–5 must keep `base`'s public façade intact even as internals move.
- **R6 — Scope creep / premature abstraction.** The committed scope is Phases 1–4 + the Phase-5 reconstruction leaves + Phase 6 — all runner-agnostic, and they deliver most of the protocol-side duplication kill plus the one correctness fix (wire drift). Everything runner-facing (Instance interface, twoab `Verifier` wiring, the driver) is deferred until Phase L exists, so the shared seam is derived from twoab's real runner rather than frozen against a non-existent one. Re-justify Phase 5 (sharing) and Phase 8 with real evidence after Phase 4.

---

## 7. Open questions for the author/reviewer

1. **Composition vs. embedding** for the shared subsystems on `Instance` (§2) — explicit delegation (recommended) or promoted methods?
2. **`EvidenceEngine` generics vs. `any` payload** (§2, R2) — leans non-generic now that only the dedup *sink* is shared (it needn't be generic over the payload union); confirm, or decide after the Phase-3 prototype.
3. **Shared-core package boundary**: grow the parent `obft` package directly, or split into sub-packages (`obft/crypto`, `obft/evidence`, …)? Growing `obft` makes a higher-fan-in hub (config + crypto + evidence + ekm + … imported by both protocols + runner + harness) — cohesive, but watch the fan-in. Sub-packages risk *import cycles*: the subs would import `obft` for the shared identity types (`OperatorID`, `Value`, `Signature`), while `obft` would need to import the subs back to re-export/compose them at the integration surface (and `reconstruct.go` uses `CryptoContext`) — that back-edge is the cycle. (Leaning: grow `obft`; revisit if the hub gets unwieldy.)
4. **Stop-point**: the re-sequencing (§4 tranches) makes this concrete — committed scope is Phases 1–4 + Phase-5 leaves + Phase 6; Phase 7 (driver) is deferred/default-don't; Phase 8 (harness) is conditional and last. Confirm this is the intended cut, or pull Phase 8 forward.

---

## 8. Non-goals

- **No deduplication for its own sake.** Any extraction that fails the §3 guardrails (different reasons to change, needs a protocol flag, hurts readability) stays duplicated — that's a successful outcome, not a gap.
- No protocol/semantic change to either consensus. This is a structural refactor; Phase-2 behavior of each protocol is preserved.
- No new external dependencies.
- Not wiring twoab into the production runner (the separate "Phase L" runner-adapter work). This plan makes it *easier* by landing the runner-agnostic shared core first (crypto, evidence mechanics, config, wire, reconstruction, the shared `Verifier` core); the runner-facing pieces it would need (twoab `Verifier` wiring, any shared driver) are deliberately left *to* Phase L, not pre-built here.
- No deletion of spec-traceability content — only relocation if the doc-comment diet is taken up.

# Phase L — SSV runner integration plan

Plan-doc for Phase L of [2abOBFT-IMPL-PLAN.md](2abOBFT-IMPL-PLAN.md): drive `twoab.Instance` from the SSV runner layer, parallel to the existing bare OBFT runner integration. After Phase L, a 4-operator cluster can run a full proposer-duty slot through 2abOBFT end-to-end via the SSV runner harness and produce a certificate.

> **Note**: this plan was originally written against the v1 2abOBFT protocol (Verdict / Onion2b / 5-row convergence). The current `twoab` implementation is the v4 redesign per [docs/2abOBFT-REDESIGN-PLAN.md](2abOBFT-REDESIGN-PLAN.md): `KindValue` / `KindNoValue` / `KindCommit` wire, dynamic Phase 2b with three triggers, no `T_commit` hard wall. This document has been updated to match the v4 API surface; the legacy spec doc [docs/2abOBFT.md](2abOBFT.md) is still v1 (rewrite pending).

## Goal

A `protocol/v2/ssv/runner/obft/twoab/` runner package that:

1. Mirrors the bare OBFT runner's shape (Controller / Scheduler / RunProposerSlot / DispatchEnvelope / rate-limit / verifier) but drives `twoab.Instance` instead of `base.Instance`.
2. Fires a Phase-2a coordination step at `T_phase_2a` (the protocol's fire-instant) — every operator emits one of `KindValue` / `KindNoValue` / `KindCommit-NRDirect` per their local state. **There is no Phase-2a observation window** in v4; the verdict-window concept from v1 is gone (the protocol's Phase 2a is fire-and-listen, with the Phase-2a-late upgrade path open until the slot deadline).
3. **Phase 2b is dynamic** — no `T_commit` hard wall, no scheduled commit-build step. The Instance's `afterStateDelta` cascade auto-fires `MaybeBuildAndBroadcastUpgrade` (Phase-2a-late A1 upgrade) and `MaybeBuildAndBroadcastCommit` (three triggers: equivocation > σ-eligibility > NR-eligibility with cannot-σ gate) on every state delta. The runner's role at Phase 2b is to detect new emissions (`OwnValueMsg` / `OwnCommit` becoming non-nil) and broadcast them.
4. Owns its own envelope dispatcher keyed on the 2ab wire format's `protocol_tag = "2abOBFT"` (no version suffix per the v4 wire format; see [protocol/v2/obft/twoab/wire/](../protocol/v2/obft/twoab/wire)).
5. Plugs into `ProposerRunner` via a runner-side variant switch — operator config picks `base` vs `twoab` at construction time.
6. **Adopts observer-mode Phase 3 from day one** — Phase 3 starts as soon as Phase-2a emissions begin arriving; no schedule-anchored Resolve cadence. Mirrors the bare OBFT runner's pattern after [OBFT-OPPORTUNISTIC-PHASE3-PLAN.md](OBFT-OPPORTUNISTIC-PHASE3-PLAN.md): `Controller` exposes a per-slot `StateDeltaChan(slot)` that fires after every successful `ObserveValueMsg` / `ObserveNoValueMsg` / `ObserveCommit` / `ObserveCertificate`; `ResolveAndSubmitOpportunistically` blocks on it and retries `Resolve` on each delta until σ-quorum reaches or ctx fires. The only hard wall is the slot's relay-submission cutoff (runner-level).

Acceptance criteria from the impl plan:
- **Integration test**: 4-operator cluster runs a full slot via the SSV runner harness with `twoab.Instance`, produces a certificate.
- **Bare OBFT runner unaffected**.
- **Mixed-variant envelope rejected** with a diagnostic log.

## Existing assets I'm modeling on

Bare OBFT runner at [protocol/v2/ssv/runner/obft/](../protocol/v2/ssv/runner/obft) (~3431 LoC):

| File | Role | LoC |
| ---- | ---- | --- |
| [`config.go`](../protocol/v2/ssv/runner/obft/config.go) | `ConfigOverrides`, `ConfigForCluster`, defaults (T_commit derivation, per-layer schedules) | 447 |
| [`controller.go`](../protocol/v2/ssv/runner/obft/controller.go) | `Controller` (per-operator state-machine wrapper), `RunningInstance`, pending-envelope buffer with LRU+endedSlots fence, `BufferEnvelope`/`DrainPending` | 539 |
| [`scheduler.go`](../protocol/v2/ssv/runner/obft/scheduler.go) | `LifecycleHooks`, `Scheduler` (procedural per-phase methods), `iterativeFetch`, `BuildAndBroadcastCommit`, `ResolveAndSubmit*`, `submitAndBroadcastCert` | 538 |
| [`runner.go`](../protocol/v2/ssv/runner/obft/runner.go) | `RunProposerSlot` — single function drives one slot end-to-end | 155 |
| [`dispatch.go`](../protocol/v2/ssv/runner/obft/dispatch.go) | `DispatchEnvelope` (wire → Controller method), `DispatchBytes` (parse + dispatch) | 78 |
| [`ratelimit.go`](../protocol/v2/ssv/runner/obft/ratelimit.go) | Per-(slot, op, content-hash) rate limiter; bounded distinct-per-slot count | 282 |
| [`verifier.go`](../protocol/v2/ssv/runner/obft/verifier.go) | Inner-message claimed signer matches outer SSV signer | 72 |
| [`candidate.go`](../protocol/v2/ssv/runner/obft/candidate.go) | V-on-the-wire encode/decode for proposer-duty (version byte + blinded SSZ) | 49 |
| [`proposer_signer.go`](../protocol/v2/ssv/runner/obft/proposer_signer.go) | BLS signer wrapper used by Controller construction | 101 |

ProposerRunner integration (~921 LoC total):

- [`proposer.go`](../protocol/v2/ssv/runner/proposer.go): `ProposerRunner` struct holds `obftCtrl`, `obftSched`, `obftRL`, per-slot `obftSlots` map; `NewProposerRunner` wires hooks → Scheduler. `OBFTController` is a required field on `ProposerRunnerOptions`.
- [`proposer_obft.go`](../protocol/v2/ssv/runner/proposer_obft.go): the seven lifecycle hooks (`obftFetchCandidate`, `obftHostValidate`, `obftBroadcast`, `obftSubmitOutput`, `obftBroadcastCertificate`, `obftOnMissedSlot`, `obftOnReplayError`) + `obftStartSlot` + `ProcessOBFTEnvelopeMsg`.

## Adapter structure (mirroring base, plus the Phase-2a fire-instant)

Two top-level naming options — answer in Open Questions before I start:

**Option A** (symmetric, more churn): rename `protocol/v2/ssv/runner/obft/` → `protocol/v2/ssv/runner/obft/base/`, add `protocol/v2/ssv/runner/obft/twoab/`. Matches the core split. Touches every `obftadapter` import site (~10 sites in runner/proposer.go + tests).

**Option B** (less churn): keep `protocol/v2/ssv/runner/obft/` in place as the "base" runner, add `protocol/v2/ssv/runner/obft/twoab/` as a sibling. Naming asymmetry is mildly awkward but no existing import sites move.

My preference: **Option B**. Phase A took option A for the core; for the runner the import churn is wider (validator code, ssv runner duty_runners, beacon plumbing — all reach in via `obftadapter`). Option B trades cosmetic asymmetry for a smaller diff.

```
protocol/v2/ssv/runner/obft/twoab/
  config.go              # ConfigForCluster builds *twoab.Config (TPhase2a + SafetyBuffer instead of TCommit + Delta2)
  controller.go          # Controller wrapping twoab.Instance
  scheduler.go           # LifecycleHooks + Scheduler with new FirePhase2a step (single fire-instant)
  runner.go              # RunProposerSlot with Phase-2a fire-instant + opportunistic Phase 3
  dispatch.go            # DispatchEnvelope for 2ab wire kinds (KindPhase1Bundle / KindValue / KindNoValue / KindCommit / KindCertificate)
  ratelimit.go           # Per-(slot, op, kind, content-hash) — same shape as base
  verifier.go            # Inner-signer-matches-outer (re-implemented; uses 2ab envelope types)
  candidate.go           # Same V encoding as base (version byte + blinded SSZ) — no change
  proposer_signer.go     # BLS signer wrapper — same shape as base
  config_test.go
  controller_test.go     # smoke + concurrency
  dispatch_test.go       # mixed-variant rejection
  runner_test.go         # end-to-end 4-operator slot
  ratelimit_test.go
```

### LifecycleHooks (deltas vs base)

| Hook | Bare OBFT | 2abOBFT |
| ---- | --------- | ------- |
| `FetchCandidate(ctx, slot, layer) ([]byte, error)` | same | same |
| `HostValidate(ctx, slot, layer, value) (bool, error)` | called once per receiver-side Observe | called at multiple points: Phase-1 bundle observe time, Phase-2a fire-time, and Phase-2b commit-eval time (host re-check is what lets the σ-eligibility trigger's side-decision pivot to A3/A4 if host has flipped). Same signature, called multiple times per (slot, layer, V). |
| `Broadcast(ctx, slot, data) error` | wire-envelope-wrapped bytes (one hook, all kinds) | **same hook, no separation needed**. Phase 1 / Phase-2a (Value/NoValue/Commit-NRDirect) / Phase-2b (Commit-Signed/NR) / Phase-2a-late upgrade / Certificate all hand pre-wrapped bytes to `Broadcast`. |
| `SubmitOutput(ctx, slot, output) error` | same signature, `output` is `*base.Output` | same signature, `output` is `*twoab.Output` (structurally identical: Layer + Value + Signature) |
| `BroadcastCertificate(ctx, slot, data) error` | optional | same |
| `OnMissedSlot(ctx, slot, reason)` | optional | same |
| `OnReplayError(ctx, slot, kind, err)` | optional | `kind` is `twoab/wire.MessageKind` (own type, parallel to base's) |

### Runner timing flow (deltas vs base)

```
Slot start                                  T_0_broadcast                T_phase_2a                                              relay-submission deadline
   │                                                │                            │                                                       │
   │                                                │                            │                                                       │
   ├─ Phase 1: leader-fetch goroutines ─────────────┤                            │                                                       │
   │  per layer: FetchAndBroadcastBundle            │                            │                                                       │
   │  (V_0 has 1·BTT to propagate before fire)      │                            │                                                       │
   │                                                │                            │                                                       │
   │                                                ├── FirePhase2a (one-shot) ──┤                                                       │
   │                                                │  every op emits one of:    │                                                       │
   │                                                │   - KindValue              │                                                       │
   │                                                │   - KindNoValue            │                                                       │
   │                                                │   - KindCommit-NRDirect    │                                                       │
   │                                                │                            │                                                       │
   │                                                │                            ├── Phase-2a-late upgrade window (A1) ─────────────────  ┤
   │                                                │                            │  KindNoValue-path ops who receive V_0 + host valid    │
   │                                                │                            │  emit upgrade KindValue                                │
   │                                                │                            │                                                       │
   │                                                │                            ├── Phase-2b dynamic commits (cascade-driven) ─────────  ┤
   │                                                │                            │  σ-eligibility / NR-eligibility / equivocation        │
   │                                                │                            │  triggers fire as pools fill                          │
   │                                                │                            │                                                       │
   │                                                │                            ├── ResolveAndSubmitOpportunistically ─────────────────  ┤
   │                                                │                            │  (observer-mode: blocks on Controller.StateDeltaChan; │
   │                                                │                            │   wakes per Observe* and Phase2a-fire; retries        │
   │                                                │                            │   Resolve until σ-quorum or relay deadline)           │
```

Key differences from v1's timing flow:
- **No T_verdict_start / T_verdict_max window**. Post-Op6, `T_phase_2a = T_0_broadcast + 1·BTT` is a **backstop**, not the primary fire-instant: the runner SHOULD async-fire `MaybeFirePhase2a` early by selecting on `inst.L0ReadyCh()` — closed once the op's L_0 emission is determinable (σ-eligible `KindValue` or equivocation-observed `KindCommit-NRDirect`) — and fall to the synchronized `T_phase_2a` backstop only if L0Ready never closes (the NoValue path: a V-drop / host-NV op waits for the backstop so V keeps its reflood window, then emits `KindNoValue`). Mirror the DES's `maybeEarlyFire` ([consensustest/twoab/events.go](../protocol/v2/consensustest/twoab/events.go)).
  - **Leader L0Ready subtlety (production-runner requirement):** `BuildPhase1Bundle` does NOT close the leader's L0Ready — twoab is retention-driven, not σ-lock-driven (unlike base, where the build itself signals via `sigmaLocked[0]`). The runner MUST self-feed the leader's own bundle through `ObservePhase1Bundle` + `ApplyHostValidity` at fetch time (as the DES does at events.go:249-262) so the leader's L0Ready closes and it async-fires alongside peers; otherwise the leader silently falls to the `T_phase_2a` backstop (a healthy-path latency regression, not a correctness bug).
- **No T_commit hard wall**. Phase 2b emissions are dynamic, driven by trigger evaluation inside the protocol's `afterStateDelta` cascade. The runner doesn't schedule a Phase-2b build step.
- **No Δ_2b propagation budget**. Phase 3 starts as soon as Phase-2a emissions begin arriving; the only hard deadline is the runner-level relay-submission cutoff.
- **The runner detects new own-emissions after each peer arrival**. After every `Observe*` call (or `MaybeFirePhase2a`), check `inst.OwnValueMsg()` / `inst.OwnNoValueMsg()` / `inst.OwnCommit()`; broadcast any newly-set values (cascade may have fired an upgrade or a commit).

### Envelope dispatch (deltas vs base)

`DispatchEnvelope` in twoab switches on `wire.MessageKind` from the 2ab wire format:

| Wire kind | Controller method |
| --------- | ----------------- |
| `KindPhase1Bundle` (0x01) | `HandlePeerPhase1Bundle` (mirrors base) |
| `KindValue` (0x02) | `ProcessValueMsg` (new) — calls `Instance.ObserveValueMsg` |
| `KindNoValue` (0x03) | `ProcessNoValueMsg` (new) — calls `Instance.ObserveNoValueMsg` |
| `KindCommit` (0x04) | `ProcessCommit` (replaces base's `ProcessCommit`, but now handles Side flag: Signed / NR / NRDirect) |
| `KindCertificate` (0x05) | `ProcessCertificate` (mirrors base) |

Pending-envelope buffer handles all five kinds — buffer schema gets `ValueMsg` / `NoValueMsg` / `Commit` fields alongside `Bundle` / `Certificate`. Pre-instance replay drains them in arrival order.

### Cross-variant rejection

Already enforced at the wire layer by Phase C's protocol tag check (`wire.Unwrap` rejects mismatched tags). The 2ab dispatcher's `DispatchBytes` calls `twoab/wire.Unwrap`, which returns an error on a bare OBFT envelope (different tag — bare OBFT's tag is `"OBFT"`, 2ab's is `"2abOBFT"`). The bare OBFT dispatcher's `base/wire.Unwrap` symmetrically rejects 2ab envelopes.

Adding the test: bare-runner-receives-2ab-envelope → drops with `wire: protocol tag mismatch`. Already covered by [protocol/v2/obft/twoab/wire/wire_test.go](../protocol/v2/obft/twoab/wire/) `TestDomainSeparation_*` tests; the new runner-level test asserts the rejection propagates as a `ProcessOBFTEnvelopeMsg` error (not silent).

## ProposerRunner integration

This is the most invasive part of Phase L — `ProposerRunner` currently hard-codes the bare OBFT runner via `OBFTController *obftadapter.Controller`. Two approaches:

**Approach 1 — Variant-polymorphic ProposerRunner (single struct, internal switch)**:
- Add `TwoabController *twoabadapter.Controller` to `ProposerRunnerOptions`. Exactly one of `OBFTController` / `TwoabController` must be non-nil.
- `ProposerRunner` carries either an `*obftadapter.Controller`+`Scheduler` OR a `*twoabadapter.Controller`+`Scheduler`. All `obft*` methods grow a variant-switch.
- Pros: one struct, no validator-side branching.
- Cons: every `obftStartSlot` / `obftFetchCandidate` / `obftBroadcast` / etc. method body gains an if-else; lifecycle hook implementations may need a re-validate-via-twoab branch.

**Approach 2 — Per-variant ProposerRunner (separate struct, factory dispatch)**:
- Introduce `TwoabProposerRunner` that mirrors `ProposerRunner` with `twoabadapter` fields. Lifecycle hooks (`twoabFetchCandidate` etc.) are 1:1 mirrors of `obft*`.
- `NewProposerRunner` factory looks at the operator-local variant flag, returns either runner.
- Pros: cleaner separation per the user's earlier directive ("prefer separation; OK to duplicate"); no branching inside hooks; tests run independently.
- Cons: lots of duplication. `proposer.go` doubles in size (~470 lines mirrored). Validator-side changes minimal — both runners implement the same `Runner` interface.

My preference: **Approach 2** with a shared helper file for the parts that genuinely don't differ (e.g., RANDAO handling, slashing-protection calls, version-decode bookkeeping). Lifecycle hooks themselves are nearly identical bodies (a 2-line change for the Instance/Controller type); a small `proposer_obft_common.go` factor preserves DRY without re-introducing branching everywhere.

**Open question for the user**: Approach 1 or 2? I'll proceed with Approach 2 unless told otherwise.

### Variant selection wiring

Per the impl plan: hardcoded operator-local config flag at runner-init. Two natural injection points:

a. **Operator config** (operator-wide): one flag for the whole operator, e.g. `consensus_variant: "twoab"` in the operator's config file. Simplest to implement — read once at startup, pass through to all duty runners.

b. **Per-cluster config** (per-validator share): the variant lives on each `SSVShare` (or computed from cluster metadata). Allows mixed-variant operation across clusters on one operator node. More configuration ceremony but matches the spec's "per-cluster opt-in" framing better.

My preference: **(a) operator-wide** for Phase L; **(b) per-cluster** is a Phase M concern (the rollout phase). Operator-wide is the simplest path to the acceptance criterion ("a cluster can be configured as `twoab` variant; SSV node starts cleanly with twoab.Instance") and we don't yet need mixed-variant on one node. Phase M can refine to per-cluster if needed.

## Sequencing

Single Phase L commit, staged execution. (Per Phase K precedent — bigger commit with a coherent narrative + integration test rather than thin slice commits.)

| Step | Output | Verification |
| ---- | ------ | ------------ |
| L1 | `twoab/config.go` + `proposer_signer.go` + `candidate.go` — config / signing / candidate-encoding helpers (mostly 1:1 mirrors of base, but ConfigForCluster derives `TPhase2a` + `SafetyBuffer` from BTT + RelayCutoff + HeaderSubmitHeadroom). | Builds; ConfigForCluster smoke test passes; defaults match impl-plan operating point (TPhase2a positioned so T_0_broadcast = TPhase2a − BTT > 0; SafetyBuffer default = RefloodDelay). |
| L2 | `twoab/controller.go` — Controller + RunningInstance + pending-envelope buffer + lookup helpers. New `ProcessValueMsg`, `ProcessNoValueMsg`, `ProcessCommit` methods alongside existing patterns. Pending buffer gains `ValueMsg` / `NoValueMsg` / `Commit` slots. | Builds; controller_test exercises BuildPhase1Bundle / ObservePhase1Bundle / MaybeFirePhase2a / ObserveValueMsg / ObserveNoValueMsg / ObserveCommit / Resolve via the controller surface. |
| L3 | `twoab/scheduler.go` — LifecycleHooks + Scheduler with new `FirePhase2a(slot)` method (single fire-instant). **No BuildAndBroadcastCommit method** — Phase-2b emissions are dynamic; instead, after every Observe* (and the FirePhase2a call) the scheduler checks `inst.OwnValueMsg() / OwnCommit()` for newly-fired emissions and broadcasts them. | Builds; scheduler_test exercises FirePhase2a + the post-Observe new-emission detection path. |
| L4 | `twoab/runner.go` — `RunProposerSlot` with the new timing flow: per-layer fetch goroutines + FirePhase2a timer + (no Phase-2b timer — dynamic) + Resolve loop (observer-mode). | Builds; smoke test: 4-operator in-process cluster runs a healthy slot end-to-end. |
| L5 | `twoab/dispatch.go` + `twoab/ratelimit.go` + `twoab/verifier.go` — envelope routing + rate limit (KindValue / KindNoValue / KindCommit each get their own bucket) + inner-signer check | Builds; dispatch_test covers each wire kind + cross-variant rejection. |
| L6 | `twoab/runner_test.go` end-to-end integration test (full slot, 4 operators, real BLS keys, asserts certificate). | Test passes; produces a valid certificate; safety invariants hold. |
| L7 | `protocol/v2/ssv/runner/twoab_proposer.go` (new file) + extracted `proposer_common.go` (RANDAO + slashing + version-decode helpers shared by both runners). Refactor `proposer.go` to call into the shared file. | Existing OBFT proposer tests pass; new twoab proposer is constructed by the factory; integration test (L6) drives through the runner not just the adapter. |
| L8 | `duty_runners.go` factory: read operator-local `consensus_variant` flag, construct `ProposerRunner` or `TwoabProposerRunner` per validator share. | Factory smoke test. End-to-end test in node-startup path with a `twoab` cluster. |
| L9 | Telemetry: extend [observability.go](../protocol/v2/ssv/runner/observability.go) with 2ab-specific counters (Phase-2a emissions by kind: Value/NoValue/Commit-NRDirect; A1 upgrade fires; per-trigger commit emissions: σ-eligibility / NR-eligibility / equivocation; Rule 6a Phase2Equivocation detections). | observability_test.go assertions on counters. Hand-trace the integration test to confirm counters fire on the expected paths. |
| L10 | Self-review pass; `twoab-impl.md` deltas; commit. | Clean diff; full repo `go test ./...` green; gofmt + vet clean. |

Between L6 and L7, the adapter itself can be exercised stand-alone (without ProposerRunner) — this is the "adapter works" milestone. L7-L8 cross the validator-integration line.

## Files outside the runner package that get touched

- `protocol/v2/ssv/runner/proposer.go` — Approach 2: refactor `ProposerRunner` to a `BaseProposerRunner` (the bare-OBFT version), introduce `TwoabProposerRunner` alongside. The Runner interface stays the same — both new types satisfy it.
- `protocol/v2/ssv/runner/duty_runners.go` — factory branches on variant.
- `protocol/v2/ssv/runner/observability.go` — add 2ab counters (`twoab_phase2a_emit_total{kind}`, `twoab_upgrade_emit_total`, `twoab_commit_emit_total{trigger}`, `twoab_evidence_rule{rule}`).
- Operator config file (`config/config.go` or wherever) — add `ConsensusVariant string` field with default `"base"`. The exact location depends on existing convention; I'll grep and pick.
- Validator-side dispatch: `ProcessOBFTEnvelopeMsg` (in proposer_obft.go) becomes variant-aware OR a separate `ProcessTwoabEnvelopeMsg` handler is added — depends on Approach 1 vs 2.

## Out of scope (deferred to Phase M or follow-up)

- **Per-cluster variant selection** (vs operator-wide). Phase M wires this if needed; for Phase L, operator-wide is enough.
- **On-chain variant registry**. Out of scope per G7.
- **Live cluster migration** (operators flipping variants mid-cluster). Out of scope per Phase M-style coordination model.
- **Benchmarks**. Optional per impl plan; defer to a follow-up if perf becomes a question.
- **Operator-facing rollout docs**. Phase M's job; per G10 we don't write them.

## Open questions for the user

1. **Directory naming**: Option A (rename `obft/` → `obft/base/` + add `obft/twoab/`, symmetric) or Option B (keep `obft/`, add `obft/twoab/` as sibling, less churn)?
2. **Runner construction**: Approach 1 (single ProposerRunner with internal variant switch) or Approach 2 (separate TwoabProposerRunner)?
3. **Variant injection point**: operator-wide flag (simpler) or per-cluster flag (matches spec framing but more config ceremony)?
4. **Host re-validation at multiple phases**: call existing `HostValidate` hook multiple times (my preference — the hook's idempotency contract already accommodates this) or add separate `HostReValidate*` hooks for the Phase-2a-fire / Phase-2b-commit re-check points?
5. **Single commit vs split**: Phase K shipped as one big commit (~2100 LoC). Phase L is similar scope (~2500-3000 LoC). One commit, or split L1-L6 (adapter) + L7-L10 (integration)?
6. **Tests with real BLS**: Phase K shipped stub-only via the consensustest adapter. The proposer runner uses real BLS in production; integration tests should too. Confirm we want a real-BLS integration test (uses [protocol/v2/obft/blsbackend/](../protocol/v2/obft/blsbackend)) — it's slower but exercises the actual signing path.

## Effort estimate

| Step | Effort |
| ---- | ------ |
| L1 — config / signer / candidate | 2h |
| L2 — controller (new ProcessValueMsg / ProcessNoValueMsg / ProcessCommit) | 4h |
| L3 — scheduler (FirePhase2a + post-Observe emission detection) | 4h |
| L4 — runner.go + smoke test | 3h |
| L5 — dispatch + ratelimit + verifier | 3h |
| L6 — end-to-end runner integration test | 3h |
| L7 — TwoabProposerRunner + shared helpers refactor | 4h |
| L8 — duty_runners factory + node-startup integration | 2h |
| L9 — telemetry | 2h |
| L10 — self-review + impl-md updates | 2h |
| **Total** | **~3-4 days** |

Matches the impl-plan estimate.

## Open architectural questions surfaced by obft↔twoab convergence

Logged from `docs/OBFT-TWOAB-CONVERGENCE-PLAN.md` §Category E. These design questions touch the Instance ↔ Runner contract and should be resolved as part of the Phase L runner adapter design rather than during Instance-layer convergence.

### E1. Snapshot-at-Finalize design

Today both `base.Instance` and `twoab.Instance` allow internal state to keep mutating post-Finalize (Observe* methods can fire, evidence can accumulate). The `ErrInstanceEnded` guards on public mutators (added in convergence Phase 3) catch the *intended* path, but the deeper concern is: the runner snapshots `Evidence()` / `RetainedBundles()` / `Stats()` at Finalize-time and expects them frozen. If a late dispatch slips through, those snapshots diverge from the live Instance.

The principled fix is snapshot-at-Finalize: have Finalize capture the relevant snapshots into freezable members and have the accessors return the frozen view post-Finalize.

**Decision required during Phase L**: does the runner dispatch loop need this contract, or is the current "guards + runner-drains-before-Finalize" discipline sufficient?

### E2. Pool-removal API unification

Twoab has `removeFromNoValuePool` (load-bearing for A1 upgrade: KindNoValue contribution removed when same op later upgrades). Base has `removeOnionEntry` (load-bearing for Rule 5 cryptoFake drop: poison-σ entry retroactively erased on bundle arrival). Different shapes, different reasons.

**Decision required during Phase L** (if it touches pool internals): should the underlying pool abstraction grow a uniform `remove(op, layer, kind, vRoot)` API, or is the per-package surface fine?

### E3. Cascade-error async model

Twoab stores `cascadeErrors []error` capped at 100 entries; surfaces via `CascadeErrors()` accessor. Base has no cascade today and no analog. A pathological always-failing signer would hit twoab's cap; the cap is silent (no error escapes), only the `Stats().CascadeErrorsCapped` flag surfaces.

**Decision required during Phase L runner integration**: the runner needs cascade-error visibility for production observability. Is the current poll-via-CascadeErrors() pattern sufficient, or should errors be channeled out via an observer-style callback (matching `EvidenceObserver`)? The latter would let the runner log/alert immediately rather than at slot end.

### E4. Verifier abstraction for twoab

Base has `verify.go` (~204 LoC, `Verifier` type with `VerifyPhase1Bundle` / `VerifyCommitNRPartials` / `VerifyCommitWitnesses` / `VerifyCertificate` methods) used by the bare-OBFT SSV runner to verify messages before dispatch into the Instance. Twoab has no equivalent.

**Required as part of Phase L**: build the parallel `twoab/verify.go` mirroring `base/verify.go`'s shape but for the twoab message kinds (Phase1Bundle, ValueMsg, NoValueMsg, Commit, Certificate). The verifier sits between the SSV envelope-verify boundary and the Instance's per-method observation calls.

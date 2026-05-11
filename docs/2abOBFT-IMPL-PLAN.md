# 2abOBFT Implementation Plan

**Status**: PLAN — locked in. Ready for Phase A execution.

## Locked-in decisions

All decisions below have been resolved through review iteration. Plan-body sections (below) reflect these answers.

### Architecture directive

- **Prefer separation** between bare-OBFT and 2abOBFT. OK to duplicate code between protocols; reuse only **super-generic cryptography primitives** (BLS signer/IBE interfaces + impls + tag derivation + bare type aliases).
- **Each protocol owns its own data structs**, including wire format, Config, validation, evidence types, and EKM coordinator.

### Q&A resolutions

| # | Question | Decision |
|---|---|---|
| Q1 | Phase1Bundle sharing | Per-protocol — each protocol's `messages.go` declares its own |
| Q2 | PhaseAState durable backend | N/A — no durable PhaseAState in initial impl (see G8 below) |
| Q3 | Shared Phase-3 walk helper | Per-protocol — walk algorithm duplicated; only cryptographic primitives shared |
| Q4 | Runner architecture | Parallel sub-packages |
| Q5 | Cross-protocol Instance interface | Defer indefinitely — each Instance is its own type |
| Q6 | MessageKind byte assignments | Each protocol owns its own wire envelope + MessageKind enum (byte values can even collide between protocols since envelopes are namespaced) |
| Q7 | Per-cluster variant selection | Hardcoded operator-local config flag; no on-chain registry in scope |
| Q8 | EKM coordinator scope | Per-protocol EKM coordinator (uses shared Signer interface at the bottom); EKM coordinator owns its own key prefix in the shared underlying BadgerDB |
| Q9 | Treat-equivocator-as-null timing | Apply at convergence-eval (start of Phase 2b, = T_commit) — per spec [2abOBFT.md:172](2abOBFT.md#L172) |
| Q10 | Full EKM-binding of verdicts | Out of scope. SHOULD-level treat-as-null is the partial close |
| G1 | n=7 validity-divergence in tests | Add to Phase J |
| G2 | Domain-separation tests | Phase C (when wire types ship) |
| G3 | Mixed-variant cluster | Reject + diagnostic logs + wire-rejection tests in Phase C |
| G4 | Telemetry / metrics | Lightweight surface in Phase L; defer dashboards |
| G5 | Benchmarks | Fold into Phase L (optional) |
| G6 | `docs/twoab-impl.md` delta catalog | Create empty template in Phase B; accrete through E-L |
| G7 | `Config.Validate()` | Per-protocol — `base.Config.Validate()` and `twoab.Config.Validate()` |
| G8 | Mid-slot crash recovery | **Out of scope per spec** — assumption 5 simplified ([2abOBFT.md:79](2abOBFT.md#L79)): crashed operators are silent for the slot; in-slot Phase-2a state is in-memory only |
| G9 | Slashing transaction filing | Out of scope; logging + manual review only |
| G10 | Operator-facing docs | Out of scope |

### Spec changes already landed in this prep work (separate from impl phases)

- [docs/2abOBFT.md](2abOBFT.md) assumption 5 simplified — "in-slot operator restart is out of scope; crashed operators silent for the slot"
- [docs/2abOBFT.md §EKM coordination model](2abOBFT.md#ekm-coordination-model) trimmed — removed the "Beyond the EKM log: persistent Phase-2a state" requirement
- [docs/2abOBFT-design-notes.md](2abOBFT-design-notes.md) open question #4 marked superseded

## Goal

Implement 2abOBFT faithfully against [docs/2abOBFT.md](2abOBFT.md), reusing the existing bare-OBFT machinery in [protocol/v2/obft/](../protocol/v2/obft/) as much as possible without entangling the two protocols' state machines. Land in incremental, individually-reviewable PRs that don't regress bare OBFT.

## Architecture directive (locked in)

**Prefer separation.** OK to duplicate protocol logic between bare OBFT and 2abOBFT. Reuse only **super-generic cryptography primitives**:

- Type aliases (`OperatorID`, `Value`, `Signature`, `Height`) — bare primitives
- `Signer` interface + `StubSigner` — BLS partial-signing surface
- `ThresholdIBE` interface + `StubIBE` — IBE encrypt/decrypt surface
- `NoQuorumTag` function — pure deterministic tag derivation
- `blsbackend/` — BLS impl + tlock IBE bindings

**Per-protocol** (each protocol owns its own copies):
- Wire envelope + `MessageKind` enum (each protocol's `wire/` sub-package; byte values can collide across protocols)
- `Config`, `LayerSpec`, `Config.Validate()`
- `Phase1Bundle`, `Commit`/`Onion2b`/`Verdict`, `Certificate`, `Output` data structs
- Evidence types and rules — even where rules conceptually overlap (1-5 in both), each protocol declares its own evidence types
- Instance state machine + per-phase methods
- Validation, verification, EKM coordinator implementation
- Phase-3 reconstruction walk algorithm (only the cryptographic primitives it calls are shared)

**Cross-protocol Instance interface**: deferred indefinitely. Each Instance is its own type; no shared interface.

**Runner layer**: parallel sub-packages mirroring the protocol packages.

### Directory layout (target)

```
protocol/v2/obft/
├── shared.go             # SHARED: type aliases (OperatorID, Value, Signature, Height)
├── signer.go             # SHARED: Signer interface + StubSigner
├── ibe.go                # SHARED: ThresholdIBE interface + StubIBE
├── tag.go                # SHARED: NoQuorumTag
├── blsbackend/           # SHARED: BLS + tlock IBE impl
│
├── base/                 # bare OBFT — owns everything else
│   ├── config.go         # base.Config, LayerSpec, Validate
│   ├── messages.go       # Phase1Bundle (with SigmaV), Commit, Certificate, Output
│   ├── wire/             # bare-OBFT wire envelope + MessageKinds (independent enum)
│   │   └── envelope.go
│   ├── evidence.go       # bare-OBFT Evidence + Rules 1-5
│   ├── instance.go
│   ├── phase1.go, phase2.go, phase3.go
│   ├── validation.go, verify.go
│   ├── ekm.go            # bare-OBFT EKM coordinator
│   ├── errors.go
│   └── *_test.go
│
└── twoab/                # 2abOBFT — owns everything else
    ├── config.go         # twoab.Config (Δ_2a, Δ_2b, T_verdict_start anchor; no Δ_2), Validate
    ├── messages.go       # Phase1Bundle (no SigmaV), Verdict, Onion2b, Certificate, Output
    ├── wire/             # 2ab wire envelope + MessageKinds (independent enum)
    │   └── envelope.go
    ├── evidence.go       # 2ab Evidence + Rules 1-5 + 6a + 6b
    ├── instance.go
    ├── phase1.go         # 2ab Phase 1 (no σ_V partial)
    ├── phase2a.go        # 2ab Phase 2a (verdict broadcast)
    ├── phase2b.go        # 2ab Phase 2b (σ-or-NR commit)
    ├── phase3.go         # 2ab reconstruction (algorithm duplicated)
    ├── convergence.go    # 5-row convergence decision table
    ├── validation.go, verify.go
    ├── ekm.go            # 2ab EKM coordinator (own key prefix in shared BadgerDB)
    ├── errors.go
    └── *_test.go
```

Runner layer parallels this:

```
protocol/v2/ssv/runner/obft/
├── base/                 # bare-OBFT runner (existing code moved)
└── twoab/                # 2abOBFT runner (new)
```

## Shared primitives — explicit inventory

Both protocols import from top-level `protocol/v2/obft/`. Strictly minimal set.

| Item | Source location | Notes |
|---|---|---|
| `OperatorID`, `Height`, `Value`, `Signature` | `shared.go` | Pure type aliases |
| `Signer` interface, `StubSigner` | `signer.go` | BLS partial-signing surface |
| `ThresholdIBE` interface, `StubIBE` | `ibe.go` | IBE encrypt/decrypt surface |
| `NoQuorumTag()` | `tag.go` | Deterministic tag derivation; 2ab uses identical construction |
| `blsbackend/` impl | unchanged | BLS + tlock IBE bindings |

All other types currently in top-level `protocol/v2/obft/` (Config, LayerSpec, Phase1Bundle, EncryptedLayer, NRPartial, LeaderSigmaWitness, Commit, Certificate, Output, CommitState, Evidence, MessageKind, envelope) **move to `base/`** as part of Phase A. 2ab declares its own parallel versions in Phase B-E (with structural differences noted per the spec).

## Phase A — Refactor bare-OBFT into `base/` sub-package

**Goal**: Move bare-OBFT code into `protocol/v2/obft/base/` with no behavior change. All existing tests pass after.

**Steps**:
1. Create `protocol/v2/obft/base/` directory.
2. Move bare-OBFT-specific files into `base/` (everything except the strictly shared cryptography primitives — see §Shared primitives inventory). Update each file's package declaration from `package obft` to `package base`.
3. Items moving to `base/`: Config, LayerSpec, Phase1Bundle, EncryptedLayer, NRPartial, LeaderSigmaWitness, Commit, Certificate, Output, CommitState, Evidence + rule constants, MessageKind enum, envelope, validation, verify, all phase methods, all bare tests.
4. Items staying at top-level `protocol/v2/obft/`: type aliases (`OperatorID`, `Value`, `Signature`, `Height`), `Signer` + `StubSigner`, `ThresholdIBE` + `StubIBE`, `NoQuorumTag`, `blsbackend/`.
5. Rename top-level `types.go` to `shared.go` for clarity (only the bare type aliases remain there); other type definitions move with their owners.
6. Update internal references: in `base/`, qualify shared primitives as `obft.OperatorID`, `obft.Signer`, etc.
7. Update consumers of `protocol/v2/obft/`:
   - `protocol/v2/ssv/runner/obft/` — change imports from `obft` to `obft/base` for moved types. (Move runner code into `runner/obft/base/` if doing it in one pass; otherwise update imports and split runner in Phase L.)
   - `protocol/v2/consensustest/obft/` — same.
   - Tests in these packages.
8. Run `go build ./...` + `go test ./protocol/v2/...` + `go vet ./...` + `gofmt -l`. All must pass.

**Acceptance criteria**: Bare-OBFT tests pass unchanged. No behavior change. Existing consensustest catalog scenarios still run.

**Effort**: 1 PR. Mostly mechanical (file moves + import updates), but touching every test file. ~1-1.5 days.

**Risks**: Import cycles if shared/per-protocol split is unclean. Mitigation: do a dry-run grep for cross-references before moving anything.

**Sub-decision flagged for Phase A start**: split runner now (move `runner/obft/` → `runner/obft/base/`) or defer to Phase L? Recommendation: defer — keep Phase A scope tight; runner split happens once `twoab` runner is built.

## Phase B — Create `twoab/` package skeleton + Config + `docs/twoab-impl.md`

**Goal**: `protocol/v2/obft/twoab/` compiles as an empty package with stub Instance, Config, and skeleton files. No protocol behavior yet.

**Steps**:
1. Create `protocol/v2/obft/twoab/` directory with `package twoab`.
2. `twoab/config.go`: define `twoab.Config` from scratch — fields: `K`, `BTT`, `Delta_2a`, `Delta_2b`, `Delta_3`, `T_verdict_start`, `Layers []LayerSpec`, etc. Aligned with spec §Setting. **No embedding** of `obft.Config` — owned outright.
3. `twoab.Config.Validate()` enforcing spec constraints: `K ≥ max(2, f+1)`, recommended `K ≥ f+2`, `Δ_2a ≥ 2 BTT` (broken-by-construction at minimum), `qV = qEnc = 2f+1`, etc.
4. `twoab.LayerSpec` declared separately (same shape as `base.LayerSpec` but separately owned).
5. `instance.go` stub with `Instance` struct (no fields filled in yet, just placeholder + `NewInstance` signature returning not-yet-implemented sentinel).
6. `errors.go` stub for protocol-specific errors.
7. Create empty `docs/twoab-impl.md` template (delta catalog — mirror of `docs/obft-impl.md`).
8. Confirm runner integration is untouched in this phase (runner gets touched in Phase L).

**Acceptance criteria**: `go build ./protocol/v2/obft/twoab/...` succeeds. `twoab.Config.Validate()` unit tests cover the spec constraints. Bare-OBFT tests still pass.

**Effort**: ~1 day (validate constraints add real work).

## Phase C — 2abOBFT wire types + domain-separation tests

**Goal**: Define 2abOBFT's complete wire format. Independent enum; no envelope shared with `base`.

**Files**:
- `twoab/messages.go`:
  - `Phase1Bundle` struct: `{ClusterID, OperatorID, Height, Layer, Value}` — **no `SigmaV` field** (Variant C).
  - `Verdict` struct: `{ClusterID, OperatorID, Height, Layer, VerdictKind (σV/NR/NV), ValueRoot}` — op-identity signed at the envelope layer.
  - `Onion2b` struct: `{ClusterID, OperatorID, Height, Layers[]EncryptedLayer, NRPartials[]NRPartial}`.
  - `Certificate` struct (own copy, structurally identical to `base.Certificate`).
  - `Output` struct (own copy).
  - `EncryptedLayer`, `NRPartial` (own copies; same shape as `base/` versions).
- `twoab/wire/envelope.go`:
  - `MessageKind` enum (independent of `base/wire`): KindPhase1Bundle, KindVerdict, KindOnion2b, KindCertificate. Byte values can collide with `base/wire` since envelopes are namespaced.
  - `Envelope` discriminated union.
  - `protocol_tag = "2abOBFT-v1"` field on every envelope.
  - `Wrap`/`Unwrap` functions.
- `twoab/validation.go`:
  - `ValidatePhase1Bundle`, `ValidateVerdict`, `ValidateOnion2b`, `ValidateCertificate`.
- **Domain-separation tests** (G2): explicit cross-protocol-rejection tests. A `base/wire`-encoded envelope must fail `twoab/wire.Unwrap` and vice versa. Adds confidence that mixed-variant clusters fail loudly.

**Acceptance criteria**: Round-trip Wrap → Unwrap → equality for each kind. Domain-separation tests pass (bare envelope rejected by twoab decoder; twoab envelope rejected by bare decoder).

**Effort**: ~1.5 days.

## Phase D — (removed)

Phase D originally covered durable `PhaseAState` persistence. Per locked-in decision G8, **mid-slot operator restart is out of scope per spec assumption 5**. In-slot state lives on the Instance struct in memory; no separate persistence interface needed.

The work that would have been Phase D collapses into Phase E (Phase-1 retention) and Phase F (Phase-2a verdict state) — both are now plain in-memory fields on `twoab.Instance`, no persistence layer.

## Phase E — Phase 1 (candidate broadcast, no σ_V)

**Goal**: Implement 2abOBFT Phase 1 per spec §Phase 1.

**Files**:
- `twoab/phase1.go`:
  - `BuildPhase1Bundle(layer, value)` — fills `Phase1Bundle` (no SigmaV field exists). **No** EKM σ-side commitment here.
  - `ObservePhase1Bundle(b, observedOffset)`:
    - Two distinct V retention per `(slot, layer, leader)` (in-memory map on Instance).
    - Late-bundle handling: bundles first-observed past `T_accept_max = T_commit − 1 BTT` go into **auth-only retention** (not verdict-eligible). Spec §Phase 1.
    - Second distinct V → Rule 2 (leader equivocation) evidence.
- All retention state in-memory on Instance (no persistence per G8).

**Differences from bare OBFT**:
- `Phase1Bundle` has no `SigmaV` field at all (not just nil — the field doesn't exist).
- No EKM σ-lock at `BuildPhase1Bundle`.
- Late-bundle acceptance window: `T_accept_max = T_commit − 1 BTT`.
- Auth-only-retained bundles distinguished from accept-eligible.

**Acceptance criteria**: Unit tests covering:
- Healthy bundle observation + retention.
- Late-bundle auth-only retention.
- Leader equivocation → Rule 2 evidence.
- 2-distinct-V retention cap.

**Effort**: ~1.5 days.

## Phase F — Phase 2a verdict broadcast

**Goal**: Implement Phase-2a per spec §Phase 2a.

**Files**:
- `twoab/phase2a.go`:
  - `ComputeLocalVerdict(layer) VerdictKind` — per spec, applies the 4-case rule (equivocation observed → NR; 1V retained + host-valid → σV; 1V + host-invalid → NV; 0V → NR). Calls into the host-validity hook (own copy in `twoab/`, no shared helper).
  - `BuildVerdict(layer) (*Verdict, error)` — wraps `ComputeLocalVerdict` output in a `Verdict` struct; op-identity signed at the envelope layer. In-memory record on Instance (no persistence per G8).
  - `ObserveVerdict(v *Verdict)` — first-observed convergence per `(slot, layer, operator)`:
    - First verdict from `i` for `(slot, layer)` → record in convergence pool.
    - Second distinct verdict → Rule 6a evidence; mark `i` as null-contributor for `(slot, layer)` (treat-as-null state).
    - Subsequent verdicts (third+) → dropped from convergence input; one Rule 6a evidence per `(slot, layer, operator)` dedup.

**Treat-equivocator-as-null SHOULD rule (Q9 — spec timing)**: On `ObserveVerdict`, just record observations. At convergence-eval (start of Phase 2b, in Phase G), the convergence rule computes pools with equivocators excluded. Per [2abOBFT.md:172](2abOBFT.md#L172).

**Acceptance criteria**: Unit tests covering:
- Healthy verdict computation per the 4-case rule.
- Rule 6a evidence on second distinct verdict.
- Treat-as-null state marked on second-verdict observation; verified in Phase G convergence test.
- Verdict-spam: third+ verdicts dropped from convergence + slashing-evidence dedup.

**Effort**: ~2 days.

## Phase G — Phase 2b convergence + emission

**Goal**: Implement the convergence rule and Phase-2b emission per spec §Phase 2b.

**Files**:
- `twoab/convergence.go`:
  - `ConvergenceDecision(instance, layer) (CommitChoice, value)`:
    - Implements the 5-row decision table from spec §Convergence rule.
    - Inputs: `verdict_pool[V]`, `nr_pool` (both with treat-as-null equivocators excluded), `V_local`, host re-validate-at-Phase-2b-sign result.
    - Output: σ on V, or NR.
- `twoab/phase2b.go`:
  - `BuildOwnOnion2b() (*Onion2b, error)` — per-layer convergence decision + emission:
    - σ on V at layer 0 → plaintext partial.
    - σ on V at layer k > 0 → chained-IBE-encrypted partial under `nr_tag_0 … nr_tag_{k-1}` (use shared `ibe.go` primitive).
    - NR at layer k < K-1 → `σ_i^{IBE}(nr_tag_k)` partial.
    - NR at layer K-1 → no emission (no tag exists, per spec §Convergence rule "Deepest-layer NR has no on-wire emission").
    - **EKM enforcement**: single signing event per `(slot, layer)`; σ-XOR-NR per layer; single-σ-V per `(slot, layer)`. Uses `twoab/ekm.go` coordinator (own key prefix in shared BadgerDB per Q8).
  - `ObserveOnion2b(o *Onion2b) error`:
    - Per-layer entry classification (σ-side vs NR-side).
    - Rule 1 (σ + NR same layer) detection.
    - Rule 3 (σ on V vs V' same layer) detection.
    - Rule 5 (fake plaintext σ at L_0) detection — requires retained V at L_0.
    - **Rule 6b** detection: cross-reference observed verdict view against observed Phase-2b action. Per spec §Slashing evidence Rule 6b boundary-conditional detection.
- `twoab/ekm.go`: EKM coordinator with σ-XOR-NR enforcement, single-σ-V enforcement, own key prefix in BadgerDB. Uses shared `obft.Signer` interface at the bottom.

**Acceptance criteria**: Unit tests covering:
- 5-row convergence rule against synthesized verdict pools (one test per row).
- Treat-as-null equivocator excluded from pool (test ties to Phase F state).
- Chained-IBE encryption round-trip per layer.
- EKM rejection of duplicate σ at same layer; σ-XOR-NR enforcement.
- Rule 1, 3, 5, 6b detection on synthesized adversarial onions.

**Effort**: ~3 days. Convergence rule + EKM + evidence detection is the most complex phase.

## Phase H — Phase 3 reconstruction

**Goal**: Reconstruction walk + final certificate gossip.

**Files**:
- `twoab/phase3.go`:
  - `Resolve() (*Output, error)` — K-layer walk per spec §Phase 3 pseudocode. **Algorithm duplicated** from `base/phase3.go` (per separation directive — Q3); only the shared cryptographic primitives (verify, aggregate, decrypt) are reused.
  - 2ab-specific delta: Rule 4 detection at deeper layers (post-decryption garbage).
  - `BuildCertificate(out *Output) (*Certificate, error)` — own copy.
  - `ObserveCertificate(c *Certificate)` — own copy.

**Acceptance criteria**: Unit tests covering:
- Healthy σ-quorum at L_0.
- NR-quorum fall-through L_0 → L_1.
- Rule 4 detection post-decryption.
- Late-onion incorporation (re-running walk on late `KindOnion2b` arrivals).

**Effort**: ~1.5 days.

## Phase I — Evidence consolidation + observer

**Goal**: Confirm all 6 rules + the EvidenceObserver wire correctly under 2ab.

**Files**:
- `twoab/evidence.go`: own copy of Rules 1-5 evidence types + new Rule 6a (`VerdictEquivocation`) + Rule 6b (`VerdictAction`). No sharing with `base/` per separation directive.
- `twoab/instance.go`: add `recordEvidence` / `EvidenceObserver` plumbing (same shape as `base.Instance`).
- Mirror the "log evidence locally" semantics from the post-slashing-change `base/` impl.

**Acceptance criteria**: All 6 rules detectable + observable in 2ab tests. Observer fires once per `(Rule, Op, Layer)` tuple as in bare OBFT.

**Effort**: ~0.5 day.

## Phase J — `twoab/sim_test.go` test scaffolding

**Goal**: Parallel test harness for 2ab scenarios.

**Files**:
- `twoab/sim_test.go`:
  - `sim` struct extended with verdict deliver helpers + Phase-2a hooks.
  - `deliverPhase1`, `deliverVerdict`, `deliverPhase2bOnion` helpers.
  - `runPhase2a`, `runPhase2b` orchestrators.
- Tests covering canonical scenarios:
  - **n=4, f=1** baseline:
    - Healthy (σ-quorum at L_0).
    - Marginal h_V=3 (3 receive, 1 doesn't) → succeeds at L_0.
    - Marginal h_V=2 (2 receive, 2 don't) → falls through to L_1.
    - Equivocation σ-locked split → falls through (vs bare OBFT slot-miss).
    - h_V=1 selective-delivery → falls through.
    - Validity-divergence 3-of-4 → succeeds at L_0 or L_1.
    - Validity-divergence 2-2 → slot-misses cleanly.
    - 2-1-byz-defect → slot-misses (documented regression).
    - Verdict-equivocation under marginal h_V → recovers via treat-as-null when re-flood completes in time.
    - Late deepest-layer broadcast → recovers via Phase-2a re-flood.
  - **n=7, f=2** validity-divergence (G1):
    - 4-3 validity split → recovers via NR-quorum fall-through (verifies spec claim that 2ab recovers validity-divergence majority at all n, not just n=4).

**Acceptance criteria**: All listed scenarios pass.

**Effort**: ~2-3 days. Scenario coverage is what gives confidence in the impl.

## Phase K — Consensustest integration

**Goal**: Integrate 2abOBFT into the existing consensustest framework alongside QBFT and OBFT.

**Files**:
- `protocol/v2/consensustest/twoab/` (new sub-directory mirroring `consensustest/obft/`):
  - Adapter that drives `twoab.Instance` from the consensustest harness.
- Extend the catalog scenarios (`protocol/v2/consensustest/catalog.go`) with 2ab-specific scenarios.
- Extend the batch-report driver to include 2ab as a comparison target.

**Acceptance criteria**: `go test ./protocol/v2/consensustest/...` passes with 2ab adapter. Cross-protocol comparison batch runs include 2ab columns.

**Effort**: ~2 days.

## Phase L — SSV runner integration + telemetry + mixed-variant rejection

**Goal**: Drive `twoab.Instance` from the SSV runner layer.

**Files**:
- `protocol/v2/ssv/runner/obft/base/` — existing runner code moved here (if not already split in Phase A).
- `protocol/v2/ssv/runner/obft/twoab/` — new runner mirror:
  - Lifecycle: slot_start → Phase 1 fetch (per layer at FetchAt offsets) → broadcast bundles → at `T_verdict_start` start observing verdicts → at `T_verdict_max − ε_proc` build + broadcast own verdict → at `T_commit` compute convergence + build `Onion2b` + broadcast → from `T_commit + Δ_2b` poll `Resolve()`.
  - New `LifecycleHooks` field `BroadcastVerdict(ctx, slot, data) error`.
- Each runner sub-package has its own envelope dispatcher (since wire envelopes are per-protocol).

**Per-cluster variant selection (Q7)**: hardcoded operator-local config flag (e.g., `obft_variant: "twoab"` vs `"base"`) read at runner-init time; controller selects which runner sub-package to construct.

**Mixed-variant cluster rejection (G3)**: when an envelope's `protocol_tag` doesn't match the local cluster's configured variant, reject at the envelope decoder with a clear diagnostic log. Tests already in Phase C cover domain-separation at the wire layer.

**Telemetry (G4)**: lightweight metrics surface — counters for verdict broadcasts, per-row convergence outcomes, Rule 6a/6b detections. Reuse existing OBFT telemetry framework. No dashboards in scope.

**Benchmarks (G5, optional)**: small bench file covering single-slot consensus end-to-end, K=4 reconstruction, IBE decryption throughput. Compare against bare OBFT baseline. Don't block shipping on this.

**Acceptance criteria**:
- 2ab runner integration test: a 4-operator cluster runs a full slot via the SSV runner harness with twoab.Instance, produces a certificate.
- Bare OBFT runner unaffected.
- Mixed-variant envelope is rejected with a diagnostic log.

**Effort**: ~3-4 days.

## Phase M — Migration / rollout

**Goal**: Make 2abOBFT deployable via per-cluster opt-in.

**Steps**:
- Wire-protocol versioning via `protocol_tag = "2abOBFT-v1"` field on the envelope (already in Phase C). Cross-protocol message rejection (already in Phase C + Phase L).
- Per-cluster opt-in: hardcoded operator-local config flag (e.g., `obft_variant: "twoab"` vs `"base"`) read at runner-init time. Operators on a cluster must agree on the variant — coordinate out-of-band. (Per Q7: no on-chain registry in scope; treat as future work.)
- DKG: 2ab requires the IBE keypair DKG (same as bare OBFT). No additional durable-state setup (G8 — in-memory only).

**Acceptance criteria**: A cluster can be configured as `twoab` variant; SSV node starts cleanly with twoab.Instance; mixed-protocol cluster (operators disagreeing on variant) refuses to converge with clear diagnostic logs (covered by Phase C + L tests).

**Effort**: ~1 day. (Reduced from initial estimate — most rollout-related wiring is already in earlier phases. No operator-facing docs per G10.)

## Testing strategy

| Layer | Coverage | Scope |
|---|---|---|
| Unit tests | Per-method | Each method in `twoab/` has direct unit tests. |
| Sim tests | Multi-operator scenarios | `twoab/sim_test.go` covers all spec-documented scenarios (Phase J list). |
| Integration tests | Runner-driven full-slot | `protocol/v2/ssv/runner/obft/twoab/runner_test.go` runs a 4-op cluster via mocked SSV runner. |
| Consensustest | Cross-protocol comparison | 2ab participates in batch-report runs alongside QBFT/OBFT. |
| Property tests | Pigeonhole invariants | Existing OBFT property tests (Rule-4 stays sealed, etc.) ported / extended for 2ab. |
| Adversarial fuzzing | Byzantine action coverage | Long-tail byzantine behavior in the simulator (random per-op delays, equivocation patterns). Reuse existing framework from `consensustest/`. |

## Execution phases — summary

| # | Phase | Effort | Self-contained PR? | Bare-OBFT regression risk |
|---|---|---|---|---|
| A | Refactor → `base/` | 1-1.5d | Yes | Low (mechanical) |
| B | `twoab/` skeleton + Config + Validate + delta-catalog doc | 1d | Yes | None |
| C | 2ab wire types + domain-separation tests | 1.5d | Yes | None |
| ~~D~~ | ~~Persistent state interface~~ | — | (removed — G8 out of scope) |  |
| E | 2ab Phase 1 (no σ_V) | 1.5d | Yes | None |
| F | 2ab Phase 2a + verdict + treat-as-null state | 2d | Yes | None |
| G | 2ab Phase 2b convergence + EKM coordinator + emission | 3d | Yes | None |
| H | 2ab Phase 3 reconstruction | 1.5d | Yes | None |
| I | Evidence types (Rules 1-5 + 6a + 6b) + observer | 0.5d | Yes | None |
| J | 2ab sim tests (n=4 + n=7 validity-divergence) | 2-3d | Yes | None |
| K | Consensustest integration | 2d | Yes | None |
| L | SSV runner integration + telemetry + mixed-variant rejection | 3-4d | Mostly | Low |
| M | Migration / rollout (config flag wiring; no docs) | 1d | Yes | None |

Total rough effort: **20-23 days** of focused implementation, plus review/iteration cycles.

## Explicit non-goals

- **No deprecation of bare OBFT**. Both protocols coexist; cluster operators select per-cluster.
- **No L_Bid bid-routing extension** (spec mentions it for OBFT only; 2ab + L_Bid is unspecified).
- **No multi-round 2abOBFT** (the "2abOBFT + R" composition mentioned in spec §Composability). Out of scope.
- **No full EKM-binding of verdicts** (open question #16). The SHOULD-level treat-as-null rule is the partial close used here.
- **No formal-verification updates**. `docs/OBFT-formal-verif.md` does not need changes for this impl; 2ab's safety pigeonholes are identical structurally (same `qV = qEnc = 2f+1`).

## Execution-readiness checklist

Before kicking off Phase A:
- [x] User has reviewed and approved this plan. (Approved via iteration on open questions Q1-Q10 and G1-G10.)
- [x] All decisions locked in. See §Locked-in decisions at the top.
- [x] Spec edits prerequisite landed: assumption 5 simplified, §EKM coordination model trimmed, design-notes #4 marked superseded.
- [ ] Branching strategy decided (one long-lived branch with multiple PRs, vs branch-per-phase).
- [ ] Code review owner assigned.
- [ ] Phase A's import-cycle dry-run completed (grep for shared-vs-specific cross-references).

## Out-of-band considerations

- **Spec evolution**: if the spec changes during implementation (e.g., open question #16 gets resolved with full EKM-binding), this plan needs revision before the relevant phase. Default: freeze spec at the commit hash that starts Phase A; revisit at quarterly checkpoint.
- **Coordination with bare OBFT impl work**: any in-flight bare-OBFT refactoring should land before Phase A to avoid merge conflicts. Coordinate with whoever owns recent commits.
- **Wire compatibility with prior bare-OBFT deployments**: Phase M's migration must not break operators still running bare OBFT during transition. Protocol_tag versioning handles this; verify in Phase M.

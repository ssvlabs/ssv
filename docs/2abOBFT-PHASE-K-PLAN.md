# Phase K — consensustest integration plan

Plan-doc for Phase K of [2abOBFT-IMPL-PLAN.md](2abOBFT-IMPL-PLAN.md): integrate `twoab.Instance` into the existing `protocol/v2/consensustest/` framework alongside the bare OBFT and QBFT adapters. After Phase K, the same scenarios that exercise OBFT and QBFT will also exercise 2abOBFT; the cross-protocol comparison batch picks up a 2ab column "for free".

## Goal

A `consensustest/twoab/` adapter package that:

1. Implements `consensustest.Protocol` (`Name() string`, `Run(SimConfig) (Outcome, error)`).
2. Wraps `twoab.Instance` under a virtual-time discrete-event simulator (DES), no `time.Now` / `time.Sleep`.
3. Translates the abstract `ByzPattern` into 2ab-internal byz behaviors; returns `ErrNotApplicable` for kinds that don't translate.
4. Emits `Evidence`-derived per-rule fire counts under the `"2abOBFT/Rule{N}/{Description}"` naming convention.
5. Populates `CommitAttestation.EquivocationChecked` (counting Rule 2 + Rule 3 + Rule 6a fires).

Every existing catalog scenario must run to a defined outcome class against the 2ab adapter; the `"2abOBFT"` `Expect` entry on every catalog scenario must match observed behavior at the canonical operating point (n=4, BTT=200ms, ConstantDelay).

## Existing assets I'm modeling on

Bare OBFT adapter at `protocol/v2/consensustest/obft/`:

| File | Role |
| ---- | ---- |
| `adapter.go` (261L) | `Protocol{}.Run`, `computeAttestation`, evidence-rule key constants, `evidenceByRule` mapper, `rawOutcome→ct.Outcome` translator |
| `des.go` (~250L) | DES setup: `runDES`, `sim` struct, `start()` (config + instance build + initial event scheduling), `runLoop`, `outcome()` |
| `events.go` (~600L) | 6 event types: `evtLeaderFetch`, `evtPhase1Arrival`, `evtPhaseTwoStart`, `evtCommitArrival`, `evtResolve`, `evtResolveRerun` |
| `byz.go` (~1000L) | `internalByz` interface + ~15 pattern implementations + `translateByz` mapping |
| `sizes.go` | Wire-size estimators for `Phase1Bundle`, `Commit` (used for bandwidth accounting) |
| `adapter_test.go` | Smoke tests verifying adapter runs each catalog scenario without panic |

Total ~2700 LoC for the OBFT adapter. The 2ab adapter will be similar size (~3000L), modulo the additional Phase-2a verdict event.

## Adapter structure (mirroring base, with 2ab-specific deltas)

```
protocol/v2/consensustest/twoab/
  adapter.go              # Protocol{}, computeAttestation, evidence keys
  des.go                  # sim struct, runDES, start(), runLoop(), outcome()
  events.go               # event types
  byz.go                  # internalByz, translateByz, ~15 pattern types
  sizes.go                # wire-size estimators
  adapter_test.go         # smoke tests
```

### Event types (deltas vs base)

| Event | Bare OBFT | 2abOBFT |
| ----- | --------- | ------- |
| `evtLeaderFetch` | builds `Phase1Bundle` with σ_V partial | builds `Phase1Bundle` (no σ_V — Variant C) |
| `evtPhase1Arrival` | ObservePhase1Bundle + ApplyHostValidity | same; classification (auth-only vs regular) handled inside Observe |
| `evtVerdictBroadcastStart` | — | **NEW**: fires at `T_verdict_max − ε_proc`; each op BuildVerdict + emit |
| `evtVerdictArrival` | — | **NEW**: ObserveVerdict |
| `evtPhaseTwoStart` | BuildOwnCommit + emit (T_commit) | BuildOwnOnion2b + emit (T_commit) |
| `evtOnion2bArrival` | (was `evtCommitArrival`) ObserveCommit | ObserveOnion2b |
| `evtResolve` | per-op Resolve + cert broadcast | same; cert is `twoab.Certificate` |
| `evtCertArrival` | ObserveCertificate | same |
| `evtResolveRerun` | re-runs Resolve on late `evtCommitArrival` | re-runs Resolve on late `evtOnion2bArrival` |

The verdict-broadcast event-pair is new. Fires at `cfg.TVerdictMax() − ε_proc` per spec §Phase 2a; ε_proc is small (~50ms — same scale as Phase-3 ε_3).

### Internal byz interface (mirrors base, with verdict hooks)

```go
type internalByz interface {
    // Phase 1 (unchanged from base)
    LeaderBroadcastPlan(s *sim, leader OperatorID, layer int, honestV Value) []broadcastPlan
    OverrideOwnPhase1Delay(s *sim, leader OperatorID) time.Duration

    // Phase 2a (NEW)
    AllowVerdictBroadcast(op OperatorID, layer int) bool
    OverrideVerdict(s *sim, op OperatorID, layer int, v *Verdict) *Verdict
    BuildExtraVerdicts(s *sim, op OperatorID, layer int, v *Verdict) []*Verdict  // for Rule 6a equivocation

    // Phase 2b
    AllowOnion2bBroadcast(op OperatorID) bool
    OverrideOnion2b(s *sim, op OperatorID, o *Onion2b) *Onion2b
    BuildExtraOnion2bs(s *sim, op OperatorID, o *Onion2b) []*Onion2b
    OverrideOwnOnion2bDispatchDelay(s *sim, op OperatorID) time.Duration

    // Phase 3 (cert)
    AllowCertificateBroadcast(op OperatorID) bool

    // Generic
    AllowDelivery(from, to OperatorID, kind ct.MsgKind) bool
    OverrideDelay(rng *mrand.Rand, from, to OperatorID, kind ct.MsgKind) time.Duration
}
```

### Byz pattern translation matrix

| Abstract `ByzKind` | 2ab translation | Notes |
| ------------------ | --------------- | ----- |
| `ByzNone` | byzNone | identity baseline |
| `ByzSilentLeader` | byzSilentLeader (Phase 1) | leader withholds bundle |
| `ByzMultiSilent` | byzMultiSilent | top K layers silent |
| `ByzEquivocate111` | byzEquivoc111 (Phase 1) | leader broadcasts distinct V per honest receiver — feeds Rule 2 |
| `ByzEquivocateAllNR` | byzEquivocAllNR | leader floods both V's |
| `ByzEquivocateSigmaLockedSplit` | byzEquivocSplit | **2ab fall-through expected vs OBFT miss** |
| `ByzHV1SelectiveDelivery` | byzHV1Selective | leader delivers V to f honest only |
| `ByzFakeEncryptedPresence` | byzFakeEncPresence (Phase 2b) | onion entry decrypts to garbage at k>0 → Rule 4 |
| `ByzSigmaRefusal` | byzSigmaRefusal | byz never σ-emits (always NR or silent) |
| `ByzCrossSigning` | byzCrossSigning | (Rule 1) σ + NR at same layer in one onion |
| `ByzCrossOnionEquivocation` | byzCrossOnionEquivoc | (Rule 3) two distinct σ partials at same layer across onions |
| `ByzFakePlaintextSigma` | byzFakePlaintextSigma | (Rule 5) σ at L_0 on a V never broadcast |
| `ByzLateLeaderBroadcast` | byzLateLeaderBroadcast | bundle past `T_accept_max`; auth-only retention |
| `ByzWithholdLeader` | byzWithholdLeader | deepest layer silent |
| `ByzPartialEquivocation` | byzPartialEquivoc | natural-recovery: V to 2f honest, V' to 1; σ-pool still reaches qV |
| `ByzCertWithholding` | byzCertWithholding | reconstructs but doesn't gossip cert |
| `ByzDelayedCommit` | byzDelayedOnion2b | onion arrival past `RoundEndOffset`; pairs with `EnableLateCommitRerun` |
| `ByzAggregatorBypass` | `ErrNotApplicable` | adversarial test, not in catalog matrix |
| `ByzWitnessForgery` | `ErrNotApplicable` | 2ab has no Witnesses array (no Phase-1 σ_V) — N/A by construction |
| `ByzGarbageMessages`, `ByzExceedsRateLimit`, `ByzOfflineDoubleVAttempt` | `ErrNotApplicable` | reserved enum slots — same as base |

**Deferred to Phase L (or a follow-up Phase J extension)**:
- 2ab-specific byz kinds: verdict-equivocation (Rule 6a) and verdict-vs-action (Rule 6b). Adding new abstract `ByzKind` values requires touching the framework's `byz.go` + every existing adapter; safer to land after Phase K stabilizes.

### Catalog `"2abOBFT"` Expect entries

The 29-scenario catalog needs a `"2abOBFT"` key on every `Expect` map. Most cases match OBFT's expectation (2ab is a strict-superset recovery story); the divergences are the test cases that make 2ab worth shipping. Predicted entries (to be verified against adapter runs):

| Scenario | OBFT | 2abOBFT |
| -------- | ---- | ------- |
| Healthy | SuccessFastest | **same** |
| SilentLeaderL0 | SuccessFallThrough | **same** |
| MultiSilent | SuccessFallThrough | **same** |
| MultiSilent_AllLayers | SuccessFastest (3 honest σ) | **same** |
| SigmaRefusal | SuccessFastest | **same** |
| WithholdLeaderDeepest | SuccessFastest | **same** |
| CertWithholding | SuccessFastest | **same** |
| Equivocate111 | MISS | **SuccessFallThrough** (NR-quorum advances) |
| EquivocateAllNR | SuccessFallThrough | **same** |
| **EquivocateSigmaLockedSplit** | **MISS** | **SuccessFallThrough** ← key 2ab win |
| PartialEquivocationNaturalRecovery | SuccessFastest | **same** |
| ValidityDivergence_3_1 | SuccessFastest | **same** |
| ValidityDivergenceAlgebraicLimit (2-2) | MISS | **MISS** (still algebraic at n=4) |
| ValidityDivergenceNRFallThrough | SuccessFallThrough | **same** |
| ValidityDivergence_PassiveByz_Silent_1NV | MISS | **MISS** or SuccessFallThrough (TBD) |
| ValidityDivergence_PassiveByz_Silent_2NV | MISS | **MISS** |
| ValidityDivergence_PassiveByz_SigmaOnV_2NV | MISS | TBD |
| ValidityDivergence_LeaderNV_PassiveByz | MISS | TBD |
| HostInvalidUntilL1 | TBD | TBD |
| HostFlipMidSlot | TBD | TBD |
| HV1SelectiveDelivery | MISS | **SuccessFallThrough** (NR-quorum) |
| LateLeaderBroadcast | SuccessFallThrough | **same** |
| AsymmetricPropagation_FSlow_Success | MISS | TBD |
| AsymmetricPropagation_FPlus1Slow_Miss | SuccessFastest | **same** |
| MeshFlakiness | MISS | TBD |
| CrossSigningRule1 | TBD | TBD |
| CrossOnionEquivocationRule3 | TBD | TBD |
| FakeEncryptedPresence | TBD | TBD |
| FakePlaintextSigmaRule5 | TBD | TBD |

`TBD` cells get resolved when the adapter runs the scenarios; entries that are surprising vs spec intuition flag a delta to record in `twoab-impl.md`.

### Real BLS support

The bare adapter has dual-mode: stub signer (default, fast for matrix sweeps) and `blsbackend` (when `SimConfig.BLSKeys != nil`). 2ab adapter mirrors this. The `blsbackend.NewKyberSigner(share)` already handles 2ab's tagSigner (it's a generic threshold IBE primitive; works identically for OBFT's nr-tag and 2ab's nr-tag). No new BLS plumbing needed.

### Tier integration

Touch:

- `protocol/v2/consensustest/correctness_test.go` — add `twoabadapter.Protocol{}` to the protocols list.
- `protocol/v2/consensustest/stress_test.go` — same.
- `protocol/v2/consensustest/batch_test.go` — same.

Each catalog scenario expectation update is mechanical (add `"2abOBFT": ...` to existing Expect maps).

## Sequencing

Single-commit Phase K, but staged execution:

| Step | Output | Verification |
| ---- | ------ | ------------ |
| K1 | `twoab/adapter.go` + `sizes.go` + Protocol skeleton | builds; `Name()` returns "2abOBFT" |
| K2 | `twoab/des.go` + `events.go` (event types + handlers for Phase 1 / 2a / 2b / 3) | builds; healthy DES run succeeds (validate against `TestScenario_HealthyL0Success`-equivalent input) |
| K3 | `twoab/byz.go` (internal interface + ~15 pattern translations) | builds; each catalog scenario translates without panic |
| K4 | `twoab/adapter_test.go` smoke tests (run all catalog scenarios) | every catalog scenario runs to a defined outcome (Decided or ErrNoQuorum); zero panics |
| K5 | `catalog_*.go` files: add `"2abOBFT"` Expect entries | the smoke-test outputs from K4 inform what to put in each cell |
| K6 | `correctness_test.go` / `stress_test.go` / `batch_test.go` adapter registration | `go test ./protocol/v2/consensustest/...` passes |
| K7 | Self-review pass; `twoab-impl.md` updated with any surfaced deltas | clean diff; tests green |

Iteration: between K4 and K5, scenarios where adapter output surprises us (esp. validity-divergence and asymmetric propagation) get a focused look — either the adapter has a translation bug, or 2ab actually behaves differently than predicted (which is the more interesting case and goes into `twoab-impl.md`).

## Out of scope (deferred)

- New 2ab-specific abstract `ByzKind`s (verdict equivocation / verdict-vs-action). Adding these touches every adapter; defer until Phase K stabilizes. Could land as Phase J' or Phase K.5.
- Cross-protocol comparison reports surfacing the 2ab column distinctively. Existing `consensustest/reporting/` consumes any Protocol the batch driver feeds it — no changes needed beyond adapter registration.
- Bandwidth / safety attestation deltas beyond what OBFT already exposes. The 2ab adapter ships with the same instrumentation level as the OBFT adapter.

## Open questions for the user

1. **Adapter dir name confirm**: `consensustest/twoab/` to match `protocol/v2/obft/twoab/` — or do you prefer `consensustest/twoabobft/` for visual symmetry with the spec name? (My preference: `twoab/`, matching production package.)
2. **Single commit vs split**: one commit for Phase K, or break into K1-K4 (adapter) + K5-K7 (integration)? My instinct says split — adapter scaffolding is a self-contained unit; catalog updates are a separate cognitive load. Two commits feels right.
3. **Stub-only first or stub+BLS together**: blsbackend mode is straightforward to mirror, but it's possible to land stub-only first and add BLS as a follow-up. My preference: ship both in one go (the bare adapter does, and it's only ~20 lines of plumbing).
4. **Catalog gaps for 2ab-distinguishing scenarios**: should we proactively add `Equivocate111_n7` etc. scenarios that exercise 2ab's larger-n recovery story? I think no — that's matrix-expansion work, separate from "wire the adapter up". Phase J' if/when we want it.
5. **`TBD` cells in expectations**: OK to fill them in based on adapter-run outputs as part of K5, rather than predicting all upfront? Yes I think this is the right approach (fast-fail beats analytic over-prediction).

## Effort estimate

| Step | Effort |
| ---- | ------ |
| K1 — Protocol skeleton | 1h |
| K2 — DES + events | 4h |
| K3 — Byz translations | 4h |
| K4 — Smoke tests | 2h |
| K5 — Catalog expectations | 2h |
| K6 — Tier registration | 30min |
| K7 — Self-review + impl-md updates | 1h |
| **Total** | **~2 days** |

Matches the impl-plan estimate.

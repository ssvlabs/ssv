# Consensus stress-test framework

## Current state

The framework is **built and tested**. This document carries both a current-state guide (this section) and the original implementation plan (sections below from "Goal" onward — kept as a historical reference for design rationale).

**Follow-on work since this plan was written:**

- [docs/OBFT-SPEC-ALIGNMENT-PLAN.md](OBFT-SPEC-ALIGNMENT-PLAN.md) — 13-task pass aligning the test suite to current OBFT.md spec wording (witness sizes, ε_3 / Δ_2 split, max-MEV broadcast knob, late-`KindCommit` re-resolve framework, plus 4 new catalog scenarios: validity-divergence-with-passive-byz × 3, mesh-flakiness × 1).
- [docs/CONSENSUSTEST-BATCH-PLAN.md](CONSENSUSTEST-BATCH-PLAN.md) — 14-task pass adding the batch-comparison framework (multi-sim distribution-aware reports). See [docs/CONSENSUSTEST-REPORT.md](CONSENSUSTEST-REPORT.md) for usage.

The "Known gaps / follow-up work" and "Catalog at 21 scenarios" notes in this doc are pre-alignment-pass; current catalog count is **25 scenarios** and several follow-up gaps have been closed in the alignment / batch plans above.

### Architecture as built

```
protocol/v2/consensustest/                       FRAMEWORK
  protocol.go         SimConfig, Outcome, OperatorOutcome, Protocol interface
  byz.go              ByzPattern (with ByzOperators slice, multi-byz)
  host.go             HostPattern + Phase enum + HostFlipMidSlot, HostInvalidUntilLayer
  network.go          NetworkModel + ConstantDelay, JitteredDelay, PerReceiverDelay, PartitionedNetwork
  schedule.go         DefaultBkSchedule, DefaultFetchSchedule (BTT-anchored, K-aware)
  bandwidth.go        BandwidthReport (per-kind, per-layer, per-op)
  stubsig.go          BLS-realistic byte-size constants
  offlineagg.go       OfflineAggregator + AttemptAll (NoOfflineDoubleV safety check)
  safety.go           SafetyReport (SingleV, HonestAgreement, Terminated, NoOfflineDoubleV)
  runner.go           RunScenarioOnProtocol (panics on safety violation)
  scenario.go         Scenario, ExpectClass, Match
  catalog.go          21 cross-protocol scenarios
  bls.go              GenerateBLSKeys (real threshold BLS); BLSKeys struct lives in protocol.go
  matrix.go           MatrixReport
  *_test.go           framework smoke + matrix + sweep + bandwidth + host-pattern + report tests

  obft/               OBFT ADAPTER (wraps real obft.Instance)
    adapter.go        Run, evidenceByRule, ruleKey
    des.go            DES driver (single goroutine, virtual time)
    events.go         evtLeaderFetch / evtPhase1Arrival / evtPhaseTwoStart / evtCommitArrival / evtResolve / evtCertArrival
    byz.go            16 byz patterns (incl. Phase-3 evidence-targeted: CrossSigning, FakePlaintextSigma, CrossOnionEquivocation, WithholdLeader, CertWithholding, LateLeaderBroadcast, AggregatorBypass)
    sizes.go          Wire-size accounting (phase1BundleSize, commitSize, certSize)
    adapter_test.go   per-cluster-size healthy + per-rule evidence + multi-byz + offline-aggregator

  qbft/               QBFT ADAPTER (wraps real qbft.Instance)
    adapter.go        Run + post-deadline clip-to-MISS
    des.go            DES driver, builds qbft.Instance per honest op
    events.go         evtStartInstance / evtMessageArrival / evtRoundTimeout / evtByzProposal
    byz.go            7 byz patterns (Phase-3 OBFT-specific kinds → ErrNotApplicable)
    network.go        virtualNetwork (specqbft.Network impl, self-delivery loopback)
    timer.go          virtualRoundTimer (specqbft.Timer impl, DES-scheduled)
    signer.go         virtualOperatorSigner (RSA via spec testingutils key sets)
    beacon_signer.go  noopBeaconSigner (qbft.IConfig dependency)
    value_check.go    virtualValueChecker (round-aware via sim.inflightRound)
    proposer.go       proposerForRound, makeProposalEnvelope (byz fabrication)
    keys.go           keysetForN — uses Testing{4,7,10,13}SharesSet
    adapter_test.go   per-cluster-size healthy + round-change + equivocation fall-through + determinism

  reporting/          REPORT RENDERERS (HTML / CSV / Markdown)
    reporting.go      Run / CellKey / cellSummary
    csv.go            RenderCSV
    markdown.go       RenderMarkdown
    html.go           RenderHTML (Chart.js via CDN)
    reporting_test.go end-to-end smoke
```

### Universal safety invariants (enforced per sim)

`RunScenarioOnProtocol` panics on violation of any of:

- **`SingleV`** — at most one distinct decided V cluster-wide (Pigeonhole 1).
- **`HonestAgreement`** — all deciders agree on V.
- **`NoOfflineDoubleV`** — the offline aggregator (worst-case byz with full message visibility) cannot reconstruct two distinct V signatures (Pigeonhole 2 + 3 — strictly stronger than `SingleV`).

`Terminated` (every op decided or has a non-empty error) is reported as a warning, not a panic — adapter bug if it fires under healthy sim.

### How to run

| Target | Description | Wall time |
|---|---|---|
| `go test ./protocol/v2/consensustest/...` | Default suite (stub-crypto, all phases) | ~3-4 s |
| `make consensustest-real-bls` | Real-BLS suite (build tag `real_bls`) — actual threshold-BLS + tlock IBE end-to-end at n=4,7,10,13 | ~17 s |
| `make consensustest-report` | Generates per-(sweep, point) HTML / CSV / Markdown comparison reports of the catalog matrix to `./consensustest-reports/` (5 curated sweeps × OBFT/QBFT × 100 iterations) | ~75 s |

### Stub-BLS vs real-BLS modes

- **Stub mode** (default): OBFT uses `obft.NewStubSigner` + `obft.NewStubIBE`; signatures are short bytes that pass structural validation but don't run real BLS math. Bandwidth accounting uses the BLS-realistic byte sizes from `stubsig.go` (96B sig, 48B pubkey, etc.) so wire-size totals match real-BLS mode at the byte level.
- **Real-BLS mode** (`cfg.BLSKeys != nil`): OBFT uses `blsbackend.New` + `blsbackend.NewKyberSigner` + `blsbackend.NewTLockIBE`. Real BLS12-381 sigs, real tlock IBE encryption.
- **QBFT** runs real RSA in both modes (the spec testing-utils' `TestKeySet` provides per-op RSA keys).

### Coverage matrix — scenarios → spec claims

| Scenario | Verifies |
|---|---|
| `Healthy` | `OBFT.md §Application` canonical operating point fits at BTT=200ms |
| `PrimaryLeaderSilent` | OBFT in-round fall-through; QBFT R2 round-change |
| `MultiSilent_K3` | OBFT advantage when ≥1 of K-1 leaders silent (`BFT-comparison.md` Table 3) |
| `Equivocate_111` / `Equivocate_AllNR` / `Equivocate_SigmaLockedSplit` | Equivocation patterns from `BFT-comparison.md` Table 3 |
| `HV1SelectiveDelivery` | OBFT-specific h_V=1 deadlock pattern |
| `FakeEncryptedPresence` | Rule 4 detection at honest receivers |
| `ValidityDivergence_AlgebraicLimit` | OBFT host-divergence at L_0 (#NV = N-2f); QBFT round-aware host validation (Phase 4 fix verifies the lock) |
| `ValidityDivergence_3_1` | Minority NV: σ-quorum still reaches at L_0 |
| `ValidityDivergence_NRFallThrough` | Majority NV (#NV = 2f+1 = qEnc): NR-quorum unlocks fall-through to L_1 |
| `SigmaRefusal` | Single-byz silence within f-bound doesn't disrupt healthy path |
| `WithholdLeader_Deepest` | Class A late-deepest-layer pathology resilience |
| `CertWithholding` | Honest ops reconstruct independently of byz cert gossip |
| `CrossSigning_Rule1` | Rule 1 evidence (σ + NR exclusivity) |
| `FakePlaintextSigma_Rule5` | Rule 5 evidence (cryptoFake at L_0) |
| `CrossOnionEquivocation_Rule3` | Rule 3 evidence (cross-onion equivocation, top-level + per-layer) |
| `HostFlipMidSlot` | OBFT validate-once-and-lock; QBFT round-aware validation passes round 1 |
| `HostInvalidUntilL1` | Both protocols fall through when L_0 / round 1 host-rejected |
| `LateLeaderBroadcast_L0` | Class A asymmetric-propagation past T_commit; cluster falls through to next layer's wider absorption (spec §Failure modes) |

### Sweep test coverage

| Test | Axis varied | Purpose |
|---|---|---|
| `TestSweep_N` | n ∈ {4, 7, 10, 13} | Healthy at every cluster size |
| `TestSweep_K` | K ∈ [MinK(n), n] for each n | Healthy at every valid (n, K) |
| `TestSweep_BTT` | BTT ∈ {100, 200, 400}ms | Catalog matrix at each BTT (logs diagnostic; asserts canonical) |
| `TestSweep_Seeds` | 5 seeds × jittered network | Safety holds under non-determinism |
| `TestSweep_MultiByz_n7` | n=7 with 2 byz silent leaders | Fall-through past multiple silent leaders |

### Real-BLS coverage (gated behind `real_bls` build tag)

| Test | Coverage |
|---|---|
| `TestRealBLS_Healthy_AllClusterSizes` | n=4,7,10,13 healthy under real BLS |
| `TestRealBLS_Catalog_n4` | Full catalog at n=4 — must match stub-mode outcomes |
| `TestRealBLS_Catalog_n7` | Full catalog at n=7 — diagnostic (some n=4 expectations don't hold at n=7) |
| `TestRealBLS_Seeds` | 10 seeds × jittered network |
| `TestRealBLS_KSweep_n7` | K ∈ {4, 5, 6, 7} at n=7 |

Spec-test key generation (`Testing{4,7,10,13}SharesSet`) is cached process-wide via `blsKeyCache`, amortizing the ~100-200ms generation cost across all tests in the suite.

### Reporting

`make consensustest-report` runs five curated sweeps (canonical operating point + cluster_scaling + btt_degradation + heavy_tail + loss) × OBFT/QBFT × 100 iterations per cell and writes per-(sweep, point) HTML / CSV / Markdown reports plus a navigation `index.html` to `./consensustest-reports/`. Each per-point HTML has five Chart.js panels: summary matrix, success rate, decision-time P50/P90/P99 grouped bars, bandwidth stacked-by-kind, P99 latency vs success-rate scatter.

Override defaults: `ITERATIONS=1000 make consensustest-report` (rare-event scenarios, ~12-15 min); `REPORT_DIR=path make consensustest-report` (custom output dir).

See [docs/CONSENSUSTEST-REPORT.md](CONSENSUSTEST-REPORT.md) for the per-chart interpretation guide and how to add new sweeps.

### Known gaps / follow-up work

- **Byz patterns covered, with three reserved enum values**. Headline rule patterns (Rules 1-5), Class A spec coverage (`ByzLateLeaderBroadcast`), `ByzPartialEquivocation` (2-1 natural recovery, OBFT.md:443), and the negative-test `ByzAggregatorBypass` are all shipped. Three enum values (`ByzGarbageMessages`, `ByzExceedsRateLimit`, `ByzOfflineDoubleVAttempt`) are reserved but **not** translated to scenarios — each is covered at another layer:
    - `ByzGarbageMessages` — covered by `obft.ValidateCommit` / `ValidatePhase1Bundle` direct unit tests; `ByzFakeEncryptedPresence` is the catalog representative for a specific garbage variant (forged IBE ciphertext → Rule 4).
    - `ByzExceedsRateLimit` — covered by `obft.Instance.peerCommitHashes` cap unit tests in `instance_test.go`, with the validation-layer mirror in `message/validation/obft_admissions_test.go`.
    - `ByzOfflineDoubleVAttempt` — `OfflineAggregator.AttemptAll` runs on **every** scenario via `recordCommitToAggregator`; `NoOfflineDoubleV` is a universal safety invariant. `ByzAggregatorBypass` is the active-attack variant (forged identities).
- **OBFT chained-decrypt approximation in `OfflineAggregator`** — chain-unlock currently checks "any V has NR-quorum at each shallower layer" (permissive); per-V chain matching is a future refinement when adapters record (layer, V) tuples per NR-quorum.
- **Phase parameter has two values: `PhasePhase1Acceptance` (OBFT) and `PhaseDecide` (QBFT)**. A `PhasePhase2Commit` value was considered but trimmed — OBFT's protocol doesn't re-validate at Phase 2 (the locked Phase-1 verdict wins), so there's no caller for it. Re-add when an explicit phase-distinction test materializes.
- **Catalog at 21 scenarios**, plan called for 25-30. Headline coverage is in place; additional scenarios can be added incrementally. Larger-cluster sweep (n=7/10/13) runs the full catalog with safety enforcement and asserts per-cell expectation matches — every scenario's Apply scales with cfg.N / cfg.F() so outcome classes are stable across all SSV cluster sizes.
- **Real-BLS suite at ~17s wall time** vs the 10-min budget. Plenty of headroom to scale up with deeper sweeps as needed.

### Proposed stress-test additions (planned, not yet executed)

Existing coverage is comprehensive on the named active-byz catalog (21 scenarios), cluster-size scaling (n ∈ {4, 7, 10, 13}), BTT operating points, and doc-table arithmetic (`TestSweep_DocTable`). Universal safety invariants panic on every run, so the existing surface is well-tested for safety. The gaps below split along the protocol's two correctness pillars: **liveness must hold flawlessly under conditions the protocol claims to tolerate**; **safety must hold under active byz grief**.

#### Tier 1 — Liveness under conditions the protocol claims to tolerate (MUST)

The protocol's liveness assumption is that, given ≤ f byzantine and network within partial-synchrony bound `(P99, δ)`, consensus completes by the relay cutoff. These tests verify the claim by stressing the `(byz, network, clock)` axes the spec says we should tolerate.

| Test | Axis | Verifies |
|---|---|---|
| `TestSweep_Jitter` | `JitteredDelay` jitter ∈ {0, 50, 100, 200ms} × catalog at BTT=200ms | Liveness holds across propagation jitter inside one P99 BTT-budget cycle. Logs decision-rate gradient. |
| `TestSweep_Asymmetric` | `PerReceiverDelay`: 1-2 honest at 2× BTT, others at BTT | Per-layer absorption-window semantics under realistic mesh asymmetry — OBFT staggered design must fall through cleanly. |
| `TestSweep_ClockSkew` | per-operator clock offset ∈ ±[0, δ] = ±50ms across catalog | Spec assumption: cluster δ-bound holds. **Pre-requisite**: new `ClockSkew map[OperatorID]time.Duration` field on SimConfig + adapter wiring (per-op virtual-clock offset honored by timer firings + message-arrival timestamps; deterministic per config). |
| `TestSweep_PassiveByz_UnderStress` | catalog scenarios where byz behavior is structurally indistinguishable from honest network/operator failure (SigmaRefusal, SilentLeader, MultiSilent, WithholdLeader_Deepest, LateLeaderBroadcast within absorption budget) × {jitter, asymmetric, clock-skew} | The "byzantine-equivalent-to-honest-failure" coverage — protocol must work flawlessly because we can't distinguish silent-byz from offline-honest in production. |

#### Tier 2 — Failure mode validation (must MISS cleanly)

These probe the "graceful failure" boundary: out-of-envelope conditions where the protocol must miss without violating safety invariants.

| Test | Axis | Verifies |
|---|---|---|
| `TestSweep_Partition` | `PartitionedNetwork` isolating f operators across catalog | BFT-comparison.md Table 3 "Sustained partition > absorption window" — must miss cleanly, no safety violation. |
| ~~`TestSweep_LivenessEdge`~~ | ~~Paired runs at partial-synchrony boundary~~ | **Dropped.** The simulator's qbft.Instance emits without per-emission scheduling slack, so the simulator's R1 actual completion is ~4·BTT instead of the doc's recommended-sizing 8·BTT. The cliff the simulator can probe (BTT≈666ms, where 3·BTT collides with `RT=2s` and partitions quorum across rounds) sits far above any production-relevant BTT, and doesn't match the doc's deadline-driven cliff (≈487ms with 8·BTT R1). Clean-miss invariants are already covered on more meaningful axes by `TestSweep_Partition`, `TestSweep_Asymmetric` and the `MultiSilent_K3` catalog cell. |
| `TestSweep_OutOfEnvelope` | BTT > deepest-layer absorption (`B_{K-1} × BTT > 4000ms`) | All protocols miss at out-of-envelope BTT; no safety violation. (Existing `TestSweep_BTT` at BTT=400ms logs misses but doesn't assert clean-miss; this test makes the assertion explicit.) |

#### Tier 3 — Active-byz grief (safety-focused, planned for later)

Active byz that *intentionally* deviates from protocol to widen the slot-miss surface or attempt safety violations. Existing catalog covers headline patterns; these are combinations and statistical exploration.

| Test | Description |
|---|---|
| `TestSweep_AdversarialTiming` | `ByzLateLeaderBroadcast` × `JitteredDelay` sized to just-exceed `B_k` at one honest receiver — exercises the spec's adversarial-byz analysis edge case. |
| `TestSweep_PartitionByz` | Partition aligned with byz subset (f isolated + f byz on the surviving side). Combined network × adversarial inside the f-bound; safety must hold. |
| `TestSweep_LargeSeeds_Catalog` | 50-100 seeds × full catalog under jittered network. Gated behind `-tags=long`. Probabilistic safety validation. |
| `TestSoak_MultiSlot` | Sequential 50-200 slots, mixed byz/honest leaders per slot. Verifies retention-state cleanup (`O(K · n)` bound), no resource leaks, rational-byzantine deterrent across slots. **Pre-requisite**: multi-slot framework. |

#### Out-of-scope (explicit non-additions)

- **L_Bid extension** — until implementation lands. Spec'd in OBFT.md Appendix B; catalog scenarios deferred.
- **Coverage-guided fuzzing of consensus state** — `message/validation/obft_admissions` already covers the validation layer with coverage-guided fuzz; consensus-layer state-space fuzzing is high-cost vs. value-add given existing safety panics.

#### Recommended execution order

1. **`TestSweep_Jitter`** — single test function, exercises existing primitive (`JitteredDelay`), biggest surface gain per LOC.
2. **`TestSweep_Asymmetric`** + **`TestSweep_Partition`** — same primitive class as (1); builds out network-stress dimension.
3. **`TestSweep_ClockSkew`** — required ("MUST"); needs SimConfig + adapter wiring for per-op virtual clock; prep work but conceptually clean.
4. **`TestSweep_PassiveByz_UnderStress`** — combines (1)/(2)/(3) with the "byz-equivalent-to-honest-failure" catalog subset.
5. ~~**`TestSweep_LivenessEdge`**~~ (dropped — see Tier 2 table) + **`TestSweep_OutOfEnvelope`** — pinpoint cliff-edge.
6. Tier 3 items deferred until Tier 1 + 2 ship and we see what they surface.

Each Tier-1/2 addition is a single test function (~30-50 LOC) in `sweep_test.go`. The framework primitives all exist (network, host, byz); clock-skew is the only one that needs new infra — a per-op `time.Duration` offset honored by the OBFT/QBFT virtual-time clocks.

---

## Goal

Build a virtual-time discrete-event simulator that runs **multiple consensus protocols** (OBFT and QBFT, with the API ready for OBFTR / 2abOBFT later) under a **shared scenario catalog**, with **universal safety invariants enforced** per simulation. Output: scenario × protocol comparison matrices generated from real code execution, plus enforcement of safety / termination / agreement properties as defense-in-depth.

## Why a unified framework

Today's `protocol/v2/obft/stresstest` is OBFT-specific. It can't validate cross-protocol claims in `docs/BFT-comparison.md` because there's no way to run the same scenario through QBFT. A unified framework lets us:

- Validate the comparison-doc tables against actual code rather than RTT-count approximations.
- Enforce safety invariants (no two full V signatures; termination; agreement) on every protocol equally.
- Test new protocols (OBFTR, 2abOBFT) in the future without reinventing the harness.

## Architecture

```
protocol/v2/consensustest/                    ABSTRACT FRAMEWORK
  ├── protocol.go        Protocol interface, SimConfig, Outcome, Result
  ├── scenario.go        Scenario interface, registry of canonical scenarios
  ├── network.go         NetworkModel (constant / jittered)
  ├── host.go            HostPattern
  ├── byz.go             abstract ByzPattern semantics
  ├── safety.go          universal SafetyReport (single-V, termination, agreement)
  ├── outcome.go         Outcome, ExpectClass, classifier
  ├── runner.go          RunScenarioOnProtocol(p, s, cfg) → Result
  ├── matrix.go          MatrixReport: scenario × protocol → Outcome
  └── *_test.go          framework-level tests

protocol/v2/consensustest/obft/               OBFT ADAPTER
  ├── adapter.go         implements consensustest.Protocol (wraps obft.Instance)
  ├── translate.go       maps abstract scenario → obft.Instance event stream
  ├── des.go             discrete-event scheduler (lifted from existing stresstest)
  └── adapter_test.go    OBFT-specific assertions (that the adapter respects the abstract contract)

protocol/v2/consensustest/qbft/               QBFT ADAPTER
  ├── adapter.go         implements consensustest.Protocol (wraps qbft.Controller + Instance)
  ├── translate.go       maps abstract scenario → qbft message stream
  ├── des.go             DES that drives qbft via virtual timer + fake network
  ├── timer.go           virtual round timer (replaces real time.AfterFunc)
  └── adapter_test.go

protocol/v2/consensustest/comparison_test.go  CROSS-PROTOCOL TESTS
                          iterates protocols × scenarios → reports & assertions
```

## Phased implementation

### Phase A — abstract framework (consensustest core)

**Types:**

```go
// SimConfig is the algorithm-agnostic input. Protocols translate fields into
// their own internal config.
type SimConfig struct {
    N                     int                 // cluster size; F = (N-1)/3 implied
    Operators             []OperatorID        // 1..N
    SlotStart             time.Duration       // anchor; usually 0
    SlotDuration          time.Duration       // 12s for Ethereum; protocol uses subset
    RelayCutoff           time.Duration       // hard submission deadline (4s for proposer)
    HeaderSubmitHeadroom  time.Duration       // 100ms at operating point

    BTT                   time.Duration       // broadcast trip time = D + δ; protocols derive their windows from this
    Network               NetworkModel
    Host                  HostPattern
    Byz                   ByzPattern          // abstract pattern; protocols translate

    Seed                  int64
    TraceEnabled          bool

    // Optional: BLSKeys, when non-nil, switches the sim to real BLS.
    // Protocols that don't need it (e.g. would use stub instead) ignore.
    BLSKeys               *BLSKeys
}

// Protocol is what we test. Stable interface; per-protocol adapters live under
// consensustest/{name}/.
type Protocol interface {
    Name() string                              // "OBFT", "QBFT", ...
    Run(cfg SimConfig) (Outcome, error)
}

// Outcome is the algorithm-agnostic per-sim result.
type Outcome struct {
    Decided           bool
    DecisionTime      time.Duration            // earliest cluster-wide decision
    DecidedValue      []byte
    DecidedRound      int                      // round / layer the cluster decided at
    PerOp             map[OperatorID]OperatorOutcome
    Trace             []TraceEntry             // event log if TraceEnabled
}

// OperatorOutcome captures one operator's view.
type OperatorOutcome struct {
    Decided      bool
    Value        []byte
    Round        int                           // round / layer
    Time         time.Duration
    Err          string
    EvidenceCount int                          // protocol-specific, opaque
}

// SafetyReport is computed from Outcome by RunScenarioOnProtocol. Any false →
// panic (these are the protocol's load-bearing safety claims).
type SafetyReport struct {
    SingleV           bool                     // ≤ 1 distinct (Round, Value) reconstructed cluster-wide
    Terminated        bool                     // all operators reached an end-state (decided or missed); no deadlock
    HonestAgreement   bool                     // among operators that decided, all agree on (Round, Value)
}
```

**ByzPattern** is a small abstract enum-with-params:

```go
type ByzPattern struct {
    Kind       ByzKind        // None / SilentLeader / EquivocateLeader / ...
    PrimaryByz OperatorID     // which operator misbehaves; 0 = none
    Params     map[string]any // pattern-specific (e.g., recipient sets for selective delivery)
}

type ByzKind int
const (
    ByzNone ByzKind = iota
    ByzSilentLeader                            // primary leader doesn't broadcast
    ByzMultiSilent                             // top k leaders silent (Params["k"])
    ByzEquivocate111                           // primary delivers 3 distinct V's
    ByzEquivocateAllNR                         // primary floods all V's to all
    ByzEquivocateSigmaLockedSplit              // primary 1-1 split delivery
    ByzHV1SelectiveDelivery                    // OBFT-specific; QBFT returns ExpectNotApplicable
    ByzFakeEncryptedPresence                   // OBFT-specific
    ByzSigmaRefusal                            // byz never contributes σ; never NRs
)
```

Each protocol's adapter inspects `Byz.Kind` and translates to its own internal byz model. For OBFT-specific kinds, QBFT's adapter returns `(zero Outcome, ErrNotApplicable)` and the test framework records this as `n/a` rather than failing.

**Scenario** wraps a `SimConfig` modifier and per-protocol expected outcome:

```go
type Scenario struct {
    Name    string
    Apply   func(*SimConfig)                   // mutates Byz / Host / Network / etc
    Expect  map[string]ExpectClass             // keyed by Protocol.Name()
    DocRef  string                             // section in BFT-comparison.md / OBFT.md
}

type ExpectClass int
const (
    ExpectSuccessFastest ExpectClass = iota    // succeeds at first opportunity
    ExpectSuccessFallThrough                   // succeeds at deeper layer / later round
    ExpectMiss                                 // slot misses cleanly (no safety violation)
    ExpectNotApplicable                        // scenario doesn't translate to this protocol
    ExpectSuccessOrMiss                        // outcome depends on byz timing; either is OK
)
```

**RunScenarioOnProtocol** is the one entry point:

```go
func RunScenarioOnProtocol(t *testing.T, p Protocol, s Scenario, base SimConfig) Result {
    cfg := base
    s.Apply(&cfg)
    out, err := p.Run(cfg)
    if err != nil { ... }
    safety := computeSafetyReport(out)
    if !safety.SingleV {
        t.Fatalf("SAFETY VIOLATION (panicking sim): %s", safety)  // or panic(); the test must not pass
    }
    if !safety.Terminated { ... }
    if !safety.HonestAgreement { ... }
    expect := s.Expect[p.Name()]
    if !match(out, expect) { ... }
    return Result{Outcome: out, Safety: safety, Expected: expect}
}
```

### Phase B — OBFT adapter (migrate existing harness)

The existing `protocol/v2/obft/stresstest/` is mostly already this shape. The migration:

1. Move `harness.go` core (DES, sim struct, event types) → `consensustest/obft/des.go`. Replace `SimConfig` field with `consensustest.SimConfig`; field-by-field translation.
2. Move `byz.go` patterns → `consensustest/obft/translate.go`. Each pattern matches a `consensustest.ByzKind` and constructs the existing OBFT-internal byz overrides.
3. Move `events.go` → `consensustest/obft/des.go` (alongside the DES core).
4. `outcome.go` → translate `obft.Outcome` to `consensustest.Outcome`.
5. The 6 existing tests in `stress_test.go` get rewritten to use the abstract framework: each test just invokes `RunScenarioOnProtocol(obftAdapter, scenarioX, baseCfg)`.
6. `protocol/v2/obft/stresstest/` is deleted (or kept as a shim that just re-exports the adapter; cleaner to delete).

Existing OBFT-specific concerns that need to live somewhere:
- Real BLS support (existing `BLSKeys` infrastructure). Lift to `consensustest.BLSKeys`; both OBFT and QBFT adapters use it.
- Per-layer FetchAt / per-layer absorption — only meaningful to OBFT. The OBFT adapter computes these from `cfg.BTT` per OBFT.md §Application operating point.

### Phase C — QBFT adapter (new)

The hard part. Need to wrap real `qbft.Controller` + `qbft.Instance` under virtual time.

**Components:**

1. **Virtual round timer** (`consensustest/qbft/timer.go`). Implements `ssv.QBFTRoundTimer` interface but, instead of `time.AfterFunc`, registers timeout events with the DES scheduler. Each instance has one timer; on `TimeoutForRound(round)`, the timer queues an event at `now + roundTimeoutOffset` that, when fired, calls back into `OnQBFTRoundTimeout`. The virtual timer takes `now` from the DES scheduler.

2. **Fake network adapter**. Each operator's `Network.Broadcast(msgID, signed)` posts the SignedSSVMessage to the DES; DES schedules per-receiver delivery events at `now + BTT` (or jittered). On delivery, the DES calls `instance.ProcessMsg` on the recipient.

3. **Real BLS for QBFT signing**. QBFT messages include BLS signatures over hashes; we need real keys (same `consensustest.BLSKeys` from Phase B's lift). Operator-identity signing also: QBFT's outer SignedSSVMessage requires an operator-key signature; for the harness, can use a stub OperatorSigner that signs with a deterministic identity tag (verification can also be stubbed).

4. **DES driver** (`consensustest/qbft/des.go`). Per-slot virtual-time loop:
   - At slot start: create `qbft.Controller` for each operator with the virtual round timer.
   - At BFT_start (= 900ms per operating point): each operator's controller calls `StartNewInstance`. The proposer of round 1 broadcasts PROPOSE.
   - DES schedules:
     - Network deliveries (PROPOSE → PREPARE → COMMIT cascade)
     - Round timeouts (= RT after instance start)
     - Round-change cascade if timeouts fire
   - Decision: when an operator reaches `IsDecided() == true`, record. Continue until all operators decided or relay deadline.
   - Post-consensus: simulate the partial-sig collection — each decided operator sign-and-broadcast their post-consensus partial sig; cluster aggregates qV; final signature reconstructed at `decision time + 1 BTT`.

5. **Byz translation**. The abstract `ByzKind` patterns map to QBFT-internal byz behaviors:
   - `ByzSilentLeader` → byz proposer of round 1 doesn't broadcast PROPOSE; cluster falls through to round-change → round 2.
   - `ByzMultiSilent` → byz controls operator silent across multiple rounds; cluster keeps round-changing.
   - `ByzEquivocate111` → byz proposer broadcasts 3 different PROPOSEs to 3 different honest. Honest send conflicting PREPAREs; quorum can't reach on any V; round-change to round 2; new leader proposes fresh V.
   - `ByzEquivocateAllNR` (OBFT semantics: byz floods both V's to all honest) → for QBFT, this maps to "byz proposer sends both V_a and V_b to all honest" — honest see two PROPOSEs and can detect equivocation; same outcome as 1-1-1 (R2 fresh-V).
   - `ByzHV1SelectiveDelivery` → return `ErrNotApplicable` (QBFT's round-change makes this a non-issue).
   - `ByzFakeEncryptedPresence` → return `ErrNotApplicable`.
   - `ByzSigmaRefusal` → byz never broadcasts PREPARE/COMMIT; cluster proceeds with N-1 honest if quorum reachable (qV=3 at n=4 means 3 honest can decide).

6. **QBFT-side outcome → consensustest.Outcome**. Decided height = OBFT's "Layer" analog (here: `Round`). Decision time = wall-clock from slot start when controller's instance reached `Decided`.

7. **Operator-signer stub**. Need a `ssvtypes.OperatorSigner` for the controller. Use a stub that produces deterministic byte tags; real-BLS-signed verification at the runner level isn't exercised in our tests because we drive `ProcessMsg` directly on the controller (skipping the SSV runner's outer-message verification).

**Estimated scope**: ~600-800 LOC for the QBFT DES + adapter + timer, plus rough parity with OBFT's translation table.

### Phase D — abstract scenarios catalog (consensustest/scenario.go)

Concrete scenarios with declared per-protocol expectations:

| Scenario | OBFT | QBFT |
|---|---|---|
| `Healthy` | `ExpectSuccessFastest` | `ExpectSuccessFastest` |
| `PrimaryLeaderSilent` | `ExpectSuccessFallThrough` (in-round, V_1 in ~600ms) | `ExpectSuccessFallThrough` (R2 in ~3.7s) |
| `MultiLeaderSilent_K1` (top 1 silent) | same as above | same |
| `MultiLeaderSilent_K3` (top 3 silent) | `ExpectSuccessFallThrough` (V_3 in-round) | `ExpectMiss` (R-budget exceeded; needs ≥ 3 round-changes; 3 × 2s = 6s > 4s) |
| `LeaderEquivocates_111` | `ExpectMiss` | `ExpectSuccessFallThrough` (R2 fresh V) |
| `LeaderEquivocates_AllNR` | `ExpectSuccessFallThrough` (V_1) | `ExpectSuccessFallThrough` (R2) |
| `LeaderEquivocates_SigmaLockedSplit` | `ExpectMiss` | `ExpectSuccessFallThrough` (R2) |
| `HV1SelectiveDelivery` | `ExpectMiss` | `ExpectNotApplicable` |
| `FakeEncryptedPresence` | `ExpectSuccessFallThrough` + Rule 4 evidence | `ExpectNotApplicable` |
| `ValidityDivergence_AlgebraicLimit` (#NV = N-2f) | `ExpectMiss` | `ExpectSuccessOrMiss` (depends on whether re-org happens between R1 and R2) |
| `Partition_Above_B0` (partition delay > V_0's budget) | `ExpectSuccessFallThrough` (V_1 absorbs) | `ExpectSuccessFallThrough` (R2) |
| `Partition_Above_B3` (delay > V_3 budget) | `ExpectMiss` | `ExpectMiss` |

Each scenario's `Apply` configures `Byz`, `Host`, `Network`. Tests use `RunScenarioOnProtocol`.

### Phase E — comparison matrix test

```go
func TestComparison_Matrix(t *testing.T) {
    base := defaultProposerDutyConfig()  // BTT=200ms, Relay_cutoff=4s, etc.
    protocols := []consensustest.Protocol{
        obft.Protocol{},
        qbft.Protocol{},
    }
    scenarios := consensustest.Catalog
    matrix := consensustest.NewMatrixReport(protocols, scenarios)
    for _, p := range protocols {
        for _, s := range scenarios {
            r := consensustest.RunScenarioOnProtocol(t, p, s, base)
            matrix.Record(s, p, r)
        }
    }
    t.Log(matrix.Render())
    // also: assert matrix matches BFT-comparison.md classifications
}
```

Output:

```
                                | OBFT          | QBFT          | Notes
Healthy                         | ✓ 1.4s        | ✓ 1.5s        |
PrimaryLeaderSilent             | ✓ 1.4s (V_1)  | ✓ 3.7s (R2)   |
MultiLeaderSilent_K3            | ✓ 1.4s (V_3)  | ✗ miss        | OBFT in-round wins
LeaderEquivocates_111           | ✗ miss        | ✓ 3.7s (R2)   | QBFT R2 fresh V
HV1SelectiveDelivery            | ✗ miss        | n/a           |
FakeEncryptedPresence           | ✓ + Rule 4    | n/a           |
ValidityDivergence_AlgebraicLimit | ✗ miss        | ✓ or ✗        |
Partition_Above_B3              | ✗ miss        | ✗ miss        | both out of envelope
```

### Phase F — universal invariants enforcement

`consensustest.RunScenarioOnProtocol` panics on any safety violation:
- Two distinct (Round, Value) reconstructed across operators.
- Operator's state didn't terminate (DES queue still has events past slot deadline + headroom).
- Honest deciders disagreed on (Round, Value).

Invariants run on every sim regardless of `Expect`. They're the protocol's load-bearing claims and must never fail.

## Migration of existing tests

`protocol/v2/obft/stresstest/` becomes `protocol/v2/consensustest/obft/` (the OBFT adapter). Existing tests are rewritten as scenarios + matrix entries.

The 4 BLS-backed equivocation tests + Rule 4 + h_V=1 tests stay (they exercise real crypto + scenarios specific to OBFT) but are rewritten to use `RunScenarioOnProtocol(obftAdapter, scenario, baseCfg)`. The "T1_Healthy", "BudgetReport" diagnostic tests get folded into the comparison matrix output.

The `bftStartAnchored` helper is replaced by `consensustest.OperatingPoint` which produces a baseline `SimConfig` from a chosen BTT (per OBFT.md §Application).

## Implementation order (concrete)

1. **Phase A** — define `consensustest/` core types + safety checker (~300 LOC).
2. **Phase B** — OBFT adapter (lift existing stresstest into `consensustest/obft/`; ~600 LOC migration + ~150 LOC adapter glue).
3. **Phase C** — QBFT adapter (~700 LOC: timer + DES + translate + adapter).
4. **Phase D** — Scenario catalog (~250 LOC, mostly tabular).
5. **Phase E** — Comparison matrix test (~200 LOC).
6. **Phase F** — Universal invariants in safety.go (~200 LOC; runs as part of Phase A but written here for emphasis).
7. Delete `protocol/v2/obft/stresstest/` after Phase B passes.

Total: ~2200 LOC (vs current 1900 in `obft/stresstest/`). Roughly 1.5× current footprint, doubled scope.

## Open questions / risks

- **QBFT post-consensus partial-sig collection**: this happens in the SSV runner, not in `qbft.Instance` itself. Our DES needs to model it (~250ms = 1 BTT after consensus). I'll model it as a virtual sub-phase that fires after `IsDecided() == true`.
- **QBFT operator-signing stubbing**: a stub OperatorSigner that produces deterministic byte-tags should be enough; QBFT internally verifies operator signatures only at the controller-level dispatch. If the controller insists on verifying outer signatures, real BLS at operator-level becomes required (extra cost).
- **Determinism**: QBFT's `time.AfterFunc`-based timer is the only real-clock dependency. The virtual-timer adapter needs to fully replace it. Risk: there's a path where QBFT calls `time.Now()` indirectly that we miss. Defense: write a sanity test that runs the same seed twice and asserts byte-identical event traces.
- **Real BLS performance**: each sim with real BLS adds ~50-100ms vs ~0.1ms stub. Default tests use stub for both protocols; real-BLS spot-checks for safety-invariant verification at a small N.
- **Scenario faithfulness**: some abstract scenarios may not translate cleanly. e.g. `LeaderEquivocates_AllNR` is an OBFT-specific shape; what does it mean for QBFT? I'm mapping it to "byz proposer sends two PROPOSEs to all honest", which produces R2-recovery in QBFT — close enough but not identical semantics. The framework should flag scenarios where the translation is approximate (via a `ScenarioNote` field).

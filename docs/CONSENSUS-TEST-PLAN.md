# Consensus stress-test framework — plan

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
| `ValidityDivergence_2_2` | `ExpectMiss` | `ExpectSuccessOrMiss` (depends on whether re-org happens between R1 and R2) |
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
ValidityDivergence_2_2          | ✗ miss        | ✓ or ✗        |
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

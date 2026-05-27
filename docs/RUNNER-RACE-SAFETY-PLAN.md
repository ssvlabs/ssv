# Runner Race-Safety Bridge Plan

Design plan for verifying the OBFT spec's safety invariants under real production-runner goroutine scheduling. Today's `consensustest` DES is single-threaded by design — its 10 safety invariants (`NoOfflineDoubleV`, `HonestCrossPhaseExclusive`, `HonestSingleSigmaV`, `HonestWalkConsistent`, `QuorumBackedDecision`, `OBFTHostValidityRespect`, etc.) are exercised across millions of seeds but only verify "safety holds for valid message sequences," not "safety holds when production goroutine scheduling produces the sequence." This plan bridges the safety machinery into the existing production-runner integration tests (`protocol/v2/ssv/runner/obft/runner_test.go`), under `-race -count=N -cpu=X,Y,Z` amplification.

## Goal

A new `runner-safety-stress` Makefile target that:

1. Runs OBFT base + 2abOBFT runner cluster tests at every (n, K) cell in {n=4: K=2,3,4} ∪ {n=7: K=3..7} (8 cells total), with real goroutines (existing fixtures parameterized in commits 2 + 4).
2. Exercises a canonical 3-scenario set per protocol: Healthy, LateCommit, SilentL0Leader_NRFallThrough (see [§Scenarios](#scenarios) below — commit 3 builds the missing SilentL0Leader_NRFallThrough on the OBFT base side; commit 5 builds the missing LateCommit on the 2abOBFT side). OpportunisticTiming has identical wire shape to Healthy and was dropped during commit-3 self-review — see the file-level design note in `race_safety_bridge_test.go`.
3. Taps the broadcast bus to record every wire emission.
4. After each slot, reconstructs a `ct.Outcome` from (recorded wire trace + per-Instance `LastResolveLayerAttempts` + per-op Controller state).
5. Calls `ct.ComputeSafetyReport` on the reconstructed `Outcome` and asserts `IsViolation() == false`.
6. Runs the whole thing under `-race -count=80 -cpu=1,4,8` so timing-pressure variation amplifies race-window exposure (~38 min wall on the implementer's hardware, measured in commit 7's calibration sweep — comfortably under the 120m Makefile timeout; see [§Iteration count](#iteration-count--measured-38-min-default-25h-nightly-deep)).

Combines the two earlier options I sketched (A: stress-amplify existing tests; B: real-cluster integration test) — neither alone surfaces "safety under races" because the existing tests assert only `no error`, not the deep invariants. The bridge supplies the missing assertion.

## Scenarios

The bridge wraps **three canonical scenarios per protocol**, each exercising a distinct runner-side path. The scenario set is the *union* of what OBFT base and 2abOBFT currently have, mirrored across both protocols for consistency (some scenarios already exist on one side and need to be built on the other — see [§Scenario convergence](#scenario-convergence) below).

- **Healthy** (`TestRunProposerSlot_Healthy_*`): clean run with all Phase-1 bundles arriving on time, all hosts validating, no late commits. Verifies the runner correctly drives the protocol through Phase 1 → Phase 2 → Phase 3 under nominal conditions. Decision lands at L_0.
- **LateCommit** (OBFT base: `TestRunProposerSlot_LateCommit_OpportunisticResolve`; 2abOBFT: `TestRunProposerSlot_RealBLS_LateCommit_Matrix`): a configurable subset of the σ-pool-fill messages to op1 (`n - qV + 1` peers, parameterized over cluster size) is deliberately delayed past the soft Phase-3 deadline. **The delayed wire-kind differs by protocol**: OBFT base delays `KindCommit` (carries σ partials there) past RoundEndOffset (T_commit + Δ_2 + ε_3); 2abOBFT delays `KindValue` (carries `ValueMsg.L0Partial` — the σ-side terminal emission) past Phase-2a's fire time. Both exercise the same opportunistic-resolve poll semantics — the runner must re-Resolve when the late σ-pool fills past the soft deadline. Catches a regression where the runner stops polling at the soft deadline.
- **SilentL0Leader_NRFallThrough** (`TestRunProposerSlot_SilentL0Leader_NRFallThrough`): the L_0 leader's Phase-1 bundle is suppressed before broadcast (or arrives so late it's past T_commit at every receiver). All non-leader ops NR-emit at L_0 per the silent-leader rule, NR-quorum unlocks chain, L_1's leader broadcast carries the decision. Verifies the runner correctly drives the deeper-layer recovery path under real concurrency.

All three are wrapped by the bridge → the 10 safety invariants must hold on the reconstructed Outcome regardless of which runner path the scenario exercises.

**Why not a 4th OpportunisticTiming-bridge scenario**: the existing `TestRunProposerSlot_OpportunisticTiming_NoDelta2Wait` asserts a TIMING property (submission < T_commit + Δ_2). Its WIRE shape is identical to Healthy under non-regression code — a bridge variant would assert safety on identical wire output, providing zero incremental coverage. The original commit-3 self-review caught this and dropped the redundant scenario.

### Scenario convergence

OBFT base and 2abOBFT runner tests started with asymmetric scenario coverage. The convergence picture (post commits 3 + 5):

| Scenario | OBFT base | 2abOBFT |
|---|---|---|
| Healthy | ✓ existing (`TestRunProposerSlot_Healthy_n4_K4`); matrix wrapper in `TestSafetyBridge_OBFT_Healthy` | ✓ existing (`TestRunProposerSlot_Healthy_n4`); matrix wrapper in `TestRunProposerSlot_RealBLS_Healthy_Matrix` |
| LateCommit | ✓ existing (`TestRunProposerSlot_LateCommit_OpportunisticResolve`); matrix wrapper in `TestSafetyBridge_OBFT_LateCommit` | ✓ **built in commit 5** (`TestRunProposerSlot_RealBLS_LateCommit_Matrix` — delays `KindValue`, not `KindCommit`; see [§Scenarios](#scenarios)) |
| SilentL0Leader_NRFallThrough | ✓ **built in commit 3** (`TestSafetyBridge_OBFT_SilentL0Leader`) | ✓ existing (`TestRunProposerSlot_RealBLS_SilentL0Leader_NRFallThrough`); matrix wrapper in `TestRunProposerSlot_RealBLS_SilentL0Leader_NRFallThrough_Matrix` |

Building the missing scenarios was mechanical — each was a small variation on existing scenarios (silent-leader suppression in the broadcast bus, late-σ-pool-fill predicate). The protocol-level paths already existed; only the runner-test fixtures needed adding. Commit 6 wraps every scenario with the safety bridge on the 2abOBFT side; OBFT base bridges are already in place via commits 1-3.

## Why

Coverage matrix today vs. post-plan:

| Layer | Real goroutines? | Real protocol code? | Deep safety check? | Probabilistic sampling? |
|---|---|---|---|---|
| `consensustest` DES (Phase 1+2 work) | ✗ | partial (`obft.Instance` + adapter; no runner) | ✓ (10 invariants) | ✓ (~10⁶-10⁷ seeds/run) |
| Existing runner tests under `-race` | ✓ | ✓ (full runner + Instance) | ✗ (only `no error`) | ✗ (single timing) |
| **New target (this plan)** | ✓ | ✓ | ✓ (10 invariants applied to reconstructed Outcome) | ✓ (`-count=N` × `-cpu=X,Y,Z`) |
| Docker 4-node cluster (`make docker-all`) | ✓ cross-process | ✓ | ✗ (manual / no automation) | ✗ |

The gap this closes: a regression where production goroutine scheduling produces a wire-state inconsistent with what the DES's serialized event loop would have produced. Concrete examples:

- A race between `evtCertArrival` and `evtResolveRerun` where both fire successful Resolve calls with different `LastResolveLayerAttempts` snapshots → trace doesn't match the cert. D1's case (b) would flag this if it saw both states; the DES can't model the race window.
- A race in `RunningInstance` mutex acquisition that lets two ObserveCommit calls interleave such that the final σ-pool view differs from any serialized ordering → could produce a NoOfflineDoubleV violation that the DES never sees.
- A race in cert broadcast that produces a cert without matching σ-quorum on the wire → C1's QuorumBackedDecision would flag it.

These regressions are unlikely under correct mutex discipline, but the race-detector alone only catches Go-level data races; semantic races that result in invalid protocol state are invisible to `-race`. The bridge applies the semantic check.

## Scope

In-scope:
- OBFT base runner (`protocol/v2/ssv/runner/obft/`) parameterized across n ∈ {4, 7} × K ∈ {f+1..N} = 8 (n, K) cells.
- 2abOBFT runner (`protocol/v2/ssv/runner/obft/twoab/`) same (n, K) matrix. Existing fixtures (`buildSmokeCluster` + `buildBLSCluster`) already accept any K via `ConfigOverrides.K`, but the only K values runtime-exercised by tests were K=2 (stub Healthy) and K=4 (real-BLS Healthy + SilentL0Leader); commit 4 adds matrix-driven verification across K ∈ {f+1..N} at both n=4 and n=7.
- 3-scenario canonical set per protocol (Healthy, LateCommit, SilentL0Leader_NRFallThrough) — convergence work in commits 3 (OBFT base SilentL0Leader_NRFallThrough) and 5 (2abOBFT LateCommit). See [§Scenarios](#scenarios) for why OpportunisticTiming is not bridged.
- 10 consensustest safety invariants applied to the reconstructed Outcome.
- New Makefile target with `-count=80 -cpu=1,4,8 -race` (default ≈ 38 min measured; nightly-deep variant at `-count=320` ≈ 2.5h measured). Sub-test count is 48 per outer iter (3 scenarios × 8 cells × 2 protocols).

Out of scope:
- QBFT / PSigs runners (the safety invariants are OBFT-family-specific).
- Docker cluster integration (cross-process; significantly higher infrastructure cost).
- Byzantine scenarios (production runner has no byz-pattern injection; the production test fixture is all-honest — that's the right shape for "verify safety under correct concurrent operation").
- Systematic scheduling exploration (Coyote / similar — separate project, much heavier).
- n=10, n=13 cells. (n=4 + n=7 hit the production-relevant small/medium subnet sizes; larger n is rarely deployed and would inflate wall-time without proportional coverage gain.)

## Decisions (resolved during design)

### Bridge construction — wire-tap + Instance introspection, not reuse-DES-recorders

The DES's recorders (`recordCommitToAggregator` etc. in `consensustest/obft/events.go`) are package-private and tied to the DES event types. The production runner emits the same underlying message types (`obft.Phase1Bundle`, `obft.Commit`, `obft.Certificate`) but via the production wire. Two routes to feed the consensustest framework's `OfflineAggregator`:

- **A**: export the DES recorders, call them from the production wire-tap.
- **B**: write a dedicated translator inside the new bridge package that takes a wire-recorded message and calls the `OfflineAggregator`'s public `Observe*` methods directly.

Pick **B**. Reasons:
- The DES recorders embed DES-specific assumptions (byz override hooks, claimed-vs-actual emitter divergence under `ByzAggregatorBypass`). Production wire has no byz layer — the actual emitter is always the network-layer identity of the sending node. Reusing the DES recorders would import unused complexity.
- The bridge becomes a thin, self-contained module with no dependency cycle into the DES test packages.
- Memory cost is one extra method-call site per message kind (~10 lines).

### Where Outcome.Byz comes from in production

`Outcome.Byz` defaults to zero-value `ByzPattern{}` — i.e., no byz operators, all ops honest. Correct: the production runner has no byz-pattern injection. The B1/B2/D1/C3 honest-filtering checks then treat every op as in-scope for the invariants. Any safety violation flagged is an honest-op regression (the only kind production should ever surface).

### Per-Instance `LastResolveLayerAttempts` capture timing

The existing getter is `Instance.LastResolveLayerAttempts() []LayerAttempt`. Question: at what point in the test should the bridge call it?

- **A**: at end-of-slot, after every op's `RunProposerSlot` returns. Captures the FINAL trace per op — equivalent to the DES adapter's behavior in `consensustest/obft/des.go`'s `outcome()`.
- **B**: continuously, snapshot after every Resolve call. More information but adds intrusion.

Pick **A**. The D1 check semantics match the DES — last-Resolve's trace, captured at end-of-sim. Matches `consensustest`'s contract documented in `obft/base/phase3.go`'s `LastResolveLayerAttempts` getter.

### Per-Instance access from the runner test

The existing `runnerNode` struct exposes `ctrl *Controller`. The Controller holds `instances map[phase0.Slot]*RunningInstance`, and `RunningInstance` holds `instance *obftcore.Instance`. The bridge needs read access to the Instance (for `LastResolveLayerAttempts`).

Two routes:
- **A**: add an exported `Controller.InstanceForSlot(slot)` accessor that returns `*obft.Instance`. Touches production API.
- **B**: keep the access internal to the runner-test package (the test is already in `package obft`, which is the same package as Controller — so unexported fields are accessible).

Pick **B**. No production API change. The bridge code lives in `_test.go` files (or a `helpers_internal_test.go` shared file in the runner package), reads Controller.instances directly.

### Outcome.PerOp construction

DES adapter writes `OperatorOutcome` from the per-Instance state at end-of-sim (`obft/des.go`'s `outcome()`). The bridge mirrors this: for each runner node, capture:
- `Decided`: true iff the runner's submit hook fired (`hooks.submitted` non-empty).
- `Value`: the submitted Output's `Value`.
- `Round`: the submitted Output's `Layer`. Or -1 if the op caught up via cert (per DES adapter convention).
- `ResolveLayerAttempts`: from `Instance.LastResolveLayerAttempts()`.
- `Err`: empty string for successful ops; "missed relay deadline" / "no quorum" for unsuccessful (mirroring DES adapter's error stringification).

The cert-gossip-decide path needs special handling: production may decide an op via cert without a local σ-reached trace (same as DES). The D1 check's case (a) cert-gossip branch already handles this via `clusterLocalDecidedOn`.

### Cluster matrix — n ∈ {4, 7} × K ∈ {f+1..N}

n=4 → f=1 → K ∈ {2, 3, 4} (3 cells); n=7 → f=2 → K ∈ {3, 4, 5, 6, 7} (5 cells). Total 8 (n, K) cells. Spans the BFT-liveness floor (K=f+1) through the maximum-fall-through depth (K=n) for each cluster size.

Existing fixtures use n=4 / K=4 hardcoded; the bridge parameterizes via `t.Run("n4_K2", ...)` sub-tests calling `buildCluster(t, n, overrides)` with the cell's parameters. Same fixture, looped over the matrix.

n=10 and n=13 are excluded — n=4 + n=7 cover the production-relevant small / medium subnet sizes, and larger n would inflate wall-time without proportional coverage gain (the safety invariants are size-independent at the check level).

### Iteration count — measured ~38 min default, ~2.5h nightly-deep

Calibration sweep (commit 7) at the **3-scenario × 8-cell × 2-protocol = 48 sub-test** matrix on the implementer's machine (Apple Silicon dev laptop):

| Component | Cost (measured at `-count=2 -race`) |
|---|---|
| OBFT base (24 sub-tests) at `-cpu=1` | 17.7s |
| 2abOBFT (24 sub-tests) at `-cpu=1` | 11.8s |
| OBFT base at `-cpu=4` | 17.3s |
| 2abOBFT at `-cpu=4` | 9.9s |
| OBFT base at `-cpu=8` | 17.1s |
| 2abOBFT at `-cpu=8` | 9.7s |
| `-count=2 × -cpu=1,4,8` total wall | **56.8s** |

Per-iteration cost (one of the 3 cpu-points): ~28s for 48 sub-tests under `-race -count=1`. Extrapolations:

- **Default `-count=80`**: 56.8s × 40 ≈ **38 min** wall (under the original 1.5h target; ~half the conservative projection).
- **Deep `-count=320`**: 56.8s × 160 ≈ **2.5h** wall (well under the 5.9h projection; ~half the original estimate).

The implementer's machine is reasonably fast; slower CI hardware may push these toward 60-90 min default / 4-5h deep. Both numbers leave ample headroom against the Makefile's 120m / 360m timeouts. If a future calibration on slower hardware exceeds the timeout, raise the timeout flag in the Makefile rather than lower `-count` — the iteration count is the load-bearing parameter for race-window coverage.

The Makefile target accepts `SAFETY_STRESS_COUNT` as a per-run override (e.g., `SAFETY_STRESS_COUNT=10` for a 5-min smoke run during local development).

### Stress amplification config

- `-race` always on — catches Go-level data races.
- `-count=80` (default) / `-count=320` (nightly-deep) per scenario — each iteration is an independent goroutine-scheduling realization.
- `-cpu=1,4,8` varies GOMAXPROCS — different timing-pressure regimes surface different race windows. `-cpu=1` serializes everything (catches races that only fire under cooperative scheduling); `-cpu=4` matches typical CI environments; `-cpu=8` introduces realistic contention pressure.
- `-timeout=120m` (default) / `-timeout=360m` (nightly-deep) — generous headroom over the projection.

### Real-BLS coverage — inherited, no extra config needed

The runner test fixture at [`runner_test.go`](../protocol/v2/ssv/runner/obft/runner_test.go) wires `blsbackend.New(share)` into every node's signer unconditionally. There's no stub-BLS path on the runner — the production code imports `blsbackend` directly. So the race-safety bridge exercises real BLS aggregation + verification as a side effect of running the production runner, with no `real_bls` build-tag toggle needed.

This means the bridge complements (rather than duplicates) the existing `make consensustest-real-bls` target. The two cover different axes of the safety surface:

| Test target | Cardinality + hash safety checks (10 invariants) | Real BLS verify + aggregate | Real goroutine scheduling |
|---|---|---|---|
| `make stresstest` (DES, stub BLS) | ✓ (10⁶-10⁷ seeds — deep statistical sampling) | ✗ | ✗ |
| `make consensustest-real-bls` (DES, real BLS) | ✓ (medium-depth sampling — slower per-seed) | ✓ | ✗ |
| `make runner-safety-stress` (race bridge, real BLS, planned) | ✓ (shallow sampling — ~100s iters) | ✓ | ✓ |
| `make unit-test` (production runner under `-race`) | ✗ (only `no error` asserted today) | ✓ | ✓ |

Three orthogonal axes, three useful targets — no two are interchangeable:

- `stresstest` is the **mainstream workhorse**: deep statistical safety sampling under cheap stub crypto. Catches the bulk of protocol-structural regressions.
- `consensustest-real-bls` is the **crypto-correctness backstop**: same 10 invariants under real BLS. Catches stub-vs-real divergence (wrong domain tags, share indices, serialization round-trips, real-BLS aggregation order). Faster per-seed than the race bridge → deeper sampling for crypto-specific bugs (which are typically deterministic and don't need race amplification).
- `runner-safety-stress` is the **race-correctness backstop**: 10 invariants on real-runner-real-crypto-real-goroutine runs. Catches race-induced wire-state inconsistencies that neither single-threaded target can model. Slowest per-seed but covers the third regression class neither of the others touches.

Race-safety-stress *technically* subsumes `consensustest-real-bls`'s coverage (it runs real BLS too) but at much lower sample depth. Crypto bugs are usually deterministic — a wrong domain tag fails on every seed. So the DES real-BLS path remains worth keeping for its sample efficiency on the crypto-correctness axis.

### Makefile target naming — fix existing `.PHONY` mismatch as part of this work

[`Makefile:109-110`](../Makefile) has a `.PHONY: consensustest-real-bls` declaration but the rule body uses `consensustest-with-real-bls:` as the target name — they don't match, so `make consensustest-real-bls` doesn't actually work today (only `make consensustest-with-real-bls` does). Surgical cleanup as the first commit of this feature:
- Rename the rule to `consensustest-real-bls:` (matches the existing `.PHONY` declaration, drops the awkward `with-`, aligns with `stresstest-negative` / `runner-safety-stress` convention).
- Grep + update any references in CI configs / docs from `consensustest-with-real-bls` → `consensustest-real-bls`.

The new `runner-safety-stress` and `runner-safety-stress-deep` targets already follow the compound-clean convention; no other renames needed. `stresstest` stays one word (well-known, widely-referenced).

### Failure handling

When `ComputeSafetyReport.IsViolation()` returns true, the bridge calls the existing `ct.SafetyPanic` with the reconstructed Outcome — same structured diagnostic the DES uses, including seed-equivalent test-run identity. The test fails loudly with the full state dump.

### What NOT to assert

The bridge does NOT assert:
- Decision time (production timing is real-wall-clock; DES has compressed timing).
- Bandwidth metrics (not relevant for safety).
- Specific layer of decision (production may decide at a different layer than DES would for the same scenario due to real timing).

Only the 10 safety invariants. Those are timing-independent.

## Architecture

Three architectural components (the bridge module, the outcome reconstructor, the matrix-parameterized test wrapper) plus the Makefile target. The seven mandatory commits in [§Order of work](#order-of-work) carve these by surface area rather than by component (e.g., commit 1 builds bridge+reconstructor end-to-end for one cell; commits 2-3 scale OBFT base; commits 4-6 mirror to 2abOBFT including fixture extension + missing scenarios).

### Component 1 — `RecordingBroadcastBus` infrastructure

New file `protocol/v2/ssv/runner/obft/race_safety_bridge_test.go` (and twoab equivalent). Defines:

```go
// recordingBroadcastBus wraps the existing broadcastBus, intercepts every
// emission, decodes the wire bytes, and records the resulting OBFT
// message into a captured-wire list. Delegates to the inner bus
// otherwise.
type recordingBroadcastBus struct {
    inner    *broadcastBus
    mu       sync.Mutex
    captured []capturedEmission
}

type capturedEmission struct {
    from    spectypes.OperatorID  // genuine emitter (network-layer identity)
    kind    obftWireKind          // bundle / commit / certificate
    bundle  *obft.Phase1Bundle    // populated iff kind == bundle
    commit  *obft.Commit          // populated iff kind == commit
    cert    *obft.Certificate     // populated iff kind == certificate
}
```

The decode path: existing `broadcastBus.broadcast(from, data)` deserializes `data` into one of the OBFT message types via the existing wire-format decoder. The wrapper does the same decode + records before forwarding.

`runnerNode.hooks.broadcastFn` gets pointed at the wrapper instead of `bus.broadcast` directly.

### Component 2 — Outcome reconstruction

Same file: a `reconstructOutcome(nodes []*runnerNode, captured []capturedEmission) ct.Outcome` function.

```go
func reconstructOutcome(nodes []*runnerNode, captured []capturedEmission) ct.Outcome {
    n := len(nodes)
    agg := ct.NewOfflineAggregator(n)

    // Replay captured wire trace into the aggregator.
    for _, em := range captured {
        switch em.kind {
        case wireKindBundle:
            // Leader broadcast: ObserveSigma + ObserveSigmaByEmitter.
            agg.ObserveSigma(ct.OperatorID(em.bundle.OperatorID), em.bundle.Layer, em.bundle.Value)
            agg.ObserveSigmaByEmitter(ct.OperatorID(em.from), em.bundle.Layer, em.bundle.Value)
        case wireKindCommit:
            // Mirror obft/events.go recordCommitToAggregator's logic
            // (σ at L_0, EncryptedClaim at L_k>0, NR partials, witnesses).
            recordCommitWire(agg, em.from, em.commit)
        case wireKindCertificate:
            // Certs don't add to σ/NR pools; they just propagate the
            // already-reconstructed signature. No aggregator update.
        }
    }

    perOp := make(map[ct.OperatorID]ct.OperatorOutcome, n)
    for _, node := range nodes {
        out := node.submittedOutput()
        inst := node.ctrl.instanceForSlot(slot).instance // internal access
        var oo ct.OperatorOutcome
        if out != nil {
            oo.Decided = true
            oo.Value = out.Value
            oo.Round = out.Layer
        } else {
            oo.Round = -1
        }
        oo.ResolveLayerAttempts = convertLayerAttempts(inst.LastResolveLayerAttempts())
        perOp[ct.OperatorID(node.op)] = oo
    }

    return ct.Outcome{
        Decided:      anyDecided(perOp),
        DecidedValue: pickClusterValue(perOp),
        DecidedRound: pickClusterRound(perOp),
        PerOp:        perOp,
        OfflineAgg:   agg.AttemptAll(),
        Byz:          ct.ByzPattern{}, // production: no byz
    }
}
```

The `recordCommitWire` helper mirrors `consensustest/obft/events.go recordCommitToAggregator` logic but operates on the production wire type (which is the same `obft.Commit`). Both paths handle:
- `c.Layers[0]` plaintext σ → `ObserveSigma + ObserveSigmaByEmitter`
- `c.Layers[k>0]` encrypted onion → `ObserveEncryptedClaim + ObserveSigmaByEmitter`
- `c.NRPartials` → `ObserveNR + ObserveNRByEmitter`
- `c.Witnesses[]` → `ObserveSigmaByValueRoot` (claimed-sender path only — same as DES)

### Component 3 — Matrix-parameterized test wrapper

A helper that wraps each existing scenario in a matrix loop. The lifted shape:

```go
// Per-cell entry: (n, K) parameterization of a TestRunProposerSlot_* setup.
type matrixCell struct {
    n int
    K int
}

func obftMatrixCells() []matrixCell {
    // n=4 → f=1 → K ∈ {2, 3, 4}
    // n=7 → f=2 → K ∈ {3, 4, 5, 6, 7}
    return []matrixCell{
        {4, 2}, {4, 3}, {4, 4},
        {7, 3}, {7, 4}, {7, 5}, {7, 6}, {7, 7},
    }
}

func TestSafetyBridge_OBFT_Healthy(t *testing.T) {
    for _, cell := range obftMatrixCells() {
        t.Run(fmt.Sprintf("n%d_K%d", cell.n, cell.K), func(t *testing.T) {
            runScenarioWithSafetyCheck(t, cell.n, cell.K, scenarioHealthy)
        })
    }
}
// Same shape for TestSafetyBridge_OBFT_LateCommit and
// TestSafetyBridge_OBFT_SilentL0Leader.
```

`runScenarioWithSafetyCheck` is the bridge entry-point: builds the cluster at (n, K), runs the scenario, captures the wire, reconstructs the Outcome, asserts `ComputeSafetyReport.IsViolation() == false`. Single helper consumed by all three OBFT scenarios + mirror three 2abOBFT scenarios.

### Component 4 — Makefile target

As built in commit 7 (`make runner-safety-stress` / `make runner-safety-stress-deep`):

```makefile
SAFETY_STRESS_COUNT ?= 80
.PHONY: runner-safety-stress
runner-safety-stress:
	@echo "Running runner-safety stress (real goroutines + safety invariants + -race × -cpu=1,4,8 × -count=$(SAFETY_STRESS_COUNT))"
	@for cpu in 1 4 8; do \
		echo ">> -cpu=$$cpu"; \
		go test -tags blst_enabled -race -count=$(SAFETY_STRESS_COUNT) -cpu=$$cpu -timeout 120m \
			-run '^TestSafetyBridge_' \
			./protocol/v2/ssv/runner/obft/... || exit 1; \
	done

.PHONY: runner-safety-stress-deep
runner-safety-stress-deep:
	@$(MAKE) runner-safety-stress SAFETY_STRESS_COUNT=320
```

Notes:
- A single `./protocol/v2/ssv/runner/obft/...` path picks up both the OBFT base bridge (in the package itself) and the 2abOBFT bridge (in the `twoab` subpackage) — `...` recurses.
- `SAFETY_STRESS_COUNT` is overridable per-run (e.g., `SAFETY_STRESS_COUNT=10` for a fast smoke check; the deep variant just bumps it to 320).
- `runner-safety-stress-deep` re-enters via `$(MAKE)` so both targets share the exact same recipe — one source of truth for the test command.

Default ≈ 38 min wall on the implementer's hardware (measured); wire into nightly CI. The deep variant ≈ 2.5h; run on demand or scheduled weekly.

## Order of work

Seven mandatory commits + one optional. Split keeps each commit ≤ ~250 lines of net change and bisectable.

0. **Commit 0 — Makefile target naming cleanup**. Fix the existing `.PHONY: consensustest-real-bls` / rule-name `consensustest-with-real-bls` mismatch. Rename the rule to match the `.PHONY` declaration. Grep + update repo references (CI / docs). Surgical 1-2 line change in the Makefile + however many call sites exist. Independent of the bridge work but natural pre-cleanup.

1. **Commit 1 — Bridge foundation** (OBFT base, single-cell). New `race_safety_bridge_test.go` in `protocol/v2/ssv/runner/obft/`. Introduces:
   - `recordingBroadcastBus` wrapping the existing `broadcastBus` with per-emission decode + capture.
   - `recordCommitWire` translator (mirrors `consensustest/obft/events.go`'s `recordCommitToAggregator` shape but operates on the production wire's `obft.Commit` and routes to the `OfflineAggregator`'s public `Observe*` methods).
   - `reconstructOutcome(nodes, captured) ct.Outcome` helper.
   - One smoke test: `TestSafetyBridge_OBFT_Healthy_n4_K4` runs the Healthy scenario, reconstructs the Outcome, asserts `ct.ComputeSafetyReport.IsViolation() == false`. Validates the bridge end-to-end before scaling.

2. **Commit 2 — OBFT base matrix × existing scenarios**. Extend `compressedTestSchedule` to a parameterized `compressedTestScheduleForK(K)`. Parameterize the LateCommit delay-target predicate over (n, qV). Wrap two existing OBFT base scenarios (Healthy, LateCommit) in matrix-parameterized table-driven tests via `t.Run("n%d_K%d", ...)`. 8 cells × 2 scenarios = 16 OBFT base sub-tests at this point. (OpportunisticTiming was initially in scope here but dropped during commit 3's self-review — see [§Scenarios](#scenarios).)

3. **Commit 3 — OBFT base SilentL0Leader_NRFallThrough scenario** (convergence work). Build the OBFT-base-side equivalent of 2abOBFT's `RealBLS_SilentL0Leader_NRFallThrough`: suppress L_0 leader's bundle, verify NR-quorum unlocks L_1, decision lands at L_1. Wrap in matrix-parameterized table-driven tests via the bridge. 8 cells × 1 scenario = 8 more OBFT base sub-tests. Self-review during this commit identified that OpportunisticTiming has wire shape identical to Healthy (see [§Scenarios](#scenarios)) and dropped it. **OBFT base coverage complete: 24 sub-tests across the matrix × 3 scenarios.**

4. **Commit 4 — 2abOBFT cluster-matrix verification**. New file `cluster_matrix_test.go` in `protocol/v2/ssv/runner/obft/twoab/`. Introduces shared matrix helpers (`matrixCell`, `twoabMatrixCells`, `compressedTestOverridesForK` — analogous to OBFT base's `compressedTestScheduleForK` but returning a `*ConfigOverrides` since 2abOBFT auto-derives FetchAt/BroadcastBudget). Verifies the 2abOBFT runner works end-to-end at K > 2 (the protocol always supported it, but tests had only exercised K=2 stub + K=4 real-BLS) by matrix-parameterizing the three existing scenarios — `TestRunProposerSlot_Healthy_Matrix`, `RealBLS_Healthy_Matrix`, `RealBLS_SilentL0Leader_NRFallThrough_Matrix`. 8 cells × 3 scenarios = 24 non-bridge sub-tests. The existing single-cell tests are retained as faster smoke checks. (LateCommit comes in commit 5; the safety-bridge overlay in commit 6 uses the same matrix helpers.)

5. **Commit 5 — 2abOBFT LateCommit scenario** (convergence work). Build the 2abOBFT-side equivalent of OBFT base's LateCommit scenario. Both protocols share the same opportunistic-resolve poll semantics — after Phase-2a fires (or RoundEndOffset in OBFT base), the scheduler re-runs Resolve on every state-delta until the relay-submission cutoff (or success). The new scenario delays a (n, qV)-parameterized subset of KindValue arrivals at the victim op past Phase-2a's fire time (not KindCommit — in 2abOBFT KindValue carries the emitter's L_0 σ partial inline via `ValueMsg.L0Partial`, so delaying KindValue is what holds σ-pool below qV at the victim; KindCommit carries NR partials only). Extends the existing `blsBus` with a delay-aware variant (`newBlsBusWithDelay`) mirroring OBFT base's `newBroadcastBusWithDelay`. Helper named `lateValueDelayPredicate` to reflect the 2abOBFT-accurate wire kind; the test keeps the OBFT-parallel `LateCommit` scenario label for bridge-level parity in commit 6.

6. **Commit 6 — 2abOBFT bridge** (full matrix × 3 scenarios). New `race_safety_bridge_test.go` in `protocol/v2/ssv/runner/obft/twoab/`. Same architecture as commit 1: `recordingBlsBus` wrapping the real-BLS `blsBus` (the bridge uses the production-grade async-delivery path, matching OBFT base's choice of real-BLS `broadcastBus` over stub `smokeBus`). `recordValueMsgWire` / `recordNoValueMsgWire` / `recordCommitWire` translators mirror `consensustest/twoab/events.go`'s recorders — `KindPhase1Bundle` is intentionally NOT recorded (leader's σ contribution rides in its own `KindValue.L0Partial`; recording both would double-count). `scenarioConfig` uses a `silentFor` field returning a `map[OperatorID]bool` (matching `blsBus.silent`) rather than OBFT base's drop-predicate function — 2abOBFT's SilentL0Leader suppresses ALL outbound from L_0 leader, not just specific message kinds. Bridge wraps all 3 scenarios × 8 cells = 24 2abOBFT sub-tests. **Full coverage complete: 48 sub-tests across both protocols.**

7. **Commit 7 — `runner-safety-stress` Makefile target + iteration tuning**. New targets `runner-safety-stress` (`-count=80 -cpu=1,4,8 -race -timeout=120m`) and `runner-safety-stress-deep` (`-count=320`, shares the same recipe via `$(MAKE)` re-entry). `SAFETY_STRESS_COUNT` env var overrides `-count` for local smoke runs. Includes a one-time calibration sweep on the implementer's machine — measured ≈ 38 min default / ≈ 2.5h deep, both ~half the original projections. Numbers documented in [§Iteration count](#iteration-count--measured-38-min-default-25h-nightly-deep).

Optional follow-up commit (defer unless real-world findings motivate):

8. **Commit 8 — Adversarial timing injection** (Coyote-lite). Layer optional artificial pauses inside the `broadcastBus` (OBFT base) / `blsBus` (2abOBFT) delivery paths to amplify specific race-window classes (e.g., delay-cert-vs-resolve). Bounded scope; ~50 lines per bus. Both already support the `delayFn` injection point via commit 5's blsBus extension — commit 8 would systematically explore the delay space rather than the single-point delays in the LateCommit scenarios. Helps surface specific race classes that random scheduling under `-race` doesn't reliably hit.

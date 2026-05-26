# Consensustest Safety-Invariants Hardening Plan

Design plan for bringing stresstest safety coverage into line with the OBFT / 2abOBFT spec's per-operator structural invariants. Today's safety machinery catches end-state cluster-wide violations (`NoOfflineDoubleV`, `SingleV`) but is blind to the underlying per-operator EKM-claims that *prove* those end-states — and to Resolve-walk regressions whose effects don't materialize as a double-V reconstruction on a given seed. This plan implements four buckets that close those gaps.

## Goal

Add safety assertions to the consensustest framework so that:

1. The four already-defined-but-uninstrumented `CommitAttestation` fields (`QuorumChecked`, `OBFTCommitKindChecked`, `OBFTHostValidityChecked`, `NoEquivocationAccepted`) actually fire on regressions — not just sit at default-true.
2. Each honest operator's cross-phase exclusivity (σ-XOR-NR per layer) and single-σ-V per layer are checked **directly** rather than only via the transitive `NoOfflineDoubleV` end-state.
3. Each honest operator's Resolve-walk consistency (σ-quorum-reachable-at-L_k ⇒ decide at L_k, do not advance past it) is checked **directly** rather than only via `NoOfflineDoubleV`.
4. Each new assert has a paired negative test that proves the assert actually trips, so the new machinery itself doesn't silently regress (the same pattern as today's [`TestAdapter_ByzAggregatorBypass_TriggersSafetyDetection`](../protocol/v2/consensustest/obft/adapter_test.go) and `TestAdapter_ByzWitnessForgery_TriggersSafetyDetection`).

## Why

`make stresstest` runs ~10k+ Healthy seeds × ~100 scenarios × 13 protocol variants. The total signal is "did `NoOfflineDoubleV` or `SingleV` fire across this matrix?" — which catches the spec's headline cluster-wide claim (Pigeonhole 2/3) but is *transitive*: it depends on the regression actually producing two distinct reconstructable V's on the seed at hand. A regression that violates the per-operator EKM rules (Pigeonhole 1, single-σ-V) but happens not to produce a double-V on a given seed slips through undetected.

The spec's load-bearing claims live at the per-operator layer ([OBFT.md:411 §Preconditions](OBFT.md), §Cross-signing detection, §Safety / Pigeonhole 1):

- **Cross-phase exclusivity (B1)** — "an NR/NV partial on `nr_tag_k` is rejected if the same operator has previously signed σ at L_k, and vice versa". EKM-enforced at signing time; consensustest currently doesn't verify the EKM contract.
- **Single-σ-V (B2)** — "a σ partial on V' at L_k is rejected if the same operator has previously signed σ on V ≠ V' at L_k". Same shape.
- **Leader-σ locks σ-side (B3)** — "Each layer's leader's Phase-1 σ counts as their σ-side commitment — they cannot emit NR/NV for that layer." Special case of B1 with leader's Phase-1 σ treated as their layer-k σ commitment.
- **Walk-state consistency (D1)** — Resolve walks layers in order; σ-quorum at L_k returns immediately ([base/phase3.go:54-80](../protocol/v2/obft/base/phase3.go)). A reorder regression would slip silently as long as the cluster's end-state remained consistent.

Today's protocol-side EKM is enforced by [`transitionToSigma` / `transitionToNR`](../protocol/v2/obft/base/instance.go) returning errors on illegal transitions. The consensustest framework currently relies on these returning errors correctly; if the implementation regresses (e.g., a transition forgets to check `nrLocked` first) and the operator emits both σ and NR partials on the wire, today's safety report stays green unless the resulting partials line up into a double-V reconstruction.

This plan adds a **second, independent verification layer** — the framework checks the *evidence on the wire* against the spec's per-operator rules, rather than trusting the Instance's internal lock state.

## Scope

In-scope:
- Bucket 1 — `Outcome.Byz` snapshot, `OfflineAggReport.SigmaCardinality` plumbing, C2 descriptive-kind wiring in both adapters, plus a 2abOBFT recording-path bug fix discovered during implementation (see [§Decisions / 2abOBFT recording fix](#bucket-1-implementation-findings--c1c4-deferred-2abobft-recording-fixed)).
- Bucket 2 — per-op B1, B2, B3 checks in `ComputeSafetyReport`.
- Bucket 3 — per-op D1 walk-state assertion (single per-op decision-layer, σ-pool snapshot vs decision-layer consistency).
- Bucket 4 — paired negative test per new invariant.
- B5 (one-decision-per-op) — implemented as an adapter-side panic guard rather than a `SafetyReport` field; see [§Decisions / B5](#b5-one-decision-per-op---adapter-side-panic-not-safetyreport-field).
- New Makefile target `stresstest-negative` — runs all `*TriggersSafetyDetection` adapter tests + `TestSafety_Honest*` synthetic-outcome tests as a quick CI smoke; see [§Decisions / Test targets](#test-targets--add-stresstest-negative-makefile-smoke).

Out of scope (separate follow-ups):
- C1 (`QuorumBackedDecision`), C3 (`OBFTHostValidityRespect`), C4 (`NoEquivocationAccepted` real count) — implementation discovered each needs deeper protocol-side instrumentation than bucket 1's "wire what's already there" framing assumed. Deferral details in [§Bucket 1 implementation findings](#bucket-1-implementation-findings--c1c4-deferred-2abobft-recording-fixed).
- Bucket 5 — `ExpectEvidence` field on scenarios (slashing-evidence-must-fire when expected).
- Bucket 6 — `go-mutesting` harness with mutation-coverage CI.
- L_Bid mini-consensus invariants (out of current stresstest scope).

## Decisions (resolved during design)

### Bucket boundary — "wired but inert" vs "new plumbing"

Bucket 1's original working definition: **wire the data the adapter already sees post-sim**, without changing the protocol-side `obft.Instance` / `twoab.Instance` API. Implementation surfaced that three of the four targeted invariants actually need more than aggregator-only data — only C2 wires cleanly under that constraint. C1/C3/C4 deferrals are detailed in [§Bucket 1 implementation findings](#bucket-1-implementation-findings--c1c4-deferred-2abobft-recording-fixed).

- **C2 OBFTCommitKindValid** — derive from `Outcome.DecidedRound`: decision at L_0 ⇒ `"sigma"`; decision at L_k>0 ⇒ `"nr"`. Descriptive tag; the existing `OBFTCommitKindValid` check at [`safety.go:251-255`](../protocol/v2/consensustest/safety.go) only validates `kind ∈ {"sigma", "nr"}` so the (descriptively imprecise) witness-driven L_k>0 σ-quorum edge case can't trip the check.

C1, C3, C4 deferred — see findings section below.

### Bucket 1 implementation findings — C1/C4 deferred, 2abOBFT recording fixed

While implementing bucket 1 against actual scenarios, three deferrals surfaced:

**C1 QuorumBackedDecision** — the original plan was to count cluster-wide σ-pool partials at the decided (layer, V) from `OfflineAggReport.SigmaCardinality`. Implementation showed the aggregator's view is an UNDERAPPROXIMATION of the protocol's local view in several legitimate cases:

- At L_k>0, the protocol combines the leader's plaintext σ_V (delivered via Phase-1 bundle, recorded by the leader-broadcast handler into `SigmaPartials`) with chain-decrypted peer onion partials (recorded in `EncryptedClaims`). The aggregator's `SigmaCardinality` accounting adds these but doesn't reflect protocol-specific paths like witness-only L_k>0 σ-quorum, opportunistic resolve, or cert-gossip catch-up.
- Under partition (e.g., `TestSweep_Partition`), some emissions don't reach the aggregator's recorded set in ways the protocol can still decide around via local message availability that the offline-aggregator model doesn't track.

A correct C1 check needs the protocol to emit a per-decision quorum count from `obft.Instance.Resolve` / `twoab.Instance.Resolve` — same plumbing shape as Bucket 3's `LayerAttempts`. Treat as a follow-up that piggybacks on Bucket 3's protocol-side hook.

**C4 NoEquivocationAccepted (real count)** — the aggregator's `SigmaPartials` records every wire-emitted partial regardless of Instance-side Rule 3 filtering. So a naive "count dual emissions in pools" overcounts under legitimate `ByzCrossSigning` / `ByzCrossOnionEquivocation` scenarios that Rule 3 correctly filtered. A real count needs per-emitter decision-pool visibility — which is exactly what Bucket 2's `SigmaByEmitter` adds. Defer C4 to Bucket 2 alongside SigmaByEmitter; `EquivocationsAccepted` stays at 0 in this commit. Rule 3 binary failures are still transitively caught by `NoOfflineDoubleV`.

**C3 OBFTHostValidityRespect** — as previously noted, requires plumbing each op's acceptance-layer through the DES boundary. Out of bucket 1 scope; separate follow-up.

**2abOBFT aggregator recording fix (pre-existing bug)** — implementation surfaced that [`twoab/events.go`](../protocol/v2/consensustest/twoab/events.go) was passing `e.Payload` (encrypted ciphertext, differs per emitter) to `ObserveEncryptedClaim` at L_k>0, but the aggregator's chained-decrypt qV-counting path keys buckets by V plaintext (per OBFT base's `el.Value` convention). Effect: each emitter at L_k>0 in 2abOBFT scattered into a separate bucket, so `NoOfflineDoubleV` at 2abOBFT L_k>0 was effectively vacuous (no bucket could reach qV). Fixed by passing `e.V` instead of `e.Payload` in `recordValueMsgToAggregator`, `recordNoValueMsgToAggregator`, and `recordCommitToAggregator`. Independent of the bucket-1 wiring work but discovered while exercising it; included in commit 1 as the natural fix-site.

Net bucket-1 deliverable:
- `Outcome.Byz` field populated by all four adapters (OBFT, 2abOBFT, QBFT, PSigs); consumed by buckets 2 and 3 for honest-op filtering. QBFT + PSigs don't need the filtering today but set the field for symmetry.
- `OfflineAggReport.SigmaCardinality` map populated by `AttemptAll` (foundation for later C1 + diagnostic; no safety check today)
- C2 wired in both OBFT and 2abOBFT (cheap descriptive tag; always valid by construction, useful in panic diagnostics)
- `safety.go` SafetyReport docstring updated to reflect C2 now-instrumented status
- 2abOBFT recording bug fix (`e.Payload` → `e.V` in 3 call sites)

C1 / C4 / C3 are tracked as separate follow-ups, with C1 + C4 both unblocked once Buckets 2 and 3 land their protocol-side / per-emitter plumbing.

### C3 OBFTHostValidityRespect — deferred, not rolled into bucket 1

The adapter docstrings in [`obft/adapter.go`](../protocol/v2/consensustest/obft/adapter.go) and [`twoab/adapter.go`](../protocol/v2/consensustest/twoab/adapter.go) (search for `OBFTHostValidityRespect (C3)`) call out that a layer-naive comparison "decided_layer vs current host verdict" over-reports — scenarios like `HostFlipMidSlot` legitimately have operators decide at L_1 on a V they accepted at L_0 when the host's L_1 verdict is now "invalid". A correct check needs each operator's *acceptance-layer* (= the layer at which the host stamped a valid verdict on the decided V), plumbed through the DES boundary into `OperatorOutcome`.

That's a new field + new adapter-side bookkeeping. It doesn't fit the "wire what's already there" framing of bucket 1; it's a separate project the same size as bucket 3. Punt to a follow-up.

### Per-op data source for bucket 2 — extend OfflineAggregator, key on actual emitter

The aggregator's existing maps ([`offlineagg.go:38-49`](../protocol/v2/consensustest/offlineagg.go)) credit `c.OperatorID` — the **claimed sender on the wire**. This is correct for the existing aggregator-bypass detection (it models a byz with full message visibility who can forge identities).

For per-op cross-phase exclusivity we need the opposite: who **actually emitted** the partial, regardless of forged claims. The check is "did this honest operator violate σ-XOR-NR for itself".

Add a parallel set of maps keyed on actual emitter (full type definitions in [§Bucket 2 architecture](#bucket-2--per-op-cross-phase-invariants-in-safetyreport)):

```go
type OfflineAggregator struct {
    // existing fields — keyed on claimed-sender (c.OperatorID)
    SigmaPartials   map[SigmaKey]map[OperatorID]struct{}
    EncryptedClaims map[SigmaKey]map[OperatorID]struct{}
    NRPartials      map[int]map[OperatorID]struct{}

    // NEW — keyed on actual emitter (passed explicitly by the adapter).
    // Honest emitters: emitter == c.OperatorID. Byzantine emitters with
    // forged-identity claims: emitter is the byz op; c.OperatorID is
    // whatever the byz wrote. The per-op invariant check reads the by-
    // emitter maps and filters out byz operators by membership.
    SigmaByEmitter map[ByEmitterSigmaKey]struct{}
    NRByEmitter    map[ByEmitterNRKey]struct{}
}
```

Same two maps are mirrored on `OfflineAggReport` (populated by `AttemptAll` via map-reference sharing — no copy, safe because the aggregator is discarded post-AttemptAll by adapter callers). Bucket-3 `ComputeSafetyReport` consumes via `o.OfflineAgg.SigmaByEmitter` / `o.OfflineAgg.NRByEmitter` without needing a reference to the live aggregator.

Adapters call a new `ObserveSigmaByEmitter(emitter, layer, v)` / `ObserveNRByEmitter(emitter, layer)` alongside the existing claimed-sender path. Adapters know the emitter at observation time (it's the byz pattern's target operator, or the honest sender's own ID).

Alternative considered: keep a single map and have it record both pieces of info. Rejected — the two views are used for different invariants (aggregator-bypass detection wants claimed-sender; per-op invariants want emitter) and conflating them complicates the check logic in `AttemptAll` / `ComputeSafetyReport`. Memory cost of two maps is trivial at consensustest scale.

### Bucket 3 walk-state introspection — Instance-side hook, not post-hoc adapter reconstruction

For the D1 walk-state check we need to know, per honest operator, **the set of layers at which the operator's local σ-pool view reached qV during Resolve**. If that set is non-empty, the operator must decide at `min(set)` and not advance past it; any later commit (or `Err = "no quorum"`) is a Resolve-side regression.

Two design options:

**A) Adapter-side post-hoc reconstruction.** After the sim ends, the adapter replays each honest op's accumulated message view through a fresh `obft.Instance.Resolve` and records which layers σ'd. Then compares against the actual decision.

**B) Instance-side hook.** Instrument `Instance.Resolve` itself to record per-layer walk state, expose via a getter. Adapter copies into `OperatorOutcome`.

Pick B. Reasons:
- A double-replays consensus per op (~N× cost on `make stresstest`).
- A risks divergence between "what the real Resolve saw" and "what the replay sees" if message-arrival ordering or side-effects matter.
- B is ~20 lines of additive instrumentation in `tryReconstructLayer`; the trace is empty on the non-decided path and small on the decided path.
- B also exposes useful diagnostic data for failure debugging (you can see how far Resolve got).

The trace is a debugging side-channel; it does NOT change Resolve's return contract. Cost: bounded by K (≤ N at the production K=N convention), one struct append per layer per Resolve call — negligible against the crypto cost of σ-pool reconstruction at each layer. Always populated (no opt-in toggle); production runner paths simply ignore the field.

**Implementation shape (as landed in commit 4):** `LayerAttempt` exported type in `obft/message.go` alongside `Output`, re-aliased into `obft/base` and `obft/twoab` so callers use unqualified `LayerAttempt`:

```go
type LayerAttempt struct {
    Layer         int
    SigmaPoolSize int
    QV            int
    SigmaReached  bool
    Decided       bool
    NRPoolSize    int
    QEnc          int
    NRReached     bool
}
```

`Instance.lastResolveTrace []LayerAttempt` is an UNEXPORTED field — the trace lives on `Instance` (not on `Output`) so it's accessible on the failure path too (`Resolve` returns `nil`-Output on no-quorum / deadlock, but D1 needs walk state in BOTH success and failure paths). Exported getter:

```go
func (i *Instance) LastResolveLayerAttempts() []LayerAttempt
```

Resolve resets `lastResolveTrace` at the start of each call (per-call snapshot, idempotent re-runs reflect the LATEST call's walk state, not accumulated). Failure paths also append the attempt before returning the error so debugging tools see how far the walk got before the crypto failure / deadlock. Adapter calls the getter post-Resolve and copies into `OperatorOutcome.ResolveLayerAttempts` (defined as `ct.LayerAttempt` in `consensustest/protocol.go`, a separate-but-identical type so the framework stays protocol-family-agnostic — adapter does trivial field-by-field translation).

### 2abOBFT parity — same buckets, same shape, minor adaptations

The per-operator invariants are protocol-family-shared. 2abOBFT has the same EKM rules (cross-phase exclusivity, single-σ-V) plus Phase-2 cross-V / downgrade equivocation (Rule 6a). All four buckets apply identically; the implementation just touches both adapters.

Specific 2abOBFT notes:
- σ-on-V at L_0 is recorded in `OfflineAggregator.SigmaPartials` from `recordValueMsgToAggregator` ([`twoab/events.go:641-648`](../protocol/v2/consensustest/twoab/events.go)) — the by-emitter mirror needs the same hook.
- NR partials at L_0 come from `KindNoValue` Phase-2a messages (not from Commit's NRPartials section as in OBFT). Both call sites need the by-emitter hook.
- 2abOBFT's Rule 6a evidence already counts into `EquivocationsObserved` ([`twoab/adapter.go:339-343`](../protocol/v2/consensustest/twoab/adapter.go)); bucket 1's `EquivocationsAccepted` honesty applies identically.
- The walk-state hook lives on `twoab.Instance.Resolve` (separate implementation from base `Instance` but same shape).

### Byzantine filtering for buckets 2 and 3 — `Outcome.Byz` snapshot, not a signature change

The per-op invariants apply to *honest* operators. Byzantine operators are *allowed* to violate them (that's the point of "byzantine"). The filter is `IsByz(op) || IsCrashed(op)` against the sim's `ByzPattern`.

Two routes to thread the byz set into `ComputeSafetyReport`:

- **A — signature change**: `ComputeSafetyReport(o Outcome, byz ByzPattern)`. ~30 call sites across `consensustest/`, `consensustest/obft/`, `consensustest/twoab/`, including the synthetic-outcome tests in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) and the runner at [`runner.go:91`](../protocol/v2/consensustest/runner.go). Every caller updated; synthetic-outcome tests pass `ByzPattern{}` (zero value = no byz).

- **B — snapshot inside `Outcome`**: add a `Byz ByzPattern` field on `Outcome`; adapter copies `cfg.Byz` into it during `toCT`. `ComputeSafetyReport` signature unchanged. Callers unchanged. Synthetic-outcome tests get zero-value `Byz` automatically (no byz, correct default for honest-only synthetic cases).

Pick **B**. Reasons:
- Zero churn on existing call sites (~30 callers stay as-is).
- Symmetric with the existing `Outcome.CommitAttestation` pattern (adapter-introspected sim metadata embedded in Outcome).
- Synthetic-outcome tests at [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) don't need updates; zero `ByzPattern{}` means `IsByz(any) = false`, which is correct for the all-honest synthetic scenarios.
- The adapter's `toCT` is the single point of plumbing; if a future adapter forgets, the failure mode is "byz ops get checked as honest" → safety panic during catalog runs, which is *louder than a silent miss*. So the failure mode is fail-loud, fail-safe.

Bucket 2 / Bucket 3 then iterate by-emitter maps filtering via `o.Byz.IsByz(op) || o.Byz.IsCrashed(op)`. No new interface type needed — `ByzPattern` is concrete and already has both methods at [`byz.go:70-87`](../protocol/v2/consensustest/byz.go).

Alternative considered: have the adapter pre-filter at observation time (only call `ObserveByEmitter` for non-byz emitters). Rejected — the by-emitter maps are also useful for diagnostic dumps where seeing byz-emitted partials matters. Filtering at check time keeps the data complete.

### Negative-test design — synthetic outcomes, not byz patterns

The original plan sketched byz patterns for B1/B2/B3 modelled on `ByzAggregatorBypass` / `ByzWitnessForgery`. Commit-3 implementation surfaced that this shape doesn't fit the per-op honest-only invariants:

- B1/B2's check filters out byz operators via `o.Byz.IsByz(op)` (the invariant applies to *honest* operators specifically — byz are *allowed* to violate σ-XOR-NR and single-σ-V).
- A byz pattern that makes a *byz* op cross-sign (e.g., the existing `byzCrossSigning`) correctly records the violation in `SigmaByEmitter` + `NRByEmitter`, but the check skips that operator because it's in `cfg.Byz.ByzOperators`.
- Making the check fire requires simulating "an honest op did the impossible" — i.e., a hypothetical EKM regression. No realistic byz attack maps to this.

So B1/B2 negative tests live in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) as synthetic-outcome tests, identical in shape to D1's planned approach: hand-build an `Outcome` with the by-emitter map state that would result from an honest op cross-signing, assert `ComputeSafetyReport` fires the check.

Test layout (one `TestSafety_*` per new field, each with table-driven sub-tests covering VIOLATION + OK cases):

| Invariant | Test name | Sub-tests |
|---|---|---|
| B1 σ-XOR-NR per honest op (subsumes B3 leader case) | `TestSafety_HonestCrossPhaseExclusive` | OK_empty_maps, OK_distinct_layers_sigma_then_nr, VIOLATION_honest_op_sigma_and_nr_same_layer, VIOLATION_leader_sigma_V_and_nr_at_own_layer (B3 case), OK_byz_emitter_filtered, OK_crashed_op_filtered |
| B2 single-σ-V per honest op | `TestSafety_HonestSingleSigmaV` | OK_empty_map, OK_one_V_per_layer, OK_same_V_different_emitters, VIOLATION_honest_op_two_Vs_same_layer, OK_byz_emitter_filtered |
| D1 walk-state | `TestSafety_HonestWalkConsistent` | OK_no_trace_graceful_default, OK_decided_at_first_sigma_reached, OK_decided_at_fallthrough_layer, OK_clip_late_decided, OK_cert_gossip_decide_with_cluster_local_decider (empty-trace early-gate path), OK_cert_gossip_after_failed_local_resolve_with_cluster_local_decider (exercises clusterLocalDecidedOn), VIOLATION_cert_gossip_no_cluster_local_decider, OK_cert_gossip_decide_skip_case_b, VIOLATION_decided_no_sigma_source (Round>=0 + empty σ-trace), VIOLATION_advanced_past_sigma, OK_byz_emitter_filtered |

B3 is **not a separate SafetyReport field** — the leader's Phase-1 σ_V is recorded in `SigmaByEmitter` alongside any NR partial they emit at the same layer, so the B1 check catches the spec-§411 leader-σ-locks-σ-side rule by construction. The `VIOLATION_leader_sigma_V_and_nr_at_own_layer` sub-test under `TestSafety_HonestCrossPhaseExclusive` documents this; no separate test name needed.

No new byz patterns, no new `ByzKind` constants, no catalog-exclusion entries needed for B1/B2/B3.

### Catalog inclusion — none of the new patterns enter the catalog

D1's synthetic-outcome test (commit 5) and B1/B2's synthetic-outcome tests don't run via the catalog so there's nothing to exclude. The Bucket-1 catalog scenarios continue to exercise the new B1/B2 checks indirectly: under any catalog scenario, all-honest cluster scenarios populate `SigmaByEmitter` / `NRByEmitter` with legal observations, and the checks confirm "no honest emitter cross-signs / dual-σ's" — silent confirmation that the EKM enforcement is intact under real-protocol paths.

### B5 one-decision-per-op — adapter-side panic as defensive future-proofing

B5 asserts each honest operator decides at most one (V, layer) per slot. Under today's adapter code this can't happen — each operator's `OperatorOutcome` is built once at end-of-sim from a single `rawOutcome` entry — so the panic guard is defensive against a future refactor that introduces a re-decision path (e.g., a hypothetical "late-cert overrides earlier decision" optimization). Tracking this in a `SafetyReport` field would require turning `OperatorOutcome` into a slice of decisions (or adding a counter), which touches every `oo.Decided` / `oo.Value` / `oo.Round` consumer — disproportionate cost for a regression class that doesn't exist yet.

Add a 3-line guard in each adapter's `toCT` translation ([`obft/adapter.go:349-386`](../protocol/v2/consensustest/obft/adapter.go), [`twoab/adapter.go`](../protocol/v2/consensustest/twoab/adapter.go)):

```go
if _, exists := perOp[op]; exists {
    panic(fmt.Sprintf("consensustest: adapter wrote PerOp[%d] twice — B5 violation", op))
}
perOp[op] = oo
```

If the regression ever lands, the guard fires at the source with a stack trace pointing at the offending adapter code. Until then it's a cheap sentinel. The protocol's `i.committed` and `i.ended` checks at [`base/instance.go:251`](../protocol/v2/obft/base/instance.go) enforce single-commit on the protocol side; this guard is the adapter-side mirror.

### C4 EquivocationsAccepted counting — simpler "any equivocating partial in any pool" (deferred to bucket 2)

Bucket 1 originally planned to wire C4. Implementation surfaced (see [§Bucket 1 implementation findings](#bucket-1-implementation-findings--c1c4-deferred-2abobft-recording-fixed)) that the OfflineAggregator's claimed-sender-keyed `SigmaPartials` records *every* wire emission regardless of Instance-side Rule 3 filtering — counting dual emissions off it falsely flags legitimate `ByzCrossSigning` scenarios where Rule 3 correctly suppressed them at the protocol level. Bucket 2's `SigmaByEmitter` (per-actual-emitter, with byz-membership filtering) is the right data source. Defer.

The wiring shape when bucket 2 lands:

```go
// In computeAttestation (or in ComputeSafetyReport alongside the
// SigmaByEmitter traversal — TBD during bucket 2 implementation):
att.EquivocationsAccepted = 0
for layer, byOp := range perHonestEmitterSigmaPartialsAtLayer(out.OfflineAgg, out.Byz) {
    for op, valueHashes := range byOp {
        if len(valueHashes) > 1 {
            att.EquivocationsAccepted += len(valueHashes) - 1
        }
    }
}
```

Spec-literal C4 is "no honest committed *based on* an equivocating proposal" — only counts violations that contaminated a decision. The simpler version above counts "same honest emitter appears in σ-pools for two distinct V's at the same layer". Behavioral difference is essentially nil because Rule 3 is binary. More sensitive (catches Rule 3 regressions on every seed with honest dual emissions, not just seeds where they feed a decision) and cheaper. Implementation comment when wired: *"Spec-literal C4 only counts decision-contaminating cases; simpler counting is equivalent in practice because Rule 3 is binary."*

### Test targets — add `stresstest-negative` Makefile smoke

The bucket-4 negative tests are `go test` units (not stresstest scenarios) and run in ~seconds each. Aggregating them into a Makefile target lets CI run a fast machinery-regression check without spinning up the full matrix:

```makefile
.PHONY: stresstest-negative
stresstest-negative:
	@echo "Running stresstest negative-test smoke (machinery regression check)"
	@go test -tags blst_enabled -timeout 5m -v \
		-run '^TestAdapter_.*_TriggersSafetyDetection$$|^TestSafety_Honest' \
		./protocol/v2/consensustest/... \
		./protocol/v2/consensustest/obft/... \
		./protocol/v2/consensustest/twoab/...
```

Two regex branches:
- `TestAdapter_.*_TriggersSafetyDetection` — existing real-byz-pattern tests in adapter test files (`ByzAggregatorBypass` / `ByzWitnessForgery` proving `NoOfflineDoubleV` fires).
- `TestSafety_Honest*` — new synthetic-outcome tests in `safety_test.go` (`TestSafety_HonestCrossPhaseExclusive`, `TestSafety_HonestSingleSigmaV`, `TestSafety_HonestWalkConsistent` from commit 5).

Cheap enough to run on every PR. Wire into the relevant CI job alongside `make unit-test`.

### Panic message format — extend `SafetyPanic`, no schema break

`SafetyPanic` ([`safety.go:274`](../protocol/v2/consensustest/safety.go)) already prints `CommitAttestation` diagnostic fields when an attestation-driven invariant fires. Extend the same pattern for new invariants:

- `HonestCrossPhaseExclusive=FAIL` line includes `(op=N layer=K)` for the first offending op.
- `HonestSingleSigmaV=FAIL` includes `(op=N layer=K values=[V_a, V_b])`.
- `HonestWalkConsistent=FAIL` includes `(op=N decided=L_d sigmaQuorumAt=[L_a, ...])`.

`SafetyReport.String()` gets matching cases per the existing per-field pattern at [`safety.go:118-155`](../protocol/v2/consensustest/safety.go).

### Performance budget — negligible at stresstest scale

Per-op invariant check is O(N × K × distinct_V_at_layer) = O(N × K × 2) under normal byz patterns. At n=4, K=2: ~16 lookups per sim. The trace-collection on Instance.Resolve is gated by `TraceEnabled`; bucket 3's safety check requires it on, which adds the existing trace overhead (one slice append per layer). Acceptable.

Spot-check via `BenchmarkStress` (if it exists) or a one-off comparison before/after on `make stresstest CLUSTER_SIZES_N=4 LAYERS_K=2 ITERATIONS_BASELINE_OPERATIONS=1000`. If wall-time increases by > 5%, revisit the trace's allocation strategy (preallocate `LayerTrace` capacity at K).

### Stub-BLS vs real-BLS — no path divergence

Bucket 1's C1 σ-partial count comes from the aggregator's `SigmaPartials` map, which is populated identically in both stub-BLS and real-BLS modes ([`offlineagg.go:25-33`](../protocol/v2/consensustest/offlineagg.go) — "Reconstruction is cardinality+hash based in both stub-BLS and real-BLS modes"). Bucket 2's by-emitter mirror inherits the same property. Buckets 3 and 4 don't touch BLS. No `real_bls`-tag-conditional code needed.

### Order of work — single PR, ordered commits

All four buckets land in **one mega-PR**. Internal commit ordering preserves bisectability and review surface-area boundaries:

1. **Commit 1 — Bucket 1 wiring (revised scope) + `Outcome.Byz` + 2abOBFT recording fix**. OBFT + 2abOBFT `computeAttestation` populates C2 only (C1/C4 deferred per [§Bucket 1 implementation findings](#bucket-1-implementation-findings--c1c4-deferred-2abobft-recording-fixed)). Adds the `OfflineAggReport.SigmaCardinality` map (computed in `AttemptAll`) — kept as plumbing for diagnostic + future C1 follow-up even though commit 1 doesn't consume it. Adds `Outcome.Byz` field; all four adapters' `Run` paths copy `cfg.Byz` into it (OBFT + 2abOBFT for the bucket-2/3 honest-op filtering; QBFT + PSigs for symmetry / future-proofing). Updates [`safety.go`](../protocol/v2/consensustest/safety.go) `SafetyReport` COVERAGE docstring to reflect OBFTCommitKindValid now instrumented (with kind always valid by construction — vacuously true check, useful as descriptive tag in panic diagnostics). Fixes pre-existing 2abOBFT aggregator recording bug (`e.Payload` → `e.V`). No `SafetyReport` schema change; existing tests stay green.
2. **Commit 2 — `OfflineAggregator` by-emitter maps + observation methods**. Pure extension; no consumers yet. Adapter call sites at [`obft/events.go`](../protocol/v2/consensustest/obft/events.go) and [`twoab/events.go`](../protocol/v2/consensustest/twoab/events.go) plumb the actual emitter through `recordCommitToAggregator(agg, emitter, c)` and call `ObserveSigmaByEmitter` / `ObserveNRByEmitter`. No `SafetyReport` change yet — the data is collected but not checked.
3. **Commit 3 — Bucket 2 SafetyReport fields (B1, B2)**. Adds `HonestCrossPhaseExclusive`, `HonestSingleSigmaV` to `SafetyReport` with paired evidence slices (`CrossPhaseEvidence`, `SingleSigmaVEvidence`) and supporting types (`CrossPhaseViolation`, `SingleSigmaVViolation`). `ComputeSafetyReport` reads `o.OfflineAgg.SigmaByEmitter` / `NRByEmitter` filtered via `o.Byz.IsByz` / `IsCrashed` (no signature change). `IsViolation` / `String` / `SafetyPanic` updated for the new fields with deterministic-ordering evidence dumps. Bucket-4 negative tests for B1/B2 (B3 subsumed by B1's leader-case sub-test) land as synthetic-outcome tests in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) — see [§Negative-test design](#negative-test-design--synthetic-outcomes-not-byz-patterns) for the rationale.
4. **Commit 4 — Bucket 3 protocol-side instrumentation**. Adds `obft.LayerAttempt` exported type alongside `obft.Output`; `obft/base` and `obft/twoab` alias it. `Instance.lastResolveTrace` unexported field on both, populated by `Resolve` via the existing per-layer walk loop (`tryReconstructLayer` / `tryDeriveNextLayerKey` return signatures extended to also report pool size). Exported getter `Instance.LastResolveLayerAttempts() []LayerAttempt` on both. Adapter copies the trace through `rawOpOutcome.resolveLayerAttempts` → `ct.LayerAttempt` (defined in consensustest, separate from `obft.LayerAttempt` to keep the framework protocol-family-agnostic; adapter does trivial field-by-field translation). New `OperatorOutcome.ResolveLayerAttempts []LayerAttempt` field on the framework side. No `SafetyReport` change yet — protocol side ships first so the adapter's data plumbing is bisectable separately from the safety check.
5. **Commit 5 — Bucket 3 SafetyReport field (D1) + B5 adapter-side panic guard**. `HonestWalkConsistent` added with `WalkConsistencyEvidence` slice + `WalkConsistencyViolation` + `WalkInconsistencyReason` enum (two reasons: `WalkDecidedNoSigmaSource`, `WalkAdvancedPastSigma`). `ComputeSafetyReport` iterates `o.PerOp` filtered via `o.Byz`, reading each op's `ResolveLayerAttempts`. Three skip cases (graceful defaults, not violations): empty trace, byz/crashed op, clip-late op (`oo.Err == "missed relay deadline"`). Case (a) splits by `oo.Round` semantics — local-decide-claim with empty σ-trace is a regression; cert-gossip-decide checks the cluster verdict via `clusterLocalDecidedOn` (some OTHER op has Decided=true && Round>=0 on the same V → legitimate catch-up). Case (b) only fires for `oo.Round >= 0` (cert-gossip's Round=-1 is excluded to avoid false positives where local Resolve parallel-progressed before cert arrived). D1 synthetic-outcome negative test lands in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) (9 sub-tests covering all violation + OK shapes). B5 guard added to both adapters' `toCT` translation as defensive future-proofing.
6. **Commit 6 — `stresstest-negative` Makefile target**. Aggregates the existing `TestAdapter_.*_TriggersSafetyDetection` adapter tests (real-byz patterns proving `NoOfflineDoubleV` fires) and the new `TestSafety_Honest*` synthetic-outcome tests (proving B1/B2/D1 fire) into a single fast CI smoke. No `matrix_test.go` changes needed since the bucket-3/4 negative tests don't add catalog-excluded byz patterns.

The single-PR scope means reviewers see the full coverage picture in one place; commit boundaries keep the diff navigable. Each commit ships green on `make unit-test`; the final state ships green on `make stresstest`.

Bucket 1 has no dependency on the others. Bucket 2 depends on the `OfflineAggregator` by-emitter extension (commit 2 ⇒ commit 3). Bucket 3 depends on the `Output.LayerAttempts` instrumentation (commit 4 ⇒ commit 5). Bucket 4 lands incrementally inside the commits that introduce each new invariant.

## Architecture

### Bucket 1 — `CommitAttestation` wiring (C2 only) + plumbing

Per [§Bucket 1 implementation findings](#bucket-1-implementation-findings--c1c4-deferred-2abobft-recording-fixed), only C2 wires cleanly under bucket 1's adapter-boundary constraint. C1 and C4 are deferred. The commit also adds plumbing other buckets depend on.

[`protocol/v2/consensustest/obft/adapter.go`](../protocol/v2/consensustest/obft/adapter.go), `computeAttestation`:

```go
func computeAttestation(_ ct.SimConfig, out ct.Outcome) ct.CommitAttestation {
    att := ct.CommitAttestation{
        EquivocationChecked: true,
    }

    if out.Decided {
        att.OBFTCommitKindChecked = true
        if out.DecidedRound == 0 {
            att.OBFTCommitKind = "sigma"
        } else {
            att.OBFTCommitKind = "nr"
        }
    }

    for _, oo := range out.PerOp {
        for rule, n := range oo.EvidenceByRule {
            if rule == RuleLeaderEquivocation ||
                rule == RuleCrossOnionEquivocation ||
                rule == RuleCommitEquivocation {
                att.EquivocationsObserved += n
            }
        }
    }
    return att
}
```

`OfflineAggReport` gains a `SigmaCardinality` map (kept for diagnostic + future C1 follow-up):

```go
type OfflineAggReport struct {
    NoOfflineDoubleV bool
    Reconstructions  []OfflineReconstruction
    // Bucket cardinalities by (layer, value_hash) — combines plaintext
    // SigmaPartials with chain-unlocked EncryptedClaims at L_k>0.
    // Pre-computed in AttemptAll.
    SigmaCardinality map[SigmaKey]int
}
```

`Outcome` gains a `Byz ByzPattern` field; all four adapters' `Run` paths set `out.Byz = cfg.Byz` immediately after `toCT` (OBFT + 2abOBFT for bucket-2/3 honest-op filtering; QBFT + PSigs for symmetry — those protocols don't need the filtering today but the field is always populated). Synthetic-outcome tests get zero-value `ByzPattern{}` (no byz / no crashed) — correct default for the honest-only synthetic scenarios.

2abOBFT recording-path bug fix: [`twoab/events.go`](../protocol/v2/consensustest/twoab/events.go) `recordValueMsgToAggregator` / `recordNoValueMsgToAggregator` / `recordCommitToAggregator` change `e.Payload` → `e.V` at all `agg.ObserveEncryptedClaim` call sites. See findings section for context.

Same `computeAttestation` shape in [`twoab/adapter.go`](../protocol/v2/consensustest/twoab/adapter.go) (2ab-specific equivocation rule mix: Rule 6a in addition to Rule 2 + Rule 3).

### Bucket 2 — per-op cross-phase invariants in `SafetyReport`

`SafetyReport` ([`safety.go:44-92`](../protocol/v2/consensustest/safety.go)) gains three fields:

```go
type SafetyReport struct {
    // ... existing fields ...

    // HonestCrossPhaseExclusive: every honest operator emitted σ-XOR-NR
    // per (slot, layer) — no honest op appears in both SigmaByEmitter and
    // NRByEmitter at the same layer. Spec: OBFT.md:411 (Pigeonhole 1).
    HonestCrossPhaseExclusive bool

    // HonestSingleSigmaV: every honest operator emitted at most one σ-on-V
    // per (slot, layer) — no honest op appears in SigmaByEmitter at the
    // same layer with two distinct value_hashes. Spec: OBFT.md:411
    // (single-σ-V).
    HonestSingleSigmaV bool

    // HonestWalkConsistent: every honest decider's decision-layer is the
    // shallowest layer at which their local σ-pool view reached qV. Spec:
    // OBFT.md §Phase 3 walk semantics. Off-by-default; set true when the
    // adapter populates OperatorOutcome.ResolveLayerAttempts.
    HonestWalkConsistent bool

    // Per-field diagnostic details for SafetyPanic output:
    CrossPhaseEvidence []CrossPhaseViolation
    SingleSigmaVEvidence []SingleSigmaVViolation
    WalkConsistencyEvidence []WalkConsistencyViolation
}

type CrossPhaseViolation struct {
    Operator OperatorID
    Layer    int
}
type SingleSigmaVViolation struct {
    Operator   OperatorID
    Layer      int
    ValueHashA [32]byte
    ValueHashB [32]byte
}
type WalkConsistencyViolation struct {
    Operator           OperatorID
    DecidedLayer       int  // -1 if no decision
    SigmaReachedLayers []int
}
```

`IsViolation()` ([`safety.go:103-111`](../protocol/v2/consensustest/safety.go)) adds the three new checks. `String()` ([`safety.go:118-155`](../protocol/v2/consensustest/safety.go)) adds matching `FAIL` lines. `SafetyPanic` ([`safety.go:274-318`](../protocol/v2/consensustest/safety.go)) appends evidence-slice contents under the existing "attestation:" block.

`Outcome` gains a byz snapshot field (no `ComputeSafetyReport` signature change — see [§Byzantine filtering](#byzantine-filtering-for-buckets-2-and-3--outcomebyz-snapshot-not-a-signature-change)):

```go
type Outcome struct {
    // ... existing fields ...

    // Byz snapshots the sim's byzantine + crashed membership so safety
    // checks can filter honest-only invariants without a function-signature
    // change at every ComputeSafetyReport call site. Adapter populates
    // from cfg.Byz during toCT; synthetic-outcome tests (e.g.,
    // safety_test.go) get zero-value ByzPattern = "no byz", which is the
    // correct default for honest-only synthetic cases.
    Byz ByzPattern
}
```

`OfflineAggregator` gains the by-emitter maps and observation methods:

```go
// New keys for the by-emitter maps.
type ByEmitterSigmaKey struct {
    Emitter   OperatorID
    Layer     int
    ValueHash [32]byte
}
type ByEmitterNRKey struct {
    Emitter OperatorID
    Layer   int
}

// New maps on OfflineAggregator.
SigmaByEmitter map[ByEmitterSigmaKey]struct{}
NRByEmitter    map[ByEmitterNRKey]struct{}

// New observation methods (adapter call sites mirror the existing pattern).
func (a *OfflineAggregator) ObserveSigmaByEmitter(emitter OperatorID, layer int, v []byte) {...}
func (a *OfflineAggregator) ObserveNRByEmitter(emitter OperatorID, layer int) {...}
```

Adapter call sites (commit 2 plumbs `emitter` through every record-function signature; Witnesses[] entries in OBFT base are intentionally NOT recorded by-emitter — they're peer-forwards of the leader's σ_V, not the emitter's own EKM commitment):

- [`obft/events.go`](../protocol/v2/consensustest/obft/events.go):
  - Leader-broadcast handler: `ObserveSigmaByEmitter(leader, layer, V)` alongside the existing `ObserveSigma`.
  - `recordCommitToAggregator(agg, emitter, c)`: σ-side entries (plaintext L_0 + encrypted L_k>0) record by-emitter via `ObserveSigmaByEmitter`; NR partials via `ObserveNRByEmitter`; Witnesses[] stays claimed-sender-only.
- [`twoab/events.go`](../protocol/v2/consensustest/twoab/events.go): same shape across `recordValueMsgToAggregator`, `recordNoValueMsgToAggregator`, `recordCommitToAggregator` — all three gain an `emitter` parameter. Phase-2a `vm.L0Partial`, Phase-2a `LayerEntrySigmaChained`, and Phase-2b NRDirect `LayerEntrySigmaChained` all record σ-by-emitter; NR-side entries (Side=NR / NR-tag partials / `LayerEntryNRPlaintext`) record NR-by-emitter.

`ComputeSafetyReport` adds three new traversals after the existing checks (filter reads `o.Byz`, not a parameter):

```go
// Cross-phase exclusivity per honest op.
for sigKey := range o.OfflineAgg.SigmaByEmitter {
    op := sigKey.Emitter
    if o.Byz.IsByz(op) || o.Byz.IsCrashed(op) {
        continue
    }
    nrKey := ByEmitterNRKey{Emitter: op, Layer: sigKey.Layer}
    if _, hasNR := o.OfflineAgg.NRByEmitter[nrKey]; hasNR {
        r.HonestCrossPhaseExclusive = false
        r.CrossPhaseEvidence = append(r.CrossPhaseEvidence,
            CrossPhaseViolation{Operator: op, Layer: sigKey.Layer})
    }
}

// Single-σ-V per honest op per layer.
sigmaByOpLayer := map[struct{Op OperatorID; Layer int}][][32]byte{}
for sigKey := range o.OfflineAgg.SigmaByEmitter {
    op := sigKey.Emitter
    if o.Byz.IsByz(op) || o.Byz.IsCrashed(op) {
        continue
    }
    key := struct{Op OperatorID; Layer int}{op, sigKey.Layer}
    sigmaByOpLayer[key] = append(sigmaByOpLayer[key], sigKey.ValueHash)
}
for key, hashes := range sigmaByOpLayer {
    if len(hashes) > 1 {
        r.HonestSingleSigmaV = false
        r.SingleSigmaVEvidence = append(r.SingleSigmaVEvidence,
            SingleSigmaVViolation{Operator: key.Op, Layer: key.Layer,
                ValueHashA: hashes[0], ValueHashB: hashes[1]})
    }
}
```

Default values: `HonestCrossPhaseExclusive` and `HonestSingleSigmaV` default to `true`. They CAN fire on any sim (the data is universally instrumented post-bucket-2).

### Bucket 3 — per-op walk-state assertions

New `LayerAttempt` type in [`obft/message.go`](../protocol/v2/obft/message.go) (re-aliased into `obft/base` and `obft/twoab` so callers use unqualified `LayerAttempt`):

```go
// obft.LayerAttempt — protocol-side trace record.
type LayerAttempt struct {
    Layer         int
    SigmaPoolSize int  // largest sigGroup's distinct-emitter count
    QV            int  // σ-quorum threshold at this layer (= 2f+1)
    SigmaReached  bool // SigmaPoolSize >= QV
    Decided       bool // walk produced an Output at this layer
    NRPoolSize    int  // distinct-emitter NR partials at this layer
    QEnc          int  // 0 at the deepest layer (no NR tag)
    NRReached     bool
}
```

Trace lives on `Instance` (not `Output`) so it's accessible on the failure path too — `Resolve` returns `nil, error` on no-quorum / deadlock, and `Output.LayerAttempts` wouldn't carry the trace through that path. Walk state is genuinely needed by D1 in BOTH success and failure paths (D1 catches "σ-quorum reachable but walk advanced anyway" as one case, and "σ-quorum reachable but walk returned no-quorum" as another).

```go
// Instance gains an unexported field.
type Instance struct {
    // ... existing fields ...
    lastResolveTrace []LayerAttempt
}

// And an exported getter for adapter / framework consumers.
func (i *Instance) LastResolveLayerAttempts() []LayerAttempt
```

`tryReconstructLayer` and `tryDeriveNextLayerKey` signatures change to also return the pool size:

```go
// BEFORE
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, error)
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, error)

// AFTER — second return is the largest sigGroup / NR-pool size at this
// layer, regardless of quorum reached. Resolve assembles the
// LayerAttempt from these values + the instance-level QV / QEnc.
func (i *Instance) tryReconstructLayer(layer int, chainedKeys [][]byte) (*Output, int, error)
func (i *Instance) tryDeriveNextLayerKey(layer int) ([]byte, int, error)
```

`Resolve` resets `lastResolveTrace` at the start (per-call snapshot, not accumulated across the idempotent re-runs), appends one `LayerAttempt` per layer visited. The deepest layer's attempt has zero NR fields (no NR tag at the deepest layer per spec).

Same instrumentation pattern in `twoab.Instance.Resolve` — the Phase-3 walk structure is identical between OBFT base and 2abOBFT (Phase-2a / Phase-2b emissions feed the σ-pool / NR-pool that Resolve consumes; no separate hook points needed). The single `LayerAttempt` schema is reused via `twoab.LayerAttempt = obft.LayerAttempt`.

`OperatorOutcome` gains:

```go
type OperatorOutcome struct {
    // ... existing fields ...

    // ResolveLayerAttempts is the per-layer trace from this operator's
    // most recent Phase-3 Resolve walk. Populated by OBFT-family adapters
    // (OBFT base + 2abOBFT) from the underlying Instance's
    // LastResolveLayerAttempts(); empty/nil for protocol families
    // without a layered Resolve walk (QBFT, PSigs).
    ResolveLayerAttempts []LayerAttempt
}
```

Note: `ct.LayerAttempt` is a separate-but-identical type defined in `consensustest/protocol.go` so the framework stays protocol-family-agnostic (doesn't import `obft`). Adapters translate `obft.LayerAttempt` → `ct.LayerAttempt` field-by-field in `toCT`. Trivial copy.

Adapter copies the trace via the instance getter in the rawOutcome → ct.Outcome translation:
- [`obft/des.go outcome()`](../protocol/v2/consensustest/obft/des.go) — captures `s.instances[op].LastResolveLayerAttempts()` into a new `rawOpOutcome.resolveLayerAttempts` field.
- [`obft/adapter.go toCT`](../protocol/v2/consensustest/obft/adapter.go) — translates obft.LayerAttempt → ct.LayerAttempt.
- Mirrored in [`twoab/des.go`](../protocol/v2/consensustest/twoab/des.go) + [`twoab/adapter.go`](../protocol/v2/consensustest/twoab/adapter.go).

`ComputeSafetyReport` walk-state check (per-op, in deterministic sorted-op order):

```go
for _, op := range sortedOpIDs(o.PerOp) {
    oo := o.PerOp[op]
    if o.Byz.IsByz(op) || o.Byz.IsCrashed(op) {
        continue
    }
    if len(oo.ResolveLayerAttempts) == 0 {
        continue // adapter didn't instrument; graceful default
    }
    // Clip-late skip: protocol internally decided (trace reflects
    // that), but ClipLateDecision turned off oo.Decided because the
    // decision time was past the relay deadline. Not a Resolve-side
    // regression — slot accounting only.
    if !oo.Decided && oo.Err == "missed relay deadline" {
        continue
    }
    sigmaReachedAt := []int{}
    for _, la := range oo.ResolveLayerAttempts {
        if la.SigmaReached {
            sigmaReachedAt = append(sigmaReachedAt, la.Layer)
        }
    }
    // Case (a) — Decided + empty σ-trace. Split by oo.Round:
    //   - Round >= 0 (local-decide claim): trace MUST confirm via a
    //     σ-reached entry; empty trace = real regression (Resolve or
    //     adapter mis-recorded the decision).
    //   - Round == -1 (cert-gossip-decide): legitimate iff some OTHER
    //     op has Decided=true && Round>=0 on this V (cluster has a
    //     local-decider that gossipped the cert this op applied);
    //     missing → bogus cert.
    if oo.Decided && len(sigmaReachedAt) == 0 {
        legitimate := false
        if oo.Round == -1 {
            legitimate = clusterLocalDecidedOn(o, op, oo.Value)
        }
        if !legitimate {
            r.HonestWalkConsistent = false
            r.WalkConsistencyEvidence = append(r.WalkConsistencyEvidence,
                WalkConsistencyViolation{Operator: op,
                    Reason: WalkDecidedNoSigmaSource, DecidedLayer: oo.Round})
        }
        continue
    }
    // Case (b) — Decided at deeper layer than shallowest σ-reached.
    // Walk advanced past a σ-decidable layer. Skipped when Round==-1
    // (cert-gossip-decide may have a parallel local trace that
    // SigmaReached at some layer; not a regression).
    if oo.Decided && oo.Round >= 0 && len(sigmaReachedAt) > 0 && oo.Round != sigmaReachedAt[0] {
        r.HonestWalkConsistent = false
        r.WalkConsistencyEvidence = append(r.WalkConsistencyEvidence,
            WalkConsistencyViolation{Operator: op,
                Reason: WalkAdvancedPastSigma, DecidedLayer: oo.Round,
                SigmaReachedLayers: sigmaReachedAt})
    }
}
```

`clusterLocalDecidedOn(o, exclude, v)` returns true when some operator OTHER than `exclude` has `Decided=true && Round>=0 && Value=v` — i.e., the cluster has at least one local-decider on V (which implies σ-quorum was reached locally there per Resolve's load-bearing contract). Implemented as a simple linear scan of PerOp; chosen over reading `OfflineAggReport.SigmaCardinality` because the offline aggregator's view is a conservative under-approximation of protocol-side σ-pool reconstruction (the protocol combines plaintext SigmaPartials + chain-unlocked EncryptedClaims + own-partial cache at each operator; the aggregator can miss combined-source reconstructions). The cluster's own verdict is the authoritative signal.

`sigmaReachedAt` is naturally sorted ascending because `ResolveLayerAttempts` is appended in walk order — `sigmaReachedAt[0]` is `min(sigmaReachedAt)`.

The original case (b) ("decided=false + σ-reached at some layer = walk failed to decide despite a σ-decidable layer") was dropped during implementation — under correct Resolve instrumentation `SigmaReached ↔ Decided` per layer (Resolve returns Output as soon as σ-quorum reaches), so the case is unreachable. Keeping it would only false-flag clip-late-decided ops where the protocol internally decided but ClipLateDecision turned off PerOp.Decided post-Resolve.

Three skip cases (graceful, not violations): empty ResolveLayerAttempts (adapter didn't instrument), byz/crashed op, and clip-late op (`!oo.Decided && oo.Err == "missed relay deadline"`).

### Bucket 4 — negative tests, all synthetic-outcome

All bucket-4 negative tests live in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) as table-driven synthetic-outcome tests — the per-op invariants (B1, B2, D1) all describe regressions that don't naturally arise under any byz pattern (the checks intentionally filter out byz operators; the violations they catch are hypothetical EKM / Resolve-side bugs in honest code). Synthetic Outcomes are sufficient for the bucket-4 goal: prove that the check fires on hand-crafted inputs that match the violation shape.

Example shape (B1):

```go
func TestSafety_HonestCrossPhaseExclusive(t *testing.T) {
    op1 := ct.OperatorID(1)
    tests := []struct {
        name   string
        sigma  []ct.ByEmitterSigmaKey
        nr     []ct.ByEmitterNRKey
        byz    ct.ByzPattern
        wantOK bool
        // ...
    }{
        // ... OK cases ...
        {
            name:   "VIOLATION_honest_op_sigma_and_nr_same_layer",
            sigma:  []ct.ByEmitterSigmaKey{{Emitter: op1, Layer: 0, ValueHash: hashForTest(0xAA)}},
            nr:     []ct.ByEmitterNRKey{{Emitter: op1, Layer: 0}},
            wantOK: false,
        },
    }
    // ... assert ComputeSafetyReport flags HonestCrossPhaseExclusive=false
}
```

Test layout (lives entirely in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go); no adapter-side test files touched):

| Invariant | Test name | # sub-tests (commit 3 / commit 5) |
|---|---|---|
| B1 σ-XOR-NR per honest op (subsumes B3) | `TestSafety_HonestCrossPhaseExclusive` | 6 (commit 3) |
| B2 single-σ-V per honest op | `TestSafety_HonestSingleSigmaV` | 5 (commit 3) |
| D1 walk-state | `TestSafety_HonestWalkConsistent` | TBD (commit 5) |

Each sub-test covers both VIOLATION cases (the check fires correctly) and OK cases (graceful default-true on empty maps; byz emitter filtered out; crashed op filtered out; legal distinct-layer / different-V scenarios that look superficially similar but don't violate the invariant). 11 sub-tests in commit 3 (B1: 6, B2: 5).

D1 lives in [`safety_test.go`](../protocol/v2/consensustest/safety_test.go) (commit 5), alongside the synthetic-outcome tests for the existing `CommitAttestation` invariants. The check itself is adapter-agnostic, so one test exercises both adapters' semantics.

No new byz patterns are introduced by buckets 2-4, so no catalog-exclusion entries are needed in `matrix_test.go` — the existing `ByzAggregatorBypass` / `ByzWitnessForgery` exclusions remain unchanged.

### Bucket interactions

Bucket 1 and Bucket 2 are independent. Bucket 3 depends on the protocol-side `Output.LayerAttempts` instrumentation; bucket 2 doesn't.

`ComputeSafetyReport`'s signature stays unchanged — byz info travels via `Outcome.Byz` (see [§Byzantine filtering](#byzantine-filtering-for-buckets-2-and-3--outcomebyz-snapshot-not-a-signature-change)). Buckets 2 and 3 land their new fields incrementally on `SafetyReport`.

`SafetyReport.IsViolation()` becomes the central panic-trigger; adding a new field is one line. The panic message stays structured and self-diagnosing.

## Implementation flag — 2abOBFT walk-state trace [resolved]

Resolved during commit 4: `twoab.Instance.Resolve` has the same single-pass Phase-3 walk loop as `obft/base.Instance.Resolve` — the same instrumentation pattern (per-layer attempt struct, populated by `tryReconstructLayer` / `tryDeriveNextLayerKey`) works for both adapters. Phase-2a / Phase-2b emissions feed the σ-pool / NR-pool that Resolve consumes; no separate hook points needed. The single `LayerAttempt` schema is shared via `twoab.LayerAttempt = obft.LayerAttempt` (same type alias pattern as `Output`).

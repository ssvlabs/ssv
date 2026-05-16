# OBFT-family early-commit plan

Switch from synchronous emission at deadline boundaries (`KindCommit` at `T_commit` in OBFT; `KindVerdict`/`KindOnion2b` at their respective latest-safe times in 2abOBFT) to event-driven emission as soon as the operator's observation set is complete. Lets the healthy slot reach reconstruction earlier and gives back the post-`T_commit` slack we'd otherwise need for reflood absorption.

This is an emission-timing change. Independent of the per-layer broadcast-budget resizing and the post-`T_commit` budget tightening — but designed to compose with them.

## §1. OBFT (bare)

### Current rule

[docs/OBFT.md:216](docs/OBFT.md:216), [L222](docs/OBFT.md:222): *"Each operator emits exactly one `KindCommit` message at `T_commit`."* Rationale at [L45](docs/OBFT.md:45): *"Phase 2 doesn't need a comparable [convergence] buffer: `KindCommit` emits are synchronous at `T_commit`, so view-convergence is not at issue."*

Impl matches: [protocol/v2/ssv/runner/obft/runner.go:111-119](protocol/v2/ssv/runner/obft/runner.go:111) does `sleepUntil(ctx, tCommit)` then `BuildAndBroadcastCommit`.

### Proposed rule

> Each operator emits exactly one `KindCommit` per (slot, operator) at `T_emit = min(T_L_0_observed, T_commit)`, where `T_L_0_observed` is the first moment the operator has observed and host-validated the Phase-1 bundle for layer `L_0`. At emit time: σ on each layer with observed-and-valid bundle, NR on each layer without.

**Why "wait for L_0" rather than "wait for all K observed":** the staggered schedule has L_0 broadcasting *latest* with the tightest budget, so under healthy propagation L_0 is the last bundle to arrive — meaning by `T_L_0_observed` all other layers are typically already observed. In edge cases (silent backup leader, mesh-flaky deeper layer for some operator), "wait for L_0" fires earlier with NR on the missing layer; "wait for all K" would wait or fall back to T_commit. The trade-off is that early-NR on deeper layers slightly reduces fall-through capability if L_0 fails — but under healthy operation L_0 succeeds in the vast majority of slots, so trading marginal fall-through capability for faster healthy-path emit is the right call.

The `min(·, T_commit)` clamp is the silent-L_0 fallback: if L_0 bundle never arrives, the operator commits NR on L_0 at T_commit per current semantics.

### Safety analysis

Pigeonhole 2 (at-most-one σ-quorum per `(slot, layer)` cluster-wide) depends on what operators **sign**, not **when**. Per-operator σ vs NR decisions are made from the operator's observation set at emission time. Earlier emission = earlier-state observation, but the σ/NR decision per layer is still locally well-defined.

| Scenario | Synchronous (current) | Early commit |
|---|---|---|
| All K bundles arrive early at all honest | Wait `Δ_2` worth of slack | Emit ~`max(B_k) − 1·BTT` earlier, same σ-quorum |
| Some bundle late but ≤ T_commit | All wait, same observations at T_commit | Each operator waits independently for their last bundle; same observations at emit time |
| Silent leader at L_k | NR on L_k at T_commit | NR on L_k at T_commit (timeout) — identical |
| Late equivocation in window `(T_emit_A, T_commit]` | A sees equivocation → NR | A already emitted σ on first-observed V — verdict stale |
| Asymmetric propagation (some honest see V late) | Late receivers NR, early σ | Same per-operator outcome |

**The only new exposure** is row 4: a byzantine can time equivocation to land between early- and late-emitters' commit times. This is *already* an out-of-envelope view-divergence pattern ([docs/OBFT.md:7](docs/OBFT.md:7)) — the spec doesn't promise recovery from equivocation in non-naturally-recovered cases (slashable instead). Early commit widens the timing surface but doesn't change the safety guarantee.

### §1.1 OBFT spec changes (docs/OBFT.md)

- **L198** Phase 2 heading: `[T_commit, T_commit + Δ_2]` → `[T_emit, T_emit + Δ_2]` where `T_emit ≤ T_commit`.
- **L216, L222** core rule: rewrite to "each operator emits exactly one `KindCommit` at `T_emit = min(T_all_observed, T_commit)`". Define `T_all_observed`.
- **L45** convergence-buffer parenthetical ("`KindCommit` emits are synchronous at `T_commit`, so view-convergence is not at issue") is now wrong. Replace with: "`KindCommit` emits are bounded by `T_commit` but may fire earlier on observation completion — see §Phase 2 emission-timing. Per-operator σ/NR decisions are locally well-defined regardless of emission time; the equivocation-window widening is bounded by the in-envelope partial-synchrony assumption."
- **New subsection §Phase 2 / Emission timing** (after L216): explicit rule, silent-layer fallback, equivocation-window note. Operational definition of "all K observed": Phase-1 bundle received AND host validity returned for every layer.
- **L218** Δ_2 sizing: still sized for KindCommit propagation from `T_emit` (worst case `T_commit`). Wording: "`Δ_2 ≥ 1·BTT` minimum propagation budget for `KindCommit` messages emitted by `T_commit` to reach all honest by `T_commit + Δ_2`."
- **L196** equivocation-NR rule: "Equivocation observed pre-T_commit (≥ 2 distinct V's retained)" → "Equivocation observed pre-emit". Add a paragraph documenting the byzantine equivocation-window widening as a known residual exposure.
- **§Application / Timing budget table** ([L697-707](docs/OBFT.md:697)): tables list V_k broadcast deadlines and MEV-fetch budgets — unchanged. The "consensus expected to complete by" line refers to the synchronous-emit worst case; add a note that healthy case decides earlier.
- **§Liveness comparison** ([L731-740](docs/OBFT.md:731)): "300ms BFT-consensus tax" partial-sigs comparison — re-derive under the new healthy-case timing.

### §1.2 OBFT impl changes

#### `protocol/v2/ssv/runner/obft/runner.go`

- Lines 109-119: replace the unconditional `sleepUntil(ctx, tCommit)` with a select on (a) an event channel signaled when all-K-bundles-observed-and-host-validated, (b) the T_commit ticker, (c) ctx.Done.

#### `protocol/v2/ssv/runner/obft/scheduler.go` and `controller.go`

- Expose `WaitForAllObserved(ctx, slot) <-chan struct{}` (or equivalent) from the Controller. The Instance already tracks per-layer `observedAt` + `hostValidated`; signal when the last per-layer observation+validation lands.

#### `protocol/v2/obft/base/instance.go`

- Add `L0BundleObservedAndValidated() bool` (or expose via channel/callback). Returns true once L_0's Phase-1 bundle is retained and host-validity has returned. The Instance already tracks per-layer `observedAt` + host-validity verdicts.

#### Tests

- `protocol/v2/ssv/runner/obft/runner_test.go`: add "healthy K=4 commit fires before T_commit" test.
- `protocol/v2/obft/base/instance_test.go`: coverage for `AllLayersDecidable`.
- `protocol/v2/consensustest/obft/`: healthy-path scenario asserting decision-time < T_commit. Existing `MaxMEVFetch_HealthyAtBoundary` test will tighten its observed `DecisionTime`.

## §2. 2abOBFT — parallel early-emit (verdict + commit)

### Why this works

Re-examining under the rational-byz deterrent ([docs/2abOBFT.md:81](docs/2abOBFT.md:81)), the equivocation-driven verdict-vs-action mismatch surface largely collapses. The spec already supports honest revision: [L181](docs/2abOBFT.md:181) explicitly anticipates earlier verdict broadcast and the "honest verdict-vs-action revision" framing; Rule 6b ([L112](docs/2abOBFT.md:112), [L455](docs/2abOBFT.md:455)) handles the wire-format consequence with "behavioral-pattern quality" evidence.

Verdict wire format is `(protocol_tag, message_kind, cluster_id, slot, operator_id, layer, verdict, value_root)` where `value_root = hash(V_{L_k})` ([L165](docs/2abOBFT.md:165)). The hash lets receivers cross-reference V's they've retained from gossipsub; V re-flood is independent and continues throughout Phase-2a ([L140](docs/2abOBFT.md:140)).

### Proposed rules

```
T_emit_verdict_2a = min(T_L_0_bundle_observed,        T_verdict_max − ε_proc)
T_emit_phase_2b  = min(T_L_0_sigma_eligibility_met + ε_proc, T_commit + ε_proc)
```

Two cascading triggers, both keyed on L_0 specifically (matching the OBFT rule's intuition that L_0 is the load-bearing layer):

- **Phase-2a verdict-emit fires when L_0's bundle is observed and host-validated.** At emit time: σV (with `value_root`) on each layer where the operator has an observed-and-valid bundle, NR on each absent layer.
- **Phase-2b commit-emit fires when L_0's σ-eligibility is locally met** (≥ qV `σV` verdicts for V_0 in the operator's local verdict-pool). At emit time: σ on each layer where local σ-eligibility is met, NR on each layer without.

Both fall back to the current latest-safe times if the trigger doesn't fire. **Mesh-flakiness behavior**: an operator with a flaky mesh that doesn't observe L_0's bundle (Phase-2a) or doesn't reach local σ-eligibility (Phase-2b) doesn't trigger early-emit — they fall back to the current timing. Strict win-win: flaky operators get current behavior, non-flaky operators gain the cascading savings.

### Cascading savings at Config A (current B_0 = 1·BTT, Δ_2a = Δ_2b = 2·BTT)

- L_0 leader broadcasts by `T_broadcast_max_0 = T_commit − 3·BTT`.
- Bundle reaches all honest by `~T_commit − 2·BTT` under typical propagation.
- **Verdict emit at `~T_commit − 2·BTT`** (vs current `T_commit − 1·BTT − ε_proc`) → saves `~1·BTT`.
- Verdicts propagate within 1·BTT → all peer verdicts observed by `~T_commit − 1·BTT`.
- **Phase-2b emit at `~T_commit − 1·BTT + ε_proc`** (vs current `T_commit + ε_proc`) → saves `~1·BTT`.
- Phase-2b partials propagate within 1·BTT → reconstruction completes by `~T_commit + ε_proc`.

**Total cumulative savings: ~2·BTT = 400ms** in the healthy case. The cascade is exactly the `Δ_2b` worth of slack we'd otherwise need to preserve for jitter — early-emit converts it into submission headroom.

### Residual exposure under rational-byz

The regression case: A early-emits `σV(V_0)` at T_a; byzantine selectively delivers V_0' to A only, in the window `(T_a, T_commit]`; A's Phase-2b is NR (saw equivocation), but A's verdict already claimed σV. Cluster's verdict-pool says σ-eligible; A's NR doesn't contribute to σ-pool. **Pool split → deadlock at L_0, no fall-through, slot misses at L_0.**

Triply-conditioned: (1) byzantine leader equivocates, (2) selective delivery to a specific victim, (3) timing engineered to land in early-emit window. Under the deterrent (assumption 4), conjunction is essentially nonexistent.

In the symmetric-delivery equivocation case (byz delivers V' to all honest), all honest see equivocation regardless of emit timing → all NR → NR-pool reaches qEnc → falls through cleanly. Same as synchronous.

### Case 3 (h_V=1 selective delivery) recovery preserved

The convergence-rule flip ([L103](docs/2abOBFT.md:103)) — operator flips from `σV` verdict to NR at Phase-2b when verdict-pool σ-eligibility is short — is kept. This is what enables h_V=1 recovery: 1 honest with V, 2 honest without → verdict-pool short on V → all honest flip to NR → NR-pool reaches qEnc → fall through to L_1.

**Why this is safe at f=1 n=4 regardless of flip**: Pigeonhole 2 holds structurally. With 3 honest and qV=3, two distinct V's reaching qV would need 6 honest signatures — impossible. So no risk of double-signing whether operators flip or not.

**Why we keep the flip anyway**: without it, h_V=1 has no recovery (1 honest σ + byz ≤ 2 < qV; NR-pool ≤ 2 < qEnc → deadlock). The flip converts a deadlock into a fall-through. Under rational-byz, h_V=1 is rare (byz-driven), but the recovery cost is just keeping the existing convergence rule — no protocol change needed.

**Interaction with early-emit**: in Case 3, the operator's "L_0 σ-eligibility met" trigger doesn't fire (local σ-eligibility is short — only the 1 honest with V is σV; others are NR). So the operator naturally falls back to `T_commit + ε_proc` and the convergence-rule flip runs at that point. **Recovery works identically to vanilla 2abOBFT.**

### Mesh-flakiness

Strict win-win: an operator with a flaky mesh fails to trigger the early-emit conditions and falls back to current timing (= current behavior, no change). Non-flaky operators gain the cascading savings. The 2abOBFT mesh-flakiness mitigation via the verdict-pool convergence rule ([docs/2abOBFT.md:548](docs/2abOBFT.md:548)) continues to operate normally — early-emit doesn't change what the convergence rule does, only when operators emit when their local view is complete enough to emit.

### §2.1 2abOBFT spec changes (docs/2abOBFT.md)

- **L157-181** (Phase 2a verdict emission): replace the "broadcast as late as possible" recommendation with the `min(T_L_0_bundle_observed, T_verdict_max − ε_proc)` rule. Update L181's "earlier broadcast risks" framing to "earlier broadcast is the intended behavior in healthy mesh; revision via Rule 6b absorbs the rare equivocation-window case under assumption 4".
- **L197** (Phase 2b emission): replace "operators emit at `T_commit + ε_proc`" with `T_emit_phase_2b = min(T_L_0_sigma_eligibility_met + ε_proc, T_commit + ε_proc)`.
- **New subsection §Phase 2a / Emission timing** and **§Phase 2b / Emission timing**: state the rules explicitly with fallback semantics and the cascading-savings note.
- **L181 / L455 / L112** Rule 6b commentary — substantive rewrite under early-emit:
  - Honest revision is the *expected* outcome of early-emit. Receiver-side single-mismatch enforcement would slash honest operators routinely; this is incorrect.
  - **Honest revision has two distinct in-protocol justifications under the current spec rules:**
    - **Case A — equivocation-driven flip**: operator emitted `σV(V)` verdict, then retained a second distinct V' (leader equivocation observed). Cross-phase exclusivity ([L142](docs/2abOBFT.md:142)) forces NR. Justified by Rule 2 evidence at the same `(slot, layer)`.
    - **Case B — convergence-rule flip**: operator emitted `σV(V)` verdict, cluster verdict-pool turned out to be σ-eligibility-quorum-short (h_V=1, 1-1-1 split, etc.), convergence rule ([L103](docs/2abOBFT.md:103)) flips operator to NR for fall-through. Justified by the verdict-pool composition at the same `(slot, layer)`.
  - **Slashing-quality Rule 6b evidence requires three conditions**: (a) no cluster-visible Rule 2 evidence at the same `(slot, layer)` (rules out Case A); (b) the verdict-pool at the same `(slot, layer)` was NOT σ-eligibility-quorum-short — i.e., qV `σV` verdicts on the operator's verdicted V were observed cluster-wide (rules out Case B); (c) multi-slot pattern from the same operator.
  - Cross-reference framework: receivers maintain a per-operator mismatch log keyed on `(slot, layer, operator_id)`. Entries with corresponding Rule 2 evidence are tagged "presumed honest — equivocation". Entries where the receiver's own verdict-pool view shows σ-eligibility-short are tagged "presumed honest — convergence flip". Only entries failing both tags accumulate toward the byzantine-pattern bar.
  - Cryptographic close (EKM-binding verdicts) is deferred to Open Question #16 in `docs/2abOBFT-design-notes.md`. The behavioral-pattern framework above is the operational mechanism until then.
- **§Failure modes** new subsection: "Late selective equivocation against early-emitter — slot-miss risk at L_0, no fall-through. Triply-conditioned (byz × selective × timing); under assumption 4, essentially nonexistent."
- **§Timing budget table** ([docs/2abOBFT.md:706](docs/2abOBFT.md:706)): healthy-case timing column shifts ~2·BTT earlier for Phase-2b end and reconstruction. Worst-case (synchronous-fallback) column matches current values.
- **§Liveness comparison** ([L789](docs/2abOBFT.md:789)): "V_0 broadcasts 600ms earlier than in OBFT" line — re-derive under early-emit. Both protocols benefit symmetrically.

### §2.2 2abOBFT impl changes (protocol/v2/obft/twoab/)

#### `instance.go` / `phase1.go` / `phase2a.go`

- Add `L0BundleObservedAndValidated() bool` (or equivalent channel/callback). Signal when L_0's bundle is retained and host-validity has returned.

#### `phase2a.go` (verdict emission)

- Expose verdict-emit trigger fires when `L0BundleObservedAndValidated()` OR `T_verdict_max − ε_proc` reached, whichever first.

#### `phase2b.go` (Phase-2b commit emission)

- Add `L0SigmaEligibilityMet() bool` — true when the local verdict-pool has ≥ qV `σV` verdicts for V_0 at L_0.
- Phase-2b trigger fires when `L0SigmaEligibilityMet()` + ε_proc convergence-computation OR `T_commit + ε_proc` fallback.

#### Scheduler / runner integration

- Mirror OBFT's runner change: select on (verdict-emit-ready, verdict-deadline, ctx.Done) for Phase 2a; select on (Phase-2b-ready, T_commit-deadline, ctx.Done) for Phase 2b.

#### Tests

- `protocol/v2/obft/twoab/phase2a_test.go`, `phase2b_test.go`: add early-emit-trigger coverage.
- `protocol/v2/consensustest/twoab/`: healthy-path scenario asserting verdict-emit < `T_verdict_max − ε_proc` and Phase-2b-emit < `T_commit`. Existing observer-mode tests will tighten their `DecisionTime` observations.

## §3. Combined effect (composes with reflood-resize and T_commit-tighten)

Three changes, composable. Both protocols win on all three:

| Change | OBFT | 2abOBFT | T_commit shift / MEV-fetch win |
|---|---|---|---|
| Reflood-aware B_k widening | Yes (B_0 grows) | Yes (Δ_2a grows, or B_0) | None — defensive sizing only |
| T_commit tighter (Δ_2 / Δ_2b shrink from 2·BTT to {1·BTT, 1·BTT + ε_proc}) | Yes (Δ_2: 2·BTT → 1·BTT) | Yes (Δ_2b: 2·BTT → 1·BTT + ε_proc) | OBFT: T_commit shifts +1·BTT later → MEV-fetch +200ms across all leaders. 2abOBFT (max-MEV anchor): T_commit shifts +150ms later → MEV-fetch +150ms |
| Early commit | Yes (~Δ_2 saved on healthy path) | Yes (~Δ_2b saved via cascade) | OBFT healthy-path emit: ~1·BTT earlier; 2abOBFT: ~2·BTT earlier via cascade |

The reflood-resize work is the precondition for the T_commit-tighten step (need reflood to fit in B_k / Δ_2a before taking it out of the post-T_commit budget). Early-commit composes with T_commit-tighten to keep healthy-case latency from regressing under the shorter Δ_2.

## §4. Execution order

Three separate landings, in this order. Each is independently mergeable and reversible:

1. **Reflood-aware B_k / Δ_2a widening** (landed).
2. **Early-commit for OBFT + 2abOBFT** (landed).
3. **T_commit-tighten** (landed). Composes both prior changes; recovers the post-T_commit budget that the first two changes don't need.

Land order matters because (3) shrinks the safety margin assumed by today's protocol; without (1) and (2), (3) would degrade liveness. With them, (3) is just removing slack we no longer need.

## §5. Open questions

None blocking. Resolved questions:

- **Trigger rule**: `min(T_L_0_observed, deadline)` for both protocols (§1, §2). L_0 is the load-bearing layer; staggered schedule means L_0 arrives last in the healthy case, so this matches "wait for all K" without the silent-deeper-layer wait.
- **Mesh-flakiness**: strict win-win — flaky operators fall back to current timing, non-flaky operators gain savings.
- **Verdict-lock vs convergence-rule flip in 2abOBFT**: keep the flip. Safety is preserved either way (Pigeonhole 2 holds structurally at f=1 n=4), but verdict-lock would lose Case 3 (h_V=1) recovery. Spec keeps the flip; Rule 6b receiver heuristic absorbs the resulting verdict-vs-action mismatches.
- **Rule 6b receiver heuristic** (§2.1): three-condition slashing-quality bar — no Rule 2 evidence (rules out Case A equivocation-driven flip) + verdict-pool wasn't σ-eligibility-quorum-short (rules out Case B convergence-driven flip) + multi-slot pattern. Cryptographic close deferred to Open Question #16 (`docs/2abOBFT-design-notes.md`).

## §6. Follow-up cleanups (separate, post-early-commit) — DONE

Two small notational cleanups landed alongside the T_commit-tighten step:

1. **Dropped `Δ_3` in favor of `ε_3` throughout.** `Δ_3` was `≥ ε_3 ≈ 50ms` everywhere; the two-name pattern was purely a renaming. Single-name fix across all spec docs and impl `Config` fields (`Delta3` → `Eps3`).
2. **Dropped `Δ_1` (Phase-1 fetch window duration).** Spec-only notational change — per-layer fetch windows now expressed directly as `[T_k, T_broadcast_max_k]` (OBFT spec already used this form; 2abOBFT.md and OBFTR.md updated to match).

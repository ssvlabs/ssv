# OBFT / 2abOBFT broadcast-budget resize plan

Resize the per-layer leader broadcast budget `B_k` from `{1, 1.5, 2.5}·BTT` to a **reflood-aware** schedule that accommodates one gossipsub IHAVE/IWANT reflood cycle when the initial eager-push fails to reach all honest peers.

## RefloodDelay model

Gossipsub's lazy-push reflood path uses periodic IHAVE digests followed by IWANT requests. Worst-case bundle delivery for a mesh-flaky receiver:

```
T_initial_propagation + RefloodDelay + T_IWANT_round_trip
   = 1·BTT          + RefloodDelay + 1·BTT
   = 2·BTT + RefloodDelay
```

where `RefloodDelay` is bounded by the cluster's gossipsub HeartbeatInterval. SSV configures `HeartbeatInterval = 700ms` ([network/topics/params/gossipsub.go:30](network/topics/params/gossipsub.go:30)), so the default RefloodDelay is **700ms** for SSV deployments.

`RefloodDelay` is a Config field with default 700ms. Operators with denser meshes (n=4 default, fully-connected) where eager-push reliably reaches all peers may set it lower (down to 0); operators on sparser meshes (n=10, n=13) keep the default or higher.

## Target schedule

Per-K shape, anchored on T_anchor (= T_commit for OBFT, T_verdict_start for 2abOBFT):

| K | Schedule (B_0, B_1, …, B_{K-1}) |
|---|---|
| 2 | `[2·BTT + RefloodDelay, T_anchor]` |
| 3 | `[2·BTT + RefloodDelay, 3·BTT + RefloodDelay, T_anchor]` |
| 4 | `[2·BTT + RefloodDelay, 3·BTT + RefloodDelay, 4·BTT + RefloodDelay, T_anchor]` |
| K>4 | shallow `[2·BTT+RD, 3·BTT+RD, 4·BTT+RD]`, then linear interpolation in duration space from `4·BTT + RefloodDelay` (at L_2) to `T_anchor` (at L_{K-1}) |
| K=1 | `[T_anchor]` (unchanged) |

Rationale: `B_0 = 2·BTT + RefloodDelay` = one initial propagation + one full reflood cycle (heartbeat + IWANT round trip). Deeper layers `B_k = (k+2)·BTT + RefloodDelay` keep the +1·BTT-per-layer absorption-margin pattern from the without-RefloodDelay design — each deeper backup gets one more BTT of jitter cushion on top of the same reflood-cycle base.

Edge case — RefloodDelay = 0 (fully-meshed deployments where eager push reaches all peers reliably): schedule collapses to `{2, 3, 4}·BTT`, matching the pre-RefloodDelay design.

Concrete numbers at Config A (BTT=200ms, RefloodDelay=700ms → `B_0 = 1100ms = 5.5·BTT`):

- **OBFT** K=4 (T_commit=3400ms): `B = [1100, 1300, 1500, 3400]ms` → `T_broadcast_max = [2300, 2100, 1900, 0]ms`. L_0 MEV-fetch ≈ 2140ms (vs current 3050ms).
- **2abOBFT** K=4 (T_verdict_start=1600ms): `B = [1100, 1300, 1500, 1600]ms` → `T_broadcast_max = [500, 300, 100, 0]ms`. L_0 MEV-fetch ≈ 340ms (tight; 2abOBFT's smaller pre-T_commit budget is fully consumed by reflood-absorbing B_k).

The 2abOBFT L_0 MEV-fetch degradation is severe at the default RefloodDelay; operators running 2abOBFT on dense meshes (n=4) should configure RefloodDelay = 0 (or small) to keep V_0 MEV-fresh. This is one of the trade-offs documented in the §When to use comparison.

## Out of scope (per Q3)

- No change to `Δ_1`, `Δ_2`, `Δ_2a`, `Δ_2b`, `Δ_3`, `JitterBuffer`, `RelayCutoff`, `T_commit`, `T_verdict_start`.
- No compensation for the lost MEV-fetch window. Wider B_k buys propagation safety margin; the L_0 fetch budget shrinks accordingly.

## §1. Spec changes (docs/)

### docs/OBFT.md (primary OBFT spec)

Schedule definition and per-K examples:
- L45 sizing line: `B_0 ≥ 1 BTT heuristic at Config A (≈0.5 BTT typical-mesh propagation + 0.5 BTT convergence buffer)` → rewrite for `B_0 = 2·BTT` as `≈1 BTT P99 propagation + 1 BTT convergence/jitter buffer`. **Open: keep the "typical-mesh + convergence buffer" framing (now ~0.5 + 1.5) or restate as "P99 propagation + jitter"?** Recommend the latter — the doubling is intentional headroom, not extra convergence.
- L48 example: `(B_0 = 1·BTT, B_1 = 1.5·BTT, B_2 = 2.5·BTT, ...)` → new values.
- L50 sizing intuition example: `B_0 = 1 BTT, B_1 = 1.5 BTT, B_2 = 2.5 BTT, B_3 = T_commit` → new values.

Inline references that quote the schedule:
- L7, L17, L63, L411, L475, L532, L607, L652, L660 — phrases of the form "`B_0 = 1 BTT = 200ms`" or "`B_0 = 1 BTT for the primary`" → replace `1 BTT` / `200ms` with `2 BTT` / `400ms`.

Timing-budget table:
- L697 row: `2900 | V_2 broadcast (T_commit − 2.5 BTT) | … 500ms; MEV-fetch 2750ms` → `2600 | V_2 broadcast (T_commit − 4 BTT) | … 800ms; MEV-fetch 2450ms`.
- L699 row: `3200 | V_0 broadcast (T_commit − 1 BTT) | … 200ms; MEV-fetch 3050ms` → `3000 | V_0 broadcast (T_commit − 2 BTT) | … 400ms; MEV-fetch 2850ms`.
- Need to also add/update the V_1 row (currently implied; check the actual table layout for L_1 = `T_commit − 1.5 BTT = 3100ms` → new `T_commit − 3·BTT = 2800ms`).
- L707 per-leader MEV-fetch summary: `[V_3: ~0ms, V_2: 2750ms, V_1: 2950ms, V_0: 3050ms]` → `[V_3: ~0ms, V_2: 2450ms, V_1: 2650ms, V_0: 2850ms]`.
- L151 fetch-window concrete: `L_2 by T_commit − 2.5 BTT = 2900ms, L_1 by 3100ms, primary L_0 by 3200ms` → 2600/2800/3000ms.

Comparison-to-partial-sigs commentary:
- L740: "**300ms BFT-consensus tax** over the partial-sigs floor (3050 − 3050ms) — this is the structural cost … 1 BTT V_0 leader-broadcast propagation + 0.5 BTT B_0 convergence buffer (the part of B_0 = 1 BTT beyond pure typical-mesh propagation)" → becomes a 500ms tax (3350 − 2850 = 500ms = 2.5 BTT): `1 BTT V_0 leader-broadcast propagation + 1.5 BTT B_0 jitter buffer (the part of B_0 = 2 BTT beyond pure typical-mesh propagation)`. Also touches the partial-sigs comparison phrasing.
- L763 validity-locking commentary: `V_0's window ≈ 200ms (= 1 BTT), V_1's ≈ 300ms (= 1.5 BTT), V_2's ≈ 500ms` → `400ms (= 2 BTT), V_1's ≈ 600ms (= 3 BTT), V_2's ≈ 800ms`.

L_Bid extension (the §L_Bid section uses B_0 explicitly in formulas):
- L976: `T_broadcast_max_0^bare = T_commit − B_0 (= T_commit − 1 BTT at Config A)` → `T_commit − 2 BTT`.
- L987 `B_0_LBid = 0.5 BTT at Config A vs bare OBFT's B_0 = 1 BTT` — this is a separate (tighter) budget for the L_Bid bid layer that intentionally drops the convergence buffer. With bare OBFT's B_0 doubled to 2·BTT, the "L_Bid drops the convergence buffer" framing breaks. **Decision needed:** does L_Bid's `B_0_LBid` stay at 0.5·BTT (now described as "drops the doubled buffer entirely, ~1.5·BTT savings"), or shift in proportion? Recommend stays at 0.5·BTT (rationale is unchanged — typical-mesh-only at the bid layer) and rephrase the doc accordingly.
- L1014-1016 (Conservative/Standard/Aggressive sizings): mostly self-relative, but the `shift = max(0, Δ_minicon − 0.5 BTT)` and absolute `T_0_broadcast_max` values (e.g., L1032 `T_0_broadcast_max = 3200ms`) refer to bare OBFT's deadline → update to `3000ms`. Also the MEV-fetch numbers `3050ms` → `2850ms`.
- L959 suited-for: `max(0, Δ_minicon − 0.5 BTT) MEV-fetch budget reduction` framing depends on B_0 = 1·BTT; re-derive for B_0 = 2·BTT (the buffer that L_Bid reuses is now ~1.5·BTT, not ~0.5·BTT).

### docs/2abOBFT.md (2abOBFT spec)

Schedule definition:
- L48 default schedule: `B_0 = 1 BTT, B_1 = 1.5 BTT, B_2 = 2.5 BTT, B_3 = T_verdict_start` → new values; also the "canonical staggered shallow multiples (1·BTT, 1.5·BTT, 2.5·BTT, ...)" phrasing in the degraded-operating-point text.
- L52 composition example: `L_0's total effective absorption window is B_0 + Δ_2a + 1·BTT = 4·BTT = 800ms (1·BTT pre-Phase-2a + 3·BTT Phase-2a re-flood)` → `5·BTT = 1000ms (2·BTT pre-Phase-2a + 3·BTT Phase-2a re-flood)`.
- L405 per-layer effective absorption: `At the default K=4 schedule: L_0 = 3·BTT − 1·BTT = 2·BTT total` → `L_0 = 3·BTT + (2·BTT − 2·BTT) = 3·BTT total`. (Formula `(Δ_2a + 1·BTT) + (B_k − 2·BTT)` is unchanged; only the B_0 substitution changes.)
- L691 glossary entry: `Default K=4: B_0=1·BTT, B_1=1.5·BTT, B_2=2.5·BTT, B_3=T_verdict_start` → new values.

Timing-budget table:
- L706: `Default K=4 schedule: L_3 = 0 (B_3 = T_verdict_start), L_2 = 1.10s (T_verdict_start − 2.5·BTT), L_1 = 1.30s, L_0 = 1.40s` → `L_3 = 0, L_2 = 0.80s (T_verdict_start − 4·BTT), L_1 = 1.00s, L_0 = 1.20s`.
- L707 propagation slack: `L_0's bundle (broadcast latest at 1.40s)` → `1.20s`.

Doc-text inconsistency (resolve during edit):
- L17, L668, L789, BFT-comparison.md L288 — these paragraphs describe the OBFT-vs-2abOBFT savings as `~600ms (= 3 BTT) = Phase 2a window 400ms + extra broadcast slack 200ms`, where the "200ms" was 2abOBFT's old `B_0 = 2·BTT` vs OBFT's `B_0 = 1·BTT`. BFT-comparison.md L288 already says 2abOBFT was recently mirrored to OBFT's `B_0 = 1·BTT`, which would reduce the savings to ~400ms — but L17/L668/L789 weren't updated to match. Under the new resize (both protocols at `B_0 = 2·BTT`), the broadcast slack cancels out and OBFT saves exactly `Δ_2a = 2·BTT = 400ms`. Update all four sites to a consistent `~400ms (= 2 BTT at Config A: Phase 2a window; broadcast slack identical at B_0 = 2 BTT)`.
- L789 row "V_0 broadcasts 600ms earlier than in OBFT for the same T_commit" → "V_0 broadcasts 400ms earlier (= Δ_2a)".
- L611, L657, L666 reference `Δ_2a + 1 BTT ≈ 600ms` absorption window — this is **not** B_k-derived (it's the Phase-2a uniform re-flood), so no change.

### docs/BFT-comparison.md

- L42 prose: `B_0 = 1 BTT for L_0` and the framing around "structurally compensated by K-layer fall-through" → `B_0 = 2 BTT for L_0`.
- L223 schedule listing: `B_0 = 1 BTT, B_1 = 1.5 BTT, B_2 = 2.5 BTT, B_3 = T_commit` → new values.
- L227-229 broadcast-time table (V_0/V_1/V_2 ms): `[V_0: 3200ms, V_1: 3100ms, V_2: 2900ms]` (current) and `[V_0: 3050ms, V_1: 2950ms, V_2: 2750ms]` (MEV-fetch column) → broadcast `[3000, 2800, 2600]`; MEV-fetch `[2850, 2650, 2450]`.
- L243, L250 QBFT R2 = 2150ms — unchanged (independent of OBFT B_k).
- L259, L261, L262 ranking — re-sort the MEV-fetch ranking with new OBFT V_0 = 2850ms (still beats QBFT R2 by 700ms, not 900ms).
- L271-272 narrative: "900ms more MEV-fresh fetch time than QBFT R2 (3050 vs 2150ms)" → "700ms more (2850 vs 2150)". "300ms BFT-consensus tax" → "500ms BFT-consensus tax" (3350 − 2850 = 500ms = 2.5 BTT) with new decomposition: 1 BTT leader-broadcast propagation + 1.5 BTT B_0 jitter buffer.
- L275 deeper-layer trade-off: 400ms / V_0's 3050ms → new values.
- L283-284 2abOBFT V_0/V_1 table: `~2850ms / ~2700ms` and `~2750ms / ~2600ms` — need to re-derive under the new schedule. With B_0 = 2·BTT and the same anchor T_verdict_start = 3.05s (max-MEV operating point), V_0 broadcasts at 3.05s − 2·BTT = 2.65s; MEV-fetch ≈ 2.65s − fetch overhead.
- L288 paragraph (mirror description): rewrite to reflect both protocols now at `B_0 = 2·BTT` and drop the "narrower B_0 vs prior 2·BTT single-deadline" historical contrast.

### docs/OBFTR.md (multi-round variant)

- L764 (timing table): `Phase 1 fetch (round 1) | 1100ms | T_broadcast_max_1 = 1.10s` — this is for OBFTR's round-1 timing, which inherits OBFT's per-layer schedule. Need to check the full OBFTR table for any other B_k-derived numbers. Likely needs the same `2900/3100/3200 → 2600/2800/3000` updates if OBFTR mirrors OBFT's K=4 schedule.
- Sweep: grep OBFTR.md for `1·BTT`, `1.5·BTT`, `2.5·BTT`, `B_0`, `200ms`, `300ms`, `500ms` in the timing context.

### docs/2abOBFT-design-notes.md

- No schedule-derived numbers found in grep. Sweep with the same patterns anyway during edit; expected: no changes.

### docs/OBFT-formal-verif.md, docs/OBFT-SPEC-ALIGNMENT-PLAN.md, docs/2abOBFT-IMPL-PLAN.md, docs/2abOBFT-PHASE-K-PLAN.md, docs/2abOBFT-PHASE-L-PLAN.md

- Sweep with the schedule-derived patterns; expected: minimal or no changes (these are process docs, not spec text). Update only if literal `1·BTT` / `1.5·BTT` / `2.5·BTT` mentions are found.

## §2. Impl changes (protocol/v2/)

### Spec-independent OBFT core — [`protocol/v2/obft/base/types.go`](protocol/v2/obft/base/types.go)

`DefaultBroadcastBudget` (L232-282):
- L240-254 switch arms: update K=2/K=3/K=4 cases.
  - K=2: `out[0] = btt` → `out[0] = btt * 200 / 100`.
  - K=3: `[btt, btt*250/100]` → `[btt*200/100, btt*300/100]`.
  - K=4: `[btt, btt*150/100, btt*250/100]` → `[btt*200/100, btt*300/100, btt*400/100]`.
- L255-267 default arm (K>4): shallow seed → `[btt*200/100, btt*300/100, btt*400/100]`; interpolation start point implicitly becomes `out[2] = 4·BTT`.
- Doc comments L205-225, L257 — restate the recommended values throughout.
- L95-96 LayerSpec doc comment — restate.

`Validate` (L395, L406) — unchanged (still requires non-decreasing).

### 2abOBFT core — [`protocol/v2/obft/twoab/config.go`](protocol/v2/obft/twoab/config.go)

`DefaultBroadcastBudget` (L302-353): same edit pattern as OBFT base (L310-338 switch + default arm). Doc comments L279-301 restate the canonical values.

### SSV adapter (OBFT) — [`protocol/v2/ssv/runner/obft/config.go`](protocol/v2/ssv/runner/obft/config.go)

`defaultLayerSchedules` map (L106-122):
- K=2: `shallowBudgetBTT100: {100}` → `{200}`.
- K=3: `{100, 250}` → `{200, 300}`.
- K=4: `{100, 150, 250}` → `{200, 300, 400}`.

`primaryBudgetDefaultBTT100` constant (L137): `100` → `200`. Guarded by `TestDefaultBroadcastBudgetSchedule_EndpointConstantMatchK4`.

`interpolatedBudgetSchedule` (L266-287):
- L274-275: `out[1] = btt * 150 / 100` → `* 300 / 100`; `out[2] = btt * 250 / 100` → `* 400 / 100`.

Doc comments to refresh: L21-32 (Config A header), L70-102 (per-layer-default block), L173-179 (BroadcastBudget field doc), L262-265, L306-329 (`DefaultBroadcastBudgetSchedule` doc). Mention "1·BTT / 1.5·BTT / 2.5·BTT" → "2·BTT / 3·BTT / 4·BTT" and refresh concrete examples (`[200, 300, 500, 3400]ms` → `[400, 600, 800, 3400]ms`; `[600, 900, 1500, 2600]ms` at BTT=600ms → `[1200, 1800, 2400, 2600]ms` — note three layers now collide at the cap rather than two).

L87-98 K=4 example block: re-derive `V_0 MEV-fetch budget = 3200 − 153 − 10 = 3037ms` → `T_broadcast_max[0] = 3000ms; MEV-fetch = 3000 − 153 − 10 = 2837ms` (vs spec target `2850ms` — within 13ms).

### Tests — [`protocol/v2/ssv/runner/obft/config_test.go`](protocol/v2/ssv/runner/obft/config_test.go)

Update all hard-coded expected values:
- `TestDefaultBroadcastBudgetSchedule_K3_TabulatedAtConfigA` (L16-25): want `[400, 600, DefaultTCommit]`.
- `TestDefaultBroadcastBudgetSchedule_K4_TabulatedAtConfigA` (L31-41): want `[400, 600, 800, DefaultTCommit]`.
- `TestDefaultBroadcastBudgetSchedule_K4_ScalesWithBTT` (L47-57, BTT=400ms): want `[800, 1200, 1600, DefaultTCommit]`.
- `TestDefaultBroadcastBudgetSchedule_K7_InterpolatesCleanly` (L64-81): assertions L68-70 for L_0/L_1/L_2 = `400/600/800` ms; interpolation L_3..L_5 from 4·BTT to T_commit recomputed.
- `TestDefaultBroadcastBudgetSchedule_K10_InterpolatesCleanly` (L85-98): L_0 = 400ms.
- `TestDefaultBroadcastBudgetSchedule_TCommitTooSmall_Caps` (L100-119): **scenario needs rework.** At BTT=400ms, T_commit=800ms, pre-cap shallow values under new schedule = `[800, 1200, 1600]` — L_0 itself sits at the cap, L_1/L_2 overshoot. Post-cap: `[800, 800, 800, 800]` — all layers collide at BFT_start. Either update the test to validate this stronger collapse, or pick a less-degraded operating point where only L_2 caps (e.g., BTT=300ms, T_commit=1000ms: pre-cap `[600, 900, 1200]`, post-cap `[600, 900, 1000, 1000]` — illustrates only L_2 capping).
- `TestDefaultBroadcastBudgetSchedule_EndpointConstantMatchK4` (L125-129): no change to test body; passes after the constant is bumped to 200.

### Tests — [`protocol/v2/ssv/runner/obft/runner_test.go`](protocol/v2/ssv/runner/obft/runner_test.go)

- L34 `DefaultBroadcastBudgetSchedule(4, 30*time.Millisecond, 200*time.Millisecond)`: returns `[30, 45, 75, 200]ms` today; under new schedule returns `[60, 90, 120, 200]ms`. Check whether any downstream assertions hard-code the old values. L40 expected fetchAt comment may need refresh.

### Tests — [`protocol/v2/obft/base/config_test.go`](protocol/v2/obft/base/config_test.go)

- L153-165 (K=4 with explicit B_0=1·BTT, B_1=1.5·BTT, B_2=2.5·BTT, B_3=T_commit): re-derive `cfg.Layers[k].BroadcastBudget` lines to `2·BTT / 3·BTT / 4·BTT / T_commit`.
- L115-116 (`L_0's T_broadcast_max = TCommit - B_0 = 1500 - 150 = 1350ms`): test uses B_0=150ms which is half the test fixture's BTT, predating any canonical-schedule conventions. Verify it's not asserting on the canonical schedule shape; likely unchanged.
- L183-188 (B_0 < BFT-min test): unchanged in spirit; verify the new B_0 = 2·BTT still admits this test's intent.

### Tests — [`protocol/v2/obft/base/sim_test.go`](protocol/v2/obft/base/sim_test.go)

- L118-127 `newSimWithStaggeredBudgets` comment `spec-recommended ratios (B_0=0.5·BTT, B_1=1·BTT, B_2=2·BTT, B_3=5·BTT)` — these are M3-staggered test ratios that don't match the spec defaults (current or new); they're a custom non-default schedule to exercise the M3 mechanism. Update the comment from "spec-recommended ratios" to "M3 staggered schedule test fixture" since the values were already non-canonical.

### Tests — [`protocol/v2/obft/twoab/config_test.go`](protocol/v2/obft/twoab/config_test.go)

- `TestDefaultBroadcastBudget_K2` (L375-382): want `[2·btt, tVerdictStart]`; comment `B_0 = 1 BTT` → `B_0 = 2 BTT`.
- `TestDefaultBroadcastBudget_K2_LowTVerdictStart` (L386-…): scenario uses `tVerdictStart = 150ms < BTT = 200ms`; under new schedule `B_0 = 2·BTT = 400ms > tVerdictStart` so caps to `tVerdictStart`. Test still validates the cap; expected becomes `[150, 150]`.
- `TestDefaultBroadcastBudget_DegradedOperatingPoint` (L337-351): pre-cap shallow under new schedule = `[400, 600, 800]` at btt=200ms, tVerdictStart=400ms → post-cap `[400, 400, 400, 400]` — re-derive assertions or pick a different operating point that exercises a partial cap. Same calibration concern as the OBFT config_test caps test.
- Sweep for any other tests asserting on the K=3/K=4 shape values.

### Consensustest framework

[`protocol/v2/consensustest/obft/adapter.go`](protocol/v2/consensustest/obft/adapter.go) L99: comment `1·/1.5·/2.5·BTT` → new values.

[`protocol/v2/consensustest/obft/adapter_test.go`](protocol/v2/consensustest/obft/adapter_test.go):
- L490-495, L515-521 (`MaxMEVFetch` tests): the `B_0 = 1 BTT` decomposition `"0.5 BTT typical-mesh propagation + 0.5 BTT convergence buffer"` is **load-bearing on the test setup** — the test uses `ConstantDelay{D: BTT/2}` to model typical-mesh propagation, leaving exactly the convergence-buffer slack. With B_0 = 2·BTT, the test still exercises the same mechanism but with much more headroom: `ConstantDelay{D: BTT/2}` now leaves `1.5·BTT` of buffer. Update the comments to reflect the new decomposition; the test behavior may pass unchanged (more buffer = same `decide-at-L_0` outcome). The `FallsThroughWhenConvergenceBufferConsumed` test uses `ConstantDelay{D: BTT}` (full-BTT propagation) which under B_0 = 1·BTT consumed the entire budget — under B_0 = 2·BTT it now leaves 1·BTT of buffer and the test would no longer fall through. **This test needs re-anchoring:** either increase the delay to `2·BTT` (full new-B_0 consumption) or recharacterize it as "boundary-with-tail-jitter" by adding 1 jittered hop.
- L807, L853, L856 (1.5·BTT dispatch delay overrides): these are byzantine dispatch convention numbers, not B_k-derived. Verify in context; likely unchanged.
- Other `T_commit + 1·BTT = 3600ms` references (L268-372): KindCommit propagation timing, independent of B_k. Unchanged.

[`protocol/v2/consensustest/twoab/adapter_test.go`](protocol/v2/consensustest/twoab/adapter_test.go): T_commit-based timings (L53, L66) are independent of B_k. Unchanged. Sweep for any B_k-derived numbers.

[`protocol/v2/consensustest/sweep_test.go`](protocol/v2/consensustest/sweep_test.go) L308-387, L454: scenario commentary referencing `B_0 = 1 BTT = 200ms`. Update comments to reflect B_0 = 2·BTT. Check whether the scenarios use `cfg.BTT` (auto-scales) or hard-coded `200ms` (needs scenario-by-scenario re-derivation — e.g., `pair-wise max = 2δ` calculation at the spec edge `B_0 = P99 + 2δ exactly fits` was tuned to `B_0 = 1·BTT`; under `B_0 = 2·BTT` the "exactly fits" framing breaks).

[`protocol/v2/consensustest/catalog_propagation.go`](protocol/v2/consensustest/catalog_propagation.go) L162-300, [`catalog_silent.go`](protocol/v2/consensustest/catalog_silent.go) L82, [`psigs/adapter_test.go`](protocol/v2/consensustest/psigs/adapter_test.go), [`psigs/byz.go`](protocol/v2/consensustest/psigs/byz.go) L46: `SlotStart + 1·BTT` refers to PSigs partial arrival time (one-hop propagation, B_k-independent). The `psigs/byz.go` "matches the OBFT adapter's 1.5·BTT convention" — verify in adapter; likely a separate convention. Unchanged.

## §3. Execution order

1. Spec edits first — settle the doc-text inconsistency (the "saves ~600ms" stale phrasing) before touching code, so impl comments reference the final spec wording.
2. Core impl (`obft/base/types.go`, `obft/twoab/config.go`).
3. SSV adapter (`ssv/runner/obft/config.go`) + constant bump.
4. Test updates (config_test, runner_test, sim_test, twoab/config_test).
5. Consensustest sweeps — re-anchor the `FallsThroughWhenConvergenceBufferConsumed` test in particular.
6. `make unit-test` and verify the OBFT/2abOBFT packages green.

## §4. Open questions to resolve during edit

- **B_0 = 2·BTT decomposition phrasing**: keep "0.5 BTT typical-mesh propagation + 1.5 BTT convergence buffer" (preserves existing framing, larger buffer share) or restate as "1 BTT P99 propagation + 1 BTT jitter/skew buffer" (cleaner, matches the doubling intent)? **Recommend: the latter.**
- **L_Bid `B_0_LBid = 0.5·BTT` policy**: stays at 0.5·BTT regardless of bare OBFT's B_0 (it's typical-mesh-only at the bid layer), or scales with bare OBFT? **Recommend: stays at 0.5·BTT; restate the rationale "L_Bid drops the convergence buffer" as "L_Bid budgets only typical-mesh propagation" so the framing is invariant to bare-OBFT B_0 changes.**
- **Degraded-operating-point test scenarios**: pick re-anchoring points where the new B_0 = 2·BTT produces a partial cap (one or two layers cap, not all three) so the test exercises the cap-then-clamp pathway meaningfully.

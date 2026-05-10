# 2abOBFT — Design notes

**Non-load-bearing reference material.** This document preserves the design-plan discussion accumulated during development of 2abOBFT — variants considered, detailed scenario walkthroughs, edge cases, open implementation questions, and the build-phase plan. **It is not part of the canonical [2abOBFT spec](2abOBFT.md).** Where this document and the spec differ, the spec is authoritative. The content here is preserved as historical reference and as a tracking record of discussions and ideas that may apply to future implementation.

## Status

- **Variant chosen**: "Variant C" below — no Phase-1 leader σ_V; verdict broadcasts in Phase-2a; σ/NR commit in Phase-2b. Justification follows.
- **Scope**: SSV proposer duty at `n = 4, f = 1, K = 4` as the running example; algebra generalizes to higher `n`/`f`.
- **Relationship to existing code**: bare OBFT (without Phase 2a/2b) is implemented in [protocol/v2/obft](../protocol/v2/obft/). 2abOBFT extends it by adding the Phase-2a observation phase; several 2abOBFT pieces are drop-in additions on top of the existing bare-OBFT state machine.

## What changes vs bare OBFT

| Component | OBFT | OBFT + Phase 2a/2b (this) |
|---|---|---|
| Phase-1 bundle | `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` | `(V_{L_k}, σ_{L_k}^{op}(envelope))` — **no σ^V** |
| Phase-2 structure | Single Phase 2 (single `KindCommit` at `T_commit`) | Two windows: Phase 2a (verdict broadcast, no σ partials) + Phase 2b (σ-or-NR commit) |
| Operator commitment states | σ / NR / NV (3 states) | σ / NR / NV (3 states) |
| σ-commit timing | Phase-1 (leader) or Phase-2 at `T_commit` (others) | Phase-2b only (uniform across all operators) |
| Convergence mechanism | Per-operator local view at `T_commit` based on retained V's | Cluster-wide verdict observation in Phase-2a, σ-quorum-eligibility check at Phase-2a end |
| EKM coordination | Single signing event per (slot, layer) per operator (V-share + IBE-share) | Same — single signing event per (slot, layer) per operator at Phase-2b; verdict envelope is op-identity-signed (not threshold) and does not consume EKM slashing-protection |
| Equivocation σ-locked split recovery | None — slot-miss class | Recovered structurally — σ-quorum-eligibility short → all honest go NR → fall-through |
| h_V=1 selective-delivery deadlock | Class B grief, not recovered in-protocol — the new Phase-2.5 σ-flip / NR-flip preserves safety but doesn't close h_V=1 at f=1, n=4 (σ-flip's `snap_NR_nl < f+1` trigger blocked when 2 honest NR; leader-only NR-flip does not apply when leader is byz). Deterred via Assumption 4 across slots. | Recovered structurally |
| Validity-divergence recovery | Out-of-scope (Class A) | In-scope at f=1 n=4 (recovered by NR-quorum fall-through); structural at higher n/f |
| Slot timing | `T_commit + Δ_2 + Δ_3` ≈ 500ms post-T_commit (Config A) | `T_commit + Δ_2a + Δ_2b + Δ_3` ≈ 1100ms post-T_commit (+600ms for Phase-2a window + Δ_3 difference) |
| Wire kinds | `Phase1Bundle`, `KindCommit`, `KindCertificate` | + `KindVerdict` (Phase-2a, op-identity-signed verdict envelope); Phase-2b uses its own commit message |
| Slashing-evidence rules | 5 rules | 5 rules + 1 (verdict-vs-action equivocation) |
| Late-deepest-layer leader broadcast (Class A) | Mitigated by K ≥ f+2; class-A residual | Closed structurally — late bundle observed in Phase-2a is σ-emittable in Phase-2b |
| Mesh-flakiness coordinated with byz σ-refusal (Class B) | Slot-miss surface | Mitigated — deferred commit lets brief flakiness recover |

## Variants considered

Three coherent design points emerge for adding a Phase 2a/2b split to OBFT, differing on whether Phase-2a carries σ partials and whether Phase-1 keeps the leader's σ_V:

### Variant A — Phase-1 σ_V kept, Phase-2a onion carries σ

Leader signs Phase-1 σ_V (same as bare OBFT). Phase-2a onion carries `V_{L_k}` plaintext and `C_k(σ_i^V(V_{L_k}))` (σ partial wrapped in chained IBE). Operators σ-commit in Phase-2a if they have V; Phase-2b is for late-comers who recover V from a peer's onion (gated by an `f+1`-distinct-Phase-2a-σ-signers witness threshold).

- **Pros**: narrowest spec gap to fill on top of bare OBFT.
- **Cons**: keeps Phase-1 σ_V — leader is σ-locked at Phase-1; **does not recover validity-divergence** (the leader's Phase-1 σ_V on stale V remains the structural blocker). At `f=1 n=4` the witness threshold `f+1 = 2` adds no widening over a leaner construction. Equivocation σ-locked splits where honest σ-commit on different V's at Phase-2a still fail.

### Variant B — Phase-1 σ_V kept, Phase-2a observation-only

Leader signs Phase-1 σ_V (same as OBFT base). Phase-2a is bundle re-flood + verdict broadcast, no σ partials. Phase-2b: each operator σ-or-NR-emits based on Phase-2a verdict observations. Cross-phase exclusivity: leader is σ-locked from Phase-1; non-leaders commit at Phase-2b.

- **Pros**: keeps the leader's Phase-1 σ_V head-start (one σ partial cluster-wide as soon as Phase-1 succeeds), helping marginal-receive cases reach qV at L_0.
- **Cons**: leader's Phase-1 σ_V still locks them on stale V at validity-divergence. At `f=1 n=4` with all-honest 2-σ vs 2-NV split (leader on σ-side), σ-pool = 2 < qV = 3; non-leader σ-eligible can rule-flip to NR, contributing to NR-pool = 3 = qEnc → fall-through. **At higher `n`/`f` (e.g., `f=2 n=7` with 4-3 validity split)**: σ-pool eligible = 4 < qV = 5, all non-leader σ-eligibles rule-flip to NR, NR-pool = 1+3 = 4 < qEnc = 5 — **slot misses; validity-divergence not recovered**. Variant B is f=1-n=4-tolerable but doesn't generalize.

### Variant C — No Phase-1 σ_V (chosen)

Leader broadcasts only `(V_{L_k}, σ_{L_k}^{op}(envelope))` — no σ^V partial in Phase-1. Phase-2a is bundle re-flood + verdict broadcast. Phase-2b: every operator (including the leader) σ-or-NR-emits based on Phase-2a verdict observations.

- **Pros**: leader is not locked at Phase-1; their verdict is re-evaluated at Phase-2b time. Validity-divergence recovers at all `n`/`f` because the leader can join the NR-pool when their verdict flips. Equivocation σ-locked split, h_V=1 selective-delivery, late-deepest-layer-leader-broadcast — all recover structurally via the convergence rule (broader than OBFT's narrow Phase-2.5 σ-flip / NR-flip R1/R2/R3 recovery scope; in particular OBFT does NOT close h_V=1 in the new design — see [OBFT.md §Failure modes](OBFT.md#failure-modes)). Mesh-flakiness mitigates because Phase-2a's window absorbs brief observability outages.
- **Cons**: marginal-receive cases (`h_V` = 2 at f=1 n=4) lose the Phase-1-σ_V head-start. They no longer σ-quorum at L_0 directly; instead they fall through to L_1. At K = n = 4 this costs one local-decryption iteration in Phase 3 — **no extra RTT** since Phase 3 walks layers via local decryption, not RTT-per-layer. Slot still succeeds.

**Why Variant C was chosen.** Recovering the Class A validity-divergence deadlock requires that the leader not be pre-locked at Phase 1 — without a Phase-1 σ_L^V, Phase-2b's σ-emit can land on the post-observation stabilized V regardless of the leader's at-fetch verdict. Variants A and B keep Phase-1 σ_V and consequently fail to recover validity-divergence at the boundary; only Variant C does. The marginal-`h_V` cost is acceptable because L_0 → L_1 fall-through is cheap (no extra RTT), and the recovery scope is genuinely wider.

## Liveness analysis

OBFT's recovery scope ([docs/OBFT.md / Liveness](OBFT.md#liveness-synchrony-conditional)) extends in three places. We walk through each scenario class.

Running example: `f = 1, n = 4, K = 4`. Honest A, B, C; byzantine D (when present). Leader at L_0 unless stated otherwise.

### Healthy path

All 4 operators receive `V_{L_0}` via gossipsub within `1 BTT`.

- Phase-2a: all 4 operators verdict-claim `σV` on V_{L_0}. `verdict_pool[V_{L_0}] = 4`, `nr_pool = 0`.
- Phase-2a end: σ-eligibility-quorum reached on V (4 ≥ qV = 3). All 4 operators σ-emit at Phase-2b.
- σ-pool actual = 4 (assuming byz cooperates) or 3 (if byz defects to NR — but EKM blocks defection if byz already σ-claimed cross-claim slashing applies; the σ-pool from honest is still 3 ≥ qV).
- Slot succeeds at L_0 in 3 RTTs (Phase 1 + Phase 2a + Phase 2b, with Phase 3's local decryption adding ε_3). At Config A: ~700ms total to certificate gossip start.

### Marginal-receive cases

#### h_V = 3 (3 of 4 operators received V on time; 1 didn't)

Suppose A, B, D have V; C does not (e.g., C's gossipsub mesh delivered V late, past T_accept_max).

- Phase-2a verdicts: A_σV, B_σV, D_σV (assuming D cooperative), C_NR. `verdict_pool[V] = 3 ≥ qV`.
- Phase-2a end: σ-eligibility-quorum on V. A, B, D have V locally → σ-emit. C does not have V locally → NR per the convergence rule (σ-eligibility met but no V to sign).
- Phase-2b actual: σ-pool = 3 (A, B, D); NR-pool = 1 (C). σ-quorum reached at L_0. Slot succeeds.

#### h_V = 2 (2 of 4 operators received V; 2 did not)

Suppose A, B have V; C, D do not.

- Phase-2a: A_σV, B_σV; C_NR, D_NR (or D arbitrary). `verdict_pool[V] = 2 < qV = 3`. `nr_pool = 2 < qEnc = 3` (if D NR; if D σV claim then 1).
- Phase-2a end: neither quorum eligible. Convergence rule: A and B (had V, σ-eligibility short) → both NR per rule. C, D → NR per default.
- Phase-2b actual: σ-pool = 0; NR-pool = 4 (or 3 if D σ-emitted-but-defected — but D would need to σ-claim and then EKM would block their NR; if D σ-claims at Phase-2a end, they σ-emit, σ-pool = 1, NR-pool = 3 = qEnc; either way NR-quorum reaches).
- NR-quorum at L_0 → fall-through to L_1. If L_1 leader honest and healthy, L_1 σ-quorum reaches in the same Phase 3 reconstruction walk (no extra RTT).
- **Cost vs bare OBFT**: bare OBFT with `h_V = 2` has σ-pool = 2 (honest σ) + 1 (leader Phase-1 σ_V) = 3 = qV → succeeds at L_0. Variant C falls through to L_1. One extra reconstruction-walk iteration in Phase 3 (no RTT).

This is the price of Variant C: the marginal `h_V = 2` case at f=1 n=4 falls through one layer instead of succeeding at L_0. Acceptable trade for the equivocation/h_V=1/validity-divergence recoveries below.

#### h_V = 1

Suppose only A has V; B, C don't; D byzantine (silent or arbitrary).

- Phase-2a: A_σV; B_NR, C_NR; D arbitrary. `verdict_pool[V] = 1 + maybe-byz ≤ 2 < qV`. `nr_pool = 2 + maybe-byz`.
  - If D verdicts NR: `nr_pool = 3 = qEnc` → NR-eligibility quorum reached. All operators (incl. A) commit NR. NR-pool actual = 3 or 4 → fall-through.
  - If D verdicts σV: `nr_pool = 2 < qEnc`, `verdict_pool[V] = 2 < qV` → neither eligibility met. A (had V, σ-eligibility short) → NR per rule. B, C, D → NR or whatever D wants. NR-pool actual = at least 3 (A, B, C) → fall-through.
  - If D silent (no verdict): same as D verdicts NR above with D missing — `nr_pool = 2 < qEnc`, fall back to per-operator default. A → NR (rule), B → NR, C → NR. NR-pool = 3 = qEnc. Fall-through.
- **All sub-cases recover via NR-quorum fall-through.** OBFT base would slot-miss at L_0 here ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)); Variant C structurally fixes it.

### Equivocation σ-locked split

Byzantine D = leader equivocates at L_0. Patterns from OBFT's analysis ([docs/OBFT.md / Equivocation handling](OBFT.md#equivocation-handling)). **At 2-1 cases, Variant C regresses vs bare OBFT**; documented after the recovered cases.

#### 1-1-1 split

D delivers V_a to A, V_b to B, V_c to C (each a distinct V) near end of Phase-1, leaving inadequate re-flood time.

- Phase-1 retention: A retains V_a; B retains V_b; C retains V_c.
- Phase-2a: A_σV(V_a), B_σV(V_b), C_σV(V_c); D verdicts arbitrary.
- During Phase-2a window: gossipsub re-flood. Bundles for V_a, V_b, V_c propagate among honest. By Phase-2a end:
  - **If re-flood completes within Phase-2a (`Δ_2a ≥ 1 BTT` from byz's late Phase-1 delivery)**: A retains V_a + V_b + V_c → equivocation observed → A's verdict was already broadcast as σV(V_a), but A's commit at Phase-2a end is `NR-due-to-equivocation` (the convergence rule's equivocation-observed branch overrides earlier verdict). **However A already broadcast σV(V_a) verdict** — so verdict-vs-action mismatch occurs. Honest A's verdict-vs-action mismatch is permitted under the convergence rule (the rule explicitly allows commit ≠ verdict when equivocation is observed); it is not slashable for honest. (Slashable detection should distinguish honest verdict-vs-action revision from byzantine verdict-vs-action equivocation. See "Edge cases / Honest verdict-vs-action revision".)
  - At Phase-2a end with all honest in `NR-due-to-equivocation`: NR-pool actual = 3 (A, B, C) ≥ qEnc → fall-through to L_1.
- **If re-flood does NOT complete within Phase-2a** (byz times deliveries to push re-flood past T_accept_max for *each* honest): A only retains V_a; B only V_b; C only V_c.
  - Phase-2a: A_σV(V_a), B_σV(V_b), C_σV(V_c). `verdict_pool[V_a] = 1; verdict_pool[V_b] = 1; verdict_pool[V_c] = 1` (plus byz's verdict, ≤ 1 distinct).
  - At Phase-2a end: no V has σ-eligibility quorum (max 2 with byz vote). NR-pool = 0 (none verdict-claimed NR). Per the convergence rule, A had V_a + verdict σV but no σ-quorum-eligibility → A goes NR. Same for B, C.
  - NR-pool actual = 3 (A, B, C). Fall-through. ✓

**Either sub-case recovers.** OBFT base 1-1-1 split slot-misses ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)); Variant C structurally fixes it via the σ-eligibility-quorum-short rule.

#### 1-1-NR-C / 1-NR-NR

These are sub-cases of byzantine selective-delivery patterns (an honest in `NR` here is either silent-leader-NR or equivocation-NR per bare OBFT's commit rules). The convergence rule resolves them the same way: any honest split that doesn't reach `σ_eligibility_quorum = qV` results in all honest going NR → NR-quorum reaches → fall-through.

#### 2-1 split — REGRESSION vs bare OBFT

D delivers V to {A, B}, V' to {C}. (One of OBFT's "naturally recovered" cases — see [docs/OBFT.md / Equivocation handling](OBFT.md#equivocation-handling).)

- Bare OBFT: D's Phase-1 σ_V(V) is on the wire. σ-pool on V = A + B + D = 3 = qV. Slot succeeds at L_0 *regardless of D's Phase-2 cooperation* — the Phase-1 σ partial is cryptographically locked-in.
- Variant C: D has no Phase-1 σ_V. D issues a Phase-2a verdict (σV(V) | σV(V') | NR | silent) and a Phase-2b action. Outcomes by D's behavior:
  - **D cooperates (verdict σV(V) + Phase-2b σ on V)**: `verdict_pool[V] = 3 ≥ qV`. A, B, D σ-emit. C does not have V_local → C NR per rule. σ-pool = 3 = qV. Slot succeeds at L_0. ✓ Same as OBFT.
  - **D silent (no verdict, no Phase-2b emission)**: `verdict_pool[V] = 2 < qV`. Per rule, A and B (had V, σ-eligibility short) → NR. C → NR (had V' only, σ-eligibility on V' short). NR-pool = 3 = qEnc. Fall-through to L_1. **One layer of latency added vs OBFT.**
  - **D defects (verdict σV(V) + Phase-2b NR-emit or silent)**: `verdict_pool[V] = 3 ≥ qV`. A, B σ-emit; D withholds σ. σ-pool = 2; NR-pool = 1 (C) + (1 if D NR-emit, 0 if silent) ≤ 2 < qEnc. **Slot misses at L_0.** Evidence: Rule 6 cryptographic under NR-emit, behavioral only under silent. **Strictly worse than OBFT base.**

The 2-1-byz-defect regression is real. OBFT's cryptographic lock on Phase-1 σ_V is what makes 2-1 patterns naturally recover; removing σ_V trades that recovery for the structural fixes elsewhere (1-1-1 split, h_V=1, validity-divergence, late-deepest-layer). Across many slots, Rule-6 deterrent absorbs the byzantine-defect-grief cost; per-slot, this case slot-misses cleanly.

A symmetric pattern — D delivers V to {A}, V' to {B, C} — has the same shape with V and V' swapped. Same outcomes by D's behavior.

#### Recovery summary

| Equivocation pattern | Bare OBFT | Variant C |
|---|---|---|
| 1-1-1 split (no re-flood) | Slot misses | Recovered (NR-quorum fall-through) |
| 1-1-1 split (re-flood completes in Phase-2a) | Slot misses | Recovered (equivocation observed → all NR) |
| 2-1, byz cooperates | Succeeds at L_0 | Succeeds at L_0 (tie) |
| 2-1, byz silent | Succeeds at L_0 (Phase-1 σ_V lock) | Falls through to L_1 (one extra layer's latency, still succeeds) |
| 2-1, byz defects (verdict σV + action NR) | Succeeds at L_0 (Phase-1 σ_V lock) | **Slot misses** (NR-pool short of qEnc) |
| All-equivocation-NR (early byz delivery) | Recovered via L_1 fall-through | Recovered via NR-quorum fall-through |

**Net liveness change vs bare OBFT**: gains 1-1-1 recovery (worst-case in OBFT); loses 2-1 byz-defect (new regression). Across realistic byzantine behavior (per the rational-byzantine-deterrent assumption — a rational byz faces the same fee outcome as going offline and gains nothing by defecting with on-wire evidence), Variant C is net positive in expectation. For deployments with short-horizon byzantines that don't value cluster fee accrual, the regression is a real cost.

### h_V = 1 selective-delivery (Class B in OBFT)

Already covered above under "Marginal-receive cases / h_V = 1". Variant C recovers structurally via the convergence rule. **Bare OBFT does NOT close this case in the new Phase-2.5 design** — σ-flip's trigger requires `snap_NR_nl < f+1`, but at h_V=1 each honest non-leader sees `snap_NR_nl = 2f` from their snapshot (under within-budget propagation, all honest NRs are visible cluster-wide), blocking σ-flip. Leader-only NR-flip cannot apply when the leader is byzantine (the typical h_V=1 trigger). Bare OBFT classifies h_V=1 selective Phase-1 delivery as Class B grief deterred via Assumption 4 rather than protocol-closed; see [OBFT.md §Failure modes](OBFT.md#failure-modes) and [OBFT-formal-verif.md §5.2](OBFT-formal-verif.md#52--verification-approach).

### Validity-divergence (Class A in OBFT)

A re-org during Phase-1 acceptance window splits honest verdicts: some operators say V valid (parent_root matches their pre-reorg head), some say invalid (parent_root mismatches their post-reorg head).

#### All-honest 2-σ vs 2-NV at f=1 n=4

- 2 operators (incl. leader) verdict σV; 2 operators verdict NV.
- `verdict_pool[V] = 2 < qV = 3`. `nr_pool = 2 < qEnc = 3`. Neither eligibility met.
- Per convergence rule: σV-side honest (had V, σ-eligibility short) → NR. NV-side honest → NR (their verdict). 
- NR-pool actual = 4 (all 4 honest NR) ≥ qEnc → fall-through to L_1. ✓
- Bare OBFT here: slot misses (leader's Phase-1 σ_V locks them to σ; non-leader σ-eligible can't switch; NR-pool capped at 2 < qEnc). Variant C recovers.

#### Higher n/f (e.g., 4-3 split at f=2 n=7)

- 4 σV verdicts, 3 NV verdicts. `verdict_pool[V] = 4 < qV = 5`. `nr_pool = 3 < qEnc = 5`.
- All σV-side honest → NR per rule. NV-side honest → NR.
- NR-pool actual = 7 ≥ qEnc → fall-through. ✓
- Hybrid (Variant B) here would have leader Phase-1 σ_V locked: 1 leader σ-locked + 3 σV-side rule-flipped to NR + 3 NV-side NR = 6 NR partials. NR-pool = 6 ≥ qEnc = 5 → fall-through? Wait, let me recount: the leader is σ-locked in Variant B, so they emit σ partial (1 σ) and not NR; 6 non-leader honest can NR. NR-pool = 6 ≥ qEnc = 5. Variant B *also* fall-throughs at f=2 n=7 cleanly. Hmm.
  - Actually I was wrong earlier. At f=2 n=7 4-3 split with Variant B: σ-pool = 1 leader + 3 σV-side honest = 4 partials. NR-pool from non-leader honest = 6 ≥ qEnc = 5. Fall-through happens.
  - The point where Variant C helps over Variant B is when the leader IS the validity-flipper. If leader's verdict flips to NV (they joined the NV side), Variant C lets the leader NR; Variant B keeps leader σ-locked. At higher splits this matters more, but at the recommended SSV configurations (n ≤ 13) the difference is small and may be invisible at f=1 n=4.
  - **Conclusion**: Variant C is uniformly safer for validity-divergence; Variant B is fine at f=1 n=4 and only differs at higher n/f or when the leader is on the divergent side.

### Late-deepest-layer leader broadcast (Class A in OBFT)

Bare OBFT's failure mode ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)): deepest-layer leader broadcasts past T_accept_max → all honest treat as silent → NR-quorum at L_{K-1} → walk advances past L_{K-1}, no L_K → slot misses.

In Variant C, late-arriving Phase-1 bundles are auth-only-retained until Phase-2a ends. If the bundle re-floods to all honest before `T_commit + Δ_2a − 1 BTT`, honest can verdict-claim σV on V; verdict-pool reaches qV; Phase-2b σ-emit on the late V. **Slot succeeds where bare OBFT fails.**

Conditions for recovery:
- Bundle propagates to all honest before `T_commit + Δ_2a − 1 BTT`. At Config A recommended Δ_2a = 400ms, this is 200ms past T_commit.
- Operationally: the leader's late broadcast is observed by at least one honest peer who re-floods immediately. The re-flood completes within `1 BTT` of the late observation.

### Mesh-flakiness coordinated with byz σ-refusal (Class B in OBFT)

Bare OBFT's failure mode ([docs/OBFT.md / Failure modes](OBFT.md#failure-modes)): mesh-flaky honest A NR-emits early → A is NR-locked → byz refuses σ → σ-pool short → deadlock.

Variant C: A's verdict at Phase-2a is based on whether A has V locally. If A's mesh recovers during Phase-2a (delivering V via re-flood), A can verdict σV. Or, if A's mesh stays poor and A verdict-claims NR, but other honest converge on σ (verdict_pool[V] ≥ qV), A still defaults to NR per the rule (V_local missing). σ-pool from healthy honest may still reach qV without A. **At f=1 n=4 with leader honest + 2 healthy honest**: σ-pool = 3 = qV. Slot succeeds without A.

This is wider mesh-flakiness mitigation than bare OBFT. The Phase-2a observation window is a structural buffer for transient flakiness.

### Sustained partition (Class A — unchanged)

Real propagation > absorption window: bundles don't reach honest in time → all honest NR-pool short → slot misses cleanly. Same as OBFT.

Variant C's absorption window is `Δ_2a + 1 BTT` (the Phase-2a-end horizon) — same shape as OBFT's `Δ_2 + 1 BTT`. At recommended sizing both are ~600ms at Config A.

### > f operators offline/byzantine (Class A — unchanged)

Standard 3f+1 violation → slot misses. Same as OBFT.

## Edge cases — where things can go wrong

### Verdict broadcast timing

**E1: Operator broadcasts verdict too early in Phase-2a.** They commit before observing late-arriving bundles. If a late bundle would have changed their verdict, they're locked on the early verdict (verdict envelope is op-identity-signed; broadcasting a second different verdict is verdict-equivocation, slashable).

- **Mitigation**: operators broadcast verdict as late as possible within Phase-2a, no earlier than `T_commit + Δ_2a − 1 BTT`. This gives maximum time for bundle re-flood while still allowing the verdict to propagate before Phase-2a end.
- **Failure mode**: an honest operator with a buggy timer broadcasts verdict at `T_commit + 50ms` (way too early). They may verdict NR before a late bundle arrives. Their NR verdict counts in `nr_pool`. If the cluster reaches NR-quorum, fall-through happens (still recovers). If not, the operator may have to emit NR at Phase-2b (since their verdict was NR) even though V arrived later — but then they'd have V_local but NR-verdict; the convergence rule says: if `nr_eligibility_quorum` is met, NR (regardless of V_local); if not, follow own verdict. Per rule, they NR-emit. Slot may still recover via other operators' σ-emits if `verdict_pool[V] ≥ qV` from those who waited.
- **Recommendation**: implementation should default to "verdict at `T_commit + Δ_2a − 1 BTT` minus a small operator-side processing buffer", not earlier.

### Verdict equivocation by operator

**E2: Byzantine `i` broadcasts σV(V) to peers A and σV(V') to peers B and NR to peers C.** Each peer first-observes a different verdict.

- **Honest detection**: any peer who observes ≥ 2 distinct verdicts from `i` at the same (slot, layer) treats `i` as a verdict-equivocator. Both envelopes are slashable evidence.
- **Convergence input**: each peer counts only `i`'s **first-observed** verdict; subsequent verdicts are dropped from convergence pools.
- **Cluster-wide effect**: at f=1 with byz `i`, byz contributes ≤ 1 verdict per pool per peer. Different peers may count `i` toward different pools (A counts `i` as σV(V); B counts `i` as σV(V'); C counts `i` as NR). Per-peer convergence may differ. **This is a real surface area**: peers may converge differently because their first-observed verdicts differ.

  Concrete: at f=1 n=4 with leader honest + 3 honest σV(V) verdicts, byz issues 4 different verdicts to 4 different peers:
    - A's first-observed from byz = σV(V'). A counts `verdict_pool[V] = 3 (honest) + 0 (byz, on V'); verdict_pool[V'] = 1 (byz)`. A converges on σ on V (3 ≥ qV).
    - B's first-observed = NR. `verdict_pool[V] = 3; nr_pool = 1`. B converges on σ on V.
    - Etc.
  - Across all peers, σ-eligibility on V is consistently observed → all 4 honest σ-emit on V → σ-pool = 4 ≥ qV. ✓
- **Edge of convergence divergence**: at higher f or pathological verdict equivocation patterns, peers may converge differently. For SSV proposer at f=1 n=4, the verdict-equivocation-induced divergence is bounded; need careful analysis at higher f.

### Honest verdict-vs-action revision

**E3: Honest A verdicts σV(V) at Phase-2a; mid-Phase-2a A retains a second V' (equivocation observed via late re-flood); A's Phase-2b commit is NR-due-to-equivocation per the convergence rule.**

- A's Phase-2a verdict envelope says σV(V); A's Phase-2b NR partial on `nr_tag_k`. Rule 6 (verdict-vs-action equivocation) would flag this, but A is honest.
- **Fix**: Rule 6 is conditional on the cluster's verdict pool. Honest revision when equivocation is observed is permitted. Receivers who observe A's mismatch should check whether A had grounds to revise:
  - Did the cluster also observe equivocation at this layer? (E.g., is there a `verdict_pool[V']` with at least one entry?) If so, A's revision is plausibly honest.
  - Was σ-eligibility-quorum reached cluster-wide? If yes and A still NR'd, that's a stronger byzantine signal.
- **Implementation**: receivers who detect a Rule-6 mismatch should retain the evidence (verdict + action) but not propagate as slashable until they have enough cluster-wide context. This is a fundamental limitation of Rule 6 (gossipsub-pattern-quality) — it's a deterrent, not a clean self-contained rule.

### Byzantine verdict-vs-action equivocation

**E4: Byzantine D verdicts NR in Phase-2a, then Phase-2b σ-emits on V (or vice versa).**

- The cluster may have converged on σ-eligibility-quorum on V (verdict_pool[V] = qV from honest). D's Phase-2b σ on V doesn't violate that — D's σ contributes to σ-pool, slot succeeds.
- The slashable mismatch is between D's Phase-2a verdict (NR) and Phase-2b action (σ). Rule 6 evidence is the verdict envelope + the σ partial (or its auth envelope).
- **Effect on slot**: minor — D's σ partial may put σ-pool above qV. Slot likely succeeds. Slashable.

### Late-arriving bundle vs op-identity verdict equivocation

**E5: Honest A first-observes V_a at T_commit + 100ms, broadcasts verdict σV(V_a) at T_commit + 150ms. Then at T_commit + 250ms, a re-flooded V_b (byzantine equivocation) arrives at A. A now retains V_a + V_b.**

- A's Phase-2a verdict was σV(V_a); A's commit at Phase-2a end is `NR-due-to-equivocation` (equivocation observed override).
- A cannot broadcast a second verdict to revise — that would be A's verdict equivocation.
- A emits NR at Phase-2b. Rule 6 evidence (verdict σV(V_a) + action NR) — receivers must check cluster context; equivocation observed cluster-wide is the honest revision condition.
- **Implementation note**: this case requires receivers to retain the operator's first verdict + full Phase-1 retention state. To enable receivers to verify a Rule 6 mismatch is honest, the gossiped equivocation evidence (the V_a, V_b bundle pair) must be globally observable. Receivers without the equivocation evidence may falsely flag A as Rule-6 byzantine.
- **Mitigation**: Rule 6 attribution is best-effort and should not trigger automated slashing or automated blacklist without cluster-wide consensus on the evidence. This is consistent with OBFT's slashing model (manual blacklist by surviving operators; planned protocol extension).

### Encrypted Phase-2b σ at deeper layers

**E6: At layer k > 0, Phase-2b σ partials are chained-IBE-encrypted under nr_tag_0..nr_tag_{k-1}. They cannot be verified by receivers until prior NR-quorums unlock decryption.**

- Same as OBFT's Phase-2 onion at deeper layers. No new attack surface, but the implementation must wrap Phase-2b σ partials in chained IBE the same way OBFT's Phase-2 onion does.
- The Phase-2b σ-emission code path constructs the chained-IBE onion the same way as the existing OBFT-family onion-build helper.

### Witness-threshold equivalent for Phase-2b σ emission

**E7: At Phase-2b time, byz emits a σ partial on V at L_0 without ever broadcasting Phase-1 V to the cluster. Honest receivers see byz's σ partial on the wire.**

- In bare OBFT, this is the "fake plaintext σ at L_0" attack (Rule 5).
- In Variant C, the σ-eligibility quorum check at Phase-2a end is the witness mechanism. Byz cannot inflate `verdict_pool[V]` alone (byz contributes ≤ f verdicts; qV − f = f+1 honest must agree to reach qV). Byz cannot get honest to σ on V without honest having V locally and the cluster reaching σ-eligibility quorum.
- Byz's Phase-2b σ on V is wasted (counts in σ-pool but cluster σ-quorum requires qV ≥ 2f+1; byz alone is < qV). At most byz's σ inflates the σ-pool by 1.
- **Result**: byz's lone σ-emission has no liveness effect at f=1 n=4 (cluster either reaches qV with honest cooperation or doesn't reach at all). Same outcome as OBFT. No new grief surface.

### Phase-2a verdict envelope rate-limit

**E8: Byzantine spams 100 distinct verdicts per (slot, layer) — different `value_root` each time.**

- Each verdict envelope is op-identity-signed. Honest receivers count *first-observed* per `(slot, layer, operator_id)`; subsequent verdicts are dropped from convergence input but recorded for slashing.
- Per-receiver memory: bounded by gossipsub message-id deduplication and the per-(slot, layer) retention cap.
- **Rate-limit (anti-amplification rule)**: same as OBFT's Rule 5 rate-limit ([docs/OBFT.md / Slashing evidence](OBFT.md#slashing-evidence)). Each honest receiver gossips slashable verdict-equivocation evidence at most once per `(slot, layer, operator_id)` tuple. Caps amplification.

### Phase-2a verdict propagation at high latency

**E9: Verdict from operator i broadcast at Phase-2a start propagates slowly to operator j due to gossipsub mesh anomaly. At Phase-2a end, j has not received i's verdict.**

- j's local convergence input is missing i's verdict. j may compute σ-eligibility-quorum incorrectly (e.g., j sees `verdict_pool[V] = qV − 1` and goes NR, while a cluster-wide-aware operator would see `qV` and go σ).
- **Effect at f=1 n=4**: rare; gossipsub propagates verdicts in `1 BTT` ≤ Δ_2a − 1 BTT. When mesh is anomalously bad, j's NR-emission joins NR-pool. If the rest of the cluster σ-quorums on V (qV partials reach), slot succeeds at L_0 without j. If not, fall-through to L_1.
- **Mitigation**: same as OBFT's mesh-flakiness mitigation — `Δ_2a ≥ 2 BTT` recommended; mesh-diversity at deployment level.

### EKM atomicity at Phase-2b

**E10: Operator i decides at Phase-2a end to σ on V at layer k. The Phase-2b sign request goes to EKM. Concurrent Phase-2b sign request for layer k' (different layer) from same operator. EKM must serialize or transactionally process both.**

- Cross-keypair atomicity: the Phase-2b onion contains per-layer commits, each requiring a separate EKM sign-and-log per (slot, layer). The implementation's commit batch should serialize EKM ops or use a single transaction spanning all layers.
- **Failure mode**: if EKM logs (slot, layer_0, "σ", V) but crashes before logging (slot, layer_1, "NR", null), restart could result in EKM allowing layer_1 σ on a different V (since no log row exists for layer_1). Not a safety violation per-layer (Pigeonhole 1 still holds at layer_0), but operator's local state is inconsistent.
- **Implementation**: EKM operations within a single Phase-2b emission must be atomic — either all per-layer log rows are written or none. Standard transactional database semantics apply. OBFT's EKM coordination model ([docs/OBFT.md / EKM coordination model](OBFT.md#ekm-coordination-model)) calls this out; it's unchanged here.

### Phase-2b σ at deeper layer arrives before its decryption is unlockable

**E11: Honest operator emits chained-IBE Phase-2b σ at layer k > 0. Receivers observe the encrypted partial but cannot decrypt until prior NR-quorums aggregate.**

- Receivers must retain the encrypted partial until decryption is possible. Retention bounds: `O(K · n)` partials per slot — bounded.
- If NR-quorums at prior layers never aggregate (e.g., σ-quorum reached at L_0 short-circuiting the walk), the encrypted partial stays sealed. Receivers eventually drop it when the slot's retention window ends.
- Same as OBFT's Phase-2 onion behavior at deeper layers. No new edge case.

### Byzantine leader emits no Phase-1 bundle but emits Phase-2a verdict σV

**E12: Byzantine leader (= L_0) is silent in Phase-1 but in Phase-2a verdict-claims σV(V) for some V they signed via auth envelope but never broadcast.**

- No honest operator has retained V (byz never broadcast Phase-1 bundle). `verdict_pool[V] = 1 (byz)` from byz's verdict alone.
- Honest operators either have V_local (no — byz didn't broadcast) or don't (yes). Honest verdict NR.
- `verdict_pool[V] = 1 < qV`; `nr_pool = 3 (honest) + 0 (byz) = 3 ≥ qEnc`. NR-quorum reached at L_0 → fall-through. ✓
- Byz's lone σV verdict is wasted; cluster proceeds to L_1.
- **No grief surface**.

### Multi-leader equivocation across layers

**E13: Byzantine controls multiple layer-leaders (e.g., byz holds L_2 and L_3 at K=4). Byz equivocates at both L_2 and L_3.**

- Each layer's equivocation is independent. The cluster falls through L_0, L_1 normally. At L_2, byz equivocates → all honest go NR (per σ-eligibility-quorum-short rule on the split V's) → fall-through to L_3. At L_3, byz equivocates again → all honest go NR → no L_4 → slot misses. Class A.
- **Rare**: byz controls f operators; pigeonhole at K = n means byz holds f / n layers (e.g., 1/4 at f=1 n=4). At K=4 f=1, byz holds exactly 1 layer; at K=4 f=2 (n=7), byz could hold 2 layers but K=4 < n=7 means not all members are leaders.
- **Mitigation**: K ≥ n − f (i.e., enough layers that byz can't hold all of them at the deepest end). At K=4 f=1 n=4, K = n − f + 1 (one more than minimum), pigeonhole guarantees at least one honest leader. ✓

### Operator restart mid-slot

**E14: Operator crashes after Phase-2a verdict broadcast but before Phase-2b. After restart, operator's EKM log shows no σ/NR row at (slot, layer). Operator must decide Phase-2b commit fresh.**

- The operator's Phase-2a verdict envelope is already on the wire — counted in cluster's verdict pool.
- On restart, operator re-evaluates convergence rule based on currently observed Phase-2a verdicts (including their own first-observed verdict, if they retain it locally). If their verdict was σV(V) and cluster reached σ-eligibility-quorum, they σ-emit at Phase-2b.
- If operator does NOT retain their pre-crash verdict locally and re-evaluates from scratch (no V_local because retention was lost), they may default to NR. This is verdict-vs-action mismatch (their on-wire σV verdict ≠ NR action). Honest exception applies (operator state recovery is permissible; the σ-pool may still reach qV from other operators).
- **Implementation guidance**: persist Phase-2a verdict and retained V state across restarts. EKM log alone is insufficient (it doesn't include verdicts). A separate per-slot state log (verdicts, retentions) should survive restarts.
- **Failure mode**: if operator state is fully ephemeral (in-memory only) and the operator restarts mid-slot, they may verdict-vs-action mismatch and trigger Rule-6 false-positives. Mitigation: persist Phase-2a state.

### Adversarial verdict timing — split convergence

**E15: Byzantine times their verdict broadcast such that some honest first-observe byz_σV(V) and some honest first-observe byz_σV(V'). Honest converge differently.**

- Per-peer convergence diverges. Some honest σ-emit on V; some on V'. Pigeonhole 2 ensures only one V can reach qV cluster-wide: the V with more honest converging. Slot succeeds on whichever V has σ-pool ≥ qV.
- Worst case: 50/50 split. At f=1 n=4 with 1 byz + 3 honest, byz issues 2 distinct σV verdicts (one to A, one to B; C either). At first-observed counting: A counts byz on V; B counts byz on V'; C counts byz on whichever first arrives. The 3 honest verdict σV on the V their host validates (presumably the V they retained from Phase-1 — bundle propagation should be consistent across honest by Phase-2a end if `Δ_2a ≥ 1 BTT`).
  - If all 3 honest have the *same* V_local (say V): all 3 verdict σV(V). `verdict_pool[V] = 3 + maybe-byz = 3 or 4 ≥ qV`. All 3 honest σ-emit on V. σ-pool = 3 ≥ qV. ✓ Slot succeeds.
  - If 2 honest have V and 1 has V' (e.g., partial equivocation propagation), divergence. Not an "adversarial verdict timing" issue per se but bundle propagation issue.
- The verdict-equivocation-by-byz-alone is bounded by f: byz contributes ≤ 1 distinct verdict per pool per peer (first-observed), so byz's adversarial contribution to per-peer convergence is at most 1 σ-pool entry inflation.

### Receiver-side validity stabilization

**E16: SSV proposer parent_root validity changes between Phase-1 acceptance and Phase-2a verdict time (re-org during the receiver acceptance window).**

- OBFT's host workflow ([docs/OBFT.md / Head-change handling](OBFT.md#head-change-handling)): validate against stable head snapshot at Phase-1 acceptance, lock the verdict.
- In Variant C, the verdict is broadcast at Phase-2a (not at Phase-1). The host must decide the verdict at Phase-2a verdict-broadcast time.
- **Two options**:
  1. **Host locks verdict at Phase-1 acceptance** (OBFT style). Phase-2a verdict echoes the locked verdict. Validity-divergence behavior same as OBFT: re-org during Phase-1 acceptance window splits operators; `verdict_pool[V]` short of qV → NR fall-through. ✓
  2. **Host re-evaluates at Phase-2a verdict-broadcast time** (Variant C "stabilization"). Each operator validates against their current head at verdict time. If the re-org has propagated to all honest by Phase-2a verdict time, all operators evaluate against the same head → unanimous verdict. ✓
- **Recommended**: option 2 — re-evaluate at Phase-2a verdict time. Phase-2a's window IS the stabilization window. This is what Variant C optimizes for.
- **Implementation**: the host's `validate(V_{L_k})` callback is called at Phase-2a verdict-broadcast time, with the operator's current head. If host wants to be conservative, they may keep a hybrid (lock at Phase-1 acceptance but re-evaluate at Phase-2a if head moved significantly).

### Phase-2b late σ when bundle re-floods very close to Phase-2a end

**E17: Bundle re-floods to operator A at T_commit + Δ_2a − ε for tiny ε. A barely has time to verdict σV(V) and broadcast.**

- A's verdict broadcast time: `T_commit + Δ_2a − ε`. Propagation to peers: peer first-observes by `T_commit + Δ_2a − ε + 1 BTT`.
- Peer's Phase-2a end: `T_commit + Δ_2a`. Peer first-observes A's verdict at `T_commit + Δ_2a + 1 BTT − ε` — past Phase-2a end.
- A's verdict missed Phase-2a's effective deadline; peer doesn't include A in `verdict_pool[V]` for convergence.
- A's vote is wasted; cluster computes convergence without A. If `verdict_pool[V]` still reaches qV without A, slot succeeds. If not (A was the marginal vote), σ-eligibility short → all NR → fall-through.
- **Mitigation**: don't broadcast verdict past `T_commit + Δ_2a − 1 BTT`. This is the effective Phase-2a verdict-broadcast cutoff.

### Asymmetric verdict observation at Phase-2a end

**E18: Operator i observes `verdict_pool[V] = qV − 1` (one short). Operator j observes `verdict_pool[V] = qV` (one more).**

- i computes σ-eligibility short → NR.
- j computes σ-eligibility met → σ on V (if j has V_local).
- Cluster split at convergence: i NR-emits, j σ-emits. σ-pool actual depends on how many honest converge each way.
- This is a realization of E9 (high-latency verdict propagation). At realistic Δ_2a sizing, this should be rare. If it happens at f=1 n=4 with leader as the marginal voter (j), σ-pool = 1 (j) + maybe-byz, NR-pool = 2 (i + others) + maybe-byz. Outcomes:
  - σ-pool < qV; NR-pool reaches qEnc → fall-through. ✓
- The asymmetric-observation case generally reduces to NR-quorum fall-through. Slot recovers via L_1.

## Open questions / decisions to make at implementation time

1. **Δ_2a vs Δ_2b sizing**: minimum vs recommended? At Config A minimum (Δ_2a = Δ_2b = 1 BTT = 200ms), submission headroom is wider; at recommended (2 BTT = 400ms) the headroom narrows by ~400ms but mesh-jitter absorption widens. Recommend recommended for mesh-flakiness mitigation; revisit if production telemetry shows submission tail eats the headroom.

2. **Verdict equivocation rate-limit**: should honest receivers gossip slashable verdict-equivocation evidence on first detection or wait for cluster confirmation? OBFT Rule 5 uses first-observed gossip with a per-(slot, layer, operator_id) cap; same rule fits here.

3. **Verdict envelope size**: 32-byte `value_root` plus envelope overhead ≈ 100-200 bytes per verdict per layer. Per slot: K verdicts × n operators × 200 bytes ≈ 3.2 KB at K=4 n=4. Within budget.

4. **Persistent Phase-2a state across restarts**: to avoid Rule-6 false-positives on operator restart, persist verdict + retained V at the EKM-log level. Implementation choice: extend the EKM log schema to include verdict envelopes alongside σ/NR rows? Or use a separate per-slot state file (recommended — keeps EKM minimal).

5. **Convergence-rule tie-break at n > 3f+1**: when multiple V's could reach `qV` (only possible at non-tight BFT-bound clusters like n=5 f=1), use lexicographic `value_root` tie-break. At n=3f+1 exactly (the SSV cluster sizes), tie-break is moot. Document for completeness.

6. **Late-bundle Phase-2a verdict path**: should an operator who first-observes V via re-flood at, say, T_commit + Δ_2a/2 still broadcast σV verdict? Yes, if propagation slack permits (broadcast ≤ T_commit + Δ_2a − 1 BTT). Implementation: per-operator timer that fires at the latest-safe verdict-broadcast time.

7. **Rule 6 evidence handling**: how do receivers determine whether a verdict-vs-action mismatch is honest revision (allowed) vs byzantine equivocation (slashable)? Implementation rule: receiver collects mismatch evidence; honest receivers cross-reference with their cluster verdict view; weakly slashable ("behavioral pattern" quality, like OBFT's selective-delivery). Surfacing this evidence requires the manual-blacklist coordination from OBFT's rational-byzantine-deterrent model (planned protocol extension) — not automated.

8. **Should leader broadcast a verdict for their own V?** Yes — leader is an operator like any other in Phase-2a. Their verdict on their own V is σV (typically; could flip to NV on host re-evaluation if state shifted, e.g., re-org). Leader's verdict counts in `verdict_pool[V]`.

9. **What if leader's verdict on their own V is NV?** This is the validity-divergence-with-leader-on-NV-side case. Handled by the convergence rule: leader's verdict NR/NV puts them in `nr_pool`. NR-pool may reach qEnc → fall-through.

10. **Phase-2b emission timing — start-of-window or based on convergence completion?** Operators compute convergence at Phase-2a end (T_commit + Δ_2a) and emit Phase-2b immediately. No need to delay further within Phase-2b; the window is for propagation.

11. **K = 4 vs K = 3 trade-off**: same as OBFT base. K=4 (= n) has maximum fall-through depth at +3 KB onion bandwidth; K=3 (= f+2) saves bandwidth but is less robust to multi-layer adversarial scenarios. Recommend K = n = 4 for SSV proposer.

12. **Hash variant?** Variant C does not need V-plaintext in the Phase-2 onion since Phase-2a's bundle re-flood is the V-recovery mechanism. There is no late-σ-emit-on-V-recovered-from-peer-onion in Variant C. So the hash-vs-full-V distinction does not apply; Phase-2b onions carry σ partials only (encrypted at deeper layers), not V plaintext.

13. **Migration / co-existence with QBFT**: rollout via per-cluster opt-in (DKG event) or feature flag. Wire-protocol versioning via `protocol_tag` (`OBFT-2ab-v1`) prevents cross-protocol message mixing. Operationally: ship behind feature flag, enable per cluster after DKG.

14. **DKG cost**: same as OBFT — one V-keypair DKG (already in SSV) + one IBE-keypair DKG (new, run once at cluster init). Per-cluster setup, not per-slot.

15. **Package layout**: 2abOBFT can be implemented as its own `protocol/v2/obft/` package (or extension of an existing OBFT-family package once one lands). Either approach works; preserving test infrastructure and IBE plumbing reuse argues for extension if a parallel OBFT package already exists.

16. **Verdict EKM-binding (open trade-off)**: should Phase-2a verdicts be logged in the EKM at issue time, with Phase-2b sign requests required to match? Closes the 2-1-byz-defect regression but adds complexity (verdicts become EKM-tracked events; honest revision upon equivocation needs a "verdict-void" EKM operation gated on equivocation evidence). Default recommendation: accept the regression in v1; revisit if production telemetry shows defection-grief at meaningful rates. If adopted, the EKM coordinator gains: `(slot, layer, verdict_side, value_root)` log row at Phase-2a issue + `LogPhase2bSign` checks against the verdict row + `VoidVerdict(equivocation_evidence)` for honest revision.

17. **Verdict-issue timing minimum**: should there be a *minimum* verdict-broadcast time (e.g., `T_commit + Δ_2a/2`) to prevent premature commits? At the boundary case where an honest operator broadcasts verdict immediately on Phase-1-acceptance success and a byzantine equivocation arrives mid-Phase-2a, the honest operator's verdict is on the wire as σV but their commit revises to NR (honest exception under Rule 6). A minimum-broadcast-time would force operators to wait long enough to observe most re-flooded equivocation evidence first. Trade-off: forces all operators to broadcast verdicts in a narrow window near Phase-2a end, potentially adding propagation pressure. Default recommendation: no minimum (broadcast at latest-safe time, which is the natural choice anyway).

## Implementation plan — high-level breakdown

The implementation is broken into phases that can be staged across PRs:

### Phase 1 — Wire format and EKM schema

- Add `KindVerdict` envelope to [protocol/v2/tbft/wire/](../protocol/v2/tbft/wire/).
- Update `Phase1Bundle` schema to remove `σ_V` partial; auth envelope retains `protocol_tag = "OBFT-2ab-v1"`.
- Extend EKM schema in [ssvsigner/ekm/](../ssvsigner/ekm/) to support `(slot, layer, side, value_root)` log rows for the V-share + IBE-share coordinator. Add per-Phase-2b sign-request handlers.
- Add domain-separation tests confirming `2abOBFT-v1` envelopes don't validate under bare OBFT or OBFTR envelope handlers.

### Phase 2 — Instance state machine

- New `Phase2abInstance` in [protocol/v2/tbft/](../protocol/v2/tbft/) (extending or alongside existing `Instance`) with state machine: Phase-1-receive → Phase-2a-verdict-emit + receive → Phase-2b-commit → Phase-3-reconstruct.
- `ObserveCandidate(layer, V)` — same as existing.
- `ObserveVerdict(verdict)` — new; record per-(slot, layer, operator) first-observed verdict.
- `ObserveOnion(onion)` — restructured for Phase-2b commit shape.
- `ObserveNonReceipt(nr)` — same as existing, but emitted at Phase-2b end.
- `Resolve()` — same K-layer reconstruction walk.

### Phase 3 — Convergence rule and Phase-2b emission

- Implement convergence rule per the table in "Convergence rule" section above.
- `BuildPhase2bOnion(operatorID)` — at Phase-2a end, compute commits per layer, sign σ partials (chained IBE at k>0) or NR partials, wrap in auth envelope.
- EKM integration: per-(slot, layer) sign-request before each per-layer partial.

### Phase 4 — Adapter integration

- Wire the proposer-duty runner to drive the Phase 2a/2b state machine.
- Add Phase-2a verdict broadcast at `T_commit + Δ_2a − 1 BTT`.
- Add Phase-2b emission at `T_commit + Δ_2a`.

### Phase 5 — Slashing-evidence rule 6

- Detect verdict-vs-action mismatches in Phase-3 reconstruction.
- Honest-revision exception: cross-reference cluster verdict view; slashable iff cluster's verdict pool would have honestly converged on the verdict side.
- Surface evidence via existing slashing-evidence gossip mechanism.

### Phase 6 — Testing and rollout

- Adversarial-byzantine tests covering the Class B recovery cases (equivocation σ-locked split, h_V=1 selective-delivery, validity-divergence at n=4 f=1).
- Mesh-flakiness simulation tests.
- Integration tests with simulated Δ_2a propagation latency.
- Feature flag rollout: disabled by default, opt-in per cluster.

## Comparison summary

| Aspect | Bare OBFT | OBFT + Phase 2a/2b (this) |
|---|---|---|
| Healthy-path latency (all 4 ops cooperate, all receive V) | ~600ms | ~700ms (+100ms for Phase-2a/2b split) |
| Marginal h_V=2 + byz σ-cooperates | Succeeds at L_0 (σ-pool = 3) | Succeeds at L_0 (verdict-quorum reached, σ-pool = 3) |
| Marginal h_V=2 + byz silent / NR | Slot misses (σ-pool = 2 < qV; NR-pool = 1 or 2 < qEnc; no fall-through) | Falls through to L_1 (verdict-quorum short → all honest NR → NR-pool = 3-4 ≥ qEnc) ✓ |
| Validity-divergence (e.g., 2-σ vs 2-NV at f=1 n=4) | Class A (slot-miss; leader σ_V locked on stale V) | Recovered (NR-quorum fall-through; leader's verdict re-evaluated at Phase-2a) |
| Equivocation 1-1-1 split | Class B (slashable, slot-miss; honest σ-locked on different V's) | Recovered (verdict-quorum-eligibility-short → fall-through) |
| Equivocation 2-1 split, byz σ-cooperates | Succeeds at L_0 (Phase-1 σ_V locked + Phase-2 σ) | Succeeds at L_0 (tie) |
| Equivocation 2-1 split, byz silent | Succeeds at L_0 (Phase-1 σ_V on the wire alone is enough) | Falls through to L_1 (one extra layer's latency, still succeeds) |
| Equivocation 2-1 split, byz defects (verdict σV + action NR) | Succeeds at L_0 (Phase-1 σ_V cryptographically locked; can't defect) | **Slot misses (regression)** — Rule-6 evidence on the wire |
| h_V=1 selective-delivery deadlock | Class B grief (slot-miss); not recovered in-protocol — the new Phase-2.5 σ-flip / NR-flip preserves safety but does not close h_V=1 at f=1, n=4. Deterred via Assumption 4 across slots. | Recovered (verdict-quorum-short → fall-through) |
| Late deepest-layer leader broadcast | Class A | Recovered (Phase-2a re-flood absorbs) |
| Mesh-flakiness coordinated with byz σ-refusal | Class B (slashable, slot-miss) | Mitigated (Phase-2a window absorbs jitter) |
| EKM complexity | Per-(slot, layer, side) coordinator with cross-keypair atomicity | Same shape; one fewer concern (no Phase-1 σ_V to coordinate with Phase-2 σ) |
| Wire format | Phase1Bundle, KindCommit, KindCertificate | + KindVerdict, KindOnion2b, KindNR2b (Phase-2b commit splits back into σ-side / NR-side because Phase-2a observation must complete before σ commitment) |
| Slashing-evidence rules | 5 | 6 (Rule 6: verdict-vs-action equivocation, weakly slashable) |
| Submission headroom (Config A) | 2.0s | 1.3s |
| Bandwidth (healthy, n=4, K=4) | Small `V` (attestations ~100 B): ~28 KB (includes `bundle_witnesses` re-flood ≈ +1.5 KB at K=4 n=4); blinded-block `V` (proposer): per-op ~88 KB typical / ~228 KB worst case — see [OBFT.md §Application](OBFT.md#application-ssv-ethereum-proposer-duty) | Small `V`: ~30 KB (no σ_L^V witness — 2abOBFT has no Phase-1 σ_L^V; +3 KB for verdicts vs OBFT baseline before witness); blinded-block `V`: 2abOBFT does not re-flood full bundles |

## What 2abOBFT does NOT close

- **Sustained partition** beyond `Δ_2a + 1 BTT` absorption window — still Class A. Multi-round (R ≥ 2) extension of Phase 2a/2b is a future direction.
- **More than f operators offline/byzantine** — Class A by trust-bound assumption.
- **Backup-leader cascade failure** at K < n − f — Class A. K = n recommended.
- **Honest software bugs producing byzantine-equivalent behavior** — same trust posture as OBFT / QBFT (honest-majority cryptographic, not 100% cryptographic).
- **2-1 equivocation byz-defect grief** — strictly worse than bare OBFT (regression). Byz with 1 vote at f=1 n=4 equivocates V/V', delivers V to 2 honest and V' to 1, verdict-claims σV(V), then withholds σ at Phase-2b (NR-emit or silent). Slot misses either way; Rule-6 cryptographic under NR-emit, behavioral only under silent. Bare OBFT would have succeeded via Phase-1 σ_V lock. The rational-byzantine deterrent absorbs this across many slots; per-slot, an adversarial byz ignoring the deterrent griefs more reliably than under bare OBFT.

  **Mitigation options at implementation time** (not in current spec; see "Open questions" #16):
  - Make verdict envelope EKM-binding: log verdict at Phase-2a issue time; reject Phase-2b sign request that doesn't match. Adds EKM complexity (verdicts as logged events) AND breaks honest revision when equivocation is observed mid-Phase-2a (operator cannot switch from σV-verdict to NR-action). To restore honest revision, EKM needs a "verdict-void" operation gated on auth-valid equivocation evidence.
  - Accept the regression: rely on rational-byzantine deterrent across slots. Recommended unless production telemetry shows byzantine 2-1 defection at meaningful rates.

## Where this came from (variant C rationale)

Variant C — dropping Phase-1 σ_V — is what lets 2abOBFT recover validity-divergence at all `n`/`f`. The verdict-broadcast mechanism is the load-bearing addition: it makes cluster-wide convergence on σ-eligibility observable before any operator commits a partial, which is the structural fix for the Class A validity-divergence and most Class B byzantine-grief patterns (1-1-1 splits, h_V=1 selective-delivery) that single-Phase-2 designs leave open. The 2-1-byz-defect pattern is the residual exception — see [§What 2abOBFT does NOT close](#what-2abobft-does-not-close) — caused by removing Phase-1 σ_L^V (which would have closed it via cryptographic lock) to enable the verdict-driven convergence in the first place. Without verdict broadcasts, a Phase-2a window would only give more time for Phase-1 bundle propagation — equivalent to a wider `Δ_2` in single-Phase-2 designs.

The Phase 2 split costs +1 RTT of slot budget. At Config A this is +200-400ms depending on sizing; at the recommended `Δ_2a = Δ_2b = 2 BTT`, it is +400ms. Submission headroom drops by ~400ms vs single-Phase-2 designs — still a comfortable margin.

The trade-off vs bare OBFT: a healthy-h_V=2 case falls through to L_1 (rather than succeeding at L_0 via the Phase-1 σ_V head-start). At K = n = 4, fall-through is one local-decryption iteration in Phase 3 — no extra RTT, slot still succeeds.

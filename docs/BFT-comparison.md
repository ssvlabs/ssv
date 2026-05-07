# QBFT vs OBFT vs OBFTR vs 2abOBFT — comparison for SSV proposer duty

This doc compares SSV's existing consensus protocol (QBFT) against the three Onion BFT family proposals — [OBFT](OBFT.md) (single-round), [OBFTR](OBFTR.md) (multi-round, R≥2), and [2abOBFT](2abOBFT.md) (Phase 2a/2b witness-bound) — across the SSV proposer-duty operating envelope. The application is held fixed: 12s Ethereum slot, 4s relay submission cutoff. Numbers reflect time-to-signed-output (full BLS signature on the agreed value, ready for downstream submission).

The comparison is structured along three axes:

- **BFT start time within the slot**: 0s (immediate, BFT begins at slot start), 400ms (moderate pre-fetch), 2.5s (late MEV fetch).
- **Worst-case P2P propagation latency D**: 200ms, 600ms, 1000ms (P99 one-way mesh propagation).
- **Mode**: success modes (healthy path: when does the protocol complete?) and failure modes (recovery paths when round-1 / single-round fails, and adversarial-byz exposure).

3 × 3 × 2 = 18 comparison cells, presented across three tables: success-mode completion, failure-recovery completion, and structural failure-mode recoverability (the last is scenario-independent — it depends on protocol structure, not on D or start time).

## Scope and assumptions

- **Cluster**: `n = 4`, `f = 1`, `K = 4` (the SSV proposer-duty default; algebra generalizes to higher `n` at the f-bound).
- **Clock skew δ = 50ms**, treated as negligible relative to D in time accounting.
- **Time unit `BTT` (broadcast trip time)** = `P99 + δ` — one one-way broadcast trip under partial-synchrony assumptions. `P99` is the propagation budget at the deployment's chosen tail percentile (P99, P999, P9999 etc. — deployment knob). Used throughout for time-budget formulas. Concrete: at P99=200ms, δ=50ms, `1 BTT = 250ms` (treated as ≈ 200ms = 1×P99 in tabulation, since δ is dropped per above).
- **Relay submission tail**: ~250ms reserved for relay round-trip after consensus completes. Effective BFT budget = 4s − BFT_start − 250ms.
- **QBFT round timeout RT = 2s** (current SSV production setting). Held fixed across D; tightening would scale RT with D but raises false-positive round-changes under jitter (a known trade-off the team has tuned).
- **No specific block-fetch cost**: BFT_start corresponds to the moment Phase 1 broadcast (or QBFT PROPOSE) begins. Pre-fetch and pre-consensus sit in `[slot_start, BFT_start]`.
- **"Miss"**: cluster fails to produce a validator signature on the proposed block before the relay cutoff. Slot lost; no safety violation in any of the four protocols (safety is cryptographic / honest-majority).
- **"Apples-to-apples"**: all four protocols run within the same 4s budget at the same `n = 4, f = 1`. The scenario axes (start time, D) are the comparison dimensions.

## Protocol summary

- **QBFT** (current SSV): 3-BTT consensus (PROPOSE → PREPARE → COMMIT) + 1-BTT post-consensus partial-sig collection = **4 BTT minimum**. R-round retry on round timeout. Recovery via round-change with new leader + potentially fresh V.
- **OBFT**: K-layer onion with chained encryption, single Phase 2 with one `KindCommit` emission per operator at `T_commit` carrying both σ and NR partials (no Defer state, no sub-phasing). Per-layer staggered broadcast deadlines `T_broadcast_max_k`, with deeper layers having wider propagation budgets `B_k`. **3 BTT** (Phase 1 + Phase 2 + Phase 3). Recovery via in-round K-layer parallel fall-through (sequential local decryption in Phase 3, no extra BTT per layer).
- **OBFTR** (R≥2): same K-layer onion as OBFT, plus R-round retry with re-flood, per-round independent commitments (no cross-round σ-or-NR exclusivity), L_C cluster-consensus signaling. **3 BTT per round**, total `3R BTT`. Recovers partition tails up to `R · P99` via re-flood across rounds — succeeds at L_0 specifically (preserves MEV freshness) at the cost of an extra round.
- **2abOBFT**: K-layer onion with Phase 2a (verdict broadcast) + Phase 2b (σ-or-NR commit driven by convergence rule on Phase-2a verdict pool). **4 BTT** (Phase 1 + 2a + 2b + 3). Single-round only.

## Total time to signed output (in BTT units)

Numbers below assume **recommended Δ sizing** consistently across all four protocols — each per-emission propagation cycle is sized at `2 BTT` for jitter absorption (one full extra propagation cycle of slack on top of P99). Per-protocol totals:

- **OBFT**: `1 BTT` broadcast slack [staggered model — `B_0 = 1 BTT` for the primary L_0; deeper layers' broadcasts pre-arrived; see [OBFT.md §Setting](OBFT.md)] + `2 BTT` Phase 2 + 0 Phase 3 = **3 BTT**.
- **OBFTR per round**: `2 BTT` broadcast slack [uniform model] + `2 BTT` Phase 2 + `1 BTT` Phase 2.5 (L_C signaling) + 0 Phase 3 = **5 BTT**. Round 2 omits fresh broadcast slack (uses re-flood ≈ `1 BTT`) → **4 BTT**.
- **2abOBFT**: `2 BTT` broadcast slack + `2 BTT` Phase 2a + `2 BTT` Phase 2b + 0 Phase 3 = **6 BTT**.
- **QBFT**: 4 phase emissions (PROPOSE + PREPARE + COMMIT + post-consensus), each sized at `2 BTT` for jitter absorption = **8 BTT**.

| Protocol | Round 1 healthy | Round 2 (recovery) | Total at R-round failure |
|---|---|---|---|
| QBFT | 8 BTT | RT (2s timeout) + 8 BTT | 16 BTT + 2s |
| OBFT | 3 BTT | n/a (single-round) | n/a (slot misses if R1 fails on adversarial pattern; K-layer fall-through is in-round, free) |
| OBFTR(R=2) | 5 BTT | 4 BTT | 9 BTT |
| 2abOBFT | 6 BTT | n/a (single-round) | n/a (K-layer fall-through is in-round, free) |

K-layer fall-through (OBFT, OBFTR, 2abOBFT) is sequential local decryption in Phase 3 — no per-layer BTT cost. It recovers silent/late leaders within the same round.

**Why OBFT < OBFTR per-round.** The staggered per-layer broadcast model in bare OBFT (see [OBFT.md §Setting](OBFT.md)) lets the primary L_0 broadcast at `T_commit − 1 BTT` (1 BTT slack) instead of OBFTR's uniform `T_commit_r − 2 BTT` (2 BTT slack). Plus OBFT has no Phase 2.5 (L_C signaling) since it has no rounds to coordinate. Net: OBFT saves 2 BTT vs OBFTR per round at recommended sizing.

**Note on QBFT sizing.** QBFT in SSV production uses round timeout `RT = 2s` and lets phases happen at organic gossipsub speed within `RT` (no explicit per-phase Δ_phase recommendation). The 8 BTT above applies the same "2× per emission cycle for jitter absorption" convention used for the OBFT family. Without that convention (i.e., minimum 1 BTT per phase), QBFT would be 4 BTT minimum — but that under-budgets jitter absorption relative to how OBFT/OBFTR/2abOBFT are sized below. Concretely: at P99=600ms, 8 BTT = 4.8s exceeds SSV's `RT = 2s`, so QBFT under recommended sizing doesn't actually fit at high P99. That mismatch is real — QBFT has weaker jitter absorption than the OBFT family at the same D if we hold the recommended-sizing convention constant.

## Effective BFT consensus budget by start time

| BFT start time | Budget |
|---|---|
| 0s (immediate) | 3.75s |
| 400ms (moderate) | 3.35s |
| 2.5s (late) | 1.25s |

## Table 1 — Success modes (healthy-path completion)

Healthy completion = leader honest, all Phase-1 bundles propagate, σ-quorum reaches at L_0 in round 1.

| BFT start, D | Budget | QBFT (8 BTT) | OBFT (3 BTT) | OBFTR R1 (5 BTT) | 2abOBFT (6 BTT) |
|---|---|---|---|---|---|
| **0s, P99=200ms** | 3.75s | 1.6s ✓ | 0.6s ✓ | 1.0s ✓ | 1.2s ✓ |
| **0s, P99=600ms** | 3.75s | **4.8s ✗** | 1.8s ✓ | 3.0s ✓ | 3.6s ✓ tight |
| **0s, P99=1000ms** | 3.75s | **8.0s ✗** | 3.0s ✓ | **5.0s ✗** | **6.0s ✗** |
| **400ms, P99=200ms** | 3.35s | 1.6s ✓ | 0.6s ✓ | 1.0s ✓ | 1.2s ✓ |
| **400ms, P99=600ms** | 3.35s | **4.8s ✗** | 1.8s ✓ | 3.0s ✓ tight | **3.6s ✗** |
| **400ms, P99=1000ms** | 3.35s | **8.0s ✗** | 3.0s ✓ tight | **5.0s ✗** | **6.0s ✗** |
| **2.5s, P99=200ms** | 1.25s | **1.6s ✗** | 0.6s ✓ | 1.0s ✓ tight | 1.2s ✓ very tight |
| **2.5s, P99=600ms** | 1.25s | **4.8s ✗** | **1.8s ✗** | **3.0s ✗** | **3.6s ✗** |
| **2.5s, P99=1000ms** | 1.25s | **8.0s ✗** | **3.0s ✗** | **5.0s ✗** | **6.0s ✗** |

**Reading Table 1:**

- **P99=200ms** (production-typical for healthy mesh): all four protocols complete healthy at 0s and 400ms BFT start. At 2.5s start, QBFT (1.6s) misses the 1.25s budget while the OBFT family fits.
- **P99=600ms** (degraded mesh): QBFT misses everywhere (8 BTT = 4.8s exceeds even the 0s budget of 3.75s — under recommended sizing, QBFT can't absorb jitter at this P99). OBFT and OBFTR R1 fit 0s/400ms; 2abOBFT fits 0s tightly but misses 400ms.
- **P99=1000ms** (severely degraded): only OBFT (3 BTT, smallest BTT count due to staggered model) fits at 0s and 400ms start. Everything else misses everywhere.
- **OBFT < OBFTR R1 in BTT count.** The staggered per-layer broadcast model in OBFT (see [OBFT.md §Setting](OBFT.md)) gives the primary L_0 a tighter 1 BTT propagation budget. OBFTR uses uniform 2 BTT broadcast slack across all leaders + Phase 2.5 (L_C signaling). Net: OBFTR R1 is 2 BTT longer than bare OBFT at recommended sizing.
- **OBFT < OBFTR R1 < 2abOBFT < QBFT** is the healthy-path ordering at recommended sizing. QBFT's 8 BTT (4 phases × 2 BTT each for jitter absorption) is the slowest under this convention; in practice SSV QBFT relies on the 2s round timeout absorbing variance rather than per-phase 2× sizing, so production QBFT runs faster than 8 BTT suggests at low P99 — but the comparison here holds the convention constant.

## Table 2 — Failure-recovery modes

When round-1 / single-round fails (silent leader, partition, network jitter, but NOT adversarial-byz-locked patterns covered in Table 3), each protocol's recovery path consumes additional time:

| BFT start, D | QBFT R2 (RT + 8 BTT) | OBFT K-layer fall-through | OBFTR R1+R2 (9 BTT) | 2abOBFT K-layer fall-through |
|---|---|---|---|---|
| **0s, P99=200ms** | 3.6s ✓ tight | in-round (free) | 1.8s ✓ | in-round (free) |
| **0s, P99=600ms** | **6.8s ✗** | in-round (free) | **5.4s ✗** | in-round (free) |
| **0s, P99=1000ms** | **10.0s ✗** | in-round (free) | **9.0s ✗** | in-round (free) |
| **400ms, P99=200ms** | **3.6s ✗** (250ms over) | in-round (free) | 1.8s ✓ | in-round (free) |
| **400ms, P99=600ms** | **6.8s ✗** | n/a (R1 missed) | **5.4s ✗** | in-round (free) |
| **400ms, P99=1000ms** | **10.0s ✗** | n/a (R1 missed) | **9.0s ✗** | n/a (R1 missed) |
| **2.5s, P99=200ms** | **3.6s ✗** | in-round (free) | **1.8s ✗** | in-round (free) |
| **2.5s, P99=600ms** | **6.8s ✗** | n/a | **5.4s ✗** | n/a |
| **2.5s, P99=1000ms** | **10.0s ✗** | n/a | **9.0s ✗** | n/a |

"In-round (free)" means the recovery happens within the same single round — no additional time cost. K-layer fall-through is sequential local decryption in Phase 3, processing-bound (~100ms ε_3), not BTT-bound.

**Reading Table 2:**

- **OBFT and 2abOBFT have the cleanest network-failure recovery profile** at any start time where their healthy path fits — silent leader / partition recovery costs zero extra time via in-round K-layer fall-through. This is the structural advantage of K-layer onion with chained encryption: every honest leader in the K-layer rotation provides a fall-through opportunity within Phase 3.
- **OBFTR's R1+R2 retry** at recommended sizing fits only at P99=200ms (1.8s vs 3.75s budget at 0s; 1.8s vs 3.35s at 400ms). At P99=600ms or above, R1+R2 doesn't fit any start time (5.4s+ exceeds 3.75s budget). **OBFTR R1+R2 fitness is narrow under recommended sizing** — production deployments of OBFTR(R=2) need to either tighten Δ_2 toward minimum (sacrificing jitter absorption) or accept that round-2 recovery only works at low-D.
- **QBFT's round-2 retry** at recommended sizing costs RT (2s) + 8 BTT = 3.6s+ — fits tightly at (0s, P99=200ms) but misses everywhere else. At (400ms, P99=200ms) it's 250ms over the 3.35s budget. **Round-2 QBFT under recommended sizing barely fits production** — a real shift from the minimum-sizing analysis where round-2 QBFT fit at multiple scenarios.
- **OBFT and 2abOBFT cannot retry** (single-round). Their "recovery" is the in-round fall-through; if that doesn't reach σ-quorum (e.g., adversarial pattern locks the σ-or-NR pools — see Table 3), the slot misses.
- The **structural QBFT-only recovery** (round-2 with fresh-V refetch) is unavailable in adversarial conditions at any reasonable D under recommended sizing — the 2s round timeout + 8 BTT phase budget exceeds 4s budget at all but the (0s, P99=200ms) edge case.

## Table 3 — Adversarial-byz failure mode recoverability (scenario-independent)

These failure modes depend on protocol *structure*, not on D or start time. They apply where the protocol's healthy path would otherwise fit (i.e., the cells in Table 1 marked ✓).

| Failure mode | QBFT | OBFT | OBFTR | 2abOBFT |
|---|---|---|---|---|
| **σ-locked equivocation 1-1-1** (byz delivers V, V', V'' to three honest at L_0) | ✓ via R2 fresh V | ✗ slot miss | ✗ slot miss (R-invariant) | ✓ convergence rule → fall-through |
| **h_V=1 selective-delivery deadlock** | ✓ via R2 | ✗ | ✗ (R-invariant) | ✓ convergence rule |
| **Validity-divergence 3-of-4 majority** (head-change splits honest verdicts) | ✓ via R2 at moved head | ✗ | ✗ | ✓ convergence rule |
| **Validity-divergence 2-2 split** | ✓ if head moves R1→R2 | ✗ | ✗ | ✗ (algebraic limit) |
| **2-1-byz-defect equivocation** (byz delivers V/V' + verdict-claims σV + Phase-2 NR-defects) | ✓ via R2 | ✓ via Phase-1 σ_V crypto-lock | ✓ via Phase-1 σ_V (R-invariant) | ✗ regression (Variant C trade) |
| **Verdict-equivocation under marginal h_V** | n/a (no verdict surface) | n/a | n/a | ✗ regression |
| **Mesh-flakiness with byz σ-refusal** | ✓ via R2 round-reset | ✗ slot miss (cross-phase exclusivity) | ✗ (R-invariant) | ✓ Phase-2a observation absorbs |
| **Multi-leader silent (K-1 = 3 silent in K=4)** | ✗ multiple round-changes exceed budget | ✓ in-round K-layer fall-through | ✓ in-round | ✓ in-round |
| **Sustained partition > absorption window** | ✗ | ✗ | ✗ (extends to R·D, then misses) | ✗ |
| **> f operators offline / byz** | ✗ | ✗ | ✗ | ✗ |

**Reading Table 3:**

- **QBFT recovers more adversarial-byz patterns** than the OBFT family in single-round comparison, but only when round-2 fits the budget. At typical SSV proposer duty (400ms start, healthy mesh): QBFT R2 doesn't fit (3.6s vs 3.35s budget = 250ms over); the apparent QBFT-recovery advantage is conditional on (BFT_start, D) leaving budget for round 2.
- **OBFT family inherits QBFT's structural disadvantage** at multi-leader-silent (K-1 ≥ 3) patterns: QBFT requires K serial round-changes (each ~RT + 8 BTT), exceeding the 4s budget at any K-1 ≥ 3 in production sizing. OBFT/OBFTR/2abOBFT recover within a single round via K-layer fall-through (free in Phase 3).
- **2abOBFT's convergence-rule recoveries** (1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness) close the patterns that bare OBFT/OBFTR leave as Class B exposures, at the cost of two narrower regressions (2-1-byz-defect, verdict-equivocation) — both slashable, both R-invariant.
- **Bare OBFT and OBFTR succeed at 2-1-byz-defect** that 2abOBFT misses, because the leader's Phase-1 σ_V crypto-locks the σ-pool against post-σ defection. 2abOBFT removes Phase-1 σ_V (Variant C) to gain validity-divergence recovery; pays the regression as the structural cost.

## Cross-scenario takeaways

**Healthy-path latency at production-typical D (200ms)**: OBFT fastest (600ms), OBFTR-R1 1.0s, 2abOBFT 1.2s, QBFT 1.6s. All fit at 0s and 400ms BFT start within the 3.35-3.75s budget; only QBFT misses at 2.5s start (1.6s exceeds 1.25s budget).

**Late-fetch tolerance (BFT start = 2.5s, budget = 1.25s)**: OBFT comfortably fits at P99=200ms (600ms in 1250ms budget); OBFTR R1 at 1.0s is tight; 2abOBFT at 1.2s is very tight (50ms margin); QBFT at 1.6s misses. At D ≥ 600ms, all four miss — late-fetch is incompatible with degraded mesh.

**Degraded-mesh tolerance (P99 = 1000ms)**: only OBFT (3 BTT, smallest BTT count due to staggered model) fits at 0s and 400ms BFT start. OBFTR R1 (5 BTT), 2abOBFT (6 BTT), QBFT (8 BTT) all miss everywhere because their consensus time meets or exceeds the 3.35-3.75s budget.

**Mid-D tolerance (P99 = 600ms)**: bare OBFT (1.8s) fits comfortably; OBFTR R1 (3.0s) fits tightly at 400ms start; 2abOBFT (3.6s) fits at 0s tightly but misses at 400ms; QBFT (4.8s) misses everywhere — under recommended sizing it can't absorb jitter at this P99.

**Round-2 retry usefulness**: **OBFTR's R1+R2 (9 BTT recommended sizing) fits only at P99=200ms** at any reasonable BFT_start; at D≥600ms it doesn't fit any start time. **QBFT's R2 (RT + 8 BTT = 3.6s+) fits only at (0s, P99=200ms) tightly**; everywhere else it misses. Above those, retry budget is unavailable. The OBFT family's K-layer in-round fall-through is structurally cheaper than either retry mechanism — it doesn't consume additional BTT and recovers silent leaders for free.

**Adversarial-byz exposure ranking** (most-recovered to least-recovered):

1. **2abOBFT** + budget for round-2 in QBFT-style scenarios (i.e., extending 2abOBFT to multi-round, hypothetical): closes most patterns including 2-1-byz-defect.
2. **Bare 2abOBFT**: closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness via convergence rule. Misses 2-1-byz-defect and verdict-equivocation.
3. **QBFT (with R2 budget)**: closes adversarial-byz via fresh-V refetch on round-change. Bound by RT + 8 BTT fitting within budget — under recommended sizing this is rarely available.
4. **Bare OBFT, bare OBFTR**: closes silent-leader / partition cases via K-layer fall-through. Adversarial patterns (1-1-1, h_V=1, etc.) are R-invariant slot-misses; rational-byzantine deterrent absorbs across slots (manual blacklist by surviving operators restores `Byzantine ≡ Down`; planned protocol extension).

**Multi-leader-silent advantage**: OBFT family (OBFT, OBFTR, 2abOBFT) all complete at K-1 ≥ 3 silent within a single round via Phase-3 reconstruction walk. QBFT cannot — serial round-changes at RT=2s exceed the 4s budget at K-1 ≥ 3. This is a structural OBFT-family advantage at any (BFT_start, D) combination where the healthy path fits.

**Choosing a protocol** (deployment guidance):

- **Healthy-path latency-critical**: OBFT or OBFTR-R1 at P99=200ms (600ms completion vs QBFT's 800ms).
- **Late-fetch / high-MEV proposer duty (BFT start ≥ 2s)**: OBFT or OBFTR-R1 — only protocols comfortably fitting at 2.5s start with healthy mesh.
- **Adversarial-byz robustness within single round**: 2abOBFT — closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness without round-2 budget cost.
- **Multi-round partition tail absorption**: OBFTR(R=2) — extends absorption to ~R·D ~600-1200ms beyond OBFT's window, when the budget admits.
- **QBFT (current SSV)**: production-mature, recovers more adversarial-byz patterns than bare OBFT/OBFTR when round-2 budget is available. Pays high latency cost (~8 BTT healthy under recommended sizing; ~4 BTT minimum) and degrades sharply at late BFT-start or high P99.

## OBFT + L_Bid mini-consensus extension

OBFT + L_Bid (specified in [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)) is an opportunistic bid-routing extension to bare OBFT. It prepends a bid-determined L_Bid layer above OBFT's K rotation-determined layers (yielding `K' = K + 1`) and adds a mini-consensus phase between Phase 1 and Phase 2 that resolves L_Bid identity cluster-wide before σ-commitment. This section identifies scenarios where OBFT+L_Bid's behavior differs from bare OBFT and from the other three protocols. **Most scenarios are identical between bare OBFT and OBFT+L_Bid**; the differences are surfaced below.

### Differences vs bare OBFT (summary)

- **+2 BTT healthy-path latency**: OBFT+L_Bid is **5 BTT** (1 BTT broadcast slack + 2 BTT mini-consensus + 2 BTT Phase 2 + 0 Phase 3) vs bare OBFT's 3 BTT, at recommended Δ sizing. The mini-consensus phase contributes 2 BTT (recommended `Δ_minicon = 2 BTT` matching `Δ_2`'s widening for jitter absorption — see [OBFT.md Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)).
- **Value capture upside**: highest-bid block on the healthy path (when L_Bid σ-quorum reaches) instead of rotation-determined V.
- **New failure modes at L_Bid**: 2-1-byz-defect and verdict-equivocation (slashable Rules 7-8 in OBFT.md Appendix B; slot-miss-without-fall-through to L_0).
- **L_0..L_{K-1} rotation layers are unchanged**: when the mini-consensus fails (C1/C2 patterns) the cluster falls through to L_0 with the same recovery profile as bare OBFT.

### Where OBFT+L_Bid's outcome differs from bare OBFT

#### Success-mode delta — Table 1

Two scenarios show different success outcomes between bare OBFT and OBFT+L_Bid (at recommended Δ sizing). In all other (BFT_start, D) combinations, both protocols complete healthy or both miss healthy. The full-protocol comparison at the differing scenarios:

| Scenario | Budget | QBFT | Bare OBFT | **OBFT+L_Bid** | OBFTR R1 | 2abOBFT |
|---|---|---|---|---|---|---|
| **0s, P99=1000ms** | 3.75s | 8.0s ✗ | 3.0s ✓ | **5.0s ✗** | 5.0s ✗ | 6.0s ✗ |
| **400ms, P99=1000ms** | 3.35s | 8.0s ✗ | 3.0s ✓ tight | **5.0s ✗** | 5.0s ✗ | 6.0s ✗ |

OBFT+L_Bid loses bare OBFT's healthy-path advantage at these severely degraded scenarios — its 5-BTT structure (vs bare OBFT's 3-BTT) joins the protocols that miss budget. At D ≤ 600ms with BFT_start ≤ 400ms, OBFT and OBFT+L_Bid fit equally.

#### Failure-recovery delta — Table 2

**No latency difference.** Both bare OBFT and OBFT+L_Bid recover via in-round K-layer / K'-layer fall-through (sequential local decryption in Phase 3, no per-layer BTT cost). OBFT+L_Bid's K' = K + 1 adds one extra layer at the top, giving an additional "first-try" recovery opportunity at no extra time. Recovery profile across all scenarios is identical between bare OBFT and OBFT+L_Bid.

#### Adversarial-byz failure modes specific to L_Bid — Table 3 delta

These failure modes don't apply to bare OBFT (no L_Bid layer):

| Failure mode | Bare OBFT | OBFT+L_Bid |
|---|---|---|
| **C1 — Selective bid-withholding at L_Bid** | n/a | ✓ closed by mini-consensus convergence rule → fall-through to L_0 |
| **C2 — Bidder equivocation at L_Bid** | n/a | ✓ closed by convergence rule → fall-through to L_0 |
| **C3 — V_LBid validity-divergence majority (3-of-4)** | n/a | ✓ closed by convergence rule |
| **2-1-byz-defect at L_Bid** | n/a | **✗ slot miss** (slashable Rule 8; deadlock at L_Bid blocks fall-through to L_0) |
| **Verdict-equivocation at L_Bid** | n/a | **✗ slot miss** (slashable Rule 8) |
| **2-2 validity split at L_Bid** | n/a | **✗ algebraic limit** |
| L_0..L_{K-1} rotation-layer failures | (per Table 3) | **Same as bare OBFT** |

In the context of L_Bid integration across the OBFT family — applicable when comparing OBFT+L_Bid against [OBFTR + L_Bid](OBFTR.md#appendix-b--l_bid-mini-consensus-extension) and [2abOBFT + L_Bid](2abOBFT.md#appendix-b--l_bid-mini-consensus-extension):

| L_Bid failure mode | OBFT+L_Bid | OBFTR+L_Bid | 2abOBFT+L_Bid |
|---|---|---|---|
| C1/C2/C3 deadlocks | ✓ closed | ✓ closed | ✓ closed |
| 2-1-byz-defect at L_Bid | ✗ slot miss | ✗ slot miss (R-invariant) | ✗ regression |
| Verdict-equivocation at L_Bid | ✗ slot miss | ✗ slot miss | ✗ regression |
| 2-2 validity split at L_Bid | ✗ algebraic limit | ✗ algebraic limit | ✗ algebraic limit |
| Multi-leader silent (across L_Bid + rotation) | ✓ in-round K'-layer fall-through | ✓ in-round | ✓ in-round |

The L_Bid-specific failure modes are structurally identical across the three protocol families — convergence-rule recoveries close C1/C2/C3, residuals (2-1-byz-defect, verdict-equivocation) match across all three.

### Adversarial-byz trigger frequency

Bare OBFT's L_0 adversarial-byz patterns (σ-locked equivocation, h_V=1, etc.) trigger only when byz is the rotation L_0 leader — typically 1/n slots at uniform rotation (25% of byz-controlled slots at f=1 n=4). OBFT+L_Bid's L_Bid surfaces (2-1-byz-defect, verdict-equivocation) can trigger any slot where byz is a bidder, which is **every slot** under SSV's all-operators-bid model (assuming the relay's signing cadence permits multi-query equivocation). The L_Bid extension increases adversarial-byz trigger frequency at the bid layer roughly n× compared to bare OBFT's rotation-only L_0 surfaces.

### Net trade vs bare OBFT

OBFT+L_Bid pays:
- **+2 BTT healthy-path latency** (loses bare OBFT's advantage at (0s, 1000ms) and (400ms, 1000ms) scenarios where bare OBFT fits and OBFT+L_Bid doesn't; all other scenarios are unaffected at the budget-fit level).
- **+adversarial-byz exposure at L_Bid** (2-1-byz-defect, verdict-equivocation; slashable but slot-miss without fall-through; higher trigger frequency than rotation-only patterns).
- **+structural complexity** (new wire kinds `KindBid` / `KindBidVerdict`, two new slashing rules, mini-consensus protocol step).

In exchange for:
- **Bid-routing value capture** on healthy path (highest-bid block vs rotation-determined V).
- **C1/C2/C3 deadlock closure at L_Bid** (vs the naive bid-routing sketch which leaves these open).

The trade is favorable when MEV bid-routing value-capture upside exceeds the combined cost of (a) the new failure modes' slot-loss rate and (b) the +2 BTT latency cost. For low-MEV slots or deployments with significant mesh degradation pushing scenarios toward the (0s, 1000ms) or (400ms, 1000ms) borderline, bare OBFT is the better choice.

## Limits of this comparison

- **Numbers are BTT-count approximations** (3 BTT, 4 BTT, etc.). Production has long tails; ε_3 (~100ms local processing) is treated as small relative to D in tabulation. Real implementations may add 50-200ms of constant overhead per round.
- **QBFT round timeout RT = 2s** is held fixed; tightening RT shrinks recovery time but raises false-positive round-changes under jitter.
- **K = n = 4** assumed. At larger n with the same f-bound, K-layer fall-through depth scales (more redundancy at the OBFT family). QBFT's recovery cost scales linearly with K serial round-changes.
- **Bandwidth**: not tabulated here. Order of magnitude: QBFT ~14 KB healthy; OBFT ~27 KB; OBFTR ~30-40 KB; 2abOBFT ~30 KB; all 4 +3-5 KB if L_Bid mini-consensus extension is used (see [each doc's Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)).
- **Pre-consensus / block-fetch overhead** is excluded — sits in `[slot_start, BFT_start]` and is ~equal across protocols.
- **Partial network partitions** (some operators have a quorum view, others don't) aren't separately modeled. All four protocols degrade to slot-miss for the partitioned operators; cluster-wide outcome depends on which side has 2f+1 honest.
- **Adversarial-byz trigger frequency** is not modeled. Practical impact depends on byz-leader rotation distribution and bid-equivocation surface for L_Bid extensions (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) for L_Bid-specific exposure analysis).

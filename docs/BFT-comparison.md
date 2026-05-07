# QBFT vs OBFT vs OBFTR vs 2abOBFT — comparison for SSV proposer duty

This doc compares SSV's existing consensus protocol (QBFT) against the three Onion BFT family proposals — [OBFT](OBFT.md) (single-round), [OBFTR](OBFTR.md) (multi-round, R≥2), and [2abOBFT](2abOBFT.md) (Phase 2a/2b witness-bound) — across the SSV proposer-duty operating envelope. The application is held fixed: 12s Ethereum slot, 4s relay submission cutoff. Numbers reflect time-to-signed-output (full BLS signature on the agreed value, ready for downstream submission).

The comparison is structured along three axes:

- **BFT start time within the slot**: 0s (immediate, BFT begins at slot start), 400ms (moderate pre-fetch), 2.5s (late MEV fetch).
- **`BTT` operating point** (broadcast trip time, `BTT = P99 + δ`): 200ms (production-typical healthy mesh), 600ms (degraded), 1000ms (severely degraded). See [Scope and assumptions](#scope-and-assumptions) for the full BTT definition.
- **Mode**: success modes (healthy path: when does the protocol complete?) and failure modes (recovery paths when round-1 / single-round fails, and adversarial-byz exposure).

3 × 3 × 2 = 18 comparison cells, presented across three tables: success-mode completion, failure-recovery completion, and structural failure-mode recoverability (the last is scenario-independent — it depends on protocol structure, not on BTT or start time).

## Scope and assumptions

- **Cluster**: `n = 4`, `f = 1`, `K = 4` (the SSV proposer-duty default; algebra generalizes to higher `n` at the f-bound).
- **Clock skew δ = 50ms**, included in `BTT` (see below).
- **Time unit `BTT` (broadcast trip time)** = `P99 + δ` — one one-way broadcast trip under partial-synchrony assumptions. `P99` is the propagation budget at the deployment's chosen tail percentile (P99, P999, P9999, etc. — deployment knob). Operating points used in tables below: `BTT = 200ms` (P99 ≈ 150ms + δ ≈ 50ms; production-typical), `BTT = 600ms` (P99 ≈ 550ms + δ; degraded), `BTT = 1000ms` (P99 ≈ 950ms + δ; severely degraded). Tables and prose key on `BTT` end-to-end.
- **Relay submission tail**: ~250ms reserved for relay round-trip after consensus completes. Effective BFT budget = 4s − BFT_start − 250ms.
- **QBFT round timeout RT = 2s** (current SSV production setting). Held fixed across BTT; tightening would scale RT with BTT but raises false-positive round-changes under jitter (a known trade-off the team has tuned).
- **No specific block-fetch cost**: BFT_start corresponds to the moment Phase 1 broadcast (or QBFT PROPOSE) begins. Pre-fetch and pre-consensus sit in `[slot_start, BFT_start]`.
- **"Miss"**: cluster fails to produce a validator signature on the proposed block before the relay cutoff. Slot lost; no safety violation in any of the four protocols (safety is cryptographic / honest-majority).
- **"Apples-to-apples"**: all four protocols run within the same 4s budget at the same `n = 4, f = 1`. The scenario axes (start time, BTT) are the comparison dimensions.

## Protocol summary

- **Partial-sigs on pre-agreed V** (baseline; not a BFT consensus protocol): each operator computes their BLS partial signature on a pre-agreed V and gossips it; threshold aggregation produces the cluster signature. **1 BTT** (single propagation cycle for partial-sig collection + local aggregation). Assumes V is pre-agreed by external mechanism (e.g., beacon-spec deterministic computation for attestations / sync committee duties). **Cannot resolve V-disagreement** (e.g., MEV bundles fetched by different operators differ) — this is what BFT consensus protocols solve. Used here as the floor: what's the cluster's pure cryptographic cost AFTER V is somehow agreed.
- **QBFT** (current SSV): 3-BTT consensus (PROPOSE → PREPARE → COMMIT) + 1-BTT post-consensus partial-sig collection = **4 BTT minimum**. R-round retry on round timeout. Recovery via round-change with new leader + potentially fresh V.
- **OBFT**: K-layer onion with chained encryption, single Phase 2 with one `KindCommit` emission per operator at `T_commit` carrying both σ and NR partials (no Defer state, no sub-phasing). Per-layer staggered broadcast deadlines `T_broadcast_max_k`, with deeper layers having wider propagation budgets `B_k`. **3 BTT** (Phase 1 + Phase 2 + Phase 3). Recovery via in-round K-layer parallel fall-through (sequential local decryption in Phase 3, no extra BTT per layer).
- **OBFTR** (R≥2): same K-layer onion as OBFT, plus R-round retry with re-flood, per-round independent commitments (no cross-round σ-or-NR exclusivity), L_C cluster-consensus signaling. **5 BTT R1 / 4 BTT R2** at recommended sizing (Phase 1 + Phase 2 + Phase 2.5 L_C + Phase 3); total `9 BTT` at R=2. Recovers partition tails up to `R · P99` via re-flood across rounds — succeeds at L_0 specifically (preserves MEV freshness) at the cost of an extra round.
- **2abOBFT**: K-layer onion with Phase 2a (verdict broadcast) + Phase 2b (σ-or-NR commit driven by convergence rule on Phase-2a verdict pool). **4 BTT** (Phase 1 + 2a + 2b + 3). Single-round only.

## Total time to signed output (in BTT units)

Numbers below use **production-aligned sizing**: OBFT/OBFTR/2abOBFT use recommended Δ sizing (2 BTT per per-emission propagation cycle for jitter absorption); QBFT uses minimum sizing (1 BTT per phase, as the `RT = 2s` round timeout absorbs jitter at the round level). See **Note on sizing conventions** below for rationale. Per-protocol totals:

- **Partial-sigs on pre-agreed V**: 1 BTT (partial-sig propagation + threshold aggregation). No consensus rounds.
- **OBFT**: `1 BTT` broadcast slack [staggered model — `B_0 = 1 BTT` for the primary L_0; deeper layers' broadcasts pre-arrived; see [OBFT.md §Setting](OBFT.md)] + `2 BTT` Phase 2 + 0 Phase 3 = **3 BTT**.
- **OBFTR per round**: `2 BTT` broadcast slack [uniform model] + `2 BTT` Phase 2 + `1 BTT` Phase 2.5 (L_C signaling) + 0 Phase 3 = **5 BTT**. Round 2 omits fresh broadcast slack (uses re-flood ≈ `1 BTT`) → **4 BTT**.
- **2abOBFT**: `2 BTT` broadcast slack + `2 BTT` Phase 2a + `2 BTT` Phase 2b + 0 Phase 3 = **6 BTT**.
- **QBFT**: 4 phase emissions (PROPOSE + PREPARE + COMMIT + post-consensus) at minimum sizing (1 BTT each) = **4 BTT**. SSV's production convention: round timeout `RT = 2s` absorbs per-phase jitter at the round level rather than via 2× per-phase widening.

| Protocol | Round 1 healthy | Round 2 (recovery) | Total at R-round failure |
|---|---|---|---|
| Partial-sigs on pre-agreed V (baseline) | 1 BTT | n/a (no rounds) | n/a (no recovery — fails on any V-disagreement) |
| QBFT | 4 BTT | RT (2s timeout) + 4 BTT | 8 BTT + 2s |
| OBFT | 3 BTT | n/a (single-round) | n/a (slot misses if R1 fails on adversarial pattern; K-layer fall-through is in-round, free) |
| OBFTR(R=2) | 5 BTT | 4 BTT | 9 BTT |
| 2abOBFT | 6 BTT | n/a (single-round) | n/a (K-layer fall-through is in-round, free) |

K-layer fall-through (OBFT, OBFTR, 2abOBFT) is sequential local decryption in Phase 3 — no per-layer BTT cost. It recovers silent/late leaders within the same round.

**Why OBFT < OBFTR per-round.** The staggered per-layer broadcast model in bare OBFT (see [OBFT.md §Setting](OBFT.md)) lets the primary L_0 broadcast at `T_commit − 1 BTT` (1 BTT slack) instead of OBFTR's uniform `T_commit_r − 2 BTT` (2 BTT slack). Plus OBFT has no Phase 2.5 (L_C signaling) since it has no rounds to coordinate. Net: OBFT saves 2 BTT vs OBFTR per round at recommended sizing.

**Note on sizing conventions.** QBFT and the OBFT family use different per-phase sizing conventions, each matching how the protocol is actually deployed. QBFT uses **minimum sizing (1 BTT per phase)** because SSV's `RT = 2s` round timeout absorbs jitter at the round level rather than via per-phase widening. The OBFT family uses **recommended sizing** (`Δ_2 = 2 BTT`, etc.) because OBFT/OBFTR/2abOBFT have explicit per-phase deadline coordination rather than round timeouts. Tables below hold these production-aligned conventions consistently.

## Effective BFT consensus budget by start time

| BFT start time | Budget |
|---|---|
| 0s (immediate) | 3.75s |
| 400ms (moderate) | 3.35s |
| 2.5s (late) | 1.25s |

## Table 1 — Success modes (healthy-path completion)

Healthy completion = leader honest, all Phase-1 bundles propagate, σ-quorum reaches at L_0 in round 1.

| BFT start, BTT | Budget | Partial-sigs (1 BTT) † | QBFT (4 BTT) | OBFT (3 BTT) | OBFTR R1 (5 BTT) | 2abOBFT (6 BTT) |
|---|---|---|---|---|---|---|
| **0s, BTT=200ms** | 3.75s | 0.2s ✓ | 0.8s ✓ | 0.6s ✓ | 1.0s ✓ | 1.2s ✓ |
| **0s, BTT=600ms** | 3.75s | 0.6s ✓ | 2.4s ✓ | 1.8s ✓ | 3.0s ✓ | 3.6s ✓ tight |
| **0s, BTT=1000ms** | 3.75s | 1.0s ✓ | **4.0s ✗** | 3.0s ✓ | **5.0s ✗** | **6.0s ✗** |
| **400ms, BTT=200ms** | 3.35s | 0.2s ✓ | 0.8s ✓ | 0.6s ✓ | 1.0s ✓ | 1.2s ✓ |
| **400ms, BTT=600ms** | 3.35s | 0.6s ✓ | 2.4s ✓ | 1.8s ✓ | 3.0s ✓ tight | **3.6s ✗** |
| **400ms, BTT=1000ms** | 3.35s | 1.0s ✓ | **4.0s ✗** | 3.0s ✓ tight | **5.0s ✗** | **6.0s ✗** |
| **2.5s, BTT=200ms** | 1.25s | 0.2s ✓ | 0.8s ✓ | 0.6s ✓ | 1.0s ✓ tight | 1.2s ✓ very tight |
| **2.5s, BTT=600ms** | 1.25s | 0.6s ✓ | **2.4s ✗** | **1.8s ✗** | **3.0s ✗** | **3.6s ✗** |
| **2.5s, BTT=1000ms** | 1.25s | 1.0s ✓ | **4.0s ✗** | **3.0s ✗** | **5.0s ✗** | **6.0s ✗** |

† **Partial-sigs only fits if V is pre-agreed.** For SSV proposer duty (V varies per operator due to MEV bundles), this isn't directly applicable — the cluster needs a BFT consensus protocol to resolve V-disagreement first. Shown here as the floor: what completion would look like if V were pre-agreed (e.g., for non-MEV duties like attestations).

**Reading Table 1:**

- **Partial-sigs floor (V pre-agreed)**: 1 BTT trivially fits every cell. Sets the absolute floor — BFT consensus protocols pay 2-8 BTT extra to resolve V-disagreement.
- **BTT=200ms** (production-typical for healthy mesh): all four BFT protocols complete healthy at every BFT_start — including QBFT at 0.8s comfortably fitting the 1.25s budget at 2.5s start.
- **BTT=600ms** (degraded mesh): QBFT (2.4s) and OBFT (1.8s) fit 0s/400ms comfortably; OBFTR R1 (3.0s) fits 400ms tightly; 2abOBFT (3.6s) fits 0s tightly but misses 400ms. All four miss at 2.5s start.
- **BTT=1000ms** (severely degraded): only OBFT (3 BTT, smallest BTT count due to staggered model) fits at 0s and 400ms start. QBFT (4.0s) misses 0s/400ms by 250–650ms; OBFTR R1 (5.0s) and 2abOBFT (6.0s) miss everywhere.
- **OBFT < OBFTR R1 in BTT count.** The staggered per-layer broadcast model in OBFT (see [OBFT.md §Setting](OBFT.md)) gives the primary L_0 a tighter 1 BTT propagation budget. OBFTR uses uniform 2 BTT broadcast slack across all leaders + Phase 2.5 (L_C signaling). Net: OBFTR R1 is 2 BTT longer than bare OBFT at recommended sizing.
- **Partial-sigs < OBFT < QBFT < OBFTR R1 < 2abOBFT** is the production-aligned healthy-path ordering. Partial-sigs is the floor (no consensus). OBFT's 3-BTT lead over QBFT is structural (staggered per-layer broadcast model + Phase-3 local CPU). OBFTR R1 and 2abOBFT pay extra BTT for cross-round retention (Phase 2.5 / L_C) and Phase 2a/2b convergence, respectively.

## Table 2 — Failure-recovery modes

When round-1 / single-round fails (silent leader, partition, network jitter, but NOT adversarial-byz-locked patterns covered in Table 3), each protocol's recovery path consumes additional time:

| BFT start, BTT | Partial-sigs (no recovery) † | QBFT R2 (RT + 4 BTT) | OBFT K-layer fall-through | OBFTR R1+R2 (9 BTT) | 2abOBFT K-layer fall-through |
|---|---|---|---|---|---|
| **0s, BTT=200ms** | n/a — slot misses on V-disagreement | 2.8s ✓ | in-round (free) | 1.8s ✓ | in-round (free) |
| **0s, BTT=600ms** | n/a | **4.4s ✗** | in-round (free) | **5.4s ✗** | in-round (free) |
| **0s, BTT=1000ms** | n/a | **6.0s ✗** | in-round (free) | **9.0s ✗** | in-round (free) |
| **400ms, BTT=200ms** | n/a | 2.8s ✓ | in-round (free) | 1.8s ✓ | in-round (free) |
| **400ms, BTT=600ms** | n/a | **4.4s ✗** | n/a (R1 missed) | **5.4s ✗** | in-round (free) |
| **400ms, BTT=1000ms** | n/a | **6.0s ✗** | n/a (R1 missed) | **9.0s ✗** | n/a (R1 missed) |
| **2.5s, BTT=200ms** | n/a | **2.8s ✗** | in-round (free) | **1.8s ✗** | in-round (free) |
| **2.5s, BTT=600ms** | n/a | **4.4s ✗** | n/a | **5.4s ✗** | n/a |
| **2.5s, BTT=1000ms** | n/a | **6.0s ✗** | n/a | **9.0s ✗** | n/a |

† **Partial-sigs has no failure-recovery mechanism**: any V-disagreement (operators sign different V's) results in cluster signature aggregation failing — no rounds, no re-flood, no fall-through. The baseline only works on the healthy V-pre-agreed path.

"In-round (free)" means the recovery happens within the same single round — no additional time cost. K-layer fall-through is sequential local decryption in Phase 3, processing-bound (~100ms ε_3), not BTT-bound.

**Reading Table 2:**

- **OBFT and 2abOBFT have the cleanest network-failure recovery profile** at any start time where their healthy path fits — silent leader / partition recovery costs zero extra time via in-round K-layer fall-through. This is the structural advantage of K-layer onion with chained encryption: every honest leader in the K-layer rotation provides a fall-through opportunity within Phase 3.
- **OBFTR's R1+R2 retry** at recommended sizing fits only at BTT=200ms (1.8s vs 3.75s budget at 0s; 1.8s vs 3.35s at 400ms). At BTT=600ms or above, R1+R2 doesn't fit any start time (5.4s+ exceeds 3.75s budget). **OBFTR R1+R2 fitness is narrow under recommended sizing** — production deployments of OBFTR(R=2) need to either tighten Δ_2 toward minimum (sacrificing jitter absorption) or accept that round-2 recovery only works at low BTT.
- **QBFT's round-2 retry** at production minimum sizing costs RT (2s) + 4 BTT = 2.8s at BTT=200ms — fits at (0s, BTT=200ms) and (400ms, BTT=200ms), missing only at 2.5s start (1.25s budget). At BTT≥600ms, R2 = 4.4s+ misses everywhere.
- **OBFT and 2abOBFT cannot retry** (single-round). Their "recovery" is the in-round fall-through; if that doesn't reach σ-quorum (e.g., adversarial pattern locks the σ-or-NR pools — see Table 3), the slot misses.
- The **structural QBFT-only recovery** (round-2 with fresh-V refetch) is available only at BTT=200ms with BFT_start ≤ 400ms — outside this envelope, the 2s round timeout + 4 BTT phase budget exceeds the slot's 4s cutoff. QBFT's adversarial-recovery advantage is narrow in production.

## Table 3 — Adversarial-byz failure mode recoverability (scenario-independent)

These failure modes depend on protocol *structure*, not on BTT or start time. They apply where the protocol's healthy path would otherwise fit (i.e., the cells in Table 1 marked ✓).

| Failure mode | Partial-sigs † | QBFT | OBFT | OBFTR | 2abOBFT |
|---|---|---|---|---|---|
| **σ-locked equivocation 1-1-1** (byz delivers V, V', V'' to three honest at L_0) | n/a (no leader/equivocation surface; V pre-agreed) | ✓ via R2 fresh V | ✗ slot miss | ✗ slot miss (R-invariant) | ✓ convergence rule → fall-through |
| **h_V=1 selective-delivery deadlock** | n/a | ✓ via R2 | ✗ | ✗ (R-invariant) | ✓ convergence rule |
| **Validity-divergence 3-of-4 majority** (head-change splits honest verdicts) | ✗ slot miss (no in-protocol V-disagreement resolution) | ✓ via R2 at moved head | ✗ | ✗ | ✓ convergence rule |
| **Validity-divergence 2-2 split** | ✗ slot miss | ✓ if head moves R1→R2 | ✗ | ✗ | ✗ (algebraic limit) |
| **2-1-byz-defect equivocation** (byz delivers V/V' + verdict-claims σV + Phase-2 NR-defects) | n/a | ✓ via R2 | ✓ via Phase-1 σ_V crypto-lock | ✓ via Phase-1 σ_V (R-invariant) | ✗ regression (Variant C trade) |
| **Verdict-equivocation under marginal h_V** | n/a | n/a (no verdict surface) | n/a | n/a | ✗ regression |
| **Mesh-flakiness with byz σ-refusal** | ✗ if 1 honest's partial doesn't reach + byz withholds, threshold under-quorum | ✓ via R2 round-reset | ✗ slot miss (cross-phase exclusivity) | ✗ (R-invariant) | ✓ Phase-2a observation absorbs |
| **Multi-leader silent (K-1 = 3 silent in K=4)** | n/a (no leader rotation) | ✗ multiple round-changes exceed budget | ✓ in-round K-layer fall-through | ✓ in-round | ✓ in-round |
| **Sustained partition > absorption window** | ✗ | ✗ | ✗ | ✗ (extends to R·BTT, then misses) | ✗ |
| **> f operators offline / byz** | ✗ | ✗ | ✗ | ✗ | ✗ |

† **Partial-sigs assumes V is pre-agreed across all honest** (e.g., via beacon-spec deterministic computation for attestations / sync committee). For SSV proposer duty with MEV, V varies per operator → partial-sigs alone cannot resolve V-disagreement → BFT consensus is required. The "n/a" entries below mark failure modes that are protocol-specific to leader/equivocation surfaces that don't exist in partial-sigs.

**Reading Table 3:**

- **QBFT recovers more adversarial-byz patterns** than the OBFT family in single-round comparison, but only when round-2 fits the budget. At typical SSV proposer duty (400ms start, healthy mesh): QBFT R2 fits at 2.8s vs 3.35s budget (production minimum sizing). At BTT≥600ms or BFT_start=2.5s, the apparent QBFT-recovery advantage disappears — round-2 doesn't fit.
- **OBFT family avoids QBFT's structural disadvantage** at multi-leader-silent (K-1 ≥ 3) patterns: QBFT requires K serial round-changes (each ~RT + 4 BTT), exceeding the 4s budget at any K-1 ≥ 3 even in production minimum sizing. OBFT/OBFTR/2abOBFT recover within a single round via K-layer fall-through (free in Phase 3).
- **2abOBFT's convergence-rule recoveries** (1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness) close the patterns that bare OBFT/OBFTR leave as Class B exposures, at the cost of two narrower regressions (2-1-byz-defect, verdict-equivocation) — both slashable, both R-invariant.
- **Bare OBFT and OBFTR succeed at 2-1-byz-defect** that 2abOBFT misses, because the leader's Phase-1 σ_V crypto-locks the σ-pool against post-σ defection. 2abOBFT removes Phase-1 σ_V (Variant C) to gain validity-divergence recovery; pays the regression as the structural cost.

## Table 4 — MEV-fetch budget by protocol (BTT=200ms)

At the SSV proposer-duty operating point — `BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`, `RANDAO_done ≈ 150ms` (see [OBFT.md §Application](OBFT.md#timing-budget) for the full derivation) — each protocol's leader has a different MEV-relay-fetch budget bounded by when its broadcast must complete. The fetch budget is the wall-clock from `RANDAO_done` to the leader's broadcast deadline.

**OBFT (K=4)** — staggered per-layer broadcast at `T_broadcast_max_k = Ls_arrival − B_k`, with `Ls_arrival = T_commit − slack = 3300ms`:

| Leader | Broadcast time | MEV-fetch budget |
|---|---|---|
| V_0 (primary) | 3200ms | **3050ms** |
| V_1 | 3100ms | 2950ms |
| V_2 | 2900ms | 2750ms |
| V_3 (deepest) | 2300ms | 2150ms |

**QBFT (RT=2s, 2-round target)** — single leader per round; fetch must complete before each round's PROPOSE:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 900ms | 750ms |
| R2 | 3100ms | 2950ms |

**Partial-sigs on pre-agreed V (baseline)** — V agreed externally; consensus = 1 BTT partial-sig propagation + aggregation. Broadcast deadline = `Relay_cutoff − 100ms − 1 BTT = 3700ms`:

| Step | Time | MEV-fetch budget |
|---|---|---|
| V determined (must be cluster-agreed) | ≤ 3700ms | **3550ms** (= 3700 − RANDAO 150ms) |

**Cross-protocol ranking**:

| Rank | Leader | MEV-fetch budget | Notes |
|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3550ms** | Floor: only available if V is pre-agreed (no MEV / no V-disagreement) |
| 2 | OBFT V_0 | **3050ms** | Best BFT-consensus protocol for MEV proposer duty |
| 3 (tie) | OBFT V_1 / QBFT R2 | 2950ms | |
| 5 | OBFT V_2 | 2750ms | |
| 6 | OBFT V_3 | 2150ms | |
| 7 | QBFT R1 | 750ms | |

† **Partial-sigs is not directly comparable** for SSV proposer duty (V varies per operator). Shown as the no-consensus floor — what would be possible if V didn't need cluster-wide agreement.

**Reading:**

- **OBFT V_0 captures 300ms more MEV-fresh fetch time than QBFT R2** (3050 vs 2950ms). All four OBFT leaders beat QBFT R1 by ≥1.4s.
- **OBFT V_0 pays a 500ms BFT-consensus tax over the partial-sigs floor** (3050 vs 3550ms). This 500ms = 2 BTT (Phase 2 + propagation slack) is the structural cost of resolving V-disagreement in a single round at this operating point. QBFT R1's tax is 2800ms (4× larger).
- **Only QBFT R2 reaches V_1 parity**, and only after paying the round-1 timeout gap (`RT_1 = 2000ms` of wall-clock during which QBFT cannot make progress). OBFT's K-layer fall-through is in-round (sequential local IBE decryption, no per-layer RTT), so OBFT lands the same `Relay_cutoff` budget without the round-change penalty.
- **QBFT R1 is structurally constrained** by needing PROPOSE_1 to fire early enough that consensus + post-consensus + R2 retry can fit the slot. At the operating point above, R1's MEV-fetch budget is just 750ms — 4× less than OBFT V_0 and ~5× less than the partial-sigs floor.
- **Deeper-layer fetch budgets (V_2, V_3) trade fetch time for propagation slack**: V_2 covers 400ms tails, V_3 covers 1000ms tails. Healthy-path fetch is V_0's 3050ms; deeper fetches are recovery-only.

**OBFTR(R=2) and 2abOBFT primary-leader fetch budgets** at BTT=200ms (approximate, sized to fit the full R1+R2 budget for OBFTR and the 6-BTT single round for 2abOBFT):

| Protocol | Total BTT | V_0 broadcast time | V_0 MEV-fetch budget |
|---|---|---|---|
| OBFTR(R=2) (R1+R2 fit) | 9 BTT | ~2100ms | ~1950ms |
| OBFTR(R=2) (R1-only) | 5 BTT | ~2900ms | ~2750ms |
| 2abOBFT | 6 BTT | ~2700ms | ~2550ms |

Both pay the V_0-MEV-freshness cost (vs bare OBFT's 3050ms) in exchange for additional structural recovery: 2abOBFT's convergence-rule adversarial-byz recoveries; OBFTR's extended partition tail absorption via cross-round retention.

The **MEV-fetch budget asymmetry is a structural OBFT-family advantage over QBFT** — under the same operating envelope, OBFT V_0 has 4× the MEV-fresh fetch time of QBFT R1 leader, and OBFT structurally avoids the round-change gap that gates QBFT's R2 fetch.

## Cross-scenario takeaways

**Partial-sigs floor (V pre-agreed)**: 1 BTT = 200ms total. Fits trivially at every (BFT_start, BTT) cell. Sets the floor: BFT-consensus protocols pay 2-8 BTT extra to resolve V-disagreement. For SSV proposer duty (V varies per operator due to MEV bundles), partial-sigs alone is not directly applicable — used here as a reference for the BFT-consensus tax.

**Healthy-path latency at production-typical BTT (200ms)**: partial-sigs floor 200ms, OBFT 600ms, QBFT 800ms, OBFTR-R1 1.0s, 2abOBFT 1.2s. All five fit at every BFT_start (including the 1.25s budget at 2.5s start).

**Late-fetch tolerance (BFT start = 2.5s, budget = 1.25s)**: OBFT fits comfortably at BTT=200ms (600ms in 1250ms budget); QBFT (800ms) also fits comfortably; OBFTR R1 at 1.0s is tight; 2abOBFT at 1.2s is very tight (50ms margin). At BTT ≥ 600ms, all four miss — late-fetch is incompatible with degraded mesh.

**Degraded-mesh tolerance (BTT = 1000ms)**: only OBFT (3 BTT, smallest BTT count due to staggered model) fits at 0s and 400ms BFT start. QBFT (4 BTT = 4.0s) misses 0s/400ms by 250–650ms; OBFTR R1 (5 BTT) and 2abOBFT (6 BTT) miss everywhere — their consensus time exceeds the 3.35–3.75s budget.

**Mid-BTT tolerance (BTT = 600ms)**: bare OBFT (1.8s) fits comfortably; QBFT (2.4s) also fits 0s/400ms; OBFTR R1 (3.0s) fits tightly at 400ms; 2abOBFT (3.6s) fits at 0s tightly but misses at 400ms. All four miss at 2.5s start (1.25s budget).

**Round-2 retry usefulness**: **OBFTR's R1+R2 (9 BTT recommended sizing) fits only at BTT=200ms** at any reasonable BFT_start; at BTT≥600ms it doesn't fit any start time. **QBFT's R2 (RT + 4 BTT = 2.8s at BTT=200ms) fits at (0s, BTT=200ms) and (400ms, BTT=200ms)** comfortably; everywhere else it misses. Above those, retry budget is unavailable. The OBFT family's K-layer in-round fall-through is structurally cheaper than either retry mechanism — it doesn't consume additional BTT and recovers silent leaders for free.

**Adversarial-byz exposure ranking** (most-recovered to least-recovered):

1. **2abOBFT** + budget for round-2 in QBFT-style scenarios (i.e., extending 2abOBFT to multi-round, hypothetical): closes most patterns including 2-1-byz-defect.
2. **Bare 2abOBFT**: closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness via convergence rule. Misses 2-1-byz-defect and verdict-equivocation.
3. **QBFT (with R2 budget)**: closes adversarial-byz via fresh-V refetch on round-change. Bound by RT + 4 BTT fitting within budget — at production minimum sizing this is available at (0s, BTT=200ms) and (400ms, BTT=200ms).
4. **Bare OBFT, bare OBFTR**: closes silent-leader / partition cases via K-layer fall-through. Adversarial patterns (1-1-1, h_V=1, etc.) are R-invariant slot-misses; rational-byzantine deterrent absorbs across slots (manual blacklist by surviving operators restores `Byzantine ≡ Down`; planned protocol extension).

**Multi-leader-silent advantage**: OBFT family (OBFT, OBFTR, 2abOBFT) all complete at K-1 ≥ 3 silent within a single round via Phase-3 reconstruction walk. QBFT cannot — serial round-changes at RT=2s exceed the 4s budget at K-1 ≥ 3. This is a structural OBFT-family advantage at any (BFT_start, BTT) combination where the healthy path fits.

**Choosing a protocol** (deployment guidance):

- **Pre-agreed V (no consensus needed)**: partial-sigs floor at 1 BTT = 200ms. Use this for SSV duties where V is deterministic (attestations, sync committee). Not applicable to MEV proposer duty since V varies per operator.
- **Healthy-path latency-critical (with consensus)**: OBFT at BTT=200ms (600ms completion); QBFT 800ms; OBFTR-R1 1000ms; 2abOBFT 1200ms — all four fit every BFT_start at this BTT.
- **Late-fetch / high-MEV proposer duty (BFT start ≥ 2s)**: OBFT (600ms) and QBFT (800ms) fit comfortably at 2.5s start with healthy mesh; OBFTR-R1 (1000ms) is tight; 2abOBFT (1200ms) is very tight.
- **Adversarial-byz robustness within single round**: 2abOBFT — closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness without round-2 budget cost.
- **Multi-round partition tail absorption**: OBFTR(R=2) — extends absorption to ~R·BTT ~600-1200ms beyond OBFT's window, when the budget admits.
- **QBFT (current SSV)**: production-mature, recovers more adversarial-byz patterns than bare OBFT/OBFTR when round-2 budget is available. At production minimum sizing (4 BTT healthy), fits every BFT_start at BTT=200ms; fits BFT_start ≤ 400ms at BTT=600ms; misses at BTT=1000ms.

## OBFT + L_Bid mini-consensus extension

OBFT + L_Bid (specified in [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)) is an opportunistic bid-routing extension to bare OBFT. It prepends a bid-determined L_Bid layer above OBFT's K rotation-determined layers (yielding `K' = K + 1`) and adds a mini-consensus phase between Phase 1 and Phase 2 that resolves L_Bid identity cluster-wide before σ-commitment. This section identifies scenarios where OBFT+L_Bid's behavior differs from bare OBFT and from the other three protocols. **Most scenarios are identical between bare OBFT and OBFT+L_Bid**; the differences are surfaced below.

### Differences vs bare OBFT (summary)

- **+2 BTT healthy-path latency**: OBFT+L_Bid is **5 BTT** (1 BTT broadcast slack + 2 BTT mini-consensus + 2 BTT Phase 2 + 0 Phase 3) vs bare OBFT's 3 BTT, at recommended Δ sizing. The mini-consensus phase contributes 2 BTT (recommended `Δ_minicon = 2 BTT` matching `Δ_2`'s widening for jitter absorption — see [OBFT.md Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)).
- **Value capture upside**: highest-bid block on the healthy path (when L_Bid σ-quorum reaches) instead of rotation-determined V.
- **New failure modes at L_Bid**: 2-1-byz-defect and verdict-equivocation (slashable Rules 7-8 in OBFT.md Appendix B; slot-miss-without-fall-through to L_0).
- **L_0..L_{K-1} rotation layers are unchanged**: when the mini-consensus fails (C1/C2 patterns) the cluster falls through to L_0 with the same recovery profile as bare OBFT.

### Where OBFT+L_Bid's outcome differs from bare OBFT

#### Success-mode delta — Table 1

Two scenarios show different success outcomes between bare OBFT and OBFT+L_Bid (at recommended Δ sizing). In all other (BFT_start, BTT) combinations, both protocols complete healthy or both miss healthy. The full-protocol comparison at the differing scenarios:

| Scenario | Budget | QBFT | Bare OBFT | **OBFT+L_Bid** | OBFTR R1 | 2abOBFT |
|---|---|---|---|---|---|---|
| **0s, BTT=1000ms** | 3.75s | **4.0s ✗** | 3.0s ✓ | **5.0s ✗** | 5.0s ✗ | 6.0s ✗ |
| **400ms, BTT=1000ms** | 3.35s | **4.0s ✗** | 3.0s ✓ tight | **5.0s ✗** | 5.0s ✗ | 6.0s ✗ |

OBFT+L_Bid loses bare OBFT's healthy-path advantage at these severely degraded scenarios — its 5-BTT structure (vs bare OBFT's 3-BTT) joins the protocols that miss budget. At BTT ≤ 600ms with BFT_start ≤ 400ms, OBFT and OBFT+L_Bid fit equally.

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

- **Numbers are BTT-count approximations** (3 BTT, 4 BTT, etc.). Production has long tails; ε_3 (~100ms local processing) is treated as small relative to BTT in tabulation. Real implementations may add 50-200ms of constant overhead per round.
- **QBFT round timeout RT = 2s** is held fixed; tightening RT shrinks recovery time but raises false-positive round-changes under jitter.
- **K = n = 4** assumed. At larger n with the same f-bound, K-layer fall-through depth scales (more redundancy at the OBFT family). QBFT's recovery cost scales linearly with K serial round-changes.
- **Bandwidth**: not tabulated here. Order of magnitude: QBFT ~14 KB healthy; OBFT ~28 KB; OBFTR ~30-40 KB; 2abOBFT ~30 KB; all 4 +3-5 KB if L_Bid mini-consensus extension is used (see [each doc's Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)). OBFT and OBFTR include the σ_L^V witness section (~1.5 KB at K=4 n=4); 2abOBFT does not (no Phase-1 σ_L^V).
- **Pre-consensus / block-fetch overhead** is excluded — sits in `[slot_start, BFT_start]` and is ~equal across protocols.
- **Partial network partitions** (some operators have a quorum view, others don't) aren't separately modeled. All four protocols degrade to slot-miss for the partitioned operators; cluster-wide outcome depends on which side has 2f+1 honest.
- **Adversarial-byz trigger frequency** is not modeled. Practical impact depends on byz-leader rotation distribution and bid-equivocation surface for L_Bid extensions (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) for L_Bid-specific exposure analysis).

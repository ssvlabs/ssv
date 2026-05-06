# QBFT vs OBFT vs OBFTR vs 2abOBFT — comparison for SSV proposer duty

This doc compares SSV's existing consensus protocol (QBFT) against the three Onion BFT family proposals — [OBFT](OBFT.md) (single-round), [OBFTR](OBFTR.md) (multi-round, R≥2), and [2abOBFT](2abOBFT.md) (Phase 2a/2b witness-bound) — across the SSV proposer-duty operating envelope. The application is held fixed: 12s Ethereum slot, 4s relay submission cutoff. Numbers reflect time-to-signed-output (full BLS signature on the agreed value, ready for downstream submission).

The comparison is structured along three axes:

- **BFT start time within the slot**: 0s (immediate, BFT begins at slot start), 1s (moderate pre-fetch), 2.5s (late MEV fetch).
- **Worst-case P2P propagation latency D**: 200ms, 600ms, 1000ms (P99 one-way mesh propagation).
- **Mode**: success modes (healthy path: when does the protocol complete?) and failure modes (recovery paths when round-1 / single-round fails, and adversarial-byz exposure).

3 × 3 × 2 = 18 comparison cells, presented across three tables: success-mode completion, failure-recovery completion, and structural failure-mode recoverability (the last is scenario-independent — it depends on protocol structure, not on D or start time).

## Scope and assumptions

- **Cluster**: `n = 4`, `f = 1`, `K = 4` (the SSV proposer-duty default; algebra generalizes to higher `n` at the f-bound).
- **Clock skew δ = 50ms**, treated as negligible relative to D in time accounting.
- **Relay submission tail**: ~250ms reserved for relay round-trip after consensus completes. Effective BFT budget = 4s − BFT_start − 250ms.
- **QBFT round timeout RT = 2s** (current SSV production setting). Held fixed across D; tightening would scale RT with D but raises false-positive round-changes under jitter (a known trade-off the team has tuned).
- **No specific block-fetch cost**: BFT_start corresponds to the moment Phase 1 broadcast (or QBFT PROPOSE) begins. Pre-fetch and pre-consensus sit in `[slot_start, BFT_start]`.
- **"Miss"**: cluster fails to produce a validator signature on the proposed block before the relay cutoff. Slot lost; no safety violation in any of the four protocols (safety is cryptographic / honest-majority).
- **"Apples-to-apples"**: all four protocols run within the same 4s budget at the same `n = 4, f = 1`. The scenario axes (start time, D) are the comparison dimensions.

## Protocol summary

- **QBFT** (current SSV): 3-RTT consensus (PROPOSE → PREPARE → COMMIT) + 1-RTT post-consensus partial-sig collection = **4 RTTs minimum**. R-round retry on round timeout. Recovery via round-change with new leader + potentially fresh V.
- **OBFT**: K-layer onion with chained encryption, single Phase-2 with sub-phasing (σ-emit during window, NR/Defer at end). **3 RTTs** (Phase 1 + Phase 2 + Phase 3). Recovery via in-round K-layer parallel fall-through (sequential local decryption in Phase 3, no extra RTT per layer).
- **OBFTR** (R≥2): same K-layer onion as OBFT, plus R-round retry, cross-round σ retention, L_C cluster-consensus signaling. **3 RTTs per round**, total `3R RTTs`. Recovers partition tails up to `R · D` via re-flood across rounds.
- **2abOBFT**: K-layer onion with Phase 2a (verdict broadcast) + Phase 2b (σ-or-NR commit driven by convergence rule on Phase-2a verdict pool). **4 RTTs** (Phase 1 + 2a + 2b + 3). Single-round only.

## RTTs to signed output

| Protocol | Round 1 healthy | Round 2 (recovery) | Total at R-round failure |
|---|---|---|---|
| QBFT | 4 RTTs | RT (2s timeout) + 4 RTTs | 8 RTTs + 2s |
| OBFT | 3 RTTs | n/a (single-round) | n/a (slot misses if R1 fails on adversarial pattern; K-layer fall-through is in-round, free) |
| OBFTR(R=2) | 3 RTTs | 3 RTTs | 6 RTTs |
| 2abOBFT | 4 RTTs | n/a (single-round) | n/a (K-layer fall-through is in-round, free) |

K-layer fall-through (OBFT, OBFTR, 2abOBFT) is sequential local decryption in Phase 3 — no per-layer RTT cost. It recovers silent/late leaders within the same round.

## Effective BFT consensus budget by start time

| BFT start time | Budget |
|---|---|
| 0s (immediate) | 3.75s |
| 1s (moderate) | 2.75s |
| 2.5s (late) | 1.25s |

## Table 1 — Success modes (healthy-path completion)

Healthy completion = leader honest, all Phase-1 bundles propagate, σ-quorum reaches at L_0 in round 1.

| BFT start, D | Budget | QBFT (4D) | OBFT (3D) | OBFTR R1 (3D) | 2abOBFT (4D) |
|---|---|---|---|---|---|
| **0s, D=200ms** | 3.75s | 0.8s ✓ | 0.6s ✓ | 0.6s ✓ | 0.8s ✓ |
| **0s, D=600ms** | 3.75s | 2.4s ✓ | 1.8s ✓ | 1.8s ✓ | 2.4s ✓ |
| **0s, D=1000ms** | 3.75s | **4.0s ✗** | 3.0s ✓ | 3.0s ✓ | **4.0s ✗** |
| **1s, D=200ms** | 2.75s | 0.8s ✓ | 0.6s ✓ | 0.6s ✓ | 0.8s ✓ |
| **1s, D=600ms** | 2.75s | 2.4s ✓ | 1.8s ✓ | 1.8s ✓ | 2.4s ✓ |
| **1s, D=1000ms** | 2.75s | **4.0s ✗** | **3.0s ✗** | **3.0s ✗** | **4.0s ✗** |
| **2.5s, D=200ms** | 1.25s | 0.8s ✓ | 0.6s ✓ | 0.6s ✓ | 0.8s ✓ |
| **2.5s, D=600ms** | 1.25s | **2.4s ✗** | **1.8s ✗** | **1.8s ✗** | **2.4s ✗** |
| **2.5s, D=1000ms** | 1.25s | **4.0s ✗** | **3.0s ✗** | **3.0s ✗** | **4.0s ✗** |

**Reading Table 1:**

- **D=200ms** (production-typical for healthy mesh): all four protocols complete healthy at all BFT start times.
- **D=600ms** (degraded mesh): all complete at 0s and 1s start; all miss at 2.5s start (budget 1.25s tighter than even OBFT's 1.8s).
- **D=1000ms** (severely degraded): only the 3-RTT protocols (OBFT, OBFTR R1) fit at 0s start (3.0s within 3.75s budget); QBFT and 2abOBFT (4-RTT each) miss everywhere; nothing fits at 1s or 2.5s.
- **OBFT and OBFTR R1 share identical healthy-path numbers** because OBFTR is OBFT-with-multi-round-retry — round 1 of OBFTR is structurally a single OBFT round.
- **QBFT and 2abOBFT share identical healthy-path numbers** because both are 4-RTT (QBFT: 3 consensus + 1 post; 2abOBFT: Phase 1 + 2a + 2b + 3).

## Table 2 — Failure-recovery modes

When round-1 / single-round fails (silent leader, partition, network jitter, but NOT adversarial-byz-locked patterns covered in Table 3), each protocol's recovery path consumes additional time:

| BFT start, D | QBFT R2 (RT + 4D) | OBFT K-layer fall-through | OBFTR R1+R2 (6D) | 2abOBFT K-layer fall-through |
|---|---|---|---|---|
| **0s, D=200ms** | 2.8s ✓ | in-round (free) | 1.2s ✓ | in-round (free) |
| **0s, D=600ms** | **4.4s ✗** | in-round (free) | 3.6s ✓ tight | in-round (free) |
| **0s, D=1000ms** | **6.0s ✗** | in-round (free) | **6.0s ✗** | in-round (free) |
| **1s, D=200ms** | 2.8s **borderline** (50ms over) | in-round (free) | 1.2s ✓ | in-round (free) |
| **1s, D=600ms** | **4.4s ✗** | in-round (free) | **3.6s ✗** | in-round (free) |
| **1s, D=1000ms** | **6.0s ✗** | n/a (R1 missed) | **6.0s ✗** | n/a (R1 missed) |
| **2.5s, D=200ms** | **2.8s ✗** | in-round (free) | 1.2s ✓ | in-round (free) |
| **2.5s, D=600ms** | **4.4s ✗** | n/a | **3.6s ✗** | n/a |
| **2.5s, D=1000ms** | **6.0s ✗** | n/a | **6.0s ✗** | n/a |

"In-round (free)" means the recovery happens within the same single round — no additional time cost. K-layer fall-through is sequential local decryption in Phase 3, processing-bound (~100ms ε_3), not RTT-bound.

**Reading Table 2:**

- **OBFT and 2abOBFT have the cleanest network-failure recovery profile** at any start time where their healthy path fits — silent leader / partition recovery costs zero extra time via in-round K-layer fall-through. This is the structural advantage of K-layer onion with chained encryption: every honest leader in the K-layer rotation provides a fall-through opportunity within Phase 3.
- **OBFTR's R1+R2 retry** doubles the consensus time, fitting only at the most generous (start, D) combinations. At 0s start with D=600ms, OBFTR R1+R2 just fits the 3.75s budget; 1s/600ms is a miss.
- **QBFT's round-2 retry** costs RT (2s) + 4D, the highest recovery cost of the four protocols. Fits only at 0s start with D=200ms; 1s/200ms is borderline (50ms over); everything else misses on R2.
- **OBFT and 2abOBFT cannot retry** (single-round). Their "recovery" is the in-round fall-through; if that doesn't reach σ-quorum (e.g., adversarial pattern locks the σ-or-NR pools — see Table 3), the slot misses.
- The **structural QBFT-only recovery** (round-2 with fresh-V refetch) costs 4.4-6s in adversarial conditions — beyond budget at typical SSV configurations except at the most generous (0s, D=200ms) end of the envelope.

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

- **QBFT recovers more adversarial-byz patterns** than the OBFT family in single-round comparison, but only when round-2 fits the budget. At typical SSV proposer duty (1s start, healthy mesh): QBFT R2 doesn't fit; the apparent QBFT-recovery advantage is conditional on (BFT_start, D) leaving budget for round 2.
- **OBFT family inherits QBFT's structural disadvantage** at multi-leader-silent (K-1 ≥ 3) patterns: QBFT requires K serial round-changes (each ~RT + 4D), exceeding the 4s budget at any K-1 ≥ 3 in production sizing. OBFT/OBFTR/2abOBFT recover within a single round via K-layer fall-through (free in Phase 3).
- **2abOBFT's convergence-rule recoveries** (1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness) close the patterns that bare OBFT/OBFTR leave as Class B exposures, at the cost of two narrower regressions (2-1-byz-defect, verdict-equivocation) — both slashable, both R-invariant.
- **Bare OBFT and OBFTR succeed at 2-1-byz-defect** that 2abOBFT misses, because the leader's Phase-1 σ_V crypto-locks the σ-pool against post-σ defection. 2abOBFT removes Phase-1 σ_V (Variant C) to gain validity-divergence recovery; pays the regression as the structural cost.

## Cross-scenario takeaways

**Healthy-path latency at production-typical D (200ms)**: OBFT and OBFTR-R1 fastest (600ms), QBFT and 2abOBFT slightly slower (800ms). All fit at any BFT start time within the 4s budget.

**Late-fetch tolerance (BFT start = 2.5s, budget = 1.25s)**: only OBFT and OBFTR can complete healthy at D=200ms with comfortable margin (600ms in 1250ms budget); QBFT and 2abOBFT have ~450ms margin. At D ≥ 600ms, all four miss — late-fetch is incompatible with degraded mesh.

**Degraded-mesh tolerance (D = 1000ms)**: only the 3-RTT protocols (OBFT and OBFTR-R1) complete healthy, and only at 0s BFT start. QBFT and 2abOBFT (4-RTT each) miss even at 0s start because consensus time (4s) exceeds budget (3.75s).

**Round-2 retry usefulness**: QBFT's R2 fits budget only at (0s, 200ms) and borderline at (1s, 200ms). OBFTR's R1+R2 fits at any 0s-start scenario through D=600ms, and at 1s/200ms. Above those, retry budget is unavailable. The OBFT family's K-layer in-round fall-through is structurally cheaper than either retry mechanism — it doesn't consume additional RTTs and recovers silent leaders for free.

**Adversarial-byz exposure ranking** (most-recovered to least-recovered):

1. **2abOBFT** + budget for round-2 in QBFT-style scenarios (i.e., extending 2abOBFT to multi-round, hypothetical): closes most patterns including 2-1-byz-defect.
2. **Bare 2abOBFT**: closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness via convergence rule. Misses 2-1-byz-defect and verdict-equivocation.
3. **QBFT (with R2 budget)**: closes adversarial-byz via fresh-V refetch on round-change. Bound by RT + 4D fitting within budget.
4. **Bare OBFT, bare OBFTR**: closes silent-leader / partition cases via K-layer fall-through. Adversarial patterns (1-1-1, h_V=1, etc.) are R-invariant slot-misses; reputation deterrent absorbs across slots.

**Multi-leader-silent advantage**: OBFT family (OBFT, OBFTR, 2abOBFT) all complete at K-1 ≥ 3 silent within a single round via Phase-3 reconstruction walk. QBFT cannot — serial round-changes at RT=2s exceed the 4s budget at K-1 ≥ 3. This is a structural OBFT-family advantage at any (BFT_start, D) combination where the healthy path fits.

**Choosing a protocol** (deployment guidance):

- **Healthy-path latency-critical**: OBFT or OBFTR-R1 at D=200ms (600ms completion vs QBFT's 800ms).
- **Late-fetch / high-MEV proposer duty (BFT start ≥ 2s)**: OBFT or OBFTR-R1 — only protocols comfortably fitting at 2.5s start with healthy mesh.
- **Adversarial-byz robustness within single round**: 2abOBFT — closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness without round-2 budget cost.
- **Multi-round partition tail absorption**: OBFTR(R=2) — extends absorption to ~R·D ~600-1200ms beyond OBFT's window, when the budget admits.
- **QBFT (current SSV)**: production-mature, recovers more adversarial-byz patterns than bare OBFT/OBFTR when round-2 budget is available. Pays high latency cost (~4D minimum healthy) and degrades sharply at late BFT-start or high D.

## OBFT + L_Bid mini-consensus extension

OBFT + L_Bid (specified in [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)) is an opportunistic bid-routing extension to bare OBFT. It prepends a bid-determined L_Bid layer above OBFT's K rotation-determined layers (yielding `K' = K + 1`) and adds a mini-consensus phase between Phase 1 and Phase 2 that resolves L_Bid identity cluster-wide before σ-commitment. This section identifies scenarios where OBFT+L_Bid's behavior differs from bare OBFT and from the other three protocols. **Most scenarios are identical between bare OBFT and OBFT+L_Bid**; the differences are surfaced below.

### Differences vs bare OBFT (summary)

- **+1 RTT healthy-path latency**: OBFT+L_Bid is **4D** (Phase 1 + mini-consensus + Phase 2 + Phase 3) vs bare OBFT's 3D.
- **Value capture upside**: highest-bid block on the healthy path (when L_Bid σ-quorum reaches) instead of rotation-determined V.
- **New failure modes at L_Bid**: 2-1-byz-defect and verdict-equivocation (slashable Rules 7-8 in OBFT.md Appendix B; slot-miss-without-fall-through to L_0).
- **L_0..L_{K-1} rotation layers are unchanged**: when the mini-consensus fails (C1/C2 patterns) the cluster falls through to L_0 with the same recovery profile as bare OBFT.

### Where OBFT+L_Bid's outcome differs from bare OBFT

#### Success-mode delta — Table 1

Only one scenario shows different success outcomes between bare OBFT and OBFT+L_Bid:

| Scenario | Budget | Bare OBFT (3D) | OBFT+L_Bid (4D) |
|---|---|---|---|
| **0s, D=1000ms** | 3.75s | 3.0s ✓ | **4.0s ✗** (over budget by 250ms) |

In all other (BFT_start, D) combinations, both protocols complete healthy or both miss healthy. The full-protocol comparison at this differing scenario:

| Scenario | QBFT | Bare OBFT | **OBFT+L_Bid** | OBFTR R1 | 2abOBFT |
|---|---|---|---|---|---|
| **0s, D=1000ms** | 4.0s ✗ | 3.0s ✓ | **4.0s ✗** | 3.0s ✓ | 4.0s ✗ |

OBFT+L_Bid loses bare OBFT's healthy-path advantage at this scenario — its 4-RTT structure puts it in the same bucket as QBFT and 2abOBFT, joining the protocols that miss budget under degraded mesh.

#### Failure-recovery delta — Table 2

**No latency difference.** Both bare OBFT and OBFT+L_Bid recover via in-round K-layer / K'-layer fall-through (sequential local decryption in Phase 3, no per-layer RTT cost). OBFT+L_Bid's K' = K + 1 adds one extra layer at the top, giving an additional "first-try" recovery opportunity at no extra time. Recovery profile across all scenarios is identical between bare OBFT and OBFT+L_Bid.

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
- **+1 RTT healthy-path latency** (loses bare OBFT's advantage at the (0s, 1000ms) borderline scenario; all other scenarios are unaffected at the budget-fit level).
- **+adversarial-byz exposure at L_Bid** (2-1-byz-defect, verdict-equivocation; slashable but slot-miss without fall-through; higher trigger frequency than rotation-only patterns).
- **+structural complexity** (new wire kinds `KindBid` / `KindBidVerdict`, two new slashing rules, mini-consensus protocol step).

In exchange for:
- **Bid-routing value capture** on healthy path (highest-bid block vs rotation-determined V).
- **C1/C2/C3 deadlock closure at L_Bid** (vs bare-TBFT-style B.3 sketch which leaves these open).

The trade is favorable when MEV bid-routing value-capture upside exceeds the combined cost of (a) the new failure modes' slot-loss rate and (b) the +1 RTT latency cost. For low-MEV slots or deployments with significant mesh degradation pushing scenarios toward the (0s, 1000ms) borderline, bare OBFT is the better choice.

## Limits of this comparison

- **Numbers are RTT-count approximations** (3D, 4D, etc.). Production has long tails; ε_3 (~100ms local processing) is treated as small relative to D in tabulation. Real implementations may add 50-200ms of constant overhead per round.
- **QBFT round timeout RT = 2s** is held fixed; tightening RT shrinks recovery time but raises false-positive round-changes under jitter.
- **K = n = 4** assumed. At larger n with the same f-bound, K-layer fall-through depth scales (more redundancy at the OBFT family). QBFT's recovery cost scales linearly with K serial round-changes.
- **Bandwidth**: not tabulated here. Order of magnitude: QBFT ~14 KB healthy; OBFT ~27 KB; OBFTR ~30-40 KB; 2abOBFT ~30 KB; all 4 +3-5 KB if L_Bid mini-consensus extension is used (see [each doc's Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)).
- **Pre-consensus / block-fetch overhead** is excluded — sits in `[slot_start, BFT_start]` and is ~equal across protocols.
- **Partial network partitions** (some operators have a quorum view, others don't) aren't separately modeled. All four protocols degrade to slot-miss for the partitioned operators; cluster-wide outcome depends on which side has 2f+1 honest.
- **Adversarial-byz trigger frequency** is not modeled. Practical impact depends on byz-leader rotation distribution and bid-equivocation surface for L_Bid extensions (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) for L_Bid-specific exposure analysis).

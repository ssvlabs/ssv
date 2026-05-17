# QBFT vs OBFT vs OBFTR vs 2abOBFT — comparison for SSV proposer duty

This doc compares SSV's existing consensus protocol (QBFT) against the three Onion BFT family proposals — [OBFT](OBFT.md) (single-round), [OBFTR](OBFTR.md) (multi-round, R≥2), and [2abOBFT](2abOBFT.md) (Phase 2a/2b witness-bound) — across the SSV proposer-duty operating envelope. The application is held fixed: 12000ms Ethereum slot, 4000ms relay submission cutoff. Numbers reflect time-to-signed-output (full BLS signature on the agreed value, ready for downstream submission).

The comparison is structured along three axes:

- **BFT start time within the slot**: 0ms (immediate, BFT begins at slot start), 800ms, 1200ms, 1800ms, 2500ms (late MEV fetch). Spans the practical range — 0ms is non-MEV duty or pre-fetched MEV; 2500ms is late MEV-fetch maximizing relay-bundle freshness.
- **`BTT` operating point** (broadcast trip time, `BTT = P99 + δ`): 200ms (production-typical healthy mesh), 600ms (degraded), 1000ms (severely degraded). See [Scope and assumptions](#scope-and-assumptions) for the full BTT definition.
- **Mode**: success modes (healthy path: when does the protocol complete?) and failure modes (recovery paths when round-1 / single-round fails, and adversarial-byz exposure).

5 × 3 × 2 = 30 comparison cells, presented across three tables: success-mode completion, failure-recovery completion, and structural failure-mode recoverability (the last is scenario-independent — it depends on protocol structure, not on BTT or start time).

## Scope and assumptions

- **Cluster**: `n = 4`, `f = 1`, `K = 4` across protocols for like-for-like comparison (OBFTR's own preferred default is K=3 since R-round retry substitutes for one layer of K-fall-through — see [OBFTR.md §Application](OBFTR.md#application-ssv-ethereum-proposer-duty); algebra generalizes to higher `n` at the f-bound).
- **Clock skew δ = 50ms**, included in `BTT` (see below).
- **Time unit `BTT` (broadcast trip time)** = `P99 + δ` — one one-way broadcast trip under partial-synchrony assumptions. `P99` is the propagation budget at the deployment's chosen tail percentile (P99, P999, P9999, etc. — deployment knob). Operating points used in tables below: `BTT = 200ms` (P99 ≈ 150ms + δ ≈ 50ms; production-typical), `BTT = 600ms` (P99 ≈ 550ms + δ; degraded), `BTT = 1000ms` (P99 ≈ 950ms + δ; severely degraded). Tables and prose key on `BTT` end-to-end.
- **Relay submission tail**: 100ms reserved for cert broadcast + relay submit after consensus completes (matches OBFT.md's `header_submit_headroom` — see [docs/OBFT.md / Operating point](OBFT.md#timing-budget)). Effective BFT budget = 4000ms − BFT_start − 100ms.
- **Per-protocol T_commit wall-clock anchors at Config A** (back-derived from `T_relay_cutoff = 4000ms` minus each protocol's post-T_commit budget — max-MEV anchoring for cross-protocol MEV-fetch comparability): OBFT ≈ 3600ms (post-T_commit = Δ_2 + ε_3 + T_submit + ~50ms jitter ≈ 400ms at tightened `Δ_2 = 1 BTT`), 2abOBFT ≈ 3600ms (post-T_commit = Δ_2b + ε_3 + T_submit ≈ 400ms at tightened `Δ_2b = 1·BTT + ε_proc`; ε_proc is the additional Phase-2b convergence-computation overhead, which substitutes for OBFT's residual jitter slot — both protocols end up at the same wall-clock T_commit at default BTT=200ms), OBFTR(R=2) round-1 `T_commit_1` ≈ 1500ms (R-round budget ≈ 1600ms; OBFTR's Δ_2 sizing unchanged in this sweep). `T_commit` is semantically aligned across the OBFT family (= σ-or-NR commit point in all three); wall-clock values differ per-protocol due to consensus-budget differences. Comparisons in this doc anchor to `T_relay_cutoff` to keep the framing uniform across protocols. (Note: 2abOBFT.md's deployment-timing table uses a more conservative anchor at `T_commit ≈ 2000ms` for production headroom — see [2abOBFT.md §Timing budget](2abOBFT.md#timing-budget--concrete-configurations); both anchors are valid deployment choices, this doc uses max-MEV for like-for-like MEV-fetch framing in Table 4.) Within each protocol's L_Bid extension, `T_commit` stays invariant (bare-vs-+L_Bid only).
- **QBFT round timeout RT = 2000ms** (current SSV production setting). Held fixed across BTT; tightening would scale RT with BTT but raises false-positive round-changes under jitter (a known trade-off the team has tuned).
- **No specific block-fetch cost**: BFT_start corresponds to the moment Phase 1 broadcast (or QBFT PROPOSE) begins. Pre-fetch and pre-consensus sit in `[slot_start, BFT_start]`.
- **"Miss"**: cluster fails to produce a validator signature on the proposed block before the relay cutoff. Slot lost; no safety violation in any of the four protocols (safety is cryptographic / honest-majority).
- **"Apples-to-apples"**: all four protocols run within the same 4000ms budget at the same `n = 4, f = 1`. The scenario axes (start time, BTT) are the comparison dimensions.

## Sizing convention

All BTT counts in this doc use a **uniform "2 BTT per emission cycle" recommended sizing**. The 2 BTT decomposes as **1 BTT P99 propagation** (cluster's typical-mesh propagation cycle) **+ 1 BTT mesh-jitter slack** (absorbs P99 → P9999 tail).

**This is the production-survival sizing for mission-critical roles.** For SSV proposer duty: missing a slot is per-slot economic loss + signal to stakers, so the configuration target is **worst-case completion under recommended sizing**, not best-case under optimal conditions. Best-case completion times (1 BTT per emission, no jitter buffer) are demonstrative — they show how fast the protocol *can* complete when nothing goes wrong, but they don't gate deployment viability. Worst-case-within-recommended-sizing does.

Convention applies uniformly across all protocols compared here. QBFT (which historically used 1 BTT per phase in SSV-internal docs, with `RT = 2000ms` absorbing jitter at the round level) is recomputed at recommended sizing for apples-to-apples comparison. Two QBFT variants appear in tables below:
- **QBFT-SSV**: current SSV production behavior (`RT = 2000ms`) with recommended per-emission sizing applied.
- **QBFT-optimal**: idealized QBFT with `RT = 3 × 2 BTT = 6 BTT` (= sum of 3 consensus phases at recommended sizing). Tighter RT triggers round-change sooner, freeing budget for additional rounds within the slot.

Both QBFT variants have the same R1 healthy-path timing (8 BTT = 6 BTT consensus + 2 BTT post-consensus). They differ in RT and consequently in how many rounds fit the slot budget. QBFT-optimal is hypothetical — not what SSV runs in production — included to show what QBFT looks like with symmetric per-emission sizing instead of round-level RT-based jitter absorption.

## Protocol summary

- **Partial-sigs on pre-agreed V** (baseline; not a BFT consensus protocol): each operator computes their BLS partial signature on a pre-agreed V and gossips it; threshold aggregation produces the cluster signature. **2 BTT** (one emission cycle at recommended sizing for partial-sig collection + threshold aggregation). Assumes V is pre-agreed by external mechanism (e.g., beacon-spec deterministic computation for attestations / sync committee duties). **Cannot resolve V-disagreement** (e.g., MEV bundles fetched by different operators differ) — this is what BFT consensus protocols solve. Used here as the floor: what's the cluster's pure cryptographic cost AFTER V is somehow agreed.
- **QBFT-SSV** (current SSV production): 3-phase consensus (PROPOSE → PREPARE → COMMIT) + post-consensus partial-sig collection = 4 emission cycles × 2 BTT = **8 BTT R1**. `RT = 2000ms = 10 BTT` round timeout absorbs propagation beyond recommended sizing before round-change. R2 fresh-V refetch on round timeout.
- **QBFT-optimal** (hypothetical): same R1 timing, `RT = 6 BTT` (= 3 consensus phases × 2 BTT). Tighter RT triggers round-change sooner; frees budget for R2/R3 within slot. Multi-round retry up to ~3 rounds within typical SSV proposer slot budget.
- **OBFT**: K-layer onion with chained encryption, single Phase 2 with one `KindCommit` emission per operator by `T_commit` (typically earlier on `T_L_0_observed` per the early-emit rule) carrying both σ and NR partials (no Defer state, no sub-phasing). Per-layer staggered broadcast deadlines `T_broadcast_max_k`, with deeper layers having wider propagation budgets `B_k`. Reflood-aware sizing: `B_k_shallow = (k+2)·BTT + RefloodDelay`; `B_0 = 2·BTT + 700ms = 1100ms` at default RefloodDelay (= SSV gossipsub HeartbeatInterval). Phase 2 budget = **1 BTT propagation** (recommended `Δ_2 = 1 BTT` — reflood is structurally absorbed by `B_k` via RefloodDelay) + 0 Phase 3. Recovery via in-round K-layer parallel fall-through (sequential local decryption in Phase 3, no extra BTT per layer). OBFT's Phase-1 per-layer absorption is structurally compensated by K-layer fall-through (deeper layers absorb up to `B_{K-1} = T_commit` at K=4 — earliest-possible deepest broadcast, full commit budget).
- **OBFTR** (R≥2): same K-layer onion as OBFT, plus R-round retry with re-flood, per-round independent commitments, L_C cluster-consensus signaling. **6 BTT R1** (2 BTT broadcast slack + 2 BTT Phase 2 + 2 BTT Phase 2.5 L_C + 0 Phase 3) **/ 6 BTT R2** (2 BTT re-flood + 2 BTT Phase 2 + 2 BTT L_C); total **12 BTT at R=2**. Recovers partition tails up to `R · P99` via re-flood across rounds.
- **2abOBFT**: K-layer onion with Phase 2a (verdict broadcast) + Phase 2b (σ-or-NR commit driven by convergence rule on Phase-2a verdict pool). **5 BTT** (2 BTT Phase 1 + 2 BTT Phase 2a + **1 BTT Phase 2b** at tightened `Δ_2b = 1·BTT + ε_proc` + 0 Phase 3; ε_proc ~50ms is absorbed). Single-round only.

## Total time to signed output (in BTT units)

All BTT counts use the uniform "2 BTT per emission cycle" recommended sizing — see [§Sizing convention](#sizing-convention) above.

| Protocol | Round 1 healthy | Round 2 (recovery) | Total at R-round failure |
|---|---|---|---|
| Partial-sigs on pre-agreed V (baseline) | 2 BTT | n/a (no rounds) | n/a (no recovery — fails on any V-disagreement) |
| OBFT | 3 BTT + RefloodDelay | n/a (single-round) | n/a (slot misses if R1 fails on adversarial pattern; K-layer fall-through is in-round, free) |
| OBFTR(R=2) | 6 BTT | 6 BTT | 12 BTT |
| 2abOBFT | 5 BTT + RefloodDelay | n/a (single-round) | n/a (K-layer fall-through is in-round, free) |
| QBFT-SSV | 8 BTT | 10 BTT (RT) + 8 BTT = 18 BTT | 18 BTT |
| QBFT-optimal | 8 BTT | 6 BTT (RT) + 8 BTT = 14 BTT | 20 BTT (R3 = +6 BTT RT + 8 BTT) |

K-layer fall-through (OBFT, OBFTR, 2abOBFT) is sequential local decryption in Phase 3 — no per-layer BTT cost. It recovers silent/late leaders within the same round.

**Why OBFT 3 BTT + RefloodDelay vs OBFTR 6 BTT per round.** OBFT's Phase-1 staggered model (see [OBFT.md §Setting](OBFT.md)) lets the primary L_0 broadcast at `T_commit − B_0` with `B_0 = 2·BTT + RefloodDelay` (the reflood-aware schedule — covers 1 BTT initial propagation + 1 BTT IWANT round-trip + one full RefloodDelay-sized lazy-push cycle for mesh-flaky receivers). The Phase-2 budget post-`T_commit` is `Δ_2 = 1 BTT` (tightened — reflood lives in `B_k`, so Δ_2 only covers the synchronous-fallback `KindCommit` propagation cycle). OBFTR uses uniform `T_commit_r − 2 BTT` broadcast slack at every round, a fuller `Δ_2 = 2 BTT` Phase 2, and a Phase 2.5 (L_C signaling). Net: OBFT saves 3 BTT vs OBFTR per round at recommended sizing (with RefloodDelay = 0 opt-out) or trades 0.5·BTT at default RefloodDelay = 700ms (RefloodDelay ≈ 3.5 BTT vs OBFTR's structural 3-BTT R1 overhead).

**Why QBFT 8 BTT > OBFT (3 BTT + RefloodDelay).** QBFT has 4 emission cycles per round (PROPOSE + PREPARE + COMMIT + post-consensus) vs OBFT's 1 emission cycle (Phase 2) plus broadcast slack. At 2 BTT per emission, QBFT's structural cost is 4 × 2 = 8 BTT; OBFT's is `B_0 + Δ_2 = (2 BTT + RefloodDelay) + 1 BTT = 3 BTT + RefloodDelay`. The OBFT structural difference (vs QBFT) is fundamental to the protocol shape (3-phase cluster-wide consensus vs onion with chained encryption); the +RefloodDelay term is OBFT's choice to absorb one gossipsub lazy-push cycle inside Phase 1 rather than fall through, which QBFT handles via its per-phase round-timer slack instead.

**Phase 3 in OBFT family.** Counted as 0 BTT in this comparison — Phase 3 is sequential local IBE decryption + cert construction, processing-bound (`ε_3 ≈ 50ms ≈ 0.25 BTT` at Config A), not propagation-bound. Cross-protocol totals here use the "0 BTT for processing-only steps" convention; deployment-time accounting (in [OBFT.md §Application's timing table](OBFT.md#timing-budget)) lists `ε_3` as a separate ~50ms row putting "consensus complete" 0.25 BTT later than the BTT count suggests.

**QBFT post-consensus is propagation-bound, not local.** Each operator broadcasts their partial-sig and the cluster threshold-aggregates — that's a real emission cycle at 2 BTT under recommended sizing. Counted as 2 BTT in the totals above. Note this is structurally different from OBFT-family Phase 3 (local-CPU only), which is why QBFT pays this 2 BTT and OBFT family doesn't.

## Effective BFT consensus budget by start time

| BFT start time | Budget |
|---|---|
| 0ms (immediate) | 3900ms |
| 800ms | 3100ms |
| 1200ms | 2700ms |
| 1800ms | 2100ms |
| 2500ms (late MEV fetch) | 1400ms |

## Table 1 — Success modes (healthy-path completion)

Healthy completion = leader honest, all Phase-1 bundles propagate, σ-quorum reaches at L_0 in round 1. All counts at recommended sizing (OBFT-family: tightened `Δ_2 = 1 BTT` / `Δ_2b = 1·BTT + ε_proc`; OBFTR and QBFT: 2 BTT per emission). **OBFT-family cells include `+RefloodDelay` (default 700ms = SSV's gossipsub HeartbeatInterval) per the reflood-aware schedule**: OBFT total = `3·BTT + RefloodDelay`, 2abOBFT total = `5·BTT + RefloodDelay` (ε_proc ~50ms folded in). For RefloodDelay=0 (fully-meshed cluster opt-out), subtract 700ms from each OBFT/2abOBFT cell.

### Table 1a — BFT start = 0ms (immediate), budget = 3900ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 400ms ✓ | 1300ms ✓ | 1200ms ✓ | 1700ms ✓ | 1600ms ✓ |
| 600ms | 1200ms ✓ | 2500ms ✓ | 3600ms ✓ | 3700ms ✓ | **4800ms ✗** |
| 1000ms | 2000ms ✓ | 3700ms ✓ | **6000ms ✗** | **5700ms ✗** | **8000ms ✗** |

### Table 1b — BFT start = 800ms, budget = 3100ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 400ms ✓ | 1300ms ✓ | 1200ms ✓ | 1700ms ✓ | 1600ms ✓ |
| 600ms | 1200ms ✓ | 2500ms ✓ | **3600ms ✗** | **3700ms ✗** | **4800ms ✗** |
| 1000ms | 2000ms ✓ | **3700ms ✗** | **6000ms ✗** | **5700ms ✗** | **8000ms ✗** |

### Table 1c — BFT start = 1200ms, budget = 2700ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 400ms ✓ | 1300ms ✓ | 1200ms ✓ | 1700ms ✓ | 1600ms ✓ |
| 600ms | 1200ms ✓ | 2500ms ✓ | **3600ms ✗** | **3700ms ✗** | **4800ms ✗** |
| 1000ms | 2000ms ✓ | **3700ms ✗** | **6000ms ✗** | **5700ms ✗** | **8000ms ✗** |

### Table 1d — BFT start = 1800ms, budget = 2100ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 400ms ✓ | 1300ms ✓ | 1200ms ✓ | 1700ms ✓ | 1600ms ✓ |
| 600ms | 1200ms ✓ | **2500ms ✗** | **3600ms ✗** | **3700ms ✗** | **4800ms ✗** |
| 1000ms | 2000ms ✓ tight | **3700ms ✗** | **6000ms ✗** | **5700ms ✗** | **8000ms ✗** |

### Table 1e — BFT start = 2500ms (late MEV fetch), budget = 1400ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 400ms ✓ | 1300ms ✓ tight | 1200ms ✓ tight | **1700ms ✗** | **1600ms ✗** |
| 600ms | 1200ms ✓ tight | **2500ms ✗** | **3600ms ✗** | **3700ms ✗** | **4800ms ✗** |
| 1000ms | **2000ms ✗** | **3700ms ✗** | **6000ms ✗** | **5700ms ✗** | **8000ms ✗** |

† **Partial-sigs only fits if V is pre-agreed.** For SSV proposer duty (V varies per operator due to MEV bundles), this isn't directly applicable — the cluster needs a BFT consensus protocol to resolve V-disagreement first. Shown here as the floor: what completion would look like if V were pre-agreed (e.g., for non-MEV duties like attestations).

**Reading Tables 1a–1e** (default RefloodDelay=700ms framing; at RefloodDelay=0 the OBFT-family cells shift down by 700ms):

- **Partial-sigs floor (V pre-agreed)**: 2 BTT fits at every BFT_start except (2500ms, 1000ms). Sets the absolute floor — BFT consensus protocols pay 1-6 BTT extra to resolve V-disagreement (the OBFT-family additionally absorbs one gossipsub reflood cycle inside Phase 1, accounting for the +RefloodDelay term).
- **BTT=200ms** (production-typical healthy mesh): OBFT (1300ms) and 2abOBFT (1700ms) fit BFT_start ≤ 1800ms. At BFT_start = 2500ms (late MEV fetch), OBFT is uniquely tight-fit (1300ms ≤ 1400ms by 100ms) alongside OBFTR R1 (1200ms); 2abOBFT (1700ms) and QBFT R1 (1600ms) miss.
- **BTT=600ms** (degraded mesh): OBFT (2500ms) fits BFT_start ≤ 1200ms; 2abOBFT (3700ms) fits only BFT_start = 0. OBFTR R1 (3600ms) fits at BFT_start = 0; misses at 800ms. QBFT R1 (4800ms) misses everywhere.
- **BTT=1000ms** (severely degraded): OBFT (3700ms) fits BFT_start = 0 only — the only BFT consensus protocol to do so. All other protocols miss everywhere.
- **OBFT < OBFTR R1 < QBFT R1 in BTT count at RefloodDelay=0**; **OBFT < OBFTR R1 < QBFT R1 at default RefloodDelay too** under the tightened sizing — OBFT's `3·BTT + RefloodDelay ≈ 6.5·BTT at default` still beats OBFTR's 6·BTT only at BTT ≤ 200ms but is comfortably ahead of QBFT R1 across the board.
- **Healthy-path ordering at default RefloodDelay**: Partial-sigs (2 BTT) < OBFT-without-RefloodDelay (3 BTT) < 2abOBFT-without-RefloodDelay (5 BTT) < OBFTR R1 = 6 BTT ≈ OBFT (3·BTT + RefloodDelay ≈ 6.5·BTT at default) < 2abOBFT (5·BTT + RefloodDelay ≈ 8.5·BTT at default) < QBFT R1 (8 BTT). At RefloodDelay=0: Partial-sigs (2) < OBFT (3) < 2abOBFT (5) < OBFTR R1 (6) < QBFT R1 (8). OBFT keeps its BTT-count lead at RefloodDelay=0; the default-RefloodDelay ordering reflects the cost of in-protocol reflood absorption.

## Table 2 — Failure-recovery modes

When round-1 / single-round fails (silent leader, partition, network jitter, but NOT adversarial-byz-locked patterns covered in Table 3), each protocol's recovery path consumes additional time. All counts at recommended sizing (2 BTT per emission).

### Table 2a — BFT start = 0ms, budget = 3900ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 2400ms ✓ | in-round (free) | 3600ms ✓ | 2800ms ✓ |
| 600ms | n/a | in-round (free) | **7200ms ✗** | in-round (free) | **10800ms ✗** | **8400ms ✗** |
| 1000ms | n/a | in-round (free) | **12000ms ✗** | n/a (R1 missed at default RefloodDelay) | **18000ms ✗** | **14000ms ✗** |

### Table 2b — BFT start = 800ms, budget = 3100ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 2400ms ✓ | in-round (free) | **3600ms ✗** | 2800ms ✓ |
| 600ms | n/a | in-round (free) | **7200ms ✗** | n/a (R1 missed) | **10800ms ✗** | **8400ms ✗** |
| 1000ms | n/a | in-round (free) | **12000ms ✗** | n/a (R1 missed) | **18000ms ✗** | **14000ms ✗** |

### Table 2c — BFT start = 1200ms, budget = 2700ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 2400ms ✓ | in-round (free) | **3600ms ✗** | **2800ms ✗** |
| 600ms | n/a | in-round (free) | **7200ms ✗** | n/a (R1 missed) | **10800ms ✗** | **8400ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **12000ms ✗** | n/a (R1 missed) | **18000ms ✗** | **14000ms ✗** |

### Table 2d — BFT start = 1800ms, budget = 2100ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | **2400ms ✗** | in-round (free) | **3600ms ✗** | **2800ms ✗** |
| 600ms | n/a | n/a (R1 missed at default RefloodDelay) | **7200ms ✗** | n/a (R1 missed) | **10800ms ✗** | **8400ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **12000ms ✗** | n/a (R1 missed) | **18000ms ✗** | **14000ms ✗** |

### Table 2e — BFT start = 2500ms (late MEV fetch), budget = 1400ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | **2400ms ✗** | n/a (R1 missed) | **3600ms ✗** | **2800ms ✗** |
| 600ms | n/a | n/a (R1 missed) | **7200ms ✗** | n/a (R1 missed) | **10800ms ✗** | **8400ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **12000ms ✗** | n/a (R1 missed) | **18000ms ✗** | **14000ms ✗** |

† **Partial-sigs has no failure-recovery mechanism**: any V-disagreement (operators sign different V's) results in cluster signature aggregation failing — no rounds, no re-flood, no fall-through. The baseline only works on the healthy V-pre-agreed path.

"In-round (free)" means the recovery happens within the same single round — no additional time cost. K-layer fall-through is sequential local decryption in Phase 3, processing-bound (~50ms ε_3 single-layer; ~200ms ε_3 × K at K=4 with K−1 silent layers), not BTT-bound.

**Reading Tables 2a–2e:**

- **OBFT and 2abOBFT have the cleanest network-failure recovery profile** at any start time where their healthy path fits — silent leader / partition recovery costs zero extra time via in-round K-layer fall-through. This is the structural advantage of K-layer onion with chained encryption: every honest leader in the K-layer rotation provides a fall-through opportunity within Phase 3.
- **OBFTR's R1+R2 retry** (12 BTT = 2400ms at BTT=200ms) fits at BFT_start ≤ 1200ms (2400ms ≤ 2700ms budget); misses at BFT_start ≥ 1800ms. At BTT ≥ 600ms, R1+R2 doesn't fit any start time. Production deployments need either tighter Δ_2 (sacrificing jitter absorption) or acceptance that round-2 recovery is narrow.
- **QBFT-SSV R2** (RT=2000ms + 8 BTT = 18 BTT = 3600ms at BTT=200ms) fits only at (0ms, 200ms) — 3600ms vs 3900ms budget, 300ms margin. **Round-2 retry is essentially unavailable** beyond the (0ms, 200ms) corner.
- **QBFT-optimal R2** (RT=6 BTT + 8 BTT = 14 BTT = 2800ms at BTT=200ms) fits at BFT_start ≤ 800ms (2800ms ≤ 3100ms); misses at BFT_start ≥ 1200ms (2800ms > 2700ms by 100ms). Tighter RT recovers some budget vs QBFT-SSV but still fails at BTT ≥ 600ms or BFT_start ≥ 1200ms. **R3** (= `2×RT + 8 BTT = 20 BTT = 4000ms` at BTT=200ms) overshoots even (0ms, 200ms) by 100ms — multi-round retry beyond R2 isn't viable under symmetric sizing.
- **OBFT and 2abOBFT cannot retry** (single-round). Their "recovery" is the in-round fall-through; if that doesn't reach σ-quorum (e.g., adversarial pattern locks the σ-or-NR pools — see Table 3), the slot misses.
- **Structural retry advantage** (round-2 with fresh-V refetch) belongs to QBFT-optimal: available at BFT_start ≤ 800ms with BTT=200ms. Outside this envelope retry doesn't fit. The OBFT family's K-layer in-round fall-through is structurally cheaper — it doesn't consume additional BTT and recovers silent leaders for free.

## Table 3 — Adversarial-byz failure mode recoverability (scenario-independent)

These failure modes depend on protocol *structure*, not on BTT or start time. They apply where the protocol's healthy path would otherwise fit (i.e., the cells in Table 1 marked ✓). QBFT-SSV and QBFT-optimal share structural recoverability since they're the same protocol with different RT — the difference is in *whether* recovery fits budget (covered in Tables 1, 2), not whether the recovery mechanism exists.

| Failure mode | Partial-sigs † | QBFT (both variants) ‡ | OBFT | OBFTR | 2abOBFT |
|---|---|---|---|---|---|
| **σ-locked equivocation 1-1-1** (byz delivers V, V', V'' to three honest at L_0) | n/a (no leader/equivocation surface; V pre-agreed) | ✓ via R2 fresh V | ✗ slot miss | ✗ slot miss (R-invariant) | ✓ convergence rule → fall-through |
| **h_V=1 selective-delivery deadlock** | n/a | ✓ via R2 | ✗ (algebraic deadlock at f=1, n=4: σ-pool=2 < qV; NR-pool=2 < qEnc; deterred via Assumption 4 across slots) | ✗ (R-invariant) | ✓ convergence rule |
| **Validity-divergence 3-of-4 majority** (head-change splits honest verdicts) | ✗ slot miss (no in-protocol V-disagreement resolution) | ✓ via R2 at moved head | ✗ | ✗ | ✓ convergence rule |
| **Validity-divergence 2-2 split** | ✗ slot miss | ✓ if head moves R1→R2 | ✗ | ✗ | ✗ (algebraic limit) |
| **2-1-byz-defect equivocation** (byz delivers V/V' + verdict-claims σV + Phase-2 NR-defects) | n/a | ✓ via R2 | ✓ via Phase-1 σ_V crypto-lock | ✓ via Phase-1 σ_V (R-invariant) | ✗ regression (Variant C trade) |
| **Verdict-equivocation under marginal h_V** | n/a | n/a (no verdict surface) | n/a | n/a | ✗ regression |
| **Mesh-flakiness with byz σ-refusal** | ✗ if 1 honest's partial doesn't reach + byz withholds, threshold under-quorum | ✓ via R2 round-reset | ✗ slot miss (cross-phase exclusivity) | ✗ (R-invariant) | ✓ Phase-2a observation absorbs |
| **Multi-leader silent (K-1 = 3 silent in K=4)** | n/a (no leader rotation) | ✗ multiple round-changes exceed budget | ✓ in-round K-layer fall-through | ✓ in-round | ✓ in-round |
| **Sustained partition > absorption window** | ✗ | ✗ | ✗ | ✗ (extends to R·BTT, then misses) | ✗ |
| **> f operators offline / byz** | ✗ | ✗ | ✗ | ✗ | ✗ |

† **Partial-sigs assumes V is pre-agreed across all honest** (e.g., via beacon-spec deterministic computation for attestations / sync committee). For SSV proposer duty with MEV, V varies per operator → partial-sigs alone cannot resolve V-disagreement → BFT consensus is required. The "n/a" entries above mark failure modes protocol-specific to leader/equivocation surfaces that don't exist in partial-sigs.

‡ **QBFT-SSV and QBFT-optimal share Table 3 cells** (same protocol, different RT). Difference between variants: how many rounds fit the slot budget — QBFT-SSV fits R2 only at (0ms, 200ms); QBFT-optimal fits R2 at BFT_start ≤ 800ms with BTT=200ms. Beyond those cells, "✓ via R2" recovery doesn't fit budget regardless of structural availability.

**Reading Table 3:**

- **QBFT recovers more adversarial-byz patterns** structurally than the OBFT family, but recovery only materializes when R2 fits budget. Under recommended sizing this means BTT=200ms with BFT_start ≤ 800ms (QBFT-optimal) or BFT_start = 0 only (QBFT-SSV). At BTT ≥ 600ms or BFT_start ≥ 1200ms, the structural QBFT-recovery advantage doesn't fit budget for any variant.
- **OBFT family avoids QBFT's structural disadvantage** at multi-leader-silent (K-1 ≥ 3) patterns: QBFT requires K serial round-changes (each ~RT + 8 BTT under recommended sizing), exceeding the 4000ms budget at any K-1 ≥ 3. OBFT/OBFTR/2abOBFT recover within a single round via K-layer fall-through (free in Phase 3).
- **2abOBFT's convergence-rule recoveries** (1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness) close the patterns that bare OBFT/OBFTR leave as Class B exposures, at the cost of two narrower regressions (2-1-byz-defect, verdict-equivocation) — both slashable, both R-invariant.
- **Bare OBFT and OBFTR succeed at 2-1-byz-defect** that 2abOBFT misses, because the leader's Phase-1 σ_V crypto-locks the σ-pool against post-σ defection. 2abOBFT removes Phase-1 σ_V (Variant C) to gain validity-divergence recovery; pays the regression as the structural cost.

## Table 4 — MEV-fetch budget by protocol (BTT=200ms)

At the SSV proposer-duty operating point — `BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`, `RANDAO_done ≈ 150ms` (see [OBFT.md §Application](OBFT.md#timing-budget) for the full derivation) — each protocol's leader has a different MEV-relay-fetch budget bounded by when its broadcast must complete. The fetch budget is the wall-clock from `RANDAO_done` to the leader's broadcast deadline. All counts at recommended sizing (2 BTT per emission).

**OBFT (K=4)** — staggered per-layer broadcast at `T_broadcast_max_k = max(0, T_commit − B_k)` with the reflood-aware schedule `B_k_shallow = (k+2)·BTT + RefloodDelay`, `B_{K-1} = T_commit` (see [OBFT.md §Setting](OBFT.md#setting)) at `T_commit = 3600ms` (post-tighten). SSV production defaults to `RefloodDelay = 700ms` (gossipsub HeartbeatInterval); fully-meshed clusters may opt out by setting RefloodDelay near zero:

| Leader | Broadcast (RefloodDelay=700ms default) | MEV-fetch (default) | Broadcast (RefloodDelay=0 opt-out) | MEV-fetch (RefloodDelay=0) |
|---|---|---|---|---|
| V_0 (primary) | 2500ms | **~2350ms** | 3200ms | **3050ms** |
| V_1 | 2300ms | ~2150ms | 3000ms | 2850ms |
| V_2 | 2100ms | ~1950ms | 2800ms | 2650ms |
| V_3 (deepest) | 0ms (slot start) | ~0ms | 0ms | ~0ms |

**Partial-sigs on pre-agreed V (baseline)** — V agreed externally; consensus = 2 BTT (recommended sizing) for partial-sig propagation + aggregation. Broadcast deadline = `Relay_cutoff − 100ms − 2 BTT = 3500ms`:

| Step | Time | MEV-fetch budget |
|---|---|---|
| V determined (must be cluster-agreed) | ≤ 3500ms | **3350ms** |

**QBFT-SSV (RT=2000ms, 2-round target)** — single leader per round; fetch must complete before each round's PROPOSE. R1 PROPOSE deadline derived from R2 fit constraint: `PROPOSE_R1 + RT + 8 BTT + 100ms ≤ 4000ms` → `PROPOSE_R1 ≤ 300ms`:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 300ms | **150ms** |
| R2 | 2300ms | 2150ms |

**QBFT-optimal (RT=6 BTT, 2-round target)** — tighter RT lets PROPOSE_R1 fire later. `PROPOSE_R1 + RT + 8 BTT + 100ms ≤ 4000ms` → `PROPOSE_R1 ≤ 1100ms`:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 1100ms | **950ms** |
| R2 | 2300ms | 2150ms |

(QBFT-optimal R3 doesn't fit even with PROPOSE_R1 = 0 — 2 × RT + 8 BTT = 4000ms exceeds the 3900ms deadline by 100ms. R-round target capped at 2.)

**Cross-protocol ranking** (recommended sizing throughout; OBFT figures shown at both RefloodDelay settings):

| Rank | Leader | MEV-fetch (RefloodDelay=700ms default) | MEV-fetch (RefloodDelay=0 opt-out) | Notes |
|---|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3350ms** | 3350ms | Floor: only available if V is pre-agreed (no MEV / no V-disagreement) |
| 2 | OBFT V_0 | **~2350ms** | **3050ms** | Best BFT-consensus protocol for MEV proposer duty |
| 3 | OBFT V_1 / QBFT R2 (tie) | ~2150ms | OBFT 2850ms / QBFT 2150ms | OBFT V_1 ties QBFT R2 at default RefloodDelay; OBFT V_1 leads by 700ms at RefloodDelay=0. QBFT R2 only after paying the R1-timeout gap |
| 4 | OBFT V_2 | ~1950ms | 2650ms | |
| 5 | QBFT-optimal R1 | 950ms | 950ms | |
| 6 | QBFT-SSV R1 | 150ms | 150ms | Tightest budget; SSV's wide RT shrinks R1 fetch window |
| 7 (last) | OBFT V_3 | ~0ms | ~0ms | Deepest "earliest possible" — fetches deepest-confirmed parent at slot start; trades MEV for full-slot propagation budget |

† **Partial-sigs is not directly comparable** for SSV proposer duty (V varies per operator). Shown as the no-consensus floor — what would be possible if V didn't need cluster-wide agreement.

**Reading:**

- **OBFT V_0 vs QBFT R2**: at default RefloodDelay (=700ms, matching SSV's gossipsub HeartbeatInterval), OBFT V_0's ~2350ms beats QBFT R2's 2150ms by **200ms** — and OBFT V_0 is the *primary* leader (always tried first; no round-timeout gap), while QBFT R2 is reachable only after R1 fails (paying the ~2s round-change cost). At RefloodDelay=0 (fully-meshed opt-out) OBFT V_0 captures **900ms more** MEV-fresh fetch time than QBFT R2 (3050 vs 2150ms). OBFT V_0/V_1/V_2 all beat QBFT-SSV R1 by ≥1800ms at either RefloodDelay setting. OBFT V_3 trades all MEV-fetch headroom for full-slot absorption width (`B_3 = T_commit`), making it the structural backstop layer rather than a competitive MEV-fetch tier.
- **OBFT V_0 pays a 300ms–1000ms BFT-consensus tax over the partial-sigs floor** (depending on RefloodDelay): 3350 − 3050 = 300ms (1.5·BTT) at RefloodDelay=0, 3350 − ~2350 = ~1000ms (~5·BTT) at default RefloodDelay. The tax decomposes as `B_0 + post-T_commit budget − partial-sigs post-fetch overhead = (2·BTT + RefloodDelay) + 2·BTT − 2.5·BTT`. The 2·BTT shallow base covers 1·BTT P99 leader-broadcast propagation + 1·BTT IWANT round-trip; RefloodDelay covers one full IHAVE/IWANT cycle for mesh-flaky receivers. OBFT runs at tightened `Δ_2 = 1 BTT`; partial-sigs floor reserves 1·BTT sign + 1·BTT agg + 0.5·BTT (100ms) submit = 2.5·BTT post-fetch.
- **QBFT-SSV R1 is structurally constrained** to 150ms MEV-fetch under the 2-round target — RT=2000ms eats 2000ms of slot budget, leaving the R1 leader essentially no fetch time. QBFT-optimal recovers ~800ms of R1 fetch by tightening RT to 6 BTT, but R2 fetch budget stays at 2150ms either way. QBFT's RT framing is RefloodDelay-independent.
- **OBFT V_3 trades all MEV-fetch for full-slot absorption**: under the "earliest possible" deepest default, V_3 fetches at slot start (deepest-confirmed parent — re-org resistant) and broadcasts immediately, giving the cluster the entire `T_commit` budget for that bundle's propagation. OBFT's K-layer fall-through is in-round (sequential local IBE decryption, no per-layer RTT).
- **Deeper-layer fetch budgets trade fetch time for propagation slack**: at default RefloodDelay, V_2 covers 1500ms tails, V_3 covers the full `T_commit` (= 3600ms at Config A). Healthy-path fetch is V_0's ~2350ms; deeper fetches are recovery-only.

**OBFTR(R=2) and 2abOBFT primary-leader fetch budgets** at BTT=200ms (recommended sizing). 2abOBFT shares OBFT's reflood-aware schedule `B_k_shallow = (k+2)·BTT + RefloodDelay` but anchors broadcast at `T_verdict_start = T_commit − Δ_2a ≈ 3.20s` (Δ_2a = 400ms of pre-Phase-2a budget vs bare OBFT's T_commit-anchored broadcast). OBFTR(R=2) totals are R-round-summed.

| Protocol | Total BTT | Broadcast (RefloodDelay=700ms default) | MEV-fetch (default) | Broadcast (RefloodDelay=0) | MEV-fetch (RefloodDelay=0) |
|---|---|---|---|---|---|
| OBFTR(R=2) (R1+R2 fit) | 12 BTT | ~1500ms | ~1350ms | ~1500ms | ~1350ms |
| OBFTR(R=2) (R1-only) | 6 BTT | ~2700ms | ~2550ms | ~2700ms | ~2550ms |
| 2abOBFT V_0 (primary) | 5 BTT + RefloodDelay | ~2100ms | ~1950ms | ~2800ms | ~2650ms |
| 2abOBFT V_1 | 5 BTT + RefloodDelay | ~1900ms | ~1750ms | ~2600ms | ~2450ms |
| 2abOBFT V_2 | 5 BTT + RefloodDelay | ~1700ms | ~1550ms | ~2400ms | ~2250ms |
| 2abOBFT V_3 (deepest) | 5 BTT + RefloodDelay | 0ms (slot start) | ~0ms (deepest-confirmed parent) | 0ms | ~0ms |

OBFTR's broadcast deadlines are anchored on R-round completion (not B_k); the schedule's reflood-aware framing doesn't apply the same way because cross-round retention already provides multi-round absorption. 2abOBFT's V_0 MEV-fetch is tighter than bare OBFT's V_0 at the same RefloodDelay setting because of the +Δ_2a anchor shift (Phase-2a window cost = 400ms = 2·BTT) — see the cross-protocol-ranking discussion above for the trade-off.

2abOBFT mirrors OBFT's per-layer staggered schedule (`B_k_shallow = (k+2)·BTT + RefloodDelay`; `B_{K-1} = T_verdict_start`); per-layer broadcast targets are `max(0, T_verdict_start − B_k)` where `T_verdict_start = T_commit − Δ_2a`. Phase-2a re-flood absorption applies *uniformly* across all layers on top of per-layer `B_k` (see [2abOBFT.md §Setting](2abOBFT.md#setting) for the composition). Both protocols use the same reflood-aware B_k formula, so the per-layer broadcast slack is identical at the same RefloodDelay setting; 2abOBFT's extra +Δ_2a cost (vs bare OBFT) reflects the dedicated Phase-2a re-flood window that absorbs late bundles uniformly across layers, in exchange for additional structural recovery (convergence-rule adversarial-byz recoveries). OBFTR pays the equivalent cost via extended partition tail absorption via cross-round retention.

The **MEV-fetch budget asymmetry is a structural OBFT-family advantage over QBFT** — under recommended sizing, OBFT V_0 has 3.2× the MEV-fresh fetch time of QBFT-optimal R1 leader and 20× of QBFT-SSV R1 leader. OBFT structurally avoids the round-change gap that gates QBFT's R2 fetch.

## Cross-scenario takeaways

**Partial-sigs floor (V pre-agreed)**: 2 BTT = 400ms total at recommended sizing. Fits at every (BFT_start, BTT) cell except (2500ms, 1000ms). Sets the floor: BFT-consensus protocols pay 1-6 BTT extra to resolve V-disagreement. For SSV proposer duty (V varies per operator due to MEV bundles), partial-sigs alone is not directly applicable — used here as a reference for the BFT-consensus tax.

**Healthy-path latency at production-typical BTT (200ms)** *(BTT-count only; add +RefloodDelay for OBFT-family totals)*: partial-sigs 400ms, OBFT 600ms, 2abOBFT 1000ms, OBFTR-R1 1200ms, QBFT R1 1600ms. OBFT/OBFTR/2abOBFT fit at every BFT_start (Tables 1a-1e, BTT=200ms); QBFT R1 fits at BFT_start ≤ 1800ms but misses at BFT_start = 2500ms.

**Late-fetch tolerance (BFT start = 2500ms, budget = 1400ms)**: at BTT=200ms, partial-sigs (400ms) and OBFT (600ms, or 1300ms incl. default RefloodDelay) fit comfortably; OBFTR R1 (1200ms) is tight (200ms margin); 2abOBFT (1000ms, or 1700ms incl. default RefloodDelay) misses at default RefloodDelay by 300ms; QBFT R1 (1600ms) misses by 200ms. At BTT ≥ 600ms, all consensus protocols miss — late-fetch is incompatible with degraded mesh.

**Degraded-mesh tolerance (BTT = 1000ms)**: OBFT (3·BTT = 3000ms BTT-count, or 3700ms incl. default RefloodDelay) is the only consensus protocol that fits any cell — at BFT_start = 0 only. QBFT R1 (8 BTT = 8000ms) misses everywhere; OBFTR R1 (6·BTT = 6000ms) and 2abOBFT (5·BTT = 5000ms BTT-count) miss at all BFT_starts.

**Mid-BTT tolerance (BTT = 600ms)**: bare OBFT (1800ms BTT-count, or 2500ms incl. default RefloodDelay) fits at BFT_start ≤ 1200ms; OBFTR R1 (3600ms) fits only at BFT_start = 0; 2abOBFT (3000ms BTT-count, or 3700ms incl. default RefloodDelay) fits only at BFT_start = 0; QBFT R1 (4800ms) misses everywhere. All consensus protocols miss at BFT_start = 2500ms.

**Round-2 retry usefulness**: under recommended sizing — **OBFTR's R1+R2 (12 BTT = 2400ms at BTT=200ms) fits at BFT_start ≤ 1200ms** (2400ms ≤ 2700ms budget); misses at BFT_start ≥ 1800ms. **QBFT-SSV R2 (RT + 8 BTT = 18 BTT = 3600ms) fits only at (0ms, 200ms)** (3600ms ≤ 3900ms budget). **QBFT-optimal R2 (RT + 8 BTT = 14 BTT = 2800ms) fits at BFT_start ≤ 800ms with BTT=200ms** (2800ms ≤ 3100ms budget); misses at BFT_start ≥ 1200ms. At BTT ≥ 600ms, no protocol's R2 fits. **QBFT-optimal R3 doesn't fit any cell.** The OBFT family's K-layer in-round fall-through is structurally cheaper than retry — it doesn't consume additional BTT and recovers silent leaders for free.

**Adversarial-byz exposure ranking** (most-recovered to least-recovered):

1. **2abOBFT + R-round retry** (hypothetical): closes most patterns including 2-1-byz-defect, but doesn't exist as a specified protocol.
2. **Bare 2abOBFT**: closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness via convergence rule. Misses 2-1-byz-defect and verdict-equivocation.
3. **QBFT (with R2 budget)**: closes adversarial-byz via fresh-V refetch on round-change. Bound by RT + 8 BTT fitting within budget — under recommended sizing, available at (0ms, BTT=200ms) for QBFT-SSV; BFT_start ≤ 800ms with BTT=200ms for QBFT-optimal.
4. **Bare OBFT**: closes silent-leader / partition cases via K-layer fall-through. **Does NOT close**: h_V=1 selective Phase-1 delivery, 1-1-1 σ-locked equivocation, validity-divergence boundary splits — these remain R-invariant Class B grief deterred via Assumption 4 (rational-byzantine deterrent + planned blacklist + staker migration).
5. **Bare OBFTR**: closes silent-leader / partition cases via K-layer fall-through (single round) and partition-tail cases via R-round retry. Adversarial patterns (1-1-1, h_V=1, etc.) are R-invariant slot-misses; rational-byzantine deterrent absorbs across slots.

**Multi-leader-silent advantage**: OBFT family (OBFT, OBFTR, 2abOBFT) all complete at K-1 ≥ 3 silent within a single round via Phase-3 reconstruction walk. QBFT cannot — serial round-changes at RT=2000ms (or RT=6 BTT for optimal) exceed the 4000ms budget at any K-1 ≥ 3. Structural OBFT-family advantage at any (BFT_start, BTT) combination where the healthy path fits.

**Choosing a protocol** (deployment guidance):

- **Pre-agreed V (no consensus needed)**: partial-sigs floor at 2 BTT = 400ms. Use for SSV duties where V is deterministic (attestations, sync committee). Not applicable to MEV proposer duty since V varies per operator.
- **Healthy-path latency-critical (with consensus)**: OBFT at BTT=200ms (600ms BTT-count completion) — best in family at the tightened sizing. 2abOBFT 1000ms; OBFTR-R1 1200ms; QBFT-SSV / QBFT-optimal R1 1600ms.
- **Late-fetch / high-MEV proposer duty (BFT start ≥ 2000ms)**: OBFT (600ms BTT-count, 1300ms incl. default RefloodDelay) fits comfortably at 2500ms start with healthy mesh; OBFTR-R1 (1200ms) tight; 2abOBFT (1000ms BTT-count, 1700ms incl. default RefloodDelay) misses at default RefloodDelay; QBFT R1 (1600ms) misses.
- **Adversarial-byz robustness within single round**: 2abOBFT — closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness without round-2 budget cost.
- **Multi-round partition tail absorption**: OBFTR(R=2) — extends absorption to ~R·BTT ~600-1200ms beyond OBFT's window, when the budget admits.
- **QBFT-SSV (current SSV)**: production-mature; under recommended sizing, fits BFT_start ≤ 1800ms at BTT=200ms; misses at 2500ms start; misses at BTT ≥ 600ms. R2 retry available only at (0ms, 200ms).
- **QBFT-optimal**: hypothetical reference point — same R1 timing as QBFT-SSV but tighter RT lets R2 fit at BFT_start ≤ 800ms with BTT=200ms. Not what SSV runs in production.

## OBFT + L_Bid mini-consensus extension

OBFT + L_Bid (specified in [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)) is an opportunistic bid-routing extension to bare OBFT. It prepends a bid-determined L_Bid layer above OBFT's K rotation-determined layers (yielding `K' = K + 1`) and adds a mini-consensus sub-phase between `T_0_arrival` and `T_commit` that resolves L_Bid identity cluster-wide before σ-commitment. This section identifies scenarios where OBFT+L_Bid's behavior differs from bare OBFT and from the other three protocols. **Most scenarios are identical between bare OBFT and OBFT+L_Bid**; the differences are surfaced below.

### Differences vs bare OBFT (summary)

- **+2 BTT total consensus time**, all in pre-`T_commit` budget: OBFT+L_Bid is **5 BTT** (2 BTT broadcast slack + 2 BTT mini-consensus + 1 BTT Phase 2 + 0 Phase 3) vs bare OBFT's 3 BTT at conservative `Δ_minicon = 2 BTT` (numbers reflect the post-tighten bare-OBFT framing; full L_Bid re-derivation under the reflood-aware schedule is pending — see [OBFT.md Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)). `T_commit` is back-end-anchored and unchanged from bare OBFT; the 2 BTT mini-consensus runs as a sub-phase at the tail of Phase 1, so the cost falls on the L_0..L_{K-1} broadcast deadlines (MEV-fetch budget shrinks by `Δ_minicon`), not on post-`T_commit` slack.
- **Value capture upside**: highest-bid eligible rotation-layer block on the healthy path (when L_Bid σ-quorum reaches) instead of fixed rotation-priority V.
- **New failure modes at L_Bid**: 2-1-byz-defect (mixed evidence quality — cryptographic Rules 7/8 for some triggers/actions, behavioral for silent variants) and verdict-equivocation (cryptographic Rule 8); both slot-miss-without-fall-through to L_0.
- **L_0..L_{K-1} rotation layers are unchanged**: when the mini-consensus fails to converge the cluster falls through to L_0 with the same recovery profile as bare OBFT. C1/C2 closure is conditional — see [Adversarial-byz failure modes](#adversarial-byz-failure-modes-specific-to-l_bid--table-3-delta) below.

### Where OBFT+L_Bid's outcome differs from bare OBFT

#### Success-mode delta — Table 1

Four scenarios show different success outcomes between bare OBFT and OBFT+L_Bid (at recommended Δ sizing). In all other (BFT_start, BTT) combinations, both protocols complete healthy or both miss healthy. The full-protocol comparison at the differing scenarios:

| Scenario | Budget | QBFT R1 | Bare OBFT | **OBFT+L_Bid** | OBFTR R1 | 2abOBFT |
|---|---|---|---|---|---|---|
| **0ms, BTT=1000ms** | 3900ms | **8000ms ✗** | 3000ms ✓ | **5000ms ✗** | **6000ms ✗** | **6000ms ✗** |
| **800ms, BTT=1000ms** | 3100ms | **8000ms ✗** | 3000ms ✓ tight | **5000ms ✗** | **6000ms ✗** | **6000ms ✗** |
| **1200ms, BTT=600ms** | 2700ms | **4800ms ✗** | 1800ms ✓ | **3000ms ✗** | **3600ms ✗** | **3600ms ✗** |
| **1800ms, BTT=600ms** | 2100ms | **4800ms ✗** | 1800ms ✓ | **3000ms ✗** | **3600ms ✗** | **3600ms ✗** |

OBFT+L_Bid loses bare OBFT's healthy-path advantage at degraded-mesh × moderate-or-later start scenarios — its 5-BTT structure (vs bare OBFT's 3-BTT) joins the protocols that miss budget. At BTT=200ms (every BFT_start) and at BTT=600ms with BFT_start ≤ 800ms, OBFT and OBFT+L_Bid fit equally.

#### Failure-recovery delta — Table 2

**No latency difference.** Both bare OBFT and OBFT+L_Bid recover via in-round K-layer / K'-layer fall-through (sequential local decryption in Phase 3, no per-layer BTT cost). OBFT+L_Bid's K' = K + 1 adds one extra layer at the top, giving an additional "first-try" recovery opportunity at no extra time. Recovery profile across all scenarios is identical between bare OBFT and OBFT+L_Bid.

#### Adversarial-byz failure modes specific to L_Bid — Table 3 delta

These failure modes don't apply to bare OBFT (no L_Bid layer):

| Failure mode | Bare OBFT | OBFT+L_Bid |
|---|---|---|
| **C1 — Selective candidate withholding at L_Bid** | n/a | ✓ closed when verdict-quorum doesn't form; otherwise folds into 2-1-byz-defect (below) |
| **C2 — Candidate / bid equivocation at L_Bid** | n/a | ✓ closed when verdict-quorum doesn't form; otherwise folds into 2-1-byz-defect (below) |
| **C3 — V_LBid validity-divergence majority (3-of-4)** | n/a | ✓ closed by convergence rule |
| **2-1-byz-defect at L_Bid** | n/a | **✗ slot miss** (deadlock blocks L_0 fall-through); mixed evidence — base leader-equivocation or Rule 7 under candidate/bid equivocation, Rule 8 under NR-emit (Rule 6b analog in 2abOBFT's numbering, were 2abOBFT+L_Bid specified), behavioral for silent variants |
| **Verdict-equivocation at L_Bid** | n/a | **✗ slot miss** (slashable Rule 8 in OBFT+L_Bid; Rule 6a analog in 2abOBFT's numbering — verdict-vs-verdict equivocation) |
| **2-2 validity split at L_Bid** | n/a | **✗ algebraic limit** |
| L_0..L_{K-1} rotation-layer failures | (per Table 3) | **Same as bare OBFT** |

In the context of L_Bid integration across the OBFT family — only **OBFT + L_Bid** is specified ([docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)); **OBFTR + L_Bid** and **2abOBFT + L_Bid** are hypothetical compositions discussed structurally below but not yet written as appendices in their respective spec docs:

| L_Bid failure mode | OBFT+L_Bid | OBFTR+L_Bid | 2abOBFT+L_Bid |
|---|---|---|---|
| C1/C2/C3 deadlocks | ✓ conditional (C1/C2 close when verdict-quorum doesn't form, else fold into 2-1-byz-defect; C3 on 3-of-4 majority) | ✓ same | ✓ same |
| 2-1-byz-defect at L_Bid | ✗ slot miss | ✗ slot miss (R-invariant) | ✗ regression |
| Verdict-equivocation at L_Bid | ✗ slot miss | ✗ slot miss | ✗ regression |
| 2-2 validity split at L_Bid | ✗ algebraic limit | ✗ algebraic limit | ✗ algebraic limit |
| Multi-leader silent (across L_Bid + rotation) | ✓ in-round K'-layer fall-through | ✓ in-round | ✓ in-round |

The L_Bid-specific failure modes are structurally identical across the three protocol families — convergence-rule recoveries close C1/C2/C3 conditionally (C1/C2 fold into 2-1-byz-defect when verdict-quorum forms via byz; C3 on 3-of-4 majority); residuals (2-1-byz-defect, verdict-equivocation) match across all three.

### Adversarial-byz trigger frequency

Bare OBFT's L_0 adversarial-byz patterns (σ-locked equivocation, h_V=1, etc.) trigger only when byz is the rotation L_0 leader — typically 1/n slots at uniform rotation (25% of byz-controlled slots at f=1 n=4). OBFT+L_Bid candidate-withholding/equivocation surfaces trigger when byz is among the K rotation leaders (`K/n` under uniform selection, every slot at `K=n`); verdict-equivocation remains available to any byz operator every slot because every operator broadcasts a verdict. The L_Bid extension therefore increases adversarial-byz trigger frequency relative to bare OBFT's L_0-only surface when `K > 1`, but it no longer assumes standalone all-operator bid envelopes.

### Net trade vs bare OBFT

OBFT+L_Bid pays:
- **+2 BTT total consensus time** (in pre-`T_commit` budget — MEV-fetch reduction; post-`T_commit` matches bare OBFT). Loses bare OBFT's advantage at the four (BFT_start, BTT) cells listed in the success-mode delta table above (degraded-mesh × moderate-or-later starts) where bare OBFT fits and OBFT+L_Bid doesn't; all other scenarios are unaffected at the budget-fit level.
- **+adversarial-byz exposure at L_Bid** (2-1-byz-defect with mixed evidence quality, verdict-equivocation cryptographic; slot-miss without fall-through; higher trigger frequency than rotation-only patterns).
- **+structural complexity** (`Phase1Bundle` bid metadata, new `KindBidVerdict`, two new slashing rules, mini-consensus protocol step).

In exchange for:
- **Bid-routing value capture** on healthy path (highest-bid eligible rotation-layer block vs fixed rotation-priority V).
- **C1/C2/C3 conditional closure at L_Bid** (vs the naive bid-routing sketch which leaves these open). C1/C2 close when verdict-quorum doesn't form; residuals fold into 2-1-byz-defect rather than deadlock without attribution.

The trade is favorable when MEV bid-routing value-capture upside exceeds the combined cost of (a) the new failure modes' slot-loss rate and (b) the +2 BTT MEV-fetch budget reduction (pre-`T_commit`). For low-MEV slots or deployments with significant mesh degradation pushing scenarios toward the (BFT_start ≤ 800ms, BTT=1000ms) or (BFT_start ≥ 1200ms, BTT=600ms) borderlines, bare OBFT is the better choice.

## Limits of this comparison

- **Numbers are BTT-count approximations** (3 BTT, 4 BTT, etc.). Production has long tails; ε_3 (~50ms local processing per layer) is treated as small relative to BTT in tabulation. Real implementations may add 50-200ms of constant overhead per round.
- **QBFT round timeout RT = 2000ms** is held fixed; tightening RT shrinks recovery time but raises false-positive round-changes under jitter.
- **K = n = 4** assumed. At larger n with the same f-bound, K-layer fall-through depth scales (more redundancy at the OBFT family). QBFT's recovery cost scales linearly with K serial round-changes.
- **Bandwidth (small `V`, e.g. attestations ~100 B; cluster-wide healthy path)**: QBFT ~14 KB across 4 emissions per round; OBFT ~28 KB across 1 emission (includes the `sigma_L_witnesses` section ≈ +2.3 KB at K=4 n=4); OBFTR ~28-30 KB across 2 emissions per round (R=2 worst case ~52 KB across 4 emissions); 2abOBFT ~30 KB across 2 emissions (no σ_L^V witness; 2abOBFT has no Phase-1 σ_L^V). All four +3-5 KB if L_Bid mini-consensus extension is used (see [each doc's Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)).
- **Bandwidth at larger V**: OBFT's σ_L^V witness section scales with the σ partial size (~145 B/witness), not with V's payload — so OBFT's bandwidth stays close to the small-`V` baseline regardless of V size. Few-large emissions (OBFT) are gentler on the gossipsub mesh than many-small (QBFT) at SSV's KB-range message sizes — per-message overhead (signature verify, dedup, peer-score, mesh forwarding) dominates over per-byte cost.
- **Pre-consensus / block-fetch overhead** is excluded — sits in `[slot_start, BFT_start]` and is ~equal across protocols.
- **Partial network partitions** (some operators have a quorum view, others don't) aren't separately modeled. All four protocols degrade to slot-miss for the partitioned operators; cluster-wide outcome depends on which side has 2f+1 honest.
- **Adversarial-byz trigger frequency** is not modeled. Practical impact depends on byz-leader rotation distribution and bid-equivocation surface for L_Bid extensions (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) for L_Bid-specific exposure analysis).

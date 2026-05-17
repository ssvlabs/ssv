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

All BTT counts in this doc use a **uniform "1 BTT per emission cycle" sizing** — each cluster-wide message exchange (PROPOSE, PREPARE, COMMIT, KindCommit, KindVerdict, KindOnion2b, etc.) is budgeted at one P99 propagation cycle. Mesh-jitter / reflood absorption is **structural** rather than per-emission: each protocol carries its jitter cushion in a dedicated mechanism instead of inflating every emission by 2× (which the previous "2 BTT per emission" convention did).

Per-protocol jitter / reflood cushion:
- **OBFT**: in the per-layer broadcast budget `B_k` — primary `B_0 = 2·BTT + RefloodDelay` covers one initial propagation + one IWANT round-trip + one lazy-push cycle; backups `B_k = T_commit` absorb up to the full commit budget. Δ_2 tightened to `1·BTT` (KindCommit propagation only).
- **2abOBFT**: in the Phase-2a re-flood window (Δ_2a ≥ 2·BTT) plus per-layer `B_k`. Δ_2b tightened to `1·BTT + ε_proc`.
- **OBFTR**: in cross-round retention — bundles missing round r's σ-window remain usable in round r+1. Per-round Δ_2 = `1·BTT` (KindCommit_r propagation only).
- **QBFT-SSV**: in round-timer slack `RT − R1 = 10·BTT − 4·BTT = 6·BTT` at BTT=200ms (production-anchored `RT = 2000ms`).
- **QBFT-optimal**: in round-timer slack `RT − R1 = 6·BTT − 4·BTT = 2·BTT` (`RT = 6·BTT` chosen to leave 1·BTT for ROUND_CHANGE preamble + 1·BTT for mesh-jitter).

**This is the production-survival sizing for mission-critical roles.** For SSV proposer duty: missing a slot is per-slot economic loss + signal to stakers, so the configuration target is worst-case completion under recommended sizing. The structural cushions above are sized for P99→P9999 absorption.

QBFT (which historically used 1 BTT per phase in SSV-internal docs, with `RT = 2000ms` absorbing jitter at the round level) is recomputed under the unified 1·BTT-per-emission convention for apples-to-apples comparison. Two QBFT variants appear in tables below:
- **QBFT-SSV**: current SSV production behavior (`RT = 2000ms`); jitter absorbed by the wide `RT − R1` slack.
- **QBFT-optimal**: idealized QBFT with `RT = 6 BTT` (3·BTT consensus + 1·BTT ROUND_CHANGE preamble + 2·BTT jitter slack). Tighter RT triggers round-change sooner, freeing budget for additional rounds within the slot.

Both QBFT variants have the same R1 healthy-path timing (4 BTT = 3 BTT consensus + 1 BTT post-consensus at tightened per-emission). They differ in RT and consequently in how many rounds fit the slot budget. QBFT-optimal is hypothetical — not what SSV runs in production — included to show what QBFT looks like with the tighter RT operational point.

## Protocol summary

- **Partial-sigs on pre-agreed V** (baseline; not a BFT consensus protocol): each operator computes their BLS partial signature on a pre-agreed V and gossips it; threshold aggregation produces the cluster signature. **1 BTT** (one emission cycle at tightened per-emission sizing for partial-sig collection + threshold aggregation). Assumes V is pre-agreed by external mechanism (e.g., beacon-spec deterministic computation for attestations / sync committee duties). **Cannot resolve V-disagreement** (e.g., MEV bundles fetched by different operators differ) — this is what BFT consensus protocols solve. Used here as the floor: what's the cluster's pure cryptographic cost AFTER V is somehow agreed.
- **QBFT-SSV** (current SSV production): 3-phase consensus (PROPOSE → PREPARE → COMMIT) + post-consensus partial-sig collection = 4 emission cycles × 1 BTT = **4 BTT R1**. `RT = 2000ms = 10 BTT` round timeout — wide `RT − R1 = 6·BTT` slack absorbs mesh-jitter / reflood tail before triggering round-change. R2 fresh-V refetch on round timeout.
- **QBFT-optimal** (hypothetical): same R1 timing, `RT = 6 BTT` (3·BTT consensus + 1·BTT ROUND_CHANGE preamble + 2·BTT jitter slack). Tighter RT triggers round-change sooner; frees budget for R2/R3 within slot. Multi-round retry up to ~3 rounds within typical SSV proposer slot budget.
- **OBFT**: K-layer onion with chained encryption, single Phase 2 with one `KindCommit` emission per operator by `T_commit` (typically earlier on `T_L_0_observed` per the early-emit rule) carrying both σ and NR partials (no Defer state, no sub-phasing). Primary-vs-backup broadcast deadlines `T_broadcast_max_k`: the primary L_0 has `B_0 = 2·BTT + RefloodDelay = 1100ms` at default RefloodDelay (= SSV gossipsub HeartbeatInterval) for the MEV-fresh fetch; all backups L_1..L_{K-1} broadcast at BFT_start (`B_k = T_commit`) with deepest-confirmed-parent fetch. Phase 2 budget = **1 BTT propagation** (recommended `Δ_2 = 1 BTT` — reflood is structurally absorbed by `B_0` via RefloodDelay) + 0 Phase 3. Recovery via in-round K-layer parallel fall-through (sequential local decryption in Phase 3, no extra BTT per layer). Backups absorb up to `B_k = T_commit` at any depth — full commit budget.
- **OBFTR** (R≥2): same K-layer onion as OBFT, plus R-round retry with re-flood, per-round independent commitments, L_C cluster-consensus signaling. **3 BTT R1** (1 BTT broadcast slack + 1 BTT Phase 2 + 1 BTT Phase 2.5 L_C + 0 Phase 3) **/ 3 BTT R2** (1 BTT re-flood + 1 BTT Phase 2 + 1 BTT L_C); total **6 BTT at R=2**. Recovers partition tails up to `R · P99` via cross-round retention (which is OBFTR's structural mesh-jitter absorber — bundles missing round r's σ-window remain usable in round r+1).
- **2abOBFT**: K-layer onion with Phase 2a (verdict broadcast) + Phase 2b (σ-or-NR commit driven by convergence rule on Phase-2a verdict pool). **5 BTT** (2 BTT Phase 1 + 2 BTT Phase 2a + **1 BTT Phase 2b** at tightened `Δ_2b = 1·BTT + ε_proc` + 0 Phase 3; ε_proc ~50ms is absorbed). Single-round only. (Phase 1 and Phase 2a remain at 2·BTT under 2abOBFT's spec sizing — Phase 2a re-flood is itself the structural jitter cushion.)

## Total time to signed output (in BTT units)

All BTT counts use the uniform "1 BTT per emission cycle" sizing — see [§Sizing convention](#sizing-convention) above.

| Protocol | Round 1 healthy | Round 2 (recovery) | Total at R-round failure |
|---|---|---|---|
| Partial-sigs on pre-agreed V (baseline) | 1 BTT | n/a (no rounds) | n/a (no recovery — fails on any V-disagreement) |
| OBFT | 3 BTT + RefloodDelay | n/a (single-round) | n/a (slot misses if R1 fails on adversarial pattern; K-layer fall-through is in-round, free) |
| OBFTR(R=2) | 3 BTT | 3 BTT | 6 BTT |
| 2abOBFT | 5 BTT + RefloodDelay | n/a (single-round) | n/a (K-layer fall-through is in-round, free) |
| QBFT-SSV | 4 BTT | 10 BTT (RT) + 4 BTT = 14 BTT | 14 BTT |
| QBFT-optimal | 4 BTT | 6 BTT (RT) + 4 BTT = 10 BTT | 16 BTT (R3 = +6 BTT RT + 4 BTT) |

K-layer fall-through (OBFT, OBFTR, 2abOBFT) is sequential local decryption in Phase 3 — no per-layer BTT cost. It recovers silent/late leaders within the same round.

**Why OBFT 3 BTT + RefloodDelay vs OBFTR 3 BTT per round.** OBFT's Phase-1 model (see [OBFT.md §Setting](OBFT.md)) lets the primary L_0 broadcast at `T_commit − B_0` with `B_0 = 2·BTT + RefloodDelay` (the reflood-aware schedule — covers 1 BTT initial propagation + 1 BTT IWANT round-trip + one full RefloodDelay-sized lazy-push cycle for mesh-flaky receivers). The Phase-2 budget post-`T_commit` is `Δ_2 = 1 BTT` (KindCommit propagation only; reflood lives in `B_k`). OBFTR uses uniform `T_commit_r − 1·BTT` broadcast slack at every round, `Δ_2 = 1·BTT` Phase 2 (KindCommit_r propagation only; mesh-jitter absorbed by cross-round retention), and a Phase 2.5 (L_C signaling). Per-round both protocols use 3·BTT of consensus exchange; OBFT additionally pays `+RefloodDelay` in its primary B_0 to absorb the lazy-push cycle within a single round, while OBFTR pays nothing for reflood inside one round but cycles through R rounds to catch tail-arriving bundles.

**Why QBFT 4 BTT > OBFT (3 BTT + RefloodDelay).** QBFT has 4 emission cycles per round (PROPOSE + PREPARE + COMMIT + post-consensus) vs OBFT's 1 emission cycle (Phase 2) plus broadcast slack. At 1 BTT per emission, QBFT's structural cost is 4 × 1 = 4 BTT; OBFT's is `B_0 + Δ_2 = (2 BTT + RefloodDelay) + 1 BTT = 3 BTT + RefloodDelay`. The OBFT structural difference (vs QBFT) is fundamental to the protocol shape (3-phase cluster-wide consensus vs onion with chained encryption); the +RefloodDelay term is OBFT's choice to absorb one gossipsub lazy-push cycle inside Phase 1 rather than fall through, which QBFT handles via its round-timer slack instead (RT − R1 = 6·BTT at QBFT-SSV BTT=200ms, RT − R1 = 2·BTT at QBFT-optimal).

**Phase 3 in OBFT family.** Counted as 0 BTT in this comparison — Phase 3 is sequential local IBE decryption + cert construction, processing-bound (`ε_3 ≈ 50ms ≈ 0.25 BTT` at Config A), not propagation-bound. Cross-protocol totals here use the "0 BTT for processing-only steps" convention; deployment-time accounting (in [OBFT.md §Application's timing table](OBFT.md#timing-budget)) lists `ε_3` as a separate ~50ms row putting "consensus complete" 0.25 BTT later than the BTT count suggests.

**QBFT post-consensus is propagation-bound, not local.** Each operator broadcasts their partial-sig and the cluster threshold-aggregates — that's a real emission cycle at 1 BTT under tightened per-emission sizing. Counted as 1 BTT in the totals above. Note this is structurally different from OBFT-family Phase 3 (local-CPU only), which is why QBFT pays this 1 BTT and OBFT family doesn't.

## Effective BFT consensus budget by start time

| BFT start time | Budget |
|---|---|
| 0ms (immediate) | 3900ms |
| 800ms | 3100ms |
| 1200ms | 2700ms |
| 1800ms | 2100ms |
| 2500ms (late MEV fetch) | 1400ms |

## Table 1 — Success modes (healthy-path completion)

Healthy completion = leader honest, all Phase-1 bundles propagate, σ-quorum reaches at L_0 in round 1. All counts at uniform "1 BTT per emission" sizing (see [§Sizing convention](#sizing-convention)). **OBFT-family cells include `+RefloodDelay` (default 700ms = SSV's gossipsub HeartbeatInterval) per the reflood-aware schedule**: OBFT total = `3·BTT + RefloodDelay`, 2abOBFT total = `5·BTT + RefloodDelay` (ε_proc ~50ms folded in). For RefloodDelay=0 (fully-meshed cluster opt-out), subtract 700ms from each OBFT/2abOBFT cell.

### Table 1a — BFT start = 0ms (immediate), budget = 3900ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1700ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | 2500ms ✓ | 1800ms ✓ | 3700ms ✓ | 2400ms ✓ |
| 1000ms | 1000ms ✓ | 3700ms ✓ | 3000ms ✓ | **5700ms ✗** | **4000ms ✗** (overshoots by 100ms) |

### Table 1b — BFT start = 800ms, budget = 3100ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1700ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | 2500ms ✓ | 1800ms ✓ | **3700ms ✗** | 2400ms ✓ |
| 1000ms | 1000ms ✓ | **3700ms ✗** | 3000ms ✓ | **5700ms ✗** | **4000ms ✗** |

### Table 1c — BFT start = 1200ms, budget = 2700ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1700ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | 2500ms ✓ | 1800ms ✓ | **3700ms ✗** | 2400ms ✓ |
| 1000ms | 1000ms ✓ | **3700ms ✗** | **3000ms ✗** | **5700ms ✗** | **4000ms ✗** |

### Table 1d — BFT start = 1800ms, budget = 2100ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1700ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | **2500ms ✗** | 1800ms ✓ | **3700ms ✗** | **2400ms ✗** |
| 1000ms | 1000ms ✓ | **3700ms ✗** | **3000ms ✗** | **5700ms ✗** | **4000ms ✗** |

### Table 1e — BFT start = 2500ms (late MEV fetch), budget = 1400ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (Phase 2a/2b split) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ tight | 600ms ✓ | **1700ms ✗** | 800ms ✓ |
| 600ms | 600ms ✓ | **2500ms ✗** | **1800ms ✗** | **3700ms ✗** | **2400ms ✗** |
| 1000ms | 1000ms ✓ tight | **3700ms ✗** | **3000ms ✗** | **5700ms ✗** | **4000ms ✗** |

† **Partial-sigs only fits if V is pre-agreed.** For SSV proposer duty (V varies per operator due to MEV bundles), this isn't directly applicable — the cluster needs a BFT consensus protocol to resolve V-disagreement first. Shown here as the floor: what completion would look like if V were pre-agreed (e.g., for non-MEV duties like attestations).

**Reading Tables 1a–1e** (default RefloodDelay=700ms framing; at RefloodDelay=0 the OBFT-family cells shift down by 700ms):

- **Partial-sigs floor (V pre-agreed)**: 1 BTT fits at every BFT_start across the BTT envelope. Sets the absolute floor — BFT consensus protocols pay 2-4 BTT extra to resolve V-disagreement (the OBFT-family additionally absorbs one gossipsub reflood cycle inside Phase 1, accounting for the +RefloodDelay term).
- **BTT=200ms** (production-typical healthy mesh): every protocol fits at every BFT_start except 2abOBFT (1700ms) at BFT_start = 2500ms (budget 1400ms). At BFT_start = 2500ms (late MEV fetch), the rest fit comfortably — OBFTR R1 (600ms) and QBFT R1 (800ms) have the most slack; OBFT (1300ms) is tight; 2abOBFT just misses.
- **BTT=600ms** (degraded mesh): OBFTR R1 (1800ms) and QBFT R1 (2400ms) fit BFT_start ≤ 1200ms; OBFT (2500ms) fits BFT_start ≤ 1200ms; 2abOBFT (3700ms) fits only BFT_start = 0. At BFT_start ≥ 1800ms only Partial-sigs fits.
- **BTT=1000ms** (severely degraded): OBFTR R1 (3000ms) and OBFT (3700ms) fit BFT_start = 0; QBFT R1 (4000ms) overshoots by 100ms even at BFT_start=0. 2abOBFT misses everywhere. All other protocols miss everywhere beyond BFT_start=0.
- **Healthy-path ordering at default RefloodDelay**: Partial-sigs (1 BTT) < OBFT-without-RefloodDelay (3 BTT) < OBFTR R1 (3 BTT) ≈ OBFT-without-RefloodDelay < QBFT R1 (4 BTT) < 2abOBFT-without-RefloodDelay (5 BTT) < OBFT (3·BTT + RefloodDelay ≈ 6.5·BTT at default) < 2abOBFT (5·BTT + RefloodDelay ≈ 8.5·BTT at default). At RefloodDelay=0: Partial-sigs (1) < OBFT (3) ≈ OBFTR R1 (3) < QBFT R1 (4) < 2abOBFT (5). The +RefloodDelay cost is OBFT/2abOBFT's choice to absorb the lazy-push cycle inside Phase 1; OBFTR and QBFT absorb it differently (cross-round retention / round-timer slack).

## Table 2 — Failure-recovery modes

When round-1 / single-round fails (silent leader, partition, network jitter, but NOT adversarial-byz-locked patterns covered in Table 3), each protocol's recovery path consumes additional time. All counts at uniform "1 BTT per emission" sizing.

### Table 2a — BFT start = 0ms, budget = 3900ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | 2800ms ✓ | 2000ms ✓ |
| 600ms | n/a | in-round (free) | 3600ms ✓ | in-round (free) | **4400ms ✗** | **6000ms ✗** |
| 1000ms | n/a | in-round (free) | **6000ms ✗** | n/a (R1 missed at default RefloodDelay) | **6000ms ✗** | **10000ms ✗** |

### Table 2b — BFT start = 800ms, budget = 3100ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | 2800ms ✓ | 2000ms ✓ |
| 600ms | n/a | in-round (free) | **3600ms ✗** | n/a (R1 missed) | **4400ms ✗** | **6000ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **10000ms ✗** |

### Table 2c — BFT start = 1200ms, budget = 2700ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | **2800ms ✗** | 2000ms ✓ |
| 600ms | n/a | in-round (free) | **3600ms ✗** | n/a (R1 missed) | **4400ms ✗** | **6000ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **10000ms ✗** |

### Table 2d — BFT start = 1800ms, budget = 2100ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | **2800ms ✗** | 2000ms ✓ |
| 600ms | n/a | n/a (R1 missed at default RefloodDelay) | **3600ms ✗** | n/a (R1 missed) | **4400ms ✗** | **6000ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **10000ms ✗** |

### Table 2e — BFT start = 2500ms (late MEV fetch), budget = 1400ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-optimal R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) tight | 1200ms ✓ tight | n/a (R1 missed) | **2800ms ✗** | **2000ms ✗** |
| 600ms | n/a | n/a (R1 missed) | **3600ms ✗** | n/a (R1 missed) | **4400ms ✗** | **6000ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **10000ms ✗** |

† **Partial-sigs has no failure-recovery mechanism**: any V-disagreement (operators sign different V's) results in cluster signature aggregation failing — no rounds, no re-flood, no fall-through. The baseline only works on the healthy V-pre-agreed path.

"In-round (free)" means the recovery happens within the same single round — no additional time cost. K-layer fall-through is sequential local decryption in Phase 3, processing-bound (~50ms ε_3 single-layer; ~200ms ε_3 × K at K=4 with K−1 silent layers), not BTT-bound.

**Reading Tables 2a–2e:**

- **OBFT and 2abOBFT have the cleanest network-failure recovery profile** at any start time where their healthy path fits — silent leader / partition recovery costs zero extra time via in-round K-layer fall-through. This is the structural advantage of K-layer onion with chained encryption: every honest leader in the K-layer rotation provides a fall-through opportunity within Phase 3.
- **OBFTR's R1+R2 retry** (6 BTT = 1200ms at BTT=200ms) fits at BFT_start ≤ 2500ms (1200ms ≤ 1400ms budget); fits at every start time at BTT=200ms. At BTT=600ms, R1+R2 (3600ms) fits at BFT_start = 0 only. At BTT=1000ms, R1+R2 (6000ms) misses everywhere. Under the tightened per-emission sizing OBFTR's retry envelope widens substantially compared to the older 2·BTT/emission framing.
- **QBFT-SSV R2** (RT=2000ms + 4 BTT = 2800ms at BTT=200ms) fits at BFT_start ≤ 800ms (2800ms ≤ 3100ms by 300ms margin); just misses at BFT_start = 1200ms (overshoots 2700ms budget by 100ms). At BTT ≥ 600ms R2 misses everywhere.
- **QBFT-optimal R2** (RT=6 BTT + 4 BTT = 2000ms at BTT=200ms) fits at BFT_start ≤ 1800ms (2000ms ≤ 2100ms budget, 100ms margin); misses only at BFT_start=2500ms. Tighter RT recovers more budget vs QBFT-SSV. **R3** (= `2×RT + 4 BTT = 16 BTT = 3200ms` at BTT=200ms) fits at BFT_start = 0 only (3200ms ≤ 3900ms with 700ms margin); overshoots the 3100ms budget at BFT_start = 800ms by 100ms. Under tightened per-emission, R3 becomes a meaningful retry tier at BFT_start = 0 — a new property vs the older 2·BTT/emission framing where R3 missed everywhere.
- **OBFT and 2abOBFT cannot retry** (single-round). Their "recovery" is the in-round fall-through; if that doesn't reach σ-quorum (e.g., adversarial pattern locks the σ-or-NR pools — see Table 3), the slot misses.
- **Structural retry advantage** (round-2 with fresh-V refetch) belongs to QBFT-optimal: available at BFT_start ≤ 1800ms with BTT=200ms (vs ≤ 800ms under the older sizing). The OBFT family's K-layer in-round fall-through is structurally cheaper — it doesn't consume additional BTT and recovers silent leaders for free.

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

‡ **QBFT-SSV and QBFT-optimal share Table 3 cells** (same protocol, different RT). Difference between variants: how many rounds fit the slot budget — QBFT-SSV fits R2 at BFT_start ≤ 800ms with BTT=200ms; QBFT-optimal fits R2 at BFT_start ≤ 1800ms with BTT=200ms. Beyond those cells, "✓ via R2" recovery doesn't fit budget regardless of structural availability.

**Reading Table 3:**

- **QBFT recovers more adversarial-byz patterns** structurally than the OBFT family, but recovery only materializes when R2 fits budget. Under tightened per-emission sizing this means BTT=200ms with BFT_start ≤ 1800ms (QBFT-optimal) or BFT_start ≤ 800ms (QBFT-SSV) — meaningfully wider envelopes than under the older 2·BTT/emission framing. At BTT ≥ 600ms or BFT_start = 2500ms, the structural QBFT-recovery advantage doesn't fit budget for any variant.
- **OBFT family avoids QBFT's structural disadvantage** at multi-leader-silent (K-1 ≥ 3) patterns: QBFT requires K serial round-changes (each ~RT + 4 BTT under tightened sizing), exceeding the 4000ms budget at any K-1 ≥ 3. OBFT/OBFTR/2abOBFT recover within a single round via K-layer fall-through (free in Phase 3).
- **2abOBFT's convergence-rule recoveries** (1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness) close the patterns that bare OBFT/OBFTR leave as Class B exposures, at the cost of two narrower regressions (2-1-byz-defect, verdict-equivocation) — both slashable, both R-invariant.
- **Bare OBFT and OBFTR succeed at 2-1-byz-defect** that 2abOBFT misses, because the leader's Phase-1 σ_V crypto-locks the σ-pool against post-σ defection. 2abOBFT removes Phase-1 σ_V (Variant C) to gain validity-divergence recovery; pays the regression as the structural cost.

## Table 4 — MEV-fetch budget by protocol (BTT=200ms)

At the SSV proposer-duty operating point — `BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`, `RANDAO_done ≈ 150ms` (see [OBFT.md §Application](OBFT.md#timing-budget) for the full derivation) — each protocol's leader has a different MEV-relay-fetch budget bounded by when its broadcast must complete. The fetch budget is the wall-clock from `RANDAO_done` to the leader's broadcast deadline. All counts at uniform "1 BTT per emission" sizing.

**OBFT (K=4)** — primary-vs-backup broadcast at `T_broadcast_max_k = max(0, T_commit − B_k)` with `B_0 = 2·BTT + RefloodDelay` (primary, MEV-fresh) and `B_1..B_{K-1} = T_commit` (backups broadcast at BFT_start with deepest-confirmed-parent fetch) — see [OBFT.md §Setting](OBFT.md#setting). `T_commit = 3600ms` post-tighten. SSV production defaults to `RefloodDelay = 700ms` (gossipsub HeartbeatInterval); fully-meshed clusters may opt out by setting RefloodDelay near zero:

| Leader | Broadcast (RefloodDelay=700ms default) | MEV-fetch (default) | Broadcast (RefloodDelay=0 opt-out) | MEV-fetch (RefloodDelay=0) |
|---|---|---|---|---|
| V_0 (primary) | 2500ms | **~2350ms** | 3200ms | **3050ms** |
| V_1, V_2, V_3 (backups) | 0ms (slot start) | ~0ms | 0ms (slot start) | ~0ms |

**Partial-sigs on pre-agreed V (baseline)** — V agreed externally; consensus = 1 BTT (one emission for partial-sig propagation + threshold aggregation). Broadcast deadline = `Relay_cutoff − 100ms − 1 BTT = 3700ms`:

| Step | Time | MEV-fetch budget |
|---|---|---|
| V determined (must be cluster-agreed) | ≤ 3700ms | **3550ms** |

**QBFT-SSV (RT=2000ms, 2-round target)** — single leader per round; fetch must complete before each round's PROPOSE. R1 PROPOSE deadline derived from R2 fit constraint: `PROPOSE_R1 + RT + 4 BTT + 100ms ≤ 4000ms` → `PROPOSE_R1 ≤ 1100ms`:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 1100ms | **950ms** |
| R2 | 3100ms | 2950ms |

**QBFT-optimal (RT=6 BTT, 2-round target)** — tighter RT lets PROPOSE_R1 fire later. `PROPOSE_R1 + RT + 4 BTT + 100ms ≤ 4000ms` → `PROPOSE_R1 ≤ 1900ms`:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 1900ms | **1750ms** |
| R2 | 3100ms | 2950ms |

(QBFT-optimal R3 fits at BFT_start=0 only — `2 × RT + 4 BTT + 100ms = 16·BTT = 3200ms ≤ 3900ms`, with 700ms slack. R-round target may extend to R3 under tightened sizing at BFT_start = 0.)

**Cross-protocol ranking** (uniform 1·BTT-per-emission sizing throughout; OBFT figures shown at both RefloodDelay settings):

| Rank | Leader | MEV-fetch (RefloodDelay=700ms default) | MEV-fetch (RefloodDelay=0 opt-out) | Notes |
|---|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3550ms** | 3550ms | Floor: only available if V is pre-agreed (no MEV / no V-disagreement) |
| 2 | QBFT R2 leader | 2950ms | 2950ms | Only reachable after R1 fails (paying the ~2s round-change cost) |
| 3 | OBFT V_0 | **~2350ms** | **3050ms** | The only OBFT layer competing on MEV; primary always tried first |
| 4 | QBFT-optimal R1 | 1750ms | 1750ms | |
| 5 | QBFT-SSV R1 | 950ms | 950ms | SSV's wide RT shrinks R1 fetch window; still meaningfully larger under tightened sizing than the old 150ms |
| 6 (last) | OBFT V_1, V_2, V_3 (backups) | ~0ms | ~0ms | Backups all broadcast at BFT_start with deepest-confirmed-parent fetch — safety nets, not MEV-fresh alternatives |

† **Partial-sigs is not directly comparable** for SSV proposer duty (V varies per operator). Shown as the no-consensus floor — what would be possible if V didn't need cluster-wide agreement.

**Reading:**

- **OBFT V_0 vs QBFT R2 (post-tighten reranking)**: under the older 2·BTT/emission framing, OBFT V_0 led QBFT R2 (~2350ms vs 2150ms at default RefloodDelay). Under tightened 1·BTT/emission, QBFT R2 now beats OBFT V_0 at default RefloodDelay (2950ms vs ~2350ms = 600ms ahead) — but OBFT V_0 is the *primary* leader (always tried first; no round-timeout gap), while QBFT R2 is reachable only after R1 fails (paying the ~2s round-change cost). At RefloodDelay=0 (fully-meshed opt-out) OBFT V_0 still lags QBFT R2 by 100ms (3050 vs 2950, near-tie). The structural tradeoff hasn't changed; the headline numbers just compress under uniform sizing.
- **OBFT V_0 pays a 500ms–1200ms BFT-consensus tax over the partial-sigs floor** (depending on RefloodDelay): 3550 − 3050 = 500ms (2.5·BTT) at RefloodDelay=0, 3550 − ~2350 = ~1200ms (~6·BTT) at default RefloodDelay. The tax decomposes as `B_0 + Δ_2 − partial-sigs post-fetch overhead = (2·BTT + RefloodDelay) + 1·BTT − 1.5·BTT`. The 2·BTT shallow base covers 1·BTT P99 leader-broadcast propagation + 1·BTT IWANT round-trip; RefloodDelay covers one full IHAVE/IWANT cycle for mesh-flaky receivers. OBFT runs at tightened `Δ_2 = 1 BTT`; partial-sigs floor reserves 1·BTT emit + 0.5·BTT (100ms) submit = 1.5·BTT post-fetch.
- **QBFT-SSV R1 widens to 950ms MEV-fetch under tightened sizing** (was 150ms under the older sizing) — R1 shrinks from 8·BTT to 4·BTT, freeing 800ms of the slot budget back to the R1 leader. QBFT-optimal recovers another 800ms by tightening RT to 6 BTT, reaching 1750ms MEV-fetch at R1. QBFT's RT framing is RefloodDelay-independent.
- **OBFT backups trade all MEV-fetch for full-slot absorption**: under the simplified backup schedule, all backups V_1..V_{K-1} fetch at slot start (deepest-confirmed parents — re-org resistant) and broadcast immediately, giving the cluster the entire `T_commit` budget for that bundle's propagation. OBFT's K-layer fall-through is in-round (sequential local IBE decryption, no per-layer RTT) — fall-through is reliable and free, just MEV-poor.

**OBFTR(R=2) and 2abOBFT primary-leader fetch budgets** at BTT=200ms (uniform 1·BTT-per-emission sizing). 2abOBFT shares OBFT's reflood-aware schedule `B_k_shallow = (k+2)·BTT + RefloodDelay` but anchors broadcast at `T_verdict_start = T_commit − Δ_2a ≈ 3.20s` (Δ_2a = 400ms of pre-Phase-2a budget vs bare OBFT's T_commit-anchored broadcast). OBFTR(R=2) totals are R-round-summed under tightened per-round 3·BTT (1·BTT slack + 1·BTT Phase 2 + 1·BTT Phase 2.5).

| Protocol | Total BTT | Broadcast (RefloodDelay=700ms default) | MEV-fetch (default) | Broadcast (RefloodDelay=0) | MEV-fetch (RefloodDelay=0) |
|---|---|---|---|---|---|
| OBFTR(R=2) (R1+R2 fit) | 6 BTT | ~2650ms | ~2500ms | ~2650ms | ~2500ms |
| OBFTR(R=2) (R1-only) | 3 BTT | ~3250ms | ~3100ms | ~3250ms | ~3100ms |
| 2abOBFT V_0 (primary) | 5 BTT + RefloodDelay | ~2100ms | ~1950ms | ~2800ms | ~2650ms |
| 2abOBFT V_1 | 5 BTT + RefloodDelay | ~1900ms | ~1750ms | ~2600ms | ~2450ms |
| 2abOBFT V_2 | 5 BTT + RefloodDelay | ~1700ms | ~1550ms | ~2400ms | ~2250ms |
| 2abOBFT V_3 (deepest) | 5 BTT + RefloodDelay | 0ms (slot start) | ~0ms (deepest-confirmed parent) | 0ms | ~0ms |

OBFTR's broadcast deadlines are anchored on R-round completion (not B_k); the schedule's reflood-aware framing doesn't apply the same way because cross-round retention already provides multi-round absorption. 2abOBFT's V_0 MEV-fetch is tighter than bare OBFT's V_0 at the same RefloodDelay setting because of the +Δ_2a anchor shift (Phase-2a window cost = 400ms = 2·BTT) — see the cross-protocol-ranking discussion above for the trade-off.

2abOBFT mirrors OBFT's per-layer staggered schedule (`B_k_shallow = (k+2)·BTT + RefloodDelay`; `B_{K-1} = T_verdict_start`); per-layer broadcast targets are `max(0, T_verdict_start − B_k)` where `T_verdict_start = T_commit − Δ_2a`. Phase-2a re-flood absorption applies *uniformly* across all layers on top of per-layer `B_k` (see [2abOBFT.md §Setting](2abOBFT.md#setting) for the composition). At the same RefloodDelay setting, OBFT's primary `B_0` matches 2abOBFT's `B_0`; OBFT's backups have wider per-layer slack (B_k = T_commit) than 2abOBFT's staggered backups, while 2abOBFT compensates via the Phase-2a re-flood window absorbing late bundles uniformly across layers, in exchange for additional structural recovery (convergence-rule adversarial-byz recoveries). OBFTR pays the equivalent cost via extended partition tail absorption via cross-round retention.

The **MEV-fetch budget asymmetry is a structural OBFT-family advantage over QBFT-SSV R1 for the primary leader** — OBFT V_0 has ~2.5× the MEV-fresh fetch time of QBFT-SSV R1 leader (~2350ms vs 950ms at default RefloodDelay). Against QBFT-optimal R1 (1750ms) OBFT V_0 is closer (~600ms ahead at default RefloodDelay; ~1300ms ahead at RefloodDelay=0). Against QBFT R2 (2950ms), OBFT V_0 trails by ~600ms at default RefloodDelay (or ~100ms at RefloodDelay=0), but OBFT V_0 is the *primary* leader (always tried first; no round-timeout gap), while QBFT R2 is reachable only after R1 fails (paying the ~2s round-change cost). (Note: OBFT's backups V_1..V_{K-1} are not MEV-fresh — they're last-resort safety nets that broadcast at BFT_start with deepest-confirmed-parent fetch; the OBFT vs QBFT MEV comparison is V_0 vs QBFT-{SSV,optimal} R1 / R2 only.)

## Cross-scenario takeaways

**Partial-sigs floor (V pre-agreed)**: 1 BTT = 200ms total at tightened per-emission sizing. Fits at every (BFT_start, BTT) cell. Sets the floor: BFT-consensus protocols pay 2-4 BTT extra to resolve V-disagreement. For SSV proposer duty (V varies per operator due to MEV bundles), partial-sigs alone is not directly applicable — used here as a reference for the BFT-consensus tax.

**Healthy-path latency at production-typical BTT (200ms)** *(BTT-count only; add +RefloodDelay for OBFT-family totals)*: partial-sigs 200ms, OBFT 600ms, OBFTR-R1 600ms, QBFT R1 800ms, 2abOBFT 1000ms. Every protocol fits at every BFT_start at BTT=200ms except 2abOBFT (at default RefloodDelay) at BFT_start = 2500ms (Tables 1a-1e, BTT=200ms).

**Late-fetch tolerance (BFT start = 2500ms, budget = 1400ms)**: at BTT=200ms, partial-sigs (200ms), OBFTR R1 (600ms), and QBFT R1 (800ms) fit comfortably; OBFT (600ms BTT-count, or 1300ms incl. default RefloodDelay) is tight (100ms margin at default RD); 2abOBFT (1000ms BTT-count, or 1700ms incl. default RefloodDelay) misses at default RefloodDelay by 300ms but fits at RD=0. At BTT ≥ 600ms, all consensus protocols miss — late-fetch is incompatible with degraded mesh.

**Degraded-mesh tolerance (BTT = 1000ms)**: at BFT_start = 0 only — OBFT (3000ms BTT-count, or 3700ms incl. default RefloodDelay) and OBFTR R1 (3000ms) fit; QBFT R1 (4000ms) overshoots by 100ms; 2abOBFT (5000ms BTT-count, or 5700ms incl. default RefloodDelay) misses. Beyond BFT_start = 0, all consensus protocols miss.

**Mid-BTT tolerance (BTT = 600ms)**: bare OBFT (1800ms BTT-count, or 2500ms incl. default RefloodDelay) fits at BFT_start ≤ 1200ms; OBFTR R1 (1800ms) fits at BFT_start ≤ 1800ms; QBFT R1 (2400ms) fits at BFT_start ≤ 1200ms; 2abOBFT (3000ms BTT-count, or 3700ms incl. default RefloodDelay) fits only at BFT_start = 0 at default RD. All consensus protocols miss at BFT_start = 2500ms.

**Round-2 retry usefulness**: under tightened per-emission sizing — **OBFTR's R1+R2 (6 BTT = 1200ms at BTT=200ms) fits at every BFT_start** (1200ms ≤ 1400ms minimum budget). **QBFT-SSV R2 (RT + 4 BTT = 14 BTT = 2800ms at BTT=200ms) fits at BFT_start ≤ 800ms** (2800ms ≤ 3100ms budget); misses at BFT_start ≥ 1200ms. **QBFT-optimal R2 (RT + 4 BTT = 10 BTT = 2000ms at BTT=200ms) fits at BFT_start ≤ 1800ms** (2000ms ≤ 2100ms budget); misses at BFT_start = 2500ms. At BTT ≥ 600ms, only OBFTR R1+R2 (3600ms) fits at BFT_start = 0. **QBFT-optimal R3 (16 BTT = 3200ms at BTT=200ms) fits at BFT_start = 0 only** (3200ms ≤ 3900ms with 700ms margin); overshoots the 3100ms budget at BFT_start = 800ms by 100ms. The OBFT family's K-layer in-round fall-through is structurally cheaper than retry — it doesn't consume additional BTT and recovers silent leaders for free.

**Adversarial-byz exposure ranking** (most-recovered to least-recovered):

1. **2abOBFT + R-round retry** (hypothetical): closes most patterns including 2-1-byz-defect, but doesn't exist as a specified protocol.
2. **Bare 2abOBFT**: closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness via convergence rule. Misses 2-1-byz-defect and verdict-equivocation.
3. **QBFT (with R2 budget)**: closes adversarial-byz via fresh-V refetch on round-change. Bound by RT + 4 BTT fitting within budget — under tightened per-emission sizing, available at BFT_start ≤ 800ms with BTT=200ms for QBFT-SSV; BFT_start ≤ 1800ms with BTT=200ms for QBFT-optimal.
4. **Bare OBFT**: closes silent-leader / partition cases via K-layer fall-through, plus h_V=1 selective Phase-1 delivery via §Phase 2 peer-reflood V. **Does NOT close**: 1-1-1 σ-locked equivocation, validity-divergence boundary splits — these remain R-invariant Class B grief deterred via Assumption 4 (rational-byzantine deterrent + planned blacklist + staker migration).
5. **Bare OBFTR**: closes silent-leader / partition cases via K-layer fall-through (single round) and partition-tail cases via R-round retry. Adversarial patterns (1-1-1, h_V=1, etc.) are R-invariant slot-misses; rational-byzantine deterrent absorbs across slots.

**Multi-leader-silent advantage**: OBFT family (OBFT, OBFTR, 2abOBFT) all complete at K-1 ≥ 3 silent within a single round via Phase-3 reconstruction walk. QBFT cannot — serial round-changes at RT=2000ms (or RT=6 BTT for optimal) exceed the 4000ms budget at any K-1 ≥ 3. Structural OBFT-family advantage at any (BFT_start, BTT) combination where the healthy path fits.

**Choosing a protocol** (deployment guidance):

- **Pre-agreed V (no consensus needed)**: partial-sigs floor at 1 BTT = 200ms. Use for SSV duties where V is deterministic (attestations, sync committee). Not applicable to MEV proposer duty since V varies per operator.
- **Healthy-path latency-critical (with consensus)**: OBFT and OBFTR-R1 at BTT=200ms (600ms BTT-count completion each) — tied as best in family at the tightened sizing. QBFT R1 800ms; 2abOBFT 1000ms.
- **Late-fetch / high-MEV proposer duty (BFT start ≥ 2000ms)**: at BTT=200ms — OBFTR-R1 (600ms) and QBFT R1 (800ms) fit comfortably; OBFT (600ms BTT-count, 1300ms incl. default RefloodDelay) tight at 2500ms start; 2abOBFT (1000ms BTT-count, 1700ms incl. default RefloodDelay) misses at default RefloodDelay.
- **Adversarial-byz robustness within single round**: 2abOBFT — closes 1-1-1 equivocation, h_V=1, validity-majority, mesh-flakiness without round-2 budget cost. Bare OBFT also closes h_V=1 in-protocol via §Phase 2 peer-reflood V.
- **Multi-round partition tail absorption**: OBFTR(R=2) — under tightened sizing, R1+R2 (6 BTT = 1200ms at BTT=200ms) fits at every BFT_start. Significantly more attractive than under the older 2·BTT/emission framing.
- **QBFT-SSV (current SSV)**: production-mature; under tightened per-emission sizing, R1 fits at every BFT_start at BTT=200ms; R2 fits at BFT_start ≤ 800ms. Misses at BTT ≥ 600ms beyond BFT_start ≤ 1200ms.
- **QBFT-optimal**: hypothetical reference point — same R1 timing as QBFT-SSV but tighter RT lets R2 fit at BFT_start ≤ 1800ms with BTT=200ms. R3 fits at BFT_start = 0 only. Not what SSV runs in production.

## OBFT + L_Bid mini-consensus extension

OBFT + L_Bid (specified in [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)) is an opportunistic bid-routing extension to bare OBFT. It prepends a bid-determined L_Bid layer above OBFT's K rotation-determined layers (yielding `K' = K + 1`) and adds a mini-consensus sub-phase between `T_0_arrival` and `T_commit` that resolves L_Bid identity cluster-wide before σ-commitment. This section identifies scenarios where OBFT+L_Bid's behavior differs from bare OBFT and from the other three protocols. **Most scenarios are identical between bare OBFT and OBFT+L_Bid**; the differences are surfaced below.

### Differences vs bare OBFT (summary)

- **+1 BTT total consensus time** at conservative `Δ_minicon = 2·BTT` (was +2·BTT under the older 2·BTT-broadcast-slack framing), all in pre-`T_commit` budget: OBFT+L_Bid is **4 BTT** (1 BTT broadcast slack + 2 BTT mini-consensus + 1 BTT Phase 2 + 0 Phase 3) vs bare OBFT's 3 BTT under uniform 1·BTT-per-emission sizing. `T_commit` is back-end-anchored and unchanged from bare OBFT; the 2 BTT mini-consensus runs as a sub-phase at the tail of Phase 1, so the cost falls on the L_0..L_{K-1} broadcast deadlines (MEV-fetch budget shrinks by `Δ_minicon`), not on post-`T_commit` slack.
- **Value capture upside**: highest-bid eligible rotation-layer block on the healthy path (when L_Bid σ-quorum reaches) instead of fixed rotation-priority V.
- **New failure modes at L_Bid**: 2-1-byz-defect (mixed evidence quality — cryptographic Rules 7/8 for some triggers/actions, behavioral for silent variants) and verdict-equivocation (cryptographic Rule 8); both slot-miss-without-fall-through to L_0.
- **L_0..L_{K-1} rotation layers are unchanged**: when the mini-consensus fails to converge the cluster falls through to L_0 with the same recovery profile as bare OBFT. C1/C2 closure is conditional — see [Adversarial-byz failure modes](#adversarial-byz-failure-modes-specific-to-l_bid--table-3-delta) below.

### Where OBFT+L_Bid's outcome differs from bare OBFT

#### Success-mode delta — Table 1

Four scenarios show different success outcomes between bare OBFT and OBFT+L_Bid (at recommended Δ sizing). In all other (BFT_start, BTT) combinations, both protocols complete healthy or both miss healthy. The full-protocol comparison at the differing scenarios:

| Scenario | Budget | QBFT R1 | Bare OBFT | **OBFT+L_Bid** | OBFTR R1 | 2abOBFT |
|---|---|---|---|---|---|---|
| **0ms, BTT=1000ms** | 3900ms | **4000ms ✗** | 3000ms ✓ | **4000ms ✗** | 3000ms ✓ | **5000ms ✗** |
| **800ms, BTT=1000ms** | 3100ms | **4000ms ✗** | **3000ms ✗** | **4000ms ✗** | 3000ms ✓ tight | **5000ms ✗** |
| **1200ms, BTT=600ms** | 2700ms | 2400ms ✓ | 1800ms ✓ | 2400ms ✓ | 1800ms ✓ | **3000ms ✗** |
| **1800ms, BTT=600ms** | 2100ms | **2400ms ✗** | **1800ms ✗** | **2400ms ✗** | 1800ms ✓ | **3000ms ✗** |

Under tightened per-emission sizing, OBFT+L_Bid (4 BTT) loses bare OBFT's healthy-path advantage at BFT_start ≥ 800ms with BTT=1000ms (where bare OBFT just fits at 3000ms but OBFT+L_Bid overshoots at 4000ms). At BTT=200ms (every BFT_start) and at most other BTT × BFT_start cells the two fit equally; OBFTR R1 widens its envelope significantly post-tighten and is the most-tolerant choice at degraded-mesh × late-start cells.

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
- **+1 BTT total consensus time** at conservative `Δ_minicon = 2·BTT` (in pre-`T_commit` budget — MEV-fetch reduction; post-`T_commit` matches bare OBFT). Loses bare OBFT's advantage at the (BFT_start, BTT) cells listed in the success-mode delta table above (degraded-mesh × moderate-or-later starts) where bare OBFT fits and OBFT+L_Bid doesn't; all other scenarios are unaffected at the budget-fit level.
- **+adversarial-byz exposure at L_Bid** (2-1-byz-defect with mixed evidence quality, verdict-equivocation cryptographic; slot-miss without fall-through; higher trigger frequency than rotation-only patterns).
- **+structural complexity** (`Phase1Bundle` bid metadata, new `KindBidVerdict`, two new slashing rules, mini-consensus protocol step).

In exchange for:
- **Bid-routing value capture** on healthy path (highest-bid eligible rotation-layer block vs fixed rotation-priority V).
- **C1/C2/C3 conditional closure at L_Bid** (vs the naive bid-routing sketch which leaves these open). C1/C2 close when verdict-quorum doesn't form; residuals fold into 2-1-byz-defect rather than deadlock without attribution.

The trade is favorable when MEV bid-routing value-capture upside exceeds the combined cost of (a) the new failure modes' slot-loss rate and (b) the +1 BTT MEV-fetch budget reduction (pre-`T_commit`, at conservative Δ_minicon). For low-MEV slots or deployments with significant mesh degradation pushing scenarios toward the (BFT_start ≥ 800ms, BTT=1000ms) borderline, bare OBFT is the better choice.

## Limits of this comparison

- **Numbers are BTT-count approximations** (3 BTT, 4 BTT, etc.). Production has long tails; ε_3 (~50ms local processing per layer) is treated as small relative to BTT in tabulation. Real implementations may add 50-200ms of constant overhead per round.
- **QBFT round timeout RT = 2000ms** is held fixed; tightening RT shrinks recovery time but raises false-positive round-changes under jitter.
- **K = n = 4** assumed. At larger n with the same f-bound, K-layer fall-through depth scales (more redundancy at the OBFT family). QBFT's recovery cost scales linearly with K serial round-changes.
- **Bandwidth (small `V`, e.g. attestations ~100 B; cluster-wide healthy path)**: QBFT ~14 KB across 4 emissions per round; OBFT ~28 KB across 1 emission (includes the `sigma_L_witnesses` section ≈ +2.3 KB at K=4 n=4); OBFTR ~28-30 KB across 2 emissions per round (R=2 worst case ~52 KB across 4 emissions); 2abOBFT ~30 KB across 2 emissions (no σ_L^V witness; 2abOBFT has no Phase-1 σ_L^V). All four +3-5 KB if L_Bid mini-consensus extension is used (see [each doc's Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)). Bandwidth is per-emission-count, not per-BTT, so the per-emission sizing tightening does not change these numbers.
- **Bandwidth at larger V**: OBFT's σ_L^V witness section scales with the σ partial size (~145 B/witness), not with V's payload — so OBFT's bandwidth stays close to the small-`V` baseline regardless of V size. Few-large emissions (OBFT) are gentler on the gossipsub mesh than many-small (QBFT) at SSV's KB-range message sizes — per-message overhead (signature verify, dedup, peer-score, mesh forwarding) dominates over per-byte cost.
- **Pre-consensus / block-fetch overhead** is excluded — sits in `[slot_start, BFT_start]` and is ~equal across protocols.
- **Partial network partitions** (some operators have a quorum view, others don't) aren't separately modeled. All four protocols degrade to slot-miss for the partitioned operators; cluster-wide outcome depends on which side has 2f+1 honest.
- **Adversarial-byz trigger frequency** is not modeled. Practical impact depends on byz-leader rotation distribution and bid-equivocation surface for L_Bid extensions (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) for L_Bid-specific exposure analysis).

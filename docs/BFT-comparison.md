# QBFT vs OBFT vs OBFTR vs 2abOBFT — comparison for SSV proposer duty

This doc compares SSV's existing consensus protocol (QBFT) against the three Onion BFT family proposals — [OBFT](OBFT.md) (single-round), [OBFTR](OBFTR.md) (multi-round, R≥2), and [2abOBFT](2abOBFT.md) (witness-bound, Phase-2 split, single-round) — across the SSV proposer-duty operating envelope. The application is held fixed: 12000ms Ethereum slot, 4000ms relay submission cutoff. Numbers reflect time-to-signed-output (full BLS signature on the agreed value, ready for downstream submission).

The comparison is structured along three axes:

- **BFT start time within the slot**: 0ms (immediate, BFT begins at slot start), 800ms, 1200ms, 1800ms, 2500ms (late MEV fetch). Spans the practical range — 0ms is non-MEV duty or pre-fetched MEV; 2500ms is late MEV-fetch maximizing relay-bundle freshness.
- **`BTT` operating point** (broadcast trip time, `BTT = P99 + δ`): 200ms (production-typical healthy mesh), 600ms (degraded), 1000ms (severely degraded). See [Scope and assumptions](#scope-and-assumptions) for the full BTT definition.
- **Mode**: success modes (healthy path: when does the protocol complete?) and failure modes (recovery paths when round-1 / single-round fails, and adversarial-byz exposure).

5 × 3 × 2 = 30 comparison cells, presented across three tables: success-mode completion, failure-recovery completion, and structural failure-mode recoverability (the last is scenario-independent — it depends on protocol structure, not on BTT or start time).

## Scope and assumptions

- **Cluster**: `n = 4`, `f = 1`. **Recommended defaults per protocol** (post-K=f+1 BFT-min flip for the bare-OBFT and 2abOBFT families): bare OBFT defaults `K = 2` (= K=f+1, BFT-liveness minimum); 2abOBFT defaults `K = 2` (same rationale); OBFTR keeps its own preferred default `K = 3` because its R-round retry substitutes for one layer of K-fall-through (see [OBFTR.md §Application](OBFTR.md#application-ssv-ethereum-proposer-duty)); QBFT has no K-parameter. Cross-protocol comparison tables below use each protocol's recommended default unless otherwise noted; K=4 worked examples appear inline as up-tier illustrations of multi-layer fall-through (relevant for higher-`n` deployments where K = f+1 grows above 2). Algebra generalizes to higher `n` at the f-bound.
- **Clock skew δ = 50ms**, included in `BTT` (see below).
- **Time unit `BTT` (broadcast trip time)** = `P99 + δ` — one one-way broadcast trip under partial-synchrony assumptions. `P99` is the propagation budget at the deployment's chosen tail percentile (P99, P999, P9999, etc. — deployment knob). Operating points used in tables below: `BTT = 200ms` (P99 ≈ 150ms + δ ≈ 50ms; production-typical), `BTT = 600ms` (P99 ≈ 550ms + δ; degraded), `BTT = 1000ms` (P99 ≈ 950ms + δ; severely degraded). Tables and prose key on `BTT` end-to-end.
- **Relay submission tail**: 100ms reserved for cert broadcast + relay submit after consensus completes (matches OBFT.md's `header_submit_headroom` — see [docs/OBFT.md / Operating point](OBFT.md#timing-budget)). Effective BFT budget = 4000ms − BFT_start − 100ms.
- **Per-protocol T_commit wall-clock anchors at Config A** (back-derived from `T_relay_cutoff = 4000ms` minus each protocol's post-T_commit budget — max-MEV anchoring for cross-protocol MEV-fetch comparability): OBFT ≈ 3600ms (post-T_commit = Δ_2 + ε_3 + T_submit + ~50ms jitter ≈ 400ms at tightened `Δ_2 = 1 BTT`), 2abOBFT ≈ 3600ms (post-`T_commit` budget ≈ 400ms = `Δ_2 + ε_3 + T_submit` at tightened `Δ_2 = 1·BTT`, the same as OBFT — the σ-side `KindValue` carries its partial inline and Phase 3 is local, so both protocols end at the same wall-clock anchor at default BTT=200ms; 2abOBFT's commit point is the dynamic `TPhase2a` backstop), OBFTR(R=2) round-1 `T_commit_1` ≈ 1500ms (R-round budget ≈ 1600ms; OBFTR's Δ_2 sizing unchanged in this sweep). `T_commit` is semantically aligned across the OBFT family (= σ-or-NR commit point in all three); wall-clock values differ per-protocol due to consensus-budget differences. Comparisons in this doc anchor to `T_relay_cutoff` to keep the framing uniform across protocols. (Note: 2abOBFT.md's deployment-timing table uses a more conservative anchor at `T_commit ≈ 2000ms` for production headroom — see [2abOBFT.md §Timing budget](2abOBFT.md#timing-budget--concrete-configurations); both anchors are valid deployment choices, this doc uses max-MEV for like-for-like MEV-fetch framing in Table 4.) Within each protocol's L_Bid extension, `T_commit` stays invariant (bare-vs-+L_Bid only).
- **QBFT round timeout RT** — three variants. **QBFT** (reflood-aware, the realistic default) derives RT per-round from BTT plus one gossip-backstop cushion: `R1 = 4·BTT + SafetyBuffer`, `R≥2 = 5·BTT + SafetyBuffer` (the structural minimum 3·BTT/4·BTT plus `SafetyBuffer + 1·BTT` to absorb one IHAVE/IWANT recovery — matching OBFT's `B_0` reflood budget). **QBFT-no-reflood** is the same per-round structure with *no* cushion: `R1 = 3·BTT`, `R≥2 = 4·BTT` (the +1·BTT is the ROUND_CHANGE hop to the new leader, armed inside the next round's window) — the zero-cushion structural floor. **QBFT-SSV** uses `RT = 2000ms` fixed (current SSV production setting; held across BTT). The per-round timers fit many rounds within the slot — the no-reflood floor at BTT=100 fits ~9 rounds before the 4s relay cutoff; the fixed 2s fits ~2.
- **No specific block-fetch cost**: BFT_start corresponds to the moment Phase 1 broadcast (or QBFT PROPOSE) begins. Pre-fetch and pre-consensus sit in `[slot_start, BFT_start]`.
- **"Miss"**: cluster fails to produce a validator signature on the proposed block before the relay cutoff. Slot lost; no safety violation in any of the four protocols (safety is cryptographic / honest-majority).
- **"Apples-to-apples"**: all four protocols run within the same 4000ms budget at the same `n = 4, f = 1`. The scenario axes (start time, BTT) are the comparison dimensions.

## Sizing convention

All BTT counts in this doc use a **uniform "1 BTT per emission cycle" sizing** — each cluster-wide message exchange (PROPOSE, PREPARE, COMMIT, KindCommit, KindVerdict, KindOnion2b, etc.) is budgeted at one P99 propagation cycle. Mesh-jitter / reflood absorption is **structural** rather than per-emission: each protocol carries its jitter cushion in a dedicated mechanism instead of inflating every emission by 2× (which the previous "2 BTT per emission" convention did).

Per-protocol jitter / reflood cushion:
- **OBFT**: in the per-layer broadcast budget `B_k` — primary `B_0 = 2·BTT + SafetyBuffer` covers one initial propagation + one IWANT round-trip + one lazy-push cycle; backups `B_k = T_commit` absorb up to the full commit budget. Δ_2 tightened to `1·BTT` (KindCommit propagation only).
- **2abOBFT**: in the `SafetyBuffer` σ-pool fill window (default `700ms`, matching `SafetyBuffer`) — absorbs one gossipsub IHAVE/IWANT cycle for a late `KindValue` / forwarded witness. Phase 2a fires async on `L0Ready` (no fixed window) and the NR-side commit is dynamic; per-emission propagation tightened to `1·BTT`.
- **OBFTR**: in cross-round retention — bundles missing round r's σ-window remain usable in round r+1. Per-round Δ_2 = `1·BTT` (KindCommit_r propagation only).
- **QBFT** (reflood-aware): `SafetyBuffer + 1·BTT` added to each round's timer (R1 = 4·BTT + SafetyBuffer, R≥2 = 5·BTT + SafetyBuffer) — one gossip-backstop recovery (heartbeat + IWANT round-trip), matching OBFT's `B_0` reflood budget. The realistic mesh-tolerant default; compare against bare OBFT.
- **QBFT-no-reflood**: *no* cushion — each round's timer equals that round's exact decision time (R1 = 3·BTT, R≥2 = 4·BTT incl the ROUND_CHANGE hop). The zero-cushion structural floor; compare against other protocols' no-reflood variants (e.g. OBFT-no-reflood).
- **QBFT-SSV**: in round-timer slack `RT − R1 = 10·BTT − 4·BTT = 6·BTT` at BTT=200ms (production-anchored `RT = 2000ms`).

**This is the production-survival sizing for mission-critical roles.** For SSV proposer duty: missing a slot is per-slot economic loss + signal to stakers, so the configuration target is worst-case completion under recommended sizing. The structural cushions above are sized for P99→P9999 absorption.

QBFT is recomputed under the unified 1·BTT-per-emission convention for apples-to-apples comparison. Three QBFT variants appear in tables below:
- **QBFT** (reflood-aware): per-round RT = structural minimum + `SafetyBuffer + 1·BTT` (R1 = 4·BTT + SafetyBuffer, R≥2 = 5·BTT + SafetyBuffer). The realistic mesh-tolerant default — absorbs one gossip-backstop cycle per round like OBFT.
- **QBFT-no-reflood**: per-round RT derived from BTT — `R1 = 3·BTT`, `R≥2 = 4·BTT` (the +1·BTT is the ROUND_CHANGE hop) — with no cushion. Each round's timer is exactly that round's decision time, so recovery rounds fire as early as soundly possible.
- **QBFT-SSV**: current SSV production behavior (`RT = 2000ms`); jitter absorbed by the wide `RT − R1` slack.

All three share the same R1 healthy-path timing (4 BTT = 3 BTT consensus + 1 BTT post-consensus at tightened per-emission). They differ in RT and consequently in how many rounds fit the slot budget — the no-reflood floor's tight timers fit the most (~9 rounds at BTT=100 within a 4s slot), the reflood default fewer (a cushion per round), QBFT-SSV the fewest (~2). The no-reflood floor is the zero-cushion structural reference (compare against OBFT-no-reflood); bare QBFT is the realistic reflood-aware default (compare against bare OBFT); QBFT-SSV is the production-deployed timing.

## Protocol summary

- **Partial-sigs on pre-agreed V** (baseline; not a BFT consensus protocol): each operator computes their BLS partial signature on a pre-agreed V and gossips it; threshold aggregation produces the cluster signature. **1 BTT** (one emission cycle at tightened per-emission sizing for partial-sig collection + threshold aggregation). Assumes V is pre-agreed by external mechanism (e.g., beacon-spec deterministic computation for attestations / sync committee duties). **Cannot resolve V-disagreement** (e.g., MEV bundles fetched by different operators differ) — this is what BFT consensus protocols solve. Used here as the floor: what's the cluster's pure cryptographic cost AFTER V is somehow agreed.
- **QBFT** (reflood-aware default): 3-phase consensus (PROPOSE → PREPARE → COMMIT) + post-consensus partial-sig collection = **4 BTT R1** healthy. Per-round RT adds one gossip-backstop cushion (`SafetyBuffer + 1·BTT`) over the structural minimum: R1 timer = 4·BTT + SafetyBuffer, R≥2 = 5·BTT + SafetyBuffer. Absorbs one IHAVE/IWANT recovery per round (like OBFT's `B_0`); R2 fresh-V refetch on round timeout.
- **QBFT-no-reflood** (structural floor): same 4 BTT R1 healthy timing, but per-round RT = its exact decision time (R1 = 3·BTT; R≥2 = 4·BTT incl the ROUND_CHANGE hop), no cushion. Recovery rounds fire as early as soundly possible — cumulative ready-to-submit is **4·r BTT** at round r (R1 = 4, R2 = 8, R3 = 12, R4 = 16 BTT, …); the tight timers fit ~9 rounds within a 4s slot at BTT=100. Zero-cushion reference; under jitter it round-changes on any tail (by design).
- **QBFT-SSV** (current SSV production): same **4 BTT R1**. `RT = 2000ms = 10 BTT` round timeout — wide `RT − R1 = 6·BTT` slack absorbs mesh-jitter / reflood tail before triggering round-change. R2 fresh-V refetch on round timeout.
- **OBFT**: K-layer onion with chained encryption, single Phase 2 with one `KindCommit` emission per operator by `T_commit` (typically earlier on `T_L_0_observed` per the early-emit rule) carrying both σ and NR partials (no Defer state, no sub-phasing). Primary-vs-backup broadcast deadlines `T_broadcast_max_k`: the primary L_0 has `B_0 = 2·BTT + SafetyBuffer = 1100ms` at default SafetyBuffer (= SSV gossipsub HeartbeatInterval) for the MEV-fresh fetch; all backups L_1..L_{K-1} broadcast at BFT_start (`B_k = T_commit`) with deepest-confirmed-parent fetch. Phase 2 budget = **1 BTT propagation** (recommended `Δ_2 = 1 BTT` — reflood is structurally absorbed by `B_0` via SafetyBuffer) + 0 Phase 3. Recovery via in-round K-layer parallel fall-through (sequential local decryption in Phase 3, no extra BTT per layer). Backups absorb up to `B_k = T_commit` at any depth — full commit budget.
- **OBFTR** (R≥2): same K-layer onion as OBFT, plus R-round retry with re-flood, per-round independent commitments, L_C cluster-consensus signaling. **3 BTT R1** (1 BTT broadcast slack + 1 BTT Phase 2 + 1 BTT Phase 2.5 L_C + 0 Phase 3) **/ 3 BTT R2** (1 BTT re-flood + 1 BTT Phase 2 + 1 BTT L_C); total **6 BTT at R=2**. Recovers partition tails via cross-round retention — OBFTR's structural mesh-jitter absorber. Each round contributes its own `Δ_2 + 1·BTT` within-round absorption *plus* the inter-round time before the next round's acceptance horizon (bundles missing round r's σ-window remain usable in round r+1). The cumulative absorption span at R=2 Config A is ≈ 1050ms (≈ 5·BTT + ε_3), wider than the simpler `R · P99` shorthand suggests; see [OBFTR.md §Trust model](OBFTR.md#trust-model) for the derivation.
- **2abOBFT**: K-layer onion with a per-layer leader σ-witness (like OBFT) and a split Phase 2 — `KindValue` (σ-side terminal emission, σ partial inline) / `KindNoValue` (non-binding coordination, no L_0 lock) / `KindCommit-NRDirect`, plus a dynamically-fired `KindCommit-NR`. Phase 2a fires async on `L0Ready` (~1·BTT after the bundle arrives; no synchronized instant), Phase 3 is local. **3 BTT + SafetyBuffer** healthy (2 BTT Phase-1 broadcast/propagation base + 1·BTT async Phase-2a + 0 Phase 3) — the same total as OBFT at default `SafetyBuffer = 700ms`. Single-round only; the `KindNoValue` no-lock + upgrade adds equivocation / flakiness recoveries over bare OBFT (see Table 3).

## Total time to signed output (in BTT units)

All BTT counts use the uniform "1 BTT per emission cycle" sizing — see [§Sizing convention](#sizing-convention) above.

| Protocol | Round 1 healthy | Round 2 (recovery) | Total at R-round failure |
|---|---|---|---|
| Partial-sigs on pre-agreed V (baseline) | 1 BTT | n/a (no rounds) | n/a (no recovery — fails on any V-disagreement) |
| OBFT | 3 BTT + SafetyBuffer | n/a (single-round) | n/a (slot misses if R1 fails on adversarial pattern; K-layer fall-through is in-round, free) |
| OBFTR(R=2) | 3 BTT | 3 BTT | 6 BTT |
| 2abOBFT | 3 BTT + SafetyBuffer | n/a (single-round) | n/a (K-layer fall-through is in-round, free) |
| QBFT (reflood) | 4 BTT | 9 BTT + SafetyBuffer (R2) | 14 BTT + 2·SafetyBuffer (R3) |
| QBFT-no-reflood | 4 BTT | 8 BTT (= 4·r at r=2) | 12 BTT (R3 = 4·r at r=3) |
| QBFT-SSV | 4 BTT | 10 BTT (RT) + 4 BTT = 14 BTT | 14 BTT |

K-layer fall-through (OBFT, OBFTR, 2abOBFT) is sequential local decryption in Phase 3 — no per-layer BTT cost. It recovers silent/late leaders within the same round.

**Why OBFT 3 BTT + SafetyBuffer vs OBFTR 3 BTT per round.** OBFT's Phase-1 model (see [OBFT.md §Setting](OBFT.md)) lets the primary L_0 broadcast at `T_commit − B_0` with `B_0 = 2·BTT + SafetyBuffer` (the reflood-aware schedule — covers 1 BTT initial propagation + 1 BTT IWANT round-trip + one full SafetyBuffer-sized lazy-push cycle for mesh-flaky receivers). The Phase-2 budget post-`T_commit` is `Δ_2 = 1 BTT` (KindCommit propagation only; reflood lives in `B_k`). OBFTR uses uniform `T_commit_r − 1·BTT` broadcast slack at every round, `Δ_2 = 1·BTT` Phase 2 (KindCommit_r propagation only; mesh-jitter absorbed by cross-round retention), and a Phase 2.5 (L_C signaling). Per-round both protocols use 3·BTT of consensus exchange; OBFT additionally pays `+SafetyBuffer` in its primary B_0 to absorb the lazy-push cycle within a single round, while OBFTR pays nothing for reflood inside one round but cycles through R rounds to catch tail-arriving bundles.

**Why QBFT 4 BTT > OBFT (3 BTT + SafetyBuffer).** QBFT has 4 emission cycles per round (PROPOSE + PREPARE + COMMIT + post-consensus) vs OBFT's 1 emission cycle (Phase 2) plus broadcast slack. At 1 BTT per emission, QBFT's structural cost is 4 × 1 = 4 BTT; OBFT's is `B_0 + Δ_2 = (2 BTT + SafetyBuffer) + 1 BTT = 3 BTT + SafetyBuffer`. The OBFT structural difference (vs QBFT) is fundamental to the protocol shape (3-phase cluster-wide consensus vs onion with chained encryption); the +SafetyBuffer term is OBFT's choice to absorb one gossipsub lazy-push cycle inside Phase 1 rather than fall through, which QBFT-SSV handles via its round-timer slack instead (RT − R1 = 6·BTT at BTT=200ms); the QBFT-no-reflood floor carries no such cushion — it round-changes on any tail rather than absorbing it.

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

Healthy completion = leader honest, all Phase-1 bundles propagate, σ-quorum reaches at L_0 in round 1. All counts at uniform "1 BTT per emission" sizing (see [§Sizing convention](#sizing-convention)). **OBFT-family cells include `+SafetyBuffer` (default 700ms = SSV's gossipsub HeartbeatInterval) per the reflood-aware schedule**: OBFT total = `3·BTT + SafetyBuffer`, 2abOBFT total = `3·BTT + SafetyBuffer` (uniform across the family at default `SafetyBuffer = 700ms`). For `SafetyBuffer = 0` (fully-meshed cluster opt-out), subtract 700ms from each OBFT/2abOBFT cell.

### Table 1a — BFT start = 0ms (immediate), budget = 3900ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (witness, single-round) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1300ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | 2500ms ✓ | 1800ms ✓ | 2500ms ✓ | 2400ms ✓ |
| 1000ms | 1000ms ✓ | 3700ms ✓ | 3000ms ✓ | 3700ms ✓ | **4000ms ✗** (overshoots by 100ms) |

### Table 1b — BFT start = 800ms, budget = 3100ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (witness, single-round) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1300ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | 2500ms ✓ | 1800ms ✓ | 2500ms ✓ | 2400ms ✓ |
| 1000ms | 1000ms ✓ | **3700ms ✗** | 3000ms ✓ | **3700ms ✗** | **4000ms ✗** |

### Table 1c — BFT start = 1200ms, budget = 2700ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (witness, single-round) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1300ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | 2500ms ✓ | 1800ms ✓ | 2500ms ✓ | 2400ms ✓ |
| 1000ms | 1000ms ✓ | **3700ms ✗** | **3000ms ✗** | **3700ms ✗** | **4000ms ✗** |

### Table 1d — BFT start = 1800ms, budget = 2100ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (witness, single-round) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ | 600ms ✓ | 1300ms ✓ | 800ms ✓ |
| 600ms | 600ms ✓ | **2500ms ✗** | 1800ms ✓ | **2500ms ✗** | **2400ms ✗** |
| 1000ms | 1000ms ✓ | **3700ms ✗** | **3000ms ✗** | **3700ms ✗** | **4000ms ✗** |

### Table 1e — BFT start = 2500ms (late MEV fetch), budget = 1400ms

| BTT | Partial-sigs (1 emission) † | OBFT (single round, K-onion) | OBFTR R1 (1 of R rounds) | 2abOBFT (witness, single-round) | QBFT R1 (3-phase + post-cons.) |
|---|---|---|---|---|---|
| 200ms | 200ms ✓ | 1300ms ✓ tight | 600ms ✓ | 1300ms ✓ tight | 800ms ✓ |
| 600ms | 600ms ✓ | **2500ms ✗** | **1800ms ✗** | **2500ms ✗** | **2400ms ✗** |
| 1000ms | 1000ms ✓ tight | **3700ms ✗** | **3000ms ✗** | **3700ms ✗** | **4000ms ✗** |

† **Partial-sigs only fits if V is pre-agreed.** For SSV proposer duty (V varies per operator due to MEV bundles), this isn't directly applicable — the cluster needs a BFT consensus protocol to resolve V-disagreement first. Shown here as the floor: what completion would look like if V were pre-agreed (e.g., for non-MEV duties like attestations).

**Reading Tables 1a–1e** (default 700ms buffer framing — SafetyBuffer across OBFT/OBFTR/2abOBFT; at buffer = 0 the OBFT-family cells shift down by 700ms):

- **Partial-sigs floor (V pre-agreed)**: 1 BTT fits at every BFT_start across the BTT envelope. Sets the absolute floor — BFT consensus protocols pay 2-4 BTT extra to resolve V-disagreement (the OBFT-family additionally absorbs one gossipsub reflood cycle inside Phase 1, accounting for the +SafetyBuffer term).
- **BTT=200ms** (production-typical healthy mesh): every protocol fits at every BFT_start. At BFT_start = 2500ms (late MEV fetch, budget 1400ms), OBFTR R1 (600ms) and QBFT R1 (800ms) have the most slack; OBFT and 2abOBFT (both 1300ms) are tight (~100ms margin).
- **BTT=600ms** (degraded mesh): OBFTR R1 (1800ms) fits BFT_start ≤ 1800ms; OBFT, 2abOBFT (both 2500ms) and QBFT R1 (2400ms) fit BFT_start ≤ 1200ms. Beyond that only Partial-sigs fits (plus OBFTR R1 up to BFT_start = 1800ms).
- **BTT=1000ms** (severely degraded): OBFTR R1 (3000ms), OBFT and 2abOBFT (both 3700ms) fit BFT_start = 0; QBFT R1 (4000ms) overshoots by 100ms even at BFT_start = 0. All protocols miss everywhere beyond BFT_start = 0.
- **Healthy-path ordering**: Partial-sigs (1 BTT) < OBFT ≈ OBFTR R1 ≈ 2abOBFT (all 3 BTT before the buffer) < QBFT R1 (4 BTT). Adding the default 700ms buffer puts OBFT ≈ 2abOBFT (`3·BTT + buffer` ≈ 6.5·BTT at BTT=200) just above QBFT R1; at buffer = 0 (fully-meshed) the ordering is Partial-sigs (1) < OBFT ≈ OBFTR R1 ≈ 2abOBFT (3) < QBFT R1 (4). The buffer (SafetyBuffer) is OBFT/2abOBFT's choice to absorb the lazy-push cycle inside Phase 1; OBFTR and QBFT absorb it via cross-round retention / round-timer slack.

## Table 2 — Failure-recovery modes

When round-1 / single-round fails (silent leader, partition, network jitter, but NOT adversarial-byz-locked patterns covered in Table 3), each protocol's recovery path consumes additional time. All counts at uniform "1 BTT per emission" sizing.

### Table 2a — BFT start = 0ms, budget = 3900ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-no-reflood R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | 2800ms ✓ | 1600ms ✓ |
| 600ms | n/a | in-round (free) | 3600ms ✓ | in-round (free) | **4400ms ✗** | **4800ms ✗** |
| 1000ms | n/a | in-round (free) | **6000ms ✗** | in-round (free) | **6000ms ✗** | **8000ms ✗** |

### Table 2b — BFT start = 800ms, budget = 3100ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-no-reflood R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | 2800ms ✓ | 1600ms ✓ |
| 600ms | n/a | in-round (free) | **3600ms ✗** | in-round (free) | **4400ms ✗** | **4800ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **8000ms ✗** |

### Table 2c — BFT start = 1200ms, budget = 2700ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-no-reflood R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | **2800ms ✗** | 1600ms ✓ |
| 600ms | n/a | in-round (free) | **3600ms ✗** | in-round (free) | **4400ms ✗** | **4800ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **8000ms ✗** |

### Table 2d — BFT start = 1800ms, budget = 2100ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-no-reflood R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) | 1200ms ✓ | in-round (free) | **2800ms ✗** | 1600ms ✓ |
| 600ms | n/a | n/a (R1 missed at default SafetyBuffer) | **3600ms ✗** | n/a (R1 missed at default SafetyBuffer) | **4400ms ✗** | **4800ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **8000ms ✗** |

### Table 2e — BFT start = 2500ms (late MEV fetch), budget = 1400ms

| BTT | Partial-sigs (no recovery) † | OBFT (K-layer fall-through) | OBFTR R1+R2 (re-flood retry) | 2abOBFT (K-layer fall-through) | QBFT-SSV R2 (round-change + fresh V) | QBFT-no-reflood R2 (round-change + fresh V) |
|---|---|---|---|---|---|---|
| 200ms | n/a | in-round (free) tight | 1200ms ✓ tight | in-round (free) tight | **2800ms ✗** | **1600ms ✗** |
| 600ms | n/a | n/a (R1 missed) | **3600ms ✗** | n/a (R1 missed) | **4400ms ✗** | **4800ms ✗** |
| 1000ms | n/a | n/a (R1 missed) | **6000ms ✗** | n/a (R1 missed) | **6000ms ✗** | **8000ms ✗** |

† **Partial-sigs has no failure-recovery mechanism**: any V-disagreement (operators sign different V's) results in cluster signature aggregation failing — no rounds, no re-flood, no fall-through. The baseline only works on the healthy V-pre-agreed path.

"In-round (free)" means the recovery happens within the same single round — no additional time cost. K-layer fall-through is sequential local decryption in Phase 3, processing-bound (~50ms ε_3 single-layer; ~100ms ε_3 × K at K=2 default with 1 silent layer; ~200ms at K=4 up-tier with 3 silent layers), not BTT-bound.

**Reading Tables 2a–2e:**

- **OBFT and 2abOBFT have the cleanest network-failure recovery profile** at any start time where their healthy path fits — silent leader / partition recovery costs zero extra time via in-round K-layer fall-through. This is the structural advantage of K-layer onion with chained encryption: every honest leader in the K-layer rotation provides a fall-through opportunity within Phase 3.
- **OBFTR's R1+R2 retry** (6 BTT = 1200ms at BTT=200ms) fits at BFT_start ≤ 2500ms (1200ms ≤ 1400ms budget); fits at every start time at BTT=200ms. At BTT=600ms, R1+R2 (3600ms) fits at BFT_start = 0 only. At BTT=1000ms, R1+R2 (6000ms) misses everywhere. Under the tightened per-emission sizing OBFTR's retry envelope widens substantially compared to the older 2·BTT/emission framing.
- **QBFT-SSV R2** (RT=2000ms + 4 BTT = 2800ms at BTT=200ms) fits at BFT_start ≤ 800ms (2800ms ≤ 3100ms by 300ms margin); just misses at BFT_start = 1200ms (overshoots 2700ms budget by 100ms). At BTT ≥ 600ms R2 misses everywhere.
- **QBFT-no-reflood R2** (8·BTT = 1600ms at BTT=200ms) fits at BFT_start ≤ 1800ms (1600ms ≤ 2100ms budget, 500ms margin); misses only at BFT_start=2500ms. The no-reflood per-round timer recovers more budget than QBFT-SSV's wide 2s RT. **R3** (12·BTT = 2400ms at BTT=200ms) fits at BFT_start ≤ 1200ms (2400ms ≤ 2700ms budget, 300ms margin); overshoots the 2100ms budget at BFT_start = 1800ms. The tight per-round timers make R3 a meaningful retry tier up to BFT_start = 1200ms — and at smaller BTT the floor fits many more (≈ 9 rounds within the 4s slot at BTT=100).
- **Reflood-aware QBFT** (the bare-QBFT default) sits between the no-reflood floor and QBFT-SSV: R2 ready ≈ 9·BTT + SafetyBuffer (≈ 2500ms at BTT=200, default SafetyBuffer), a narrower BFT_start envelope than the floor but wider than QBFT-SSV. Tables 2a–2e show the no-reflood floor + QBFT-SSV bounds; the reflood default lies between them.
- **OBFT and 2abOBFT cannot retry** (single-round). Their "recovery" is the in-round fall-through; if that doesn't reach σ-quorum (e.g., adversarial pattern locks the σ-or-NR pools — see Table 3), the slot misses.
- **Structural retry advantage** (round-2 with fresh-V refetch) belongs to QBFT-no-reflood: available at BFT_start ≤ 1800ms with BTT=200ms (vs ≤ 800ms for QBFT-SSV). The OBFT family's K-layer in-round fall-through is structurally cheaper — it doesn't consume additional BTT and recovers silent leaders for free.

## Table 3 — Adversarial-byz failure mode recoverability (scenario-independent)

These failure modes depend on protocol *structure*, not on BTT or start time. They apply where the protocol's healthy path would otherwise fit (i.e., the cells in Table 1 marked ✓). QBFT and QBFT-SSV share structural recoverability since they're the same protocol with different RT — the difference is in *whether* recovery fits budget (covered in Tables 1, 2), not whether the recovery mechanism exists.

| Failure mode | Partial-sigs † | QBFT (both variants) ‡ | OBFT | OBFTR | 2abOBFT |
|---|---|---|---|---|---|
| **σ-locked equivocation 1-1-1** (byz delivers V, V', V'' to three honest at L_0) | n/a (no leader/equivocation surface; V pre-agreed) | ✓ via R2 fresh V | ✗ slot miss | ✗ slot miss (R-invariant) | ✗ slot miss (σ-locked, no NR pivot) |
| **σ-locked-split equivocation f-f** (byz delivers V_a to f honest, V_b to f honest, ∅ to the rest) | n/a | ✓ via R2 fresh V | ✗ slot miss | ✗ slot miss (R-invariant) | ✓ at L_0 (witness head-start + peer-reflood-V harvest + upgrade) |
| **h_V=1 selective-delivery deadlock** | n/a | ✓ via R2 | ✓ under healthy mesh (peer-reflood-V); ✗ under degraded mesh (algebraic deadlock at f=1, n=4: σ-pool=2 < qV; NR-pool=2 < qEnc; deterred via Assumption 4 across slots) | ✗ (R-invariant) | ✓ under healthy mesh (peer-reflood-V harvest + upgrade); ✗ under degraded mesh (same algebraic deadlock as OBFT) |
| **Validity-divergence, honest majority (3-1 / 1-3)** (head-change splits honest verdicts) | ✗ slot miss (no in-protocol V-disagreement resolution) | ✓ via R2 at moved head | ✓ (3-σ → L_0; 1-σ-3NV → NR fall-through to L_1) | ✓ (same) | ✓ (same) |
| **Validity-divergence 2-2 split** | ✗ slot miss | ✓ if head moves R1→R2 | ✗ (algebraic limit) | ✗ (algebraic limit) | ✗ (algebraic limit) |
| **2-1 partial equivocation** (byz delivers V to 2f honest, V' to one) | n/a | ✓ via R2 | ✓ via Phase-1 σ_V crypto-lock | ✓ via Phase-1 σ_V (R-invariant) | ✓ at L_0 via leader witness |
| **Transient mesh-flakiness** | ✗ if too few honest partials reach in time, threshold under-quorum | ✓ via R2 round-reset | ✗ slot miss (cross-phase exclusivity) | ✓ via R-round retry | ✓ (KindNoValue no-lock + upgrade once mesh delivers V) |
| **Multi-leader silent (K-1 = 3 silent in K=4)** | n/a (no leader rotation) | ✗ multiple round-changes exceed budget | ✓ in-round K-layer fall-through | ✓ in-round | ✓ in-round |
| **Sustained partition > absorption window** | ✗ | ✗ | ✗ | ✗ (extends to R·BTT, then misses) | ✗ |
| **> f operators offline / byz** | ✗ | ✗ | ✗ | ✗ | ✗ |

† **Partial-sigs assumes V is pre-agreed across all honest** (e.g., via beacon-spec deterministic computation for attestations / sync committee). For SSV proposer duty with MEV, V varies per operator → partial-sigs alone cannot resolve V-disagreement → BFT consensus is required. The "n/a" entries above mark failure modes protocol-specific to leader/equivocation surfaces that don't exist in partial-sigs.

‡ **QBFT and QBFT-SSV share Table 3 cells** (same protocol, different RT). Difference between variants: how many rounds fit the slot budget — QBFT-SSV fits R2 at BFT_start ≤ 800ms with BTT=200ms; QBFT-no-reflood fits R2 at BFT_start ≤ 1800ms with BTT=200ms. Beyond those cells, "✓ via R2" recovery doesn't fit budget regardless of structural availability.

**Reading Table 3:**

- **QBFT recovers more adversarial-byz patterns** structurally than the OBFT family, but recovery only materializes when R2 fits budget. This means BTT=200ms with BFT_start ≤ 1800ms (QBFT-no-reflood) or BFT_start ≤ 800ms (QBFT-SSV). At BTT ≥ 600ms or BFT_start = 2500ms, the structural QBFT-recovery advantage doesn't fit budget for any variant.
- **OBFT family avoids QBFT's structural disadvantage** at multi-leader-silent (K-1 ≥ 3) patterns: QBFT requires K serial round-changes (each ~RT + 4 BTT under tightened sizing), exceeding the 4000ms budget at any K-1 ≥ 3. OBFT/OBFTR/2abOBFT recover within a single round via K-layer fall-through (free in Phase 3).
- **2abOBFT's wins over bare OBFT** come from the `KindNoValue` no-lock + upgrade: **σ-locked-split f-f equivocation** (silent operators harvest the value via a forwarded witness and upgrade) and **transient mesh-flakiness** (the flaky operators upgrade once their mesh delivers V instead of hard-NR-locking). Both protocols share the same leader-witness, so h_V=1, 2-1 partial equivocation, and honest-majority validity-divergence recover identically.
- **The OBFT family's shared residual misses** are **1-1-1 equivocation** and the **2-2 validity boundary** — σ-locked operators can't pivot, and neither cohort reaches quorum. Only QBFT's fresh-V round-change recovers these (when R2 fits budget); the OBFT family relies on the rational-byzantine deterrent (Assumption 4).

## Table 4 — MEV-fetch budget by protocol (BTT=200ms)

At the SSV proposer-duty operating point — `BTT = 200ms`, `Relay_cutoff = 4000ms`, `header_submit_headroom = 100ms`, `RANDAO_done ≈ 150ms` (see [OBFT.md §Application](OBFT.md#timing-budget) for the full derivation) — each protocol's leader has a different MEV-relay-fetch budget bounded by when its broadcast must complete. The fetch budget is the wall-clock from `RANDAO_done` to the leader's broadcast deadline. All counts at uniform "1 BTT per emission" sizing.

**OBFT (K=4)** — primary-vs-backup broadcast at `T_broadcast_max_k = max(0, T_commit − B_k)` with `B_0 = 2·BTT + SafetyBuffer` (primary, MEV-fresh) and `B_1..B_{K-1} = T_commit` (backups broadcast at BFT_start with deepest-confirmed-parent fetch) — see [OBFT.md §Setting](OBFT.md#setting). `T_commit = 3600ms` post-tighten. SSV production defaults to `SafetyBuffer = 700ms` (gossipsub HeartbeatInterval); fully-meshed clusters may opt out by setting SafetyBuffer near zero:

| Leader | Broadcast (SafetyBuffer=700ms default) | MEV-fetch (default) | Broadcast (SafetyBuffer=0 opt-out) | MEV-fetch (SafetyBuffer=0) |
|---|---|---|---|---|
| V_0 (primary) | 2500ms | **~2350ms** | 3000ms | **~2850ms** |
| V_1, V_2, V_3 (backups) | 0ms (slot start) | ~0ms | 0ms (slot start) | ~0ms |

(At SafetyBuffer=0 the recovery floor `max(SafetyBuffer, 1·BTT)` pulls V_0's broadcast to `T_commit − 3·BTT = 3000ms` — 1·BTT earlier than the naive `T_commit − B_0 = 3200ms` — so the h_V=1 peer-reflood-V σ-upgrade still lands before `T_commit`; see [OBFT.md §Setting](OBFT.md#setting). Dormant at default SafetyBuffer=700ms since `SafetyBuffer ≥ 1·BTT`.)

**Partial-sigs on pre-agreed V (baseline)** — V agreed externally; consensus = 1 BTT (one emission for partial-sig propagation + threshold aggregation). Broadcast deadline = `Relay_cutoff − 100ms − 1 BTT = 3700ms`:

| Step | Time | MEV-fetch budget |
|---|---|---|
| V determined (must be cluster-agreed) | ≤ 3700ms | **3550ms** |

**QBFT-SSV (RT=2000ms, 2-round target)** — single leader per round; fetch must complete before each round's PROPOSE. R1 PROPOSE deadline derived from R2 fit constraint: `PROPOSE_R1 + RT + 4 BTT + 100ms ≤ 4000ms` → `PROPOSE_R1 ≤ 1100ms`:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 1100ms | **950ms** |
| R2 | 3100ms | 2950ms |

**QBFT-no-reflood (2-round target)** — the tight per-round timers let PROPOSE_R1 fire later. R2 ready (from PROPOSE_R1) = R1 timeout (3·BTT) + ROUND_CHANGE hop (1·BTT) + R2 (4·BTT) = 8·BTT, so `PROPOSE_R1 + 8 BTT + 100ms ≤ 4000ms` → `PROPOSE_R1 ≤ 2300ms`:

| Round | PROPOSE time | MEV-fetch budget |
|---|---|---|
| R1 | 2300ms | **2150ms** |
| R2 | 3100ms | 2950ms |

(QBFT-no-reflood R3 fits at BFT_start ≤ 1200ms — `R3 ready = 12·BTT = 2400ms`, so `BFT_start + 2400ms + 100ms ≤ 4000ms` holds through BFT_start = 1200ms. The tight per-round timers make R3 a usable retry tier across a wide start-time range.)

**Cross-protocol ranking** (uniform 1·BTT-per-emission sizing throughout; OBFT figures shown at both SafetyBuffer settings):

| Rank | Leader | MEV-fetch (SafetyBuffer=700ms default) | MEV-fetch (SafetyBuffer=0 opt-out) | Notes |
|---|---|---|---|---|
| 1 † | Partial-sigs on pre-agreed V | **3550ms** | 3550ms | Floor: only available if V is pre-agreed (no MEV / no V-disagreement) |
| 2 | QBFT R2 leader | 2950ms | 2950ms | Only reachable after R1 fails (paying the ~2s round-change cost) |
| 3 | OBFT V_0 | **~2350ms** | **2850ms** | The only OBFT layer competing on MEV; primary always tried first (SafetyBuffer=0 capped by the recovery floor at `T_commit − 3·BTT`) |
| 4 | QBFT-no-reflood R1 | 2150ms | 2150ms | Tight per-round timer leaves more R1 fetch budget than QBFT-SSV |
| 5 | QBFT-SSV R1 | 950ms | 950ms | SSV's wide RT shrinks R1 fetch window; still meaningfully larger under tightened sizing than the old 150ms |
| 6 (last) | OBFT V_1, V_2, V_3 (backups) | ~0ms | ~0ms | Backups all broadcast at BFT_start with deepest-confirmed-parent fetch — safety nets, not MEV-fresh alternatives |

† **Partial-sigs is not directly comparable** for SSV proposer duty (V varies per operator). Shown as the no-consensus floor — what would be possible if V didn't need cluster-wide agreement.

**Reading:**

- **OBFT V_0 vs QBFT R2 (post-tighten reranking)**: under the older 2·BTT/emission framing, OBFT V_0 led QBFT R2 (~2350ms vs 2150ms at default SafetyBuffer). Under tightened 1·BTT/emission, QBFT R2 now beats OBFT V_0 at default SafetyBuffer (2950ms vs ~2350ms = 600ms ahead) — but OBFT V_0 is the *primary* leader (always tried first; no round-timeout gap), while QBFT R2 is reachable only after R1 fails (paying the ~2s round-change cost). At SafetyBuffer=0 (fully-meshed opt-out) QBFT R2 still leads OBFT V_0, by 100ms (2950 vs 2850) — the recovery floor pulls V_0's SafetyBuffer=0 broadcast 1·BTT earlier (to `T_commit − 3·BTT`), so OBFT no longer overtakes at the opt-out as it did under the pre-floor framing (3050 vs 2950). The structural tradeoff hasn't changed (OBFT V_0 is always tried first); the headline numbers just compress under uniform sizing.
- **OBFT V_0 pays a 700ms–1200ms BFT-consensus tax over the partial-sigs floor** (depending on SafetyBuffer): 3550 − 2850 = 700ms (3.5·BTT) at SafetyBuffer=0, 3550 − ~2350 = ~1200ms (~6·BTT) at default SafetyBuffer. The tax decomposes as `broadcast slack + Δ_2 − partial-sigs post-fetch overhead = max(2·BTT + SafetyBuffer, 3·BTT) + 1·BTT − 1.5·BTT` (the broadcast slack is floored at 3·BTT by the recovery margin). The 2·BTT shallow base covers 1·BTT P99 leader-broadcast propagation + 1·BTT IWANT round-trip; SafetyBuffer covers one full IHAVE/IWANT cycle for mesh-flaky receivers. OBFT runs at tightened `Δ_2 = 1 BTT`; partial-sigs floor reserves 1·BTT emit + 0.5·BTT (100ms) submit = 1.5·BTT post-fetch.
- **QBFT-SSV R1 widens to 950ms MEV-fetch under tightened sizing** (was 150ms under the older sizing) — R1 shrinks from 8·BTT to 4·BTT, freeing 800ms of the slot budget back to the R1 leader. QBFT-no-reflood recovers more: its tight per-round timers (R1 timeout 3·BTT, so R2 needs only 8·BTT total) let PROPOSE_R1 fire as late as 2300ms, reaching 2150ms MEV-fetch at R1. QBFT's RT framing is SafetyBuffer-independent.
- **OBFT backups trade all MEV-fetch for full-slot absorption**: under the simplified backup schedule, all backups V_1..V_{K-1} fetch at slot start (deepest-confirmed parents — re-org resistant) and broadcast immediately, giving the cluster the entire `T_commit` budget for that bundle's propagation. OBFT's K-layer fall-through is in-round (sequential local IBE decryption, no per-layer RTT) — fall-through is reliable and free, just MEV-poor.

**OBFTR(R=2) and 2abOBFT primary-leader fetch budgets** at BTT=200ms (uniform 1·BTT-per-emission sizing). 2abOBFT's Phase 2a fires async (no separate pre-commit window), so V_0's broadcast deadline and MEV-fetch budget are on par with bare OBFT V_0 at default SafetyBuffer (both ~2350ms). At the SafetyBuffer=0 opt-out they diverge by 1·BTT: bare OBFT's recovery floor pulls its V_0 broadcast to `T_commit − 3·BTT` (~2850ms MEV-fetch), while 2abOBFT V_0 stays at ~3050ms — 2abOBFT's later resolve-window deadline lets it broadcast 1·BTT later at the same recovery margin (its Δ_2-equivalent commit propagation sits inside the resolve window rather than after a separate `T_commit`). The staggered backup fetches (`fetchAt[k] = T_0_broadcast − (k+2)·BTT`, deepest at slot start) are not MEV-fresh — see [2abOBFT.md §Timing parameters](2abOBFT.md#timing-parameters) for the exact per-layer schedule. OBFTR(R=2) totals are R-round-summed under tightened per-round 3·BTT (1·BTT slack + 1·BTT Phase 2 + 1·BTT Phase 2.5).

| Protocol | Total BTT | Broadcast (SafetyBuffer=700ms default) | MEV-fetch (default) | Broadcast (SafetyBuffer=0) | MEV-fetch (SafetyBuffer=0) |
|---|---|---|---|---|---|
| OBFTR(R=2) (R1+R2 fit) | 6 BTT | ~2650ms | ~2500ms | ~2650ms | ~2500ms |
| OBFTR(R=2) (R1-only) | 3 BTT | ~3250ms | ~3100ms | ~3250ms | ~3100ms |
| 2abOBFT V_0 (primary) | 3 BTT + SafetyBuffer | ~2500ms | ~2350ms | ~3200ms | ~3050ms |
| 2abOBFT backups (V_1..V_{K-1}) | 3 BTT + SafetyBuffer | staggered, earlier | not MEV-fresh | staggered | not MEV-fresh |

OBFTR's broadcast deadlines are anchored on R-round completion (not B_k); the schedule's reflood-aware framing doesn't apply the same way because cross-round retention already provides multi-round absorption. 2abOBFT's V_0 MEV-fetch is on par with bare OBFT's V_0 at default SafetyBuffer (async-fire Phase 2a adds no pre-commit window); at the SafetyBuffer=0 opt-out 2abOBFT V_0 is 1·BTT more MEV-fresh, because bare OBFT's recovery floor pulls its broadcast earlier (see above).

2abOBFT uses a per-layer staggered fetch schedule (`fetchAt[k] = T_0_broadcast − (k+2)·BTT` for shallow layers, deepest at slot start; see [2abOBFT.md §Timing parameters](2abOBFT.md#timing-parameters)). At default SafetyBuffer 2abOBFT's V_0 budget matches bare OBFT's (at the SafetyBuffer=0 opt-out it's 1·BTT more MEV-fresh — the recovery floor pulls bare OBFT's broadcast earlier); OBFT's backups have wider per-layer slack (`B_k = T_commit`), while 2abOBFT's `SafetyBuffer` σ-pool fill window absorbs late `KindValue` / witness arrivals and the `KindNoValue` no-lock adds the equivocation / flakiness recoveries over bare OBFT (see Table 3). OBFTR pays the equivalent cost via cross-round retention.

The **MEV-fetch budget asymmetry is a structural OBFT-family advantage over QBFT-SSV R1 for the primary leader** — OBFT V_0 has ~2.5× the MEV-fresh fetch time of QBFT-SSV R1 leader (~2350ms vs 950ms at default SafetyBuffer). Against QBFT-no-reflood R1 (2150ms) OBFT V_0 is closer (~200ms ahead at default SafetyBuffer; ~700ms ahead at SafetyBuffer=0). Against QBFT R2 (2950ms), OBFT V_0 trails by ~600ms at default SafetyBuffer (and by ~100ms at SafetyBuffer=0), but OBFT V_0 is the *primary* leader (always tried first; no round-timeout gap), while QBFT R2 is reachable only after R1 fails (paying the round-change cost). (Note: OBFT's backups V_1..V_{K-1} are not MEV-fresh — they're last-resort safety nets that broadcast at BFT_start with deepest-confirmed-parent fetch; the OBFT vs QBFT MEV comparison is V_0 vs QBFT R1 (SSV or no-reflood) / R2 only.)

## Cross-scenario takeaways

**Partial-sigs floor (V pre-agreed)**: 1 BTT = 200ms total at tightened per-emission sizing. Fits at every (BFT_start, BTT) cell. Sets the floor: BFT-consensus protocols pay 2-4 BTT extra to resolve V-disagreement. For SSV proposer duty (V varies per operator due to MEV bundles), partial-sigs alone is not directly applicable — used here as a reference for the BFT-consensus tax.

**Healthy-path latency at production-typical BTT (200ms)** *(BTT-count only; add +700ms buffer for OBFT-family totals)*: partial-sigs 200ms, OBFT 600ms, OBFTR-R1 600ms, 2abOBFT 600ms, QBFT R1 800ms. Every protocol fits at every BFT_start at BTT=200ms; OBFT and 2abOBFT (1300ms incl. the default buffer) are tight at BFT_start = 2500ms (~100ms margin).

**Late-fetch tolerance (BFT start = 2500ms, budget = 1400ms)**: at BTT=200ms, partial-sigs (200ms), OBFTR R1 (600ms), and QBFT R1 (800ms) fit comfortably; OBFT and 2abOBFT (600ms BTT-count, or 1300ms incl. the default 700ms buffer) are tight (100ms margin), fitting with slack at buffer = 0. At BTT ≥ 600ms, all consensus protocols miss — late-fetch is incompatible with degraded mesh.

**Degraded-mesh tolerance (BTT = 1000ms)**: at BFT_start = 0 only — OBFT and 2abOBFT (3000ms BTT-count, or 3700ms incl. the default buffer) and OBFTR R1 (3000ms) fit; QBFT R1 (4000ms) overshoots by 100ms. Beyond BFT_start = 0, all consensus protocols miss.

**Mid-BTT tolerance (BTT = 600ms)**: bare OBFT and 2abOBFT (1800ms BTT-count, or 2500ms incl. the default buffer) fit at BFT_start ≤ 1200ms; OBFTR R1 (1800ms) fits at BFT_start ≤ 1800ms; QBFT R1 (2400ms) fits at BFT_start ≤ 1200ms. All consensus protocols miss at BFT_start = 2500ms.

**Round-2 retry usefulness**: under tightened per-emission sizing — **OBFTR's R1+R2 (6 BTT = 1200ms at BTT=200ms) fits at every BFT_start** (1200ms ≤ 1400ms minimum budget). **QBFT-SSV R2 (RT + 4 BTT = 14 BTT = 2800ms at BTT=200ms) fits at BFT_start ≤ 800ms** (2800ms ≤ 3100ms budget); misses at BFT_start ≥ 1200ms. **QBFT-no-reflood R2 (8 BTT = 1600ms at BTT=200ms) fits at BFT_start ≤ 1800ms** (1600ms ≤ 2100ms budget); misses at BFT_start = 2500ms. At BTT ≥ 600ms, only OBFTR R1+R2 (3600ms) fits at BFT_start = 0. **QBFT-no-reflood R3 (12 BTT = 2400ms at BTT=200ms) fits at BFT_start ≤ 1200ms** (2400ms ≤ 2700ms budget); overshoots the 2100ms budget at BFT_start = 1800ms. The OBFT family's K-layer in-round fall-through is structurally cheaper than retry — it doesn't consume additional BTT and recovers silent leaders for free.

**Adversarial-byz exposure ranking** (most-recovered to least-recovered):

1. **QBFT (with R2 budget)**: fresh-V round-change recovers every adversarial-byz equivocation/validity pattern — 1-1-1, σ-locked-split, 2-1, h_V=1, and validity-divergence (both 3-1 and the 2-2 boundary). Bound by R2 fitting budget (BFT_start ≤ 800ms QBFT-SSV / ≤ 1800ms no-reflood at BTT=200ms) and excludes multi-leader-silent (serial round-change overshoots).
2. **Bare 2abOBFT**: the `KindNoValue` no-lock + leader witness recover σ-locked-split f-f equivocation, transient mesh-flakiness, h_V=1 (healthy mesh), 2-1 partial equivocation, and honest-majority validity-divergence in a single round, plus multi-leader-silent via K-layer fall-through. Misses 1-1-1 and the 2-2 validity boundary (deterred via Assumption 4).
3. **Bare OBFT**: recovers h_V=1 (healthy mesh, peer-reflood-V), 2-1 partial equivocation, honest-majority validity-divergence, and multi-leader-silent. Misses σ-locked-split equivocation and transient mesh-flakiness (no `KindNoValue` no-lock) on top of 2abOBFT's misses — all deterred via Assumption 4 (rational-byzantine deterrent + planned blacklist + staker migration).
4. **Bare OBFTR**: same adversarial-byz coverage as bare OBFT (R-invariant), plus partition-tail recovery via R-round retry.

**Multi-leader-silent advantage**: OBFT family (OBFT, OBFTR, 2abOBFT) all complete at K-1 ≥ 3 silent within a *single* round via Phase-3 reconstruction walk — free, at any (BFT_start, BTT) where the healthy path fits. QBFT must serial-round-change through each silent leader: QBFT-SSV (RT=2000ms) overshoots the budget once K-1 ≥ 2; QBFT-no-reflood's tight per-round timers fit more (R4 ready = 16·BTT = 3200ms at BTT=200ms, so K-1 = 3 silent leaders recover at BFT_start = 0), but that envelope collapses as BFT_start or BTT grows. The OBFT-family in-round fall-through stays structurally cheaper (one round, zero extra BTT) across the whole envelope.

**Choosing a protocol** (deployment guidance):

- **Pre-agreed V (no consensus needed)**: partial-sigs floor at 1 BTT = 200ms. Use for SSV duties where V is deterministic (attestations, sync committee). Not applicable to MEV proposer duty since V varies per operator.
- **Healthy-path latency-critical (with consensus)**: OBFT, OBFTR-R1, and 2abOBFT at BTT=200ms (600ms BTT-count completion each) — tied as best in family at the tightened sizing. QBFT R1 800ms.
- **Late-fetch / high-MEV proposer duty (BFT start ≥ 2000ms)**: at BTT=200ms — OBFTR-R1 (600ms) and QBFT R1 (800ms) fit comfortably; OBFT and 2abOBFT (600ms BTT-count, 1300ms incl. the default buffer) are tight at 2500ms start (fit with slack at buffer = 0).
- **Adversarial-byz robustness within single round**: 2abOBFT — recovers σ-locked-split equivocation and transient mesh-flakiness (via `KindNoValue` no-lock + upgrade), plus h_V=1, 2-1 partial equivocation, and honest-majority validity-divergence, with no round-2 budget cost; misses 1-1-1 and the 2-2 boundary (deterred via Assumption 4). Bare OBFT recovers h_V=1, 2-1, and honest-majority validity but not σ-locked-split or transient mesh-flakiness.
- **Multi-round partition tail absorption**: OBFTR(R=2) — under tightened sizing, R1+R2 (6 BTT = 1200ms at BTT=200ms) fits at every BFT_start. Significantly more attractive than under the older 2·BTT/emission framing.
- **QBFT-SSV (current SSV)**: production-mature; under tightened per-emission sizing, R1 fits at every BFT_start at BTT=200ms; R2 fits at BFT_start ≤ 800ms. Misses at BTT ≥ 600ms beyond BFT_start ≤ 1200ms.
- **QBFT-no-reflood**: zero-cushion structural reference — same R1 timing as QBFT-SSV but tight per-round timers let R2 fit at BFT_start ≤ 1800ms and R3 at BFT_start ≤ 1200ms with BTT=200ms. Under jitter it round-changes on any tail (by design); compare against OBFT-no-reflood. Not the production deployment (that's QBFT-SSV).

## OBFT + L_Bid mini-consensus extension

OBFT + L_Bid (specified in [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)) is an opportunistic bid-routing extension to bare OBFT. It prepends a bid-determined L_Bid layer above OBFT's K rotation-determined layers (yielding `K' = K + 1`) and adds a mini-consensus sub-phase between `T_0_arrival` and `T_commit` that resolves L_Bid identity cluster-wide before σ-commitment. This section identifies scenarios where OBFT+L_Bid's behavior differs from bare OBFT and from the other three protocols. **Most scenarios are identical between bare OBFT and OBFT+L_Bid**; the differences are surfaced below.

### Differences vs bare OBFT (summary)

- **+1 BTT total consensus time** at conservative `Δ_minicon = 2·BTT` (was +2·BTT under the older 2·BTT-broadcast-slack framing), all in pre-`T_commit` budget: OBFT+L_Bid is **4 BTT** (1 BTT broadcast slack + 2 BTT mini-consensus + 1 BTT Phase 2 + 0 Phase 3) vs bare OBFT's 3 BTT under uniform 1·BTT-per-emission sizing. `T_commit` is back-end-anchored and unchanged from bare OBFT; the 2 BTT mini-consensus runs as a sub-phase at the tail of Phase 1, so the cost falls on the L_0..L_{K-1} broadcast deadlines (MEV-fetch budget shrinks by `Δ_minicon`), not on post-`T_commit` slack.
- **Value capture upside**: highest-bid eligible rotation-layer block on the healthy path (when L_Bid σ-quorum reaches) instead of fixed rotation-priority V.
- **New failure modes at L_Bid**: candidate-withholding (closed at f=1 by the value-keyed witness harvest, residual at f≥2 with behavioral evidence), equivocation-promotion (the genuinely-new `K/n` residual; relay attestation bounds its frequency), and verdict-equivocation (a rider on bid divergence, not standalone; slashable Rule 8 = two distinct verdicts); the residual cases slot-miss-without-fall-through to L_0.
- **L_0..L_{K-1} rotation layers are unchanged**: when the mini-consensus fails to converge the cluster falls through to L_0 with the same recovery profile as bare OBFT. C1/C2 closure is conditional — see [Adversarial-byz failure modes](#adversarial-byz-failure-modes-specific-to-l_bid--table-3-delta) below.

### Where OBFT+L_Bid's outcome differs from bare OBFT

#### Success-mode delta — Table 1

Two scenarios show different success outcomes between bare OBFT and OBFT+L_Bid (at recommended Δ sizing, with cells stated under the Table 1 `+SafetyBuffer` convention). In all other (BFT_start, BTT) combinations, both protocols complete healthy or both miss healthy. The full-protocol comparison at the differing scenarios:

| Scenario | Budget | QBFT R1 | Bare OBFT | **OBFT+L_Bid** | OBFTR R1 | 2abOBFT |
|---|---|---|---|---|---|---|
| **0ms, BTT=1000ms** | 3900ms | **4000ms ✗** | 3700ms ✓ | **4700ms ✗** | 3000ms ✓ | **4700ms ✗** |
| **1200ms, BTT=600ms** | 2700ms | 2400ms ✓ | 2500ms ✓ | **3100ms ✗** | 1800ms ✓ | **3100ms ✗** |

Under tightened per-emission sizing, OBFT+L_Bid (4·BTT + SafetyBuffer at default) loses bare OBFT's healthy-path advantage where bare OBFT fits but OBFT+L_Bid's extra `Δ_minicon` overshoots — the two scenarios above. (At the `800ms, BTT=1000ms` and `1800ms, BTT=600ms` cells flagged in earlier drafts, the `+SafetyBuffer` accounting puts bare OBFT itself out of budget — see Table 1b and Table 1d above — so the outcomes are the same between bare OBFT and OBFT+L_Bid — both ✗ — and the cells no longer belong in this "differing scenarios" table.) At BTT=200ms (every BFT_start) and at most other BTT × BFT_start cells the two fit equally; OBFTR R1 widens its envelope significantly post-tighten and is the most-tolerant choice at degraded-mesh × late-start cells.

#### Failure-recovery delta — Table 2

**No latency difference.** Both bare OBFT and OBFT+L_Bid recover via in-round K-layer / K'-layer fall-through (sequential local decryption in Phase 3, no per-layer BTT cost). OBFT+L_Bid's K' = K + 1 adds one extra layer at the top, giving an additional "first-try" recovery opportunity at no extra time. Recovery profile across all scenarios is identical between bare OBFT and OBFT+L_Bid.

#### Adversarial-byz failure modes specific to L_Bid — Table 3 delta

These failure modes don't apply to bare OBFT (no L_Bid layer):

| Failure mode | Bare OBFT | OBFT+L_Bid |
|---|---|---|
| **C1 — Selective candidate withholding at L_Bid** | n/a | ✓ closed when verdict-quorum doesn't form; when byz forces the quorum, the value-keyed witness harvest closes it **at f=1** (residual at f≥2) |
| **C2 — Candidate / bid equivocation at L_Bid** | n/a | ✓ closed when verdict-quorum doesn't form; else the genuinely-new equivocation-promotion residual |
| **C3 — V_LBid validity-divergence majority (3-of-4)** | n/a | ✓ closed by convergence rule |
| **Candidate-withholding at L_Bid** | n/a | **✓ closed at f=1** by the witness harvest; **✗ slot miss at f≥2** (deadlock blocks L_0 fall-through; behavioral evidence) |
| **Equivocation-promotion at L_Bid** (genuinely-new) | n/a | **✗ slot miss** — byz inflates `bid_value` to promote an equivocated candidate to the gate (`K/n` surface; base leader-equivocation / Rule 7; relay attestation bounds the frequency) |
| **Verdict-equivocation at L_Bid** | n/a | **✗ slot miss** when it bites — a *rider* on bid divergence, not standalone (slashable Rule 8 = two distinct verdicts) |
| **2-2 validity split at L_Bid** | n/a | **✗ algebraic limit** (all-honest falls through to the inherited L_0 floor; an L_Bid-layer deadlock needs byz-blocked NR) |
| L_0..L_{K-1} rotation-layer failures | (per Table 3) | **Same as bare OBFT** |

In the context of L_Bid integration across the OBFT family — only **OBFT + L_Bid** is specified ([docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)); **OBFTR + L_Bid** and **2abOBFT + L_Bid** are hypothetical compositions discussed structurally below but not yet written as appendices in their respective spec docs:

| L_Bid failure mode | OBFT+L_Bid | OBFTR+L_Bid | 2abOBFT+L_Bid |
|---|---|---|---|
| C1/C2/C3 deadlocks | ✓ conditional (C1 also closed at f=1 by the witness harvest; C2 → equivocation-promotion; C3 on 3-of-4 majority) | ✓ same | ✓ same |
| Candidate-withholding at L_Bid | ✓ closed at f=1 (witness harvest); ✗ at f≥2 | ✓ same (R-invariant) | ✓ same |
| Equivocation-promotion at L_Bid | ✗ slot miss (`K/n`; attestation bounds) | ✗ same | ✗ same |
| Verdict-equivocation at L_Bid | ✗ slot miss (rider on bid divergence) | ✗ same | ✗ same |
| 2-2 validity split at L_Bid | ✗ algebraic limit | ✗ algebraic limit | ✗ algebraic limit |
| Multi-leader silent (across L_Bid + rotation) | ✓ in-round K'-layer fall-through | ✓ in-round | ✓ in-round |

The L_Bid-specific failure modes are structurally identical across the three protocol families (all carry the shared leader σ-witness, so the f=1 witness-harvest closure applies to each) — C3 closes on 3-of-4 majority; C1 closes when verdict-quorum doesn't form and at f=1 via the witness harvest; the residuals (equivocation-promotion, f≥2 candidate-withholding, verdict-equivocation) match across all three.

### Adversarial-byz trigger frequency

Bare OBFT's L_0 adversarial-byz patterns (σ-locked equivocation, h_V=1, etc.) trigger only when byz is the rotation L_0 leader — typically 1/n slots at uniform rotation (25% of byz-controlled slots at f=1 n=4). OBFT+L_Bid equivocation-promotion (and f≥2 candidate-withholding) triggers when byz is among the K rotation leaders (`K/n` under uniform selection, every slot at `K=n`; relay attestation bounds equivocation-promotion back toward `1/n` by barring promotion-by-inflation); verdict-equivocation is available every slot but bites only as a rider on bid divergence. The L_Bid extension therefore increases adversarial-byz trigger frequency relative to bare OBFT's L_0-only surface when `K > 1`, but it no longer assumes standalone all-operator bid envelopes.

### Net trade vs bare OBFT

OBFT+L_Bid pays:
- **+1 BTT total consensus time** at conservative `Δ_minicon = 2·BTT` (in pre-`T_commit` budget — MEV-fetch reduction; post-`T_commit` matches bare OBFT). Loses bare OBFT's advantage at the (BFT_start, BTT) cells listed in the success-mode delta table above (degraded-mesh × moderate-or-later starts) where bare OBFT fits and OBFT+L_Bid doesn't; all other scenarios are unaffected at the budget-fit level.
- **+adversarial-byz exposure at L_Bid** (equivocation-promotion — the genuinely-new `K/n` residual, bounded by relay attestation; f≥2 candidate-withholding; verdict-equivocation as a rider; slot-miss without fall-through; higher trigger frequency than rotation-only patterns). Candidate-withholding is closed at f=1 by the witness harvest.
- **+structural complexity** (`Phase1Bundle` bid metadata, new `KindBidVerdict`, two new slashing rules, mini-consensus protocol step).

In exchange for:
- **Bid-routing value capture** on healthy path (highest-bid eligible rotation-layer block vs fixed rotation-priority V).
- **C1/C2/C3 closure at L_Bid** (vs the naive bid-routing sketch which leaves these open). C1 closes when verdict-quorum doesn't form and at f=1 via the witness harvest; C2/C3 close per the convergence rule; the residual equivocation-promotion carries cryptographic evidence (base leader-equivocation / Rule 7) rather than deadlocking without attribution.

The trade is favorable when MEV bid-routing value-capture upside exceeds the combined cost of (a) the new failure modes' slot-loss rate and (b) the +1 BTT MEV-fetch budget reduction (pre-`T_commit`, at conservative Δ_minicon). For low-MEV slots or deployments with significant mesh degradation pushing scenarios toward the (BFT_start ≥ 800ms, BTT=1000ms) borderline, bare OBFT is the better choice.

## Limits of this comparison

- **Numbers are BTT-count approximations** (3 BTT, 4 BTT, etc.). Production has long tails; ε_3 (~50ms local processing per layer) is treated as small relative to BTT in tabulation. Real implementations may add 50-200ms of constant overhead per round.
- **QBFT round timeout**: QBFT-SSV holds `RT = 2000ms` fixed; QBFT-no-reflood uses per-round BTT-derived timers (R1 = 3·BTT, R≥2 = 4·BTT). The fixed timer trades recovery budget for jitter absorption; the per-round floor recovers fastest but round-changes on any tail.
- **K = n = 4** assumed. At larger n with the same f-bound, K-layer fall-through depth scales (more redundancy at the OBFT family). QBFT's recovery cost scales linearly with K serial round-changes.
- **Bandwidth (small `V`, e.g. attestations ~100 B; cluster-wide healthy path; at each protocol's recommended K-default)**: QBFT ~14 KB across 4 emissions per round; OBFT ~6–8 KB across 1 emission at K=2 default (includes the `sigma_L_witnesses` section ≈ +1.2 KB at K=2 n=4; K=4 up-tier: ~28 KB); OBFTR ~25 KB across 2 emissions per round at K=3 default (R=2 worst case ~50 KB across 4 emissions); 2abOBFT ~20–22 KB across 2 emissions at K=2 default. All four +3-5 KB if L_Bid mini-consensus extension is used (see [each doc's Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension)). Bandwidth is per-emission-count, not per-BTT, so the per-emission sizing tightening does not change these numbers.
- **Bandwidth at larger V**: OBFT's σ_L^V witness section scales with the σ partial size (~145 B/witness), not with V's payload — so OBFT's bandwidth stays close to the small-`V` baseline regardless of V size. Few-large emissions (OBFT) are gentler on the gossipsub mesh than many-small (QBFT) at SSV's KB-range message sizes — per-message overhead (signature verify, dedup, peer-score, mesh forwarding) dominates over per-byte cost.
- **Pre-consensus / block-fetch overhead** is excluded — sits in `[slot_start, BFT_start]` and is ~equal across protocols.
- **Partial network partitions** (some operators have a quorum view, others don't) aren't separately modeled. All four protocols degrade to slot-miss for the partitioned operators; cluster-wide outcome depends on which side has 2f+1 honest.
- **Adversarial-byz trigger frequency** is not modeled. Practical impact depends on byz-leader rotation distribution and bid-equivocation surface for L_Bid extensions (see [docs/OBFT.md / Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) for L_Bid-specific exposure analysis).

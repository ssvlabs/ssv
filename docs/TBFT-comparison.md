# QBFT vs TBFT/TBFTR — failure-mode comparison

This document compares SSV's existing consensus protocol [QBFT](https://github.com/ConsenSys/qbft-formal-spec) against TBFT-family replacements under a sequence of progressively worse operating conditions, holding the application (SSV proposer duty) fixed:

- For `n = 4` clusters: QBFT vs **[TBFT](TBFT.md)** (K=2, primary + backup) vs **[TBFTR](TBFTR.md)** (K=2, with V plaintext + Phase-2 split).
- For `n ≥ 7` clusters: QBFT vs **[TBFTR](TBFTR.md)** (K=f+1, with V plaintext + Phase-2 split).

At n=4 the operator picks between TBFT (lean) and TBFTR(K=2) (fuller). Both cover the **same** marginal-synchrony band at `f = 1` — "≤ 1 of 3 honest missing re-flood" — because TBFTR's witness threshold (`f+1 = 2` distinct Phase-2a σ-signers) coincides with the leaner TBFT's coverage. TBFTR-at-n=4 adds only redundancy within that band (extra σ partials), not extension. At n ≥ 7 only TBFTR is supported; the leaner TBFT-shape protocol's marginal-synchrony coverage stays capped at "1 honest missing re-flood" regardless of `f`, while TBFTR's secondary closure extends to "up to `f` honest missing" — non-zero widening only at `f ≥ 2`.

The goal is to show how time-to-finish and bandwidth degrade as conditions worsen, not to compare protocols in the abstract. The interesting findings are at the failure boundaries, not the healthy case.

## Methodology

For each scenario we report two numbers per protocol, both measured **from start of consensus to a validator-signed value ready for submission**:

- **Time to signed output** — wall-clock from when consensus starts to when a full BLS signature on the agreed value is reconstructed and ready for the beacon node. For QBFT this includes the mandatory post-consensus phase (each operator broadcasts a partial signature on the decided value, 2f+1 are collected, full sig reconstructed) — without it, QBFT has only a decided value, not a signature. For TBFT/TBFTR there is no separate post-consensus phase: partial signatures ride inside the Phase 2 onion (or Phase 2b late-σ broadcasts in TBFTR), and Phase 3's local decryption produces the signed output directly. Excluded from both: pre-consensus (RANDAO, applicable to proposer duty only) and block-fetch — they're ~equal across protocols and would drown out the protocol-level differences we're surfacing.

- **Bandwidth** — all gossipsub deliveries during the same window: from start of consensus to signed output, summed across the cluster.

This scope choice matters because of a structural asymmetry: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus); TBFT/TBFTR fuse the two by embedding partial signatures inside the consensus-bearing onion.** Comparing only the consensus phase would understate QBFT's cost. The numbers below reflect total work to signed output.

Assumptions:

- Healthy gossip "RTT" (one full propagation across the mesh): ~100 ms; congested: ~500 ms.
- BLS verify/aggregate: ~ms-scale, treated as instant.
- QBFT round timeout: 2 s, matching current SSV ([protocol/v2/qbft/roundtimer/timer.go:154-158](protocol/v2/qbft/roundtimer/timer.go:154)).
- "Miss" means the cluster fails to produce a validator signature before the slot deadline — the proposer duty is missed.
- TBFT and TBFTR share the same cryptographic core: leader-authenticated candidates over a structured envelope, leaders publish their own σ-on-V in Phase 1, unified threshold `qV = qEnc = 2f+1` (cryptographic safety), equivocation-to-non-receipt rule. TBFTR additionally carries V plaintext in onions and splits Phase 2 into 2a/2b — extending marginal-synchrony coverage from "1 honest missing re-flood" (what the leader-σ head-start covers) to "up to `f` honest missing", at the cost of bandwidth + latency. Both specs hold safety unconditionally and rely on partial synchrony within the slot window for liveness — same envelope SSV's QBFT relies on per round. See [TBFT.md](TBFT.md) and [TBFTR.md](TBFTR.md) for the specs.

## n=4 cluster (f=1) — QBFT vs TBFT vs TBFTR

At n=4 the SSV operator has a choice between the leaner [TBFT](TBFT.md) and the full [TBFTR](TBFTR.md) (configured with K=2). Both are cryptographically safe and cover the **same** marginal-synchrony band at `f = 1` ("≤ 1 of 3 honest missing re-flood"); the choice is between minimal protocol complexity (TBFT) and within-band redundancy (TBFTR-at-n=4) at the cost of bandwidth + latency. Comparing all three apples-to-apples:

Bandwidth constants for this cluster size: QBFT 1 round + post-consensus ~14 KB; QBFT 2 rounds ~27 KB; **TBFT (K=2) ~21 KB**; **TBFTR (K=2, full-V) ~30 KB** (full V plaintext + Phase-2b overhead). The TBFTR(K=2) numbers below assume the full-V variant — the secondary-closure scenario (6c) needs full V plaintext in onions for peer-onion V-recovery; hash variant trims a small amount of bandwidth at K=2 but disables the secondary closure (in which case 6c reduces to TBFT-shape behavior). See [TBFTR.md](TBFTR.md) "Phase 2a / hash variant caveat" for the trade-off.

| # | Scenario | QBFT | TBFT (K=2) | TBFTR (K=2) |
|---|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **14 KB** | ~250 ms / 21 KB | ~400 ms / 30 KB |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 14 KB | ~600 ms / 21 KB | ~750 ms / 30 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 27 KB | **~250 ms** / 21 KB | **~400 ms** / 30 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 27 KB | **~250 ms** / 21 KB ¹ | **~400 ms** / 30 KB ¹ |
| **5** | f offline incl top leader (=1 offline) | ~3.0 s / 27 KB | **~250 ms** / 21 KB | **~400 ms** / 30 KB |
| **6a** | Byzantine in worst-case leader position, passive (no votes) | ~3.0 s / 27 KB | **~250 ms** / 21 KB | **~400 ms** / 30 KB |
| **6b** | Byzantine layer-0 leader actively griefs (selective delivery + dark on votes) | ~3.0 s / 27 KB ² | **~250 ms** / 21 KB ³ | **~400 ms** / 30 KB ³ |
| **6c** | Same as 6b, plus marginal breakdown (re-flood misses ≥2 of 3 honest) | ~3.0 s / 27 KB ² | miss ⁴ | miss ⁵ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss |

¹ Equivocation triggers the equivocation-to-non-receipt rule cluster-wide — falls through cleanly to the backup.
<br>
² QBFT recovers via round change.
<br>
³ Under partial synchrony with the candidate-acceptance cutoff `T_candidate_accept = T_commit − (D + δ)` enforced, gossipsub re-flooding propagates the leader's bundle to all 3 honest before *their* cutoffs; all 3 sign σ in Phase 2, σ-quorum reaches `qV = 3` cleanly. Both TBFT and TBFTR(K=2) close this scenario via the same primary mechanism.
<br>
⁴ TBFT relies on the primary closure only; if re-flooding misses the budget, the σ-pool fragments and the slot misses (no safety violation).
<br>
⁵ **TBFTR(K=2) also misses at n=4.** The secondary closure's witness threshold (≥ `f+1 = 2` distinct Phase-2a σ-signers on V) coincides with TBFT's "≤ 1 honest missing" bound — when ≥ 2 honest miss re-flood, only 1 honest signed Phase-2a σ < 2, the witness threshold isn't met, late σ is blocked, and the slot misses just like TBFT. The full-V variant doesn't extend the marginal band at `f = 1`; widening starts at `f ≥ 2` (n ≥ 7). See [TBFTR.md](TBFTR.md) "Liveness / Comparison with a leaner (TBFT-shape) protocol" for the per-`f` widening table.

### Reading the n=4 table

- **Scenarios 3, 5, 6a, 6b all collapse to clean outcomes for both TBFT and TBFTR under partial synchrony with the cutoff enforced.** With f=1, all of "top leader silent" / "f offline including top" / "f byz passive" / "active byz-leader grief" finish quickly via fall-through to backup or via the σ-quorum reaching `qV = 3` through the leader's Phase-1 σ + 2 honest σ partials. The differentiator vs QBFT is variance: QBFT's failure path is timeout-driven (~3.0 s), TBFT/TBFTR's is flat (~250–400 ms).
- **Scenario 6c at n=4: both TBFT and TBFTR miss.** The witness threshold's `f+1 = 2` precondition forces at least 2 honest to have signed Phase-2a σ on V before late σ is allowed. When ≥ 2 of 3 honest miss re-flood, only 1 honest signs Phase-2a σ — the threshold isn't met, late σ is blocked, and TBFTR's secondary closure can't help. **TBFTR-at-n=4 doesn't earn its premium for marginal-synchrony robustness** — its coverage band coincides with TBFT's; what it adds at this size is redundancy within the band (extra σ partials when both protocols would already succeed). The premium is only justified at this size if that within-band redundancy specifically matters in production. Marginal-coverage extension (the original motivation for the secondary closure) starts at `f ≥ 2`, which is the n ≥ 7 regime — see the n=7 table below.
- **Scenario 1 favors QBFT on bandwidth** (14 KB vs 21 KB vs 30 KB). QBFT's round-1-success path is genuinely the cheapest in bytes. TBFT/TBFTR trade extra bandwidth for cryptographic safety + flat failure-mode behavior.
- **Scenario 2 (slow network, no failures) is the cleanest comparison of pure protocol latency.** TBFT/TBFTR finish in ~600–750 ms vs QBFT's ~2.0 s — multiple-× advantage from 1-RTT vs 3-RTT structure. Inside MEV's 4 s budget this is the difference between making the relay cutoff and not.
- **Scenario 3 (top leader silent) is the proposer-duty MEV killer for QBFT.** ~3.0 s of consensus puts the validator on the wrong side of the 4 s relay cutoff once you add ~1.5 s of pre-consensus + block-fetch. TBFT/TBFTR just use the backup, well within the cutoff.

## n=7 cluster (f=2) — QBFT vs TBFTR

This is the cluster size where TBFTR's distinct contribution starts mattering broadly. A TBFT-shape protocol's marginal-synchrony coverage caps at "1 honest missing re-flood" regardless of `f` — the leader-σ head-start adds exactly one σ partial to the σ pool. At `f = 1` (n=4), 1-of-3 honest missing covers most plausibly-degraded conditions. At `f ≥ 2`, the practical marginal band reaches "up to `f` honest missing" (multiple slow links are increasingly likely as `n` grows), and the TBFT-shape σ count caps at `f+2 < qV = 2f+1` once `f ≥ 2`, missing slots in the 2-or-more-honest-missing band. TBFTR's secondary closure pushes coverage out to the full `f`-honest-missing limit, which is what makes it the appropriate choice at `n ≥ 7`.

Bandwidth constants: QBFT 1 round + post-consensus ~37 KB; QBFT round-change adds ~12 KB; QBFT 2 rounds ~76 KB; QBFT 3 rounds ~115 KB. TBFTR has two operational modes:

- **TBFTR (K=3, hash) ~108 KB** — practical bandwidth; primary closure only (gossipsub re-flooding under partial synchrony). Disables Phase-2a peer-onion V-recovery — hash variant carries only `hash(V)` at non-leader layers, so honest operators that miss V via Phase 1 cannot recover via peer onions.
- **TBFTR (K=3, full-V) ~325 KB** — roughly K× bandwidth (each layer carries full V plaintext); enables the secondary closure (Phase-2a peer-onion V-recovery + Phase-2b late σ) that extends marginal-synchrony coverage from "≤ 1 of 5 honest missing re-flood" (what the leader-σ head-start covers) to "≤ 2 of 5 honest missing" (the witness-threshold-bounded secondary closure at `f = 2`).

Both variants are cryptographically safe; they differ in marginal-synchrony liveness vs bandwidth. Under partial synchrony with the cutoff enforced — and through the moderate-marginal band (≤ 1 of 5 honest missing re-flood) — scenarios 1–6b complete identically in both variants (primary closure + leader-σ head-start suffice). Scenario 6c — extended-marginal breakdown (2 of 5 honest missing re-flood, within `f = 2` bound) — is where the variants diverge: full-V's secondary closure handles it, hash variant misses.

| # | Scenario | QBFT | TBFTR (K=3, hash) | TBFTR (K=3, full-V) |
|---|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **37 KB** | ~400 ms / 108 KB | ~400 ms / 325 KB |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 37 KB | ~750 ms / 108 KB | ~750 ms / 325 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 76 KB | **~400 ms** / 108 KB | **~400 ms** / 325 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 76 KB | **~400 ms** / 108 KB ¹ | **~400 ms** / 325 KB ¹ |
| **5** | f operators offline, including top leader | ~3.0 s / 76 KB | **~400 ms** / 108 KB | **~400 ms** / 325 KB |
| **6a** | f byzantine in worst-case leader positions, passive (no votes) | ~5.0 s / 115 KB | **~400 ms** / 108 KB | **~400 ms** / 325 KB |
| **6b** | Byzantine layer-0 leader actively griefs (selective delivery + dark on votes), bundle released with re-flood headroom | ~3.0 s / 76 KB ² | **~400 ms** / 108 KB ³ | **~400 ms** / 325 KB ³ |
| **6b'** | Same as 6b, but bundle released *at* `T_candidate_accept` edge (re-flood lands inside worst-case skew window) | ~3.0 s / 76 KB ² | miss ⁶ | **~400 ms** / 325 KB ⁵ |
| **6c** | Same as 6b, plus marginal breakdown within `f = 2` bound (Phase-1 re-flood misses 2 of 5 honest) | ~3.0 s / 76 KB ² | miss ⁴ | **~400 ms** / 325 KB ⁵ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss |

¹ Equivocation triggers the equivocation-to-non-receipt rule cluster-wide — falls through cleanly to the next layer.
<br>
² QBFT recovers via round change — round 1 fails to reach prepare-quorum, new leader elected in round 2.
<br>
³ Under partial synchrony with `T_candidate_accept` enforced AND byz releasing the bundle with at least `D + δ` headroom before the cutoff, gossipsub re-flooding propagates the leader's Phase-1 bundle to all 2f+1 honest before *their* cutoffs (primary closure). σ-quorum reaches via the 2f+1 onion partials + leader's Phase-1 σ = 2f+2 ≥ qV = 5. Both variants close this case the same way; Phase-2a peer-onion recovery is dormant because re-flooding already did the work. (The "byz releases at the cutoff edge" sub-case is row 6b' below — that's where the variants diverge under partial synchrony.)
<br>
⁴ Hash variant has only the primary closure (re-flooding). When re-flooding misses the budget, peer onions carry only `hash(V)` at non-leader layers — honest operators that didn't get V via Phase 1 cannot recover. Their σ-pool fragments and the slot misses (no safety violation; safety is cryptographic).
<br>
⁵ **Full-V variant succeeds via the secondary closure.** Phase-2a peer onions carry full V plaintext, so honest operators that missed Phase-1 re-flooding recover V via peer onions and broadcast late σ in Phase 2b. σ-quorum reaches at qV = 5: f+1 onion partials + f late-σ partials + 1 leader Phase-1 σ = 2f+2. See [TBFTR.md](TBFTR.md) "Fault tolerance / Liveness" for the full breakdown.
<br>
⁶ **Byzantine-leader-at-cutoff edge.** Byz releases the bundle to f+1 honest at exactly their `T_candidate_accept`; the f+1 accept and re-flood, but worst-case clock skew leaves the re-flood landing past the remaining f honest's own cutoffs. Those f peers hold leader auth as late-retention but cannot sign Phase-2a σ. Hash variant: σ pool = f+1 Phase-2a + 1 leader = f+2 = 4 < qV = 5; NR pool < qEnc. **Slot misses under partial synchrony — this is a residual hole the hash variant accepts at f ≥ 2.** Full-V variant: secondary closure recovers via peer-onion V-recovery + late σ; the witness threshold is met (f+1 honest signed Phase-2a σ). See [TBFTR.md](TBFTR.md) "Phase 1 / Bundle propagation" for the clock-skew analysis.

### Reading the n=7 table

- **TBFTR's flat ~400 ms across in-bound failure modes (scenarios 1–6b) in both variants** is the headline for the common case. The +Δ_2b window (~150 ms) over a TBFT-shape protocol pays for the byzantine-leader grief closure under partial synchrony. QBFT pays per round change — passive failures cost ~3.0 s, the worst-case 6a costs ~5.0 s.
- **Scenario 6a (passive byzantine in worst-leader positions) is where TBFTR's K=f+1 coverage matters most.** TBFTR guarantees ≥1 honest leader in the top-K by construction; the worst case still completes in ~400 ms when byz are passive. QBFT pays for two round changes here.
- **Scenario 6b (active byzantine grief, with re-flood headroom) is closed at n=7 in both variants under partial synchrony**, via gossipsub re-flooding. QBFT also recovers via round change, at higher latency.
- **Scenario 6b' (byzantine-leader-at-cutoff edge) splits the variants under partial synchrony.** When byz times the bundle release exactly at honest receivers' `T_candidate_accept` (within worst-case clock skew), re-flood lands past the remaining f honest's cutoffs and only f+1 honest sign Phase-2a σ. Hash variant misses (σ pool = f+2 < qV at f=2); full-V's secondary closure handles it (peer-onion V-recovery + late σ, witness threshold met). This sits within partial synchrony, not marginal — propagation is bounded, byz just exploits clock-skew arithmetic at the cutoff edge. The cluster's residual exposure to 6b' depends on clock-skew bounds and how often byz can time the release this precisely.
- **Scenario 6c is the variant-differentiator.** Aggressive-marginal breakdown means propagation degrades enough that re-flooding misses ≥2 of `2f+1` honest within `D + δ` — gossipsub-wide congestion or multiple slow mesh edges, not just one slow link. Phase-2a propagation still completes within its (longer) window. The full-V variant's Phase-2a peer-onion V-recovery + Phase-2b late σ closes the gap; the hash variant has nothing in reserve and misses. **Whether 6c is worth the bandwidth premium (~3× over hash) depends on production data: how often does propagation degrade to the multi-honest-pair-slow regime?**
- **Bandwidth: TBFTR(hash) is consistently larger** than QBFT in healthy-case (108 KB vs 37 KB) but stays flat across failures while QBFT scales toward 115 KB in worst-case 6a. TBFTR(full-V) is much larger (~325 KB) and only earns its premium when the secondary closure's coverage extension matters (i.e., 2-or-more honest missing re-flood within the `f` bound). The gap in failure scenarios narrows; the gap in healthy case stays open.
- **Scenario 1 still favors QBFT on bandwidth.** TBFTR's V-plaintext + composition premium is real. The trade is paying for guaranteed flat performance across the failure modes (both variants) plus optional secondary-closure coverage extension (full-V only).

### n=10, n=13 — same shape, scaled K

Bandwidth constants (hash variant; full-V variant scales as roughly `K × |V| × n` and is much larger):

| Cluster | f | K | TBFTR (hash, worst case) | TBFTR (full-V, approx) | QBFT 1 round | QBFT 2 rounds | QBFT 3 rounds |
|---|---|---|---|---|---|---|---|
| n=10 | 3 | 4 | ~253 KB | ~1 MB | ~50 KB | ~100 KB | ~150 KB |
| n=13 | 4 | 5 | ~497 KB | ~2.5 MB | ~85 KB | ~170 KB | ~255 KB |

The scenario-by-scenario shape is the same as n=7 — TBFTR finishes in flat ~400–500 ms across all in-bound failures (1–6b) under partial synchrony in **both** variants, QBFT degrades per round change. The bandwidth gap widens with n (TBFTR's `K · n²` term grows faster than QBFT's per-round constant). The variant differentiator (scenario 6c, marginal breakdown within the `f`-honest-missing bound) applies the same way at n=10/13 as at n=7: full-V variant succeeds via Phase-2a peer-onion recovery, hash variant misses.

**Practical reality at n ≥ 10**: the full-V variant is too expensive to be a real default. At n=10 it's ~1 MB cluster-wide per slot; at n=13 ~2.5 MB. Hash variant is the typical deployment, which means **operating at n ≥ 10 effectively gives up the secondary closure** — the cluster's marginal-synchrony coverage reduces to "1 honest missing re-flood" (the leader-σ head-start, same as TBFT at n=4), even though the practical marginal band at f ≥ 3 reaches "up to `f` honest missing". If marginal breakdowns within `[2, f]` honest missing are observed in production, the engineering options are:

- Switch to full-V variant at the cluster (accept the bandwidth cost).
- Tighten `T_candidate_accept` by lowering `D + δ` toward the actual measured propagation P99/P999 — buys margin in the primary closure at the cost of slightly higher false-miss rate on jitter.
- Accept the marginal miss rate within `[2, f]` honest missing as a deployment cost and pay it via observability (track miss rate; investigate when it spikes).
- Stay with TBFT-shape (no V plaintext, no Phase-2b composition) at n=13 and accept the residual marginal miss window — `[2, f] = [2, 4]` honest-missing-reflood cases that the leaner protocol can't cover. Active griefing at n=13 in the modeled scenario would cost ~3 missed slots out of every byzantine-layer-0-leader slot multiplied by the precision-attack hit rate. Whether this is acceptable depends on operator economics.

## Cross-cluster takeaway

- **The protocols differ in shape, not just in performance.** QBFT decides on a value first, then runs a separate post-consensus phase to sign it. TBFT/TBFTR embed partial signatures inside the consensus-bearing onion (and TBFTR adds late-σ broadcasts in Phase 2b) so that consensus and signing happen in the same broadcast window. Most numerical advantages above trace back to this structural difference.

- **TBFT/TBFTR have constant-cost handling for *all* in-bound failures under partial synchrony** at their respective cluster sizes. At every cluster size, the *primary* closure is gossipsub re-flooding of the leader's Phase-1 bundle (under `T_candidate_accept`); this works in both hash and full-V variants of TBFTR and is what TBFT relies on at n=4. Under marginal synchrony (re-flooding doesn't reach all honest), the leader-σ head-start covers the *moderate* band (1 honest missing re-flood) in all variants. The *secondary* closure (Phase-2a peer-onion V-recovery + Phase-2b late σ) extends coverage to the *aggressive* band (up to `f` honest missing re-flood) — but only in TBFTR's full-V variant, which is practical at n ≤ 7 and impractical at n ≥ 10 due to bandwidth. QBFT is fundamentally different in shape: failures cost real time (~2 s per round change) and real bandwidth (~12 KB plus another full round per round change). All three protocols share the same partial-synchrony assumption for liveness; safety in TBFT/TBFTR is cryptographic and unconditional via `qEnc = qV = 2f+1`.

- **TBFT (n=4) and TBFTR (n≥4) close byzantine-leader selective-delivery grief at all SSV-supported cluster sizes under partial synchrony.** Closure mechanism is gossipsub re-flooding of the leader's Phase-1 bundle (primary closure), the same in TBFT and in both TBFTR variants. QBFT also handles the byzantine-leader-grief case (via round change), but with larger latency cost.

- **Bandwidth ordering depends on conditions, not just on protocol.** QBFT is cheapest in the healthy case across all cluster sizes. TBFT/TBFTR become cheaper in time and competitive in bandwidth once round changes accumulate, with the crossover happening earlier at larger cluster sizes.

- **The QBFT vs TBFT/TBFTR trade is variance and failure-mode shape**, not just average-case performance. QBFT optimizes for the common case (cheap round 1) at the cost of expensive failure recovery. TBFT/TBFTR spend more on every slot in exchange for flat behavior under all in-bound failure conditions, including active byzantine-leader grief. Which is "better" depends on how often the network is in a degraded state, how badly missed slots hurt, and how realistic active byz-leader grief is in practice — empirical questions that need production data to answer.

| Cluster size | f | Recommended TBFT-family protocol |
|---|---|---|
| n=4 | 1 | **TBFT** as default (~21 KB) — primary closure via leader-σ + gossipsub re-flooding; covers up to "1 of 3 honest missing re-flood". **TBFTR(K=2)** (~30 KB full-V or smaller hash) covers the **same** marginal-synchrony band at this `f` (witness threshold caps secondary closure at the same bound); the only thing it adds at n=4 is redundancy in that band, not an extension. Pick TBFTR-at-n=4 only if that within-band redundancy specifically matters. |
| n=7 | 2 | **TBFTR (K=3, hash)** for typical deployment (~108 KB) — primary closure + leader-σ head-start covers up to "1 of 5 honest missing re-flood". **TBFTR (K=3, full-V)** (~325 KB) if extending coverage to "up to 2 of 5 honest missing" (the secondary closure's `f`-bound at `f = 2`) justifies the bandwidth premium. |
| n=10 | 3 | **TBFTR (K=4, hash)** practical (~253 KB) — full-V variant (~1 MB) is too expensive for typical deployment, so the cluster gives up the secondary closure. |
| n=13 | 4 | **TBFTR (K=5, hash)** (~497 KB; bandwidth tight) — full-V variant (~2.5 MB) is impractical. Same caveat as n=10: secondary closure unavailable; cluster relies on partial-synchrony primary closure. |

## Limits of this comparison

- These are consensus-to-signed-output numbers. End-to-end time has another ~1.5 s of pre-consensus + block-fetch sitting on top of every row, so the *relative* gaps matter more than the absolute milliseconds.

- The 2 s QBFT round timeout is current SSV. Tightening it would shrink scenarios 3-6's QBFT times but raise the false-positive round-change rate under normal jitter (a known trade-off the team has already tuned).

- Numbers are approximate constants; real production has long tails. For small n=4 clusters at low duty load these gaps may not matter; at n=10/13 with frequent slots they very much do.

- Partial network partitions (where some operators have a quorum view and others don't) aren't separately modeled here. TBFT/TBFTR degrade to "missed slot by some operators" cleanly under partitions; QBFT's view-change behavior is more nuanced and would warrant its own analysis.

- The "byzantine in worst-case leader positions" scenarios (6a, 6b) assume leader rotation is *not* byzantine-aware. Byzantine-aware rotation (VRF-based, distinct sub-quorums per role) would reduce the probability of hitting these worst cases without changing the per-scenario bandwidth or time numbers.

- TBFTR has two operational variants — hash and full-V — and the comparison tables above split scenarios where the variants diverge (notably 6c at n=7 and the n=10/13 discussion). The hash variant cuts bandwidth from `~K × |V| × n²` to `~K × 32 B + |V|` per onion, but disables Phase-2a peer-onion V-recovery (the secondary closure mechanism). For scenarios under partial synchrony plus moderate marginal (≤1 honest missing re-flood) where the primary closure + leader-σ head-start suffice, both variants behave identically; for marginal scenarios within `[2, f]` honest missing — the band where the secondary closure adds extension over the leaner protocol — only full-V succeeds. At `f = 1` (n=4) the band is empty (witness threshold coincides with leaner coverage), so hash and full-V are equivalent in marginal coverage there. See [TBFTR.md](TBFTR.md) "Phase 2a" hash-variant caveat for the trade-off.

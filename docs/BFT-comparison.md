# QBFT vs TBFT/TBFTR — comparison for SSV proposer duty

This doc compares SSV's existing consensus protocol [QBFT](https://github.com/ConsenSys/qbft-formal-spec) against the TBFT-family proposals across both common-case and failure-mode operating conditions. The application is held fixed to **SSV's Ethereum proposer duty** (4s relay submission cutoff). The scope is intentionally narrow: scenario-by-scenario observable cost, with all protocol mechanics deferred to the design docs.

- For `n = 4` clusters: QBFT vs **[TBFT](TBFT.md)** (K=2) vs **[TBFTR](TBFTR.md)** (K=2).
- For `n ≥ 7` clusters: QBFT vs **[TBFTR](TBFTR.md)** at the default `K = f+1`, plus **TBFTR(K=2)** as a comparison configuration matching QBFT's effective 2-round budget.

For protocol details — leader-authenticated candidates over a structured envelope, threshold IBE primitive, equivocation-to-NR rule, primary/secondary closure mechanics, and so on — see [TBFT.md](TBFT.md) and [TBFTR.md](TBFTR.md).

## Scope and assumptions

- **Application**: SSV Ethereum proposer duty. End-to-end budget: ~4 s from `slot_start` (relay submission cutoff). Pre-consensus + block-fetch consumes ~1.5 s. Numbers below measure **consensus-to-signed-output** (full BLS signature on the agreed value, ready for downstream submission), excluding pre-consensus and block-fetch — those are ~equal across protocols and would drown out protocol-level differences.
- **QBFT round-timeout = 2 s** (current SSV setting, cf. [protocol/v2/qbft/roundtimer/timer.go](../protocol/v2/qbft/roundtimer/timer.go)). With the 4 s relay cutoff and ~1.5 s pre-consensus + fetch overhead, **QBFT has room for at most 2 rounds within the proposer-duty timing budget** — round-1 timeout (2 s) + round-2 success (~750 ms) ≈ 2.75 s consensus, fitting once added on top of pre-consensus + fetch. Failure scenarios requiring round 3 or beyond are reported as **miss**: the cluster doesn't produce a signed value before the relay cutoff, regardless of consensus correctness.
- **Healthy gossip RTT** (one full propagation across the mesh): ~100 ms; **congested**: ~500 ms.
- BLS verify/aggregate: ms-scale, treated as instant.
- "Miss" = cluster fails to produce a validator signature on the proposed block before the relay cutoff. Slot lost; no safety violation in any of the TBFT-family protocols (safety is cryptographic).
- Numbers are approximate constants; production has long tails. The *shape* of degradation matters more than absolute milliseconds.

## Closure mechanisms primer

TBFT/TBFTR's byzantine-leader-grief resistance is described in their respective design docs in terms of two layered closure mechanisms; this doc references them frequently.

- **Primary closure** (TBFT and TBFTR, both variants, both protocols): under partial synchrony with the candidate-acceptance cutoff `T_candidate_accept = T_commit − (D + δ)` honored, gossipsub re-flooding propagates the leader's Phase-1 bundle to all `2f+1` honest before *their* cutoffs (when byz releases the bundle with at least `D + δ` of headroom). σ-quorum reaches via the leader's Phase-1 σ + 2f honest Phase-2(a) σ = 2f+1. This handles the common-case byzantine-leader grief at any `f`.
- **Secondary closure** (TBFTR full-V variant only): when re-flooding falls short for some honest (≤ f honest miss the bundle by their Phase-2a cutoff), Phase-2a peer-onion V-recovery surfaces the value via plaintext V in onions, and Phase-2b late σ broadcasts collect σ partials from operators that recovered V late. Gated by the witness threshold (≥ `f+1` distinct Phase-2a σ-signers on the recovered V). Extends marginal-synchrony coverage from "≤ 1 honest missing re-flood" (primary alone) to "≤ `f` honest missing re-flood" (primary + secondary) — non-zero widening only at `f ≥ 2`. Hash variant of TBFTR disables secondary closure.

## Methodology

For each scenario, two numbers per protocol:

- **Time to signed output** — wall-clock from start of consensus to a full BLS signature on the agreed value being available for downstream submission. For QBFT this includes the post-consensus partial-sig collection phase (without it, QBFT has only a decided value, not a signature). For TBFT/TBFTR the partial sigs ride inside Phase 2 (and Phase 2b for TBFTR), so Phase 3's local decryption produces the signed output directly — no separate post-consensus phase.
- **Bandwidth** — gossipsub deliveries during the same window, summed across the cluster.

This scope choice matters because of a structural asymmetry: **QBFT separates consensus from signing; TBFT/TBFTR fuse them.** Comparing only the consensus phase would understate QBFT's cost; the numbers below reflect total work to signed output.

Columns in the scenario tables are ordered left-to-right from slowest (QBFT) to fastest (the partial-sigs-only theoretical floor) — rightward direction is "improvement direction".

## Common case (healthy operation)

Before failure-mode analysis, the headline finding: **TBFT and TBFTR are multiple-× faster than QBFT in healthy network conditions**, due to the structural difference between QBFT's 3-RTT consensus + 1-RTT post-consensus path (4 RTTs minimum) and TBFT/TBFTR's 1–2 RTT fused-signing path. Bandwidth is a wash to slightly QBFT-favored in the healthy case — that's the cost TBFT/TBFTR pay for guaranteed flat behavior under all in-bound failure modes.

| Cluster | Scenario | QBFT | TBFTR (default K) | TBFT | Partial-sigs-only ⁰ |
|---|---|---|---|---|---|
| n=4 (f=1) | All honest, healthy | ~750 ms / 14 KB | ~500 ms / 30 KB | **~250 ms / 21 KB** | ~150 ms / ~1 KB |
| n=4 (f=1) | All honest, congested ⁶ | ~2.0 s / 14 KB | ~750 ms / 30 KB | **~600 ms / 21 KB** | ~550 ms / ~1 KB |
| n=7 (f=2) | All honest, healthy | ~750 ms / 37 KB | **~500 ms / 108 KB** | n/a | ~150 ms / ~1.5 KB |
| n=7 (f=2) | All honest, congested ⁶ | ~2.0 s / 37 KB | **~750 ms / 108 KB** | n/a | ~550 ms / ~1.5 KB |
| n=10 (f=3) | All honest, healthy | ~750 ms / 50 KB | **~500 ms / 253 KB** (hash) | n/a | ~150 ms / ~2 KB |
| n=13 (f=4) | All honest, healthy | ~750 ms / 85 KB | **~500 ms / 497 KB** (hash) | n/a | ~150 ms / ~2.5 KB |

Reading the common-case table:

- **TBFT at n=4 is ~3× faster than QBFT** in healthy networks (~250 ms vs ~750 ms). Inside MEV's 4 s relay cutoff, that's the difference between making the relay submission and not — pre-consensus + block-fetch already eats ~1.5 s.
- **TBFTR is ~1.5× faster than QBFT** in healthy networks across cluster sizes (~500 ms vs ~750 ms), despite a larger bandwidth footprint.
- **Bandwidth ordering favors QBFT in the healthy case** at all cluster sizes. Both TBFT/TBFTR pay extra healthy-case bandwidth for guaranteed flat-failure behavior; the trade is paid up-front rather than amortized over failure recovery.
- **Congested-network gap widens to multiple-×** because each QBFT RTT compounds.
- **The partial-sigs-only baseline** (⁰) is a theoretical floor: a hypothetical protocol that bypasses consensus entirely and only collects `2f+1` partial sigs on a *pre-agreed* value. TBFT lands closest to this floor; TBFTR is +Δ_2b away; QBFT is multiple round-trips away. Defined only when an agreed value can exist without consensus — n/a in scenarios involving leader silence, equivocation, or selective delivery.

## Failure modes

Two scenario tables: one per cluster size we model in detail (n=4 and n=7), plus a brief note on n=10 / n=13.

### n=4 cluster (f=1) — QBFT vs TBFTR(K=2) vs TBFT

At n=4, the SSV operator picks between TBFT (lean) and TBFTR(K=2) (fuller); both cover the **same** marginal-synchrony band at this `f` ("≤ 1 of 3 honest missing re-flood"). TBFTR-at-n=4 adds only within-band redundancy, not coverage extension — see [TBFTR.md Appendix A.1](TBFTR.md).

| # | Scenario | QBFT | TBFTR (K=2) | TBFT | Partial-sigs-only ⁰ |
|---|---|---|---|---|---|
| **1** | All honest, healthy | ~750 ms / 14 KB | ~500 ms / 30 KB | ~250 ms / 21 KB | ~150 ms / 1 KB |
| **2** | All honest, congested ⁶ | ~2.0 s / 14 KB | ~750 ms / 30 KB | ~600 ms / 21 KB | ~550 ms / 1 KB |
| **3** | Top leader silent (offline / refuses to propose) | ~3.0 s / 27 KB ¹ | **~500 ms** / 30 KB ³ | **~250 ms** / 21 KB ³ | n/a |
| **4** | Top leader byzantine equivocating | ~3.0 s / 27 KB ¹ | **~500 ms** / 30 KB ²,³ | **~250 ms** / 21 KB ²,³ | n/a |
| **5** | f offline including top leader | ~3.0 s / 27 KB ¹ | **~500 ms** / 30 KB ³ | **~250 ms** / 21 KB ³ | n/a |
| **6a** | f byz in worst-case leader position, passive (no votes) | ~3.0 s / 27 KB ¹ | **~500 ms** / 30 KB ³ | **~250 ms** / 21 KB ³ | n/a |
| **6b** | Byz layer-0 leader actively griefs (selective delivery + dark on votes), bundle released with re-flood headroom | ~3.0 s / 27 KB ¹ | **~500 ms** / 30 KB ³ | **~250 ms** / 21 KB ³ | n/a |
| **6c** | Same as 6b, plus marginal breakdown (re-flood misses ≥ 2 of 3 honest) — **transient ¹¹** | ~3.0 s / 27 KB ¹,¹¹ | **miss** ⁴,¹¹ | **miss** ⁴,¹¹ | n/a |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss | miss |

**Footnotes:**

- ⁰ **Partial-sigs-only**: theoretical baseline — protocol that collects `2f+1` partial sigs on a *pre-agreed* value with no consensus. Defined only when an agreed value can exist without consensus; n/a in scenarios involving byzantine value-fragmentation.
- ¹ **QBFT round-2 recovery**: round-1 fails (timeout, equivocation, or invalid proposal), round-2 elects a new leader and succeeds. Round-1 timeout 2 s + round-2 ~750 ms = ~3.0 s consensus; bandwidth = ~14 KB (round 1) + ~12 KB (round-change) + ~14 KB (round 2) ≈ ~27 KB after dedup.
- ² **Equivocation handling**: equivocation triggers the equivocation-to-NR rule cluster-wide — falls through cleanly to the backup leader. See [TBFT.md](TBFT.md) / [TBFTR.md](TBFTR.md) "Equivocation handling".
- ³ **Primary closure**: gossipsub re-flooding under `T_candidate_accept` propagates the leader's Phase-1 bundle to all `2f+1` honest before their cutoffs; σ-quorum reaches via leader's Phase-1 σ + 2f honest Phase-2 σ = 2f+1. See "Closure mechanisms primer" above.
- ⁴ **Aggressive marginal at n=4 is uncoverable** by either protocol: TBFT relies on primary closure alone; TBFTR's witness threshold (`f+1 = 2` distinct Phase-2a σ-signers) coincides with TBFT's "≤ 1 honest missing re-flood" bound at this size. When ≥ 2 of 3 honest miss re-flood, only 1 honest signs Phase-2a σ, witness threshold not met, late σ blocked → slot misses. **No safety violation** (safety is cryptographic).
- ⁶ **Congested 500 ms RTT** assumes the cluster's pre-configured Phase-2 windows (Δ_2 ≈ 500 ms for TBFT; Δ_2a ≈ Δ_2b ≈ 250 ms for TBFTR) absorb the spike via jitter slack. If 500 ms is the steady-state envelope, windows would need re-sizing per [TBFT.md](TBFT.md) / [TBFTR.md](TBFTR.md) "Practical caveats / Deadline coordination".
- ¹¹ **Transient-breakdown asymmetry in 6c.** The 6c outcome row is conditional on the marginal breakdown being **transient** — short-lived enough to pass within QBFT's round-1 timeout (~2 s) so that round 2 propagates normally. Under this implicit assumption, QBFT round-2 succeeds (~3.0 s consensus); TBFT/TBFTR miss because their **tight Phase-2 deadlines** (~250-500 ms) close before continued gossipsub re-flooding can deliver V to the missing honest. **The asymmetry is in deadline configuration, not protocol fundamentals**: with TBFT/TBFTR's Phase-2 deadlines extended to match QBFT's effective ~2 s recovery window, the same continued re-flooding would also reach the missing honest in time and the slot would close. The tight deadlines are a deliberate latency-favoring trade-off — TBFT's healthy-case ~250 ms vs QBFT's ~750 ms is exactly what deadlines that don't wait for transient breakdowns buy. Sub-cases:
  - *Transient-breakdown* (passes within ~2 s): row as shown — QBFT survives via wait-and-retry; TBFT/TBFTR miss with current deadlines, would survive with deadlines extended to match QBFT's recovery window.
  - *Sustained-breakdown* (>2 s): QBFT round-2 also fails (round-3 needed → miss under proposer-duty budget); TBFT/TBFTR miss; **all three miss** regardless of deadline configuration.

**Reading the n=4 failure-mode table:**

- **Scenarios 3-6b all close cleanly for TBFT and TBFTR in flat ~250-500 ms**, while QBFT pays ~3.0 s consensus per round-1 failure. Once pre-consensus + fetch (~1.5 s) is added, QBFT's ~3.0 s consensus path puts the validator on the wrong side of the 4 s relay cutoff — proposer duty effectively missed in any of these scenarios.
- **Scenario 6c**: under transient breakdown, QBFT survives via round-2 (network recovers between rounds); TBFT/TBFTR's tight deadlines miss the recovery window. Under sustained breakdown all three miss. **The QBFT advantage in this row is a deadline-configuration trade-off, not a protocol-fundamental edge** — see footnote ¹¹. **TBFTR-at-n=4 doesn't earn its bandwidth/latency premium** for marginal-synchrony robustness even relative to TBFT (TBFTR's secondary closure doesn't extend coverage at `f = 1` — witness threshold caps at the same bound TBFT covers via the leader-σ head-start); pick TBFT unless within-band redundancy specifically matters.

### n=7 cluster (f=2) — QBFT vs TBFTR(K=3) vs TBFTR(K=2)

At n=7, only TBFTR is supported as a TBFT-family option. The table includes both `K = f+1 = 3` (the recommended default) and `K = 2` (a comparison configuration matching QBFT's effective 2-round budget for apples-to-apples comparison; not the recommended deployment, since `K < f+1` means byzantine can hold all top-K leader slots).

For TBFTR variants (hash vs full-V): rows where they diverge are noted explicitly (`(hash) / (full-V)`); rows without that annotation behave identically. See [TBFTR.md](TBFTR.md) "Phase 2a" hash-variant caveat for the trade-off.

| # | Scenario | QBFT | TBFTR (K=3, default) | TBFTR (K=2) ⁷ | Partial-sigs-only ⁰ |
|---|---|---|---|---|---|
| **1** | All honest, healthy | ~750 ms / 37 KB | ~500 ms / 108 KB | ~500 ms / ~70 KB | ~150 ms / 1.5 KB |
| **2** | All honest, congested ⁶ | ~2.0 s / 37 KB | ~750 ms / 108 KB | ~750 ms / ~70 KB | ~550 ms / 1.5 KB |
| **3** | Top leader silent | ~3.0 s / 76 KB ¹ | **~500 ms** / 108 KB ³ | **~500 ms** / ~70 KB ³ | n/a |
| **4** | Top leader byz equivocating | ~3.0 s / 76 KB ¹ | **~500 ms** / 108 KB ²,³ | **~500 ms** / ~70 KB ²,³ | n/a |
| **5** | f operators offline, including top leader | ~3.0 s / 76 KB ¹ | **~500 ms** / 108 KB ³ | **~500 ms** / ~70 KB ³,⁸ | n/a |
| **6a** | f byz in worst-case leader positions, passive (no votes) | **miss** ⁹ (~76 KB consumed) | **~500 ms** / 108 KB ³ | **miss** ⁸ | n/a |
| **6b** | Byz layer-0 leader actively griefs, bundle released with re-flood headroom | ~3.0 s / 76 KB ¹ | **~500 ms** / 108 KB ³ | **~500 ms** / ~70 KB ³ | n/a |
| **6b'** | Same as 6b, but bundle released *at* `T_candidate_accept` (re-flood lands inside worst-case skew) | ~3.0 s / 76 KB ¹ | **miss** (hash) ⁵ / **~500 ms** (full-V) ⁵ | **miss** (hash) ⁵ / **~500 ms** (full-V) ⁵ | n/a |
| **6c** | Same as 6b, plus marginal breakdown within `f = 2` bound (Phase-1 re-flood misses 2 of 5 honest) — **transient ¹¹** | ~3.0 s / 76 KB ¹,¹¹ | **miss** (hash) ⁵,¹¹ / **~500 ms** (full-V) ⁵ | **miss** (hash) ⁵,¹¹ / **~500 ms** (full-V) ⁵ | n/a |
| **6d** | f byz leaders ALL actively griefing (layer-0 AND layer-1 byzantine and griefing — extends 6a from passive to active) | **miss** ⁹ | **~500 ms** / 108 KB ³,¹⁰ | **miss** ⁸ | n/a |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss | miss |

**Additional footnotes (continuing from n=4 table):**

- ⁵ **Hash vs full-V variant differentiator**: full-V's Phase-2a peer-onion V-recovery + Phase-2b late σ (the secondary closure) handles the byzantine-at-cutoff edge and the marginal-breakdown band within `[1, f]`-honest-missing. Hash variant carries only `hash(V)` at non-leader layers in onions, so honest operators that miss V via Phase 1 cannot recover via peer onions — primary closure is all that's available, which doesn't cover these cases at `f ≥ 2`. See [TBFTR.md](TBFTR.md) "Liveness / Byzantine-at-cutoff edge" and "Phase 2a" caveat.
- ⁷ **K=2 at n=7 is `K < f+1`**; byz can hold both top-K leader slots in worst-case scenarios. Not the recommended TBFTR deployment at this size — included here for apples-to-apples comparison with QBFT's 2-round budget.
- ⁸ **K=2 worst-case misses**: when both top-K leaders are byzantine or offline, no honest leader exists in the top-K → cluster has no fall-through path → slot misses. K=3 (the default) avoids this by guaranteeing ≥ 1 honest leader in the top-K (since byz can hold at most `f = 2` of 3 leader slots).
- ⁹ **QBFT round-3+ miss under proposer-duty budget**: scenarios requiring a third round to reach an honest leader exceed the timing budget (round-1 timeout 2s + round-2 timeout 2s = 4s, leaves no time for round 3 + post-consensus + relay round-trip on top of pre-consensus + fetch). Bandwidth shown is the ~76 KB consumed across rounds 1 and 2 before the cluster gives up. See "Scope and assumptions / QBFT round-timeout".
- ¹⁰ **TBFTR K=3 fall-through to layer-2**: with byz holding L_0 and L_1, the cluster NR-quorums layer 0 and layer 1 (via chained encryption) and reaches L_2 (honest by `K = f+1` construction). Phase 3 is local, so fall-through doesn't add network latency.

**Reading the n=7 failure-mode table:**

- **Scenarios 3-6b (single-byz-leader-grief) close in flat ~500 ms for TBFTR at any K**, primary closure handles them. QBFT pays ~3.0 s for round-2 recovery — a wash with the relay cutoff after pre-consensus + fetch.
- **Scenarios 6a and 6d differentiate K choice and also break QBFT**: both byz operators are in leader positions (L_0 and L_1 byz), so QBFT needs round 3 to reach an honest leader → miss under proposer-duty budget. TBFTR(K=3) closes them via K-layer fall-through (one round-trip for everyone, Phase 3 is local); TBFTR(K=2) misses when byz holds both top-K positions.
- **Scenarios 6b' and 6c (hash vs full-V variant differentiator)**: full-V succeeds via secondary closure; hash variant misses. QBFT survives 6c (only one byz leader, so round-2 has an honest leader and recovers) — but at the usual ~3.0 s round-change cost, AND **conditional on the marginal breakdown being transient** (footnote ¹¹). With sustained breakdown beyond ~2 s, QBFT round 2 also fails. **Hash is the typical deployment** at n=7 for bandwidth reasons; clusters running hash accept the byz-at-cutoff and aggressive-marginal cases as residual miss surfaces. Mitigation options (tighten `T_candidate_accept`, switch to full-V, extend Phase-2 deadlines, or accept the rate) live in [TBFTR.md](TBFTR.md) "Practical caveats".
- **Scenario 6d (new): both byzantine leaders actively griefing.** This is the `f = 2` worst case and is where K=3's fall-through depth earns its bandwidth. QBFT misses (round-3+ needed). K=2 misses (no honest in top-K). K=3 closes in ~500 ms.
- **Bandwidth**: TBFTR(K=3) is consistently larger than QBFT in healthy case (~108 KB vs ~37 KB) but stays flat across failures while QBFT scales toward ~76 KB in failure scenarios. The gap narrows with failure rate.

### n=10, n=13 — same shape, scaled K

The scenario shape is the same as at n=7, with `K = f+1 = 4` (n=10) or `5` (n=13), proportionally widened failure surface for K=2 comparisons (more byz can hold top-K leader slots), and proportionally larger bandwidth.

| Cluster | f | K (default) | TBFTR (default, hash) | TBFTR (default, full-V) | QBFT (1 round) | QBFT (round 2) | QBFT (round 3+) |
|---|---|---|---|---|---|---|---|
| n=10 | 3 | 4 | ~253 KB | ~1 MB | ~50 KB | ~100 KB | miss ⁹ |
| n=13 | 4 | 5 | ~497 KB | ~2.5 MB | ~85 KB | ~170 KB | miss ⁹ |

At n=10 / n=13:

- **QBFT's worst-case-leader-position scenarios** (6a, 6d) require `f = 3` or `f = 4` round changes to reach an honest leader → miss outright under proposer-duty budget. The `f`-byz-grief failure mode is uncovered for QBFT at these sizes.
- **TBFTR's full-V variant becomes impractical** (~1 MB at n=10, ~2.5 MB at n=13). Hash variant is the typical deployment, which means **operating at n ≥ 10 effectively gives up the secondary closure** — scenarios 6b' and 6c miss in hash-only mode.
- **Mitigation options for the residual hash-variant miss surface**: (1) tighten `T_candidate_accept` to `T_commit − (2D + δ)` (closes byz-at-cutoff edge in primary closure itself, at the cost of squeezing the leader's fetch window); (2) accept the rate as a deployment cost and pay it via observability; (3) switch to full-V if bandwidth budget allows. See [TBFTR.md](TBFTR.md) "Practical caveats / Deadline coordination".

## Cross-cluster takeaway

- **The protocols differ in shape, not just performance.** QBFT decouples consensus from signing (3 RTT consensus + 1 RTT post-consensus); TBFT/TBFTR fuse the two (1–2 RTT). Most numerical advantages above trace back to this structural difference.
- **TBFT/TBFTR have constant-cost handling for *most* in-bound failures** at their respective cluster sizes via primary closure. Scenarios where QBFT pays a round timeout (~2 s) cost TBFT/TBFTR nothing extra over the healthy path. The exceptions are aggressive-marginal synchrony (≥ 2 honest missing re-flood at n=4; covered by full-V secondary closure at `f ≥ 2` only) and the byzantine-at-cutoff edge (covered by full-V's secondary closure; uncovered in hash variant at `f ≥ 2`).
- **QBFT's byzantine-leader-grief recovery costs real time and bandwidth** — ~12 KB plus another full round per round change, ~2 s timeout per round. Combined with the proposer-duty round-2-max budget, **QBFT misses outright at n ≥ 7 in worst-case-leader-position scenarios** (6a, 6d) regardless of consensus correctness.
- **Bandwidth ordering depends on conditions, not just on protocol.** QBFT is cheapest in healthy case at all cluster sizes. TBFT/TBFTR become cheaper in time and competitive in bandwidth as round changes accumulate, with the crossover happening earlier at larger cluster sizes.
- **QBFT vs TBFT/TBFTR is a variance-vs-failure-mode-shape trade**, not just average-case performance. QBFT optimizes for cheap round 1 at the cost of expensive failure recovery. TBFT/TBFTR spend more on every slot in exchange for flat behavior under in-bound failures.

| Cluster size | f | Recommended |
|---|---|---|
| n=4 | 1 | **TBFT** as default — primary closure covers up to "1 of 3 honest missing re-flood"; minimal complexity. **TBFTR(K=2)** offers redundancy in the same band but no extension; pick only if redundancy specifically matters. |
| n=7 | 2 | **TBFTR (K=3, hash)** for typical deployment (~108 KB) — primary closure covers up to "1 of 5 honest missing re-flood". **TBFTR (K=3, full-V)** (~325 KB) extends coverage to "≤ 2 of 5 honest missing" if the bandwidth premium is justified. |
| n=10 | 3 | **TBFTR (K=4, hash)** practical (~253 KB). Full-V (~1 MB) impractical for typical deployment, so the cluster gives up the secondary closure. |
| n=13 | 4 | **TBFTR (K=5, hash)** (~497 KB; bandwidth tight). Full-V (~2.5 MB) impractical. |

## Limits of this comparison

- **Numbers are consensus-to-signed-output**, excluding pre-consensus (RANDAO) and block-fetch (~1.5 s combined). Relative gaps matter more than absolute milliseconds.
- **2 s QBFT round timeout is the current SSV setting.** Tightening shrinks scenarios 3-6's QBFT times but raises the false-positive round-change rate under normal jitter (a known trade-off the team has tuned).
- **Numbers are approximate constants**; production has long tails. For small n=4 clusters at low duty load these gaps may not matter; at n=10 / n=13 with frequent slots they very much do.
- **Partial network partitions** (where some operators have a quorum view and others don't) aren't separately modeled. TBFT/TBFTR degrade to "missed slot by some operators" cleanly under partitions; QBFT's view-change behavior is more nuanced and would warrant its own analysis.
- **"Byzantine in worst-case leader positions"** scenarios assume leader rotation is *not* byzantine-aware. Byzantine-aware rotation (VRF-based, distinct sub-quorums per role) would reduce the probability of hitting these worst cases without changing per-scenario bandwidth/time numbers.
- **Hash vs full-V variants** of TBFTR differ in marginal-synchrony band coverage at `f ≥ 2`. The split is noted in tables where relevant; for the trade-off mechanics see [TBFTR.md](TBFTR.md) "Phase 2a" hash-variant caveat.
- **Partial-sigs-only baseline** is theoretical, not a deployable protocol — it assumes the agreed value is already known to all operators, which doesn't hold in scenarios involving leader silence, equivocation, or selective delivery. Included as a floor on how fast a protocol *could possibly* go if consensus weren't needed.

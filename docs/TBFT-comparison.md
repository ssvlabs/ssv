# QBFT vs TBFT vs TBFT2 — failure-mode comparison

This document compares the three protocols — [QBFT](https://github.com/ConsenSys/qbft-formal-spec) (the consensus protocol SSV currently uses), [TBFT](TBFT.md), and [TBFT2](TBFT2.md) — under a sequence of progressively worse operating conditions, holding the application (SSV proposer duty) fixed.

The goal is to show how time-to-finish and bandwidth degrade across protocols as conditions worsen, not to compare them in the abstract. The interesting findings are at the failure boundaries, not the healthy case.

## Methodology

For each scenario we report two numbers per protocol, both measured **from start of consensus to a validator-signed value ready for submission**:

- **Time to signed output** — wall-clock from when consensus starts to when a full BLS signature on the agreed value is reconstructed and ready for the beacon node. For QBFT this includes the mandatory post-consensus phase (each operator broadcasts a partial signature on the decided value, 2f+1 are collected, full sig reconstructed) — without it, QBFT has only a decided value, not a signature. For TBFT and TBFT2 there is no separate post-consensus phase: partial signatures ride inside the Phase 2 onion, and Phase 3's local decryption produces the signed output directly. Excluded from both: pre-consensus (RANDAO, applicable to proposer duty only) and block-fetch — they're ~equal across protocols and would drown out the protocol-level differences we're surfacing.

- **Bandwidth** — all gossipsub deliveries during the same window: from start of consensus to signed output, summed across the cluster.

This scope choice matters because of a structural asymmetry: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus); TBFT and TBFT2 fuse the two by embedding partial signatures inside the consensus-bearing onion.** Comparing only the consensus phase would understate QBFT's cost. The numbers below reflect total work to signed output.

Assumptions:

- Healthy gossip "RTT" (one full propagation across the mesh): ~100 ms; congested: ~500 ms.
- BLS verify/aggregate: ~ms-scale, treated as instant.
- QBFT round timeout: 2 s, matching current SSV ([protocol/v2/qbft/roundtimer/timer.go:154-158](protocol/v2/qbft/roundtimer/timer.go:154)).
- "Miss" means the cluster fails to produce a validator signature before the slot deadline — the proposer duty is missed.
- TBFT and TBFT2 here are the post-rewrite versions: leader-authenticated candidates, leader publishes their own σ-on-V in Phase 1 (gives the cluster a head-start partial), threshold separation (`qV = 2f+1` for V, `qEnc = f+1` for the IBE-based unlock), equivocation-to-non-receipt rule. See [TBFT.md](TBFT.md) for the spec.

## n=7 cluster (f=2)

This is the more interesting cluster size — `f=2` is large enough to expose TBFT2's dual-leader weakness while still being a typical SSV deployment.

Bandwidth constants for this cluster size: QBFT 1 round + post-consensus ~37 KB; QBFT round-change adds ~12 KB; QBFT 2 rounds ~76 KB; QBFT 3 rounds ~115 KB; TBFT (K=3) ~85 KB; TBFT2 ~53 KB; TBFTR (K=3, hash variant + composition) ~108 KB. See "TBFTR bandwidth estimates" below for the derivation.

| # | Scenario | QBFT | TBFT (K=3) | TBFT2 |
|---|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **37 KB** | ~250 ms / 85 KB | ~250 ms / **53 KB** |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 37 KB | ~600 ms / 85 KB | ~600 ms / 53 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 76 KB | **~250 ms** / 85 KB | **~250 ms** / 53 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 76 KB | **~250 ms** / 85 KB ¹ | **~250 ms** / 53 KB ¹ |
| **5** | f operators offline, including top leader | ~3.0 s / 76 KB | **~250 ms** / 85 KB | ~250 ms / 53 KB *or* **miss** ² |
| **6a** | f byzantine in worst-case leader positions, passive (no votes) | ~5.0 s / 115 KB | **~250 ms** / 85 KB | **miss** ³ |
| **6b** | Byzantine layer-0 leader actively griefs (selective delivery + dark on votes — the P0.1 attack) | ~3.0 s / 76 KB ⁴ | **miss** ⁵ | **miss** ⁵ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss |

¹ Equivocation triggers the equivocation-to-non-receipt rule cluster-wide ([TBFT.md](TBFT.md) Phase 1 equivocation handling) — falls through cleanly to the next layer (or backup).
² TBFT2 misses iff both `L_p` and `L_b` are in the offline set; otherwise succeeds via fall-through.
³ For f ≥ 2, both `L_p` and `L_b` can be byzantine simultaneously; at n=4 (f=1) this scenario doesn't exist.
⁴ QBFT recovers via round change — round 1 fails to reach prepare-quorum, new leader elected in round 2.
⁵ The byzantine-layer-leader selective-delivery grief at n=7 has a residual single-point window (`k = 3` honest delivered to) even with leader-σ-in-Phase-1. At this `k`, real σ on V = 3 honest + 1 leader = 4 < qV = 5, and real NR = 2 < qEnc = 3 (other byz refuses NR). Both TBFT and TBFT2 stuck. The residual window narrows from `[f+1, 2f]` (size f) without leader-σ to `[f+1, 2f-1]` (size f-1) with it; for n=4 this collapses to empty, but for n=7 it's a single point. See [TBFT.md](TBFT.md) caveat 1 algebra and [TBFTR.md](TBFTR.md) for the protocol-level fix that closes the residual.

### Reading the n=7 table

The single most important pattern: **TBFT and TBFT2 have ~flat time and bandwidth across in-bound *passive* failure modes; QBFT pays per round change.** Active byzantine-leader grief is a different story — TBFT and TBFT2 lose to it (scenario 6b), QBFT recovers via round change.

A few less-obvious things worth flagging:

- **Scenario 1 favors QBFT on bandwidth** (37 KB vs 53 KB / 85 KB). QBFT's round-1-success path is genuinely the cheapest in bytes. TBFT/TBFT2 trade extra bandwidth for the cryptographic guarantee that *passive* failure modes stay flat. If you assume scenario 1 is the typical case (which it should be on healthy mainnet most slots), QBFT's lower constant adds up.

- **Scenario 2 (slow network, no failures) is the cleanest comparison of pure protocol latency.** TBFT/TBFT2 finish in ~600 ms vs QBFT's ~2.0 s — ~3× advantage from 1-RTT vs 3-RTT structure. Inside MEV's 4-second budget this is the difference between making the relay cutoff and not.

- **Scenario 3 (top leader silent) is the proposer-duty MEV killer for QBFT.** ~3.0 s of consensus puts the validator on the wrong side of the 4 s relay cutoff once you add ~1.5 s of pre-consensus + block-fetch. TBFT and TBFT2 just shrug and use the next layer/backup, well within the cutoff.

- **Scenario 4 (equivocation) is now clean for TBFT/TBFT2.** The post-rewrite equivocation-to-non-receipt rule converts leader equivocation into a clean fall-through. The pair of signed candidates is also slashable evidence against the leader.

- **Scenario 6a (passive byzantine in worst-leader positions) is where TBFT (K=f+1) earns its keep over TBFT2.** TBFT guarantees ≥1 honest leader in the top-K by construction; the worst case still completes in ~250 ms when byz are passive. TBFT2 with both `L_p` and `L_b` byzantine has no fallback — slot missed.

- **Scenario 6b (active byzantine grief) is a residual liveness gap at n ≥ 7.** With leader-σ-in-Phase-1, the grief window narrows to a single point (`k = 3` honest delivered to) at n=7. The byzantine attacker must time delivery to land precisely there; outside that point the cluster either reconstructs σ-quorum or falls through via NR-quorum. At n=4 the window is empty (closed). At n=10 the window is two points; at n=13, three. QBFT recovers from any byz-leader grief via round change at the cost of round-change latency. The protocol-level fix that closes the residual at all cluster sizes lives in [TBFTR.md](TBFTR.md).

- **Bandwidth crossover happens at scenario 3.** Healthy: QBFT cheapest. Top-leader-silent: QBFT (76 KB) overtakes TBFT2 (53 KB). Scenario 6a: QBFT (115 KB) exceeds TBFT (85 KB). The bandwidth advantage QBFT shows on healthy mainnet evaporates as soon as anything goes wrong.

- **All three protocols converge in scenario 7.** Beyond `f` failures, no protocol can produce a quorum signature; the slot is missed. TBFT/TBFT2 fail cleanly with no output; QBFT keeps trying rounds until the slot timeout (so QBFT actually burns *more* bandwidth on the way to missing the slot).

## n=4 cluster (f=1)

Bandwidth constants for this cluster size: QBFT 1 round + post-consensus ~14 KB; QBFT 2 rounds ~27 KB; TBFT (K=3) ~33 KB; TBFT2 ~21 KB; TBFTR (K=3, hash variant + composition) ~43 KB.

| # | Scenario | QBFT | TBFT (K=3) | TBFT2 |
|---|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **14 KB** | ~250 ms / 33 KB | ~250 ms / 21 KB |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 14 KB | ~600 ms / 33 KB | ~600 ms / 21 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 27 KB | **~250 ms** / 33 KB ¹ | **~250 ms** / 21 KB ¹ |
| **5** | f offline incl top leader (=1 offline) | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB |
| **6a** | byz passively in worst leader position (=1 byz, no votes) | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB |
| **6b** | Byzantine layer-0 leader actively griefs (selective delivery + dark on votes — the P0.1 attack) | ~3.0 s / 27 KB ² | **~250 ms** / 33 KB ³ | **~250 ms** / 21 KB ³ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss |

¹ Equivocation triggers the equivocation-to-non-receipt rule cluster-wide — falls through cleanly to the next layer (or backup).
² QBFT recovers via round change.
³ At n=4 the byzantine-layer-leader selective-delivery grief is closed by the leader-publishes-σ-on-V mechanism in Phase 1: the leader's forced threshold partial plus the f+1 = 2 honest partials sum to qV = 3 exactly, leaving no `k` value where both σ-quorum and NR-quorum can fail simultaneously. See [TBFT.md](TBFT.md) caveat 1 algebra. The grief window for n=4 is empty. **Note**: earlier versions of this comparison alternately claimed "TBFT2 cannot miss within-bound at n=4" (wrong reasoning, right outcome) and then "TBFT2 misses just like TBFT at n=4" (right pre-leader-σ analysis, now superseded). The current state with leader-σ-in-Phase-1 is: TBFT and TBFT2 at n=4 both close P0.1/P0.2 mechanically.

### Reading the n=4 table

Several things shift meaningfully when you cut from n=7 to n=4:

- **Scenarios 3, 5, 6a, 6b all collapse to clean outcomes at n=4.** With f=1, all of "top leader silent" / "f offline including top" / "f byz passive" / "active byz-leader grief" finish in ~250 ms via fall-through (or, in 6b's case, via the σ-quorum reaching `qV = 3` exactly through the leader's Phase-1 σ + 2 honest σ partials). The leader-σ-in-Phase-1 mechanism closes scenario 6b at n=4 mechanically; an active byzantine has no `k` value (number of honest delivered to) that produces grief.

- **TBFT and TBFT2 have equivalent byzantine resilience at n=4.** Both close P0.1/P0.2 via the leader-σ-in-Phase-1 mechanism. The difference is bandwidth (TBFT2 ~21 KB vs TBFT(K=3) ~33 KB) and complexity (TBFT2 has one tag, two layers, no `K`). TBFT2 wins on those axes.

- **TBFT's K=3 cap is over-provisioned for n=4.** With f=1, K=2 is sufficient for byzantine resilience (the formula `K = max(3, f+1)` gives 3 only because of the floor). A "TBFT(K=2)" specialization for n=4 is structurally identical to TBFT2 (same 2-layer onion, same single tag). The K=3 floor is defense-in-depth at small bandwidth cost; it doesn't change the byzantine-resilience picture.

- **Time-to-finish differences are similar to n=7.** QBFT's failure path is ~12× longer than TBFT/TBFT2's passive-failure path (3.0 s vs 250 ms). The pain doesn't get better at smaller clusters because round-change time is timeout-driven, not n-driven.

- **No in-bound miss scenarios for TBFT/TBFT2 at n=4 once leader-σ-in-Phase-1 lands.** The original audit P0.2 finding (TBFT2 misses just like TBFT at n=4 under selective-delivery grief) is mechanically resolved at this cluster size.

## TBFTR bandwidth estimates

[TBFTR](TBFTR.md) is TBFT plus two changes: (1) onions carry `V_{L_k}` plaintext (TBFTR core, "Hash-only at non-leader layers" variant — 32 B hash everywhere, full V only at the layer the operator is leader of), and (2) a Phase-2 split (Phase 2a / 2b) where non-receipt commitment defers to the end so honest operators that missed V can recover it from peer onions and sign σ late. The combination closes the byzantine-leader selective-delivery grief at all cluster sizes (the audit P0.1 / P0.2 residual at n ≥ 7).

Bandwidth premium over TBFT, summed across cluster gossipsub deliveries:

| Cluster | f | K | TBFT (worst case) | TBFTR (worst case) | TBFTR premium |
|---|---|---|---|---|---|
| n=4 | 1 | 3 | ~33 KB | ~43 KB | +10 KB (~30%) |
| n=7 | 2 | 3 | ~85 KB | ~108 KB | +23 KB (~27%) |
| n=10 | 3 | 4 | ~220 KB | ~253 KB | +33 KB (~15%) |
| n=13 | 4 | 5 | ~454 KB | ~497 KB | +43 KB (~9%) |

**Estimate derivation (hash variant + composition).** Per-onion plaintext addition: full `V_{L_k}` (~1 KB) at the one layer the operator leads, plus `hash(V_{L_k})` (32 B) at every other layer they signed. Per-cluster onion-content addition: `K · |V| + K · 32B · (n−1) ≈ K KB + 32 K(n−1) B`. Through gossipsub fan-out (capped at mesh degree ~6 for n ≥ 7, full mesh for n=4), times that by the per-broadcast delivery count. The composition adds late-σ broadcasts in Phase 2b — ~`f · 96B · n` per cluster after fan-out, i.e. low-single-digit KB even at n=13. Numbers rounded.

**Premium % decreases at larger n** because TBFT's base bandwidth grows as `K · n²` while TBFTR's hash-variant additions grow as `K · n` (hashes dominate; the single full-V is once per leader, not per operator). At n=4 you pay ~30% extra to close P0.1/P0.2; at n=13 it's ~9%.

**Without the hash optimization** (every operator carries full V at every layer): bandwidth premium scales as `K · |V| · n²`, putting n=13 well past 1 MB cluster-wide. The hash variant is what makes TBFTR practical at production cluster sizes.

**TBFTR's distinguishing scenario coverage.** TBFTR's only meaningful behavioral difference vs TBFT is at scenario 6b (active byzantine-leader selective-delivery grief): TBFT misses at n ≥ 7 (residual `[f+1, 2f-1]` window per [TBFT.md](TBFT.md) caveat 1); TBFTR succeeds at all cluster sizes via deferred-NR's late-σ recovery. All other scenarios behave identically.

## Cross-cluster takeaway

Putting the two pictures together, what stands out:

- **The protocols differ in shape, not just in performance.** QBFT decides on a value first, then runs a separate post-consensus phase to sign it; TBFT and TBFT2 embed partial signatures inside the consensus-bearing onion so that consensus and signing happen in the same broadcast. Most of the numerical advantages below trace back to this structural difference: QBFT pays for two phases sequentially, TBFT/TBFT2 pay for one combined phase.

- **TBFT and TBFT2 have constant-cost handling for *passive* failures.** At every cluster size, in-bound passive failures cost essentially nothing extra in time or bandwidth. QBFT is fundamentally different in shape: failures cost real time (~2 s per round change) and real bandwidth (~12 KB plus another full round per round change).

- **TBFT and TBFT2 close active byzantine-leader grief (scenario 6b) at n=4 and narrow it at larger n.** The leader-σ-in-Phase-1 mechanism gives the cluster a head-start partial that, combined with the f+1 honest partials in Phase 2, reaches qV exactly at f=1 — closing P0.1/P0.2 at n=4. At n ≥ 7 (f ≥ 2) a residual single-or-multi-point grief window remains (size `f-1`), which TBFTR's deferred-NR composition closes. QBFT recovers from active grief at every cluster size via round change, but at the latency cost of round-change recovery (~3 s). TBFT/TBFT2 still win on common-case latency and on the structural simplicity of "no round changes."

- **Bandwidth ordering depends on conditions, not just on protocol.** QBFT is cheapest in the healthy case across all cluster sizes. TBFT/TBFT2 are cheaper than QBFT-with-failure across all cluster sizes. The crossover happens at scenario 3 (top-leader-silent) and stays in TBFT/TBFT2's favor from there in passive failure modes.

- **TBFT2 is the right protocol for n=4; TBFT (K=f+1) is the right protocol for n ≥ 7.** At n=4 (f=1), TBFT and TBFT2 have equivalent byzantine resilience (both close scenario 6b via leader-σ-in-Phase-1); TBFT2 wins on bandwidth and complexity. At f ≥ 2, TBFT2 has worst-case dual-byzantine miss scenarios (scenario 6a in the n=7 table) that TBFT (K=f+1) closes off, at the cost of more bandwidth. **Both have a residual scenario-6b selective-delivery grief at n ≥ 7 (size `f-1`); that's TBFTR's job to close.**

| Cluster size | f | Best fit | Reasoning | Upgrade to TBFTR? |
|---|---|---|---|---|
| n=4 | 1 | **TBFT2** | Same byz resilience as TBFT (scenario 6b closed at n=4 by leader-σ-in-Phase-1); lowest bandwidth; simplest protocol | Not needed — n=4 has no residual 6b grief |
| n=7 | 2 | TBFT (K=3) | TBFT2 has dual-byzantine-leader miss exposure (scenario 6a); TBFT(K=3) covers it. Both have residual scenario-6b grief (single point). | Optional, +27% bandwidth; closes the single-point residual |
| n=10 | 3 | TBFT (K=4) | Same logic, K scales with f. Residual scenario-6b grief size 2. | Worth considering, +15% bandwidth; closes a 2-point residual |
| n=13 | 4 | TBFT (K=5) | Same; bandwidth still tractable at this scale. Residual scenario-6b grief size 3. | Recommended, +9% bandwidth; closes a 3-point residual |

- **The QBFT vs TBFT/TBFT2 trade isn't just average-case performance; it's variance and failure mode.** QBFT optimizes for the common case (cheap round 1) at the cost of expensive failure recovery. TBFT/TBFT2 spend more on every slot in exchange for flat behavior under *passive* failure, but lose to active byz-leader grief. Which is "better" depends on how often the network is in a degraded state, how badly missed slots hurt, and how realistic active byz-leader-grief is in practice — empirical questions that need production data to answer.

## Limits of this comparison

- These are consensus-to-signed-output numbers. End-to-end time has another ~1.5 s of pre-consensus + block-fetch sitting on top of every row, so the *relative* gaps matter more than the absolute milliseconds.

- The 2 s QBFT round timeout is current SSV. Tightening it would shrink scenarios 3-6's QBFT times but raise the false-positive round-change rate under normal jitter (a known trade-off the team has already tuned).

- Numbers are approximate constants; real production has long tails. For small n=4 clusters at low duty load these gaps may not matter; at n=10/13 with frequent slots they very much do.

- Partial network partitions (where some operators have a quorum view and others don't) aren't separately modeled here. Both TBFT variants degrade to "missed slot by some operators" cleanly under partitions; QBFT's view-change behavior is more nuanced and would warrant its own analysis.

- The "byzantine in worst-case leader positions" scenarios (6a, 6b) assume leader rotation is *not* byzantine-aware. Byzantine-aware rotation (VRF-based, distinct sub-quorums per role) would reduce the probability of hitting these worst cases without changing the per-scenario bandwidth or time numbers.

- Scenario 6b assumes byzantine has the timing precision and mesh awareness to selectively deliver `V` to exactly the right number of honest (one of the residual grief-window `k` values) just before `T_d`. The grief window narrows with f: `[f+1, 2f-1]` of size `f-1`, which is empty at n=4, single-point at n=7, etc. A naive byzantine attacker who can't time precisely degrades to scenario 6a (passive failure) and TBFT/TBFT2 recover. Tighter `T_d` tuning (P99/P999 vs P95) shrinks the window where 6b is achievable in the first place.

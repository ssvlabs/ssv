# QBFT vs TBFT vs TBFT2 — failure-mode comparison

This document compares the three protocols — [QBFT](https://github.com/ConsenSys/qbft-formal-spec) (the consensus protocol SSV currently uses), [TBFT](TBFT.md), and [TBFT2](TBFT2.md) — under a sequence of progressively worse operating conditions, holding the application (SSV proposer duty) fixed.

The goal is to show how time-to-finish and bandwidth degrade across protocols as conditions worsen, not to compare them in the abstract. The interesting findings are at the failure boundaries, not the healthy case.

## Methodology

For each scenario we report two numbers per protocol, and both are measured **from start of consensus to a validator-signed value ready for submission**:

- **Time to signed output** — wall-clock from when consensus starts to when a full BLS signature on the agreed value is reconstructed and ready for the beacon node. For QBFT this includes the mandatory post-consensus phase (each operator broadcasts a partial signature on the decided value, 2f+1 are collected, full sig reconstructed) — without it, QBFT has only a decided value, not a signature. For TBFT and TBFT2 there is no separate post-consensus phase: partial signatures ride inside the Phase 2 onion, and Phase 3's local decryption produces the signed output directly. Excluded from both: pre-consensus (RANDAO, applicable to proposer duty only) and block-fetch — they're ~equal across protocols and would drown out the protocol-level differences we're surfacing.

- **Bandwidth** — all gossipsub deliveries during the same window: from start of consensus to signed output, summed across the cluster. QBFT's bandwidth includes the post-consensus partial-sig broadcast; TBFT/TBFT2's bandwidth already includes partial sigs inside the onion. Same scope as the time metric.

This scope choice matters because of a structural asymmetry between the protocols: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus); TBFT and TBFT2 fuse the two by embedding partial signatures inside the consensus-bearing onion.** Comparing only the consensus phase would understate QBFT's cost, since QBFT alone doesn't produce a signature. The numbers below reflect total work to signed output, which is the apples-to-apples comparison.

Assumptions:

- Healthy gossip "RTT" (one full propagation across the mesh): ~100 ms; congested: ~500 ms.
- BLS verify/aggregate: ~ms-scale, treated as instant.
- QBFT round timeout: 2 s, matching current SSV ([protocol/v2/qbft/roundtimer/timer.go:154-158](protocol/v2/qbft/roundtimer/timer.go:154)).
- "Miss" means the cluster fails to produce a validator signature before the slot deadline — the proposer duty is missed.

## n=7 cluster (f=2)

This is the more interesting cluster size — `f=2` is large enough to expose TBFT2's weakness (both leaders byzantine in the worst case) while still being a typical SSV deployment.

Bandwidth constants for this cluster size: QBFT 1 round + post-consensus ~37 KB; QBFT round-change adds ~12 KB; QBFT 2 rounds ~76 KB; QBFT 3 rounds ~115 KB; TBFT (K=3) ~85 KB; TBFT2 ~53 KB.

| # | Scenario | QBFT | TBFT (K=3) | TBFT2 |
|---|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **37 KB** | ~250 ms / 85 KB | ~250 ms / **53 KB** |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 37 KB | ~600 ms / 85 KB | ~600 ms / 53 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 76 KB | **~250 ms** / 85 KB | **~250 ms** / 53 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 76 KB | **~250 ms** / 85 KB | **~250 ms** / 53 KB |
| **5** | f operators offline, including top leader | ~3.0 s / 76 KB | **~250 ms** / 85 KB | ~250 ms / 53 KB *or* **miss** ¹ |
| **6** | f byzantine in worst-case leader positions | ~5.0 s / 115 KB | **~250 ms** / 85 KB | **miss** ² |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss |

¹ TBFT2 misses iff both `L_p` and `L_b` are in the offline set; otherwise succeeds.
² For f ≥ 2, both `L_p` and `L_b` can be byzantine simultaneously; this scenario doesn't exist for n=4 (f=1).

### Reading the n=7 table

The single most important pattern: **TBFT and TBFT2 have ~flat time and bandwidth across all in-bound failure modes; QBFT pays per round change.** TBFT's "fall through to layer 2 or 3" is local computation on already-broadcast partial sigs — costs essentially zero additional latency or bandwidth. QBFT's round change costs a full 2 s timeout plus ~12 KB of round-change messages plus another full round.

A few less-obvious things worth flagging:

- **Scenario 1 favors QBFT on bandwidth** (37 KB vs 53 KB / 85 KB). QBFT's round-1-success path is genuinely the cheapest in bytes. TBFT/TBFT2 trade extra bandwidth for the cryptographic guarantee that *all* failure scenarios stay flat. If you assume scenario 1 is the typical case (which it should be on a healthy mainnet most slots), QBFT's lower constant adds up.

- **Scenario 2 (slow network, no failures) is the cleanest comparison of pure protocol latency.** TBFT/TBFT2 finish in ~600 ms vs QBFT's ~2.0 s — ~3× advantage from 1-RTT vs 3-RTT structure. Inside MEV's 4-second budget this is the difference between making the relay cutoff and not.

- **Scenario 3 (top leader silent) is the proposer-duty MEV killer for QBFT.** ~3.0 s of consensus puts the validator on the wrong side of the 4 s relay cutoff once you add ~1.5 s of pre-consensus + block-fetch. TBFT and TBFT2 just shrug and use the next layer, well within the cutoff.

- **Scenario 6 is where TBFT (K=f+1) earns its keep over TBFT2.** TBFT guarantees ≥1 honest leader in the top-K by construction; the worst case still completes in ~250 ms. TBFT2 with both `L_p` and `L_b` byzantine has no fallback — slot missed.

- **Bandwidth crossover happens at scenario 3.** Healthy: QBFT cheapest. Top-leader-silent: QBFT (76 KB) overtakes TBFT2 (53 KB). Byzantine worst case: QBFT (115 KB) exceeds TBFT (85 KB). The bandwidth advantage QBFT shows on healthy mainnet evaporates as soon as anything goes wrong.

- **All three protocols converge in scenario 7.** Beyond `f` failures, no protocol can produce a quorum signature; the slot is missed. TBFT/TBFT2 fail cleanly with no output; QBFT keeps trying rounds until the slot timeout (so QBFT actually burns *more* bandwidth on the way to missing the slot).

## n=4 cluster (f=1)

Bandwidth constants for this cluster size: QBFT 1 round + post-consensus ~14 KB; QBFT 2 rounds ~27 KB; TBFT (K=3) ~33 KB; TBFT2 ~21 KB.

| # | Scenario | QBFT | TBFT (K=3) | TBFT2 |
|---|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **14 KB** | ~250 ms / 33 KB | ~250 ms / 21 KB |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 14 KB | ~600 ms / 33 KB | ~600 ms / 21 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB |
| **5** | f operators offline, including top leader (= 1 offline) | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB ¹ |
| **6** | f byzantine in worst-case leader positions (= 1 byz) | ~3.0 s / 27 KB | **~250 ms** / 33 KB | **~250 ms** / 21 KB ¹ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss | miss |

¹ Unlike at n=7, TBFT2 *cannot* miss within-bound at n=4: with f=1, at most one of `{L_p, L_b}` can be the failing operator, so the other is always available.

### Reading the n=4 table

Several things shift meaningfully when you cut from n=7 to n=4:

- **Scenarios 3–6 collapse to one outcome.** With f=1, "top leader silent" / "byzantine equivocating" / "f offline including top" / "f byzantine in worst case" are all manifestations of the same thing: a single bad operator. The protocol behavior is identical across all four. (At n=7 these were genuinely different — multiple-byzantine and multiple-offline patterns produce different worst cases.)

- **TBFT2 has no in-bound miss scenarios.** This is the key practical difference. At n=7 (f=2), TBFT2 could lose if both `L_p` and `L_b` happened to be byzantine; at n=4 (f=1) that's impossible by definition. **For n=4 clusters, TBFT2 is strictly the right choice over TBFT** — same time, same robustness within bound, less bandwidth, simpler protocol.

- **TBFT's K=3 cap is over-provisioned for n=4.** With f=1, K=2 is sufficient for byzantine resilience (the formula `K = max(3, f+1)` gives 3 only because of the floor). A "TBFT(K=2)" specialization for n=4 would have ~21 KB bandwidth — exactly TBFT2. Which is the same observation: at n=4, TBFT2 *is* the natural specialization of TBFT.

- **Time-to-finish differences are similar to n=7.** QBFT's failure path is ~12× longer than TBFT/TBFT2's (3.0 s vs 250 ms). The pain doesn't get better at smaller clusters because round-change time is timeout-driven, not n-driven.

## Cross-cluster takeaway

Putting the two pictures together, what stands out:

- **The protocols differ in shape, not just in performance.** QBFT decides on a value first, then runs a separate post-consensus phase to sign it; TBFT and TBFT2 embed partial signatures inside the consensus-bearing onion so that consensus and signing happen in the same broadcast. Most of the numerical advantages below trace back to this structural difference: QBFT pays for two phases sequentially, TBFT/TBFT2 pay for one combined phase.

- **TBFT and TBFT2 have constant-cost failure handling.** At every cluster size, in-bound failures cost essentially nothing extra in time or bandwidth. QBFT is fundamentally different in shape: failures cost real time (~2 s per round change) and real bandwidth (~12 KB plus another full round per round change).

- **Bandwidth ordering depends on conditions, not just on protocol.** QBFT is cheapest in the healthy case across all cluster sizes. TBFT/TBFT2 are cheaper than QBFT-with-failure across all cluster sizes. The crossover happens at scenario 3 (top-leader-silent) and stays in TBFT/TBFT2's favor from there.

- **TBFT2 is the right protocol for n=4; TBFT (K=f+1) is the right protocol for n ≥ 7.** TBFT2's dual-leader byzantine-grief exposure doesn't exist at f=1, so it dominates TBFT on bandwidth and complexity at n=4. At f ≥ 2, TBFT2 has worst-case miss scenarios that TBFT (with K = f+1) closes off — at the cost of more bandwidth.

| Cluster size | f | Best fit | Reasoning |
|---|---|---|---|
| n=4 | 1 | **TBFT2** | No in-bound misses; lowest bandwidth; simplest protocol |
| n=7 | 2 | TBFT (K=3) | TBFT2 has dual-byzantine-leader miss exposure; TBFT(K=3) covers it |
| n=10 | 3 | TBFT (K=4) | Same logic, K scales with f |
| n=13 | 4 | TBFT (K=5) | Same; bandwidth still tractable at this scale |

- **The QBFT vs TBFT/TBFT2 trade isn't about average-case performance; it's about variance.** QBFT optimizes for the common case (cheap round 1) at the cost of expensive failure recovery. TBFT/TBFT2 spend more on every slot in exchange for flat behavior under failure. Which is "better" depends on how often the network is in a degraded state and how badly missed slots hurt — both empirical questions that need production data to answer.

## Limits of this comparison

- These are consensus-phase-only numbers. End-to-end time has another ~1.5 s of pre-consensus + block-fetch sitting on top of every row, so the *relative* gaps matter more than the absolute milliseconds.

- The 2 s QBFT round timeout is current SSV. Tightening it would shrink scenarios 3-6's QBFT times but raise the false-positive round-change rate under normal jitter (a known trade-off the team has already tuned).

- Numbers are approximate constants; real production has long tails. For small n=4 clusters at low duty load these gaps may not matter; at n=10/13 with frequent slots they very much do.

- Partial network partitions (where some operators have a quorum view and others don't) aren't separately modeled here. Both TBFT variants degrade to "missed slot by some operators" cleanly under partitions; QBFT's view-change behavior is more nuanced and would warrant its own analysis.

- The "byzantine in worst-case leader positions" scenarios assume leader rotation is *not* byzantine-aware. Byzantine-aware rotation (VRF-based, distinct sub-quorums per role) would reduce the probability of hitting these worst cases without changing the per-scenario bandwidth or time numbers.

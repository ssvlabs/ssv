# QBFT vs TBFT/TBFTR — failure-mode comparison

This document compares SSV's existing consensus protocol [QBFT](https://github.com/ConsenSys/qbft-formal-spec) against the cluster-size-appropriate replacement under a sequence of progressively worse operating conditions, holding the application (SSV proposer duty) fixed:

- For `n = 4` clusters: QBFT vs **[TBFT](TBFT.md)** (K=2, primary + backup).
- For `n ≥ 7` clusters: QBFT vs **[TBFTR](TBFTR.md)** (K = max(3, f+1), with V plaintext in onions + Phase 2 split).

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
- TBFT (n=4) and TBFTR (n≥7) here are the post-rewrite versions: leader-authenticated candidates, leaders publish their own σ-on-V in Phase 1, threshold separation (`qV = 2f+1`, `qEnc = f+1`), σ+NR exclusion at aggregation, equivocation-to-non-receipt rule. TBFTR additionally carries V plaintext in onions and splits Phase 2 into 2a/2b. See [TBFT.md](TBFT.md) and [TBFTR.md](TBFTR.md) for the specs.

## n=4 cluster (f=1) — QBFT vs TBFT

Bandwidth constants for this cluster size: QBFT 1 round + post-consensus ~14 KB; QBFT 2 rounds ~27 KB; **TBFT (K=2) ~21 KB**.

| # | Scenario | QBFT | TBFT (K=2) |
|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **14 KB** | ~250 ms / 21 KB |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 14 KB | ~600 ms / 21 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 27 KB | **~250 ms** / 21 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 27 KB | **~250 ms** / 21 KB ¹ |
| **5** | f offline incl top leader (=1 offline) | ~3.0 s / 27 KB | **~250 ms** / 21 KB |
| **6a** | Byzantine in worst-case leader position, passive (no votes) | ~3.0 s / 27 KB | **~250 ms** / 21 KB |
| **6b** | Byzantine layer-0 leader actively griefs (selective delivery + dark on votes — the P0.1 attack) | ~3.0 s / 27 KB ² | **~250 ms** / 21 KB ³ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss |

¹ Equivocation triggers TBFT's equivocation-to-non-receipt rule cluster-wide — falls through cleanly to the backup.
² QBFT recovers via round change.
³ At n=4 the byzantine-layer-leader selective-delivery grief is closed by the leader-publishes-σ-on-V mechanism in Phase 1: the leader's forced threshold partial plus the f+1 = 2 honest partials sum to qV = 3 exactly, leaving no `k` value where both σ-quorum and NR-quorum can fail simultaneously. See [TBFT.md](TBFT.md) "Liveness profile" for the per-`k` table.

### Reading the n=4 table

- **Scenarios 3, 5, 6a, 6b all collapse to clean outcomes for TBFT.** With f=1, all of "top leader silent" / "f offline including top" / "f byz passive" / "active byz-leader grief" finish in ~250 ms via fall-through to backup or via the σ-quorum reaching `qV = 3` exactly through the leader's Phase-1 σ + 2 honest σ partials. The leader-σ-in-Phase-1 mechanism closes scenario 6b mechanically; an active byzantine has no `k` value that produces grief. **No in-bound miss scenarios for TBFT at n=4.**
- **Scenario 1 favors QBFT on bandwidth** (14 KB vs 21 KB). QBFT's round-1-success path is genuinely the cheapest in bytes. TBFT trades some extra bandwidth for the cryptographic guarantee that all in-bound failure modes stay flat.
- **Scenario 2 (slow network, no failures) is the cleanest comparison of pure protocol latency.** TBFT finishes in ~600 ms vs QBFT's ~2.0 s — ~3× advantage from 1-RTT vs 3-RTT structure. Inside MEV's 4 s budget this is the difference between making the relay cutoff and not.
- **Scenario 3 (top leader silent) is the proposer-duty MEV killer for QBFT.** ~3.0 s of consensus puts the validator on the wrong side of the 4 s relay cutoff once you add ~1.5 s of pre-consensus + block-fetch. TBFT just uses the backup, well within the cutoff.
- **Time-to-finish differences don't get better at smaller clusters because round-change time is timeout-driven, not n-driven.** QBFT's failure path is ~12× longer than TBFT's flat-failure path (3.0 s vs 250 ms).

## n=7 cluster (f=2) — QBFT vs TBFTR

This is the cluster size where TBFTR's distinct contribution matters most — at `f ≥ 2` plain TBFT (the n=4 protocol) wouldn't close the byzantine-leader selective-delivery grief, so n ≥ 7 needs TBFTR.

Bandwidth constants: QBFT 1 round + post-consensus ~37 KB; QBFT round-change adds ~12 KB; QBFT 2 rounds ~76 KB; QBFT 3 rounds ~115 KB; **TBFTR (K=3, hash variant) ~108 KB**.

| # | Scenario | QBFT | TBFTR (K=3) |
|---|---|---|---|
| **1** | All honest, healthy network | ~750 ms / **37 KB** | ~400 ms / 108 KB |
| **2** | All honest, congested network (RTT 500 ms) | ~2.0 s / 37 KB | ~750 ms / 108 KB |
| **3** | Top leader silent (offline or refuses to propose) | ~3.0 s / 76 KB | **~400 ms** / 108 KB |
| **4** | Top leader byzantine equivocating | ~3.0 s / 76 KB | **~400 ms** / 108 KB ¹ |
| **5** | f operators offline, including top leader | ~3.0 s / 76 KB | **~400 ms** / 108 KB |
| **6a** | f byzantine in worst-case leader positions, passive (no votes) | ~5.0 s / 115 KB | **~400 ms** / 108 KB |
| **6b** | Byzantine layer-0 leader actively griefs (selective delivery + dark on votes — the P0.1 attack) | ~3.0 s / 76 KB ² | **~400 ms** / 108 KB ³ |
| **7** | More than f failures (beyond byzantine bound) | miss | miss |

¹ Equivocation triggers the equivocation-to-non-receipt rule cluster-wide — falls through cleanly to the next layer.
² QBFT recovers via round change — round 1 fails to reach prepare-quorum, new leader elected in round 2.
³ TBFTR closes the n ≥ 7 selective-delivery residual via its Phase-2 split: f honest who missed V via selective delivery recover V via peer onions in Phase 2a (TBFTR core), then sign late σ in Phase 2b. Real σ count = (f+1) onion + f late-σ + 1 leader = 2f+2 ≥ qV = 2f+1. Reconstruction succeeds at the same layer the byzantine tried to grief. See [TBFTR.md](TBFTR.md) "Liveness profile".

### Reading the n=7 table

- **TBFTR's flat ~400 ms across all in-bound failure modes** is the headline. The +Δ_2b window (~150 ms) over a TBFT-shape protocol pays for the byzantine-leader grief closure via the late-σ recovery path. QBFT pays per round change — passive failures cost ~3.0 s, the worst-case 6a costs ~5.0 s.
- **Scenario 6a (passive byzantine in worst-leader positions) is where TBFTR's K=f+1 coverage matters most.** TBFTR guarantees ≥1 honest leader in the top-K by construction; the worst case still completes in ~400 ms when byz are passive. QBFT pays for two round changes here.
- **Scenario 6b (active byzantine grief) is closed at n=7 by TBFTR.** The byzantine layer-0 leader does selective delivery + goes dark on votes; without the composition the cluster would stick at the residual `k = 3` grief point, but with the Phase-2 split the f honest who missed V recover via TBFTR core and sign late σ in Phase 2b. σ-quorum reaches at qV = 5. Slot succeeds at layer 0. QBFT also recovers from active grief via round change, at the cost of round-change latency.
- **Bandwidth: TBFTR is consistently larger** than QBFT in healthy-case (108 KB vs 37 KB) but stays flat across failures while QBFT scales toward 115 KB in worst-case 6a. The gap in failure scenarios narrows; the gap in healthy case stays open.
- **Scenario 1 still favors QBFT on bandwidth** (37 KB vs 108 KB). The premium for TBFTR's V-plaintext + composition is real. The trade is paying for guaranteed flat performance across the failure modes, including the active-byz-leader-grief closure.

### n=10, n=13 — same shape, scaled K

Bandwidth constants:

| Cluster | f | K | TBFTR (worst case) | QBFT 1 round | QBFT 2 rounds | QBFT 3 rounds |
|---|---|---|---|---|---|---|
| n=10 | 3 | 4 | ~253 KB | ~50 KB | ~100 KB | ~150 KB |
| n=13 | 4 | 5 | ~497 KB | ~85 KB | ~170 KB | ~255 KB |

The scenario-by-scenario shape is the same as n=7 — TBFTR finishes in flat ~400–500 ms across all in-bound failures (including 6b), QBFT degrades per round change. The bandwidth gap widens with n (TBFTR's `K · n²` term grows faster than QBFT's per-round constant). At n=13, TBFTR uses ~3× QBFT-3-round bandwidth — the price of mechanical closure of the byzantine-leader selective-delivery grief at f=4.

If bandwidth at n=13 is the binding constraint, the engineering options are:

- Stay with TBFT-shape (no V plaintext, no Phase-2b composition) at n=13 and accept the residual `[f+1, 2f-1] = [5, 7]` size-3 grief window. P0.1 grief at n=13 in the modeled byzantine scenario would cost ~3 missed slots out of every byzantine-layer-0-leader slot multiplied by the precision-attack hit rate. Whether this is acceptable depends on operator economics.
- Use TBFTR's hash-only variant (described in [TBFTR.md](TBFTR.md) "Phase 2a"), which trades full-V-everywhere for hash-everywhere-plus-leader-V. Cuts bandwidth premium to ~+9% over a hypothetical "leader-σ-only" baseline.

## Cross-cluster takeaway

- **The protocols differ in shape, not just in performance.** QBFT decides on a value first, then runs a separate post-consensus phase to sign it. TBFT/TBFTR embed partial signatures inside the consensus-bearing onion (and TBFTR adds late-σ broadcasts in Phase 2b) so that consensus and signing happen in the same broadcast window. Most numerical advantages above trace back to this structural difference.

- **TBFT/TBFTR have constant-cost handling for *all* in-bound failures** at their respective cluster sizes. At n=4, leader-σ-V-in-Phase-1 closes byzantine-leader grief mechanically. At n≥7, the TBFTR composition closes the residual via late-σ recovery. QBFT is fundamentally different in shape: failures cost real time (~2 s per round change) and real bandwidth (~12 KB plus another full round per round change).

- **TBFT (n=4) and TBFTR (n≥7) close P0.1/P0.2 at all SSV-supported cluster sizes.** TBFT does it at n=4 via the leader-σ algebra; TBFTR does it at n≥7 via the Phase-2 split + V plaintext. QBFT also handles the byzantine-leader-grief case (via round change), but with larger latency cost.

- **Bandwidth ordering depends on conditions, not just on protocol.** QBFT is cheapest in the healthy case across all cluster sizes. TBFT/TBFTR become cheaper in time and competitive in bandwidth once round changes accumulate, with the crossover happening earlier at larger cluster sizes.

- **The QBFT vs TBFT/TBFTR trade is variance and failure-mode shape**, not just average-case performance. QBFT optimizes for the common case (cheap round 1) at the cost of expensive failure recovery. TBFT/TBFTR spend more on every slot in exchange for flat behavior under all in-bound failure conditions, including active byzantine-leader grief. Which is "better" depends on how often the network is in a degraded state, how badly missed slots hurt, and how realistic active byz-leader grief is in practice — empirical questions that need production data to answer.

| Cluster size | f | Recommended TBFT-family protocol |
|---|---|---|
| n=4 | 1 | **TBFT (K=2)** — mechanical P0.1/P0.2 closure via leader-σ, no Phase-2 split, no V plaintext. ~21 KB. |
| n=7 | 2 | **TBFTR (K=3)** — composition closes residual byz-leader grief; ~108 KB. |
| n=10 | 3 | **TBFTR (K=4)** — same shape as n=7; ~253 KB (hash variant essential). |
| n=13 | 4 | **TBFTR (K=5)** — same; ~497 KB (hash variant essential; bandwidth tight). |

## Limits of this comparison

- These are consensus-to-signed-output numbers. End-to-end time has another ~1.5 s of pre-consensus + block-fetch sitting on top of every row, so the *relative* gaps matter more than the absolute milliseconds.

- The 2 s QBFT round timeout is current SSV. Tightening it would shrink scenarios 3-6's QBFT times but raise the false-positive round-change rate under normal jitter (a known trade-off the team has already tuned).

- Numbers are approximate constants; real production has long tails. For small n=4 clusters at low duty load these gaps may not matter; at n=10/13 with frequent slots they very much do.

- Partial network partitions (where some operators have a quorum view and others don't) aren't separately modeled here. TBFT/TBFTR degrade to "missed slot by some operators" cleanly under partitions; QBFT's view-change behavior is more nuanced and would warrant its own analysis.

- The "byzantine in worst-case leader positions" scenarios (6a, 6b) assume leader rotation is *not* byzantine-aware. Byzantine-aware rotation (VRF-based, distinct sub-quorums per role) would reduce the probability of hitting these worst cases without changing the per-scenario bandwidth or time numbers.

- TBFTR's bandwidth numbers assume the hash variant (full V at the leader's own layer, 32-B hashes elsewhere). Without that optimization, bandwidth scales as `K · |V| · n²` — pushing n=13 well past 1 MB cluster-wide. The hash variant is what makes TBFTR practical at large cluster sizes.

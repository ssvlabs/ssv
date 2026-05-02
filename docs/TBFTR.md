# TBFTR — Threshold BFT (Roach)

A single-shot agreement protocol that produces one collective threshold-signed value per "slot" against a hard deadline, with full byzantine-leader selective-delivery resilience under partial synchrony plus an additional secondary-closure layer that extends robustness into a marginal-synchrony band.

TBFTR runs at any cluster size `n = 3f+1` with `f ≥ 1`. SSV's supported cluster sizes are `n ∈ {4, 7, 10, 13}`. Two additions over a minimal TBFT-shape protocol:

- **V plaintext in onions**: each operator's onion at layer `k` carries `V_{L_k}` plaintext alongside the encrypted partial sig, so an operator that didn't receive `V_{L_k}` during Phase 1 can recover it from any peer's onion in Phase 2.
- **Phase 2 split**: Phase 2 splits into 2a (onion broadcast, no NR yet) and 2b (late σ from operators who recovered V; NR otherwise). Together with V plaintext, this lets honest operators that missed V via Phase-1 propagation still contribute σ partials, reaching `qV` and reconstructing.

Under partial synchrony, gossipsub re-flooding of the Phase-1 bundle handles byzantine-leader selective delivery at any `f` — see "Fault tolerance / Liveness". The TBFTR additions form the *secondary* closure: when actual propagation slightly exceeds the partial-synchrony budget, peer-onion V-recovery + late σ extends the band where the cluster still completes. The marginal band widens with `f`, so the additions earn their complexity more at larger cluster sizes; at `n = 4`, the lean alternative is preferred unless marginal-synchrony robustness specifically matters. The cost is extra bandwidth (~10–80 KB per slot depending on cluster size) and one extra gossip window (`Δ_2b`, ~100–200 ms).

For SSV's `n = 4` clusters, [TBFT](TBFT.md) is a leaner alternative: same cryptographic core, drops the V-plaintext + Phase-2-split machinery, lower bandwidth and latency. See **Appendix A** for a side-by-side comparison.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it

**Suited for:** SSV clusters of any supported size (`n ∈ {4, 7, 10, 13}`), single-shot duties with a fixed deadline, where missing the slot is the natural failure mode. The cluster's bandwidth budget can absorb the V-plaintext / late-σ overhead, and the timing budget can absorb the extra Phase-2b window.

**At `n = 4`,** TBFTR works but [TBFT](TBFT.md) is leaner. Pick TBFTR-at-n=4 if marginal-synchrony robustness is worth the bandwidth/latency premium; pick TBFT if the cluster operates well within partial-synchrony bounds and minimal protocol complexity is preferred.

## Setting

- A cluster of `n = 3f + 1` participants with `f ≥ 1`. SSV's supported cluster sizes are `n ∈ {4, 7, 10, 13}` (so `f ∈ {1, 2, 3, 4}`).
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) to sign no-quorum tags and (b) as the decryption oracle for threshold IBE. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety".
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- For each slot, a **leader priority order** `(L_0, L_1, …, L_{n-1})` deterministically derived from slot data. `L_0` is the highest-priority leader. (0-based indexing throughout.)
- A **fallback depth** `K = f+1` per cluster: `K=2` for n=4, `K=3` for n=7, `K=4` for n=10, `K=5` for n=13. (`K = f+1` ensures at least one honest leader exists in the top-`K` since byz can hold at most `f`.)
- Per-slot deadlines: `T_commit` (Phase 1 ends, Phase 2a starts), `T_commit + Δ_2a` (Phase 2a ends, Phase 2b starts), `T_commit + Δ_2a + Δ_2b` (Phase 2b ends, Phase 3 starts).
- A **candidate acceptance cutoff** `T_candidate_accept = T_commit − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers drop any Phase-1 candidate whose first-observation time is later than `T_candidate_accept` — treated locally as not-received. With the cutoff, a candidate accepted by any honest operator before `T_candidate_accept` has at least `D + δ` left to re-flood (or to land in peer Phase-2a onions before *their* cutoff), so the byzantine cannot fragment the cluster by timing-based selective delivery. The cutoff makes both the gossipsub-re-flooding closure and the Phase-2a peer-recovery closure operational — see "Fault tolerance / Liveness".

## Protocol

### Phase 1 — Candidate broadcast `[T_commit − Δ_1, T_commit]`

Each leader `L_k` for `k ∈ {0, …, K−1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(version, cluster_id, slot, layer k, leader_id, value_root, parent_root)`. The envelope rules out cross-cluster / cross-layer / cross-slot replay at the protocol level rather than relying on application validity to surface those mistakes.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, validate `V_{L_k}` against application-level rules, and **check the first-observation timestamp against `T_candidate_accept`**:

- **First-observation ≤ T_candidate_accept**: bundle is **accepted** as a Phase-1 candidate. The operator may sign σ on `V_{L_k}` in Phase 2a (or stand by their Phase-1 σ if they are the layer's leader — see Phase 2b's commitment rule).
- **First-observation > T_candidate_accept**: bundle is **not accepted as a Phase-1 candidate** (the cutoff prevents timing-based selective delivery from fragmenting the σ-pool — see "Bundle propagation" below). However, the leader's authentication signatures (`σ_{L_k}^V`, `σ_{L_k}^{op}`, envelope) are **retained for the slot**. They can be used in Phase 2b to validate `V_{L_k}` recovered from a Phase-2a peer onion — see Phase 2b. Late retention is auth-only; it does *not* allow the operator to sign σ in their own Phase-2a onion (which would re-open the timing-fragmentation attack the cutoff exists to prevent).

Bundles failing signature verification, envelope re-derivation, or application-level validation are silently dropped — neither accepted nor retained. A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.

**Bundle propagation.** Honest receivers re-flood the bundle via standard gossipsub — this is what closes selective-delivery attempts by a byzantine leader. The argument requires the **candidate acceptance cutoff** above: an honest receiver accepting a bundle at time `t ≤ T_candidate_accept` has `T_commit − t ≥ D + δ` left for re-flooding to reach every other honest operator before *their* `T_candidate_accept` (clock skew bounded by `δ`). Without the cutoff, a byzantine could release the bundle at `T_commit − ε` for `ε < D + δ`, fragmenting the cluster within the synchrony bound; with the cutoff, late releases are uniformly rejected. This is the same partial-synchrony envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)), made operational by a concrete cutoff.

**Equivocation handling.** If a participant observes two distinct `σ_V` partials from the same `L_k` at the same slot/layer (regardless of `parent_root`), that's leader equivocation: locally treat layer `k` as non-receipt (don't include a positive partial signature for it in the onion; broadcast the matching non-receipt attestation in Phase 2b instead, for `k ≤ K-2`). The pair of signed bundles is self-contained slashing evidence — see "Fault tolerance / Equivocation handling" for the analysis. The leader is required to sign σ_V exactly once per slot/layer (refreshes during the fetch window are pre-signing only — see "Head-change handling" in the Application section); any second σ_V from the same leader is a protocol violation regardless of intent.

### Phase 2a — Onion broadcast with V plaintext `[T_commit, T_commit + Δ_2a]`

Each participant `i` constructs a `K`-layer onion. Layer `k` is structured as:

```
layer k:  V_{L_k}  ‖  C_k( σ_i^V( V_{L_k} ) )
```

where:

- `V_{L_k}` is the leader's value, **plaintext** in the onion. (TBFTR core: gives operators that missed Phase-1 broadcast a recovery channel via peers in Phase 2a.)
- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share.
- `C_k(·)` is the **chained IBE wrapper** under the cluster's IBE keypair (threshold `qEnc = 2f+1`):
  - `C_0(x) = x` — layer 0 is plaintext (the highest-priority layer is always openable).
  - `C_k(x) = E_{nr_tag_{k-1}}(C_{k-1}(x))` for `k ≥ 1` — nested IBE encryption, one wrapper per prior layer's no-quorum tag.
- `E_T(·)` is threshold IBE under tag `T`; a ciphertext under `T` decrypts iff `qEnc` partial sigs on `T` from the IBE keypair exist.
- `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")`.

Chained gating means a σ partial at layer `k > 0` carries `k` nested IBE wrappers (`nr_tag_0` innermost, `nr_tag_{k-1}` outermost). To recover the σ partial, every prior NR-quorum (on `nr_tag_0`, ..., `nr_tag_{k-1}`) must aggregate; decryption peels outermost-first. This **chain-of-NR** gate is what gives the protocol its cross-layer safety property — a deeper-layer σ partial cannot be aggregated unless every shallower layer has actually fallen through. See "Fault tolerance / Safety" for the algebra.

For layers where `i` doesn't have a valid `V_{L_k}` (or observed `L_k` equivocate), the layer slot is **null** in `i`'s onion (no plaintext V, no encrypted σ).

**No non-receipt attestations are broadcast yet.** `i` waits for Phase 2b to commit on the NR side.

(Bandwidth optimization, the "hash variant": instead of full V plaintext at every layer, operators may carry `hash(V_{L_k})` (32 B) at each layer they signed and full `V_{L_k}` plaintext only at the layer where `i` is the leader. The spec algebra is unchanged. **Caveat — hash variant disables the secondary liveness mechanism**: see "Fault tolerance / Liveness" for the primary/secondary breakdown. Under the hash variant, an honest operator that didn't receive V via Phase 1 cannot recover V from a peer's onion (the peer is carrying only `hash(V)` unless it happens to be the layer's leader, and a byzantine leader will withhold its own onion). Recovery collapses to gossipsub re-flooding of the Phase-1 bundle alone — the same robustness as TBFT-shape protocols. Hash variant is only safe to deploy where bandwidth is the binding constraint AND the cluster operates well within partial-synchrony bounds; if marginal-synchrony robustness matters, use the full-V variant. See "Practical caveats" for the bandwidth analysis.)

### Phase 2b — Late σ or non-receipt commitment `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`

For each layer `k` where the operator has not yet committed (neither a σ in their Phase-2a onion nor a Phase-1 σ as the layer-`k` leader):

- If during Phase 2a the operator extracted `V_{L_k}` from a peer's onion and validated it — using `L_k`'s authentication signatures from any Phase-1 bundle the operator has access to (including bundles received late, after `T_candidate_accept`, retained per Phase-1 receiver checks) plus application-level rules — broadcast a **late** σ partial on `V_{L_k}`, wrapped with the same chained IBE encryption used in the Phase-2a onion at layer `k` — i.e., `C_k(σ_i^V(V_{L_k}))`. Late σ at layer `k > 0` is gated by the same chain-of-NR condition as the onion σ: every prior NR-quorum (`nr_tag_0`, ..., `nr_tag_{k-1}`) must aggregate before it can be peeled and counted toward σ-quorum.
- Else (didn't recover V, or recovered V is invalid): broadcast a **non-receipt attestation** `σ_i^{IBE}(nr_tag_k)` from the IBE keypair (only for layers `k ∈ {0, …, K-2}`; the last layer has no successor — see "Treatment of missing onions / late broadcasts").

**Per-operator-per-layer commitment is exclusive across phases.** The σ-or-NR commitment for layer `k` is *one decision per operator per layer, spanning Phase 1, Phase 2a, and Phase 2b*. Concretely:

- The layer-`k` **leader** signed `σ_{L_k}^V` in Phase 1; that is their σ-side commitment for layer `k`. At Phase 2a they include σ on `V_{L_k}` in their own onion uniformly with any other σ-committed operator — Phase 3 dedup collapses Phase-1 σ + Phase-2a onion σ from the same operator to one partial. They **cannot emit NR for layer `k`** — even if their head subsequently moves and `V_{L_k}` is now stale relative to their local view. The σ stays committed; if other honest validate `V_{L_k}` against their heads and contribute σ partials, σ-quorum may reach; if not, σ-pool stays under qV and the cluster falls through via NR-quorum at this layer (driven by the *other* operators' NRs).
- A non-leader operator commits to σ or NR in Phase 2a/2b per the rules above — once committed, no switching.

A byzantine operator that publishes both is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For the layer-`k` leader specifically, "cross-signing" includes any Phase-1 σ + Phase-2b NR pair on the same (slot, layer); the rule applies uniformly across phases.

**Why late σ at `k > 0` is chained-encrypted.** Late σ partials are publicly observable on the gossipsub mesh once broadcast. With chained encryption the late σ at layer `k` is wrapped under the same chain of NR tags as the Phase-2a onion partial — so deeper-layer σ partials (whether onion or late) can only be aggregated when *every* prior layer has reached NR-quorum. This is what closes the cross-layer safety attack: an offline-aggregating byzantine cannot combine σ-partials at one layer with later-layer NR-partials to bypass the per-layer pigeonhole. See "Fault tolerance / Safety / Pigeonhole 3" for the algebra.

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_2a + Δ_2b, finalize]`

Each operator attempts reconstruction:

```
nr_keys = []                                       # accumulated NR decryption keys
                                                    # nr_keys[j] = aggregate(NR partials on nr_tag_j)
                                                    # populated only when nr_tag_j reached qEnc

loop k = 0..K-1:
    # σ pool at layer k. For k > 0, both the onion-encoded σ partials and the
    # Phase-2b late-σ partials are wrapped in the chained IBE C_k = E_{nr_tag_{k-1}}(...
    # E_{nr_tag_0}(σ)). To peel the chain we need every prior NR key — so σ at
    # layer k can only be aggregated if all of nr_tag_0..nr_tag_{k-1} reached qEnc
    # (i.e., len(nr_keys) == k at this point in the loop).
    sigs = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
        ∪ {σ_j^V(V_{L_k}) from Phase-2a onion contents at layer k,
           obtained by peeling C_k with nr_keys (outermost-first) if k > 0}
        ∪ {σ_j^V(V_{L_k}) from Phase-2b late-σ broadcasts at layer k,
           peeled the same way}
        # deduplicated per operator: the leader's own Phase-1 σ and the same
        # leader's onion-layer-k σ are the same partial, counted once.

    if |valid sigs on a single V_{L_k}| ≥ qV:
        output (V_{L_k}, reconstruct(sigs)); halt

    if k == K-1:
        halt with no output                         # missed slot (no successor)

    nrs = {σ_j^{IBE}(nr_tag_k) partials}
          # deduplicated per operator.
    if |valid nrs| ≥ qEnc:
        nr_keys.append(aggregate(nrs))              # threshold sig on nr_tag_k
        continue
    else:
        halt with no output                         # missed slot
```

**`T_arrival`** for the deadline rule is the cutoff by which the operator must have received any Phase-2a onion or Phase-2b broadcast it intends to count — practically, it's `T_commit + Δ_2a + Δ_2b`. The deadline rule bounds the gap between `T_commit` and `T_arrival` against propagation P99/P999 and clock skew (see "Practical caveats / Deadline coordination").

The leader's Phase-1 σ partial appears unencrypted in the σ pool at every layer. At layer `k > 0` this means one partial is visible early, before the chain is peeled — but one partial alone can't reconstruct (need `qV`), and the remaining onion + late σ partials are wrapped in `C_k` and stay sealed until every prior layer's NR-quorum unlocks the chain.

Once a participant produces an output `(V, S)`, it submits to the downstream system. Multiple operators may submit independently; the downstream system de-duplicates. See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers as a fourth wire-envelope kind: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions / late broadcasts

A participant that hasn't received `j`'s onion / late σ / NR at decryption time treats `j` as not having contributed at all. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f` operators are offline or byzantine combined, no quorum reaches its threshold and the slot is missed.

**Last-layer failure is terminal.** NR attestations are only meaningful when there's a successor layer to unlock — they exist for layers `k ∈ {0, …, K-2}` only. If the last layer (`k = K-1`) fails to reach σ-quorum, the slot misses; there's no `nr_tag_{K-1}` and no fallback. Phase 2b emits NR only on tags `nr_tag_0` through `nr_tag_{K-2}`; Phase 3 halts at `k = K-1` after the σ check, regardless of NR partial availability at that layer.

## Preconditions on the host application

(Same as [TBFT](TBFT.md) "Preconditions on the host application" — application-level validity is a host responsibility; TBFTR extends the precondition to apply at *both* Phase 2a's σ commit AND Phase 2b's late σ commit, since the same operator may sign σ in either window.)

Importantly: an operator that submits `(V_{L_k}, S)` after recovering `V_{L_k}` from a peer's onion must still re-run application-level validity checks before signing the late σ. The peer's signature on V doesn't transfer the validity precondition; each signer validates independently.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot, but two stricter constraints apply per (slot, layer):

- **Single σ_V per leader.** The layer-`k` leader signs σ_V exactly once per (slot, layer) — on the final V they commit to, after any pre-signing refreshes during the fetch window. Refreshes update V plaintext via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Head-change handling" in the Application section for the operational workflow.
- **σ-vs-NR exclusivity per operator across phases.** For any (slot, layer), an operator that has signed σ (in Phase 1 as leader, in Phase 2a as a peer, or in Phase 2b as a late signer) cannot subsequently emit NR for the same layer; an operator that has emitted NR cannot subsequently sign σ. This applies *across phases* — the layer-`k` leader's Phase-1 σ counts as their σ-commitment for that layer, and they cannot emit NR in Phase 2b even if their head subsequently moves. EKM enforces this cryptographically: an NR-sign attempt at (slot, layer) is rejected if the same EKM has previously signed σ at the same (slot, layer), and vice versa. This is what makes Pigeonhole 1 (see "Fault tolerance / Safety") cap each honest's contribution to one side per layer.
- Every operator signs each layer's `V_{L_k}` it considers valid in its Phase-2a onion (subject to the cross-phase exclusivity above).
- Operators who recover via Phase-2a peer onions also sign late σ on those values in Phase 2b (same constraint).

EKM/slashing-protection must permit the operator's per-layer Phase-2 σ signings (one per layer with valid V) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — but with the σ-vs-NR exclusivity check above. The cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating point is **candidate signing** (Phase-1 leader and Phase-2 onion/late-σ alike), not just submission.

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (equivocation-to-NR, cross-signing detection, head-change refresh) are still described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`**: up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, lie about bids, etc.). Cluster size is `n = 3f+1`, so `2f+1` honest are guaranteed.
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `D` (propagation P99/P999) and clock skew `δ`. The cutoffs `T_candidate_accept = T_commit − (D + δ)` and `T_arrival = T_commit + Δ_2a + Δ_2b` operationalize this bound. Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

### Safety (cryptographic, unconditional)

**Claim:** at most one full `V` signature is ever produced per TBFTR instance per slot — across any layer, on any value, across any combination of σ sources (Phase-1 leader σ, Phase-2a onion σ, Phase-2b late σ) — cluster-wide, against an offline-aggregating byzantine, regardless of which honest aggregation rules are followed.

The proof rests on three pigeonhole arguments. The first two operate at a single layer; the third extends safety across layers via the chained encryption from Phase 2a / 2b.

**Pigeonhole 1 — σ-vs-NR at the same layer.** σ-quorum on `V_{L_k}` and NR-quorum on `nr_tag_k` cannot both be reached:

- σ-quorum: `h_σ + byz_σ ≥ qV = 2f+1` (where `h_σ` counts honest σ partials at layer k from any phase — Phase-1 leader σ, Phase-2a onion σ, Phase-2b late σ — uniformly).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Honest sign at most one side per layer (across all phases — see "Slashing-protection scope"): `h_σ + h_NR ≤ 2f+1`. **This includes the layer-`k` leader**: their Phase-1 σ counts as their σ-side commitment, and they are protocol-bound not to subsequently emit NR at the same layer (and EKM-prevented from doing so, even after head changes); so they contribute to either σ or NR for layer `k`, never both.
- Each byzantine can sign both sides (cross-signing): `byz_σ + byz_NR ≤ 2f`. A byzantine leader violating the cross-phase exclusivity is bounded the same way — they are one of the f byz, and their σ-and-NR contributions count toward `byz_σ + byz_NR`, not against the honest bound.
- If both quorums reached: `h_σ + h_NR ≥ (2f+1) + (2f+1) − 2f = 2f+2`. But `≤ 2f+1`. Contradiction. ∎

**Pigeonhole 2 — two σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach σ-quorum at the same layer (e.g. via leader equivocation that some honest don't observe in time, or a byzantine signing both):

- σ-quorum on `V`: `h_σ_V + byz_σ_V ≥ qV = 2f+1`.
- σ-quorum on `V'`: `h_σ_V' + byz_σ_V' ≥ qV = 2f+1`.
- Honest sign at most one value at the same layer: `h_σ_V + h_σ_V' ≤ 2f+1`. **The leader counts as one honest if honest, one byzantine if byzantine** — they sign σ_V exactly once per (slot, layer) per the protocol's single-σ-V rule (refreshes are pre-signing only — see "Head-change handling"), so an honest leader contributes to one V's pool, never both. The bound holds whether the leader is honest or byzantine.
- Each byzantine can sign both values: `byz_σ_V + byz_σ_V' ≤ 2f`. A byzantine leader violating the single-σ-V rule would still be bounded here — they're one of the f byzantine, contributing at most one σ partial per V (BLS partial signatures are deterministic given key + message, so the byzantine can't sign the *same* V twice; they can sign two *different* V's, which is precisely what this bound covers).
- If both quorums reached: `(2f+1) + (2f+1) − 2f = 2f+2`. But `(2f+1) + 2f = 4f+1 < 4f+2`. Contradiction. ∎

**Pigeonhole 3 — σ-quorums at different layers can't both reach.** For any two layers `k_1 < k_2`, the chained encryption `C_{k_2}` on layer-`k_2` σ partials nests every prior NR tag — including `nr_tag_{k_1}`. So σ-quorum at layer `k_2` requires `nr_tag_{k_1}`-quorum to have aggregated (otherwise the chain can't peel). But Pigeonhole 1 at layer `k_1` says σ-quorum on `V_{L_{k_1}}` and `nr_tag_{k_1}`-quorum cannot both reach. Therefore:

- σ-quorum at layer `k_1` reaches → `nr_tag_{k_1}`-quorum doesn't reach (Pigeonhole 1)
- → `C_{k_2}` cannot be peeled (chained encryption requires it)
- → σ-quorum at layer `k_2` cannot reach. ∎

Cross-layer safety reduces to Pigeonhole 1 via the chain gate. **Without chained encryption** — i.e., if layer-`k` σ were merely encrypted under `nr_tag_{k-1}` (the immediately-prior NR tag only) — Pigeonhole 3 would not hold for `k_2 − k_1 ≥ 2`: a byzantine could combine σ_{k_1}-partials + NR_{k_2-1}-partials to decrypt and aggregate σ_{k_2}, since Pigeonhole 1 doesn't constrain σ_{k_1} vs `nr_tag_{k_2-1}`. Chained encryption is what makes the cross-layer safety cryptographic rather than honest-only.

None of the three proofs depend on honest operators excluding cross-signers from their aggregation — all are properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. The Phase-2 split doesn't change this: both σ sources (Phase-2a onion and Phase-2b late broadcast) count uniformly against the σ-pool, and both are wrapped in the same chained encryption.

### Liveness (synchrony-conditional)

TBFTR has **two layered closure mechanisms** for byzantine-leader selective-delivery grief. Under partial synchrony with `T_candidate_accept` enforced, the *primary* mechanism — gossipsub re-flooding of Phase-1 bundles — already suffices. The *secondary* mechanism — Phase-2a peer-onion V-plaintext + Phase-2b late σ — extends the synchrony band where the cluster still completes.

**Primary closure (partial synchrony).** Byzantine `L_k` releases `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})` to exactly `f+1` honest operators *before `T_candidate_accept`*, withholds from the remaining `f` honest:

- The `f+1` honest who received V via Phase 1 re-flood the bundle via gossipsub. Within `D + δ`, the bundle reaches the remaining `f` honest before *their* `T_candidate_accept`.
- All `2f+1` honest hold V by Phase 2a start. They include V plaintext + encrypted σ in their onions.
- Cluster-wide σ count on V: `2f+1` honest σ + `1` leader Phase-1 σ = `2f+2 ≥ qV`. **Slot succeeds.**

No Phase-2b late σ is needed in this regime; the recovery channel is dormant.

**Secondary closure (marginal synchrony).** If actual propagation slightly exceeds the budget `D` — gossipsub re-flooding doesn't reach every honest by their cutoff, but Phase-2a onion propagation does — the recovery channel kicks in:

- The `f+1` honest who received V via Phase 1 broadcast Phase-2a onions carrying V plaintext. Their Phase-1 bundle (with the leader's authentication signatures `σ_{L_k}^V`, `σ_{L_k}^{op}`, envelope) continues to gossipsub-propagate; honest receivers retain it for the slot, even when first-observation crosses `T_candidate_accept` (see Phase-1 receiver checks).
- The `f` remaining honest extract V from peer onions during Phase 2a. They validate V using the late-retained Phase-1 bundle's leader auth (the onion itself doesn't re-carry it) plus application-level rules, then broadcast late σ in Phase 2b (wrapped in `C_k` — chained IBE for `k > 0`, plaintext for `k = 0`).
- Cluster-wide σ count on V: `f+1` (Phase-2a onions) + `f` (Phase-2b late) + `1` (leader's Phase-1 σ) = `2f+2 ≥ qV`. **Slot succeeds.**

The composition extends robustness into the marginal-synchrony band where partial synchrony breaks but Phase-2a propagation still completes. A leaner protocol (no Phase-2 split, no V plaintext) misses the slot in this band at `f ≥ 2`, since its single-window σ count caps at `f+1` honest direct + `1` leader = `f+2`, falling short of `qV = 2f+1` once `f ≥ 2`. The marginal band widens with `f`, which is why the additions earn their complexity at larger cluster sizes more than at `n = 4`.

**Coordinated grief across layers.** If multiple byzantine operators each grief a different layer (one does selective delivery at layer 0, another at layer 1, etc.), the cluster falls through to whichever layer has an honest leader. With `K = f+1`, at least one honest leader exists in the top-`K` (byz hold at most `f`); at that honest leader's layer the closure above applies cleanly.

### Failure modes

The slot misses (no V signature is produced) under any of the following:

- **Bad synchrony**: even Phase-2a onion delivery doesn't reach all honest within `Δ_2a`. Some honest emit NR in 2b without recovering V; σ-quorum doesn't form, NR-quorum may or may not depending on distribution. Slot misses; **no safety violation** (the algebra above doesn't depend on synchrony).
- **More than `f` faults**: if more than `f` operators are offline or byzantine combined (beyond the byzantine bound), no quorum reaches its threshold at any layer.
- **Last-layer failure**: if layer `K-1` doesn't reach σ-quorum, there's no successor to fall through to. NR is only emitted on tags `nr_tag_0` through `nr_tag_{K-2}`; last-layer failure is terminal.
- **Cluster-wide head divergence on the only valid layer**: if honest operators on different beacon-chain heads disagree on parent-root validity for every candidate they've received, neither σ-quorum nor NR-quorum may form. See "Head divergence" below.

### Equivocation handling

If a participant observes two distinct `σ_V` partials from the same `L_k` at the same slot/layer (regardless of `parent_root`), that's leader equivocation:

1. Locally treat layer `k` as non-receipt: don't include a positive partial signature for layer `k` in the onion (Phase 2a); broadcast the matching non-receipt attestation in Phase 2b instead (only for `k ≤ K-2`).
2. The pair of signed bundles is a self-contained slashable fault proof against `L_k`.

The leader is required to sign σ_V *exactly once per (slot, layer)*; refreshes during the fetch window are pre-signing only (see "Head-change handling" in the Application section) and don't surface multiple σ_V partials on the wire. Any second σ_V from the same leader is a protocol violation regardless of `parent_root`.

The equivocation rule is what makes Pigeonhole 2 above tight in practice: honest operators who observe the equivocation evidence avoid signing either V at that layer, capping `h_σ_V + h_σ_V'` strictly below `2f+1`. Without the rule, honest could split their σ across the two values; with it, they emit NR instead and the equivocation evidence is gossipped for slashing.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at layer `k` AND an NR attestation on `nr_tag_k` is a slashable cross-signer. The σ source is uniform across phases — a Phase-1 leader σ, a Phase-2a onion σ, and a Phase-2b late σ all count equally as the operator's σ-commitment. The most subtle case is the layer-`k` leader: their Phase-1 σ already commits them to σ-side; emitting NR in Phase 2b after a head change is the same kind of cross-signing as a non-leader signing both σ and NR, and is detectable from the same dual-partial evidence.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it, with the cross-phase commitment captured in the `h_σ + h_NR ≤ 2f+1` bound). The detection is purely for **attribution** and out-of-band punishment. Honest aggregation may filter cross-signers, but doing so is not load-bearing for safety.

**Path-conditional detection limit at deep layers.** At `K ≥ 3`, σ partials at deep layers are encrypted; if an upper layer succeeds, the deep layer doesn't open and σ+NR cross-signing at that depth goes undetected for *attribution*. Doesn't affect safety (the algebra is over published cluster-wide messages and holds whether or not honest aggregate at that depth). Accepted as a path-conditional limit; deep-layer cross-signers may escape attribution but cannot break safety.

### Head divergence

`parent_root` validity is evaluated *locally* by each operator against its own observed beacon-chain head at the moment of accepting the candidate (no later than `T_candidate_accept`). If honest operators temporarily disagree on the current head — e.g., during an in-flight re-org — they may evaluate the same candidate differently:

- An operator on `H1` accepts a `parent_root = H1` candidate and signs σ.
- An operator on `H2` rejects it as stale and emits NR.

This split is a **liveness failure, not slashable equivocation**: the leader broadcast a single signed bundle, no equivocation evidence exists. The σ-pool may not reach `qV` and the NR-pool may not reach `qEnc`, in which case the slot misses with no safety violation. The protocol does not attempt to resolve head disagreement at the cluster level — that's an upstream concern (beacon-chain re-org dynamics), not a TBFTR responsibility.

### Slashing evidence

Three rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to make byzantine misbehavior accountable.

- **Self-contradiction (σ + NR).** If operator `i`'s onion contains `σ_i^V(V_{L_k})` *and* `i` broadcasts `σ_i^{IBE}(nr_tag_k)`, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) — regardless of `parent_root` — are a self-contained slashable fault proof. The leader is required to sign `σ_V` exactly once per (slot, layer); refreshes during the fetch window update V plaintext via re-fetch but happen pre-signing and don't surface multiple `σ_V` partials on the wire (see "Head-change handling" in the Application section). Any observable double-signing is therefore protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` appearing in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different `V` is detectable from the partial sigs alone. Slashable on the same logic.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys), so attribution doesn't require cluster-wide coordination — any observer with the published partials can produce the slashing case.

## Cryptographic primitive

Same as [TBFT](TBFT.md): threshold IBE / signature-based witness encryption (`drand/tlock` or equivalent). The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

## Properties summary

| Property | TBFTR |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1`, unconditional |
| Validity | Yes, conditional on host-application precondition |
| Termination | **No**, single-shot |
| Equivocation detection | Yes |
| Byzantine-leader-grief resistance | **Closed** under partial synchrony via gossipsub re-flooding (primary); marginal-synchrony band closed via Phase-2 composition (secondary) |
| Built-in leader fallback | Yes (K layers) |
| Round-change recovery | No |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block:

| TBFTR concept | SSV mapping |
|---|---|
| `n` participants | 4, 7, 10, or 13 |
| K | `f+1` — 2 for n=4, 3 for n=7, 4 for n=10, 5 for n=13 |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| Leader priority `(L_0, …, L_{K-1})` | reuse existing rotation order |
| `T_commit` | derived from the relay 4s cutoff — e.g. `T_commit ≈ slot_start + 2.7s` |
| `T_candidate_accept` | `T_commit − (D + δ)`; the effective Phase-1 acceptance deadline (~`slot_start + 2.5s` for D + δ ≈ 200 ms) |
| `Δ_1` | block-fetch window (~1s, accommodating worst-of-K beacon-fetch latency) |
| `Δ_2a` | onion broadcast window (~300ms) |
| `Δ_2b` | late-σ window (~150ms) |

Phase timeline (assuming `D + δ ≈ 200 ms`; same shape across `n`):

- Phase 1 fetch: `slot_start + 1.7s` to `slot_start + 2.7s` (top-K leaders fetch and broadcast bundles).
- **Effective Phase 1 acceptance ends at `T_candidate_accept ≈ slot_start + 2.5s`**: candidates first observed by an honest receiver after this point are dropped. The primary-leader's late-fetch window is therefore effectively `T_0` to `T_candidate_accept`, ~200 ms shorter than the nominal `T_0` to `T_commit`. This is the cost of cryptographic-safe re-flooding.
- Phase 2a: `slot_start + 2.7s` to `slot_start + 3.0s` (onions with V plaintext).
- Phase 2b: `slot_start + 3.0s` to `slot_start + 3.15s` (late σ or NR).
- Phase 3: `slot_start + 3.15s` onwards (reconstruct + submit + certificate gossip, headroom for relay 4s cutoff).

The compressed timeline at larger cluster sizes (n=10, 13) is tighter against the relay cutoff; production telemetry should validate the budget — and in particular, telemetry should track `D + δ` so `T_candidate_accept` can be set just-tight-enough to the propagation envelope without over-shrinking the late-fetch window.

### Timing budget

The end-to-end budget for a proposer slot must fit inside the relay submission cutoff (~4 s after `slot_start`). The structure:

```
slot_start
  + pre-consensus            (RANDAO partial-sig collection, ~T_pre)
  + block fetch              (Δ_1; worst-of-K parallel fetches — see below)
  + Phase 2a broadcast       (Δ_2a ≈ 300 ms)
  + Phase 2b broadcast       (Δ_2b ≈ 150 ms)
  + Phase 3 reconstruct      (BLS aggregate, ~few ms)
  + downstream submission    (relay round-trip, ~T_submit)
≤ slot_start + 4s            (relay cutoff)
```

**Worst-of-K beacon-fetch latency.** The top-K leaders fetch in parallel from K distinct beacons. `Δ_1` must accommodate the *slowest* of K independent block-fetch RTTs, not the typical one. Tail-percentile estimation: if a single beacon's fetch is at P99 = `t`, the worst-of-K is approximately at the `(1 − (1−P99)^K)`-percentile of the underlying distribution. For K=3 (n=7) at single-fetch P99 = 800 ms, worst-of-3 P99 ≈ 950–1000 ms; for K=5 (n=13), worst-of-5 ≈ 1.1–1.2 s. Δ_1 must be sized accordingly.

Concrete numbers for each leg should come from production telemetry. Until that lands, the Phase-timeline above is a placeholder default; tighten per cluster size as data arrives.

The deadline-tuning rule from caveat 5 below applies: `T_commit − T_arrival > D + δ` where `D` is the propagation P99/P999 and `δ` is the bounded clock-skew across operators.

### Head-change handling

If the head changes during the Phase-1 fetch window (between `T_commit − Δ_1` and `T_commit`), candidate values fetched from the previous head are stale (their parent root no longer matches the new head). The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, the leader's σ_V is locked on the originally-signed V. **The leader of layer `k` also cannot subsequently emit NR on `nr_tag_k`** — the Phase-1 σ is their σ-side commitment per the cross-phase exclusivity rule (see Phase 2b's "Per-operator-per-layer commitment is exclusive across phases"). Honest operators (other than the layer-`k` leader) validate received candidates against *their* head at candidate-acceptance time:

- If their head matches the leader's signed V (`parent_root` matches): they accept and contribute to the σ-pool on that V.
- If their head has moved past it: they reject the bundle (stale parent) and emit NR for the layer.

Outcome under post-signing head-change: cluster either σ-quorums on the originally-signed V (if a majority of honest are still on the matching head) or falls through to the next layer via NR. Single output, safety preserved.

The structured envelope binds `parent_root`, so the (now-stricter) equivocation rule has clean evidence: **any two distinct `σ_V` partials from the same leader at the same slot/layer are slashable equivocation, regardless of `parent_root`** (see "Fault tolerance / Equivocation handling"). A leader following the protocol produces exactly one σ_V; multiple σ_V partials are a self-contained fault proof against the leader. `parent_root` differences in re-broadcasts do not exempt the equivocation — refreshes are pre-signing-internal and don't surface multiple σ_V partials on the wire.

`parent_root` validity is evaluated locally against each operator's observed beacon-chain head at candidate-acceptance time. Honest operators on different heads (in-flight re-org) may reach different validity conclusions on the same candidate; that's a liveness concern handled at the protocol level by "Fault tolerance / Head divergence", not slashable equivocation.

Implementation notes:

- Each operator must track the current head locally and validate `parent_root` of received candidates against it.
- The leader's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" — a second signing attempt at the same (slot, layer) is rejected. This is the cryptographic enforcement of the single-σ rule on the leader side; combined with the strengthened equivocation rule it makes byzantine-leader double-signing detectable cluster-wide.

## Practical caveats

1. **Bandwidth: V plaintext vs hash variant.** Carrying full `V` plaintext at every onion layer scales as `K · |V| · n` per cluster, which exceeds 200 KB at n=10 and 400 KB at n=13. The hash variant (full V at the leader's own layer, 32-B hashes elsewhere) cuts onion growth from `K · |V|` to `K · 32B + |V|`. **Trade-off**: hash variant disables Phase-2a peer-onion V-recovery (see "Phase 2a" caveat and "Fault tolerance / Liveness" → secondary mechanism), so the cluster's robustness against byzantine-leader-grief reduces to the same partial-synchrony assumption as TBFT-shape protocols at n=4. Pick hash variant only when bandwidth is the binding constraint and partial synchrony is reliable; pick full-V variant when marginal-synchrony robustness matters. Hash-only domain separation: hash by `(slot, cluster, layer, leader)` so hashes can't be replayed across slots/layers.

2. **Phase 2b latency.** The composition adds `Δ_2b` (~100–200 ms on a healthy mesh) over plain TBFT-style timing. Tight against the 4s relay cutoff at n=10/13; the timing budget needs to be tracked against production gossip-propagation P99/P999.

3. **Chained-encryption cost.** σ partials at layer `k > 0` are wrapped in `k` nested IBE encryptions (one per prior `nr_tag`). Per onion, the deepest layer carries `K-1` nested wrappers; total encryption ops summed across all layers per onion is `K(K-1)/2` (so 1 op at K=2, 3 ops at K=3, 6 ops at K=4, 10 ops at K=5). Each IBE wrapper adds a small constant ciphertext expansion (typically a few hundred bytes per wrapper for `drand/tlock`-style constructions) — at n=13 (K=5), ~500 B per partial × n operators ≈ ~6.5 KB cluster-wide additional vs a hypothetical single-tag scheme. Decryption is symmetric: peel `k` wrappers at layer `k`, using the cumulatively-aggregated NR keys (outermost first). Per-op latency is microseconds; total chain peeling is a few hundred microseconds at K=5 — negligible against the protocol's per-slot timing budget. The chained encryption is what closes the cross-layer safety attack at K ≥ 3 (see "Fault tolerance / Safety / Pigeonhole 3"); a single-tag scheme would require honest-only enforcement to be safe and is not recommended.

4. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

5. **Deadline coordination.** Clock skew across operators must be bounded by `δ`. Two cutoffs derived from `D` (propagation P99/P999) and `δ` together drive the partial-synchrony assumption:

   - **`T_candidate_accept = T_commit − (D + δ)`** for Phase-1 candidates. Receivers reject candidates whose first-observation time is later.
   - **`T_arrival = T_commit + Δ_2a + Δ_2b`** for Phase-2a onion / Phase-2b late-σ / NR contributions — the cutoff for accepting Phase-2 messages into the local pools. Same `D + δ` budget against `T_arrival`.

   Both are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

6. **Tag construction and replay.** The `nr_tag_k` tags must uniquely bind `(slot, cluster, layer)` so that ciphertexts from one slot/cluster/layer cannot be replayed/reused.

7. **"At most one full sig" is per-instance.** Holds within one TBFTR instance and assumes:
   - Single TBFTR instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between TBFTR and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase-1 leader and Phase-2 onion/late σ alike), not just submission.

   With TBFTR each operator's V-share signs `K` distinct values across its onion plus possibly more in late-σ broadcasts; EKM must permit this without flagging duplicates.

## Where this came from

TBFTR builds on [TBFT](TBFT.md) (originally "Proposal 3" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829)). The two TBFTR additions are:

- **V plaintext in onions**: gives operators that missed Phase 1 a recovery channel via peers' Phase-2a onions.
- **Phase 2 split (composition)**: defers non-receipt commitment to Phase 2b, allowing late σ broadcasts from operators who recovered V via the plaintext channel.

Both additions form the *secondary* closure mechanism described in "Fault tolerance / Liveness". Under partial synchrony, the *primary* closure (gossipsub re-flooding under `T_candidate_accept`) handles the byzantine-leader grief at any `f` without help from these additions. The TBFTR additions extend closure into the marginal-synchrony band; that band widens with `f`, which is why the additions earn their complexity more at larger cluster sizes than at `n = 4`. At `n = 4`, [TBFT](TBFT.md) is the leaner alternative that drops the additions for a smaller protocol footprint.

## Appendix A — How TBFTR differs from TBFT

Both protocols share the same cryptographic core (`qEnc = qV = 2f+1`, leader-authenticated candidates with both V-keypair and operator-identity sigs over a structured envelope, equivocation-to-NR rule, IBE primitive, two DKGs at the same threshold). The differences are in onion structure, Phase-2 timing, and the resulting fault-tolerance band — comparing both at the same `n = 4, K = 2` cluster size to make the trade-off concrete:

| Aspect | [TBFT](TBFT.md) (n=4, K=2) | TBFTR (n=4, K=2) |
|---|---|---|
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Equivocation-to-NR rule | Yes | Yes |
| qV / qEnc | `qV = qEnc = 2f+1 = 3` | Same |
| Onion at layer k | `E_{nr_tag_0}(σ_i^V(V_{L_k}))` (encrypted partial only; at K=2 the chained wrapper has only one tag) | `V_{L_k} ‖ C_k(σ_i^V(V_{L_k}))` (V plaintext + chained-IBE-wrapped partial; `C_k` reduces to single-tag encryption at K=2 and to full chain at K ≥ 3) |
| Phase 2 timing | Single window `[T_commit, T_commit + Δ_2]` (onion + NR together) | Split: 2a (onion only) + 2b (late σ or NR) |
| Late σ broadcasts | None | Phase 2b — operators who recovered V via peer onions sign σ then |
| Byzantine-leader-grief closure | Primary only (gossipsub re-flooding under partial synchrony) | Primary + secondary (Phase-2 composition extends closure into a marginal-synchrony band; full-V variant only) |
| Bandwidth (worst case) | ~21 KB | larger by V-plaintext + Phase-2b overhead (slot-dependent) |
| Latency overhead | None beyond standard Phase 2 | +Δ_2b (~100–200 ms) |

At `n ≥ 7`, only TBFTR is supported — the marginal-synchrony band widens with `f` enough that the secondary closure becomes load-bearing, and the leaner TBFT-shape protocol's single-window σ count caps at `f+2 < qV = 2f+1` once `f ≥ 2`, missing slots in that band.

**If you're choosing between protocols at `n = 4`**: pick TBFT if the cluster operates well within partial-synchrony bounds and minimal protocol complexity is preferred (the secondary closure rarely earns its bandwidth/latency premium); pick TBFTR if marginal-synchrony robustness is worth that premium. Either is cryptographically safe.

## Appendix B — Dynamic leader-ordering extensions

This appendix sketches two related extensions where the choice of which leader's value the cluster commits to is **not fixed by priority** but **emerges from a deterministic rule** over the candidates the cluster actually had time to validate. Neither is part of the baseline TBFTR spec; both are forward-compatible directions documented here because they fall naturally out of the same 2a/2b machinery and may be relevant if production data shows head-divergence or per-slot leader-quality variance hurting liveness.

- **B.1** notes how the **bid-ordered selection** sketched for TBFT in [TBFT.md](TBFT.md) Appendix B extends to K > 2 — application-driven deterministic rule, cluster-consistent inputs, attribution-friendly.
- **B.2** is the **general dynamic-leader-ordering** sketch — protocol-shape independent of any specific deterministic rule — and includes a note on why parent-root-based "ordering" fits naturally into it but doesn't actually buy much.

The safety machinery (per-layer commit-tags + IBE-encrypted σ partials + per-operator commit exclusivity) is shared between the two; only the deterministic rule differs.

**Status: design sketches, not specified for implementation.** The safety argument carries through under the same cryptographic primitives, but several details (exact commitment-rule semantics, late-commit handling, interaction with equivocation, slashing-protection scope) need precise specification before either could be deployed.

### B.1 — Extending TBFT's bid-ordered selection to K > 2

The bid-ordered variant for TBFT ([TBFT.md](TBFT.md) Appendix B) generalizes naturally to TBFTR's K-layer setting without changing the cryptographic core or the Phase-2 split. The full mechanism is described over there — what's worth noting here is how it fits within TBFTR specifically:

- **Phase 1 envelope** picks up an additional `bid` field, signed alongside `value_root` and `parent_root`. Same equivocation rules; same `T_candidate_accept` cutoff.
- **Phase 2a onion** carries V plaintext at every locally-validated layer (TBFTR core, unchanged) plus *one* encrypted σ partial at the layer the operator commits to, where the commit is `argmax_k bid_k` over locally-validated `V_{L_k}`. Tiebreaker on equal bids: lower `leader_id`. The σ partial is encrypted under the chosen layer's `commit_tag_k` (K total tags, one per layer).
- **Phase 2b** carries late commits + encrypted σ for operators who recovered a candidate during 2a and didn't commit yet. No NR side.
- **Phase 3** walks "find the layer with commit-quorum" across all K layers, same shape as the general variant in B.2 below.

Bandwidth at K > 2 is slightly better than the baseline TBFTR onion (one encrypted σ + one commit partial per operator instead of K encrypted σ + per-layer NR), but the per-onion savings are small and the K commit-tags add some constant overhead. Latency is unchanged (same 2a/2b windows).

The wins are the same as at K=2 — cluster routes to the highest-bid valid layer directly, no NR-walk steps when `L_0` is unavailable / lying high — just generalized over more layers. The attribution story (post-hoc relay-bid verification) carries over unchanged: each leader's signed envelope plus the relay-reported actual bid for the published block forms self-contained liveness-fault evidence.

For SSV's proposer-duty case, the natural prototype path is **TBFT first, TBFTR second** — the K=2 design space is much smaller, the safety/liveness analysis is cleaner, and any production lessons translate directly. Deploying at K > 2 only makes sense after the K=2 variant has been validated.

### B.2 — General dynamic leader-ordering

#### Motivation

Baseline TBFTR walks layers in **priority order**: layer 0 is tried first; only if `nr_tag_0` reaches `qEnc` does layer 1 become reachable. The fixed priority is what enables the `qEnc = qV` safety pigeonhole — each honest operator commits to either σ or NR per layer, mutually exclusively, so at most one σ-quorum can materialize across the entire layer walk.

The cost of fixed priority: if `L_0`'s candidate happens to be the worst choice for *this* slot (e.g., its parent root is in a re-org-divergent zone, or its application-level validity differs across honest operators' local views), the cluster has to "burn" an NR-quorum on layer 0 before getting to a more convergent layer. That uses up the same `2f+1` honest-agreement budget that signing layer `k` directly would have used — so it's not strictly worse, but it's strictly slower (extra IBE decryption walk step) and depends on operators being able to converge on NR for the layer they're skipping.

The dynamic-ordering variant generalizes "layer 0 first, fall through" to "any layer can win, decided by which layer's commit-quorum lands first" — without giving up the cryptographic safety property.

#### Core idea

Replace per-layer σ-XOR-NR commits with **per-operator commitment to at most one layer**. Each layer gets its own IBE tag (`commit_tag_k`); each operator's σ partial at their chosen layer is encrypted under that layer's commit-tag. Aggregation finds the layer whose commit-quorum reaches `qEnc` — the same safety pigeonhole applies, just keyed on layers instead of σ-vs-NR sides:

- `h_commit_k` = honest who committed to layer `k`.
- `byz_commit_k` ≤ `f` per layer (each byz can cross-commit, but each contributes at most 1 partial per layer).
- For two layers `k_1 ≠ k_2` to both reach commit-quorum: `h_commit_{k_1} + h_commit_{k_2} ≥ 2 · qEnc − 2f = 2(2f+1) − 2f = 2f+2`. But honest commit to ≤ 1 layer each, so `h_commit_{k_1} + h_commit_{k_2} ≤ 2f+1`. Contradiction — at most one commit-quorum reaches. ∎

Each layer that *does* reach commit-quorum yields a single output. Same cryptographic safety as baseline TBFTR.

#### Protocol shape (delta from baseline TBFTR)

**Setting (unchanged):** same K leaders, same DKGs, same `qV = qEnc = 2f+1`, same envelope binding, same `T_candidate_accept`. One additional tag family: `commit_tag_k = ("slot", N, "cluster", C, "layer", k, "commit")` for `k ∈ {0, …, K−1}` — `K` tags total instead of baseline's `K−1` `nr_tag_k`.

**Phase 1 (unchanged):** every leader broadcasts `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op}(envelope))` to peers. Receivers verify, validate, check the cutoff, drop invalid bundles.

**Phase 2a (modified):** each operator `i` broadcasts:

- `V_{L_k}` plaintext at every layer `k` where `i` accepted and validated `V_{L_k}` (recovery channel — same as baseline TBFTR core).
- A **single** committed layer `k*_i ∈ {0, …, K−1}`, chosen by a deterministic rule over the candidates `i` validated (e.g. "lowest-indexed layer where `V_{L_k}` validates against my local head" — recovers the baseline primary-first behavior in the common case where everyone validates `V_{L_0}`).
- `σ_i^{IBE}(commit_tag_{k*_i})` — the commitment partial sig.
- `E_{commit_tag_{k*_i}}( σ_i^V(V_{L_{k*_i}}) )` — `i`'s σ partial on the chosen layer's value, encrypted under that layer's commit-tag.

If `i` has no valid candidate at any layer by `T_candidate_accept`, `i` skips Phase 2a (no commitment, no encrypted σ).

**Phase 2b (modified):** for operators who didn't commit in 2a but recovered a candidate via peer onions during 2a — they pick a layer using the same deterministic rule, and broadcast their late commitment + encrypted σ for that layer. Operators who already committed in 2a don't re-emit. (Baseline 2b's "late σ or NR" collapses to "late commitment only"; NR is gone — there's no longer a separate NR side, just "did or didn't commit to some layer.")

**Phase 3 (modified):** each operator runs:

```
for k = 0..K-1:
    commits_k = {σ_j^{IBE}(commit_tag_k) partials}
    if |valid commits_k| ≥ qEnc:
        decryption_key_k = aggregate(commits_k)
        sigs_k = decrypt and verify all E_{commit_tag_k}(σ_j^V(V_{L_k})) ciphertexts
                 ∪ {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
        if |valid sigs_k on V_{L_k}| ≥ qV:
            output (V_{L_k}, reconstruct(sigs_k)); halt

halt with no output            # no layer reached commit-quorum
```

The walk is over commit-quorums, not over priority. By the safety pigeonhole, at most one `k` satisfies `|commits_k| ≥ qEnc`.

#### Where this wins (and where it doesn't)

**Wins:**

- **Direct backup commit on head divergence.** If `2f+1` honest validate `V_{L_1}` (say, against a deeper-confirmed parent) but the split on `V_{L_0}`'s parent prevents agreement at layer 0, dynamic ordering converges directly on layer 1. Baseline TBFTR would need `qEnc = 2f+1` honest to NR layer 0 first — same threshold, but the protocol burns an extra IBE decryption walk step getting there.
- **Per-slot leader-quality variance.** If a particular slot has `L_0` producing a marginal candidate (e.g. low MEV, non-canonical relay) but `L_1` producing an objectively better one, operators following the deterministic rule can converge on `L_1` directly without the round-trip through `nr_tag_0`. (Baseline TBFTR is locked into the priority order regardless of candidate quality.)

**Doesn't win:**

- **Doesn't reduce the agreement threshold.** Both variants need `2f+1` honest to converge on the same layer. Dynamic ordering changes *which* layer they converge on, not whether they need to converge.
- **Doesn't help if honest can't agree on a deterministic rule.** The rule has to be over inputs that are cluster-consistent enough — "lowest-indexed valid layer" works under partial synchrony if all honest validate the same V's; head divergence makes the inputs differ, and the variant is no better than baseline in that case (and might be worse if the rule splits operators across layers in ways the baseline wouldn't).
- **Doesn't help against byzantine-leader grief at `f ≥ 2`.** That's already closed by TBFTR core (V plaintext + Phase-2 split) under partial synchrony. Dynamic ordering is orthogonal.

#### Trade-offs vs baseline

| Aspect | Baseline TBFTR | Dynamic-ordering variant |
|---|---|---|
| Per-operator commit | σ XOR NR per layer, multi-layer | One layer per slot, exclusive |
| Tag count | `K−1` `nr_tag_k` | `K` `commit_tag_k` |
| σ partials per onion | At every layer with valid V (encrypted) | One (encrypted, at chosen layer) |
| Phase 3 walk | Priority order (layer 0 first, NR-quorum unlocks next) | Find layer with commit-quorum |
| Primary-bias optimization | Built in (priority order) | Recoverable via deterministic rule, but rule-dependent |
| Bandwidth per onion | `K · |V|` plaintext + `K` encrypted σ + (separately) per-layer NR | `K · |V|` plaintext + 1 encrypted σ + 1 commit partial — slightly less per onion |
| Liveness in head-divergence | Same agreement threshold; baseline burns an NR-walk step before reaching layer 1 | Same threshold; converges directly on agreed layer |
| Slashing-protection scope | σ at every valid layer + NR per missed layer | σ at one layer only — simpler EKM accounting |

#### Open questions before this could be specified

- **Late-commit timing.** Can an operator who committed in 2a "switch" their commit if they later learn their chosen layer won't reach commit-quorum? Naively no (would break per-operator exclusivity), but more subtle schemes (revocable commits with deadlines) might be possible. Adds protocol-state complexity.
- **Equivocation interaction.** Baseline's equivocation-to-NR rule converts a layer with a misbehaving leader into a clean NR-quorum. The dynamic variant has no NR side — equivocation at layer `k` would have to translate to "operators won't commit to layer `k`," not "operators commit to NR_k." Defining this precisely (and ensuring no path to two outputs) needs care.
- **Slashing-protection scope.** Per-share signing log shows σ at one layer per slot under this variant — simpler than baseline. But the per-operator picking rule means EKM has to allow σ at *any* layer per slot, not a fixed one. Same envelope-bound slashing-protection check applies.
- **Last-layer fallback.** Baseline TBFTR makes last-layer failure terminal because there's no layer K to unlock. The dynamic variant doesn't gate by NR-quorum on the previous layer, so the analog is: what does "no commit-quorum at any layer" mean for the protocol's halt condition? Likely the same — slot misses — but worth specifying.
- **Hash variant interaction.** The hash variant disables Phase-2a peer-onion V-recovery (see "Phase 2a" caveat). Under dynamic ordering, the secondary closure path is the same as in baseline — so the hash variant has the same disabling effect. Worth re-confirming once the variant is fully specified.

#### Choosing the deterministic rule

The protocol shape above is independent of *what* deterministic rule operators use to pick `k*_i`. The rule's only job is to be (a) deterministic over the inputs each operator has at commit time, (b) cluster-consistent enough that 2f+1 honest converge on the same `k*_i`, and (c) defined when at least one layer's candidate validates locally. Two candidate rules:

- **Bid-based (recommended; see B.1).** `argmax_k bid_k` over locally-validated layers, with leader-id tiebreak. Bids are signed in the envelope, byte-identical for every honest receiver, so the rule's output is cluster-consistent under partial synchrony. Lies are bounded (single layer per byz, can't cross-commit beyond f) and attributable post-hoc from the signed envelope + relay record.
- **Parent-root-based ("commit to the layer whose parent_root matches my canonical chain; tiebreak by layer-index").** Fits the protocol shape but doesn't actually buy much: parent-root is a *validity filter*, not a ranking comparator, so the "rule" collapses to "fixed priority among locally-valid layers" — essentially baseline TBFTR's behavior, just routed through commit-tags instead of NR. Worse, the rule's input (parent-root match against local head) isn't cluster-consistent — different operators' beacon-node views of the canonical chain can diverge during a re-org, and the rule's *output* fragments along the same line. Adding parent-root as a routing primitive doesn't fix head-divergence-driven misses; it just relocates them. The cleaner mitigation for head-divergence is application-level (deeper-confirmed parent for non-priority layers), see [TBFT.md](TBFT.md) Appendix B.2 for the corresponding analysis at K=2.

The bid-based rule is the cleaner of the two — and is the one we'd recommend if any dynamic-ordering scheme is pursued.

#### When to consider this

This variant is most relevant if production data shows the baseline TBFTR's fixed priority order causing measurable misses or wasted slots — e.g., re-org-frequent periods where layer 0 routinely fails its NR-quorum step before layer 1 picks up. Without that data, the baseline's simplicity is the right default.

If pursued, the natural place to prototype is **TBFT first** (K=2, see B.1 → [TBFT.md](TBFT.md) Appendix B for the concrete instantiation), then TBFTR. The K=2 design space is much smaller, the safety/liveness analysis is cleaner, and any production lessons translate directly to the K-layer generalization here.

For SSV's n=4 case specifically, the lighter mitigation — fetching `V_{L_1}` from a deeper-confirmed parent so the backup is structurally re-org-resistant — gets most of the benefit without any protocol change. That's the recommended first move; the dynamic-ordering variant is a second-line option for cluster sizes and conditions where the application-level mitigation isn't enough.

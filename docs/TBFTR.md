# TBFTR — Threshold BFT (Roach)

A single-shot agreement protocol that produces one collective threshold-signed value per "slot" against a hard deadline, with full byzantine-leader selective-delivery resilience under partial synchrony plus an additional secondary-closure layer that — at `f ≥ 2` — extends marginal-synchrony coverage from "1 honest missing re-flood" (what a leaner leader-σ-V-only mechanism covers) to "up to `f` honest missing".

TBFTR runs at any cluster size `n = 3f+1` with `f ≥ 1`. SSV's supported cluster sizes are `n ∈ {4, 7, 10, 13}`. Two additions over a minimal TBFT-shape protocol:

- **V plaintext in onions**: each operator's onion at layer `k` carries `V_{L_k}` plaintext alongside the encrypted partial sig, so an operator that didn't receive `V_{L_k}` during Phase 1 can recover it from any peer's onion in Phase 2.
- **Phase 2 split**: Phase 2 splits into 2a (onion broadcast, no NR yet) and 2b (late σ from operators who recovered V; NR otherwise). Together with V plaintext, this lets honest operators that missed V via Phase-1 propagation still contribute σ partials, reaching `qV` and reconstructing.

Under partial synchrony with re-flood headroom (byz releases the Phase-1 bundle at least `D + δ` before `T_candidate_accept`), gossipsub re-flooding handles byzantine-leader selective delivery at any `f`. The **byzantine-leader-at-cutoff edge** (byz times release at the cutoff so worst-case skew lands the re-flood past remaining honest's cutoffs) is **not** fully closed by primary alone: at `f = 1` TBFT-shape's `f+2 = qV` math closes it; at `f ≥ 2` only the **full-V** variant's secondary closure covers it, and the **hash variant carries a residual partial-synchrony miss surface** unless `T_candidate_accept` is moved earlier — see "Fault tolerance / Liveness". The TBFTR additions form the *secondary* closure for the marginal-synchrony band, gated by a **timely-acceptance witness threshold** (≥ `f+1` distinct peers' Phase-2a σ partials on the recovered V) that prevents a byzantine leader from bypassing the timing cutoff via late retention. A leaner protocol caps marginal coverage at "1 honest missing re-flood"; TBFTR pushes it out to "up to `f` honest missing" — the widening is `f − 1`, so:

- At `n = 4` (`f = 1`): widening is **zero** — the witness threshold bound (`f+1 = 2` honest signed Phase-2a σ) coincides with the leaner protocol's coverage. The secondary closure adds redundancy (more partials in the same band) but doesn't extend the band itself. The lean alternative is preferred unless that extra redundancy specifically matters in production.
- At `n = 7` (`f = 2`): widening is +1 honest (1-of-5 → 2-of-5). The leaner protocol misses at `missing = 2` while TBFTR closes — the secondary closure starts earning its complexity here.
- At `n = 10` (`f = 3`): widening is +2 honest (1-of-7 → 3-of-7). Secondary closure is load-bearing in plausible production regimes.

The cost is extra bandwidth (~10–80 KB per slot depending on cluster size) and one extra gossip window (`Δ_2b`, ~250 ms for the worked SSV example with `D + δ ≈ 200 ms`; sized strictly above `D + δ` per the per-window deadline rule).

For SSV's `n = 4` clusters, [TBFT](TBFT.md) is a leaner alternative: same cryptographic core, drops the V-plaintext + Phase-2-split machinery, lower bandwidth and latency. See **Appendix A** for a side-by-side comparison.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it

**Suited for:** SSV clusters of any supported size (`n ∈ {4, 7, 10, 13}`), single-shot duties with a fixed deadline, where missing the slot is the natural failure mode. The cluster's bandwidth budget can absorb the V-plaintext / late-σ overhead, and the timing budget can absorb the extra Phase-2b window.

**At `n = 4`,** TBFTR works but [TBFT](TBFT.md) is leaner. The two protocols cover the **same** marginal-synchrony band at `f = 1` (≤ 1 of 3 honest missing re-flood); TBFTR adds redundancy in that band (more σ partials via the secondary closure) but doesn't extend the band itself — the witness threshold's `f+1 = 2` lower bound coincides with the leaner protocol's coverage. Pick TBFTR-at-n=4 only if that redundancy specifically matters; pick TBFT for minimal protocol complexity at no coverage cost.

## Setting

- A cluster of `n = 3f + 1` participants with `f ≥ 1`. SSV's supported cluster sizes are `n ∈ {4, 7, 10, 13}` (so `f ∈ {1, 2, 3, 4}`).
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) to sign no-quorum tags and (b) as the decryption oracle for threshold IBE. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety".
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- For each slot, a **leader priority order** `(L_0, L_1, …, L_{n-1})` deterministically derived from slot data. `L_0` is the highest-priority leader. (0-based indexing throughout.)
- A **fallback depth** `K = f+1` per cluster: `K=2` for n=4, `K=3` for n=7, `K=4` for n=10, `K=5` for n=13. (`K = f+1` ensures at least one honest leader exists in the top-`K` since byz can hold at most `f`.)
- Per-slot deadlines: `T_commit` (Phase 1 ends, Phase 2a starts), `T_commit + Δ_2a` (Phase 2a ends, Phase 2b starts), `T_commit + Δ_2a + Δ_2b` (Phase 2b ends, Phase 3 starts).
- A **candidate acceptance cutoff** `T_candidate_accept = T_commit − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers treat any Phase-1 candidate whose first-observation time is later than `T_candidate_accept` as not-accepted-as-candidate (auth-only retention is permitted — see Phase 1 / Bundle propagation). The cutoff bounds late acceptance: byzantine cannot get a bundle accepted as a Phase-1 candidate after `T_candidate_accept`. It does **not** by itself guarantee that an honest re-flood started at exactly the cutoff reaches every other honest before *their* cutoffs in the worst-case clock-skew scenario — that **byzantine-leader-at-cutoff edge** is the residual surface that full-V's Phase-2a peer-onion V-recovery + Phase-2b late σ (the secondary closure) handles at `f ≥ 2`; the hash variant accepts it as a residual miss surface unless the cutoff is tightened (e.g., to `T_commit − (2D + δ)`) at the cost of squeezing the leader's fetch window. See "Fault tolerance / Liveness" for the full breakdown.

## Protocol

### Phase 1 — Candidate broadcast `[T_commit − Δ_1, T_commit]`

Each leader `L_k` for `k ∈ {0, …, K−1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "TBFTR-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates TBFTR Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, other SSV protocols, future TBFTR message kinds, etc.) — the operator-identity key is shared with these other uses, and without an explicit protocol/kind tag a TBFTR envelope encoding could collide with another protocol's signed-payload encoding. The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run two **independent** kinds of checks:

1. **Cryptographic-auth checks** (BFT-internal, structural): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs. Bundles failing these are **silently dropped — neither accepted nor retained** (a leader who broadcasts `(V, σ^{op})` without `σ^V`, or with a bad signature, is treated as not having broadcast at all).
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict.

Bundles passing cryptographic-auth checks are then classified by the first-observation timestamp + application verdict:

- **First-observation ≤ T_candidate_accept AND application says valid**: bundle is **accepted** as a Phase-1 candidate. The operator may sign σ on `V_{L_k}` in Phase 2a (or stand by their Phase-1 σ if they are the layer's leader — see Phase 2b's commitment rule).
- **First-observation ≤ T_candidate_accept AND application says not-valid**: bundle is **not accepted for σ commitment**. The operator's commitment for this layer will be a **non-validity attestation (NV)** in Phase 2b — operationally identical to a non-receipt attestation (NR), counted in the same no-σ pool. Diagnostic distinction is local-only (see "Operator commitments — σ, NR, NV"). The leader auth signatures are still retained for the slot (see next bullet's retention rule).
- **First-observation > T_candidate_accept**: bundle is **not accepted as a Phase-1 candidate** (the cutoff prevents timing-based selective delivery from fragmenting the σ-pool — see "Bundle propagation" below). The cryptographic-auth checks have already passed at this point, so the leader's authentication signatures (`σ_{L_k}^V`, `σ_{L_k}^{op}`, envelope) are **retained for the slot, auth-only**. They can be used in Phase 2b to validate `V_{L_k}` recovered from a Phase-2a peer onion — see Phase 2b. Auth-only retention does *not* allow the operator to sign σ in their own Phase-2a onion (which would re-open the timing-fragmentation attack the cutoff exists to prevent), and any Phase-2b late σ based on this auth is also gated by the **timely-acceptance witness threshold** (≥ `f+1` distinct peers' Phase-2a σ partials on the recovered `V_{L_k}`) — without that witness count the operator must emit NR/NV instead of late-signing. The witness threshold ensures at least one honest peer accepted the bundle on time, preventing a byzantine leader from bypassing the cutoff via late retention.

  **Retention bounds.** Retention state is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient for both Phase-2b auth-validation of recovered V *and* leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Retention lifetime: until the operator's local end of Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. This caps memory at `O(K · n)` bundles per slot in the worst case (every leader equivocates), well under any practical pressure budget.

The split is load-bearing: a malformed or bad-signature bundle has no use at all — drop it. A late-but-cryptographically-valid bundle has limited use — its leader auth is the evidence the secondary closure path needs in Phase 2b, even though the bundle itself can no longer fragment the σ-pool by being accepted as a Phase-1 candidate.

**Bundle propagation.** Honest receivers re-flood the bundle via standard gossipsub. The **candidate acceptance cutoff** is what bounds the byzantine leader's late-release window: a bundle first observed after `T_candidate_accept` is treated as auth-only and cannot fragment the σ-pool via timing.

A subtlety in the clock-skew arithmetic: the cutoff `T_candidate_accept = T_commit − (D + δ)` is enough to bound *late* delivery (the byzantine cannot first-deliver to any honest after their `T_candidate_accept` without all of them rejecting), but it does **not** by itself guarantee that a re-flood from an honest receiver who accepted *exactly at* `T_candidate_accept` (in their own clock) reaches all *other* honest receivers before *their* `T_candidate_accept` in the worst-case clock-skew scenario — the re-flood still takes `D` to propagate, and a slow-clock peer's cutoff (in absolute time) can be earlier than the fast-clock acceptor's, so the re-flood arrival can land in the late-retention window for some peers. In that edge case, those peers retain leader auth but cannot sign Phase-2a σ. Whether the cluster still closes:

- **Full-V variant**: the f peers that fell into late-retention can still recover V from a Phase-2a peer onion (carried plaintext by the f+1 honest who did sign Phase-2a σ on time) and contribute Phase-2b late σ — gated by the witness threshold, which is met because f+1 honest signed Phase-2a σ. The σ pool reaches `qV` via the secondary closure (see "Liveness").
- **Hash variant** (and TBFT-shape protocols generally): no peer-onion V-recovery, so the f late-retainers cannot late-sign. σ pool = `f+1` honest Phase-2a σ + 1 leader Phase-1 σ = `f+2`, reaching `qV = 2f+1` only at `f = 1`. At `f ≥ 2` the slot misses in this byzantine-leader-at-cutoff edge under the hash variant.

This is the same partial-synchrony envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)), made operational by a concrete cutoff. The cutoff could be tightened (e.g., to `T_commit − (2D + δ)` to give re-flood a full propagation budget) at the cost of shrinking the leader's fetch window — see "Practical caveats / Deadline coordination" for the trade-off; the docs as written use the looser `T_commit − (D + δ)` cutoff and rely on full-V secondary closure to handle the byzantine-leader-at-cutoff edge.

**Equivocation handling.** If a participant observes two distinct `σ_V` partials from the same `L_k` at the same slot/layer, that's leader equivocation: locally treat layer `k` as non-receipt (don't include a positive partial signature for it in the onion; broadcast the matching non-σ attestation in Phase 2b instead, for `k ≤ K-2`). The pair of signed bundles is self-contained slashing evidence — see "Fault tolerance / Equivocation handling" for the analysis. The leader is required to sign σ_V exactly once per slot/layer (refreshes during the fetch window are pre-signing only — see "Head-change handling" in the Application section); any second σ_V from the same leader is a protocol violation regardless of intent.

**Operator commitments — σ, NR, NV.** For each layer, an operator's commitment falls into one of three buckets:

- **σ (sign-on-V)**: the operator received the leader's bundle on time, both protocol-level and application-level checks passed. Materializes as a σ partial in Phase-2a onion or Phase-2b late broadcast (or as the leader's Phase-1 σ for the layer's own leader).
- **NR (non-receipt)**: the operator did not receive the leader's bundle by their Phase-2b cutoff, or received it after `T_candidate_accept` and could not recover via Phase-2a peer onion + witness threshold. Includes "received but BFT auth failed" (silently dropped → equivalent to not-received).
- **NV (non-validity)**: the operator received the bundle on time with valid BFT auth, but the host application returned `not valid` for `V_{L_k}` — so the operator cannot sign σ on it.

**NR and NV are operationally interchangeable for the protocol.** Both materialize on the wire as a single message kind: a partial `σ_i^{IBE}(nr_tag_k)` from the IBE keypair at layers `k ∈ {0, …, K-2}`. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to throughout as "NR-quorum" or "no-σ quorum" for short — both mean the union of NR and NV partials). The distinction between NR and NV is **local-only diagnostic** (an operator may log *why* it didn't sign σ — for telemetry — but the cluster-wide message and counting logic are identical). All references to "NR" in the rest of this document encompass both NR and NV unless stated otherwise.

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

(Bandwidth optimization, the "hash variant": instead of full V plaintext at every layer, operators may carry `hash(V_{L_k})` (32 B) at each layer they signed and full `V_{L_k}` plaintext only at the layer where `i` is the leader. The spec algebra is unchanged. **Caveat — hash variant disables the secondary liveness mechanism**: see "Fault tolerance / Liveness" for the primary/secondary breakdown. Under the hash variant, an honest operator that didn't receive V via Phase 1 cannot recover V from a peer's onion (the peer is carrying only `hash(V)` unless it happens to be the layer's leader, and a byzantine leader will withhold its own onion). Recovery collapses to gossipsub re-flooding of the Phase-1 bundle alone — coverage caps at "1 honest missing re-flood" via the leader-σ head-start, the same as TBFT-shape protocols. At `f = 1` the full-V variant also caps at this bound (witness threshold coincides), so hash and full-V have identical marginal-synchrony coverage there; at `f ≥ 2` the full-V variant's secondary closure extends to "≤ `f` honest missing" while hash stays at the leaner 1-honest bound. Hash variant is therefore safe at `n = 4` whenever bandwidth is the constraint, and is the trade-off cost at `n ≥ 7` when full-V's bandwidth is impractical. See "Practical caveats" for the bandwidth analysis.)

### Phase 2b — Late σ or non-receipt commitment `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`

For each layer `k` where the operator has not yet committed (neither a σ in their Phase-2a onion nor a Phase-1 σ as the layer-`k` leader):

- Broadcast a **late** σ partial on `V_{L_k}` — `C_k(σ_i^V(V_{L_k}))`, same chained IBE wrapping used in Phase-2a onions at layer `k` — only if **all** of the following hold:
  1. The operator extracted `V_{L_k}` from a peer's Phase-2a onion.
  2. The operator validated `V_{L_k}` using `L_k`'s authentication signatures (from any Phase-1 bundle the operator has access to — including bundles first observed after `T_candidate_accept` and retained auth-only per Phase-1 receiver checks) plus application-level rules.
  3. The operator received Phase-2a onions from **at least `f+1` distinct peers** each claiming a σ partial at layer `k` on the same `V_{L_k}` — the **timely-acceptance witness threshold**.

  Late σ at layer `k > 0` is additionally gated by the same chain-of-NR condition as the onion σ: every prior NR-quorum (`nr_tag_0`, ..., `nr_tag_{k-1}`) must aggregate before it can be peeled and counted toward σ-quorum.
- Else (didn't recover V → **NR**, recovered V but application returned not-valid → **NV**, or witness threshold not met → **NR**): broadcast a **no-σ attestation** `σ_i^{IBE}(nr_tag_k)` from the IBE keypair (only for layers `k ∈ {0, …, K-2}`; the last layer has no successor — see "Treatment of missing onions / late broadcasts"). NR and NV are wire-identical and counted together (see "Operator commitments — σ, NR, NV" above).

**Timely-acceptance witness threshold rationale.** The `f+1`-distinct-peers requirement on Phase-2a σ partials guarantees that at least one honest peer signed Phase-2a σ on this `V_{L_k}` at this layer (byzantine controls ≤ `f` operators, so the most they can manufacture alone is `f` Phase-2a σ partials — below the threshold). An honest Phase-2a σ signer accepted the Phase-1 bundle on time (≤ `T_candidate_accept` per Phase-1 receiver checks); their existence is the cluster-level witness that the bundle was timely. Without this gate, a byzantine leader could bypass `T_candidate_accept`'s liveness role: see "Fault tolerance / Liveness / Late-bundle bypass". The witness count is per-distinct-peer (each operator broadcasts at most one Phase-2a onion per slot and contributes at most one σ partial per layer per V), and is enforceable even at layers `k > 0` where the σ partial itself is encrypted — counting *distinct senders' onions claiming σ at layer k on V* binds byzantine to `f` regardless of whether the encrypted partial is yet verifiable.

**Per-operator-per-layer commitment is exclusive across phases.** The σ-vs-no-σ commitment for layer `k` is *one decision per operator per layer, spanning Phase 1, Phase 2a, and Phase 2b*. Concretely:

- The layer-`k` **leader** signed `σ_{L_k}^V` in Phase 1; that is their σ-side commitment for layer `k`. At Phase 2a they include σ on `V_{L_k}` in their own onion uniformly with any other σ-committed operator — Phase 3 dedup collapses Phase-1 σ + Phase-2a onion σ from the same operator to one partial. They **cannot emit NR/NV for layer `k`** — even if their host application's verdict on `V_{L_k}` would have changed by Phase 2b (e.g., due to state drift between fetch and Phase 2b). The σ stays committed; depending on how other honest evaluate `V_{L_k}` and how byzantine behaves, the layer either σ-quorums on `V_{L_k}` (slot succeeds), NR-quorums on `nr_tag_k` (cluster falls through to layer `k+1`), or both pools stay short and the slot misses overall (the leader's σ-side commitment caps non-leader honest NR/NV at `2f < qEnc = 2f+1`, so NR-fall-through requires byzantine cooperation when honest evaluations diverge — see "Fault tolerance / Liveness / Application-validity-divergence").
- A non-leader operator commits to σ or NR/NV in Phase 2a/2b per the rules above — once committed, no switching.

A byzantine operator that publishes both is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For the layer-`k` leader specifically, "cross-signing" includes any Phase-1 σ + Phase-2b NR/NV pair on the same (slot, layer); the rule applies uniformly across phases.

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

**`T_arrival`** for the deadline rule is the cutoff by which the operator must have received any Phase-2a onion or Phase-2b broadcast it intends to count — practically, it's `T_commit + Δ_2a + Δ_2b`. The deadline rules bound *each* Phase-2 sub-window independently against propagation P99/P999 and clock skew — `Δ_2a > D + δ` AND `Δ_2b > D + δ` — see "Practical caveats / Deadline coordination" for why the aggregate-only bound is insufficient.

The leader's Phase-1 σ partial appears unencrypted in the σ pool at every layer. At layer `k > 0` this means one partial is visible early, before the chain is peeled — but one partial alone can't reconstruct (need `qV`), and the remaining onion + late σ partials are wrapped in `C_k` and stay sealed until every prior layer's NR-quorum unlocks the chain.

Once a participant produces an output `(V, S)`, it submits to the downstream system. Multiple operators may submit independently; the downstream system de-duplicates. See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers as a fourth wire-envelope kind: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions / late broadcasts

A participant that hasn't received `j`'s onion / late σ / NR at decryption time treats `j` as not having contributed at all. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f` operators are offline or byzantine combined, no quorum reaches its threshold and the slot is missed.

**Last-layer failure is terminal.** NR attestations are only meaningful when there's a successor layer to unlock — they exist for layers `k ∈ {0, …, K-2}` only. If the last layer (`k = K-1`) fails to reach σ-quorum, the slot misses; there's no `nr_tag_{K-1}` and no fallback. Phase 2b emits NR only on tags `nr_tag_0` through `nr_tag_{K-2}`; Phase 3 halts at `k = K-1` after the σ check, regardless of NR partial availability at that layer.

## Preconditions on the host application

TBFTR is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. The check runs at *both* Phase 2a's σ commit AND Phase 2b's late σ commit (since the same operator may σ-commit in either window — the peer's signature on V does not transfer the validity precondition; each signer validates independently against their current application state). A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV").

The protocol-level checks (cryptographic auth, envelope re-derivation, timing cutoff `T_candidate_accept`, `f+1`-distinct-peers witness threshold for Phase-2b late σ) are protocol-internal and do not depend on the application.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot, but two stricter constraints apply per (slot, layer):

- **Single σ_V per leader.** The layer-`k` leader signs σ_V exactly once per (slot, layer) — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Head-change handling" in the Application section for the SSV-specific operational workflow.
- **σ-vs-no-σ exclusivity per operator across phases.** For any (slot, layer), an operator that has signed σ (in Phase 1 as leader, in Phase 2a as a peer, or in Phase 2b as a late signer) cannot subsequently emit NR/NV for the same layer; an operator that has emitted NR/NV cannot subsequently sign σ. This applies *across phases* — the layer-`k` leader's Phase-1 σ counts as their σ-commitment for that layer, and they cannot emit NR/NV in Phase 2b even if the host application's verdict on `V_{L_k}` would have changed. EKM enforces this cryptographically: an NR/NV-sign attempt at (slot, layer) is rejected if the same EKM has previously signed σ at the same (slot, layer), and vice versa. This is what makes Pigeonhole 1 (see "Fault tolerance / Safety") cap each honest's contribution to one side per layer.
- Every operator signs each layer's `V_{L_k}` it considers valid (host returns `valid`) in its Phase-2a onion (subject to the cross-phase exclusivity above).
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

**Pigeonhole 1 — σ-vs-no-σ at the same layer.** σ-quorum on `V_{L_k}` and NR-quorum on `nr_tag_k` cannot both be reached. (Recall NR-quorum here counts NR + NV uniformly — see "Operator commitments — σ, NR, NV".)

- σ-quorum: `h_σ + byz_σ ≥ qV = 2f+1` (where `h_σ` counts honest σ partials at layer k from any phase — Phase-1 leader σ, Phase-2a onion σ, Phase-2b late σ — uniformly).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Honest sign at most one side per layer (across all phases — see "Slashing-protection scope"): `h_σ + h_NR ≤ 2f+1`. **This includes the layer-`k` leader**: their Phase-1 σ counts as their σ-side commitment, and they are protocol-bound not to subsequently emit NR/NV at the same layer (and EKM-prevented from doing so, even if the host application's verdict on `V_{L_k}` would have changed); so they contribute to either σ or NR for layer `k`, never both.
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

TBFTR has **two layered closure mechanisms** for byzantine-leader selective-delivery grief. Under partial synchrony with `T_candidate_accept` enforced, the *primary* mechanism — gossipsub re-flooding of Phase-1 bundles — handles the common case. The *secondary* mechanism — Phase-2a peer-onion V-plaintext + Phase-2b late σ — covers the byzantine-at-cutoff edge that primary alone doesn't fully close, and extends the synchrony band more broadly at `f ≥ 2`.

**Primary closure (partial synchrony, byzantine releases bundle with re-flood headroom).** Byzantine `L_k` releases `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})` to exactly `f+1` honest operators **at least `D + δ` before `T_candidate_accept`**, withholds from the remaining `f`:

- The `f+1` honest who received V via Phase 1 re-flood the bundle via gossipsub. Within `D + δ`, the bundle reaches the remaining `f` honest before *their* `T_candidate_accept` (with the headroom on the original release time covering worst-case clock skew).
- All `2f+1` honest hold V by Phase 2a start. They include V plaintext + encrypted σ in their onions.
- Cluster-wide σ count on V: `2f+1` honest σ + `1` leader Phase-1 σ = `2f+2 ≥ qV`. **Slot succeeds.**

No Phase-2b late σ is needed in this regime; the recovery channel is dormant.

**Byzantine-at-cutoff edge (full-V handles it; hash variant misses at `f ≥ 2`).** If byzantine releases the bundle to `f+1` honest *exactly at* their `T_candidate_accept` (or in a worst-case-clock-skew window where re-flood arrives at the remaining `f` honest just after their own cutoffs), those `f` peers retain leader auth but cannot sign Phase-2a σ. Outcomes:

- **Full-V variant**: the `f` late-retainers recover V from a Phase-2a peer onion (carried plaintext by the `f+1` honest who signed Phase-2a σ on time) and contribute Phase-2b late σ — the witness threshold (`f+1` distinct Phase-2a σ-signers) is met. σ pool = `f+1` Phase-2a + `f` late + 1 leader = `2f+2 ≥ qV`. **Slot succeeds via secondary closure.**
- **Hash variant** (or any TBFT-shape protocol): no peer-onion V-recovery, so the `f` late-retainers cannot late-sign. σ pool = `f+1` Phase-2a + 1 leader = `f+2`. At `f = 1` this equals `qV = 3` (slot succeeds). At `f ≥ 2`, `f+2 < qV = 2f+1` — slot misses in this byzantine-at-cutoff edge. NR pool = `f` honest non-signers (others are σ-committed) < qEnc when byz withholds NR.

So at `f ≥ 2` the hash variant's byzantine-leader-grief closure has a residual hole at the byzantine-at-cutoff edge — only the full-V variant's secondary closure covers it. This is one of the central reasons full-V is the right deployment at `n ≥ 7` when bandwidth allows; if the cluster runs the hash variant for bandwidth reasons, the cluster accepts the byzantine-at-cutoff edge as a residual miss surface (typically rare in practice, since byzantine has to time the release within the worst-case-clock-skew window of every honest receiver's cutoff). See "Comparison with a leaner (TBFT-shape) protocol" below for the per-`f` widening this implies.

**Secondary closure (marginal synchrony, gated by the witness-threshold precondition).** When **`f+1` honest signed Phase-2a σ on V** but the remaining `≤ f` honest didn't get V via Phase 1 (re-flooding fell short for them), the recovery channel kicks in:

- The `f+1` honest who received V via Phase 1 broadcast Phase-2a onions carrying V plaintext **and** their σ partial on V at the layer. Their Phase-1 bundle (with the leader's authentication signatures `σ_{L_k}^V`, `σ_{L_k}^{op}`, envelope) continues to gossipsub-propagate; honest receivers retain it for the slot, even when first-observation crosses `T_candidate_accept` (see Phase-1 receiver checks).
- The `≤ f` remaining honest extract V from peer onions during Phase 2a. They validate V using the late-retained Phase-1 bundle's leader auth (the onion itself doesn't re-carry it) plus application-level rules. Their Phase-2a σ-witness count for V at this layer = `f+1` distinct peers (the honest Phase-2a signers above), meeting the timely-acceptance witness threshold; they then broadcast late σ in Phase 2b (wrapped in `C_k` — chained IBE for `k > 0`, plaintext for `k = 0`).
- Cluster-wide σ count on V: `f+1` (Phase-2a onions) + `≤ f` (Phase-2b late) + `1` (leader's Phase-1 σ) = `≥ 2f+2 ≥ qV`. **Slot succeeds.**

**Witness-threshold precondition.** The witness threshold `X = f+1` is what makes late σ safe (see "Late-bundle bypass — prevented by the witness threshold" below). A side effect: it bounds the secondary closure's coverage to scenarios where ≥ `f+1` honest actually signed Phase-2a σ on the same V. If fewer than `f+1` honest received V via Phase 1 (e.g., a byzantine leader withholds the bundle from `> f` honest, or marginal synchrony is severe enough that re-flooding leaves `> f` honest without V by `T_candidate_accept`), the witness threshold isn't met and late σ is blocked — late-retained leader auth alone cannot rescue the slot at this layer. The cluster falls through to the next layer's NR-quorum if reachable, or misses overall.

**Comparison with a leaner (TBFT-shape) protocol.** A protocol relying solely on the leader-σ-V head-start (no Phase-2 split, no V plaintext, no late σ) closes the byzantine-leader-grief band where the byzantine leader's forced Phase-1 σ pushes σ-quorum over the line: σ count = `(2f+1 − missing)` honest direct + `1` byzantine leader Phase-1 σ ≥ `qV = 2f+1` iff `missing ≤ 1`. TBFTR's secondary closure pushes coverage out to `missing ≤ f` honest (under the witness-threshold precondition above). The gap by cluster size:

| Cluster | f | Leaner protocol covers | TBFTR secondary closure covers | Net gain at this f |
|---|---|---|---|---|
| n = 4 | 1 | ≤ 1 of 3 honest missing | ≤ 1 of 3 honest missing (witness-threshold-bounded) | **None** — the band coincides with the leaner protocol; secondary closure adds redundancy in the same band but no extension |
| n = 7 | 2 | ≤ 1 of 5 honest missing | ≤ 2 of 5 honest missing | +1 honest — the leaner protocol misses at `missing = 2`, TBFTR closes |
| n = 10 | 3 | ≤ 1 of 7 honest missing | ≤ 3 of 7 honest missing | +2 honest — leaner protocol misses at `missing ∈ {2, 3}`, TBFTR closes |

The widening is `f − 1` at each `f`. **At `f = 1` (n=4) the secondary closure does not extend marginal coverage** under byzantine-leader-grief — the witness threshold's `f+1 = 2` lower bound on Phase-2a σ-signers coincides exactly with the leaner protocol's `2f+1 − missing ≥ 2f` bound at `missing ≤ 1`. At `f ≥ 2` the secondary closure earns its complexity by closing the band the leaner protocol misses. Whether the f=1 case still wants TBFTR is then about secondary-closure *redundancy* (more partials in the same band, useful margin against slow links + late jitter) rather than *coverage extension*; at `n = 4` the trade-off mostly favors the leaner protocol unless that redundancy specifically matters in production.

**Coordinated grief across layers.** If multiple byzantine operators each grief a different layer (one does selective delivery at layer 0, another at layer 1, etc.), the cluster falls through layers via NR-quorums on `nr_tag_0`, …, `nr_tag_{K-2}` until reaching a layer with an honest leader (or the terminal layer `K-1`). With `K = f+1`, byz hold at most `f` of the top-`K` leader slots — so at least one honest leader exists somewhere in the top-`K` and the closure above applies cleanly at *that* layer. Reaching it depends on every intervening layer being able to NR-quorum: if a non-terminal layer deadlocks (e.g., application-validity-divergence under adversarial byz withholding — see "Application-validity-divergence" above), fall-through stops there and the slot misses overall, even if later layers would have closed.

**Late-bundle bypass — prevented by the witness threshold (relevant at `f ≥ 2`).** Without the timely-acceptance witness threshold on Phase-2b late σ (Phase 2b condition 3), a byzantine leader could try to bypass `T_candidate_accept`'s liveness role. The attack construction:

1. Byzantine `L_k` releases the bundle *strictly after* `T_candidate_accept` to `f+1` honest operators. They retain it auth-only per Phase-1 receiver checks but cannot sign Phase-2a σ on it.
2. The `f` byzantine operators selectively deliver Phase-2a onions carrying `V_{L_k}` plaintext to those same `f+1` honest operators (and not to the remaining `f` honest who never received the bundle).
3. Without the witness threshold, the `f+1` honest with late-retained auth + byzantine-delivered V would late-sign in Phase 2b, contributing `f+1` σ partials.
4. Byzantine withholds remaining σ and NR.
5. σ-pool: `f+1` honest late + `1` leader Phase-1 σ = `f+2`.
6. NR-pool: `f` honest (the operators who never received V) `< qEnc = 2f+1`.

At `f = 1`: `f+2 = 3 = qV` exactly — late σ would actually reach σ-quorum, so the construction doesn't yield a miss. The attack only **produces a miss at `f ≥ 2`**, where `f+2 < 2f+1 = qV` and NR-pool is also below quorum (slot misses, NR-quorum can't unlock the next layer either).

The witness threshold is still load-bearing at `f = 1` for **safety against the timing-fragmentation attack** the cutoff exists to prevent more generally (operators must not commit σ on a bundle whose timely acceptance can't be witnessed cluster-wide), and for keeping the late-σ path consistent across `f`. At `f ≥ 2` it's also a liveness-attack mitigation — without it, byzantine can deterministically construct slot misses via the steps above.

**How the threshold blocks the attack (at any `f`).** Byzantine peers can manufacture at most `f` Phase-2a σ contributions (one per byz operator, regardless of whether the encrypted partial is verifiable at layer `k > 0` — the count is per-distinct-sender, byzantine-bounded by their `f` operators). With the `f+1` distinct-peers requirement, the `f+1` honest with late-retained auth see at most `f` Phase-2a σ partials on `V_{L_k}` — below the threshold — and fall through to NR instead of late-signing. NR-pool then reaches `(f+1) + f = 2f+1 ≥ qEnc`; the cluster unlocks layer `k+1` (assuming `k ≤ K-2`; otherwise the slot misses, since the terminal layer has no successor). At `K = f+1` there is at least one honest leader in the top-`K` (byz hold at most `f`); the slot succeeds at that honest leader's layer if it's reached before the terminal layer.

**Application-validity-divergence — known liveness limit.** When honest receivers' application verdicts on `V_{L_k}` diverge — some return `valid` (commit σ), others return `not-valid` (commit NV) — the cluster can deadlock at this layer under adversarial byzantine. The mechanism:

- The honest layer-`k` leader's Phase-1 σ commits them to σ-side. Per cross-phase exclusivity, the leader cannot emit NR/NV at layer `k`.
- Non-leader honest who returned `not-valid` emit NV (≤ `2f` total — the bound, since the leader is excluded from this side).
- Adversarial byzantine withholds NR/NV (and σ).
- σ-pool: `1` (leader) + `m` (honest who returned valid) + `0` byz = `m + 1 < qV = 2f+1` whenever `m < 2f` (i.e., whenever any honest returned `not-valid`).
- NR-pool: at most `2f` honest NV + `0` byz = `2f < qEnc = 2f+1`.
- Neither quorum reaches; the layer can't fall through (NR-quorum at layer `k` is needed to unlock layer `k+1`'s chained encryption); the slot misses overall. **Safety holds; liveness is lost for this slot.**

This is a known property of the `qEnc = qV = 2f+1` threshold + cross-phase exclusivity rule. The cluster's no-σ pool is capped at `2f` honest contributors when the leader is σ-committed, which is one short of `qEnc` without byzantine cooperation. The trade is: cryptographic safety against an offline-aggregating adversary (Pigeonhole 1 with `qEnc = qV`) in exchange for a deadlock window when honest application verdicts diverge.

For SSV's proposer duty, divergence on a single `V_{L_k}` typically arises from **post-signing application-state changes** — e.g., the leader fetched `V` against beacon-head `H1` at signing time, the head moves to `H2` between Phase 1 and Phase 2b, and some honest receivers' application validation now returns `not-valid` (parent root mismatch). The protocol cannot resolve this; the host is responsible for managing application-validity stability across the consensus window. See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational guidance.

### Failure modes

The slot misses (no V signature is produced) under any of the following:

- **Bad synchrony (beyond aggressive marginal)**: degradation severe enough that even Phase-2a onion delivery doesn't reach all honest within `Δ_2a` — neither re-flooding nor peer-onion-recovery completes. Some honest emit NR in 2b without recovering V; σ-quorum doesn't form, NR-quorum may or may not depending on distribution. Slot misses; **no safety violation** (the algebra above doesn't depend on synchrony).
- **More than `f` faults**: if more than `f` operators are offline or byzantine combined (beyond the byzantine bound), no quorum reaches its threshold at any layer.
- **Last-layer failure**: if layer `K-1` doesn't reach σ-quorum, there's no successor to fall through to. NR is only emitted on tags `nr_tag_0` through `nr_tag_{K-2}`; last-layer failure is terminal.
- **Application-validity-divergence on any non-final layer (under adversarial byzantine)**: see "Liveness / Application-validity-divergence" above. Because the chained encryption requires NR-quorum at layer `k` to unlock layer `k+1`, divergence on **any** non-terminal layer (any `k ∈ {0, …, K-2}`) under adversarial byzantine withholding can deadlock that layer's NR-quorum (which caps at `2f` non-leader honest, below `qEnc = 2f+1`), blocking fall-through to all subsequent layers — even when later layers would have been fine. The slot then misses overall. (Divergence on the terminal layer `K-1` alone is not relevant for fall-through — there's nothing to fall through to anyway — but it does prevent that layer from σ-quorumming if other layers had already failed.)

### Equivocation handling

If a participant observes two distinct `σ_V` partials from the same `L_k` at the same slot/layer, that's leader equivocation:

1. Locally treat layer `k` as non-receipt: don't include a positive partial signature for layer `k` in the onion (Phase 2a); broadcast the matching no-σ attestation in Phase 2b instead (only for `k ≤ K-2`).
2. The pair of signed bundles is a self-contained slashable fault proof against `L_k`.

The leader is required to sign σ_V *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance) and don't surface multiple σ_V partials on the wire. Any second σ_V from the same leader is a protocol violation.

The equivocation rule is what makes Pigeonhole 2 above tight in practice: honest operators who observe the equivocation evidence avoid signing either V at that layer, capping `h_σ_V + h_σ_V'` strictly below `2f+1`. Without the rule, honest could split their σ across the two values; with it, they emit NR/NV instead and the equivocation evidence is gossipped for slashing.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed in two distinct Phase-2a onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation (see "Slashing evidence"). For aggregation: `i` contributes **at most 1 σ-claim per distinct V** to that V's witness-threshold count regardless of how many onions they produced — counting is per-distinct-sender per-V, not per-onion, so two onions from `i` claiming σ on V don't double-count. The byzantine f-bound on per-V witness-threshold contributions therefore holds without explicit suppression: f byz × 1 per V = f, and the `f+1` threshold still requires ≥1 honest. Honest receivers MAY additionally elect to fully suppress `i`'s partials from σ-, NR-, and witness-threshold aggregation upon observing the equivocation evidence — this is symmetric with the optional cross-signer filter described under "Cross-signing detection" and is similarly not load-bearing for safety.

**Late-retainer interaction with leader equivocation.** A late-retainer (operator that received the leader's bundle auth-only after `T_candidate_accept`) who only ever observes one of two equivocating bundles (V, but not V') cannot detect equivocation locally, by definition. Their late-σ on the V they recovered follows the regular Phase-2b path — witness-threshold check, then late-σ broadcast wrapped in `C_k`. Pigeonhole 2 still bounds cluster outcomes regardless of who detected what, so safety holds. At liveness level, the late-retainer's σ commitment is locked once broadcast (per cross-phase exclusivity); if the cluster subsequently fails to converge on either V or V' at this layer (Pigeonhole 2 prevents both σ-quorums; locked σ commitments may also reduce NR contributors below `qEnc`), the slot can deadlock at this layer. Out-of-protocol mitigation: honest operators SHOULD defer their late-σ broadcast to as late as practical within Δ_2b (consistent with the per-window deadline rule — broadcasts must still arrive at all honest by `T_arrival = T_commit + Δ_2a + Δ_2b`) to maximize observation time for any late-arriving second σ_V partial that would surface equivocation. This is a host-policy knob, not a protocol-level requirement; the protocol's safety doesn't depend on it.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at layer `k` AND an NR/NV attestation on `nr_tag_k` is a slashable cross-signer. The σ source is uniform across phases — a Phase-1 leader σ, a Phase-2a onion σ, and a Phase-2b late σ all count equally as the operator's σ-commitment. The most subtle case is the layer-`k` leader: their Phase-1 σ already commits them to σ-side; emitting NR/NV in Phase 2b after the host application's verdict on `V_{L_k}` would have changed is the same kind of cross-signing as a non-leader signing both σ and NR/NV, and is detectable from the same dual-partial evidence.

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it, with the cross-phase commitment captured in the `h_σ + h_NR ≤ 2f+1` bound). The detection is purely for **attribution** and out-of-band punishment. Honest aggregation may filter cross-signers, but doing so is not load-bearing for safety.

**Path-conditional detection limit at deep layers.** At `K ≥ 3`, σ partials at deep layers are encrypted; if an upper layer succeeds, the deep layer doesn't open and σ+NR cross-signing at that depth goes undetected for *attribution*. Doesn't affect safety (the algebra is over published cluster-wide messages and holds whether or not honest aggregate at that depth). Accepted as a path-conditional limit; deep-layer cross-signers may escape attribution but cannot break safety.

### Slashing evidence

Three rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to make byzantine misbehavior accountable.

- **Self-contradiction (σ + NR/NV).** If operator `i`'s onion contains `σ_i^V(V_{L_k})` *and* `i` broadcasts `σ_i^{IBE}(nr_tag_k)`, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. The leader is required to sign `σ_V` exactly once per (slot, layer); refreshes in the host's fetch loop happen pre-signing and don't surface multiple `σ_V` partials on the wire (see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance). Any observable double-signing is therefore protocol-violating regardless of the leader's stated intent.
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
| Byzantine-leader-grief resistance | **With re-flood headroom** (byz releases bundle ≥ `D + δ` before `T_candidate_accept`): closed under partial synchrony via gossipsub re-flooding (primary), in both full-V and hash variants. **Byzantine-leader-at-cutoff edge** (byz releases at the cutoff so re-flood lands past worst-case-skew peers): closed by full-V's secondary closure at `f ≥ 2`; hash variant has a residual partial-synchrony miss surface unless `T_candidate_accept` is moved earlier. Marginal-synchrony band closed via Phase-2 composition (secondary, full-V only) up to "≤ `f` honest miss re-flood" — gated by the `f+1`-distinct-Phase-2a-σ-signers witness threshold; widening over the leaner protocol's "≤ 1 honest" bound is `f − 1` (zero at `f = 1`, +1 at `f = 2`, etc.) |
| Built-in leader fallback | Yes (K layers) |
| Round-change recovery | Limited to K round-changes (leader fallback) |

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
| `Δ_2a` | onion broadcast window (~250ms; must satisfy `Δ_2a > D + δ`) |
| `Δ_2b` | late-σ window (~250ms; must satisfy `Δ_2b > D + δ`) |

Phase timeline (assuming `D + δ ≈ 200 ms`; same shape across `n`):

- Phase 1 fetch: `slot_start + 1.7s` to `slot_start + 2.7s` (top-K leaders fetch and broadcast bundles).
- **Effective Phase 1 acceptance ends at `T_candidate_accept ≈ slot_start + 2.5s`**: candidates first observed by an honest receiver after this point are dropped. The primary-leader's late-fetch window is therefore effectively `T_0` to `T_candidate_accept`, ~200 ms shorter than the nominal `T_0` to `T_commit`. This is the cost of cryptographic-safe re-flooding.
- Phase 2a: `slot_start + 2.7s` to `slot_start + 2.95s` (onions with V plaintext; ~250ms window > D+δ).
- Phase 2b: `slot_start + 2.95s` to `slot_start + 3.2s` (late σ or NR; ~250ms window > D+δ).
- Phase 3: `slot_start + 3.2s` onwards (reconstruct + submit + certificate gossip, ~800ms headroom against relay 4s cutoff).

Each Phase-2 sub-window is sized strictly above `D + δ` per the per-window deadline rule (see "Practical caveats / Deadline coordination"). The compressed timeline at larger cluster sizes (n=10, 13) is tighter against the relay cutoff; production telemetry should validate the budget — and in particular, telemetry should track `D + δ` so `T_candidate_accept` and the per-Phase-2 windows can be set just-tight-enough to the propagation envelope without over-shrinking the late-fetch window or under-sizing either Phase-2 sub-window.

### Timing budget

The end-to-end budget for a proposer slot must fit inside the relay submission cutoff (~4s after `slot_start`). The structure:

```
slot_start
  + pre-consensus            (RANDAO partial-sig collection, ~T_pre)
  + block fetch              (Δ_1; worst-of-K parallel fetches — see below)
  + Phase 2a broadcast       (Δ_2a ≈ 250 ms; must satisfy Δ_2a > D + δ)
  + Phase 2b broadcast       (Δ_2b ≈ 250 ms; must satisfy Δ_2b > D + δ)
  + Phase 3 reconstruct      (BLS aggregate, ~few ms)
  + downstream submission    (relay round-trip, ~T_submit)
≤ slot_start + 4s            (relay cutoff)
```

**Worst-of-K beacon-fetch latency.** The top-K leaders fetch in parallel from K distinct beacons. `Δ_1` must accommodate the *slowest* of K independent block-fetch RTTs, not the typical one. Tail-percentile estimation: if a single beacon's fetch is at P99 = `t`, the worst-of-K is approximately at the `(1 − (1−P99)^K)`-percentile of the underlying distribution. For K=3 (n=7) at single-fetch P99 = 800 ms, worst-of-3 P99 ≈ 950–1000 ms; for K=5 (n=13), worst-of-5 ≈ 1.1–1.2 s. Δ_1 must be sized accordingly.

Concrete numbers for each leg should come from production telemetry. Until that lands, the Phase-timeline above is a placeholder default; tighten per cluster size as data arrives.

The deadline-tuning rules from caveat 5 below apply: each Phase-2 sub-window must independently exceed the propagation budget plus clock skew — `Δ_2a > D + δ` AND `Δ_2b > D + δ` (the aggregate-only bound `Δ_2a + Δ_2b > D + δ` is not sufficient — see caveat 5). `D` is the propagation P99/P999 and `δ` is the bounded clock-skew across operators.

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_{L_k}`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during the Phase-1 fetch window (between `T_commit − Δ_1` and `T_commit`), candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_{L_k}` is locked on the originally-signed value. **The leader of layer `k` also cannot subsequently emit NR/NV on `nr_tag_k`** — the Phase-1 σ is their σ-side commitment per the cross-phase exclusivity rule (see Phase 2b's "Per-operator-per-layer commitment is exclusive across phases").

**Receiver-side application-validity divergence.** Honest non-leader operators run the host's validity check on received candidates at acceptance time and may run it again at Phase-2b late-σ time. If a head change between Phase 1 and Phase 2b causes the verdict to flip from `valid` to `not-valid` for some honest receivers, those operators commit NV in Phase 2b (cross-phase exclusivity prevents a switch back to σ once committed). This is the divergence scenario analyzed in "Fault tolerance / Liveness / Application-validity-divergence" — under adversarial byzantine withholding it can deadlock the layer (the slot misses overall).

**Operational implications for the host:**

- Re-orgs at slot boundaries are rare events; the deadlock window is bounded by re-org rate. Single-shot consensus means no in-protocol retry — recovery is at the next slot.
- The host controls the receiver-side validity behavior. A stricter receiver-side check (re-validate `parent_root` against the current head at every acceptance and Phase-2b commit) maximizes correctness against re-orgs but exposes the cluster to the post-signing-divergence deadlock. A looser check (validate once at acceptance, then commit regardless of subsequent head movements) avoids the deadlock at the cost of potentially committing on a value whose parent later becomes orphaned (beacon-chain submission rejection then causes the slot miss instead). The right choice is operational and depends on observed re-org rates and the host's tolerance for each failure mode. The TBFTR protocol works correctly in either case.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" on the leader side — a second signing attempt at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing detectable cluster-wide.

## Practical caveats

1. **Bandwidth: V plaintext vs hash variant.** Carrying full `V` plaintext at every onion layer scales as `K · |V| · n` per cluster, which exceeds 200 KB at n=10 and 400 KB at n=13. The hash variant (full V at the leader's own layer, 32-B hashes elsewhere) cuts onion growth from `K · |V|` to `K · 32B + |V|`. **Trade-off**: hash variant disables Phase-2a peer-onion V-recovery (see "Phase 2a" caveat and "Fault tolerance / Liveness" → secondary mechanism), so the cluster's marginal-synchrony coverage reduces to "1 honest missing re-flood" via the leader-σ head-start (same as TBFT-shape protocols) regardless of `f`. At `f = 1` this is the same band the full-V variant covers (witness threshold coincides), so hash is a free win at `n = 4`. At `f ≥ 2` the full-V variant extends to "≤ `f` honest missing"; hash variant gives this up — pick it only when bandwidth is the binding constraint and the cluster can tolerate the narrower band. Hash-only domain separation: hash by `(slot, cluster, layer, leader)` so hashes can't be replayed across slots/layers.

2. **Phase 2b latency.** The composition adds `Δ_2b` (sized strictly above `D + δ`; ~250 ms for the worked SSV example with `D + δ ≈ 200 ms`, larger in production envelopes with worse measured propagation) over plain TBFT-style timing. Tight against the 4s relay cutoff at n=10/13; the timing budget needs to be tracked against production gossip-propagation P99/P999.

3. **Chained-encryption cost.** σ partials at layer `k > 0` are wrapped in `k` nested IBE encryptions (one per prior `nr_tag`). Per onion, the deepest layer carries `K-1` nested wrappers; total encryption ops summed across all layers per onion is `K(K-1)/2` (so 1 op at K=2, 3 ops at K=3, 6 ops at K=4, 10 ops at K=5). Each IBE wrapper adds a small constant ciphertext expansion (typically a few hundred bytes per wrapper for `drand/tlock`-style constructions) — at n=13 (K=5), ~500 B per partial × n operators ≈ ~6.5 KB cluster-wide additional vs a hypothetical single-tag scheme. Decryption is symmetric: peel `k` wrappers at layer `k`, using the cumulatively-aggregated NR keys (outermost first). Per-op latency is microseconds; total chain peeling is a few hundred microseconds at K=5 — negligible against the protocol's per-slot timing budget. The chained encryption is what closes the cross-layer safety attack at K ≥ 3 (see "Fault tolerance / Safety / Pigeonhole 3"); a single-tag scheme would require honest-only enforcement to be safe and is not recommended.

4. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

5. **Deadline coordination.** Clock skew across operators must be bounded by `δ`. The cutoffs derived from `D` (propagation P99/P999) and `δ` together drive the partial-synchrony assumption:

   - **`T_candidate_accept = T_commit − (D + δ)`** for Phase-1 candidates. Receivers reject candidates whose first-observation time is later. (Caveat: under worst-case clock skew, a re-flood from an honest acceptor at exactly this cutoff may not reach all other honest before *their* cutoffs — see "Phase 1 / Bundle propagation". The full-V variant's secondary closure handles this edge; the hash variant accepts a residual miss surface at `f ≥ 2`.)
   - **`Δ_2a > D + δ`** for Phase-2a onions: every honest's Phase-2a onion broadcast at the start of the 2a window must arrive at every other honest by `T_commit + Δ_2a` (the start of Phase 2b), so honest can decide late σ vs NR/NV in 2b based on a complete view of who σ-committed in 2a. A weaker bound (e.g., aggregate `Δ_2a + Δ_2b > D + δ` only) is **not** sufficient: it permits the witness-threshold visibility to slip past the 2b decision point.
   - **`Δ_2b > D + δ`** for Phase-2b late-σ / NR-NV broadcasts: every honest's Phase-2b broadcast at the start of the 2b window must arrive at every other honest by `T_arrival = T_commit + Δ_2a + Δ_2b` (the start of Phase 3 reconstruction), so the σ-pool / NR-quorum aggregation in Phase 3 has a complete view.

   The aggregate-only bound `T_arrival − T_commit > D + δ` is too weak — it bounds total propagation but not per-window. Per-window bounds are what actually drive liveness: each Phase-2 sub-phase's broadcasts must complete within that sub-phase's own window.

   All bounds above are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

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

Both additions form the *secondary* closure mechanism described in "Fault tolerance / Liveness". Under partial synchrony with re-flood headroom, the *primary* closure (gossipsub re-flooding under `T_candidate_accept`) handles the byzantine-leader grief at any `f` without help from these additions. The byzantine-leader-at-cutoff edge (byz times the release at the cutoff so re-flood lands inside worst-case skew) is what the secondary additions buy at `f ≥ 2` — the hash variant has a residual partial-synchrony miss surface there unless the cutoff is moved earlier. Beyond that, the TBFTR additions push the marginal coverage limit from "1 honest missing re-flood" (what the leader-σ-V head-start alone covers) to "up to `f` honest missing re-flood" — gated by the `f+1`-distinct-Phase-2a-σ-signers witness threshold. The widening between the two bounds is `f − 1`, so it's **zero at `f = 1` (n=4)** — the witness threshold caps secondary closure at the same bound the leaner protocol already covers, and the additions add only redundancy in the same band — and grows as `f − 1` for `f ≥ 2`. At `n = 4`, [TBFT](TBFT.md) is the leaner alternative that drops the additions for a smaller protocol footprint at no marginal-coverage cost.

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the two protocols TBFTR shares deployment context with: [TBFT](TBFT.md) (the leaner specialization for `n = 4`) and QBFT (SSV's existing consensus protocol, applicable at any cluster size). For detailed scenario-by-scenario comparison with bandwidth and latency numbers across failure modes, see [TBFT-comparison.md](TBFT-comparison.md).

### A.1 — Comparison with TBFT (at n=4)

Both protocols share the same cryptographic core (`qEnc = qV = 2f+1`, leader-authenticated candidates with both V-keypair and operator-identity sigs over a structured envelope, equivocation-to-NR rule, IBE primitive, two DKGs at the same threshold). The differences are in onion structure, Phase-2 timing, and the resulting fault-tolerance band — comparing both at the same `n = 4, K = 2` cluster size to make the trade-off concrete:

| Aspect | [TBFT](TBFT.md) (n=4, K=2) | TBFTR (n=4, K=2) |
|---|---|---|
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Equivocation-to-NR rule | Yes | Yes |
| qV / qEnc | `qV = qEnc = 2f+1 = 3` | Same |
| Onion at layer k | `E_{nr_tag_0}(σ_i^V(V_{L_k}))` (encrypted partial only; at K=2 the chained wrapper has only one tag) | `V_{L_k} ‖ C_k(σ_i^V(V_{L_k}))` (V plaintext + chained-IBE-wrapped partial; `C_k` reduces to single-tag encryption at K=2 and to full chain at K ≥ 3) |
| Phase 2 timing | Single window `[T_commit, T_commit + Δ_2]` (onion + NR together) | Split: 2a (onion only) + 2b (late σ or NR) |
| Late σ broadcasts | None | Phase 2b — operators who recovered V via peer onions sign σ then |
| Byzantine-leader-grief closure | Primary (gossipsub re-flooding under partial synchrony) + moderate marginal (1-of-3-honest-missing-reflood, via leader-σ head-start) | Same primary + secondary-closure redundancy in the **same band** at K=2 / n=4 (witness threshold caps at `f+1 = 2` Phase-2a σ-signers, coinciding with the leaner protocol's coverage). At `f ≥ 2` the secondary closure extends coverage to `f`-honest-missing-reflood — non-zero widening only there. |
| Bandwidth (worst case) | ~21 KB | larger by V-plaintext + Phase-2b overhead (slot-dependent) |
| Latency overhead | None beyond standard Phase 2 | +Δ_2b (~250 ms for `D + δ ≈ 200 ms`; sized strictly above `D + δ`) |

At `n ≥ 7`, only TBFTR is supported — the secondary closure becomes load-bearing because the leaner TBFT-shape protocol covers only "1 honest missing re-flood" regardless of `f`, while at `f ≥ 2` the practical marginal band reaches "2-or-more honest missing"; the leaner protocol's single-window σ count caps at `f+2 < qV = 2f+1` once `f ≥ 2`, missing slots in that gap.

**If you're choosing between protocols at `n = 4`**: pick TBFT for minimal protocol complexity. TBFTR-at-n=4 covers the **same** marginal-synchrony band (≤ 1 of 3 honest missing re-flood — the witness threshold caps secondary closure at the same bound TBFT already covers via the leader-σ head-start); the only thing TBFTR adds at this size is redundancy within the band (extra σ partials), which rarely earns its bandwidth/latency premium. Either is cryptographically safe.

### A.2 — Comparison with QBFT (any n)

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; TBFTR fuses the two by embedding partial signatures inside Phase-2 onions and Phase-2b late-σ broadcasts. Most observable trade-offs trace back to this structural difference. (Scenario-level numbers across cluster sizes live in [TBFT-comparison.md](TBFT-comparison.md); this section is conceptual.)

| Aspect | QBFT | TBFTR |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round change on timeout | Single-shot, K=f+1 layered leader fallback + Phase-2a/2b composition; no rounds |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2a onions and Phase-2b late-σ broadcasts carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 3 (Phase 1 + Phase 2a + Phase 2b) |
| Round-change recovery | Yes — view-change protocol on timeout; ~2s per round at SSV's tuning | None — single-shot; slot misses on bad synchrony |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | No — single-shot, partial-synchrony-conditional within `T_commit + Δ_2a + Δ_2b` |
| Safety | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Cryptographic via `qEnc = qV = 2f+1` + chained encryption (Pigeonhole 1, 2, 3) — unconditional, holds against arbitrary network adversary regardless of honest aggregation rules |
| Byzantine-leader-grief resistance | Round-change recovers (slow — ~2s per round timeout) | Primary closure (gossipsub re-flooding of Phase-1 bundle under `T_candidate_accept`) under partial synchrony, plus secondary closure (Phase-2a peer-onion V-recovery + Phase-2b late σ, full-V variant) extending marginal coverage at `f ≥ 2` to "≤ `f` honest missing re-flood" |
| Equivocation handling | Detected as conflicting prepare/commit votes; round change | Cluster-wide non-receipt via equivocation-to-NR rule; pair of bundles is self-contained slashable evidence |
| Bandwidth (1 round, by cluster size) | ~14 / ~37 / ~50 / ~85 KB (n=4 / 7 / 10 / 13) | ~21 / ~108 / ~253 / ~497 KB (hash variant); ~30 / ~325 KB / ~1 MB / ~2.5 MB (full-V variant) |
| Bandwidth (round change) | +12 KB per round + a full additional round on top | n/a (no rounds) |
| Latency (healthy) | ~750 ms across cluster sizes | ~500 ms across cluster sizes (Phase 1 + Phase 2a + Phase 2b windows) |
| Latency (1 round failure) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | ~500 ms (built-in K-layer fall-through completes locally in Phase 3) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2a onion, Phase-2b late σ) gated by EKM cross-keypair coordination — see "Preconditions on the host application / Slashing-protection scope" |
| Cluster-size scaling | Same shape at any `n`; `O(n²)` bandwidth per round | `O(K · n²)` (full-V) or `O(K · n)` (hash); `K = f+1` |
| Hash vs full-V variants | n/a | Hash variant disables secondary closure — applicable when bandwidth is the binding constraint, accepts narrower marginal-synchrony band; full-V is recommended when bandwidth budget allows |

**The QBFT vs TBFTR trade is structural.** QBFT is failure-recovery-oriented: it converges eventually across rounds at the cost of expensive failure recovery (~2s per round change, plus per-round bandwidth). TBFTR is single-shot with built-in redundancy (one-shots through all K leaders/rounds locally): flat ~500ms behavior across all in-bound failure modes, accepting slot-miss on out-of-bound failures. For SSV's proposer duty with a hard 4s relay cutoff, TBFTR's flat-failure-mode profile is what makes it competitive at `n ≥ 7`: QBFT's `~3.0s × per-round-failure` cost compounds rapidly at larger `f` where worst-case-leader-position scenarios require multiple round changes.

A practical note on the QBFT round budget for proposer duty: with the current 2s round timeout (cf. [protocol/v2/qbft/roundtimer/timer.go](../protocol/v2/qbft/roundtimer/timer.go)) and the 4s relay cutoff, **QBFT has room for at most 2 rounds within the proposer-duty timing budget** — round 1 timeout consumes 2s, leaving the rest tight against round 2 + post-consensus + relay round-trip. Failure modes requiring 3+ QBFT rounds (e.g., `f` byzantine in worst-case leader positions at `n ≥ 7`, where `f` round changes would be needed to reach an honest leader) miss the relay cutoff regardless of consensus correctness. TBFTR's K=f+1 layered fallback completes in the same ~500ms regardless of how many of the top-K leaders are byzantine, because fall-through is a Phase-3 local operation rather than a network round.

## Appendix B — Dynamic leader-ordering extensions

This appendix sketches two related extensions where the choice of which leader's value the cluster commits to is **not fixed by priority** but **emerges from a deterministic rule** over the candidates the cluster actually had time to validate. Neither is part of the baseline TBFTR spec; both are forward-compatible directions documented here because they fall naturally out of the same 2a/2b machinery and may be relevant if production data shows application-validity-divergence or per-slot leader-quality variance hurting liveness.

Under TBFTR's application-agnostic framing (see "Preconditions on the host application"), these extensions are best read as **examples of plugging an application-supplied ordering criterion into TBFTR's leader-ordering slot**. The mechanism (per-layer commit-tags + IBE-encrypted σ partials + per-operator commit exclusivity) is protocol-level and shared between the two variants; the criterion (B.1: bid; B.2: any deterministic rule, including parent-root) is host-supplied.

- **B.1** notes how the **bid-ordered selection** sketched for TBFT in [TBFT.md](TBFT.md) Appendix B extends to K > 2 — application-driven deterministic rule, cluster-consistent inputs, attribution-friendly.
- **B.2** is the **general dynamic-leader-ordering** sketch — protocol-shape independent of any specific deterministic rule — and includes a note on why parent-root-based "ordering" fits naturally into it but doesn't actually buy much.

**Status: design sketches, not specified for implementation.** The safety argument carries through under the same cryptographic primitives, but several details (exact commitment-rule semantics, late-commit handling, interaction with equivocation, slashing-protection scope) need precise specification before either could be deployed.

### B.1 — Extending TBFT's bid-ordered selection to K > 2

The bid-ordered variant for TBFT ([TBFT.md](TBFT.md) Appendix B) generalizes naturally to TBFTR's K-layer setting without changing the cryptographic core or the Phase-2 split. The full mechanism is described over there — what's worth noting here is how it fits within TBFTR specifically:

- **Phase 1 envelope** picks up an additional `bid` field (application-supplied), signed alongside the protocol-level `value_root`. Same equivocation rules; same `T_candidate_accept` cutoff.
- **Phase 2a onion** carries V plaintext at every locally-validated layer (TBFTR core, unchanged) plus *one* encrypted σ partial at the layer the operator commits to, where the commit is `argmax_k bid_k` over locally-validated `V_{L_k}`. Tiebreaker on equal bids: lower `leader_id`. The σ partial is encrypted under the chosen layer's `commit_tag_k` (K total tags, one per layer).
- **Phase 2b** carries late commits + encrypted σ for operators who recovered a candidate during 2a and didn't commit yet. No NR side.
- **Phase 3** walks "find the layer with commit-quorum" across all K layers, same shape as the general variant in B.2 below.

Bandwidth at K > 2 is slightly better than the baseline TBFTR onion (one encrypted σ + one commit partial per operator instead of K encrypted σ + per-layer NR), but the per-onion savings are small and the K commit-tags add some constant overhead. Latency is unchanged (same 2a/2b windows).

The wins are the same as at K=2 — cluster routes to the highest-bid valid layer directly, no NR-walk steps when `L_0` is unavailable / lying high — just generalized over more layers. The attribution story (post-hoc relay-bid verification) carries over unchanged: each leader's signed envelope plus the relay-reported actual bid for the published block forms self-contained liveness-fault evidence.

For SSV's proposer-duty case, the natural prototype path is **TBFT first, TBFTR second** — the K=2 design space is much smaller, the safety/liveness analysis is cleaner, and any production lessons translate directly. Deploying at K > 2 only makes sense after the K=2 variant has been validated.

### B.2 — General dynamic leader-ordering

#### Motivation

Baseline TBFTR walks layers in **priority order**: layer 0 is tried first; only if `nr_tag_0` reaches `qEnc` does layer 1 become reachable. The fixed priority is what enables the `qEnc = qV` safety pigeonhole — each honest operator commits to either σ or NR per layer, mutually exclusively, so at most one σ-quorum can materialize across the entire layer walk.

The cost of fixed priority: if `L_0`'s candidate happens to be the worst choice for *this* slot (e.g., its application-level validity differs across honest operators' local states — see "Fault tolerance / Liveness / Application-validity-divergence"), the cluster has to "burn" an NR-quorum on layer 0 before getting to a more convergent layer. That uses up the same `2f+1` honest-agreement budget that signing layer `k` directly would have used — so it's not strictly worse, but it's strictly slower (extra IBE decryption walk step) and depends on operators being able to converge on NR for the layer they're skipping.

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

- **Direct backup commit on application-validity-divergence.** If `2f+1` honest validate `V_{L_1}` (say, against a deeper-confirmed parent) but the application-validity split on `V_{L_0}` prevents agreement at layer 0, dynamic ordering converges directly on layer 1. Baseline TBFTR would need `qEnc = 2f+1` honest to NR layer 0 first — same threshold, but the protocol burns an extra IBE decryption walk step getting there.
- **Per-slot leader-quality variance.** If a particular slot has `L_0` producing a marginal candidate (e.g. low MEV, non-canonical relay) but `L_1` producing an objectively better one, operators following the deterministic rule can converge on `L_1` directly without the round-trip through `nr_tag_0`. (Baseline TBFTR is locked into the priority order regardless of candidate quality.)

**Doesn't win:**

- **Doesn't reduce the agreement threshold.** Both variants need `2f+1` honest to converge on the same layer. Dynamic ordering changes *which* layer they converge on, not whether they need to converge.
- **Strictly worse when honest disagree on the rule's output — no fall-through path.** This is the variant's **biggest structural regression** versus baseline. The deterministic rule has to be over inputs that are cluster-consistent enough — "lowest-indexed valid layer" works under partial synchrony if all honest validate the same V's; any input divergence (application-validity split, different deterministic-rule outcomes from differing local state) splits honest commitments across layers without the safety net baseline TBFTR provides. **Baseline has an NR side at every non-terminal layer**: when honest disagree on V at layer k, the cluster can still NR-quorum that layer (assuming honest converge on "this V isn't agreeable") and walk through the chained encryption to layer k+1. **Dynamic ordering has no NR side**: per-operator commits are layer-exclusive, so a fragmented commitment pattern (some honest committing to layer 0, others to layer 1, etc.) leaves *no* layer with commit-quorum and *no* mechanism to fall through. Slot misses immediately. The same input-divergence event that baseline would burn one walk step on costs the dynamic variant the entire slot.
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
| Liveness when honest agree | Same agreement threshold; baseline burns an NR-walk step before reaching the agreed layer | Same threshold; converges directly on agreed layer |
| Liveness when honest *disagree* on rule's output | Falls through via NR-quorum on each disagreed-upon layer (assuming honest converge on "not signing this V") to a layer where they can agree | **No fall-through** — fragmented commits across layers leave no layer at commit-quorum and no NR side to walk through; slot misses |
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
- **Parent-root-based ("commit to the layer whose parent_root matches my canonical chain; tiebreak by layer-index").** Fits the protocol shape but doesn't actually buy much: parent-root match is a *validity filter*, not a ranking comparator, so the "rule" collapses to "fixed priority among locally-valid layers" — essentially baseline TBFTR's behavior, just routed through commit-tags instead of NR. Worse, the rule's input (parent-root match against local head) isn't cluster-consistent — different operators' beacon-node views of the canonical chain can diverge during a re-org, and the rule's *output* fragments along the same line. Adding parent-root as a routing primitive doesn't fix application-validity-divergence-driven misses; it just relocates them. The cleaner mitigation is application-level (deeper-confirmed parent for non-priority layers), see [TBFT.md](TBFT.md) Appendix B.2 for the corresponding analysis at K=2.

The bid-based rule is the cleaner of the two — and is the one we'd recommend if any dynamic-ordering scheme is pursued.

#### When to consider this

This variant is most relevant if production data shows the baseline TBFTR's fixed priority order causing measurable misses or wasted slots — e.g., re-org-frequent periods where layer 0 routinely fails its NR-quorum step before layer 1 picks up. Without that data, the baseline's simplicity is the right default.

**Caveat — the no-fall-through regression matters for the rule choice.** Because the dynamic variant has no NR fall-through, the deterministic rule must be one whose inputs are cluster-consistent enough that honest converge on the same layer with very high probability. Bid-based (B.1) clears that bar — bids are byte-identical envelope fields, so honest see the same input. Parent-root-based does not — local-head views can fragment under re-org, and a fragmented rule output costs the entire slot in this variant (whereas baseline's NR walk would absorb it). The regression is what makes B.1's bid rule the *only* well-behaved candidate; "any deterministic rule" is too generous a framing.

If pursued, the natural place to prototype is **TBFT first** (K=2, see B.1 → [TBFT.md](TBFT.md) Appendix B for the concrete instantiation), then TBFTR. The K=2 design space is much smaller, the safety/liveness analysis is cleaner, and any production lessons translate directly to the K-layer generalization here.

For SSV's n=4 case specifically, the lighter mitigation — fetching `V_{L_1}` from a deeper-confirmed parent so the backup is structurally re-org-resistant — gets most of the benefit without any protocol change. That's the recommended first move; the dynamic-ordering variant is a second-line option for cluster sizes and conditions where the application-level mitigation isn't enough.

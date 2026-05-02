# TBFTR — Threshold BFT (Roach) for n ≥ 7 clusters

This document describes **TBFTR** ("TBFT Roach") for `n ∈ {7, 10, 13}` clusters: a single-shot agreement protocol that produces one collective threshold-signed value per "slot" against a hard deadline, with full byzantine-leader selective-delivery resilience at all supported cluster sizes.

TBFTR is a natural extension of [TBFT](TBFT.md) — same cryptographic safety story (`qEnc = qV = 2f+1`, leader-σ-V-in-Phase-1, equivocation-to-NR rule) — generalized to `K = max(3, f+1)` priority-ordered leaders and augmented with two additions that close the P0.1 byzantine-leader-grief residual that TBFT alone has at `f ≥ 2`:

- **V plaintext in onions** (TBFTR core): each operator's onion at layer `k` carries `V_{L_k}` plaintext alongside the encrypted partial sig, so an operator that didn't receive `V_{L_k}` during Phase 1 can recover it from any peer's onion in Phase 2.
- **Phase 2 split** (composition): Phase 2 splits into 2a (onion broadcast, no NR yet) and 2b (late σ from operators who recovered V; NR otherwise). Together with V plaintext, this lets the `f` honest operators that missed V via selective delivery still contribute σ partials, reaching `qV` and reconstructing.

These two additions cost extra bandwidth (~10–80 KB per slot depending on cluster size) and one extra gossip window (`Δ_2b`, ~100–200 ms). At `n = 4` the P0.1 residual doesn't exist so they're unneeded; at `n ≥ 7` they're what makes the protocol fully resilient. See **Appendix A** at the end for a side-by-side comparison with TBFT.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it

**Suited for:** larger SSV clusters (`n = 7, 10, 13`), single-shot duties with a fixed deadline, where missing the slot is the natural failure mode. The cluster's bandwidth budget can absorb the V-plaintext / late-σ overhead, and the timing budget can absorb the extra Phase-2b window.

**Not suited for:** `n = 4` clusters — use [TBFT](TBFT.md) instead (same protocol minus the TBFTR additions; less bandwidth, no residual to close at this cluster size).

## Setting

- A cluster of `n = 3f + 1` participants with `f ∈ {2, 3, 4}` (so `n ∈ {7, 10, 13}`).
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) to sign no-quorum tags and (b) as the decryption oracle for threshold IBE. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Why it's safe".
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- For each slot, a **leader priority order** `(L_0, L_1, …, L_{n-1})` deterministically derived from slot data. `L_0` is the highest-priority leader. (0-based indexing throughout.)
- A **fallback depth** `K = max(3, f+1)` per cluster: `K=3` for n=7, `K=4` for n=10, `K=5` for n=13.
- Per-slot deadlines: `T_commit` (Phase 1 ends, Phase 2a starts), `T_commit + Δ_2a` (Phase 2a ends, Phase 2b starts), `T_commit + Δ_2a + Δ_2b` (Phase 2b ends, Phase 3 starts).
- A **candidate acceptance cutoff** `T_candidate_accept = T_commit − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers drop any Phase-1 candidate whose first-observation time is later than `T_candidate_accept` — treated locally as not-received. With the cutoff, a candidate accepted by any honest operator before `T_candidate_accept` has at least `D + δ` left to re-flood (or, for TBFTR, to land in peer Phase-2a onions before *their* cutoff), so the byzantine cannot fragment the cluster by timing-based selective delivery. (TBFTR's V-plaintext-in-onions + Phase-2 split also uses this gap; the cutoff makes the recovery channel's timing argument operational, see "Liveness profile".)

## Protocol

### Phase 1 — Candidate broadcast `[T_commit − Δ_1, T_commit]`

Each leader `L_k` for `k ∈ {0, …, K−1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(version, cluster_id, slot, layer k, leader_id, value_root, parent_root)`. The envelope rules out cross-cluster / cross-layer / cross-slot replay at the protocol level rather than relying on application validity to surface those mistakes.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, validate `V_{L_k}` against application-level rules, **check the first-observation timestamp against `T_candidate_accept`** (drop and treat as not-received if later), and silently drop bundles failing any check. A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.

**Bundle propagation.** Honest receivers re-flood the bundle via standard gossipsub — this is what closes selective-delivery attempts by a byzantine leader. The argument requires the **candidate acceptance cutoff** above: an honest receiver accepting a bundle at time `t ≤ T_candidate_accept` has `T_commit − t ≥ D + δ` left for re-flooding to reach every other honest operator before *their* `T_candidate_accept` (clock skew bounded by `δ`). Without the cutoff, a byzantine could release the bundle at `T_commit − ε` for `ε < D + δ`, fragmenting the cluster within the synchrony bound; with the cutoff, late releases are uniformly rejected. This is the same partial-synchrony envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)), made operational by a concrete cutoff.

#### Equivocation handling

If a participant observes two distinct, validly-signed candidates `V_{L_k}` and `V'_{L_k}` from the same `L_k` at layer `k`:

1. Locally treat layer `k` as non-receipt: don't include a positive partial signature for layer `k` in the onion (Phase 2a); broadcast the matching non-receipt attestation in Phase 2b instead.
2. The pair is a self-contained slashable fault proof against `L_k`.

### Phase 2a — Onion broadcast with V plaintext `[T_commit, T_commit + Δ_2a]`

Each participant `i` constructs a `K`-layer onion. Layer `k` is structured as:

```
layer k:  V_{L_k}  ‖  E_{enc_tag_k}( σ_i^V( V_{L_k} ) )
```

where:

- `V_{L_k}` is the leader's value, **plaintext** in the onion. (TBFTR core: gives operators that missed Phase-1 broadcast a recovery channel via peers in Phase 2a.)
- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share.
- `E_{enc_tag_k}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = 2f+1`).
- `enc_tag_0 = ⊥` (layer 0 is plaintext — the highest-priority layer is always openable).
- `enc_tag_k = nr_tag_{k-1}` for `k ≥ 1` (layer `k`'s ciphertext is locked under the previous layer's no-quorum tag).
- `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")`.

For layers where `i` doesn't have a valid `V_{L_k}` (or observed `L_k` equivocate), the layer slot is **null** in `i`'s onion (no plaintext V, no encrypted σ).

**No non-receipt attestations are broadcast yet.** `i` waits for Phase 2b to commit on the NR side.

(Bandwidth optimization, the "hash variant": instead of full V plaintext at every layer, operators may carry `hash(V_{L_k})` (32 B) at each layer they signed and full `V_{L_k}` plaintext only at the layer where `i` is the leader. Any single full-V delivery is sufficient for cluster-wide recovery; everyone else verifies by hash. This is the recommended default for n=10/13 where bandwidth matters; the spec algebra is unchanged. See "Practical caveats" for the bandwidth analysis.)

### Phase 2b — Late σ or non-receipt commitment `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`

For each layer `k` where the operator hasn't yet broadcast σ in their Phase 2a onion:

- If during Phase 2a the operator extracted `V_{L_k}` from a peer's onion and validated it (against `L_k`'s leader-auth signature, the leader's σ^V from Phase 1, and application-level rules): broadcast a **late** `σ_i^V(V_{L_k})` directly. (No encryption; the σ partial counts toward layer-k σ-quorum aggregation just like onion partials.)
- Else (didn't recover V, or recovered V is invalid): broadcast a **non-receipt attestation** `σ_i^{IBE}(nr_tag_k)` from the IBE keypair.

Each honest operator commits to **exactly one** of `{σ, NR}` per layer (whether the σ commit lands in Phase 2a's onion or Phase 2b's late-σ broadcast). A byzantine that publishes both is publicly attributable (see "Cross-signing detection" in Phase 3 and "Why it's safe" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior).

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_2a + Δ_2b, finalize]`

Each operator attempts reconstruction:

```
loop k = 0..K-1:
    sigs = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
        ∪ {σ_j^V(V_{L_k}) from received Phase-2a onion contents at layer k}
        ∪ {σ_j^V(V_{L_k}) from received Phase-2b late-σ broadcasts}
        # deduplicated per operator: the leader's own Phase-1 σ and the same
        # leader's onion-layer-k σ are the same partial, counted once.

    if |valid sigs on a single V_{L_k}| ≥ qV:
        S = reconstruct full V signature on V_{L_k}
        output (V_{L_k}, S); halt

    nrs = {σ_j^{IBE}(nr_tag_k) partials}
          # deduplicated per operator.
    if |valid nrs| ≥ qEnc:
        decryption_key = aggregate(nrs)            # threshold sig on nr_tag_k
        unlock layer (k+1) ciphertexts             # enc_tag_{k+1} = nr_tag_k
        continue
    else:
        halt with no output                         # missed slot

halt with no output                                 # exhausted top-K
```

**`T_arrival`** for the deadline rule is the cutoff by which the operator must have received any Phase-2a onion or Phase-2b broadcast it intends to count — practically, it's `T_commit + Δ_2a + Δ_2b`. The deadline rule (caveat 5) bounds the gap between `T_commit` and `T_arrival` against propagation P99/P999 and clock skew.

The leader's Phase-1 σ partial appears unencrypted in the σ pool at every layer. At layer `k > 0` this means one partial is visible early, before `enc_tag_k` is unlocked — but one partial alone can't reconstruct (need `qV`), and the remaining encrypted onion partials stay sealed until the lower layer's NR-quorum unlocks them. Late σ broadcasts at layer `k > 0` are similarly visible early but can't be aggregated until layer `k` is unlocked.

**Cross-signing detection (attribution-only).** Any operator whose published messages contain *both* a σ partial at layer `k` AND an NR attestation on `nr_tag_k` is a slashable cross-signer. Detection is straightforward — the dual partials are public — and the pair forms self-contained slashing evidence. Under `qEnc = qV`, cross-signing has no safety impact (see "Why it's safe"); the detection is purely for attribution and out-of-band punishment.

Once a participant produces an output `(V, S)`, it submits to the downstream system. Multiple operators may submit independently; the downstream system de-duplicates.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers as a fourth wire-envelope kind: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions / late broadcasts

A participant that hasn't received `j`'s onion / late σ / NR at decryption time treats `j` as not having contributed at all. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f` operators are offline or byzantine combined, no quorum reaches its threshold and the slot is missed.

## Preconditions on the host application

(Same as [TBFT](TBFT.md) "Preconditions on the host application" — application-level validity is a host responsibility; TBFTR extends the precondition to apply at *both* Phase 2a's σ commit AND Phase 2b's late σ commit, since the same operator may sign σ in either window.)

Importantly: an operator that submits `(V_{L_k}, S)` after recovering `V_{L_k}` from a peer's onion must still re-run application-level validity checks before signing the late σ. The peer's signature on V doesn't transfer the validity precondition; each signer validates independently.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot:

- Each layer's leader signs its Phase-1 candidate value — and refreshes (head-change handling, below) re-sign with the new parent root.
- Every operator signs each layer's `V_{L_k}` it considers valid in its Phase-2a onion.
- Operators who recover via Phase-2a peer onions also sign late σ on those values in Phase 2b.

EKM/slashing-protection must permit all of these per-slot V-share signing events without flagging duplicates — the cluster's safety property collapses them to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating point is **candidate signing** (Phase-1 leader and Phase-2 onion/late-σ alike), not just submission.

## Why it's safe

**Safety claim**: at most one full `V` signature is ever produced per TBFTR instance per slot — *cryptographically*, against an arbitrary network adversary, regardless of byzantine cross-signing.

**The safety pigeonhole** at each layer `k`: σ-quorum on `V_{L_k}` and NR-quorum on `nr_tag_k` cannot both be reached.

Algebra (with cross-signing allowed; no exclusion rule needed):

- σ-quorum: `h_σ + byz_σ ≥ qV = 2f+1`, where `byz_σ` is byzantine σ contribution.
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Honest don't sign both sides: `h_σ + h_NR ≤ 2f+1` (total honest count, each signs one side per layer).
- Each byzantine can sign both sides: `byz_σ + byz_NR ≤ 2f` (worst case, all `f` byzantine cross-sign).
- If both quorums reached: `h_σ + h_NR ≥ (2f+1) + (2f+1) − (byz_σ + byz_NR) ≥ 4f+2 − 2f = 2f+2`.
- But `h_σ + h_NR ≤ 2f+1`.
- Contradiction: `2f+2 ≤ 2f+1` is impossible. ∎

Both quorums cannot both be reached at any layer. The proof does not depend on honest operators excluding cross-signers from their aggregation — it's a property of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule.

**On cross-signing.** A byzantine that publishes both `σ_byz^V(V_{L_k})` and `σ_byz^{IBE}(nr_tag_k)` does no safety damage (their contribution to one of the pools is wasted by the algebra above). Cross-signing is **publicly attributable** via the dual partials and treated as slashable evidence (see "Practical caveats"); honest aggregation may filter cross-signers, but doing so is not load-bearing for safety.

The Phase-2 split doesn't change this — both σ sources (Phase-2a onion and Phase-2b late broadcast) count uniformly against the σ-pool, and the algebra above is over cluster-wide signed messages, not protocol phases.

## Liveness profile

TBFTR's liveness is **partial-synchrony-conditional within `T_commit + Δ_2a + Δ_2b`**, the same per-window envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)). If propagation between honest operators stays bounded by the propagation budget, the protocol terminates cleanly. If propagation is degraded badly enough that no σ-quorum and no NR-quorum reach their thresholds at any layer up to `K`, the slot is missed. There is no "round 2" — TBFTR is single-shot by design. **Safety holds in either case** (cryptographic, "Why it's safe").

**P0.1/P0.2 fully closed at n=7, 10, 13 under partial synchrony.** Walking the worst-case byzantine attack at any of these sizes: byzantine `L_k` delivers `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})` to exactly `f+1` honest operators *before `T_candidate_accept`* (else those `f+1` would also reject the bundle), withholds from the remaining `f` honest, refuses to vote in Phase 2a:

- **Phase 2a**: `f+1` honest broadcast σ on V in their onions (with V plaintext per TBFTR core). `f` honest didn't have V from Phase 1 — their onion's layer-k slot is null.
- **Recovery**: the `f` missing-V honest receive V via peer onions in Phase 2a, validate against `L_k`'s leader sig + app rules.
- **Phase 2b**: those `f` honest broadcast late σ. Cluster-wide σ count on V: `f+1` (Phase-2a onions) + `f` (Phase-2b late) + `1` (leader's Phase-1 σ) = `2f+2 ≥ qV = 2f+1`.
- σ-quorum reached at the same layer the byzantine tried to grief. Reconstruct. **Slot succeeds.**

The byzantine has no useful counter-move under partial synchrony. The composition closes the residual `[f+1, 2f-1]` grief window that TBFT alone has at `f ≥ 2`.

Under degraded synchrony where the recovery step fails for some honest (peer 2a onions don't propagate within Δ_2a), those honest emit NR in 2b and the slot may miss — but no safety violation arises (cross-sign-aggregate offline still bounded by the algebra above).

If both `L_k` and other byzantine operators coordinate (e.g., one does P0.1 grief at layer 0, another does P0.1 grief at layer 1), the cluster falls through to whichever layer has an honest leader — `K = max(3, f+1) ≥ f+1` guarantees at least one honest leader in the top-`K`, and at that honest leader's layer the σ-quorum forms cleanly.

## Cryptographic primitive

Same as [TBFT](TBFT.md): threshold IBE / signature-based witness encryption (`drand/tlock` or equivalent). The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

## Properties summary

| Property | TBFTR (n=7+) |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1`, unconditional |
| Validity | Yes, conditional on host-application precondition |
| Termination | **No**, single-shot |
| Equivocation detection | Yes |
| P0.1/P0.2 grief resistance | **Closed** under partial synchrony at all supported cluster sizes via composition |
| Built-in leader fallback | Yes (K layers) |
| Round-change recovery | No |

## Application: SSV Ethereum proposer duty

For an SSV cluster of `n = 7, 10, or 13` proposing an Ethereum block:

| TBFTR concept | SSV mapping |
|---|---|
| `n` participants | 7, 10, or 13 |
| K | `max(3, f+1)` — 3 for n=7, 4 for n=10, 5 for n=13 |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| Leader priority `(L_0, …, L_{n-1})` | reuse existing rotation order |
| `T_commit` | derived from the relay 4s cutoff — e.g. `T_commit ≈ slot_start + 2.7s` |
| `Δ_1` | block-fetch window (~1s, accommodating worst-of-K beacon-fetch latency) |
| `Δ_2a` | onion broadcast window (~300ms) |
| `Δ_2b` | late-σ window (~150ms) |

Phase timeline (n=7 example):

- Phase 1: `slot_start + 1.7s` to `slot_start + 2.7s` (top-K leaders fetch and broadcast bundles).
- Phase 2a: `slot_start + 2.7s` to `slot_start + 3.0s` (onions with V plaintext).
- Phase 2b: `slot_start + 3.0s` to `slot_start + 3.15s` (late σ or NR).
- Phase 3: `slot_start + 3.15s` onwards (reconstruct + submit + certificate gossip, headroom for relay 4s cutoff).

The compressed timeline at larger cluster sizes (n=10, 13) is tighter against the relay cutoff; production telemetry should validate the budget.

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

If the head changes during the Phase-1 fetch window (between `T_commit − Δ_1` and `T_commit`), all candidate values fetched from the previous head are stale (their parent root no longer matches the new head). Each leader detects head changes during its fetch window and refreshes its candidate by re-fetching from the new head, then re-broadcasts the bundle with the new value — superseding the stale bundle. Each refresh signs the new envelope with the new `value_root` and `parent_root`, so the per-share signing log shows multiple V-share signatures for the same slot (covered by the slashing-protection scope in "Preconditions on the host application").

**Refresh vs equivocation.** The structured envelope binds `parent_root`, which makes refresh and equivocation mechanically distinguishable:

- Two bundles from `L_k` with **different** `parent_root` → legitimate refresh on head change. Honest receivers accept the bundle whose `parent_root` matches the current head; stale bundles fail the application-validity check and are silently dropped.
- Two bundles from `L_k` with the **same** `parent_root` but different `value_root` → equivocation. Triggers the equivocation-to-non-receipt rule (Phase 1) and forms self-contained slashing evidence.

**Head validity is locally evaluated at the candidate acceptance time.** Each operator validates `parent_root` against *its own* observed head at the moment of accepting the candidate (no later than `T_candidate_accept`). If honest operators temporarily disagree on the current head — e.g., during an in-flight re-org — they may evaluate the same candidate differently: an operator on `H1` accepts a `parent_root = H1` candidate and signs σ; an operator on `H2` rejects it as stale and emits NR. This split is a **liveness failure, not slashable equivocation**: the leader broadcast a single signed bundle, no equivocation evidence exists. The σ-pool may not reach `qV` and the NR-pool may not reach `qEnc`, in which case the slot misses with no safety violation. The protocol does not attempt to resolve head disagreement at the cluster level — that's an upstream concern (beacon-chain re-org dynamics), not a TBFTR responsibility.

Implementation note: each operator must track the current head locally and validate `parent_root` of received candidates against it.

## Practical caveats

1. **Bandwidth: V plaintext vs hash variant.** Carrying full `V` plaintext at every onion layer scales as `K · |V| · n` per cluster, which exceeds 200 KB at n=10 and 400 KB at n=13. The hash variant (full V at the leader's own layer, 32-B hashes elsewhere) cuts onion growth from `K · |V|` to `K · 32B + |V|` — recommended default for n ≥ 10. Hash-only domain separation: hash by `(slot, cluster, layer, leader)` so hashes can't be replayed across slots/layers.

2. **Phase 2b latency.** The composition adds `Δ_2b` (~100–200 ms on a healthy mesh) over plain TBFT-style timing. Tight against the 4s relay cutoff at n=10/13; the timing budget needs to be tracked against production gossip-propagation P99/P999.

3. **Inconsistency-slashing — three rules.** Same as [TBFT](TBFT.md); not load-bearing for safety (the `qEnc = qV` algebra at "Why it's safe" handles that). Useful for attribution and punishment. **Path-conditional detection limit at deep layers** — at K ≥ 3, σ partials at deep layers are encrypted; if an upper layer succeeds, the deep layer doesn't open and σ+NR cross-signing at that depth goes undetected for *attribution*. Doesn't affect safety (the algebra is over published cluster-wide messages and holds whether or not honest aggregate at that depth). Accepted as a path-conditional limit; deep-layer cross-signers may escape attribution but cannot break safety.

4. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

5. **Deadline coordination.** Clock skew across operators must be bounded by `δ`. Two cutoffs derived from `D` (propagation P99/P999) and `δ` together drive the partial-synchrony assumption:

   - **`T_candidate_accept = T_commit − (D + δ)`** for Phase-1 candidates. Receivers reject candidates whose first-observation time is later. This is what makes the gossipsub re-flooding + Phase-2a peer-recovery argument operational (see "Phase 1" / "Bundle propagation" / "Liveness profile").
   - **`T_arrival = T_commit + Δ_2a + Δ_2b`** for Phase-2a onion / Phase-2b late-σ / NR contributions — the cutoff for accepting Phase-2 messages into the local pools. Same `D + δ` budget against `T_arrival`.

   Both are *liveness* requirements only — safety is unaffected by skew or propagation breakdown (the safety algebra at "Why it's safe" is over cluster-wide signed messages, not per-operator views).

6. **Tag construction and replay.** The `nr_tag_k` tags must uniquely bind `(slot, cluster, layer)` so that ciphertexts from one slot/cluster/layer cannot be replayed/reused.

7. **"At most one full sig" is per-instance.** Same as TBFT. Note that with TBFTR each operator's V-share signs `K` distinct values across its onion plus possibly more in late-σ broadcasts; EKM must permit this without flagging duplicates.

## Where this came from

TBFTR builds on [TBFT](TBFT.md) (originally "Proposal 3" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829)). The two TBFTR additions are:

- **V plaintext in onions**: gives operators that missed Phase 1 a recovery channel via peers' Phase-2a onions.
- **Phase 2 split (composition)**: defers non-receipt commitment to Phase 2b, allowing late σ broadcasts from operators who recovered V via the plaintext channel.

Together they close the P0.1/P0.2 grief residual `[f+1, 2f-1]` that the leader-σ-V-in-Phase-1 mechanism in TBFT alone leaves at `f ≥ 2`. At `f = 1` (n=4) that residual is empty, so TBFTR's additions aren't needed there; use [TBFT](TBFT.md).

## Appendix A — How TBFTR differs from TBFT

| Aspect | [TBFT](TBFT.md) (n=4) | TBFTR (n=7+) |
|---|---|---|
| Cluster size | 4 (f=1) | 7, 10, 13 (f=2, 3, 4) |
| K (fallback depth) | 2 (primary + backup) | `max(3, f+1)` (3, 4, or 5) |
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Equivocation-to-NR rule | Yes | Yes |
| qV / qEnc | `qV = qEnc = 2f+1 = 3` | `qV = qEnc = 2f+1` |
| Onion at layer k | `E_{enc_tag_k}(σ_i^V(V_{L_k}))` (encrypted partial only) | `V_{L_k} ‖ E_{enc_tag_k}(σ_i^V(V_{L_k}))` (V plaintext + encrypted partial) — **TBFTR core** |
| Phase 2 timing | Single window `[T_commit, T_commit + Δ_2]` (onion + NR together) | Split: 2a (onion only) + 2b (late σ or NR) — **composition** |
| Late σ broadcasts | None | Phase 2b — operators who recovered V via TBFTR core sign σ then |
| Liveness against byz-leader grief | Closed under partial synchrony via gossipsub propagation of leader bundle at f=1 | Closed under partial synchrony via Phase-2 composition at all sizes |
| Bandwidth (worst case) | ~21 KB | n=7: ~108 KB, n=10: ~253 KB, n=13: ~497 KB (hash variant) |
| Latency overhead | None beyond standard Phase 2 | +Δ_2b (~100–200 ms) |
| Tag count | 1 (`nr_tag_0` only) | K-1 (`nr_tag_0`, …, `nr_tag_{K-2}`) |
| Number of leader candidates per slot | 2 (primary + backup) | K (top-K priority) |

The two specs share the cryptographic core (`qEnc = qV = 2f+1` for cryptographic safety, leader-authenticated candidates with both V-keypair and operator-identity sigs over a structured envelope, equivocation-to-NR rule, IBE primitive, two DKGs at the same threshold). What differs is K (and consequently the layered-onion depth and tag count), the Phase-2 timing structure, and the V-plaintext / late-σ machinery — all of it doing the work of closing the residual byzantine-leader grief window that doesn't exist at `f = 1`.

**If you're choosing between protocols**: at `n = 4` use TBFT (simpler, cheaper, equally safe). At `n ≥ 7` use TBFTR (only protocol that closes the byzantine-leader selective-delivery grief at `f ≥ 2`).

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

- **Commitment rule.** How do operators pick `k*_i`? "Lowest valid layer" recovers baseline behavior in healthy cases but provides no tiebreaker advantage; smarter rules (e.g., "most-recent-parent-root") might help in re-org scenarios but also might split operators in ways the baseline would not. Needs adversarial-attack analysis.
- **Late-commit timing.** Can an operator who committed in 2a "switch" their commit if they later learn their chosen layer won't reach commit-quorum? Naively no (would break per-operator exclusivity), but more subtle schemes (revocable commits with deadlines) might be possible. Adds protocol-state complexity.
- **Equivocation interaction.** Baseline's equivocation-to-NR rule converts a layer with a misbehaving leader into a clean NR-quorum. The dynamic variant has no NR side — equivocation at layer `k` would have to translate to "operators won't commit to layer `k`," not "operators commit to NR_k." Defining this precisely (and ensuring no path to two outputs) needs care.
- **Slashing-protection.** Per-share signing log shows σ at one layer per slot under this variant — simpler than baseline. But the per-operator picking rule means EKM has to allow σ at *any* layer per slot, not a fixed one. Same envelope-bound slashing-protection check applies.
- **Bandwidth and latency.** The variant cuts σ partials per onion (fewer encrypted layers per operator) but adds K commit-tags to track. Net savings depend on K and the relative size of σ vs commit partial sigs. Phase 2b is shorter (no NR side) but the same Δ_2b window is needed for late commits.
- **Implementation complexity.** Adds a new tag family, a new commitment-partial-sig type, a different aggregation walk. Probably 1.5–2× the spec footprint of the baseline.

#### Choosing the deterministic rule

The protocol shape above is independent of *what* deterministic rule operators use to pick `k*_i`. The rule's only job is to be (a) deterministic over the inputs each operator has at commit time, (b) cluster-consistent enough that 2f+1 honest converge on the same `k*_i`, and (c) defined when at least one layer's candidate validates locally. Two candidate rules:

- **Bid-based (recommended; see B.1).** `argmax_k bid_k` over locally-validated layers, with leader-id tiebreak. Bids are signed in the envelope, byte-identical for every honest receiver, so the rule's output is cluster-consistent under partial synchrony. Lies are bounded (single layer per byz, can't cross-commit beyond f) and attributable post-hoc from the signed envelope + relay record.
- **Parent-root-based ("commit to the layer whose parent_root matches my canonical chain; tiebreak by layer-index").** Fits the protocol shape but doesn't actually buy much: parent-root is a *validity filter*, not a ranking comparator, so the "rule" collapses to "fixed priority among locally-valid layers" — essentially baseline TBFTR's behavior, just routed through commit-tags instead of NR. Worse, the rule's input (parent-root match against local head) isn't cluster-consistent — different operators' beacon-node views of the canonical chain can diverge during a re-org, and the rule's *output* fragments along the same line. Adding parent-root as a routing primitive doesn't fix head-divergence-driven misses; it just relocates them. The cleaner mitigation for head-divergence is application-level (deeper-confirmed parent for non-priority layers), see [TBFT.md](TBFT.md) Appendix B.2 for the corresponding analysis at K=2.

The bid-based rule is the cleaner of the two — and is the one we'd recommend if any dynamic-ordering scheme is pursued.

#### When to consider this

This variant is most relevant if production data shows the baseline TBFTR's fixed priority order causing measurable misses or wasted slots — e.g., re-org-frequent periods where layer 0 routinely fails its NR-quorum step before layer 1 picks up. Without that data, the baseline's simplicity is the right default.

If pursued, the natural place to prototype is **TBFT first** (K=2, see B.1 → [TBFT.md](TBFT.md) Appendix B for the concrete instantiation), then TBFTR. The K=2 design space is much smaller, the safety/liveness analysis is cleaner, and any production lessons translate directly to the K-layer generalization here.

For SSV's n=4 case specifically, the lighter mitigation — fetching `V_{L_1}` from a deeper-confirmed parent so the backup is structurally re-org-resistant — gets most of the benefit without any protocol change. That's the recommended first move; the dynamic-ordering variant is a second-line option for cluster sizes and conditions where the application-level mitigation isn't enough.

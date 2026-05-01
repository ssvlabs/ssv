# TBFTR — Threshold BFT (Roach) for n ≥ 7 clusters

This document describes **TBFTR** ("TBFT Roach") for `n ∈ {7, 10, 13}` clusters: a single-shot agreement protocol that produces one collective threshold-signed value per "slot" against a hard deadline, with full byzantine-leader selective-delivery resilience at all supported cluster sizes.

TBFTR is a natural extension of [TBFT](TBFT.md) — same cryptographic safety story (σ+NR exclusion at aggregation, threshold separation, leader-σ-V-in-Phase-1, equivocation-to-NR rule) — generalized to `K = max(3, f+1)` priority-ordered leaders and augmented with two additions that close the P0.1 byzantine-leader-grief residual that TBFT alone has at `f ≥ 2`:

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
  - **IBE keypair** at threshold `qEnc = f+1`. Used (a) to sign no-quorum tags and (b) as the decryption oracle for threshold IBE. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- For each slot, a **leader priority order** `(L_0, L_1, …, L_{n-1})` deterministically derived from slot data. `L_0` is the highest-priority leader. (0-based indexing throughout.)
- A **fallback depth** `K = max(3, f+1)` per cluster: `K=3` for n=7, `K=4` for n=10, `K=5` for n=13.
- Per-slot deadlines: `T_d` (Phase 1 ends, Phase 2a starts), `T_d + Δ_2a` (Phase 2a ends, Phase 2b starts), `T_d + Δ_2a + Δ_2b` (Phase 2b ends, Phase 3 starts).

## Protocol

### Phase 1 — Candidate broadcast `[T_d − Δ_1, T_d]`

Each leader `L_k` for `k ∈ {0, …, K−1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction.
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(V_{L_k})`. This proves the candidate originated with `L_k`.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(V_{L_k}))` to peers.

Receivers verify both signatures against the leader's known pubkeys, validate `V_{L_k}` against application-level rules, and silently drop bundles failing any check (treated as not-received). A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.

#### Equivocation handling

If a participant observes two distinct, validly-signed candidates `V_{L_k}` and `V'_{L_k}` from the same `L_k` at layer `k`:

1. Locally treat layer `k` as non-receipt: don't include a positive partial signature for layer `k` in the onion (Phase 2a); broadcast the matching non-receipt attestation in Phase 2b instead.
2. The pair is a self-contained slashable fault proof against `L_k`.

### Phase 2a — Onion broadcast with V plaintext `[T_d, T_d + Δ_2a]`

Each participant `i` constructs a `K`-layer onion. Layer `k` is structured as:

```
layer k:  V_{L_k}  ‖  E_{enc_tag_k}( σ_i^V( V_{L_k} ) )
```

where:

- `V_{L_k}` is the leader's value, **plaintext** in the onion. (TBFTR core: gives operators that missed Phase-1 broadcast a recovery channel via peers in Phase 2a.)
- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share.
- `E_{enc_tag_k}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = f+1`).
- `enc_tag_0 = ⊥` (layer 0 is plaintext — the highest-priority layer is always openable).
- `enc_tag_k = nr_tag_{k-1}` for `k ≥ 1` (layer `k`'s ciphertext is locked under the previous layer's no-quorum tag).
- `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")`.

For layers where `i` doesn't have a valid `V_{L_k}` (or observed `L_k` equivocate), the layer slot is **null** in `i`'s onion (no plaintext V, no encrypted σ).

**No non-receipt attestations are broadcast yet.** `i` waits for Phase 2b to commit on the NR side.

(Bandwidth optimization, the "hash variant": instead of full V plaintext at every layer, operators may carry `hash(V_{L_k})` (32 B) at each layer they signed and full `V_{L_k}` plaintext only at the layer where `i` is the leader. Any single full-V delivery is sufficient for cluster-wide recovery; everyone else verifies by hash. This is the recommended default for n=10/13 where bandwidth matters; the spec algebra is unchanged. See "Practical caveats" for the bandwidth analysis.)

### Phase 2b — Late σ or non-receipt commitment `[T_d + Δ_2a, T_d + Δ_2a + Δ_2b]`

For each layer `k` where the operator hasn't yet broadcast σ in their Phase 2a onion:

- If during Phase 2a the operator extracted `V_{L_k}` from a peer's onion and validated it (against `L_k`'s leader-auth signature, the leader's σ^V from Phase 1, and application-level rules): broadcast a **late** `σ_i^V(V_{L_k})` directly. (No encryption; the σ partial counts toward layer-k σ-quorum aggregation just like onion partials.)
- Else (didn't recover V, or recovered V is invalid): broadcast a **non-receipt attestation** `σ_i^{IBE}(nr_tag_k)` from the IBE keypair.

Each operator commits to **exactly one** of `{σ, NR}` per layer (whether the σ commit lands in Phase 2a's onion or Phase 2b's late-σ broadcast). Cross-signing σ and NR for the same layer triggers the σ+NR exclusion rule at aggregation (see "Why it's safe").

### Phase 3 — Local decryption and reconstruction `[T_d + Δ_2a + Δ_2b, finalize]`

Each operator runs the σ+NR exclusion at aggregation, then attempts reconstruction:

```
loop k = 0..K-1:
    sigs = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
        ∪ aggregate σ_j^V(V_{L_k}) from received Phase-2a onion contents at layer k
        ∪ aggregate σ_j^V(V_{L_k}) from received Phase-2b late-σ broadcasts
        EXCLUDING any operator that also broadcast σ_j^{IBE}(nr_tag_k)
        (only counted when enc_tag_k is unlocked; trivially unlocked at k=0)

    if |valid sigs on a single V_{L_k}| ≥ qV:
        S = reconstruct full V signature on V_{L_k}
        output (V_{L_k}, S); halt

    nrs = aggregate σ_j^{IBE}(nr_tag_k) partials
          EXCLUDING any operator that also has a σ at layer k
    if |valid nrs| ≥ qEnc:
        decryption_key = aggregate(nrs)            # threshold sig on nr_tag_k
        unlock layer (k+1) ciphertexts             # enc_tag_{k+1} = nr_tag_k
        continue
    else:
        halt with no output                         # missed slot

halt with no output                                 # exhausted top-K
```

The leader's Phase-1 σ partial appears unencrypted in the σ pool at every layer. At layer `k > 0` this means one partial is visible early, before `enc_tag_k` is unlocked — but one partial alone can't reconstruct (need `qV`), and the remaining encrypted onion partials stay sealed until the lower layer's NR-quorum unlocks them. Late σ broadcasts at layer `k > 0` are similarly visible early but can't be aggregated until layer `k` is unlocked.

Once a participant produces an output `(V, S)`, it submits to the downstream system. Multiple operators may submit independently; the downstream system de-duplicates.

### Treatment of missing onions / late broadcasts

A participant that hasn't received `j`'s onion / late σ / NR at decryption time treats `j` as not having contributed at all. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f` operators are offline or byzantine combined, no quorum reaches its threshold and the slot is missed.

## Preconditions on the host application

(Same as [TBFT](TBFT.md) "Preconditions on the host application" — application-level validity is a host responsibility; TBFTR extends the precondition to apply at *both* Phase 2a's σ commit AND Phase 2b's late σ commit, since the same operator may sign σ in either window.)

Importantly: an operator that submits `(V_{L_k}, S)` after recovering `V_{L_k}` from a peer's onion must still re-run application-level validity checks before signing the late σ. The peer's signature on V doesn't transfer the validity precondition; each signer validates independently.

## Why it's safe

Same algebra as [TBFT](TBFT.md) "Why it's safe", generalized to general `K` and arbitrary cluster sizes.

**Safety claim**: at most one full `V` signature is ever produced per TBFTR instance per slot.

**The σ+NR exclusion rule at aggregation.** Every operator running Phase 3 builds two pools at each layer `k`: σ partials at layer `k` (from onion contents + late-σ broadcasts), and NR partials on `nr_tag_k`. Any operator whose published messages contain *both* a σ partial at layer `k` AND an NR attestation on `nr_tag_k` has both contributions excluded from their respective pools. Structural exclusion — happens regardless of byzantine intent.

**The safety pigeonhole** under this rule: σ-quorum on `V_{L_k}` and NR-quorum on `nr_tag_k` cannot both be reached at any layer.

Algebra:

- σ-quorum: `h_σ + byz_σ_alone ≥ qV = 2f+1`.
- NR-quorum: `h_NR + byz_NR_alone ≥ qEnc = f+1`.
- Each byzantine contributes to at most one side: `byz_σ_alone + byz_NR_alone ≤ f`.
- Sum: `h_σ + h_NR ≥ 3f+2 − f = 2f+2`.
- Honest don't sign both: `h_σ + h_NR ≤ 2f+1`.
- Contradiction: `2f+2 ≤ 2f+1` is impossible.

Both quorums cannot both be reached at any layer. Byzantine σ+NR cross-signing has at worst a *liveness* impact (excluded contributions might prevent quorum), never a safety impact.

The Phase-2 split doesn't change this — each operator commits to exactly one of σ/NR per layer regardless of whether the σ commit is in Phase 2a's onion or Phase 2b's late broadcast. The σ+NR exclusion rule treats both σ sources uniformly.

## Liveness profile

TBFTR does **not** guarantee termination. If the network is bad enough that no σ-quorum and no NR-quorum reach their thresholds at any layer up to `K`, the slot is missed.

**P0.1/P0.2 fully closed at n=7, 10, 13.** Walking the worst-case byzantine attack at any of these sizes: byzantine `L_k` delivers `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})` to exactly `f+1` honest operators just before `T_d`, withholds from the remaining `f` honest, refuses to vote in Phase 2a:

- **Phase 2a**: `f+1` honest broadcast σ on V in their onions (with V plaintext per TBFTR core). `f` honest didn't have V from Phase 1 — their onion's layer-k slot is null.
- **Recovery**: the `f` missing-V honest receive V via peer onions in Phase 2a, validate against `L_k`'s leader sig + app rules.
- **Phase 2b**: those `f` honest broadcast late σ. Cluster-wide σ count on V: `f+1` (Phase-2a onions) + `f` (Phase-2b late) + `1` (leader's Phase-1 σ) = `2f+2 ≥ qV = 2f+1`.
- σ-quorum reached at the same layer the byzantine tried to grief. Reconstruct. **Slot succeeds.**

The byzantine has no useful counter-move — going dark in Phase 2a contributes nothing, and signing NR in Phase 2b is excluded if they also have σ from Phase 1 (their forced bundle commitment). The composition closes the residual `[f+1, 2f-1]` grief window that TBFT alone has at `f ≥ 2`.

If both `L_k` and other byzantine operators coordinate (e.g., one does P0.1 grief at layer 0, another does P0.1 grief at layer 1), the cluster falls through to whichever layer has an honest leader — `K = max(3, f+1) ≥ f+1` guarantees at least one honest leader in the top-`K`, and at that honest leader's layer the σ-quorum forms cleanly.

## Cryptographic primitive

Same as [TBFT](TBFT.md): threshold IBE / signature-based witness encryption (`drand/tlock` or equivalent). The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = f+1`, run once at cluster init.

## Properties summary

| Property | TBFTR (n=7+) |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic, structural via σ+NR exclusion |
| Validity | Yes, conditional on host-application precondition |
| Termination | **No**, single-shot |
| Equivocation detection | Yes |
| P0.1/P0.2 grief resistance | **Closed** at all supported cluster sizes via composition |
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
| `T_d` | derived from the relay 4s cutoff — e.g. `T_d ≈ slot_start + 2.7s` |
| `Δ_1` | block-fetch window (~1s, accommodating worst-of-K beacon-fetch latency) |
| `Δ_2a` | onion broadcast window (~300ms) |
| `Δ_2b` | late-σ window (~150ms) |

Phase timeline (n=7 example):

- Phase 1: `slot_start + 1.7s` to `slot_start + 2.7s` (top-K leaders fetch and broadcast bundles).
- Phase 2a: `slot_start + 2.7s` to `slot_start + 3.0s` (onions with V plaintext).
- Phase 2b: `slot_start + 3.0s` to `slot_start + 3.15s` (late σ or NR).
- Phase 3: `slot_start + 3.15s` onwards (reconstruct + submit, headroom for relay 4s cutoff).

The compressed timeline at larger cluster sizes (n=10, 13) is tighter against the relay cutoff; production telemetry should validate the budget.

## Practical caveats

1. **Bandwidth: V plaintext vs hash variant.** Carrying full `V` plaintext at every onion layer scales as `K · |V| · n` per cluster, which exceeds 200 KB at n=10 and 400 KB at n=13. The hash variant (full V at the leader's own layer, 32-B hashes elsewhere) cuts onion growth from `K · |V|` to `K · 32B + |V|` — recommended default for n ≥ 10. Hash-only domain separation: hash by `(slot, cluster, layer, leader)` so hashes can't be replayed across slots/layers.

2. **Phase 2b latency.** The composition adds `Δ_2b` (~100–200 ms on a healthy mesh) over plain TBFT-style timing. Tight against the 4s relay cutoff at n=10/13; the timing budget needs to be tracked against production gossip-propagation P99/P999.

3. **Inconsistency-slashing — three rules.** Same as [TBFT](TBFT.md); not load-bearing for safety (σ+NR exclusion handles that). Useful for attribution and punishment.

4. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation.

5. **Deadline coordination.** Clock skew across operators must be bounded by `δ`. Liveness requirement only — safety is unaffected by skew (the σ+NR pigeonhole is over cluster-wide signed messages, not per-operator views).

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
| Phase 1 bundle | `(V, σ^V_L, σ^op_L)` | Same |
| σ+NR exclusion at aggregation | Yes | Yes |
| Equivocation-to-NR rule | Yes | Yes |
| Threshold separation | qV=3, qEnc=2 | qV=2f+1, qEnc=f+1 |
| Onion at layer k | `E_{enc_tag_k}(σ_i^V(V_{L_k}))` (encrypted partial only) | `V_{L_k} ‖ E_{enc_tag_k}(σ_i^V(V_{L_k}))` (V plaintext + encrypted partial) — **TBFTR core** |
| Phase 2 timing | Single window `[T_d, T_d + Δ_2]` (onion + NR together) | Split: 2a (onion only) + 2b (late σ or NR) — **composition** |
| Late σ broadcasts | None | Phase 2b — operators who recovered V via TBFTR core sign σ then |
| P0.1/P0.2 closure | Closed by leader-σ-V + algebra at f=1 | Closed by composition at all sizes |
| Bandwidth (worst case) | ~21 KB | n=7: ~108 KB, n=10: ~253 KB, n=13: ~497 KB (hash variant) |
| Latency overhead | None beyond standard Phase 2 | +Δ_2b (~100–200 ms) |
| Tag count | 1 (`nr_tag_p` only) | K-1 (`nr_tag_0`, …, `nr_tag_{K-2}`) |
| Number of leader candidates per slot | 2 (primary + backup) | K (top-K priority) |

The two specs share the cryptographic core (threshold separation, σ+NR exclusion, leader-authenticated candidates with both V-keypair and operator-identity sigs, equivocation-to-NR rule, IBE primitive, two DKGs at `qV` and `qEnc`). What differs is K (and consequently the layered-onion depth and tag count), the Phase-2 timing structure, and the V-plaintext / late-σ machinery — all of it doing the work of closing the residual byzantine-leader grief window that doesn't exist at `f = 1`.

**If you're choosing between protocols**: at `n = 4` use TBFT (simpler, cheaper, equally safe). At `n ≥ 7` use TBFTR (only protocol that closes the byzantine-leader selective-delivery grief at `f ≥ 2`).

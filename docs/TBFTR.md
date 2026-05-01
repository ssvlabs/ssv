# TBFTR — TBFT Roach

A delta against [TBFT.md](TBFT.md) — read that first. Only what differs is described here.

The idea: piggyback `V_{L_k}` as plaintext inside the Phase-2 onion alongside the encrypted partial signature, so an operator that didn't receive `V_{L_k}` during Phase 1 can still recover it later from any peer's onion. The IBE encryption was always gating *signature aggregation*, not `V_{L_k}` itself, so revealing `V_{L_k}` doesn't break the cryptographic safety argument at [TBFT.md:84](TBFT.md).

## What changes

Layer `k` of an onion in TBFT:

```
layer k:  E_{tag_k}( σ_i( V_{L_k} ) )
```

`V_{L_k}` is not present in any form — only embedded one-way under the partial signature, with no recovery path.

Layer `k` of an onion in TBFTR:

```
layer k:  V_{L_k} ‖ E_{tag_k}( σ_i( V_{L_k} ) )   — if operator i has V_{L_k} and signed it
layer k:  null                                    — otherwise (and i broadcasts non-receipt as today)
```

The IBE primitive, tag construction, and gating role are unchanged. Only the operator's own commitment now carries the underlying value alongside the encrypted sig.

## What this enables

An operator that missed `V_{L_k}` during Phase 1 can:

1. Extract `V_{L_k}` from any peer's onion that contains it.
2. Verify partial signatures on `V_{L_k}` they receive (BLS verify needs the message).
3. Reconstruct the full BLS signature once quorum lands.
4. Submit `(V_{L_k}, S)` downstream.

For SSV's proposer duty: a node that didn't get the block in Phase 1 can still be the one whose submission reaches the relay first — better resilience on lossy meshes.

## What this does not enable

The operator still cannot retroactively contribute their own `σ_i(V_{L_k})` after extracting `V_{L_k}` from a peer's onion. They've already broadcast a non-receipt attestation on `nr_tag_k` alongside their onion, and switching to a positive partial would be the slashable contradiction at [TBFT.md:203](TBFT.md). The protocol's commitment point at `T_d` is unchanged.

## Bandwidth tradeoff

Each onion grows by up to `K · |V_{L_k}|`. For SSV proposer duty (`|V| ≈ 1 KB`, `K ≤ 5`), that's ~5 KB per onion, scaled by gossipsub fan-out — same scaling class as TBFT's existing onion bandwidth, with a higher constant.

Carriage variants worth comparing:

- **Hash-only at non-leader layers.** Operators carry `hash(V_{L_k})` (32 B) at each layer they signed; full `V_{L_k}` plaintext only at the layer they're the leader of. Any single full-V delivery suffices; everyone else verifies by hash. Cuts onion growth from `K·|V|` to `K·32B + |V|`. Probably the right default.

- **Leader-only plaintext.** Only the layer-`k` leader includes `V_{L_k}` plaintext in their own onion at layer `k`. Tightest bandwidth, weakest robustness — if the leader's onion is dropped, V is unavailable to operators that missed Phase 1.

## Things to pay attention to

1. **Slashing extension.** The `σ + NR` slashable-contradiction rule at [TBFT.md:203](TBFT.md) extends naturally: if operator `i`'s onion at layer `k` contains plaintext `V_{L_k}` (which means `i` signed `σ_i(V_{L_k})`) AND `i` broadcast a non-receipt for `nr_tag_k`, that's the same contradiction. No new logic — just treat "onion-layer-k carries V/σ" as the positive-sign witness.

2. **Application revalidation.** An operator that extracts `V_{L_k}` from a peer's onion and ends up submitting `(V_{L_k}, S)` must re-run application-level validity checks before submitting. The peer's signature on `V_{L_k}` doesn't transfer the validity precondition; each submitter validates independently.

3. **Equivocation surface.** Onion-carried plaintext makes leader equivocation more visible — different operators' onions advertise different `V_{L_k}` values, immediately detectable cluster-wide. This intersects the leader-equivocation handling planned for TBFT proper; the two need to be specified together.

4. **DoS surface.** Onions are larger and now include potentially-untrusted `V_{L_k}` at every layer. Receivers must cap `|V|` and reject malformed/oversized envelopes early.

5. **Hash-binding domain separation (hash variant).** If hash-only is adopted, the hash should be domain-separated by `(slot, layer, leader)` so hashes can't be replayed across slots/layers.

## Open questions

- Hash-only vs full-V: bandwidth/resilience curve at SSV cluster sizes (n=4, 7, 10, 13).
- Does this help against the selective-delivery grief in [TBFT-audit.md] P0.1? Plausibly no on its own — operators that missed Phase 1 now have `V` but still can't sign positively (they've already committed to non-receipt). Could be paired with a Phase-2 resignaling extension; out of scope for TBFTR.

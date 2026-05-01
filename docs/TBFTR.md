# TBFTR — TBFT Roach

A delta against [TBFT.md](TBFT.md) — read that first. Only what differs is described here.

The idea: piggyback `V_{L_k}` as plaintext inside the Phase-2 onion alongside the encrypted partial signature, so an operator that didn't receive `V_{L_k}` during Phase 1 can still recover it later from any peer's onion. The IBE encryption was always gating *signature aggregation*, not `V_{L_k}` itself, so revealing `V_{L_k}` doesn't break the cryptographic safety argument (see TBFT.md "Why it's safe").

## What changes

Layer `k` of an onion in TBFT:

```
layer k:  E_{enc_tag_k}( σ_i^V( V_{L_k} ) )
```

`V_{L_k}` is not present in any form — only embedded one-way under the partial signature, with no recovery path.

Layer `k` of an onion in TBFTR:

```
layer k:  V_{L_k} ‖ E_{enc_tag_k}( σ_i^V( V_{L_k} ) )   — if operator i has V_{L_k} and signed it
layer k:  null                                          — otherwise (and i broadcasts non-receipt as today)
```

The IBE primitive, tag construction, and gating role are unchanged. Only the operator's own commitment now carries the underlying value alongside the encrypted sig.

## What this enables

An operator that missed `V_{L_k}` during Phase 1 can:

1. Extract `V_{L_k}` from any peer's onion that contains it.
2. Verify partial signatures on `V_{L_k}` they receive (BLS verify needs the message).
3. Reconstruct the full V signature once `qV` partial sigs land.
4. Submit `(V_{L_k}, S)` downstream.

For SSV's proposer duty: a node that didn't get the block in Phase 1 can still be the one whose submission reaches the relay first — better resilience on lossy meshes.

## What this does not enable (under TBFTR alone)

Under TBFTR by itself (with TBFT's existing Phase 2 timing), the operator still cannot retroactively contribute their own `σ_i^V(V_{L_k})` after extracting `V_{L_k}` from a peer's onion. They've already broadcast a non-receipt attestation `σ_i^{IBE}(nr_tag_k)` alongside their onion, and switching to a positive partial would be the slashable σ+NR self-contradiction defined in TBFT.md "Inconsistency-slashing". The protocol's commitment point at `T_d` is unchanged.

This constraint is lifted under the **TBFTR + deferred-NR composition** below, which is what closes the selective-delivery grief described in TBFT.md caveat 1 (P0.1 in [TBFT-audit.md](TBFT-audit.md)).

## Bandwidth tradeoff

Each onion grows by up to `K · |V_{L_k}|`. For SSV proposer duty (`|V| ≈ 1 KB`, `K ≤ 5`), that's ~5 KB per onion, scaled by gossipsub fan-out — same scaling class as TBFT's existing onion bandwidth, with a higher constant.

Carriage variants worth comparing:

- **Hash-only at non-leader layers.** Operators carry `hash(V_{L_k})` (32 B) at each layer they signed; full `V_{L_k}` plaintext only at the layer they're the leader of. Any single full-V delivery suffices; everyone else verifies by hash. Cuts onion growth from `K·|V|` to `K·32B + |V|`. Probably the right default.

- **Leader-only plaintext.** Only the layer-`k` leader includes `V_{L_k}` plaintext in their own onion at layer `k`. Tightest bandwidth, weakest robustness — if the leader's onion is dropped, V is unavailable to operators that missed Phase 1.

## Things to pay attention to

1. **Slashing rule extension.** The σ+NR slashable-contradiction rule (TBFT.md "Inconsistency-slashing") extends naturally: if operator `i`'s onion at layer `k` contains plaintext `V_{L_k}` (which means `i` signed `σ_i^V(V_{L_k})`) AND `i` broadcast a non-receipt for `nr_tag_k`, that's the same contradiction. No new logic — just treat "onion-layer-k carries V/σ" as the positive-sign witness.

2. **Application revalidation.** An operator that extracts `V_{L_k}` from a peer's onion and ends up submitting `(V_{L_k}, S)` must re-run application-level validity checks (TBFT.md "Preconditions on the host application") before submitting. The peer's signature on `V_{L_k}` doesn't transfer the validity precondition; each submitter validates independently.

3. **Equivocation surface — narrowed.** With TBFT's leader-authentication on candidates (TBFT.md Phase 1) and TBFTR's plaintext-V in onions, leader equivocation is immediately visible cluster-wide: different operators' onions advertise different `V_{L_k}` values, both leader-signed. The equivocation-to-non-receipt rule already in TBFT.md handles the protocol-level response; TBFTR makes that rule effective even when honest operators received the equivocating leader's broadcasts via disjoint gossip paths.

4. **DoS surface.** Onions are larger and now include potentially-untrusted `V_{L_k}` at every layer. Receivers must cap `|V|` and reject malformed/oversized envelopes early.

5. **Hash-binding domain separation (hash variant).** If hash-only is adopted, the hash should be domain-separated by `(slot, cluster, layer, leader)` so hashes can't be replayed across slots/layers.

## Composition: deferred non-receipt for selective-delivery resilience

TBFTR alone doesn't save the deterministic byzantine-leader grief described in TBFT.md caveat 1 (P0.1/P0.2 in [TBFT-audit.md](TBFT-audit.md)) — an operator that missed `V_{L_k}` in Phase 1 has already committed its non-receipt by the time peer onions arrive carrying `V_{L_k}` plaintext.

The composition that does close P0.1/P0.2 is TBFTR + a Phase 2 timing change: split Phase 2 into 2a + 2b, deferring non-receipt commitment to the end of Phase 2.

### Phase 2a `[T_d, T_d + Δ_2a]`

Each operator broadcasts its onion with `σ_i^V(V_{L_k})` at every layer where it has `V_{L_k}` (with `V_{L_k}` plaintext per TBFTR core, leader-signed per TBFT.md Phase 1). **No non-receipt attestations yet.**

### Phase 2b `[T_d + Δ_2a, T_d + Δ_2a + Δ_2b]`

For each layer `k` where the operator has not yet signed `σ`:

- If during Phase 2a the operator extracted `V_{L_k}` from a peer onion and validated it (against `L_k`'s leader signature and against application-level rules): broadcast a late `σ_i^V(V_{L_k})`.
- Else: broadcast the non-receipt attestation `σ_i^{IBE}(nr_tag_k)`.

### What it does to P0.1

Walking the n=4, f=1 case (2 honest with V, 1 honest without V, 1 byzantine going dark):

- Phase 2a: 2 honest broadcast σ on V (with V plaintext). Real σ count = 2.
- The 1 missing honest receives a peer's onion in Phase 2a, extracts V from the plaintext, validates it against L_k's operator signature and the application checks.
- Phase 2b: that honest operator signs σ. Real σ count = 3 = `qV`.
- σ-quorum reached on the original layer. Reconstruct. **Slot saved on the same layer the byzantine tried to grief.**

The byzantine has no useful counter-move. Going dark in Phase 2a contributes nothing to either side; signing NR in Phase 2b only matters if `σ` doesn't reach `qV`, and TBFTR closes that path.

### Safety

Each operator commits to exactly one of `{σ, NR}` per layer, regardless of whether the σ commit lands in Phase 2a or 2b. The σ+NR slashable rule is unchanged. Cluster-wide σ-vs-NR quorum exclusion (TBFT.md "Why it's safe") is unchanged. The change is timing-only — no new safety assumption beyond the load-bearing slashing already required by threshold separation.

### Cost

- **Latency**: the additional `Δ_2b` window pushes finalization later. Should be at least P99 onion-propagation so peer onions actually reach operators that need to recover `V_{L_k}` before they commit. Estimate ~100–200 ms on a healthy mesh.
- **Bandwidth**: late `σ` broadcasts only from operators that recovered V — bounded by `f × |partial_sig| × n` via gossipsub. Trivial.

### Dependencies

Requires both:

- TBFTR (this document's core change) — the plaintext V channel that lets operators recover `V_{L_k}` after `T_d`.
- Leader-authenticated candidates (TBFT.md Phase 1) — without them, byzantine peers could ship false `V` plaintext in their onion and trick honest operators into signing garbage during Phase 2b.

### What this does for the audit

Resolves P0.1 (TBFT selective-delivery grief) at the protocol level — no DKG change, no phantom signatures, no further-lowered thresholds. The cost is one extra gossip window (`Δ_2b`). The analogous fix for P0.2 (TBFT2 n=4) is structurally the same: split Phase 2 of TBFT2 likewise; out of scope here.

## Open questions

- Hash-only vs full-V variant: bandwidth/resilience curve at SSV cluster sizes (n=4, 7, 10, 13).
- Tuning `Δ_2a` and `Δ_2b` against the 4s relay deadline once the end-to-end timing budget (TBFT-audit.md P2.3) lands. The composition lengthens TBFT's Phase 2 by `Δ_2b`; whether this fits depends on real-world propagation tails.

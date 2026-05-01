# TBFT2 — Two-Layer Threshold BFT for Single-Shot Deadline-Driven Agreement

This document describes **TBFT2**, a two-layer specialization of [TBFT](TBFT.md) that collapses the n-layer leader fallback to a single primary/backup transition. Same problem, different operating point: substantially lower bandwidth, simpler protocol. Byzantine resilience is the same as TBFT — TBFT2 does *not* trade safety/liveness against byzantine leaders for simplicity.

The protocol is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use TBFT2 (vs TBFT, vs QBFT)

TBFT2 is the right pick when **all** of these hold:

- The application has a natural primary / backup separation (e.g. high-MEV block vs. safe early-fetched block).
- Byzantine grief from one or both leaders is acceptable as a missed slot rather than a safety failure (same as TBFT).
- Bandwidth must scale to the largest cluster sizes you operate.
- Implementation simplicity is valued — TBFT2 has a single layer transition and no `K` parameter to tune.

Pick **TBFT** instead when you want arbitrary leader fallback ordering (more than one fallback after the primary), or when the dual-leader-byzantine probability under random rotation is not acceptable (`f ≥ 2`).

Pick **QBFT** instead when you need round-change liveness recovery (termination across rounds within a slot).

## Setting

Same as [TBFT](TBFT.md):

- A cluster of `n = 3f + 1` participants with byzantine bound `f`.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Reconstructing a full validator signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = f+1`. Used to sign the no-primary-quorum tag and as the IBE decryption oracle.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts, distinct from the threshold keypairs.

Differences from TBFT:

- Two **designated leaders** per slot, deterministically derived from slot data: a **primary leader** `L_p` and a **backup leader** `L_b`, required to be distinct.
- Two leader-fetch deadlines, `T_b < T_p`, plus a final cluster deadline `T_d`.
- A single tag `nr_tag_p = ("slot", N, "cluster", C, "no-primary-quorum")` instead of a per-layer family.

## Protocol

### Phase 1A — Backup candidate broadcast `[T_b, T_b + Δ_1]`

`T_b` is set early (e.g. `T_d − 4s`). `L_b`:

1. Produces a backup candidate `V_b` and validates it against application-level rules.
2. Signs `V_b` with its V-keypair share (producing `σ_{L_b}^V(V_b)`, a partial threshold sig contributing one of the `qV` partials needed for cluster-wide reconstruction) and with its operator-identity key (producing `σ_{L_b}^{op}(V_b)`, the leader-auth proof).
3. Gossips the bundle `(V_b, σ_{L_b}^V(V_b), σ_{L_b}^{op}(V_b))` to peers.

If the head changes between `T_b` and slot start (so that `V_b`'s parent becomes stale), `L_b` should refresh `V_b` and re-broadcast (with both signatures regenerated).

If `L_b` fails to broadcast, the backup path is unavailable for this slot.

### Phase 1B — Primary candidate broadcast `[T_p, T_p + Δ_2]`

`T_p` is set late (e.g. `T_d − 1s`), to allow the primary candidate to capture as much late-arriving value (e.g. MEV) as possible. `L_p` follows the same shape as Phase 1A: produces `V_p`, validates, signs with both keys, and gossips `(V_p, σ_{L_p}^V(V_p), σ_{L_p}^{op}(V_p))`.

Some peers may not receive `V_p` in time.

### Phase 1 receiver checks (both 1A and 1B)

Before accepting a candidate, every receiver verifies:

- The leader-identity signature (`σ_{L_p}^{op}` for primary, `σ_{L_b}^{op}` for backup) against the leader's known operator pubkey.
- The leader's V-keypair partial signature (`σ_{L_p}^V` or `σ_{L_b}^V`) against the leader's V-share pubkey.
- The candidate against application-level rules.

Bundles failing any check are silently dropped (treated as not-received).

**Why both signatures.** Same rationale as in [TBFT.md](TBFT.md) Phase 1: the leader's V-keypair partial gives the cluster a head start of one real threshold partial, which combined with the f+1 honest partials in Phase 2 reaches `qV = 2f+1` exactly at n=4 — closing the byzantine-leader selective-delivery grief at this cluster size (caveat 1 below). At n ≥ 7 the head start narrows the grief window but doesn't close it.

#### Equivocation handling

If a participant observes two distinct, validly-signed candidates from the same leader (`V_p` and `V'_p` both signed by `L_p`, or `V_b` and `V'_b` both signed by `L_b`):

1. Locally treat that leader's slot as non-receipt: don't include the corresponding partial signature in the onion (Phase 2); broadcast the matching no-receipt attestation instead (for primary equivocation, broadcast a non-receipt attestation on `nr_tag_p`; for backup equivocation, omit layer 1 of the onion entirely).
2. The pair of signed candidates is a self-contained slashable fault proof against that leader.

### Phase 2 — Onion broadcast `[T_d, T_d + Δ_3]`

Each participant `i` constructs a 2-layer onion:

```
layer 0:  σ_i^V(V_p)                                # primary, plaintext
layer 1:  E_{nr_tag_p}( σ_i^V(V_b) )                # backup, IBE-encrypted
```

where:

- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share (threshold `qV = 2f+1`).
- `E_{nr_tag_p}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = f+1`); the ciphertext decrypts iff `qEnc` partial IBE-keypair sigs on `nr_tag_p` exist.

If `i` did not receive a valid `V_p`, it omits layer 0 and broadcasts a **non-receipt attestation**: a partial signature `σ_i^{IBE}(nr_tag_p)` from the IBE keypair. These attestations are the witnesses that unlock layer 1.

If `i` did not receive a valid `V_b` either, it omits layer 1 entirely (no useful signature to embed there).

`i` gossips its onion together with any non-receipt attestation.

### Phase 3 — Local decryption and reconstruction `[T_d + Δ_3, finalize]`

```
positive_sigs = aggregate σ_j^V(V_p) from received layer-0 contents
if |valid positive_sigs on a single V_p| ≥ qV:
    S = reconstruct full V signature on V_p
    output (V_p, S); halt
else:
    nrs = aggregate σ_j^{IBE}(nr_tag_p) partials
    if |valid nrs| ≥ qEnc:
        decryption_key = aggregate(nrs)        # threshold sig on nr_tag_p
        unlock layer 1
        backup_sigs = aggregate σ_j^V(V_b) from received layer-1 contents
        if |valid backup_sigs| ≥ qV:
            S = reconstruct full V signature on V_b
            output (V_b, S); halt
halt with no output                            # missed slot
```

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

### Treatment of missing onions

Same as TBFT: an absent operator contributes nothing — no positive partial signature, no non-receipt attestation. No "ALL-value" rule.

## Preconditions on the host application

Same as [TBFT](TBFT.md): each honest operator must validate `V_p` and `V_b` against application-specific rules **before** including a positive partial signature in their onion. For SSV's Ethereum proposer duty, the validation rules are listed in [TBFT.md](TBFT.md) "Preconditions on the host application".

## Why it's safe

The safety pigeonhole at the single layer transition: σ-quorum on `V_p` (`qV = 2f+1` partial sigs) and NR-quorum on `nr_tag_p` (`qEnc = f+1` partial sigs from the IBE keypair) cannot both be reached, given the σ+NR exclusion rule at aggregation (TBFT.md "Why it's safe") — any operator that publishes both a σ partial on V_p AND an NR attestation on nr_tag_p has both contributions excluded from their respective pools.

Algebra is identical in shape to [TBFT](TBFT.md) "Why it's safe" with `K = 2`. Safety is structural — byzantine σ+NR cross-signing has at worst a liveness impact (excluded contributions might prevent quorum), never a safety one. The σ+NR slashing rule is for attribution, not safety enforcement.

Therefore: at most one V signature can ever be reconstructed cluster-wide. Either `V_p` (if positive quorum at layer 0) or `V_b` (if non-receipt quorum unlocks layer 1 and backup positive quorum is also met) — never both.

## Liveness profile

TBFT2 has the same byzantine-resilience profile as TBFT under their respective settings. Specifically:

- If `L_p` is offline / silent and the cluster has at least `qEnc = f+1` honest operators that didn't receive `V_p`, the cluster falls through to `V_b` cleanly. Good — the intended fallback path.
- If `L_b` is offline / silent, the cluster has **no fallback**. If `L_p` also fails (or is byzantine and griefs) the slot is missed.
- **Selective-delivery grief by a byzantine `L_p`** is the same shape as [TBFT.md](TBFT.md) caveat 1: a byzantine `L_p` can selectively deliver `V_p` to exactly `f+1` honest operators and refuse to vote, leaving real σ at `f+1 < qV` and real NR at `f < qEnc`. Both quorums fail. The slot is stuck at the primary, and layer 1 (backup) doesn't unlock either. This is **not** specific to TBFT2 nor to n=4 — it's the same byzantine-leader-grief gap as TBFT, and the doc-honesty / mitigation story is the same. See caveat 1 below.
- For `f ≥ 2`, both `L_p` and `L_b` could be byzantine in the worst case if leader rotation isn't byzantine-aware. The slot is missed.
- For `f = 1` (n=4), at most one of `{L_p, L_b}` can be byzantine, so the cluster has at least one *honest* leader — but the byzantine one can still grief the layer it leads via selective delivery.

Like TBFT, TBFT2 is single-shot — no round-change recovery within a slot.

## Cryptographic primitive

Same as TBFT: threshold IBE / signature-based witness encryption. Only **one tag** is used per slot (`nr_tag_p`), which makes implementation substantially simpler than TBFT's per-layer tags. A `drand/tlock`-style construction works directly for the IBE keypair at threshold `qEnc = f+1`.

## Application: SSV Ethereum proposer duty

| TBFT2 concept | SSV mapping |
|---|---|
| `n` participants | cluster size (4, 7, 10, 13) |
| Slot | Ethereum slot for which the cluster is proposer |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| `L_p` (primary leader) | designated MEV proposer for the slot (e.g. round-1 leader from existing rotation) |
| `V_p` | MEV-optimized block fetched late from the relay |
| `L_b` (backup leader) | a separately designated operator (e.g. round-2 leader; required ≠ `L_p`) |
| `V_b` | safe early-fetched block from a vanilla beacon-node payload, refreshed on head changes |
| `T_b` | early backup window (e.g. `slot_start − 4s`) |
| `T_p` | late primary window (e.g. `slot_start + 2s`) |
| `T_d` | submission deadline (e.g. `slot_start + 3s` to leave headroom for the relay 4s cutoff) |

This is essentially Proposal 1 from the original SSV issue ([ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829)), restructured around TBFT-style cryptographic safety instead of running two QBFT instances. Benefits over Proposal 1:

- Single 1-RTT broadcast instead of two QBFT instances (each 3+ RTTs).
- Cryptographic safety eliminates the "split-vote at deadline `T_d`" liveness risk that Proposal 1 had to handle protocol-side: with TBFT2 there's no decision boundary where some operators sign profitable and others sign backup. The cryptography enforces that only one path succeeds, regardless of operators' local views.

Cost over Proposal 1:

- TBFT2 has no QBFT-style round changes — if both `L_p` and `L_b` fail (or `L_p` actively griefs and the cluster doesn't fall through), the slot is missed and the cluster cannot retry within the slot.

## Comparison

For SSV cluster sizes with blinded blocks (~1 KB), partial signatures (~96 B), and gossipsub broadcast.

For a more detailed scenario-by-scenario comparison (healthy vs degraded networks vs byzantine leaders), see [TBFT-comparison.md](TBFT-comparison.md).

### Bandwidth (per slot, summed across gossip deliveries)

| Cluster | f | QBFT (1 round) | TBFT2 (worst case) | TBFT (K=max(3,f+1), worst case) |
|---|---|---|---|---|
| n=4  | 1 | ~10 KB | ~21 KB  | ~33 KB  |
| n=7  | 2 | ~27 KB | ~53 KB  | ~85 KB  |
| n=10 | 3 | ~50 KB | ~100 KB | ~220 KB |
| n=13 | 4 | ~85 KB | ~161 KB | ~454 KB |

TBFT2 is roughly 2× QBFT-1-round bandwidth across all cluster sizes — about half the constant factor of TBFT(K=3) and substantially less than TBFT at larger `n`.

### Round trips

| | QBFT (1 round) | TBFT | TBFT2 |
|---|---|---|---|
| RTTs | 3 (propose → prepare → commit) | 1 | 1 |

TBFT and TBFT2 have the same RTT advantage: 1 RTT vs 3 for QBFT.

### Asymptotic scaling

- **QBFT:** `O(r · n²)` for `r` rounds.
- **TBFT (K-cap):** `O(K · n²)` — `O(n²)` for fixed `K`.
- **TBFT2:** `O(n²)` — constant 2-layer onion, fixed leaders, only the per-operator partial-signature broadcast scales quadratically.

### Tradeoff summary

| Aspect | TBFT2 | TBFT (K=max(3,f+1)) | QBFT |
|---|---|---|---|
| Leader byzantine tolerance | up to 1 leader fully broken (regardless of `n`) | up to `f` leaders byzantine in top-`K` | up to `f` byzantine, recoverable via round change |
| Selective-delivery grief by a byzantine leader | same as TBFT (the byzantine layer leader grief is layer-local, not protocol-specific) | yes | no (round change recovers) |
| Bandwidth (constant factor over `n²`) | ~2× QBFT-1-round | ~3–5× QBFT-1-round | 1× per round |
| RTTs | 1 | 1 | 3 per round |
| Liveness recovery within a slot | none | none | yes (round changes) |
| Implementation complexity | low (1 tag, 2 layers) | medium (`K` tags, `K` layers) | high (mature in SSV) |
| Cryptographic safety | yes (structural — σ+NR exclusion) | yes (structural — σ+NR exclusion) | no (consensus-based) |

## Practical caveats

1. **Selective-delivery grief by a byzantine `L_p` — closed at n=4 (TBFT2's home turf).** With the Phase-1B leader-σ-on-V mechanism above, the byzantine `L_p` is forced to either publish `σ_{L_p}^V(V_p)` (which counts toward `qV`) or fail the receiver checks (treated as no broadcast → all honest sign NR → fall through to backup). Concretely at `n=4`, `f=1`, `qV=3`, `qEnc=2`:

   - L_p delivers `(V_p, σ_{L_p}^V, σ_{L_p}^{op})` to `k` honest, withholds from `3 - k`.
   - Real σ on V_p: `k + 1` (k honest in onions + leader's σ from Phase 1B).
   - Real NR on `nr_tag_p`: `3 − k`.

   | k | σ count | NR count | Outcome |
   |---|---|---|---|
   | 0 | 1 < qV | 3 ≥ qEnc | Fall through to V_b ✓ |
   | 1 | 2 < qV | 2 ≥ qEnc | Fall through to V_b ✓ |
   | 2 | 3 = qV | 1 < qEnc | Reconstruct V_p ✓ |
   | 3 | 4 ≥ qV | 0 < qEnc | Reconstruct V_p ✓ |

   No grief window. **TBFT2 P0.2 closed at n=4.** This was the cluster size TBFT2 is targeted at; the headline-grief case the audit flagged is now mechanically eliminated.

   At `n ≥ 7` (`f ≥ 2`), a residual grief window of size `f − 1` remains — same algebra as [TBFT.md](TBFT.md) caveat 1 because TBFT2's primary→backup transition is structurally identical to TBFT's layer 0 → layer 1. TBFT2 isn't typically the recommended choice at `n ≥ 7` anyway (caveat 2 below); when run there, the residual grief is the same as full TBFT's, and the protocol-level fix is the [TBFTR](TBFTR.md) deferred-NR composition.

2. **Dual-leader-byzantine grief at `f ≥ 2`.** For `f ≥ 2`, both `L_p` and `L_b` could be byzantine in the same slot if leader rotation isn't byzantine-aware. Probabilities under random rotation: roughly 4.8% (n=7), 6.7% (n=10), 7.7% (n=13). VRF or sub-quorum rotation reduces but doesn't eliminate. Mitigations:

   - **Byzantine-aware leader rotation.** Pick `L_p` and `L_b` from distinct sub-quorums or via VRF, so an attacker controlling `f` operators is unlikely to control both leaders for any given slot.
   - **Frequency amortization.** A cluster missing a small fraction of slots may be acceptable in practice.
   - **Fall back to TBFT.** If single-slot grief is unacceptable, use TBFT with `K ≥ f + 1` (which guarantees ≥1 honest leader in the top-`K`). At n=4 (f=1) this concern doesn't exist.

3. **Inconsistency-slashing — three rules.** Same as [TBFT.md](TBFT.md) "Inconsistency-slashing" caveat:

   - **Self-contradiction (σ + NR at primary).** If operator `i`'s onion contains `σ_i^V(V_p)` and `i` broadcasts `σ_i^{IBE}(nr_tag_p)`, that's a slashable contradiction. Load-bearing for safety under threshold separation.
   - **Leader equivocation.** Two distinct, validly-signed candidates from the same leader (`L_p` or `L_b`) at the same role form a slashable fault proof.
   - **Cross-onion partial-sig equivocation.** Operator `i` appearing in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same role for different `V` is detectable from the partial sigs alone. Slashable on the same logic.

   At TBFT2's two-layer scope, the path-conditional detection limit ([TBFT.md](TBFT.md) caveat 2) is narrower than full TBFT: there's only one fall-through transition. If layer 0 succeeds, layer-1 σ partials are never decrypted, so an operator's σ+NR cross-signing at the backup role escapes detection in that path. Same engineering tradeoff as TBFT.

4. **Deadline coordination.** Same as [TBFT.md](TBFT.md) — clock skew across operators must be bounded by `δ` and known. Deadline rule: `T_d − T_arrival > D + δ` with `D` the propagation P99/P999.

5. **Tag uniqueness.** The single `nr_tag_p` per slot must uniquely bind `(slot, cluster)` to prevent replay across slots. Structure: `("slot", N, "cluster", C, "no-primary-quorum")`.

## Where this came from

TBFT2 is the result of a design exploration starting from "Proposal 1" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829) (backup + profitable block race via two QBFT instances) and reformulating its safety mechanism in terms of TBFT-style threshold cryptography. The result is a substantially simpler protocol than either Proposal 1 (no QBFT instances) or full TBFT (no `K` parameter, no per-layer tags), at the cost of giving up byzantine fallback past the second leader.

If production telemetry shows that the primary candidate succeeds in the vast majority of slots (which is the expected case under healthy networks and honest leaders), TBFT2 dominates full TBFT for SSV's proposer duty: most of TBFT's machinery is overhead for a fallback path that's rarely exercised.

Subsequent refinements (matching the TBFT spec rewrite):

- Leader-authenticated candidates (operator-identity signatures on `V_p` and `V_b`) and the equivocation-to-non-receipt rule.
- Threshold separation (`qV = 2f+1` for V, `qEnc = f+1` for IBE) via a separate DKG.
- Three-rule inconsistency-slashing model.
- Explicit application-validity precondition (deferred to TBFT.md).
- P0.2 audit framing — TBFT2 at n=4 has the *same* byzantine-leader-grief exposure as TBFT, not better. Earlier versions of this doc and [TBFT-comparison.md](TBFT-comparison.md) overstated this; corrected here.

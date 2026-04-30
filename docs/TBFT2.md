# TBFT2 — Two-Layer Threshold BFT for Single-Shot Deadline-Driven Agreement

This document describes **TBFT2**, a two-layer specialization of [TBFT](TBFT.md) that collapses the n-layer leader fallback to a single primary/backup transition. Same problem, different operating point: substantially lower bandwidth, simpler protocol, and weaker liveness against byzantine leadership.

The protocol is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use TBFT2 (vs TBFT, vs QBFT)

TBFT2 is the right pick when **all** of these hold:

- The application has a natural primary / backup separation (e.g. high-MEV block vs. safe early-fetched block).
- Byzantine grief from one or both leaders is acceptable as a missed slot rather than a safety failure.
- Bandwidth must scale to the largest cluster sizes you operate.
- Implementation simplicity is valued — TBFT2 has a single layer transition and no `K` parameter to tune.

Pick **TBFT** instead when you need byzantine fault tolerance up to `f` in the leader role, or want arbitrary leader fallback ordering.

Pick **QBFT** instead when you need round-change liveness recovery (termination across slots).

## Setting

Same as TBFT:

- A cluster of `n = 3f + 1` participants with byzantine bound `f`. Quorum threshold `q = 2f + 1`.
- Threshold BLS keypair via DKG; reconstructing a full signature requires `q` partial signatures.
- A threshold IBE / signature-based-witness-encryption primitive (e.g. `drand/tlock`-style).

Differences from TBFT:

- Two **designated leaders** per slot, deterministically derived from slot data: a **primary leader** `L_p` and a **backup leader** `L_b`, required to be distinct.
- Two leader-fetch deadlines, `T_b < T_p`, plus a final cluster deadline `T_d`.

## Protocol

### Phase 1A — Backup candidate broadcast `[T_b, T_b + Δ_1]`

`T_b` is set early (e.g. `T_d − 4s`). `L_b` produces a backup candidate `V_b` and gossips it to peers. Other participants observe and store `V_b`.

If `L_b` fails to broadcast, the backup path is unavailable for this slot.

If the head changes between `T_b` and slot start (so that `V_b`'s parent becomes stale), `L_b` should refresh `V_b` and re-broadcast.

### Phase 1B — Primary candidate broadcast `[T_p, T_p + Δ_2]`

`T_p` is set late (e.g. `T_d − 1s`), to allow the primary candidate to capture as much late-arriving value (e.g. MEV) as possible. `L_p` produces the primary candidate `V_p` and gossips it. Some peers may not receive `V_p` in time.

### Phase 2 — Onion broadcast `[T_d, T_d + Δ_3]`

Each participant `i` constructs a 2-layer onion:

```
layer 1:  σ_i(V_p)                       # primary, plaintext
layer 2:  E_{tag_no_p}( σ_i(V_b) )       # backup, encrypted
```

where `tag_no_p = ("slot", N, "no-primary-quorum")` and `E_{tag}(·)` is threshold IBE that decrypts iff `q` partial BLS signatures on `tag` exist.

If `i` did not receive `V_p` from `L_p`, it omits layer 1 and instead broadcasts a **non-receipt attestation**: a partial signature on `tag_no_p`. These attestations are the witnesses that unlock layer 2.

If `i` did not receive `V_b` either, it omits layer 2 entirely (no useful signature to embed there).

`i` gossips its onion together with any non-receipt attestation.

### Phase 3 — Local decryption and reconstruction `[T_d + Δ_3, finalize]`

```
positive_sigs = aggregate σ_j(V_p) from received layer-1 contents
if |valid positive_sigs| ≥ q:
    S = reconstruct full BLS signature on V_p
    output (V_p, S); halt
else:
    non_receipts = aggregate non-receipt-attestation partials for tag_no_p
    if |valid non_receipts| ≥ q:
        decryption_key = aggregate(non_receipts)
        unlock layer 2
        backup_sigs = aggregate σ_j(V_b) from layer 2
        if |valid backup_sigs| ≥ q:
            S = reconstruct full BLS signature on V_b
            output (V_b, S); halt
halt with no output                 # missed slot
```

### Treatment of missing onions

Same as TBFT: an absent operator contributes nothing — no positive partial signature, no non-receipt attestation. No "ALL-value" rule.

## Why it's safe

Cryptography enforces: at layer 1, a positive quorum on `V_p` and a non-receipt quorum under `tag_no_p` cannot both be reached.

In a `3f+1` cluster:

- Honest participants either contribute to `V_p`'s positive sig **or** attest non-receipt of `V_p`, never both.
- `f` byzantine can contribute to both sides.
- Honest count = `2f+1`.
- Reaching both quorums requires `f+1` honest contributors per side, i.e. `2f+2 > 2f+1` honest available — impossible.

Therefore: at most one V-signature can ever be reconstructed cluster-wide. Either `V_p` (if positive quorum) or `V_b` (if non-receipt quorum unlocks layer 2 and backup positive quorum is also met) — never both.

The argument is identical in shape to TBFT's, but only one transition needs reasoning instead of `K-1`.

## Liveness profile

TBFT2 has weaker liveness than TBFT against byzantine leaders:

- If `L_p` is offline / silent, the cluster falls through to `V_b` cleanly. Good — the intended fallback path.
- If `L_b` is offline / silent, the cluster has **no fallback**. If `L_p` also fails (or is byzantine and equivocates) the slot is missed.
- For `f = 1` (n=4), at most one of `{L_p, L_b}` can be byzantine, so the cluster always has at least one working leader.
- For `f ≥ 2`, both leaders could be byzantine in the worst case if leader rotation isn't byzantine-aware. The slot is missed.

Like TBFT, TBFT2 is single-shot — no round-change recovery within a slot.

## Cryptographic primitive

Same as TBFT: threshold IBE / signature-based witness encryption. Only **one tag** is used per slot (`tag_no_p`), making implementation substantially simpler than TBFT's per-layer tags. A `drand/tlock`-style construction works directly.

## Application: SSV Ethereum proposer duty

| TBFT2 concept | SSV mapping |
|---|---|
| `n` participants | cluster size (4, 7, 10, 13) |
| Slot | Ethereum slot for which the cluster is proposer |
| `L_p` (primary leader) | designated MEV proposer for the slot (e.g. round-1 leader from existing rotation) |
| `V_p` | MEV-optimized block fetched late from the relay |
| `L_b` (backup leader) | a separately designated operator (e.g. round-2 leader; required ≠ `L_p`) |
| `V_b` | safe early-fetched block from a vanilla beacon-node payload, refreshed on head changes |
| `T_b` | early backup window (e.g. `slot_start − 4s`) |
| `T_p` | late primary window (e.g. `slot_start + 2s`) |
| `T_d` | submission deadline (e.g. `slot_start + 3s` to leave headroom for the relay 4s cutoff) |

This is essentially Proposal 1 from the original SSV issue ([ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829)), restructured around TBFT-style cryptographic safety instead of running two QBFT instances. The benefits over Proposal 1:

- Single 1-RTT broadcast instead of two QBFT instances (each 3+ RTTs).
- Cryptographic safety eliminates the "split-vote at deadline `T_d`" liveness risk that Proposal 1 had to handle protocol-side: with TBFT2 there's no decision boundary where some operators sign profitable and others sign backup. The cryptography enforces that only one path succeeds, regardless of operators' local views.

The cost over Proposal 1:

- TBFT2 has no QBFT-style round changes — if both `L_p` and `L_b` fail, the slot is missed and the cluster cannot retry within the slot.

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

If the cluster runs with a **deterministic backup** (every operator locally produces an identical `V_b` rather than relying on `L_b` to broadcast it), strip the `L_b` block broadcast and TBFT2's bandwidth drops by `n KB` per slot, putting it at ~1.7× QBFT-1-round and removing `L_b`'s single-point-of-failure (see caveat 2 below).

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
| Leader byzantine tolerance | up to 1 (regardless of `n`) | up to `f` | up to `f` |
| Bandwidth (constant factor over `n²`) | ~2× QBFT-1-round | ~3–5× QBFT-1-round | 1× per round |
| RTTs | 1 | 1 | 3 per round |
| Liveness recovery within a slot | none | none | yes (round changes) |
| Implementation complexity | low (1 tag, 2 layers) | medium (`K` tags, `K` layers) | high (mature in SSV) |
| Cryptographic safety | yes | yes | no (consensus-based) |

## Practical caveats

1. **Single-leader-pair byzantine grief.** For `f ≥ 2`, both `L_p` and `L_b` could be byzantine and grief the slot. Mitigations:
   - **Byzantine-aware leader rotation.** Pick `L_p` and `L_b` from distinct sub-quorums or via VRF, so an attacker controlling `f` operators is unlikely to control both leaders for any given slot.
   - **Frequency amortization.** A cluster missing one slot every `n / C(f, 2)` slots in expectation may be acceptable in practice.
   - **Fall back to TBFT.** If single-slot grief is unacceptable, use TBFT with `K ≥ f + 1` (which guarantees ≥1 honest leader in the top-`K`).

2. **Byzantine vote choice in marginal network conditions.** Same shape as TBFT's analysis (see [TBFT.md](TBFT.md) caveat 1) but simpler in scope: only one transition (primary → backup via `tag_no_p`) instead of `K-1`. A byzantine operator's `f` non-receipt votes can flip the outcome only when `f ≤ x ≤ f+1` honest operators didn't receive `V_p` by the deadline. With `x = 0` (typical healthy case), byzantine flooding contributes only `f` sigs — well below the `2f+1` negative-quorum threshold — and the primary candidate succeeds regardless. Mitigations: tune `T_d` above P95 propagation latency (the real defense); slash any operator whose onion contains both `σ_i(V_p)` and a non-receipt attestation for `tag_no_p` (cheap, deters the lazy byzantine). The single-transition structure means there's only one place a byzantine attacker could get leverage, so the residual risk is correspondingly smaller than TBFT's.

3. **Deadline coordination.** Same as TBFT — clock skew across operators must be bounded.

4. **Tag uniqueness.** The single `tag_no_p` per slot must uniquely bind (slot, cluster) to prevent replay across slots.

## Where this came from

TBFT2 is the result of a design exploration starting from "Proposal 1" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829) (backup + profitable block race via two QBFT instances) and reformulating its safety mechanism in terms of TBFT-style threshold cryptography. The result is a substantially simpler protocol than either Proposal 1 (no QBFT instances) or full TBFT (no `K` parameter, no per-layer tags), at the cost of giving up byzantine fallback past the second leader.

If production telemetry shows that the primary candidate succeeds in the vast majority of slots (which is the expected case under healthy networks and honest leaders), TBFT2 dominates full TBFT for SSV's proposer duty: most of TBFT's machinery is overhead for a fallback path that's rarely exercised.

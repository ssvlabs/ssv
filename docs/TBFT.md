# TBFT — Threshold BFT for Single-Shot Deadline-Driven Agreement

This document describes **TBFT** (Threshold BFT), a single-shot agreement protocol for distributed clusters that produce one collective threshold-signed value per "slot" against a hard deadline. TBFT achieves agreement *cryptographically* rather than via multi-round message exchange, trading away classical liveness guarantees in exchange for a one-RTT decision path and built-in leader fallback.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it (and when not to)

**Suited for:** single-shot duties with a fixed deadline, where missing the slot is the natural failure mode and where round-trip latency is the binding constraint. The leader priority order matters (the highest-priority responsive leader's value is preferred).

**Not suited for:** general-purpose state-machine replication, long-running agreement, situations where guaranteed termination across rounds is required, or where the bandwidth budget cannot absorb the `K · n²` constant factor (~3–5× a single QBFT round at typical settings).

## Setting

- A cluster of `n = 3f + 1` participants with byzantine bound `f`. Quorum threshold `q = 2f + 1`. (Same assumption as QBFT.)
- Each participant holds a share of a threshold BLS keypair generated via DKG. Reconstructing a full signature requires `q` partial signatures.
- A second threshold-IBE / signature-based-witness-encryption (SWE) capability — practically, threshold BLS used as a tag-based decryption oracle (this is exactly what `drand/tlock` does).
- For each slot, a **leader priority order** is deterministically derived (e.g. shuffling the participant set by `slot_seed`). Call it `(L_1, L_2, …, L_n)`. `L_1` is the highest-priority leader.
- A **fallback depth** `K` is configured per cluster, with `1 ≤ K ≤ n`. The protocol only attempts the top-`K` leaders; deeper leaders are not used. Recommended default: `K = max(3, f+1)` — large enough to guarantee at least one honest leader in the top-`K` under the byzantine bound, small enough to keep bandwidth bounded.
- A deadline `T_d` is fixed per slot (the time by which a decision must finalize).

## Protocol

### Phase 1 — Candidate broadcast `[T_d − Δ_1, T_d]`

Each leader `L_k` for `k ∈ {1, …, K}`:

1. Independently produces its candidate value `V_{L_k}` (e.g. fetches a block from a beacon node).
2. Gossips `V_{L_k}` to peers.

Other participants observe and store the candidates they receive, but do not need to broadcast their own. By `T_d`, each participant has 0..K candidates from the designated leaders. Missing candidates are treated as null at the corresponding layer.

### Phase 2 — Layered onion broadcast `[T_d, T_d + Δ_2]`

Each participant `i` constructs a `K`-layer onion, one layer per leader in the top-`K` priority set:

```
layer k:  E_{tag_k}( σ_i( V_{L_k} ) )
```

where:

- `σ_i(x)` is `i`'s threshold-BLS partial signature on value `x`.
- `E_{tag}(·)` is threshold IBE: a ciphertext under tag `tag` that decrypts iff `q` partial BLS signatures on the same `tag` exist.
- For layer 1, `tag_1 = ⊥` (plaintext — the highest-priority layer is always openable).
- For layer `k > 1`, `tag_k = ("slot", N, "layer", k−1, "no-quorum")` — i.e. the layer below can only be unlocked when a quorum of "no value reached at layer k−1" attestations exists.

Alongside the onion, participant `i` broadcasts a **non-receipt attestation** for each layer where it doesn't hold a candidate: a partial BLS signature on the corresponding `tag_k`. These attestations are the witnesses that unlock deeper layers.

`i` gossips both the onion and its non-receipt attestations.

### Phase 3 — Local decryption and reconstruction `[T_d + Δ_2, finalize]`

Each participant has now received 0..n onions and a set of non-receipt attestations from peers. Starting at layer 1:

```
loop k = 1..K:
    sigs   = aggregate σ_j(V_{L_k}) from received onions at layer k
    if |valid sigs| ≥ q:
        S = reconstruct full BLS signature on V_{L_k}
        output (V_{L_k}, S); halt
    else:
        nrs = aggregate non-receipt-attestation partials for tag_k
        if |valid nrs| ≥ q:
            decryption_key = aggregate(nrs)
            unlock layer k+1 using decryption_key
            continue
        else:
            halt with no output      # missed slot
halt with no output                  # exhausted top-K, no positive quorum
```

Once a participant produces an output `(V, S)`, it submits to the downstream system (the beacon node, in the SSV example).

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at all: no positive partial signature at any layer, no non-receipt attestation. This is just standard threshold cryptography — only signed messages count, missing operators contribute nothing.

Implication: liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f` operators are offline (or byzantine combined), neither a positive nor a negative quorum will reach `q = 2f+1` and the slot is missed — exactly the failure mode the trust model already assumes.

An earlier version of this protocol (the original Proposal 3) introduced an "absent = ALL-value" rule that treated missing onions as having signed positively at every layer. The intent was to keep liveness in degraded networks. We dropped it because: (a) cryptographic safety already guarantees positive and negative quorums on the same layer can't both be reached, so the rule wasn't load-bearing for safety; (b) the rule effectively counted offline-honest operators as endorsing every block, which weakens the byzantine bound; and (c) the liveness it bought is liveness the cluster wasn't entitled to anyway under the `3f+1` assumption. Standard threshold semantics is simpler, preserves the trust model, and matches the byzantine bound.

## Why it's safe

Cryptography enforces: at any layer `k`, a positive quorum (`q` valid partial signatures on `V_{L_k}`) and a negative quorum (`q` non-receipt attestations under `tag_k`) cannot both be reachable.

In a `3f+1` cluster:

- Honest participants don't sign both "saw `V`" and "didn't see `V`".
- `f` byzantine can sign both sides.
- Honest count = `2f+1`.
- For both quorums to be reachable, each side needs at least `f+1` honest signatures. That's `2f+2 > 2f+1` honest needed — impossible.

Therefore the layer at which reconstruction succeeds is uniquely determined cluster-wide, and at most one full threshold signature is ever produced per slot. Two participants cannot independently reconstruct two contradictory outputs.

This is a different shape of safety than QBFT: QBFT enforces safety via *agreement* (all honest operators decide the same value at decision time); TBFT enforces it via *cryptography* (operators may have different local views or no output at all, but the math precludes contradictory outputs).

## Liveness profile

TBFT does **not** guarantee termination. If the network is bad enough that no positive quorum and no negative quorum are reachable at any layer, no output is produced and the slot is missed. There is no "round 2" — TBFT is single-shot by design.

This is a deliberate tradeoff. For deadline-driven duties where missing a slot is the natural failure mode (you'll get another slot later), this matches the problem. For state-machine replication where progress must be made, this is unacceptable.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023. Uses threshold BLS as the decryption oracle; the tag is conventionally a round number, but the construction is content-agnostic.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain and is integrating with Ethereum PBS.

A TBFT implementation could integrate `drand/tlock`-style ciphertext construction directly. The DKG for the threshold key can reuse SSV's existing operator share setup.

## Properties summary

| Property | TBFT |
|---|---|
| Safety (no contradictory outputs) | Yes, cryptographic |
| Validity (output ∈ proposed values) | Yes |
| Termination (output guaranteed) | **No**, single-shot |
| Equivocation detection | Implicit (each operator commits all `K` partial sigs in one signed onion) |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (the layered structure) |
| Round-change recovery | No |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block:

| TBFT concept | SSV mapping |
|---|---|
| `n` participants | cluster size (4, 7, 10, 13) |
| Slot | Ethereum slot for which the cluster is proposer |
| Candidate `V_i` | block fetched independently from operator `i`'s beacon/relay |
| Threshold key | the validator's split BLS key (already exists in SSV) |
| Leader priority `(L_1, …, L_n)` | reuse QBFT-style leader rotation order |
| Fallback depth `K` | `max(3, f+1)` per cluster: 3 for n=4 and n=7, 4 for n=10, 5 for n=13 |
| Output | full validator-signed Ethereum block |
| `T_d` | derived from the relay 4s cutoff — e.g. `T_d ≈ slot_start + 3s` to leave headroom for relay submission |
| `Δ_1` | block-fetch window (~1s) |
| `Δ_2` | onion-gossip window (~500ms) |

**Phase 1** (`slot_start + 2s` to `slot_start + 3s`): the top-`K` leaders each request a block from their beacon node and gossip the blinded block to peers. Other operators observe.

**Phase 2** (`slot_start + 3s` to `slot_start + 3.5s`): each operator builds a `K`-layer onion of partial validator-signature shares and gossips it.

**Phase 3** (`slot_start + 3.5s` onwards): each operator locally decrypts; first to reconstruct submits the full block to the beacon network. Cryptography ensures only one block can ever get a valid validator signature, so no double-sign risk.

The high-priority leader's block is preferred (highest MEV), with automatic fallback through lower-priority leaders if higher layers don't reach quorum.

## Comparison vs QBFT for SSV cluster sizes

Assuming blinded-block proposals (~1 KB), partial signatures (~96 B), QBFT prepare/commit messages (~200 B with overhead), and gossipsub broadcast (each emitted message reaches all `n−1` peers).

For a more detailed scenario-by-scenario comparison (healthy vs degraded networks vs byzantine leaders), see [TBFT-comparison.md](TBFT-comparison.md).

### Round trips

| Phase | QBFT | TBFT |
|---|---|---|
| Per round | 3 RTTs (propose → prepare → commit) | — |
| Common case (1 QBFT round / single TBFT shot) | **3 RTTs** | **1 RTT** |
| Worst case (8 quick QBFT rounds) | 24 RTTs | still 1 RTT (but may produce no output) |

This is TBFT's main advantage: 1 RTT vs 3 RTTs in the common case translates to roughly 200–500ms saved per slot in typical SSV clusters — meaningful inside a 4s relay deadline.

### Bandwidth

Per slot, summed across all gossipsub deliveries:

| Cluster | f | K | QBFT (1 round) | QBFT (8 rounds) | TBFT (worst case) |
|---|---|---|---|---|---|
| n=4  | 1 | 3 | ~10 KB  | ~80 KB  | ~33 KB |
| n=7  | 2 | 3 | ~27 KB  | ~210 KB | ~85 KB |
| n=10 | 3 | 4 | ~50 KB  | ~400 KB | ~220 KB |
| n=13 | 4 | 5 | ~85 KB  | ~680 KB | ~454 KB |

(`K = max(3, f+1)` per cluster.)

Asymptotic scaling:

- **QBFT:** `O(r · n²)` for `r` rounds — `O(n²)` per round, dominated by `n` operators each gossiping prepare+commit to `n−1` peers.
- **TBFT:** `O(K · n²)` — with `K` capped (default `K = max(3, f+1)`), this is `O(n²)` asymptotically — same class as QBFT — with a higher constant. Without a cap (the original Proposal 3) it would be `O(n³)`, which is what made n=13 borderline-unworkable.

### Reading the comparison

- At **n=4**, TBFT (K=3) is ~3× QBFT-1-round and well below QBFT worst-case. Bandwidth differences in the tens of KB don't matter at this scale; the 1-RTT win is pure upside.
- At **n=7**, TBFT (K=3) is ~3× QBFT-1-round and well below QBFT worst-case. Comfortable.
- At **n=10**, TBFT (K=4) is ~4× QBFT-1-round and roughly half of QBFT worst-case. Tractable within a 500 ms window on a healthy mesh.
- At **n=13** (SSV's largest cluster size), TBFT (K=5) is ~5× QBFT-1-round but still below QBFT worst-case. With the K cap, TBFT remains viable at all SSV cluster sizes; without it, n=13 would have exceeded ~870 KB of onion bandwidth alone.

The decision metric is not bandwidth in isolation — it's bandwidth against time budget. TBFT spends its bandwidth in a single 500 ms gossip window vs. QBFT spreading across 3–24 RTTs. Whether this is faster depends on the mesh's instantaneous capacity, not its sustained throughput.

## Practical caveats and open questions

Things that need to be resolved before TBFT could be deployed for real:

1. **Byzantine vote choice in marginal network conditions.** A byzantine operator's `f` votes for non-receipt can flip the cluster outcome only in the narrow band `f ≤ x ≤ f+1`, where `x` is the number of *honest* operators who didn't receive `V_{L_k}` by the deadline. Outside this band, the network state alone determines the outcome — byzantine choice is irrelevant. With `x = 0` (the typical case under healthy gossip), byzantine "flooding" non-receipt attestations contributes only `f` sigs, well below quorum `q = 2f+1`, and the high-MEV layer succeeds regardless. The protocol does *not* need to detect "lying" non-receipt; it just needs `x` to be small in practice.

   Mitigations that work:
   - **Deadline tuning.** Set `T_d − Δ_1` (the time available for `V_{L_k}` to propagate before operators commit attestations) comfortably above P95 gossip propagation latency for the cluster's mesh. This drives `P(x ≥ f+1) ≪ 1` and eliminates the byzantine leverage band in the common case. **This is the real mitigation; everything else is window dressing.**
   - **Inconsistency-slashing.** If operator `i`'s onion contains `σ_i(V_{L_k})` *and* `i` broadcasts a non-receipt attestation for `tag_k`, that's provably contradictory — slashable. Cheap to implement and deters the lazy byzantine. Doesn't constrain a careful attacker who signs *only* non-receipt (no contradiction to detect), but worth having anyway.

   Mitigations that don't actually help (despite intuitive appeal):
   - "Deadline ordering" of attestations (only valid if signed after `T_d`). Honest operators decide their own attestations from their own view at `T_d`; byzantine pre-broadcasts don't change honest behavior. Adds protocol complexity for no benefit.
   - Delivery acknowledgements / ACK aggregation. A byzantine who doesn't ACK is then free to claim non-receipt without contradiction — which is exactly the case we couldn't catch anyway. Costs a round-trip (eating TBFT's RTT advantage) for no marginal coverage.

   Residual risk: byzantine can opportunistically exploit marginal-network slots where `x` happens to land in `[f, f+1]`. Bounded by frequency of such slots × byzantine fraction. For healthy SSV clusters this should be rare; for a cluster persistently in degraded gossip conditions it's larger and merits monitoring.

2. **Bandwidth scales with `K`.** With the recommended cap `K = max(3, f+1)`, bandwidth is `O(K · n²)`, which is viable for all current SSV cluster sizes (n=4, 7, 10, 13). Larger clusters or higher byzantine bounds would require larger `K`, increasing constant-factor bandwidth proportionally. The cap is a deliberate tradeoff: deeper leader fallback would buy marginal availability in pathological networks but at quickly-rising bandwidth cost.

3. **No prior-art DVT implementation.** Threshold IBE itself is deployed (Drand, Shutter), but the full TBFT-style protocol with negative-attestation-driven layered decryption appears unbuilt. Engineering risk and audit cost are substantial.

4. **DKG cost and key rotation.** If the threshold IBE keypair is per-cluster (long-lived), this is a one-time DKG. If it must rotate per slot for forward-secrecy reasons, the rotation overhead alone may dominate the protocol's budget.

5. **Deadline coordination.** TBFT's safety relies on participants agreeing on what `T_d` means. Clock skew across operators must be bounded and known.

6. **Tag construction and replay.** The `tag` strings used in IBE must uniquely bind (slot, cluster, layer) so that ciphertexts from one slot cannot be replayed/reused in another. Standard hygiene but easy to get wrong.

## Where this came from

This protocol corresponds to "Proposal 3" in the SSV discussion at [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829). The name TBFT (Threshold-BFT) is introduced here for clarity. The cryptographic primitive it relies on is the same one underlying tlock and Shutter.

# TBFT — Threshold BFT for n=4 clusters

This document describes **TBFT** for `n = 4` (`f = 1`) clusters: a single-shot agreement protocol that produces one collective threshold-signed value per "slot" against a hard deadline. TBFT achieves agreement *cryptographically* — single-RTT decision path, structural safety guard against byzantine cross-signing, primary/backup leader fallback.

For larger cluster sizes (`n = 7, 10, 13`), see [TBFTR.md](TBFTR.md) — a natural extension of TBFT that handles the byzantine-leader selective-delivery grief residual at `f ≥ 2`. TBFTR is less efficient than TBFT (extra bandwidth from V plaintext in onions, extra latency from a Phase-2 split); see [TBFTR.md](TBFTR.md) Appendix A for the comparison.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it

**Suited for:** SSV's typical small cluster (`n = 4`), single-shot duties with a fixed deadline, where missing the slot is the natural failure mode and where round-trip latency is the binding constraint. The application has a natural primary/backup separation (high-MEV block vs. safe early-fetched block).

**Not suited for:** larger cluster sizes (use [TBFTR](TBFTR.md)), general-purpose state-machine replication, situations where guaranteed termination across rounds is required.

## Setting

- A cluster of `n = 4` participants with byzantine bound `f = 1`.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1 = 3`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = f+1 = 2`. Used (a) to sign the no-quorum tag and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- Two designated leaders per slot, deterministically derived from slot data: a **primary leader** `L_p` and a **backup leader** `L_b`, required to be distinct.
- Two leader-fetch deadlines, `T_b < T_p`, plus a final cluster deadline `T_commit`. (`T_commit` is a *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after `T_commit`.)
- A single tag `nr_tag_p = ("slot", N, "cluster", C, "no-primary-quorum")`.

The two thresholds are deliberately different. `qV = 3` is the standard BFT quorum and is what makes a reconstructed `V` signature valid against the cluster's pubkey. `qEnc = 2` is the layer-unlock threshold.

## Protocol

### Phase 1A — Backup candidate broadcast `[T_b, T_b + Δ_1]`

`T_b` is set early (e.g. `T_commit − 4s`). `L_b`:

1. Produces a backup candidate `V_b` and validates it against application-level rules (see "Preconditions on the host application").
2. Signs `V_b` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_b}^V(V_b)`. This counts as one of the `qV = 3` partials needed for cluster-wide reconstruction.
   - The operator-identity key — producing the leader-auth signature `σ_{L_b}^{op}(V_b)`. This proves the candidate originated with `L_b`.
3. Gossips the bundle `(V_b, σ_{L_b}^V(V_b), σ_{L_b}^{op}(V_b))` to peers.

If the head changes between `T_b` and slot start (so that `V_b`'s parent becomes stale), `L_b` should refresh `V_b` and re-broadcast (with both signatures regenerated).

If `L_b` fails to broadcast, the backup path is unavailable for this slot.

### Phase 1B — Primary candidate broadcast `[T_p, T_p + Δ_2]`

`T_p` is set late (e.g. `T_commit − 1s`), to allow the primary candidate to capture as much late-arriving value (e.g. MEV) as possible. `L_p` follows the same shape as Phase 1A: produces `V_p`, validates, signs with both keys, and gossips `(V_p, σ_{L_p}^V(V_p), σ_{L_p}^{op}(V_p))`.

Some peers may not receive `V_p` in time.

### Phase 1 receiver checks (both 1A and 1B)

Before accepting a candidate, every receiver verifies:

- The leader-identity signature (`σ_{L_p}^{op}` for primary, `σ_{L_b}^{op}` for backup) against the leader's known operator pubkey for this slot.
- The leader's V-keypair partial signature (`σ_{L_p}^V` or `σ_{L_b}^V`) against the leader's V-share pubkey.
- The candidate against application-level rules.

Bundles failing any check are silently dropped (treated as not-received). A leader who broadcasts `(V, σ^{op})` without `σ^V` (or with garbage in its place) is treated as not having broadcast at all.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with the `f+1 = 2` honest threshold partials produced in Phase 2 by operators who received the leader's bundle, the cluster reaches `qV = 3` real partials on `V_{L_k}` *exactly* — closing the byzantine-leader selective-delivery grief at this cluster size (see "Liveness profile").

#### Equivocation handling

If a participant observes two distinct, validly-signed candidates from the same leader (`V_p` and `V'_p` both signed by `L_p`, or `V_b` and `V'_b` both signed by `L_b`):

1. Locally treat that leader's slot as non-receipt: don't include the corresponding partial signature in the onion (Phase 2); broadcast the matching non-receipt instead (for primary equivocation, broadcast a non-receipt attestation on `nr_tag_p`; for backup equivocation, omit layer 1 of the onion entirely).
2. The pair of signed candidates is a self-contained slashable fault proof against that leader.

### Phase 2 — Onion broadcast `[T_commit, T_commit + Δ_3]`

Each participant `i` constructs a 2-layer onion:

```
layer 0:  σ_i^V(V_p)                                # primary, plaintext
layer 1:  E_{nr_tag_p}( σ_i^V(V_b) )                # backup, IBE-encrypted
```

where:

- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share (threshold `qV = 3`).
- `E_{nr_tag_p}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = 2`); a ciphertext under tag `nr_tag_p` decrypts iff `qEnc` partial sigs on `nr_tag_p` from the IBE keypair exist.

If `i` did not receive a valid `V_p`, it omits layer 0 and broadcasts a **non-receipt attestation**: a partial signature `σ_i^{IBE}(nr_tag_p)` from the IBE keypair. These attestations are the witnesses that unlock layer 1.

If `i` did not receive a valid `V_b` either, it omits layer 1 entirely.

`i` gossips its onion together with any non-receipt attestation.

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_3, finalize]`

Each operator runs the **σ+NR exclusion rule** (see "Why it's safe") then attempts reconstruction:

```
positive_sigs = {σ_{L_p}^V(V_p) from Phase 1B, if valid}
              ∪ aggregate σ_j^V(V_p) from received layer-0 onion contents
                EXCLUDING any operator that also broadcast σ_j^{IBE}(nr_tag_p)
if |valid positive_sigs on V_p| ≥ qV = 3:
    S = reconstruct full V signature on V_p
    output (V_p, S); halt
else:
    nrs = aggregate σ_j^{IBE}(nr_tag_p) partials
          EXCLUDING any operator that also has a σ at layer 0
    if |valid nrs| ≥ qEnc = 2:
        decryption_key = aggregate(nrs)            # threshold sig on nr_tag_p
        unlock layer 1 ciphertexts
        backup_sigs = {σ_{L_b}^V(V_b) from Phase 1A, if valid}
                    ∪ aggregate σ_j^V(V_b) from decrypted layer 1
                      EXCLUDING the same set of σ+NR cross-signers
        if |valid backup_sigs on V_b| ≥ qV = 3:
            S = reconstruct full V signature on V_b
            output (V_b, S); halt
halt with no output                               # missed slot
```

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers as a fourth wire-envelope kind: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at all: no positive partial signature, no non-receipt attestation. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f = 1` operators are offline or byzantine combined, neither quorum will reach its threshold and the slot is missed.

## Preconditions on the host application

TBFT itself guarantees safety (no two contradictory outputs cluster-wide) and provides best-effort liveness. **Validity** — that the output `V` is application-valid — is a precondition the host application must enforce.

Each honest operator must validate `V_p` and `V_b` against application-specific rules **before** including a positive partial signature in their onion at the corresponding layer.

For SSV's Ethereum proposer duty, application-level checks include:

- Slot match (`block.slot == cluster_slot`).
- Proposer index match.
- Fork/domain match (current fork version, expected domain).
- Parent root: matches the operator's view of the head.
- Relay metadata: bid claim, builder pubkey, value validity (against the cluster's relay allow-list).
- Doppelganger and slashing-protection checks: not signing for a slot already signed at.
- Block encoding: well-formed SSZ, reasonable size.

Slashing protection should gate **candidate signing** (Phase 2 onion construction), not just submission. Each operator's V-signing share signs *two* distinct values per slot inside the onion (one per leader); the cluster's safety property collapses these to a single output, but the per-share signing log shows two block sigs at the same slot. EKM must handle this without flagging a violation.

## Why it's safe

**Safety claim**: at most one full `V` signature is ever produced per TBFT instance per slot.

**The σ+NR exclusion rule at aggregation.** Every operator running Phase 3 builds two pools at the primary layer: σ partials on `V_p` (in onions' layer 0), and NR partials on `nr_tag_p` (broadcast separately). Any operator whose published messages contain *both* a σ partial on `V_p` AND an NR attestation on `nr_tag_p` has both contributions excluded from their respective pools. This is a structural exclusion — it happens at aggregation time regardless of byzantine intent or economic incentives. (Same exclusion applies symmetrically at the backup layer once it's unlocked.)

**The safety pigeonhole** under this rule: σ-quorum on `V_p` and NR-quorum on `nr_tag_p` cannot both be reached.

Algebra. Let `h_σ`, `h_NR` be honest operators contributing σ on `V_p` and NR on `nr_tag_p` respectively; `byz_σ_alone`, `byz_NR_alone` the byzantine operators contributing to *exactly one* side (cross-signers excluded from both).

- σ-quorum reached: `h_σ + byz_σ_alone ≥ qV = 3` ⇒ `h_σ ≥ 3 − byz_σ_alone`.
- NR-quorum reached: `h_NR + byz_NR_alone ≥ qEnc = 2` ⇒ `h_NR ≥ 2 − byz_NR_alone`.
- Each byzantine operator contributes to at most one side: `byz_σ_alone + byz_NR_alone ≤ f = 1`.
- Sum: `h_σ + h_NR ≥ 5 − (byz_σ_alone + byz_NR_alone) ≥ 5 − 1 = 4`.
- Honest don't sign both: `h_σ + h_NR ≤ 2f+1 = 3`.
- Contradiction: `4 ≤ 3` is impossible.

Therefore at most one full `V` signature ever materializes. **Byzantine σ+NR cross-signing has at worst a *liveness* impact** (their excluded contributions might prevent a quorum from reaching), **never a safety impact**: they cannot produce two outputs by attempting it.

The σ+NR slashing rule (caveat below) is for *attribution and punishment* of byzantine cross-signers, **not load-bearing for safety**.

## Liveness profile

TBFT does **not** guarantee termination. If the network is bad enough that no σ-quorum and no NR-quorum reach their thresholds, no output is produced and the slot is missed. There is no "round 2" — TBFT is single-shot by design.

**P0.1/P0.2 fully closed at n=4.** Walking the worst-case byzantine attack (`L_p` byzantine, selective delivery to `k` of 3 honest, refuses Phase-2 votes):

- Real σ on V_p: `k` honest in onions + 1 leader's σ from Phase 1B = `k + 1`.
- Real NR on `nr_tag_p`: `3 − k` honest didn't get V_p.

| `k` | σ-side | NR-side | Outcome |
|---|---|---|---|
| 0 | 1 < qV = 3 | 3 ≥ qEnc = 2 | Fall through to V_b ✓ |
| 1 | 2 < qV = 3 | 2 ≥ qEnc = 2 | Fall through to V_b ✓ |
| 2 | 3 = qV | 1 < qEnc = 2 | Reconstruct V_p ✓ |
| 3 | 4 ≥ qV | 0 < qEnc = 2 | Reconstruct V_p ✓ |

No grief window. **Slot succeeds in every byzantine-`L_p` attack scenario.** Symmetric analysis for byzantine `L_b` with `L_p` honest: the primary path resolves cleanly; the backup is irrelevant.

The threshold separation (`qV = 3` vs `qEnc = 2`) is what enables fall-through at `k ∈ {0, 1}` without requiring all three honest to NR. Combined with leader-σ-V-in-Phase-1 closing the `k = 2` boundary case, every value of `k` ends in success.

If both leaders are byzantine (impossible at f=1 — there's only one byzantine), or more than `f` operators are offline combined, the slot misses. That's the standard `3f+1` trust bound.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

Only **one tag** is used per slot (`nr_tag_p`), making implementation substantially simpler than per-layer-tag protocols. A `drand/tlock`-style construction works directly. The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2`, run once at cluster init. Long-lived, no per-slot rotation.

## Properties summary

| Property | TBFT (n=4) |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic, structural via σ+NR exclusion at aggregation |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition |
| Termination (output guaranteed) | **No**, single-shot |
| Equivocation detection | Yes — leaders sign candidates; conflicting signed candidates trigger non-receipt at that layer; pair forms slashable evidence |
| P0.1/P0.2 grief resistance | **Closed** — leader-σ-V-in-Phase-1 closes every byzantine-leader attack |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (primary → backup) |
| Round-change recovery | No |

## Application: SSV Ethereum proposer duty

For an SSV `n=4` cluster proposing an Ethereum block:

| TBFT concept | SSV mapping |
|---|---|
| `n` participants | 4 |
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
| `T_commit` | submission deadline (e.g. `slot_start + 3s` to leave headroom for the relay 4s cutoff) |

Phase timeline:

- Phase 1A: ~`slot_start − 4s` (backup fetch and broadcast).
- Phase 1B: ~`slot_start + 2s` to `slot_start + 3s` (primary fetch and broadcast).
- Phase 2: `slot_start + 3s` to `slot_start + 3.5s` (onion + non-receipt broadcast).
- Phase 3: `slot_start + 3.5s` onwards (reconstruct + submit + certificate gossip).

Cryptography plus σ+NR exclusion ensures only one block can ever get a valid validator signature.

### Timing budget

The end-to-end budget for a proposer slot must fit inside the relay submission cutoff (~4 s after `slot_start`). The structure:

```
slot_start
  + pre-consensus            (RANDAO partial-sig collection, ~T_pre)
  + block fetch              (Δ_1 above; for primary, slot_start + 2s onward)
  + Phase 2 broadcast        (Δ_3 ≈ 500 ms)
  + Phase 3 reconstruct      (BLS aggregate, ~few ms)
  + downstream submission    (relay round-trip, ~T_submit)
≤ slot_start + 4s            (relay cutoff)
```

Concrete numbers for each leg should come from production telemetry (P99 / P999 tails of gossip propagation, EKM signing latency, beacon submit latency, relay submission latency). Until that lands, the values above are placeholder defaults; tighten per cluster as data arrives.

The deadline-tuning rule from caveat 3 below applies: `T_commit − T_arrival > D + δ` where `D` is the propagation P99/P999 and `δ` is the bounded clock-skew across operators.

## Practical caveats

1. **Inconsistency-slashing — three rules.** These rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the σ+NR exclusion rule (see "Why it's safe"), they are **not load-bearing for safety**.

   - **Self-contradiction (σ + NR).** If operator `i`'s onion contains `σ_i^V(V_p)` *and* `i` broadcasts `σ_i^{IBE}(nr_tag_p)`, that's a slashable contradiction. The same publicly-signed pair triggers the σ+NR exclusion at aggregation time.
   - **Leader equivocation.** Two distinct, validly-signed candidates from the same leader are a self-contained slashable fault proof. Honest operators that observe both treat that leader's slot as non-receipt locally and may broadcast the pair as a fault claim.
   - **Cross-onion partial-sig equivocation.** Operator `i` appearing in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different `V` is detectable from the partial sigs alone. Slashable on the same logic; cross-signer's contributions excluded from all groups at this layer.

2. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 3` and IBE at `qEnc = 2` — one DKG each at cluster init. Long-lived, no per-slot rotation.

3. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. The deadline rule is `T_commit − T_arrival > D + δ` where `D` is the propagation P99/P999. This is a *liveness* requirement — under unbounded skew some operators miss the deadline and the slot fails to finalize. Safety is unaffected: the σ+NR pigeonhole is a global property over cluster-wide signed messages, not over per-operator views.

4. **Tag construction and replay.** The single `nr_tag_p` per slot must uniquely bind `(slot, cluster)` to prevent replay across slots. Structure: `("slot", N, "cluster", C, "no-primary-quorum")`.

5. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one TBFT instance and assumes:

   - Single TBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between TBFT and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase 2 onion), not just submission.

## Where this came from

TBFT for n=4 is the result of a design exploration starting from "Proposal 1" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829) (backup + profitable block race via two QBFT instances), reformulated around TBFT-style cryptographic safety. The result is a substantially simpler protocol than Proposal 1 (no QBFT instances) at the cost of giving up byzantine fallback past the second leader.

Refinements applied during specification:

- Leader-authenticated candidates with both V-keypair partial and operator-identity sig. The Phase-1 `σ_{L_k}^V` plus the f+1=2 honest partials in Phase 2 sum to `qV = 3` exactly — closing P0.1/P0.2 at n=4 mechanically.
- σ+NR exclusion rule at aggregation. Makes safety structural rather than slashing-deterred; the exclusion is what closes byzantine σ+NR cross-signing as a safety attack vector.
- Threshold separation (`qV = 3` σ + `qEnc = 2` NR via separate DKG) — buys fall-through liveness when 1 of 3 honest didn't receive V_p.
- 0-based tag indexing.
- Explicit application-validity preconditions.

For larger cluster sizes (`n = 7, 10, 13`), see [TBFTR.md](TBFTR.md).

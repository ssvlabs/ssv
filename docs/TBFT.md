# TBFT — Threshold BFT for n=4 clusters

This document describes **TBFT** for `n = 4` (`f = 1`) clusters: a single-shot agreement protocol that produces one collective threshold-signed value per "slot" against a hard deadline. TBFT achieves agreement *cryptographically* — single-RTT decision path, structural cryptographic safety, primary/backup leader fallback.

TBFT is **only meant for `n = 4`**. For larger cluster sizes (`n = 7, 10, 13`), use [TBFTR](TBFTR.md) — a natural extension of TBFT that handles the byzantine-leader selective-delivery grief residual at `f ≥ 2`. The two specs share the cryptographic core (`qEnc = qV = 2f+1`, structured leader-auth envelope, equivocation-to-NR rule, IBE primitive, two DKGs at the same threshold). What TBFT keeps minimal at n=4: `K = 2`, single tag, asymmetric per-layer fetch times, no V-plaintext in onions, no Phase-2 split. See **Appendix A** for the side-by-side.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it

**Suited for:** SSV's typical small cluster (`n = 4`), single-shot duties with a fixed deadline, where missing the slot is the natural failure mode and where round-trip latency is the binding constraint. The application has a natural primary/backup separation (high-MEV block vs. safe early-fetched block).

**Not suited for:** larger cluster sizes (use [TBFTR](TBFTR.md)), general-purpose state-machine replication, situations where guaranteed termination across rounds is required.

## Setting

- A cluster of `n = 4` participants with byzantine bound `f = 1`.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1 = 3`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = 2f+1 = 3`. Used (a) to sign the no-quorum tag and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Why it's safe".
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- For each slot, two **layers** with deterministically-derived leaders: layer 0 with **primary leader** `L_0` and layer 1 with **backup leader** `L_1`, required to be distinct.
- Two leader-fetch deadlines, `T_1 < T_0`, plus a final cluster deadline `T_commit`. (`T_commit` is a *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after `T_commit`.) The asymmetric fetch times let the primary fetch a high-MEV value late (`T_0` close to `T_commit`) while the backup fetches a safe early value (`T_1` well before `T_0`).
- A **candidate acceptance cutoff** `T_candidate_accept = T_commit − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers drop any candidate whose first-observation time is later than `T_candidate_accept` — treated locally as not-received. This cutoff is what makes the gossipsub-re-flooding argument actually work: a candidate accepted by any honest operator before `T_candidate_accept` has at least `D + δ` to re-flood to every other honest operator before *their* `T_candidate_accept`, modulo clock skew. With the cutoff, the byzantine cannot fragment the cluster by timing-based selective delivery: either a candidate is published early enough that all honest accept it, or so late that none accept it (see "Liveness profile").
- A single tag `nr_tag_0 = ("slot", N, "cluster", C, "layer", 0, "no-quorum")`. (Only one tag is needed because there's only one transition — primary→backup — that requires unlocking.)

## Protocol

### Phase 1 — Candidate broadcast

Phase 1 has two per-layer windows (driven by the asymmetric fetch times): `[T_1, T_1 + Δ_1]` for the backup, then `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, 1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV = 3` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(version, cluster_id, slot, layer k, leader_id, value_root, parent_root)`. The envelope rules out cross-cluster / cross-layer / cross-slot replay at the protocol level rather than relying on application validity to surface those mistakes.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, validate `V_{L_k}` against application-level rules, **check the first-observation timestamp against `T_candidate_accept`** (drop and treat as not-received if later), and silently drop bundles failing any check. A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.

If `L_1` fails to broadcast, the backup path is unavailable for this slot. If `L_0` fails, only the backup path remains. If both fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood the bundle via standard gossipsub — this is what closes selective-delivery attempts by a byzantine leader. The argument requires the **candidate acceptance cutoff** above: an honest receiver accepting a bundle at time `t ≤ T_candidate_accept` has `T_commit − t ≥ D + δ` left for re-flooding to reach every other honest operator before *their* `T_candidate_accept` (clock skew bounded by `δ`). Without the cutoff, a byzantine could release the bundle at `T_commit − ε` for `ε < D + δ`, fragmenting the cluster within the synchrony bound; with the cutoff, late releases are uniformly rejected. This is the same partial-synchrony envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)), made operational by a concrete cutoff.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with the two honest threshold partials produced in Phase 2 by operators who received the leader's bundle (or recovered it via gossipsub re-flooding), the cluster reaches `qV = 3` real partials on `V_{L_k}` exactly — closing the byzantine-leader selective-delivery grief at this cluster size under partial synchrony (see "Liveness profile").

#### Equivocation handling

If a participant observes two distinct, validly-signed candidates from the same leader at layer `k`:

- Two bundles with **different** `parent_root` → legitimate refresh on head change (see "Head-change handling"). Honest receivers accept the bundle whose `parent_root` matches the current head; stale bundles fail the application-validity check and are silently dropped.
- Two bundles with the **same** `parent_root` but different `value_root` → equivocation:
  1. Locally treat that leader's layer as non-receipt: don't include the corresponding partial signature in the onion (Phase 2); for layer-0 equivocation, broadcast a non-receipt attestation on `nr_tag_0`; for layer-1 equivocation, omit layer 1 of the onion entirely.
  2. The pair of signed candidates is a self-contained slashable fault proof against that leader.

### Phase 2 — Onion broadcast `[T_commit, T_commit + Δ_2]`

Phase 2 is a **single window** at n=4 — no 2a/2b split, since the leader-σ + gossipsub re-flooding mechanism already closes the byzantine-leader grief at f=1 under partial synchrony. Each participant `i` constructs a 2-layer onion:

```
layer 0:  σ_i^V(V_{L_0})                                # primary, plaintext
layer 1:  E_{nr_tag_0}( σ_i^V(V_{L_1}) )                # backup, IBE-encrypted
```

where:

- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share (threshold `qV = 3`).
- `E_{nr_tag_0}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = 3`); a ciphertext under tag `nr_tag_0` decrypts iff `qEnc` partial sigs on `nr_tag_0` from the IBE keypair exist.

If `i` did not receive a valid `V_{L_0}` (after the gossipsub re-flooding window), it omits layer 0 and broadcasts a **non-receipt attestation**: a partial signature `σ_i^{IBE}(nr_tag_0)` from the IBE keypair. These attestations are the witnesses that unlock layer 1.

If `i` did not receive a valid `V_{L_1}` either, it omits layer 1 entirely.

`i` gossips its onion together with any non-receipt attestation.

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_2, finalize]`

Each operator attempts reconstruction:

```
sigs = {σ_{L_0}^V(V_{L_0}) from Phase 1, if valid}
     ∪ {σ_j^V(V_{L_0}) from received layer-0 onion contents}
     # deduplicated per operator: the leader's own Phase-1 σ and the same
     # leader's onion-layer-0 σ are the same partial, counted once.

if |valid sigs on V_{L_0}| ≥ qV = 3:
    S = reconstruct full V signature on V_{L_0}
    output (V_{L_0}, S); halt

nrs = {σ_j^{IBE}(nr_tag_0) partials}
      # deduplicated per operator.
if |valid nrs| ≥ qEnc = 3:
    decryption_key = aggregate(nrs)            # threshold sig on nr_tag_0
    unlock layer 1 ciphertexts
    backup_sigs = {σ_{L_1}^V(V_{L_1}) from Phase 1, if valid}
                ∪ {σ_j^V(V_{L_1}) from decrypted layer 1}
                # deduplicated per operator.
    if |valid backup_sigs on V_{L_1}| ≥ qV = 3:
        S = reconstruct full V signature on V_{L_1}
        output (V_{L_1}, S); halt

halt with no output                            # missed slot
```

**`T_arrival`** for the deadline rule is the cutoff by which the operator must have received any Phase-2 onion or NR it intends to count — practically, it's `T_commit + Δ_2`. The deadline rule (caveat 3) bounds the gap between `T_commit` and `T_arrival` against propagation P99/P999 and clock skew.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

**Cross-signing detection (attribution-only).** Any operator whose published messages contain *both* a σ partial at a layer AND an NR attestation on the layer's `nr_tag_0` is a slashable cross-signer. Detection is straightforward — the dual partials are public — and the pair forms self-contained slashing evidence. Under `qEnc = qV`, cross-signing has no safety impact (see "Why it's safe"); the detection is purely for attribution and out-of-band punishment.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers as a fourth wire-envelope kind: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at all: no positive partial signature, no non-receipt attestation. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within `T_commit + Δ_2` (see "Liveness profile"). If more than `f = 1` operators are offline or byzantine combined, neither quorum will reach its threshold and the slot is missed.

## Preconditions on the host application

TBFT itself guarantees safety (no two contradictory outputs cluster-wide) and provides best-effort liveness. **Validity** — that the output `V` is application-valid — is a precondition the host application must enforce.

Each honest operator must validate `V_{L_0}` and `V_{L_1}` against application-specific rules **before** including a positive partial signature in their onion at the corresponding layer.

For SSV's Ethereum proposer duty, application-level checks include:

- Slot match (`block.slot == cluster_slot`).
- Proposer index match.
- Fork/domain match (current fork version, expected domain).
- Parent root: matches the operator's view of the head.
- Relay metadata: bid claim, builder pubkey, value validity (against the cluster's relay allow-list).
- Doppelganger and slashing-protection checks: not signing for a slot already signed at.
- Block encoding: well-formed SSZ, reasonable size.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot:

- Each layer's leader signs its Phase-1 candidate value — and refreshes (head-change handling, below) re-sign with the new parent root.
- Every operator signs each layer's `V_{L_k}` it considers valid in its Phase-2 onion.

EKM/slashing-protection must permit all of these per-slot V-share signing events without flagging duplicates — the cluster's safety property collapses them to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating point is **candidate signing** (Phase-1 leader and Phase-2 onion alike), not just submission.

## Why it's safe

**Safety claim**: at most one full `V` signature is ever produced per TBFT instance per slot — *cryptographically*, against an arbitrary network adversary, regardless of byzantine cross-signing.

**The safety pigeonhole**: σ-quorum on `V_{L_0}` and NR-quorum on `nr_tag_0` cannot both be reached.

Algebra (with cross-signing allowed; no exclusion rule needed):

- σ-quorum: `h_σ + byz_σ ≥ qV = 2f+1 = 3`, where `byz_σ` is byzantine σ contribution.
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1 = 3`.
- Honest don't sign both sides: `h_σ + h_NR ≤ 2f+1 = 3` (total honest count, each signs one side).
- Each byzantine can sign both sides: `byz_σ + byz_NR ≤ 2f = 2` (worst case, all `f` byzantine cross-sign).
- If both quorums reached: `h_σ + h_NR ≥ 3 + 3 − (byz_σ + byz_NR) ≥ 6 − 2 = 4`.
- But `h_σ + h_NR ≤ 3`.
- Contradiction: `4 ≤ 3` is impossible. ∎

Both quorums cannot both be reached. The proof does not depend on honest operators excluding cross-signers from their aggregation — it's a property of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule.

**On cross-signing.** A byzantine that publishes both `σ_byz^V(V_{L_0})` and `σ_byz^{IBE}(nr_tag_0)` does no safety damage (their contribution to one of the pools is wasted by the algebra above). Cross-signing is **publicly attributable** via the dual partials and treated as slashable evidence (see "Practical caveats"); honest aggregation may filter cross-signers, but doing so is not load-bearing for safety.

The same argument applies symmetrically to the backup layer once it's unlocked.

## Liveness profile

TBFT's liveness is **partial-synchrony-conditional within `T_commit + Δ_2`**, the same per-window envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)). If propagation between honest operators stays bounded by the propagation budget, the protocol terminates cleanly. If propagation is degraded badly enough that no σ-quorum and no NR-quorum reach their thresholds, the slot is missed. There is no "round 2" — TBFT is single-shot by design. **Safety holds in either case** (cryptographic, "Why it's safe").

**Byzantine-leader selective-delivery resistance under partial synchrony.** Walk the worst-case attack: byzantine `L_0` tries to selectively deliver `(V_{L_0}, σ_{L_0}^V, σ_{L_0}^{op})` to a strict subset of the 3 honest operators, intending to fragment the σ pool while keeping NR-quorum reachable. With the **candidate acceptance cutoff** (`T_candidate_accept = T_commit − (D + δ)`) honored by every honest receiver, the byzantine has only two consistent outcomes:

| Bundle release time | What every honest sees by `T_candidate_accept` | σ-side | NR-side | Outcome |
|---|---|---|---|---|
| Released at or before `T_commit − (D + δ)`, reaches at least one honest | All 3 honest accept (gossipsub re-flooding completes within `D + δ`) | 3 honest σ + leader's Phase-1 σ = 4 ≥ qV = 3 | 0 | Reconstruct V_{L_0} ✓ |
| Released later than `T_commit − (D + δ)`, or never released, or fully eclipsed | All 3 honest treat as not-received (uniformly past their cutoff, or never arrived) | 0 | 3 ≥ qEnc = 3 | Fall through to V_{L_1} ✓ |

**Slot succeeds in every byzantine-`L_0` attack scenario under partial synchrony.** Symmetric analysis for byzantine `L_1` with `L_0` honest: the primary path resolves cleanly; the backup is irrelevant.

If the synchrony assumption is violated — i.e., real propagation exceeds the budget `D` used to set `T_candidate_accept`, so some honest accepts the bundle and others don't — the cluster fragments: the accepting honest sign σ in Phase 2, the rejecting honest emit NR. With 1 σ + leader's σ + 2 NR, σ-side = 2 < qV = 3 and NR-side = 2 < qEnc = 3 — **both quorums miss, slot misses, no safety violation**. This is the price of single-shot: there's no round 2, so a synchrony break inside the window is unrecoverable. Tightening the cutoff (smaller `D + δ`) trades miss-on-jitter rate for resilience against late byzantine releases; loosening it does the opposite.

If both leaders are byzantine (impossible at f=1 — there's only one byzantine), or more than `f` operators are offline combined, the slot misses. That's the standard `3f+1` trust bound.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

Only **one tag** is used per slot (`nr_tag_0`), making implementation substantially simpler than per-layer-tag protocols. A `drand/tlock`-style construction works directly. The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 3`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

## Properties summary

| Property | TBFT (n=4) |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1`, unconditional |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition |
| Termination (output guaranteed) | **No**, single-shot; partial-synchrony-conditional |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates trigger non-receipt at that layer; pair forms slashable evidence |
| P0.1/P0.2 grief resistance | **Closed under partial synchrony** — leader-σ-V-in-Phase-1 + gossipsub re-flooding closes every byzantine-leader attack |
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
| `L_0` (primary leader) | designated MEV proposer for the slot (e.g. round-1 leader from existing rotation) |
| `V_{L_0}` | MEV-optimized block fetched late from the relay |
| `L_1` (backup leader) | a separately designated operator (e.g. round-2 leader; required ≠ `L_0`) |
| `V_{L_1}` | safe early-fetched block from a vanilla beacon-node payload, refreshed on head changes |
| `T_1` | early backup window (e.g. `slot_start − 4s`) |
| `T_0` | late primary window (e.g. `slot_start + 2s`) |
| `T_commit` | commit / view-fix deadline (e.g. `slot_start + 3s`); reconstruction and submission happen after, with headroom against the relay 4s cutoff |

Phase timeline:

- Phase 1 layer-1: ~`slot_start − 4s` (backup fetch and broadcast).
- Phase 1 layer-0: ~`slot_start + 2s` to `slot_start + 3s` (primary fetch and broadcast; gossipsub re-flooding window between this and Phase 2 closes selective delivery).
- Phase 2: `slot_start + 3s` to `slot_start + 3.5s` (onion + non-receipt broadcast).
- Phase 3: `slot_start + 3.5s` onwards (reconstruct + submit + certificate gossip).

Cryptographic safety (`qEnc = qV`) ensures only one block can ever get a valid validator signature.

### Timing budget

The end-to-end budget for a proposer slot must fit inside the relay submission cutoff (~4 s after `slot_start`). The structure:

```
slot_start
  + pre-consensus            (RANDAO partial-sig collection, ~T_pre)
  + block fetch              (Δ_1; for primary, slot_start + 2s onward)
  + Phase 2 broadcast        (Δ_2 ≈ 500 ms)
  + Phase 3 reconstruct      (BLS aggregate, ~few ms)
  + downstream submission    (relay round-trip, ~T_submit)
≤ slot_start + 4s            (relay cutoff)
```

Concrete numbers for each leg should come from production telemetry (P99 / P999 tails of gossip propagation, EKM signing latency, beacon submit latency, relay submission latency). Until that lands, the values above are placeholder defaults; tighten per cluster as data arrives.

The deadline-tuning rule from caveat 3 below applies: `T_commit − T_arrival > D + δ` where `T_arrival` is the cutoff for accepting Phase-2 contributions (typically `T_commit + Δ_2`), `D` is the propagation P99/P999 and `δ` is the bounded clock-skew across operators.

### Head-change handling

If the head changes during a Phase-1 fetch window, the affected leader's candidate is stale (parent root no longer matches the new head). The leader detects head changes during its fetch window and refreshes its candidate by re-fetching from the new head, then re-broadcasts the bundle with the new value — superseding the stale bundle. Each refresh signs the new envelope with the new `value_root` and `parent_root`, so the per-share signing log shows multiple V-share signatures for the same slot (covered by the slashing-protection scope in "Preconditions on the host application").

The structured envelope binds `parent_root`, which makes refresh and equivocation mechanically distinguishable — see "Equivocation handling".

**Head validity is locally evaluated at the candidate acceptance time.** Each operator validates `parent_root` against *its own* observed head at the moment of accepting the candidate (no later than `T_candidate_accept`). If honest operators temporarily disagree on the current head — e.g., during an in-flight re-org — they may evaluate the same candidate differently:

- An operator on head `H1` accepts a candidate with `parent_root = H1` and signs σ.
- An operator on head `H2` rejects the same candidate (parent stale relative to its view) and emits NR.

This split is a **liveness failure, not slashable equivocation**: the leader broadcast a single signed bundle, no equivocation evidence exists. The σ-pool may not reach `qV` and the NR-pool may not reach `qEnc`, in which case the slot misses with no safety violation. The protocol does not attempt to resolve head disagreement at the cluster level — that's an upstream concern (beacon-chain re-org dynamics), not a TBFT responsibility. Operators on the "right" head will continue normally; operators on the stale head will follow head-tracking back to consensus on the next slot.

## Practical caveats

1. **Inconsistency-slashing — three rules.** These rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the cryptographic safety from `qEnc = qV` (see "Why it's safe"), they are **not load-bearing for safety**.

   - **Self-contradiction (σ + NR).** If operator `i`'s onion contains `σ_i^V(V_{L_0})` *and* `i` broadcasts `σ_i^{IBE}(nr_tag_0)`, that's a slashable contradiction. Detection is from the public dual partials.
   - **Leader equivocation.** Two distinct, validly-signed candidates from the same leader with the same `parent_root` but different `value_root` are a self-contained slashable fault proof. (Two with different `parent_root` are a legitimate refresh, not equivocation.) Honest operators that observe both treat that leader's slot as non-receipt locally and may broadcast the pair as a fault claim.
   - **Cross-onion partial-sig equivocation.** Operator `i` appearing in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different `V` is detectable from the partial sigs alone. Slashable on the same logic.

2. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 3` and IBE at `qEnc = 3` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

3. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. Two cutoffs derived from `D` (propagation P99/P999) and `δ` together drive the partial-synchrony assumption:

   - **`T_candidate_accept = T_commit − (D + δ)`** for Phase-1 candidates. Receivers reject candidates whose first-observation time is later. This is what makes the gossipsub re-flooding argument operational (see "Phase 1 receiver checks" / "Bundle propagation").
   - **`T_arrival = T_commit + Δ_2`** for Phase-2 onion / NR contributions — the cutoff for accepting Phase-2 messages into the local pools. Same `D + δ` budget against `T_arrival` (i.e. messages should arrive at all honest operators by `T_arrival + δ`).

   Both are *liveness* requirements — under unbounded skew or propagation breakdown, some operators miss the deadlines and the slot fails to finalize. Safety is unaffected: the safety algebra at "Why it's safe" is a global property over cluster-wide signed messages, not over per-operator views or timing.

4. **Tag construction and replay.** The single `nr_tag_0` per slot must uniquely bind `(slot, cluster, layer 0)` to prevent replay across slots/layers/clusters. Structure: `("slot", N, "cluster", C, "layer", 0, "no-quorum")`.

5. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one TBFT instance and assumes:

   - Single TBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between TBFT and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction), not just submission.

## Where this came from

TBFT for n=4 is the result of a design exploration starting from "Proposal 1" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829) (backup + profitable block race via two QBFT instances), reformulated around TBFT-style cryptographic safety. The result is a substantially simpler protocol than Proposal 1 (no QBFT instances) at the cost of giving up byzantine fallback past the second leader.

Refinements applied during specification:

- Leader-authenticated candidates with both V-keypair partial and operator-identity sig over a structured envelope. The Phase-1 `σ_{L_k}^V` plus the f+1=2 honest partials in Phase 2 sum to `qV = 3` exactly — closing P0.1/P0.2 at n=4 mechanically under partial synchrony.
- `qEnc = qV = 2f+1` for cryptographic safety against byzantine cross-signing. The σ+NR structural-exclusion approach that an earlier draft relied on was honest-only and could be subverted by a byzantine aggregating partials offline; the unified threshold makes the safety property hold regardless.
- Bundle propagation via gossipsub re-flooding called out explicitly. This is the mechanism that closes selective-delivery attempts at f=1 under partial synchrony.
- 0-based layer indexing aligned with [TBFTR](TBFTR.md) (`L_0`/`L_1` instead of `L_p`/`L_b`).
- Explicit application-validity preconditions covering Phase-1 leader signing as well as Phase-2 onion construction.

For larger cluster sizes (`n = 7, 10, 13`), see [TBFTR](TBFTR.md).

## Appendix A — How TBFT differs from TBFTR

| Aspect | TBFT (n=4) | [TBFTR](TBFTR.md) (n=7+) |
|---|---|---|
| Cluster size | 4 (f=1) | 7, 10, 13 (f=2, 3, 4) |
| K (fallback depth) | 2 (primary + backup) | `max(3, f+1)` (3, 4, or 5) |
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Equivocation-to-NR rule | Yes | Yes |
| qV / qEnc | `qV = qEnc = 2f+1 = 3` | `qV = qEnc = 2f+1` |
| Onion at layer k | `E_{enc_tag_k}(σ_i^V(V_{L_k}))` (encrypted partial only; no V plaintext) | `V_{L_k} ‖ E_{enc_tag_k}(σ_i^V(V_{L_k}))` (V plaintext + encrypted partial) — TBFTR core |
| Phase 2 timing | Single window `[T_commit, T_commit + Δ_2]` (onion + NR together) | Split: 2a (onion only) + 2b (late σ or NR) — composition |
| Late σ broadcasts | None | Phase 2b — operators who recovered V via TBFTR core sign σ then |
| Phase-1 fetch timing | Asymmetric: `T_1 < T_0` (backup early, primary late) | Uniform: all leaders fetch in `[T_commit − Δ_1, T_commit]` |
| Liveness against byz-leader grief | Closed under partial synchrony via leader-σ + gossipsub re-flooding | Closed under partial synchrony via Phase-2 composition |
| Bandwidth (worst case) | ~21 KB | n=7: ~108 KB, n=10: ~253 KB, n=13: ~497 KB (hash variant) |
| Latency overhead | None beyond standard Phase 2 | +Δ_2b (~100–200 ms) |
| Tag count | 1 (`nr_tag_0` only) | K-1 (`nr_tag_0`, …, `nr_tag_{K-2}`) |
| Number of leader candidates per slot | 2 (primary + backup) | K (top-K priority) |

The two specs share the cryptographic core. TBFT keeps the protocol minimal at n=4 because the byzantine-leader grief residual that TBFTR's machinery exists to close (`[f+1, 2f-1]` at `f ≥ 2`) is empty at `f = 1` — leader-σ-V-in-Phase-1 plus gossipsub re-flooding cover it under partial synchrony, with no need for V plaintext or a Phase-2 split.

## Appendix B — Dynamic leader-ordering extensions

This appendix sketches two related extensions that replace the fixed primary→backup priority with **dynamic ordering** based on a deterministic rule over the candidates supplied. Both are design sketches, not part of the baseline TBFT spec — the baseline's fixed priority is simpler and equally good in healthy operation. The two variants differ in what the deterministic rule operates on.

- **B.1** sketches **bid-ordered** selection — clean fit at n=4, well-defined semantics, attribution story.
- **B.2** sketches **parent-root-based** "ordering" — included for contrast, mostly to call out why TBFT doesn't extend well to support it.

The cryptographic safety machinery (per-layer commit-tags + IBE-encrypted σ partials + per-operator commit exclusivity) is shared between the two variants; only the deterministic rule differs. The general K-layer formulation lives in [TBFTR.md](TBFTR.md) Appendix B.

### B.1 — Bid-ordered leader selection at n=4

Sketches a TBFT extension where each leader attaches a bid value to their Phase-1 envelope; operators commit to whichever layer's bid is higher (subject to application validity). Specialized to `K = 2` and SSV's proposer-duty bid concept.

**Status: design sketch, not part of the baseline TBFT spec.** Captured because the deterministic rule has a natural application-level instantiation (the relay's MEV bid is already part of proposer duty), and the safety/liveness/attribution story is clean — easier to evaluate than the general K-layer case.

#### Motivation

Baseline TBFT fixes `L_0` as the primary leader for the slot regardless of which leader actually produced a higher-value block. In healthy operation `L_0`'s late-fetched MEV-optimized block typically dominates `L_1`'s safe early block, so the fixed priority is "right" most of the time. But:

- If `L_0`'s MEV fetch fails (relay timeout, network hiccup, byzantine producer), the cluster has to walk through `nr_tag_0` to reach `L_1`. With `qEnc = qV = 3`, that walk requires all 3 honest to NR `L_0` first — same agreement threshold as just committing to `L_1` directly, but a wasted step.
- A byzantine `L_0` that produces a *valid but low-MEV* block forces the cluster to either accept the suboptimal block or reach NR-quorum (which costs honest cooperation that might be hard to coordinate). Either way the slot's value capture is degraded.

Bid-ordered selection lets the cluster pick whichever layer's leader actually produced the higher-value block, making the fixed-priority bias a no-op when bids reflect reality and a graceful skip when they don't.

#### Core idea

Each leader includes a **bid** in their Phase-1 envelope, signed by the leader-identity key (so it can't be repudiated post-hoc). Operators receive both bundles, validate both candidates, then commit to the layer whose bid is highest among the layers where the candidate validated locally. Same `qEnc = qV = 2f+1` safety pigeonhole as the general dynamic-ordering variant — at most one commit-quorum reaches.

Crucially: **bids are trusted at runtime, verified post-hoc**. A byzantine leader that lies about their bid can steer the cluster onto a suboptimal block but cannot create two outputs (safety holds cryptographically) and cannot cause a slot miss on its own (if their lying-bid block passes validation, slot completes with the lying block; if it fails validation, the cluster commits to the other layer). The lie is a **liveness fault** in the value-capture sense, attributable from the signed envelope after the slot.

#### Protocol shape (delta from baseline TBFT)

**Setting (modified):** the structured envelope in Phase 1 binds `(version, cluster_id, slot, layer k, leader_id, value_root, parent_root, bid)`. Bid is whatever numeric type the application uses for value comparison (uint256 wei, fixed-point, etc. — the protocol just needs a total ordering with a tiebreaker). Tiebreaker for equal bids: lower `leader_id` wins (deterministic).

Two commit-tags replace the single `nr_tag_0`:

- `commit_tag_0 = ("slot", N, "cluster", C, "layer", 0, "commit")`
- `commit_tag_1 = ("slot", N, "cluster", C, "layer", 1, "commit")`

**Phase 1 (modified):** each leader broadcasts the bundle including the bid in the envelope. Receivers verify both signatures against the envelope (now including `bid`), validate the candidate, check `T_candidate_accept`, drop on any failure. Equivocation rule unchanged — two distinct envelopes from the same leader for the same `(slot, layer)` is slashable regardless of bid values.

**Phase 2 (modified):** single window, but the onion structure is uniform across layers (no plaintext layer 0):

```
For operator i, given the validated candidates in i's local view:
  k*_i = argmax_k { bid_k : V_{L_k} validated by i }
         (with leader_id tiebreak; undefined if no V validates)

If k*_i is defined:
  Broadcast:
    σ_i^{IBE}(commit_tag_{k*_i})                          # commitment partial sig
    E_{commit_tag_{k*_i}}( σ_i^V(V_{L_{k*_i}}) )           # σ partial encrypted under chosen-layer commit-tag

If k*_i is undefined (no candidate validated):
  Broadcast nothing in Phase 2. Slot misses for this operator's contribution.
```

Each operator commits to **exactly one** layer (or none). No separate NR side — "didn't commit anywhere" is the absence of a commitment, not a positive signal that needs aggregating.

**Phase 3 (modified):** each operator runs:

```
for k in {0, 1}:                                          # K = 2
    commits_k = {σ_j^{IBE}(commit_tag_k) partials received}
    if |valid commits_k| ≥ qEnc = 3:
        decryption_key_k = aggregate(commits_k)
        sigs_k = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
              ∪ decrypted σ partials from operators who committed to layer k
        if |valid sigs_k| ≥ qV = 3:
            output (V_{L_k}, reconstruct(sigs_k)); halt

halt with no output                                       # no layer reached commit-quorum
```

By the safety pigeonhole, at most one layer reaches commit-quorum.

#### Why it's safe

Same algebra as the baseline "Why it's safe", with the σ-pool / NR-pool split replaced by per-layer commit pools:

- Honest commit to ≤ 1 layer per slot: `h_commit_0 + h_commit_1 ≤ 2f+1 = 3`.
- Each byz can commit to both layers (cross-commit): `byz_commit_0 + byz_commit_1 ≤ 2f = 2`.
- Both quorums reaching requires `h_commit_0 + h_commit_1 ≥ (2f+1) + (2f+1) − (byz_commit_0 + byz_commit_1) ≥ 4f+2 − 2f = 2f+2 = 4`. But `≤ 3`. Contradiction.

Bid lies don't enter this argument — the algebra is over commit *partials* on signed tags, not over what the bid was. A byzantine claiming a fake bid still occupies one commitment slot (no extra power); their lie influences which layer the cluster converges on, not whether two converge simultaneously.

#### Liveness & attribution

**Slot success conditions (under partial synchrony with `T_candidate_accept`):**

- All 3 honest validate `V_{L_0}` and `V_{L_1}`, agree on the bid order: all commit to the same layer, that layer reaches commit-quorum + σ-quorum, slot succeeds.
- All 3 honest validate only one of the two candidates: all commit to that one, slot succeeds at that layer.
- 2 honest validate one candidate, 1 honest validates the other: split — neither commit-quorum reaches without byzantine help. Byz cross-commit can fill in (giving 3 on whichever side they choose), so slot can still succeed if byz cooperates; if byz refuses to cooperate, slot misses (same liveness threshold as baseline).
- 0 honest validate any candidate: slot misses regardless of bid order.

**Bid lies as liveness faults:**

- Byzantine `L_0` claims `bid_0 = ∞` for an honest-looking but suboptimal `V_{L_0}`. All honest commit to layer 0 (highest bid). Slot completes with `V_{L_0}`. Cluster captured `actual_bid_0` (low) instead of `actual_bid_1` (potentially higher). **Loss = `actual_bid_1 − actual_bid_0`.** Attributable.
- Byzantine `L_0` claims `bid_0 = ∞` for an *invalid* `V_{L_0}` (parent stale, fork mismatch, etc.). All honest reject `V_{L_0}` at the validation step, fall back to `argmax` over remaining valid candidates → all commit to layer 1, slot completes with `V_{L_1}`. Same outcome as if `L_0` had been silent. Bid lie is wasted.
- Byzantine `L_0` lies low (claims `bid_0 = 0` for a high-MEV block). Operators commit to whichever layer has the higher claimed bid — `L_1` wins, slot completes with the lower-value block. Self-griefing for the byz; no one else loses except the cluster's value capture.

In every case the slot completes (or misses for reasons unrelated to the bid lie). No double-sig; no validator-key slashing.

**Post-hoc attribution.** Each Phase-1 envelope is a self-contained record `(slot, cluster, layer, leader_id, value_root, parent_root, bid, σ^op)`. Anyone — operator, watchdog, slasher — can after the slot:

1. Resolve `value_root` to the actual block (e.g., from the cluster's submission cache or the beacon chain).
2. Query the relay (or other ground truth) for the actual bid that block was offered at.
3. Compare `bid_claimed` (in the envelope) vs `bid_actual` (from the relay).
4. If they don't match within tolerance, the envelope + relay record is a slashable liveness-fault proof against `leader_id`.

The protocol layer doesn't need to do this in real time — it's an audit performed asynchronously. Out-of-band slashing or reputation penalty follows.

#### Trade-offs vs baseline TBFT

| Aspect | Baseline TBFT | Bid-ordered variant |
|---|---|---|
| Phase 1 envelope | `(version, cluster_id, slot, layer, leader_id, value_root, parent_root)` | + `bid` |
| Phase 2 onion | layer 0 plaintext σ + layer 1 IBE-encrypted σ + (separately) NR partials | one IBE-encrypted σ at chosen layer + commitment partial sig |
| Phase 2 tag(s) | 1 (`nr_tag_0`) | 2 (`commit_tag_0`, `commit_tag_1`) |
| Per-operator commitment | σ XOR NR per layer, multi-layer | one layer per slot, exclusive |
| IBE encryption per onion | 1 | 1 (for chosen layer only) — net same |
| Phase 3 walk | priority order, NR-quorum unlocks layer 1 | parallel commit-quorum lookup across both layers |
| Equivocation handling | Equivocation → NR for that layer | Equivocation → operator skips that layer, commits to the other |
| Liveness when L_0 unavailable | NR-walk required (same agreement threshold, extra step) | Direct commit to L_1 (no walk step) |
| Liveness when L_0 byz lies high | Slot completes with low-MEV block (cluster has no choice — fixed priority) | Slot completes with low-MEV block (same outcome; lie not detectable in real time), but attributable post-hoc and triggers slashing |
| Liveness when L_0 byz lies high + V_0 invalid | NR-walk to L_1 needed | Direct commit to L_1, no walk step |
| Bid attribution | n/a (no bid in protocol) | Self-contained envelope + relay record |
| Application contract | Application validity only | + bid concept, signed in envelope |

Bandwidth difference is small: the variant adds a bid field to the envelope (~32 bytes of bid + maybe a hash-of-bid in the value_root extension) and one more tag, but removes the separate NR-broadcast machinery for `nr_tag_0`. Net per-slot bandwidth at n=4 is roughly equal.

Latency: same single-window Phase 2; same K=2 walk in Phase 3. No additional rounds.

#### Open questions before this could be specified

- **Bid type and ordering.** Float64 has comparison-edge cases (NaN, ±0, denormals). uint256 wei is more natural for MEV but bigger. Pick a canonical type with stable total ordering and bound it (sanity ranges) at the application validation layer.
- **Bid binding inside the envelope.** Either `bid` is a primitive field in the envelope, or it's `H(bid)` to keep envelope size bounded with the actual bid carried alongside. The latter is more cache-friendly but adds a hash check.
- **Validity-check contract.** Application validation must reject bundles where the bid is "obviously wrong" (e.g., negative, exceeding any reasonable bound). Real-time validation can filter outright lies; subtle lies (claimed bid 1.5× actual) need post-hoc verification. Spec the boundary.
- **Tiebreaker formalization.** With float-or-bigint bids, exact equality is rare. With ordering ties, default to lower `leader_id` wins; document so all operators apply the same tiebreaker.
- **Post-hoc verification mechanism.** Who runs it (per-operator after each slot? a separate watcher? slasher operator?), where the evidence lives (gossiped fault-proof? on-chain registry?), and what the slashing trigger is (immediate or accumulated). Out of TBFT scope but needed for the "liveness fault attributed" promise to be real.
- **Relationship to equivocation.** A byz that signs two distinct envelopes for the same `(slot, layer)` with different bids is *both* an equivocator (slashable on the existing rule) and a bid-liar (slashable post-hoc). The two evidence types are independent; the protocol should accept either.

#### When to consider this

The straightforward case where this wins meaningfully is a production environment where:

- `L_0` failures (relay timeouts, missed MEV fetches) are non-rare, and the wasted NR-walk step in baseline TBFT measurably hurts slot timing.
- Or: byzantine bid-misrepresentation is observed (e.g., operators consistently overclaiming bids they don't deliver) and the slashing model wants attributable evidence for those events.

Without those signals, baseline TBFT's fixed-priority ordering is simpler and equally good — the bid-ordered variant adds spec surface and deployment complexity without producing more slots. If pursued, this is the natural place to start *before* reaching for the more general dynamic-ordering scheme in [TBFTR.md](TBFTR.md) Appendix B, since at K=2 the design space is much smaller and the "deterministic rule" has a clean application-level instantiation.

### B.2 — Why parent-root-based "ordering" doesn't extend TBFT well

A natural-sounding alternative to bidding is to let the cluster route based on which leader's candidate matches the *current head* — e.g., "commit to the layer whose `parent_root` is in my canonical chain; tiebreak by layer index." Captured here mostly to call out why this isn't a productive extension at TBFT's K=2.

**Parent-root is a validity *filter*, not an ordering *score*.** The check `parent_root ∈ my_local_canonical_chain` returns a boolean — a candidate is either valid against the operator's current head view or it isn't. There's no ranking comparator hiding inside it. To use parent-root as an "ordering" rule you'd combine the validity filter with some external tiebreaker (layer-index priority being the obvious one) — and once you do that you're back to "fixed priority among locally-valid layers," which is exactly what baseline TBFT already does (an operator that doesn't validate `V_{L_0}` falls through via NR_0; an operator that does validate it signs σ at layer 0). Wrapping that in a commit-tag structure adds machinery without adding routing flexibility.

**The input isn't cluster-consistent.** Each operator's `parent_root` validity check resolves against *their own* beacon-node view of the canonical chain. During an in-flight re-org, two honest operators on different heads can reach opposite verdicts on the same candidate — operator A says "valid against H1," operator B says "stale relative to H2." Same input, different outputs by definition. Bid-based routing avoids this: the bid is a signed claim in the envelope, byte-identical for every honest receiver, so `argmax(bid)` gives the same answer everywhere.

The split case is the head-divergence scenario from "Why it's safe" / "Liveness profile": with operators disagreeing on which candidate is valid against their current head, neither layer's commit-quorum can reach. Slot misses. (Safety still holds via the same commit-tag exclusivity machinery as B.1 — the issue is purely liveness.) Adding parent-root as a routing rule doesn't fix this; the rule's *output* fragments along exactly the same line as the underlying head disagreement.

**At K=2 specifically, there's no useful design space.** Two layers gives parent-root match at most two distinct outcomes (`{valid, valid}`, `{valid, invalid}`, `{invalid, valid}`, `{invalid, invalid}`). The first and last collapse to baseline behavior or unanimous miss; the middle two collapse to "commit to whichever is valid, fixed-priority tiebreaker." None of those produce a routing decision the baseline doesn't already make implicitly. At K ≥ 3 (TBFTR territory) there's at least the question of "skip L_0 because parent_root mismatches, go to L_1 vs L_2 by some rule" — but that's the general case, [TBFTR.md](TBFTR.md) Appendix B handles it.

**The right mitigation for head-divergence at TBFT is application-level**, not protocol-level: fetch `V_{L_1}` from a deeper-confirmed parent (a few slots back from the current head) so the backup's `parent_root` is structurally re-org-resistant. This is something the SSV runner can do without any protocol change — the asymmetric `T_1 < T_0` fetch times already accommodate fetching the backup well before the slot's most volatile period. It catches most of the same scenarios parent-root-based routing would address, with no spec growth.

**Summary.** Parent-root match doesn't give a useful new routing rule at K=2, fragments under the very condition it would purport to address (head disagreement), and is best handled at the application layer by choosing a more re-org-resistant parent for the backup candidate. It's worth understanding as a *non-extension* — a direction explored and ruled out — rather than a path forward.

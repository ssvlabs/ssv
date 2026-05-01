# TBFT — Threshold BFT for Single-Shot Deadline-Driven Agreement

This document describes **TBFT** (Threshold BFT), a single-shot agreement protocol for distributed clusters that produce one collective threshold-signed value per "slot" against a hard deadline. TBFT achieves agreement *cryptographically plus economic deterrence* rather than via multi-round message exchange, trading away classical liveness guarantees in exchange for a one-RTT decision path and built-in leader fallback.

The protocol description is generic. SSV's Ethereum proposer duty is used as the running example.

## When to use it (and when not to)

**Suited for:** single-shot duties with a fixed deadline, where missing the slot is the natural failure mode and where round-trip latency is the binding constraint. The leader priority order matters (the highest-priority responsive leader's value is preferred).

**Not suited for:** general-purpose state-machine replication, long-running agreement, situations where guaranteed termination across rounds is required, or where the bandwidth budget cannot absorb the `K · n²` constant factor (~3–5× a single QBFT round at typical settings).

## Setting

- A cluster of `n = 3f + 1` participants with byzantine bound `f`.
- Each participant holds shares of **two** threshold BLS keypairs, each established by an independent DKG run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = f+1`. Used (a) to sign no-quorum tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair.
- A leader-authentication signature scheme for candidate broadcasts. Practical choice: reuse each operator's long-term P2P/SSV identity key (already used for cluster networking); any per-operator scheme with cluster-wide pubkey distribution works. Distinct from the two threshold keypairs.
- For each slot, a **leader priority order** is deterministically derived (e.g. shuffling the participant set by `slot_seed`). Call it `(L_0, L_1, …, L_{n-1})`. `L_0` is the highest-priority leader. (0-based indexing throughout, matching the implementation.)
- A **fallback depth** `K` is configured per cluster, with `1 ≤ K ≤ n`. The protocol only attempts the top-`K` leaders. Recommended default: `K = max(3, f+1)`.
- A deadline `T_d` is fixed per slot. `T_d` is a *view-fix point*: each operator commits its stance based on what it observed by `T_d`. Reconstruction and submission happen after `T_d`.

The two thresholds are deliberately different. `qV = 2f+1` is the standard BFT quorum and is what makes a reconstructed `V` signature valid against the cluster's pubkey. `qEnc = f+1` is the layer-unlock threshold; the safety argument below shows why these can diverge without producing contradictory outputs (and what assumption that depends on).

## Protocol

### Phase 1 — Candidate broadcast `[T_d − Δ_1, T_d]`

Each leader `L_k` for `k ∈ {0, …, K−1}`:

1. Independently produces its candidate value `V_{L_k}` (e.g. fetches a block from a beacon node) and validates it against application-level rules (see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction of the validator output signature on `V_{L_k}`.
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(V_{L_k})`. This proves the candidate originated with `L_k` (rejects forgery).
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(V_{L_k}))` to peers.

Before accepting a candidate, every receiver:

- Verifies `σ_{L_k}^{op}(V_{L_k})` against `L_k`'s known operator pubkey for this layer in this slot. Unverified bundles are silently dropped.
- Verifies `σ_{L_k}^V(V_{L_k})` against `L_k`'s known V-share pubkey. Unverified bundles are silently dropped.
- Validates `V_{L_k}` against application-level rules. Invalid bundles are silently dropped.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with the f+1 honest threshold partials produced in Phase 2 by operators who received V_{L_k}, the cluster reaches `qV = 2f+1` real partials on V_{L_k} *exactly* at n=4 (f=1) — closing the byzantine-leader selective-delivery grief at this cluster size (caveat 1). At larger n the head start narrows the grief window by one but doesn't close it; see caveat 1 for the algebra.

A leader who broadcasts `(V, σ^{op})` without `σ^V`, or with garbage in its place, is treated as not having broadcast at all. There is no incentive for an honest leader to omit `σ^V`, and a byzantine leader withholding it just causes the cluster to fall through to NR-quorum at qEnc = f+1 (which the missing-V honest can reach on their own).

#### Equivocation handling

If a participant observes two distinct candidates `V_{L_k}` and `V'_{L_k}`, both validly signed by `L_k`, at the same layer `k`:

1. **Locally treat layer `k` as non-receipt**: do *not* include a positive partial signature for layer `k` in the onion (Phase 2); broadcast a non-receipt attestation on `nr_tag_k` instead.
2. The pair `(V_{L_k}, σ_{L_k}^{op}(V_{L_k}))` and `(V'_{L_k}, σ_{L_k}^{op}(V'_{L_k}))` is a self-contained slashable fault proof against `L_k`. Operators may broadcast it to ensure cluster-wide attribution.

This rule converts leader equivocation into a clean fall-through to layer `k+1`, with cryptographic blame attached.

By `T_d`, each participant has 0..K validly-signed candidates from the designated leaders. Layers where the leader didn't broadcast, broadcast something invalid, or equivocated are treated as null at the corresponding layer position.

### Phase 2 — Layered onion broadcast `[T_d, T_d + Δ_2]`

Each participant `i` constructs a `K`-layer onion, one slot per leader in the top-`K` priority set:

```
layer k:  E_{enc_tag_k}( σ_i^V( V_{L_k} ) )
```

where:

- `σ_i^V(x)` is `i`'s partial signature on value `x` using the V-signing share (threshold `qV = 2f+1`).
- `E_{enc_tag}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = f+1`); a ciphertext under tag `T` decrypts iff `qEnc` partial sigs on `T` from the IBE keypair exist.
- `enc_tag_0 = ⊥` (layer 0 is plaintext — the highest-priority layer is always openable).
- `enc_tag_k = nr_tag_{k-1}` for `k ≥ 1` (layer `k`'s ciphertext is locked under the previous layer's no-quorum tag).
- `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` — the tag honest operators sign when they didn't successfully observe `V_{L_k}` at layer `k`. Cluster-id `C` and slot `N` give domain separation across slots and clusters.

For layers where `i` doesn't hold a valid `V_{L_k}` (or observed `L_k` equivocate), `i` omits the encrypted partial in that layer slot and instead broadcasts a **non-receipt attestation**: a partial signature `σ_i^{IBE}(nr_tag_k)` from the IBE keypair. These attestations are the witnesses that unlock subsequent layers' IBE ciphertexts.

`i` gossips the onion together with any non-receipt attestations.

### Phase 3 — Local decryption and reconstruction `[T_d + Δ_2, finalize]`

Each participant has now received 0..n onions and a set of non-receipt attestations from peers. Starting at layer 0:

```
loop k = 0..K-1:
    sigs = {σ_{L_k}^V(V_{L_k}) from Phase 1, if received and valid}
        ∪ aggregate σ_j^V(V_{L_k}) from received onions at layer k
           (only counted when enc_tag_k is unlocked; trivially unlocked at k=0;
            the leader's Phase-1 σ^V is plaintext and counts at every layer)
    if |valid sigs on a single V_{L_k}| ≥ qV:
        S = reconstruct full V signature on V_{L_k}
        output (V_{L_k}, S); halt
    else:
        nrs = aggregate σ_j^{IBE}(nr_tag_k) partials
        if |valid nrs| ≥ qEnc:
            decryption_key = aggregate(nrs)         # threshold sig on nr_tag_k
            unlock layer (k+1) ciphertexts          # enc_tag_{k+1} = nr_tag_k
            continue
        else:
            halt with no output                     # missed slot
halt with no output                                 # exhausted top-K, no positive quorum
```

The leader's Phase-1 partial appears unencrypted in the σ pool at every layer. At layer `k > 0` this means one partial is visible early, before `enc_tag_k` is unlocked — but one partial alone can't reconstruct (need `qV`), and the remaining partials stay encrypted until the lower layer's NR-quorum unlocks them. So the IBE-gating property is preserved.

Once a participant produces an output `(V, S)`, it submits to the downstream system (the beacon node, in the SSV example). Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at all: no positive partial signature at any layer, no non-receipt attestation. Standard threshold cryptography — only signed messages count, missing operators contribute nothing.

Implication: liveness is bounded by the standard `3f+1` byzantine assumption. If more than `f` operators are offline (or byzantine, combined), neither a positive nor a non-receipt quorum will reach its threshold and the slot is missed — exactly the failure mode the trust model already assumes.

#### Why not "absent = ALL-value"

An earlier design (the original Proposal 3) treated missing onions as if the absent operator had signed positively at every layer, combined with a lowered ~`f+1` NR-quorum threshold. The combined rule is unsafe under standard threshold semantics (both quorums simultaneously reachable at `f ≥ 1`) and cryptographically infeasible (phantom partial sigs from absent operators don't exist as a usable IBE decryption witness). Threshold separation in this spec — `qV = 2f+1` σ + `qEnc = f+1` NR via a separate DKG — is the safe way to lower the NR threshold without invoking phantom signatures. See "Why it's safe" for the algebra; the σ+NR exclusion rule at aggregation is what keeps both quorums mutually exclusive at the asymmetric thresholds.

## Preconditions on the host application

TBFT itself guarantees safety (no two contradictory outputs cluster-wide, given the slashing-deterrence assumption below) and provides best-effort liveness. **Validity** — that the output `V` is application-valid — is a precondition the host application must enforce.

Each honest operator must validate `V_{L_k}` against application-specific rules **before** including a positive partial signature `σ_i^V(V_{L_k})` in their onion at layer `k`. If even one honest operator skips validation, the cluster may produce a fully-signed `V` that doesn't satisfy the application's invariants — the cryptographic safety property is on uniqueness of the output, not on its application-level correctness.

For SSV's Ethereum proposer duty, application-level checks include:

- Slot match (`block.slot == cluster_slot`).
- Proposer index match.
- Fork/domain match (current fork version, expected domain).
- Parent root: matches the operator's view of the head.
- Relay metadata: bid claim, builder pubkey, value validity (against the cluster's relay allow-list).
- Doppelganger and slashing-protection checks: not signing for a slot already signed at.
- Block encoding: well-formed SSZ, reasonable size.

Slashing protection should gate **candidate signing** (Phase 2 onion construction), not just submission. Each operator's V-signing share signs `K` distinct values per slot inside the onion (one per layer); the cluster's safety property collapses these to a single output, but the per-share signing log shows `K` distinct block sigs at the same slot. EKM must handle this without flagging it as a violation, while still preventing duplicate signing across slot boundaries.

## Why it's safe

**Safety claim**: at most one full `V` signature is ever produced per TBFT instance per slot.

**The σ+NR exclusion rule at aggregation.** Every operator running Phase 3 builds two pools at each layer `k`: σ partials at layer `k`, and NR partials on `nr_tag_k`. Any operator whose published messages contain *both* a σ partial at layer `k` (in their onion) AND an NR attestation on `nr_tag_k` (broadcast separately) has both contributions excluded from their respective pools at this layer. This is a structural exclusion — it happens at aggregation time regardless of byzantine intent or economic incentives.

**The safety pigeonhole at any layer `k`** under this rule: σ-quorum on `V_{L_k}` and NR-quorum on `nr_tag_k` cannot both be reached.

Algebra. Let `h_σ`, `h_NR` be honest operators contributing σ on V and NR on nr_tag respectively at layer `k`; `byz_σ_alone`, `byz_NR_alone` the byzantine operators contributing to *exactly one* side (cross-signers contribute to neither side under the exclusion rule).

- σ-quorum reached: `h_σ + byz_σ_alone ≥ qV = 2f+1` ⇒ `h_σ ≥ 2f+1 − byz_σ_alone`.
- NR-quorum reached: `h_NR + byz_NR_alone ≥ qEnc = f+1` ⇒ `h_NR ≥ f+1 − byz_NR_alone`.
- Each byzantine operator contributes to at most one side after exclusion: `byz_σ_alone + byz_NR_alone ≤ f`.
- Sum: `h_σ + h_NR ≥ 3f+2 − (byz_σ_alone + byz_NR_alone) ≥ 3f+2 − f = 2f+2`.
- Honest don't sign both: `h_σ + h_NR ≤ 2f+1`.
- Contradiction: `2f+2 ≤ 2f+1` is impossible.

Therefore the layer at which the cluster first reaches σ-quorum is uniquely determined cluster-wide, and at most one full `V` signature ever materializes. Two participants cannot independently reconstruct two contradictory outputs. **Byzantine σ+NR cross-signing has at worst a *liveness* impact** — their excluded contributions might prevent a quorum from reaching at this layer — **never a safety impact**: they cannot produce two outputs by attempting it.

This is a different shape of safety than QBFT. QBFT enforces safety via *agreement* (all honest operators decide the same value at decision time). TBFT enforces it via *cryptography*: aggregator-level filtering of σ+NR cross-signers makes the same-layer quorum exclusion algebraic, so the math precludes contradictory outputs regardless of byzantine behavior.

The σ+NR slashing rule (caveat 2) remains useful for *attribution and punishment* of byzantine cross-signers — collecting slashable evidence, monitoring, reputation. It is **not load-bearing for safety**. The same is true of the path-conditional detection limit at deep layers (also caveat 2): undetected deep-layer cross-signers escape attribution but cannot break safety, because the exclusion rule applies wherever aggregation actually happens (at opened layers) and at unopened layers no aggregation occurs to be subverted.

## Liveness profile

TBFT does **not** guarantee termination. If the network is bad enough that no σ-quorum and no NR-quorum reach their thresholds at any layer up to `K`, no output is produced and the slot is missed. There is no "round 2" — TBFT is single-shot by design.

This is a deliberate tradeoff. For deadline-driven duties where missing a slot is the natural failure mode (you'll get another slot later), this matches the problem.

**What threshold separation buys.** Lowering the unlock threshold from `2f+1` to `qEnc = f+1` lets the protocol fall through to the next layer in degraded-network scenarios that the symmetric design (`qEnc = qV = 2f+1`) would get stuck on. Let `x` = number of honest operators that didn't receive `V_{L_k}` by `T_d` at layer `k` (gossip lossiness, partial partition, slow leader fetch — *not* a worst-case selective-delivery attack). Those `x` honest sign NR; the remaining `2f+1 − x` honest sign σ. With byzantine refusing both sides (worst case for liveness):

- σ-quorum at `qV = 2f+1` real partials: reachable iff `x = 0`.
- NR-quorum at `qEnc = f+1`: reachable iff `x ≥ f+1` ⇒ layer falls through; the slot can succeed at layer `k+1` if its leader is honest.
- NR-quorum at the symmetric `qEnc = qV = 2f+1`: reachable iff `x ≥ 2f+1` — impossible since there are only `2f+1` honest total ⇒ slot stuck for any `x ≥ 1`.

So the separation saves all moderate-degradation slots in the range `x ∈ [f+1, 2f]`. For `n = 7`, `f = 2`: that's `x ∈ {3, 4}`. For `n = 13`, `f = 4`: `x ∈ {5, 6, 7, 8}`. Without it, the symmetric `qEnc = 2f+1` design effectively gives no fall-through unless every single honest operator missed `V` — which means in any partial-failure scenario with a mix of σ-signers and NR-signers, the slot just gets stuck on whatever layer the partial failure happened on, with no recourse short of waiting for the slot to expire.

The boundary case `x = f` exactly — a byzantine leader selectively splitting honest at the worst possible point (caveat 1's selective-delivery attack) — is **not** saved by this threshold change alone; that's the gap TBFTR's deferred-NR composition closes.

**What it costs.** A second DKG, run once at cluster init for the IBE keypair at threshold `qEnc = f+1` (caveat 5). And the σ+NR exclusion rule at aggregation must be in place — under threshold separation it's what keeps the same-layer σ-quorum and NR-quorum mutually exclusive (see "Why it's safe"). At `qEnc = qV = 2f+1` the exclusion rule isn't strictly needed for safety (algebra holds without it), but at `qEnc = f+1` it is — and it's a small, structural addition (no extra messages or trust assumptions). Per-slot bandwidth and latency are unchanged.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023. Uses threshold BLS as the decryption oracle; the tag is conventionally a round number, but the construction is content-agnostic.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain and is integrating with Ethereum PBS.

A TBFT implementation can integrate `drand/tlock`-style ciphertext construction directly. The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = f+1`, run once at cluster init alongside (or after) the V-keypair DKG. Long-lived, no per-slot rotation needed.

## Properties summary

| Property | TBFT |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic, structural via σ+NR exclusion at aggregation |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition |
| Termination (output guaranteed) | **No**, single-shot |
| Equivocation detection | Yes — leaders sign candidates; conflicting signed candidates trigger non-receipt at that layer; pair forms slashable evidence |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (the layered structure) |
| Round-change recovery | No |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block:

| TBFT concept | SSV mapping |
|---|---|
| `n` participants | cluster size (4, 7, 10, 13) |
| Slot | Ethereum slot for which the cluster is proposer |
| Candidate `V_{L_k}` | block fetched independently from operator `L_k`'s beacon/relay |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| Leader priority `(L_0, …, L_{n-1})` | reuse QBFT-style leader rotation order |
| Fallback depth `K` | `max(3, f+1)` per cluster: 3 for n=4 and n=7, 4 for n=10, 5 for n=13 |
| Output | full validator-signed Ethereum block |
| `T_d` | derived from the relay 4s cutoff — e.g. `T_d ≈ slot_start + 3s` |
| `Δ_1` | block-fetch window (~1s) |
| `Δ_2` | onion-gossip window (~500ms) |

**Phase 1** (`slot_start + 2s` to `slot_start + 3s`): the top-`K` leaders each request a block from their beacon node, sign the blinded block with their operator key, and gossip `(block, leader_sig)` to peers.

**Phase 2** (`slot_start + 3s` to `slot_start + 3.5s`): each operator builds a `K`-layer onion of partial validator-signature shares (V-keypair) and gossips it, alongside non-receipt attestations (IBE-keypair) for any layer where they don't have a valid candidate.

**Phase 3** (`slot_start + 3.5s` onwards): each operator locally decrypts; first to reconstruct submits the full block to the beacon network. Cryptography plus σ/NR slashing ensure only one block can ever get a valid validator signature.

## Comparison vs QBFT for SSV cluster sizes

The bandwidth and round-trip comparison is unchanged from earlier specs of TBFT — the threshold-separation refinement adds a one-time DKG for the IBE keypair at cluster init but doesn't change per-slot bandwidth. See [TBFT-comparison.md](TBFT-comparison.md) for scenario-by-scenario detail.

Headline: TBFT is 1 RTT vs QBFT's 3 RTTs in the common case, at the cost of `O(K · n²)` constant-factor bandwidth and a second DKG at cluster init.

## Practical caveats and open questions

1. **Deterministic byzantine-leader grief on selective delivery — closed at n=4, residual at larger n.** A byzantine `L_k` may attempt to miss the slot at its own layer by selectively delivering `V_{L_k}` to a subset of honest operators just before the deadline and refusing to vote in Phase 2. The Phase-1 `σ_{L_k}^V` head start (above) closes this grief at `n = 4` and narrows it to a small residual window at larger `n`.

   **Algebra.** A byzantine layer-`k` leader delivers the bundle `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})` to exactly `k` honest operators, withholds from the remaining `2f+1 − k` honest, then refuses to vote in Phase 2 (no additional σ contribution beyond the Phase-1 partial they were forced to publish; no NR contribution).

   - Real σ-side count on `V_{L_k}`: `k + 1` (the `k` honest who publish `σ^V` in Phase 2 onions plus the leader's `σ^V` from Phase 1).
   - Real NR-side count on `nr_tag_k`: `2f + 1 − k` (the honest who didn't receive `V_{L_k}`).
   - σ-quorum unreachable: `k + 1 < qV = 2f+1` ⇒ `k ≤ 2f − 1`.
   - NR-quorum unreachable: `(2f+1 − k) < qEnc = f+1` ⇒ `k ≥ f + 1`.
   - Grief window: `k ∈ [f+1, 2f−1]`.

   | n | f | grief window | size |
   |---|---|---|---|
   | 4 | 1 | empty | 0 ✓ |
   | 7 | 2 | {3} | 1 |
   | 10 | 3 | {4, 5} | 2 |
   | 13 | 4 | {5, 6, 7} | 3 |

   **At n=4** the leader's Phase-1 `σ^V` plus the `f+1 = 2` honest partials sum to exactly `qV = 3`. Byzantine has no `k` value that produces grief: deliver to 0–1 honest and NR-quorum reaches qEnc=2; deliver to 2+ honest and σ-quorum reaches qV=3. **n=4 P0.1 closed; TBFT2 P0.2 closed by the same algebra.**

   **At larger n** a residual grief window remains (size `f − 1`). At n=7 it's a single point (`k = 3`); at n=13 it's three points. The byzantine attacker must time delivery to land precisely in the window, which constrains the attack significantly but does not eliminate it.

   Framing implications for the residual:

   - `K = max(3, f+1)` does **not** save the slot when a byzantine leader griefs its own layer at one of the residual `k` values. It guarantees an honest *successor* leader exists in the top-`K`, but the byzantine layer's grief still blocks fall-through.
   - The deadline-tuning condition still matters: `T_d − T_arrival > D + δ` with `D` the propagation **P99 (or P999)** of the cluster's mesh. Tighter deadlines shrink the byzantine attacker's timing window for hitting the residual `k`.

   Mitigations for the residual:

   - **Tighter deadline** (above) is the defense that lives entirely inside this spec.
   - **TBFTR + deferred non-receipt** ([TBFTR.md](TBFTR.md)) is the protocol-level fix at all cluster sizes — it gets *real* late σ partials from honest operators that recover `V` via TBFTR's plaintext channel, closing the residual `[f+1, 2f-1]` window for n ≥ 7. Out of scope for this spec; see TBFTR.md.

2. **Inconsistency-slashing — three rules.**

   These rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the σ+NR exclusion rule (see "Why it's safe"), they are **not load-bearing for safety** — aggregator-level filtering of cross-signers makes the σ-quorum / NR-quorum mutual exclusion algebraic regardless of byzantine behavior. The slashing rules are about consequences, not safety enforcement.

   - **Self-contradiction (σ + NR at same layer).** If operator `i`'s onion contains `σ_i^V(V_{L_k})` *and* `i` broadcasts `σ_i^{IBE}(nr_tag_k)`, that's a slashable contradiction. The same publicly-signed pair is what triggers the σ+NR exclusion rule at aggregation time: aggregators exclude `i`'s contributions from both pools at layer `k`.

   - **Leader equivocation.** Two distinct, validly-signed candidates `(V_{L_k}, σ_{L_k}^{op}(V_{L_k}))` and `(V'_{L_k}, σ_{L_k}^{op}(V'_{L_k}))` from the same `L_k` at the same layer are a self-contained slashable fault proof against `L_k`. Honest operators that observe both treat layer `k` as non-receipt locally (Phase 1 equivocation handling) and may broadcast the pair as a fault claim.

   - **Cross-onion partial-sig equivocation by an operator.** If operator `i` appears in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different `V`, that's detectable from the partial sigs alone (two distinct partials from the same identity at the same `(slot, layer)`). Slashable on the same logic as the σ + NR case. The aggregator at layer `k` already groups partials by value; same-identity contributions to two different value groups surface this — and the cross-signer's contributions are excluded from all groups at this layer (analogous to σ+NR exclusion).

   **Path-conditional detection limit.** The σ+NR detector requires observing `σ_i^V` at the relevant layer. At deep layers (those whose ciphertext only opens when an upper layer fails σ-quorum), if the upper layer succeeds, the deep layer's σ partials are never decrypted and an operator's σ+NR contradiction at that depth goes undetected *for attribution purposes*. This doesn't affect safety: the σ+NR exclusion rule applies wherever aggregation actually happens (at opened layers), and at unopened layers no aggregation occurs to be subverted. The mitigation question is purely about how aggressive we want attribution to be — post-protocol gossip of all-layer σ partials would catch deep-layer cross-signers retroactively for slashing, at a wire-format and bandwidth cost. Engineering choice.

3. **Bandwidth scales with `K`.** With the recommended cap `K = max(3, f+1)`, bandwidth is `O(K · n²)`, viable for all current SSV cluster sizes (n=4, 7, 10, 13). Larger clusters or higher byzantine bounds would require larger `K`, increasing constant-factor bandwidth proportionally.

4. **No prior-art DVT implementation.** Threshold IBE itself is deployed (Drand, Shutter), but the full TBFT protocol with the layered-onion + dual-DKG + leader-authentication + non-receipt-driven fall-through structure appears unbuilt. Engineering risk and audit cost are substantial.

5. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The IBE DKG is a one-time setup cost on top of the existing V-keypair DKG.

6. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. The deadline rule is `T_d − T_arrival > D + δ` where `D` is the propagation P99/P999. This is a *liveness* requirement — under unbounded skew some operators miss the deadline and the slot fails to finalize. Safety is unaffected: the σ+NR pigeonhole is a global property over cluster-wide signed messages, not over per-operator views.

7. **Tag construction and replay.** The `nr_tag_k` tags must uniquely bind `(slot, cluster, layer)` so that ciphertexts from one slot/cluster/layer cannot be replayed/reused. The structure `("slot", N, "cluster", C, "layer", k, "no-quorum")` provides this. Standard hygiene but easy to get wrong.

8. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one TBFT instance and assumes:

   - Single TBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between TBFT and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase 2 onion), not just submission.

   A host that runs a parallel signing path loses this guarantee unless slashing protection gates both paths and they share a domain-separating tag. Each operator's V-share signs `K` distinct values per slot inside the onion (one per layer); this is expected and the per-instance safety property collapses these to a single output cluster-wide.

## Where this came from

This protocol corresponds to "Proposal 3" in the SSV discussion at [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829), with subsequent refinements:

- Leader-authenticated candidates and the equivocation-to-non-receipt rule, addressing audit findings on candidate authenticity and equivocation detection.
- Threshold separation (`qV = 2f+1` for V, `qEnc = f+1` for IBE) via a separate DKG, capturing Proposal 3's degraded-network-liveness intuition without phantom signatures.
- 0-based tag indexing with distinct `enc_tag_k` and `nr_tag_k` symbols (matching the implementation).
- Explicit application-validity preconditions and per-instance scoping of the "at most one signature" claim.
- Cross-onion partial-sig equivocation added to the inconsistency-fault detector.

The cryptographic primitive is the same one underlying tlock and Shutter. See [TBFTR.md](TBFTR.md) for the in-progress companion design that addresses the deterministic byzantine-leader grief in caveat 1.

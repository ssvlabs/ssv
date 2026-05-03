# TBFT — Threshold BFT

A single-shot agreement protocol for SSV's `n = 4` (`f = 1`) clusters that produces one collective threshold-signed value per "slot" against a hard deadline. TBFT achieves agreement *cryptographically* — single-RTT decision path, structural cryptographic safety, primary/backup leader fallback.

TBFT is the lean specialization for `n = 4`. The full multi-layer spec — applicable at any cluster size, including `n = 4` — lives in [TBFTR](TBFTR.md); TBFT drops TBFTR's V-plaintext + Phase-2-split machinery in exchange for less bandwidth and less protocol surface. At `n = 4` (`f = 1`), the two protocols **cover the same marginal-synchrony band** ("≤ 1 of 3 honest missing re-flood") — TBFTR's witness threshold (`f+1 = 2` distinct Phase-2a σ-signers) bounds the secondary closure to the same band TBFT's leader-σ head-start already covers. TBFTR adds redundancy in that band but doesn't extend it. The trade is therefore minimal protocol complexity (TBFT) vs marginal extra redundancy (TBFTR-at-n=4) — see "Fault tolerance" and **Appendix A** for the side-by-side. For larger cluster sizes (7, 10, ...), TBFTR's secondary closure does extend the band (the widening is `f − 1`, so non-zero at `f ≥ 2`), and only TBFTR is BFT-safe.

The protocol description below is specific to TBFT. SSV's Ethereum proposer duty is used as the running example.

## When to use it

**Suited for:** small clusters, single-shot duties with a fixed deadline, where missing the slot is the natural failure mode and round-trip latency is the binding constraint. The application has a natural primary/backup separation (high-MEV block vs. safe early-fetched block).

**Not suited for:** larger cluster sizes (use [TBFTR](TBFTR.md)), general-purpose state-machine replication, situations where guaranteed termination across rounds is required.

## Setting

- A cluster of 4 participants with byzantine bound `f = 1`. So `2f+1 = 3` honest are guaranteed.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1 = 3`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs.
  - **IBE keypair** at threshold `qEnc = 2f+1 = 3`. Used (a) to sign the no-quorum tag and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety".
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- Two **layers** with deterministically-derived leaders: layer 0 with **primary leader** `L_0` and layer 1 with **backup leader** `L_1`, required to be distinct.
- Two leader-fetch deadlines, `T_1 < T_0`, plus a final cluster deadline `T_commit`. (`T_commit` is a *view-fix point*: each operator commits its stance based on what it observed by `T_commit`. Reconstruction and submission happen after `T_commit`.) The asymmetric fetch times let the primary fetch a high-MEV value late (`T_0` close to `T_commit`) while the backup fetches a safe early value (`T_1` well before `T_0`).
- A **candidate acceptance cutoff** `T_candidate_accept = T_commit − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers drop any candidate whose first-observation time is later than `T_candidate_accept` — treated locally as not-received. The cutoff bounds late acceptance: byzantine cannot first-deliver to any honest after their `T_candidate_accept` without all of them rejecting. It does **not** by itself guarantee that a re-flood from an honest receiver at exactly the cutoff reaches every other honest before *their* cutoffs in the worst-case clock-skew scenario — under that byzantine-leader-at-cutoff edge, only `f+1` honest sign Phase-2 σ on time. **TBFT at `n = 4` still closes the slot in this edge** because `f+1 = 2` honest σ + 1 leader Phase-1 σ = `f+2 = qV = 3` exactly; the cluster reaches σ-quorum without needing all honest to accept before their cutoffs (see "Fault tolerance / Liveness" and "Phase 1 / Bundle propagation"). This is also the structural reason TBFT-shape doesn't extend to `f ≥ 2` (where `f+2 < qV`), and why TBFTR introduces secondary closure at larger cluster sizes.
- A single tag `nr_tag_0 = ("slot", N, "cluster", C, "layer", 0, "no-quorum")`. (Only one tag is needed because there's only one transition — primary→backup — that requires unlocking.)

## Protocol

### Phase 1 — Candidate broadcast

Phase 1 has two per-layer windows (driven by the asymmetric fetch times): `[T_1, T_1 + Δ_1]` for the backup, then `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, 1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV = 3` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "TBFT-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates TBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, other SSV protocols including TBFTR running on the same operator key, future TBFT message kinds, etc.) — the operator-identity key is shared with these other uses, and without an explicit protocol/kind tag a TBFT envelope encoding could collide with another protocol's signed-payload encoding. The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp against `T_candidate_accept` (drop and treat as not-received if later). Bundles failing any of these are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV" below).

If `L_1` fails to broadcast, the backup path is unavailable for this slot. If `L_0` fails, only the backup path remains. If both fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood the bundle via standard gossipsub. The **candidate acceptance cutoff** bounds the byzantine leader's late-release window: a bundle first observed after `T_candidate_accept` is rejected (TBFT has no late-retention), so byzantine cannot deliver bundles for the first time after the cutoff to fragment the cluster.

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed before `T_candidate_accept`, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed (also before `T_candidate_accept`, since TBFT rejects late bundles outright), that one (sufficient for both Phase-2 σ-signing on the chosen V *and* leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Bundles first observed after `T_candidate_accept` are rejected entirely (no auth-only retention path, unlike TBFTR). Retention lifetime: until the operator's local end of Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. This caps memory at `O(K · n) = O(2 · 4) = O(8)` bundles per slot in the worst case (every leader equivocates), well under any practical pressure budget.

A subtlety in the clock-skew arithmetic: the cutoff `T_candidate_accept = T_commit − (D + δ)` is enough to bound *late* delivery (byzantine cannot first-deliver to any honest after their `T_candidate_accept` without all of them rejecting), but it does **not** by itself guarantee that a re-flood from an honest receiver who accepted *exactly at* `T_candidate_accept` (in their own clock) reaches all *other* honest receivers before *their* `T_candidate_accept` in the worst-case clock-skew scenario — the re-flood still takes `D` to propagate, and a slow-clock peer's cutoff (in absolute time) can be earlier than the fast-clock acceptor's. Under this byzantine-leader-at-cutoff edge, the f peers receiving the re-flood late count it as not-received (TBFT has no auth-only retention), so σ pool = `f+1` honest Phase-2 σ + 1 leader Phase-1 σ = `f+2`. At `f = 1` (n=4) this equals `qV = 3` and the slot succeeds via the leader-σ head-start; **TBFT's primary closure works precisely because `f+2 = qV` at `f = 1`**, not because re-flooding strictly reaches all honest before their cutoffs. (At `f ≥ 2` the same edge would leave `f+2 < qV`, which is why TBFTR's secondary closure exists — see [TBFTR.md](TBFTR.md).)

The cutoff could be tightened (e.g., to `T_commit − (2D + δ)` to give re-flood a full propagation budget) at the cost of shrinking the leader's fetch window — see "Practical caveats / Deadline coordination" for the trade-off; the docs as written use the looser `T_commit − (D + δ)` cutoff and rely on the leader-σ head-start exactly closing `qV` at `f = 1`. This is the same partial-synchrony envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)), made operational by a concrete cutoff.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with the two honest threshold partials produced in Phase 2 by operators who received the leader's bundle (or recovered it via gossipsub re-flooding), the cluster reaches `qV = 3` real partials on `V_{L_k}` exactly — closing the byzantine-leader selective-delivery grief at this cluster size under partial synchrony (see "Fault tolerance / Liveness").

**Equivocation handling.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation: locally treat the layer as non-receipt (don't include the corresponding partial signature in the onion; broadcast a non-σ attestation on `nr_tag_0` for layer-0 equivocation, or omit layer 1 of the onion for layer-1 equivocation). The pair of signed bundles is self-contained slashable evidence — see "Fault tolerance / Equivocation handling" for the analysis. The leader is required to sign σ_V exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second σ_V from the same leader is a protocol violation regardless of intent.

**Operator commitments — σ, NR, NV.** For each layer, an operator's commitment falls into one of three buckets:

- **σ (sign-on-V)**: the operator received the leader's bundle on time, both protocol-level and application-level checks passed. Materializes as a σ partial in the Phase-2 onion (or as the leader's Phase-1 σ for the layer's own leader).
- **NR (non-receipt)**: the operator did not receive the leader's bundle by their Phase-2 cutoff. Includes "received but BFT auth failed" (silently dropped → equivalent to not-received) and "first-observed after `T_candidate_accept`".
- **NV (non-validity)**: the operator received the bundle on time with valid BFT auth, but the host application returned `not valid` for `V_{L_k}` — so the operator cannot sign σ on it.

**NR and NV are operationally interchangeable for the protocol.** Both materialize on the wire as a single message kind: a partial `σ_i^{IBE}(nr_tag_0)` from the IBE keypair (TBFT only emits no-σ attestations at layer 0; layer 1 has no NR tag). The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to throughout as "NR-quorum" or "no-σ quorum" for short — both mean the union of NR and NV partials). The distinction between NR and NV is **local-only diagnostic** (an operator may log *why* it didn't sign σ — for telemetry — but the cluster-wide message and counting logic are identical). All references to "NR" in the rest of this document encompass both NR and NV unless stated otherwise.

### Phase 2 — Onion broadcast `[T_commit, T_commit + Δ_2]`

Phase 2 is a **single window** — no 2a/2b split, since the leader-σ + gossipsub re-flooding mechanism already closes the byzantine-leader grief under partial synchrony at this cluster size. Each participant `i` constructs a 2-layer onion:

```
layer 0:  σ_i^V(V_{L_0})                                # primary, plaintext
layer 1:  E_{nr_tag_0}( σ_i^V(V_{L_1}) )                # backup, IBE-encrypted
```

where:

- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share (threshold `qV = 3`).
- `E_{nr_tag_0}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc = 3`); a ciphertext under tag `nr_tag_0` decrypts iff `qEnc` partial sigs on `nr_tag_0` from the IBE keypair exist.

If `i` did not receive a valid `V_{L_0}` (after the gossipsub re-flooding window) — either NR (didn't receive in time) or NV (received with valid auth but host application returned `not-valid`) — it omits layer 0 and broadcasts a **no-σ attestation**: a partial signature `σ_i^{IBE}(nr_tag_0)` from the IBE keypair. These attestations are the witnesses that unlock layer 1. NR and NV broadcasts are wire-identical and counted together (see "Operator commitments — σ, NR, NV" above).

If `i` did not receive a valid `V_{L_1}` either, it omits layer 1 entirely.

`i` gossips its onion together with any no-σ attestation.

**Per-operator commitment for layer 0 is exclusive across phases.** TBFT has only one NR tag (`nr_tag_0`) — layer 1 has no NR side, so the σ-or-no-σ exclusivity rule applies at layer 0 only. The commitment is *one decision per operator, spanning Phase 1 and Phase 2*:

- An operator who included `σ_i^V(V_{L_0})` in their Phase-2 onion at layer 0 has σ-side committed; they may **not** also broadcast an NR/NV partial on `nr_tag_0`.
- The layer-0 **leader** signed `σ_{L_0}^V` in Phase 1; that is their σ-side commitment for layer 0. At Phase 2 they include σ on `V_{L_0}` in their own onion uniformly with any other σ-committed operator — Phase 3 dedup collapses Phase-1 σ + Phase-2 onion σ from the same operator to one partial. They **cannot emit NR/NV on `nr_tag_0`** even if the host application's verdict on `V_{L_0}` would have changed by Phase 2.
- Layer 1 has no commitment exclusivity to enforce: an operator may include `σ_i^V(V_{L_1})` in their Phase-2 onion at layer 1 (when host returns `valid` for V_{L_1}) regardless of whether they signed σ_0 or NR_0/NV_0 — these are independent commitments to two different values, both bounded by Pigeonhole 2 at their own layer.

A byzantine operator that publishes both σ_0 and NR/NV_0 is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For the layer-0 leader specifically, "cross-signing" includes any Phase-1 σ + Phase-2 NR/NV pair on the same slot; the rule applies uniformly across phases.

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

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers as a fourth wire-envelope kind: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at all: no positive partial signature, no non-receipt attestation. Standard threshold cryptography — only signed messages count.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within `T_commit + Δ_2` (see "Fault tolerance / Liveness"). If more than `f = 1` operators are offline or byzantine combined, neither quorum will reach its threshold and the slot is missed.

## Preconditions on the host application

TBFT is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 2 / Operator commitments — σ, NR, NV").

The protocol-level checks (cryptographic auth, envelope re-derivation, timing cutoff `T_candidate_accept`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot, but two stricter constraints apply per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-no-σ exclusively per (slot, layer), across phases.** Honest who include σ in their Phase-2 onion at layer 0 may not subsequently broadcast NR/NV on `nr_tag_0`; honest who broadcast NR/NV may not subsequently include σ. The layer-0 leader's Phase-1 σ counts as their σ-side commitment — they cannot emit NR/NV for layer 0 even if the host application's verdict on `V_{L_0}` would have changed by Phase 2. EKM enforces this cross-phase exclusivity by coordinating across the operator's V-signing and IBE-signing shares (which are distinct keys, but the slashing-protection log keys on (slot, layer)): an NR/NV partial on `nr_tag_0` is rejected if the same operator has previously signed σ on `V_{L_0}` at the same slot, and vice versa. Pigeonhole 1 below relies on this rule.
- Every operator signs each layer's `V_{L_k}` it considers valid (host returns `valid`) in its Phase-2 onion.

EKM/slashing-protection must permit the operator's per-layer Phase-2 σ signings (one per layer with valid V) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating point is **candidate signing** (Phase-1 leader and Phase-2 onion alike) and **no-σ signing** (Phase-2 IBE partial on `nr_tag_0`), not just submission.

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (equivocation-to-NR, cross-signing detection, head-change refresh) are still described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f = 1`**: up to 1 operator may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, lie about bids, etc.). 3 honest are guaranteed.
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `D` (propagation P99/P999) and clock skew `δ`. The cutoffs `T_candidate_accept = T_commit − (D + δ)` and `T_arrival = T_commit + Δ_2` operationalize this bound. Safety holds against arbitrary network adversaries; only liveness depends on synchrony.

### Safety (cryptographic, unconditional)

**Claim:** at most one full `V` signature is ever produced per TBFT instance per slot — across either layer, on any value, across any combination of σ sources — cluster-wide, against an offline-aggregating byzantine, regardless of which honest aggregation rules are followed.

The proof rests on two pigeonhole arguments at each layer.

**Pigeonhole 1 — σ-vs-no-σ at the same layer.** σ-quorum on `V_{L_0}` and NR-quorum on `nr_tag_0` cannot both be reached. (Recall NR-quorum here counts NR + NV uniformly — see "Operator commitments — σ, NR, NV".)

- σ-quorum: `h_σ + byz_σ ≥ qV = 3` (where `h_σ` counts honest σ partials at layer 0 from any phase — Phase-1 leader σ and Phase-2 onion σ — uniformly, deduplicated per operator).
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 3`.
- Honest sign at most one side per layer (across phases — see "Slashing-protection scope"): `h_σ + h_NR ≤ 3`. **This includes the layer-0 leader**: their Phase-1 σ counts as their σ-side commitment, and they are protocol-bound not to subsequently emit NR/NV at layer 0 (and EKM-prevented from doing so, even if the host application's verdict on `V_{L_0}` would have changed); so they contribute to either σ or NR for layer 0, never both.
- Byzantine can sign both sides (cross-signing): `byz_σ + byz_NR ≤ 2` (with `f = 1`, byz contributes at most 1 to each pool, so at most 2 total).
- If both quorums reached: `h_σ + h_NR ≥ 3 + 3 − 2 = 4`. But `≤ 3`. Contradiction. ∎

**Pigeonhole 2 — two σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach σ-quorum at the same layer (e.g. via leader equivocation that some honest don't observe in time, or a byzantine signing both):

- `(h_σ_V + h_σ_V') + (byz_σ_V + byz_σ_V') ≥ 2 · qV = 6`.
- Honest sign at most one V per layer: `h_σ_V + h_σ_V' ≤ 3`. **The leader counts as one honest if honest, one byzantine if byzantine** — they sign σ_V exactly once per (slot, layer) per the protocol's single-σ-V rule (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling"), so an honest leader contributes to one V's pool, never both.
- Byzantine can sign both V's: `byz_σ_V + byz_σ_V' ≤ 2`. A byzantine leader violating the single-σ-V rule is still bounded here — they're one of the f byzantine, contributing at most one σ partial per V.
- Bound: `3 + 2 = 5 < 6`. Contradiction. ∎

The same arguments apply symmetrically to the backup layer once it's unlocked. Neither proof depends on honest operators excluding cross-signers from their aggregation — both are properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule.

### Liveness (synchrony-conditional)

TBFT's liveness is **partial-synchrony-conditional within `T_commit + Δ_2`**, the same per-window envelope SSV's QBFT relies on per round (cf. [protocol/v2/qbft/roundtimer/timer.go:148](../protocol/v2/qbft/roundtimer/timer.go)). If propagation between honest operators stays bounded by the propagation budget, the protocol terminates cleanly. If propagation is degraded badly enough that no σ-quorum and no NR-quorum reach their thresholds, the slot is missed. There is no "round 2" — TBFT is single-shot by design. **Safety holds in either case.**

**Byzantine-leader selective-delivery resistance under partial synchrony.** Worst-case attack: byzantine `L_0` tries to selectively deliver `(V_{L_0}, σ_{L_0}^V, σ_{L_0}^{op})` to a strict subset of the 3 honest, intending to fragment the σ pool while keeping NR-quorum reachable. With the **candidate acceptance cutoff** honored by every honest receiver, the byzantine has three consistent outcomes:

| Bundle release time | What every honest sees by `T_candidate_accept` | σ-side | NR-side | Outcome |
|---|---|---|---|---|
| Released with re-flood headroom (`≥ D + δ` before cutoff), reaches at least one honest | All 3 honest accept | 3 honest σ + leader's Phase-1 σ = 4 ≥ qV = 3 | 0 | Reconstruct V_{L_0} ✓ |
| Released *at* `T_candidate_accept` edge (re-flood lands inside worst-case skew window) | f+1 = 2 honest accept on time; 1 honest's first-observation crosses their own cutoff and treats as not-received | 2 honest σ + 1 leader Phase-1 σ = **3 = qV** | 1 | Reconstruct V_{L_0} ✓ (succeeds because `f+2 = qV` at `f = 1`) |
| Released later than `T_candidate_accept`, or never released, or fully eclipsed | All 3 honest treat as not-received | 0 | 3 ≥ qEnc = 3 | Fall through to V_{L_1} ✓ |

**Slot succeeds in every byzantine-`L_0` attack scenario under partial synchrony at `f = 1`** — the middle row's exact `f+2 = qV` equality is what closes the byzantine-leader-at-cutoff edge for TBFT specifically; without that, the same edge would miss (which is why TBFT-shape doesn't extend to `f ≥ 2` — see [TBFTR.md](TBFTR.md) "Liveness / Byzantine-at-cutoff edge"). Symmetric analysis for byzantine `L_1` with `L_0` honest: the primary path resolves cleanly; the backup is irrelevant.

**Marginal-synchrony resilience.** Between the two clean outcomes lies a band where bundle release is on-time but re-flooding doesn't reach every honest within `D + δ`. TBFT's leader-σ-V head-start covers a slice of this band; the rest spills into the failure modes below:

| Re-flood outcome at `T_candidate_accept` | σ-side (incl. leader's Phase-1 σ) | NR-side | Outcome |
|---|---|---|---|
| All 3 honest received V | 3 honest σ + 1 leader σ = 4 ≥ qV | 0 | Slot succeeds ✓ |
| **Moderate marginal**: 1 of 3 honest missed re-flood (one slow link in the mesh) | 2 honest σ + 1 leader σ = **3 = qV** | 1 | Slot succeeds ✓ |
| **Aggressive marginal**: 2 of 3 honest missed re-flood (gossipsub propagation between *multiple* honest pairs exceeding budget — not just one slow link) | 1 honest σ + 1 leader σ = 2 < qV | 2 < qEnc | **Slot misses** (covered by "Bad synchrony" below) |

The leader's Phase-1 σ is what makes the moderate-marginal row close at `n = 4`: without it, 2-of-3-honest σ count = 2 < qV. The head-start partial pushes σ-quorum to exactly `qV = 3`. The aggressive-marginal row is **uncoverable at `n = 4`** by either TBFT or TBFTR — TBFTR's secondary closure has a witness-threshold precondition (≥ `f+1 = 2` distinct Phase-2a σ-signers) that, at `n = 4`, coincides with TBFT's "≤ 1 honest missing re-flood" bound. The secondary closure starts extending the band only at `f ≥ 2` (n ≥ 7); see [TBFTR.md](TBFTR.md) "Liveness / Comparison with a leaner (TBFT-shape) protocol" for the per-`f` widening.

**Application-validity-divergence — known liveness limit.** When honest receivers' application verdicts on `V_{L_0}` diverge — some return `valid` (commit σ), others return `not-valid` (commit NV) — the cluster can deadlock at layer 0 under adversarial byzantine. The mechanism:

- The honest layer-0 leader's Phase-1 σ commits them to σ-side. Per cross-phase exclusivity, the leader cannot emit NR/NV at layer 0.
- Non-leader honest who returned `not-valid` emit NV (≤ 2 total — the bound, since the leader is excluded from this side).
- Adversarial byzantine withholds NR/NV (and σ).
- σ-pool: 1 (leader) + `m` (honest who returned valid) + 0 byz = `m + 1 < qV = 3` whenever `m < 2` (i.e., whenever any non-leader honest returned `not-valid`).
- NR-pool: at most 2 honest NV + 0 byz = 2 < qEnc = 3.
- Neither quorum reaches; the layer can't fall through (NR-quorum at layer 0 is needed to unlock layer 1's encryption); the slot misses overall. **Safety holds; liveness is lost for this slot.**

This is a known property of the `qEnc = qV = 2f+1` threshold + cross-phase exclusivity rule. The cluster's no-σ pool is capped at 2 honest contributors when the leader is σ-committed, which is one short of `qEnc` without byzantine cooperation. The trade is: cryptographic safety against an offline-aggregating adversary (Pigeonhole 1 with `qEnc = qV`) in exchange for a deadlock window when honest application verdicts diverge.

For SSV's proposer duty, divergence on a single `V_{L_0}` typically arises from **post-signing application-state changes** — e.g., the leader fetched `V` against beacon-head `H1` at signing time, the head moves to `H2` between Phase 1 and Phase 2, and some honest receivers' application validation now returns `not-valid` (parent root mismatch). The protocol cannot resolve this; the host is responsible for managing application-validity stability across the consensus window. See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational guidance.

### Failure modes

The slot misses (no V signature is produced) under any of the following:

- **Bad synchrony (aggressive marginal and beyond)** — real propagation exceeds the budget `D` so badly that ≥2 of 3 honest miss re-flood by their cutoff (the aggressive-marginal row above). The cluster fragments: 1 σ + leader's σ + 2 NR — σ-side (= 2 < qV) and NR-side (= 2 < qEnc) both fall short. **No safety violation.** Single-shot means no round 2. Tightening the cutoff trades miss-on-jitter rate for resilience against late byzantine releases.
- **More than `f` faults** — both leaders byzantine (impossible at `f = 1`) or more than 1 operator offline/byzantine combined. Standard `3f+1` trust bound.
- **Backup unavailable plus primary path failure** — `L_1` doesn't broadcast and `L_0`'s path also fails. Layer 1 has nothing to fall through to.
- **Application-validity-divergence on layer 0 (under adversarial byzantine)** — see "Application-validity-divergence" above. If honest application verdicts diverge on `V_{L_0}` and adversarial byzantine withholds, layer 0 deadlocks: σ-pool short of qV, NR-pool capped at 2f < qEnc=3, so `nr_tag_0` does **not** reach NR-quorum. Because layer 1's encrypted σ partials require `nr_tag_0`'s NR-quorum to peel, the cluster cannot fall through to layer 1 even when `L_1` broadcast a perfectly valid backup. Slot misses overall — independent of whether the backup itself is available.

### Equivocation handling

If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation:

1. Locally treat that leader's layer as non-receipt: don't include the corresponding partial signature in the onion (Phase 2); for layer-0 equivocation, broadcast a no-σ attestation on `nr_tag_0`; for layer-1 equivocation, omit layer 1 of the onion entirely.
2. The pair of signed bundles is a self-contained slashable fault proof against that leader.

The leader is required to sign σ_V *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple σ_V partials on the wire. Any second σ_V from the same leader is a protocol violation.

The equivocation rule is what makes Pigeonhole 2 above tight in practice: honest operators who observe the equivocation evidence avoid signing either V at that layer, capping `h_σ_V + h_σ_V'` strictly below 3. Without the rule, honest could split their σ across the two values; with it, they emit NR/NV instead and the equivocation evidence is gossipped for slashing.

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed in two distinct Phase-2 onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation (see "Slashing evidence"). For aggregation: `i` contributes **at most 1 σ-claim per distinct V** to that V's σ-quorum count regardless of how many onions they produced — counting is per-distinct-sender per-V, not per-onion, so two onions from `i` claiming σ on V don't double-count. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression: 1 byz × 1 per V = 1 (with `f = 1`), and Pigeonhole 2 still bounds two σ-quorums on different V's at the same layer cluster-wide. Honest receivers MAY additionally elect to fully suppress `i`'s partials from σ- and NR-aggregation upon observing the equivocation evidence — this is symmetric with the optional cross-signer filter described under "Cross-signing detection" and is similarly not load-bearing for safety.

**Single-V receiver interaction with leader equivocation.** An honest non-leader receiver who only ever observes one of two equivocating bundles (V, but not V') cannot detect equivocation locally, by definition. Their σ-commit on the V they saw follows the regular Phase-2 path. Pigeonhole 2 still bounds cluster outcomes regardless of who detected what, so safety holds. At liveness level, the receiver's σ commitment is locked once broadcast (per cross-phase exclusivity); if the cluster subsequently fails to converge on either V or V' at layer 0 (Pigeonhole 2 prevents both σ-quorums; locked σ commitments may also reduce NR contributors below `qEnc = 3`), the slot can deadlock at layer 0 without falling through to layer 1 — the same deadlock shape as application-validity-divergence. Out-of-protocol mitigation: honest operators SHOULD defer their Phase-2 onion broadcast to as late as practical within Δ_2 (consistent with the per-window deadline rule — broadcasts must still arrive at all honest by `T_arrival = T_commit + Δ_2`) to maximize observation time for any late-arriving second σ_V partial that would surface equivocation. This is a host-policy knob, not a protocol-level requirement; the protocol's safety doesn't depend on it.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_0` is a slashable cross-signer. The pair is detected uniformly across phases:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to the layer-0 leader specifically: a `σ_{L_0}^V(V_{L_0})` from the Phase-1 bundle paired with an `σ_{L_0}^{IBE}(nr_tag_0)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** — any operator who included σ in their onion *and* broadcast a no-σ attestation.

Detection is straightforward — the dual partials are public.

Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1 above proves it). The detection is purely for **attribution** and out-of-band punishment. Honest aggregation may filter cross-signers, but doing so is not load-bearing for safety.

### Slashing evidence

Three rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to make byzantine misbehavior accountable.

- **Self-contradiction (σ + NR/NV).** If operator `i`'s onion contains `σ_i^V(V_{L_0})` *and* `i` broadcasts `σ_i^{IBE}(nr_tag_0)`, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. The leader is required to sign `σ_V` exactly once per (slot, layer); refreshes in the host's fetch loop happen pre-signing and don't surface multiple `σ_V` partials on the wire (see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance). Any observable double-signing is therefore protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` appearing in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different `V` is detectable from the partial sigs alone. Slashable on the same logic.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys), so attribution doesn't require cluster-wide coordination — any observer with the published partials can produce the slashing case.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

Only **one tag** is used per slot (`nr_tag_0`), making implementation substantially simpler than per-layer-tag protocols. A `drand/tlock`-style construction works directly. The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 3`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

## Properties summary

| Property | TBFT |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1`, unconditional |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition |
| Termination (output guaranteed) | **No**, single-shot; partial-synchrony-conditional |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates trigger non-receipt at that layer; pair forms slashable evidence |
| Byzantine-leader-grief resistance | **Closed under partial synchrony** via leader-σ-V-in-Phase-1 + gossipsub re-flooding; extends to marginal synchrony at "≤ 1 of 3 honest miss re-flood" via the head-start partial. Beyond that band (≥ 2 of 3 honest miss re-flood) is uncoverable at `n = 4` — TBFTR's secondary closure at this size also caps at the same bound (its witness threshold of `f+1 = 2` Phase-2a σ-signers coincides with the leaner protocol's coverage), so widening only occurs at `f ≥ 2`. |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (primary → backup) |
| Round-change recovery | No |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block:

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
| `T_1` | early backup window (e.g. `slot_start + 1s`) |
| `T_0` | late primary window (e.g. `slot_start + 2s`) |
| `T_commit` | commit / view-fix deadline (e.g. `slot_start + 3s`); reconstruction and submission happen after, with headroom against the relay 4s cutoff |

Phase timeline:

- Phase 1 layer-1: ~`slot_start + 1s` (backup fetch and broadcast).
- Phase 1 layer-0: ~`slot_start + 2s` to `slot_start + 3s` (primary fetch and broadcast; gossipsub re-flooding window between this and Phase 2 closes selective delivery).
- Phase 2: `slot_start + 3s` to `slot_start + 3.5s` (onion + non-receipt broadcast).
- Phase 3: `slot_start + 3.5s` onwards (reconstruct + submit + certificate gossip).

Cryptographic safety (`qEnc = qV`) ensures only one block can ever get a valid validator signature.

### Timing budget

The end-to-end budget for a proposer slot must fit inside the relay submission cutoff (~4s after `slot_start`). The structure:

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

The deadline-tuning rule from caveat 3 below applies: `T_arrival − T_commit > D + δ` (i.e., the Phase-2 window `Δ_2` must exceed the propagation budget plus clock skew), where `T_arrival` is the cutoff for accepting Phase-2 contributions (typically `T_commit + Δ_2`), `D` is the propagation P99/P999 and `δ` is the bounded clock-skew across operators.

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_{L_k}`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_{L_k}` is locked on the originally-signed value. **The layer-0 leader specifically also cannot subsequently emit NR/NV on `nr_tag_0`** — the Phase-1 σ is their σ-side commitment per the cross-phase exclusivity rule (see Phase 2's "Per-operator commitment for layer 0 is exclusive across phases").

**Receiver-side application-validity divergence.** Honest non-leader operators run the host's validity check on received candidates at acceptance time. If a head change between Phase 1 and Phase 2 causes the verdict to flip from `valid` to `not-valid` for some honest receivers, those operators commit NV in Phase 2 (cross-phase exclusivity prevents a switch back to σ once committed). This is the divergence scenario analyzed in "Fault tolerance / Application-validity-divergence" — under adversarial byzantine withholding it can deadlock layer 0 (and the slot misses overall if layer 1 is also unavailable or itself diverges).

**Operational implications for the host:**

- Re-orgs at slot boundaries are rare events; the deadlock window is bounded by re-org rate. Single-shot consensus means no in-protocol retry — recovery is at the next slot.
- The host controls the receiver-side validity behavior. A stricter receiver-side check (re-validate `parent_root` against the current head at acceptance) maximizes correctness against re-orgs but exposes the cluster to the post-signing-divergence deadlock. A looser check (validate once at acceptance, then commit regardless of subsequent head movements) avoids the deadlock at the cost of potentially committing on a value whose parent later becomes orphaned (beacon-chain submission rejection then causes the slot miss instead). The right choice is operational and depends on observed re-org rates and the host's tolerance for each failure mode. The TBFT protocol works correctly in either case.

Implementation notes:

- Each operator must track the current head locally and validate `parent_root` of received candidates against it as part of the host's validity verdict.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" — a second signing attempt at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing detectable cluster-wide.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 3` and IBE at `qEnc = 3` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same.

2. **Deadline coordination.** Clock skew across operators must be bounded by `δ` and known. The cutoffs derived from `D` (propagation P99/P999) and `δ` together drive the partial-synchrony assumption:

   - **`T_candidate_accept = T_commit − (D + δ)`** for Phase-1 candidates. Receivers reject candidates whose first-observation time is later. (Caveat: under worst-case clock skew, a re-flood from an honest acceptor at exactly this cutoff may not reach all other honest before *their* cutoffs — see "Phase 1 / Bundle propagation". TBFT's primary closure works at `f = 1` because `f+2 = qV = 3` exactly closes σ-quorum even when this happens; the edge isn't separately recovered.)
   - **`Δ_2 > D + δ`** for the Phase-2 window: every honest's Phase-2 onion / NR broadcast at the start of the window must arrive at every other honest by `T_arrival = T_commit + Δ_2` (the start of Phase 3 reconstruction), so the σ-pool / NR-quorum aggregation in Phase 3 has a complete view.

   Both are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Tag construction and replay.** The single `nr_tag_0` per slot must uniquely bind `(slot, cluster, layer 0)` to prevent replay across slots/layers/clusters. Structure: `("slot", N, "cluster", C, "layer", 0, "no-quorum")`.

4. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one TBFT instance and assumes:

   - Single TBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between TBFT and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction), not just submission.

## Where this came from

TBFT is the result of a design exploration starting from "Proposal 1" in [ssvlabs/ssv#1829](https://github.com/ssvlabs/ssv/issues/1829) (backup + profitable block race via two QBFT instances), reformulated around cryptographic safety. The result is a substantially simpler protocol than Proposal 1 (no QBFT instances) at the cost of giving up byzantine fallback past the second leader.

For larger cluster sizes (`n ≥ 7`), see [TBFTR](TBFTR.md). TBFTR also supports `n = 4` as a configurable special case (with `K = 2`); TBFT is the leaner alternative that drops TBFTR's V-plaintext + Phase-2-split machinery — see Appendix A.

## Appendix A — How TBFT differs from TBFTR at n=4

Both protocols share the same cryptographic core (`qEnc = qV = 2f+1`, leader-authenticated candidates with both V-keypair and operator-identity sigs over a structured envelope, equivocation-to-NR rule, IBE primitive, two DKGs at the same threshold). The differences are in onion structure, Phase-2 timing, and the resulting fault-tolerance band — comparing both at `n = 4, K = 2` to make the trade-off concrete:

| Aspect | TBFT | [TBFTR](TBFTR.md) (n=4, K=2) |
|---|---|---|
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Equivocation-to-NR rule | Yes | Yes |
| qV / qEnc | `qV = qEnc = 2f+1 = 3` | Same |
| Onion at layer k | `E_{nr_tag_0}(σ_i^V(V_{L_k}))` (encrypted partial only; no V plaintext; at K=2 the chained wrapper has only one tag) | `V_{L_k} ‖ C_k(σ_i^V(V_{L_k}))` (V plaintext + chained-IBE-wrapped partial; same single-tag at K=2) |
| Phase 2 timing | Single window `[T_commit, T_commit + Δ_2]` (onion + NR together) | Split: 2a (onion only) + 2b (late σ or NR) |
| Late σ broadcasts | None | Phase 2b — operators who recovered V via peer onions sign σ then |
| Phase-1 fetch timing | Asymmetric: `T_1 < T_0` (backup early, primary late) | Uniform across layers (configurable per leader) |
| Byzantine-leader-grief closure | Primary (gossipsub re-flooding under partial synchrony) + moderate marginal (1-of-3-honest-missing-reflood, via leader-σ head-start) | Same primary + secondary-closure redundancy in the same band; coverage band coincides at K=2 / n=4 (witness threshold caps secondary closure at the same 1-of-3 bound). At larger `f` the secondary closure extends the band to `f`-honest-missing-reflood. |
| Bandwidth (worst case) | ~21 KB | larger by V-plaintext + Phase-2b overhead (slot-dependent) |
| Latency overhead | None beyond standard Phase 2 | +Δ_2b (~250 ms for `D + δ ≈ 200 ms`; sized strictly above `D + δ`) |
| Tag count | 1 (`nr_tag_0` only) | Same — single tag at K=2 |
| Number of leader candidates per slot | 2 (primary + backup) | Same — K=2 |

At `n ≥ 7`, only TBFTR is supported — the leaner TBFT-shape protocol covers only "1 honest missing re-flood" regardless of `f`, while at `f ≥ 2` the practical marginal band reaches "up to `f` honest missing"; TBFT-shape's single-window σ count caps at `f+2 < qV = 2f+1` once `f ≥ 2`, missing slots in the gap.

**If you're choosing between protocols at `n = 4`**: pick TBFT for minimal protocol complexity. TBFTR-at-n=4 covers the same marginal-synchrony band (≤ 1 of 3 honest missing re-flood — the witness threshold caps secondary closure at the same bound TBFT already covers); the only thing TBFTR adds at this size is redundancy within the band (extra σ partials), which rarely earns its bandwidth/latency premium. Either is cryptographically safe.

## Appendix B — Dynamic leader-ordering extensions

This appendix sketches two related extensions that replace the fixed primary→backup priority with **dynamic ordering** based on a deterministic rule over the candidates supplied. Both are design sketches, not part of the baseline TBFT spec — the baseline's fixed priority is simpler and equally good in healthy operation. The two variants differ in what the deterministic rule operates on.

- **B.1** sketches **bid-ordered** selection — clean fit at n=4, well-defined semantics, attribution story.
- **B.2** sketches **parent-root-based** "ordering" — included for contrast, mostly to call out why TBFT doesn't extend well to support it.

Under TBFT's application-agnostic framing (see "Preconditions on the host application" / "Operator commitments — σ, NR, NV"), these extensions are best read as **examples of plugging an application-supplied ordering criterion into TBFT's leader-ordering slot**. The mechanism (per-layer commit-tags + IBE-encrypted σ partials + per-operator commit exclusivity) is protocol-level and shared between the two variants; the criterion (B.1: bid; B.2: parent-root match) is host-supplied. The general K-layer formulation lives in [TBFTR.md](TBFTR.md) Appendix B.

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

**Setting (modified):** the structured envelope in Phase 1 binds `(protocol_tag = "TBFT-v1", message_kind = "phase1-bundle-bid", cluster_id, slot, layer k, leader_id, value_root, bid)`. The `message_kind` is bumped from `"phase1-bundle"` (baseline) to `"phase1-bundle-bid"` to domain-separate bid-bearing envelopes from baseline ones — operators running both baseline and bid-ordered configurations on the same identity key cannot have their envelopes collide. Bid is application-supplied — whatever numeric type the application uses for value comparison (uint256 wei, fixed-point, etc. — the protocol just needs a total ordering with a tiebreaker). Tiebreaker for equal bids: lower `leader_id` wins (deterministic, could be any other deterministic function instead). The protocol carries `bid` as an opaque payload bound into the envelope's signature; it doesn't interpret bid semantics. (Other host applications could plug in different ordering payloads — `bid` is the SSV instance.)

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

Same algebra as the baseline "Fault tolerance / Safety", with the σ-pool / NR-pool split replaced by per-layer commit pools:

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

**Post-hoc attribution.** Each Phase-1 envelope is a self-contained record `(slot, cluster, layer, leader_id, value_root, bid, σ^op)`. Anyone — operator, watchdog, slasher — can after the slot:

1. Resolve `value_root` to the actual block (e.g., from the cluster's submission cache or the beacon chain).
2. Query the relay (or other ground truth) for the actual bid that block was offered at.
3. Compare `bid_claimed` (in the envelope) vs `bid_actual` (from the relay).
4. If they don't match within tolerance, the envelope + relay record is a slashable liveness-fault proof against `leader_id`.

The protocol layer doesn't need to do this in real time — it's an audit performed asynchronously. Out-of-band slashing or reputation penalty follows.

#### Trade-offs vs baseline TBFT

| Aspect | Baseline TBFT | Bid-ordered variant |
|---|---|---|
| Phase 1 envelope | `(protocol_tag, message_kind, cluster_id, slot, layer, leader_id, value_root)` | + `bid` (application-supplied) |
| Phase 2 onion | layer 0 plaintext σ + layer 1 IBE-encrypted σ + (separately) NR partials | one IBE-encrypted σ at chosen layer + commitment partial sig |
| Phase 2 tag(s) | 1 (`nr_tag_0`) | 2 (`commit_tag_0`, `commit_tag_1`) |
| Per-operator commitment | σ XOR NR per layer, multi-layer | one layer per slot, exclusive |
| IBE encryption per onion | 1 | 1 (for chosen layer only) — net same |
| Phase 3 walk | priority order, NR-quorum unlocks layer 1 | parallel commit-quorum lookup across both layers |
| Equivocation handling | Equivocation → NR for that layer | Equivocation → operator skips that layer, commits to the other |
| Liveness when L_0 unavailable | NR-walk required (same agreement threshold, extra step) | Direct commit to L_1 (no walk step) |
| Liveness when L_0 byz lies high | Slot completes with low-MEV block (cluster has no choice — fixed priority) | Slot completes with low-MEV block (same outcome; lie not detectable in real time), but attributable post-hoc and triggers slashing |
| Liveness when L_0 byz lies high + V_0 invalid | NR-walk to L_1 needed | Direct commit to L_1, no walk step |
| Liveness when honest disagree on application validity | Layer 0 deadlocks (σ-pool short of qV when leader is σ-committed; NR-pool capped at 2 non-leader honest < qEnc); slot misses without byz cooperation | Same threshold — commits fragment across layers, no commit-quorum reaches without byz cooperation. **Note**: the variant has no NR side to walk through (per-operator commits are layer-exclusive), so under disagreement it cannot fall through the way a multi-layer baseline would. At K=2 this doesn't bite (baseline's fall-through hits the same byz-cooperation requirement); at K > 2 the regression is real — see [TBFTR.md](TBFTR.md) Appendix B.2 "Doesn't win" for the structural analysis |
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

The split case is the application-validity-divergence scenario from "Fault tolerance / Application-validity-divergence": with operators disagreeing on which candidate is valid against their current head, neither layer's commit-quorum can reach. Slot misses. (Safety still holds via the same commit-tag exclusivity machinery as B.1 — the issue is purely liveness.) Adding parent-root as a routing rule doesn't fix this; the rule's *output* fragments along exactly the same line as the underlying head disagreement.

**At K=2 specifically, there's no useful design space.** Two layers gives parent-root match at most two distinct outcomes (`{valid, valid}`, `{valid, invalid}`, `{invalid, valid}`, `{invalid, invalid}`). The first and last collapse to baseline behavior or unanimous miss; the middle two collapse to "commit to whichever is valid, fixed-priority tiebreaker." None of those produce a routing decision the baseline doesn't already make implicitly. At K ≥ 3 (TBFTR territory) there's at least the question of "skip L_0 because parent_root mismatches, go to L_1 vs L_2 by some rule" — but that's the general case, [TBFTR.md](TBFTR.md) Appendix B handles it.

**The right mitigation for head-divergence-induced application-validity-divergence at TBFT is application-level**, not protocol-level: fetch `V_{L_1}` from a deeper-confirmed parent (a few slots back from the current head) so the backup's `parent_root` is structurally re-org-resistant. This is something the SSV runner can do without any protocol change — the asymmetric `T_1 < T_0` fetch times already accommodate fetching the backup well before the slot's most volatile period. It catches most of the same scenarios parent-root-based routing would address, with no spec growth.

**Summary.** Parent-root match doesn't give a useful new routing rule at K=2, fragments under the very condition it would purport to address (head disagreement), and is best handled at the application layer by choosing a more re-org-resistant parent for the backup candidate. It's worth understanding as a *non-extension* — a direction explored and ruled out — rather than a path forward.

## Appendix C — Extending TBFT to K > 2

TBFT's protocol body fixes K = 2 (one primary, one backup). The same shape generalizes to K layers: K leaders, K-1 NR tags, K-layer onions, fall-through walks deeper as each prior layer's σ-quorum fails and NR-quorum advances. Captured here as a forward-compatible extension — calling out what changes vs the K=2 baseline, why the spec doesn't make it first-class, and where to look if the extension is ever pursued.

**Cross-layer safety requires chained encryption.** A naive extension to K ≥ 3 — keeping each layer's σ partials encrypted under the immediately-prior NR tag (`nr_tag_{k-1}`) — has a cross-layer safety hole. Suppose `L_0` is honest and broadcasts V_0 normally, all 3 honest sign σ_0 (σ_0-quorum reaches `qV = 3`). Suppose also that `L_1` is byzantine and refuses to broadcast V_1, so all 3 honest sign NR on `nr_tag_1` (NR_1-quorum reaches `qEnc = 3`). Suppose `L_2` is honest and broadcasts V_2 normally; all 3 honest signed σ_2 in their onions, encrypted under `nr_tag_1`. A byzantine offline aggregator now has:

- Public σ_0 partials → V_0 sig.
- Public NR_1 partials → IBE decryption key for `nr_tag_1` → decrypts σ_2 partials → V_2 sig.

Two distinct V signatures cluster-wide. Slashing.

The bug is that Pigeonhole 1 at "Fault tolerance / Safety" is *per-layer* (σ-quorum vs NR-quorum at the same layer). It doesn't constrain σ_0-quorum vs NR_1-quorum at separate layers. With single-tag encryption, an honest who has V_0, no V_1, V_2 signs σ_0 + NR_1 + σ_2 — exactly the recipe the byzantine aggregator needs.

The fix is **chained encryption**: σ partials at layer `k` are wrapped in `k` nested IBE encryptions, one per prior NR tag (`nr_tag_0` innermost, `nr_tag_{k-1}` outermost). To peel σ_k, every prior NR-quorum must aggregate. This makes σ_2 require both NR_0 *and* NR_1 — and Pigeonhole 1 at layer 0 prevents σ_0 + NR_0 from coexisting, so σ_0 reach blocks σ_2 cryptographically.

Same fix is what [TBFTR.md](TBFTR.md) uses for `K ≥ 3` (see "Phase 2a / 2b" and "Fault tolerance / Safety / Pigeonhole 3" there). At K = 2 the chain has only one tag (`nr_tag_0`) so chained encryption reduces to single-tag — TBFT's current spec is already correct at K=2.

**Why K > 2 isn't first-class in TBFT.**

- **Byzantine fault tolerance is already saturated at K=2.** With `f = 1`, a single byzantine can hold at most one leader slot; K=2 always has at least one honest leader in {L_0, L_1}. A third leader L_2 can't dodge an extra byzantine because there isn't one. So K > 2 buys nothing in the standard byzantine model at n=4.
- **Multi-failure non-byzantine scenarios are beyond the f-bound anyway.** K > 2 could help if both L_0 and L_1 fail for unrelated non-byzantine reasons (bad luck, network jitter, relay timeout) — but that's `f+1` failures, beyond what the protocol promises to handle. Recovering one extra slot per million via L_2 isn't a strong signal vs the spec/audit cost.
- **Implementation complexity cost is real.** Chained encryption adds K(K-1)/2 nested IBE ops per onion (3 ops at K=3, 6 at K=4) — small absolutely, but the spec text grows: the σ-pool decryption walk now accumulates NR keys, the safety proof needs Pigeonhole 3, the practical caveats need a chained-encryption cost row. TBFT prioritizes minimal protocol surface for the n=4 case; that's what makes the current spec auditable in a single sitting.
- **Asymmetric fetch times don't generalize cleanly.** TBFT's `T_1 < T_0` (backup fetched early, primary fetched late for max MEV) makes sense at K=2 because the two fetch points have distinct application meaning. At K=3 the structure would need `T_2 < T_1 < T_0` and a rationale for why each successive layer is "even safer than backup" — the natural progression is "MEV-late, vanilla-early, deeper-confirmed-earlier" but each step buys less than the last. Application-side cost grows faster than protocol-side benefit.

**When K > 2 might be worth pursuing.**

- If production data shows non-byzantine multi-failure misses are a measurable cost (typically ≤0.01% of slots per leader; rare).
- If the spec is being unified — i.e., TBFT and TBFTR collapsed into a single K-configurable protocol. In that case K=2 at n=4 falls out as a config setting, the chained-encryption machinery is already paying its cost at TBFTR n≥7, and K=3 at n=4 becomes a small additional config dimension with no extra spec surface. This is probably the right time to lift the K=2 restriction — when the unified protocol is being built — rather than as a standalone TBFT extension.

**If pursued, the canonical reference is [TBFTR.md](TBFTR.md).** Its protocol body already specifies the K-generic shape: K-layer onion with chained encryption, `K-1` NR tags (`nr_tag_0` ... `nr_tag_{K-2}`), Phase-3 walk with accumulated NR keys, Pigeonhole 3 cross-layer safety. TBFT extended to K > 2 would be a strict subset of that spec, minus the V-plaintext + Phase-2-split machinery (which is independently optional — see Appendix A).

**Effect on Appendix B.1 (bid-ordered selection).** The bid-ordered variant is structurally easier to extend to K > 2 than baseline TBFT, because its per-operator single-commit invariant gives cross-layer safety for free — see [TBFTR.md](TBFTR.md) Appendix B for the K-layer bid-ordered description. The bid-ordered variant doesn't need chained encryption: each operator commits to *exactly one* layer per slot, so the σ-quorum-at-two-layers attack is ruled out by per-operator commitment exclusivity rather than cryptographic chaining. If TBFT ever adopts both K > 2 and bid-ordered selection, the bid-ordered variant should be the implementation choice — it's the cleaner spec at K ≥ 3 because it sidesteps the chained-encryption machinery entirely.

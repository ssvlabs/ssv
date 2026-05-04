# OBFT — Onion BFT

A multi-round agreement protocol for SSV clusters that produces one collective threshold-signed value per "slot" against a hard deadline. OBFT achieves agreement *cryptographically* (no honest-majority-aggregation safety dependency) over a configurable K-layer onion structure, with up to R recovery rounds providing graceful degradation under network partitions and byzantine-leader equivocation.

OBFT generalizes the [TBFT](TBFT.md) shape: K layers (configurable, `K ≤ n`), R rounds (configurable, `R ≥ 1`). At `R = 1` and `K = 2`, OBFT reduces to baseline TBFT. The added machinery — Defer state for deferred commitment, R-round retry with re-flood, L_C cluster-consensus for round-transition coordination, and the **winner-completion rule for equivocation** (honest who haven't σ-committed converge on the V with most σ-pool support, breaking ties deterministically) — extends recovery to network-partition cases (≤ R·D propagation tolerance) and equivocation single-V receivers, both of which baseline TBFT misses.

The protocol description below targets `n = 4` (`f = 1`) as the running example, with K and R tunable per duty. SSV's Ethereum proposer duty is used as the running application example. Generalizations to `n ≥ 7` are noted inline where the algebra changes.

## When to use it

**Suited for:** any SSV duty (proposer, attestation, sync committee, DKG) where TBFT's 2-RTT healthy-path latency is desired plus tolerance for network partitions and equivocation. The configurable R and RT let operators tune recovery aggression per duty's deadline budget. Particularly suited for proposer duty (4s relay cutoff) where the round-2 overhead is ~250ms vs QBFT's ~2s round-change.

**Not suited for:** scenarios requiring host-validity-divergence recovery under strict host policy (where some honest validate V and others return NV due to mid-slot state changes). OBFT inherits baseline TBFT's known limit on this class — recovery requires fresh-V refetching mid-slot, which OBFT lacks (see [TBFT.md](TBFT.md) "Application: SSV Ethereum proposer duty / Head-change handling" for host-policy mitigation; QBFT covers this via round-change with new leader if the duty's deadline budget allows).

**Also not suited for:** general-purpose state-machine replication where decision *agreement* across operators (not just *output*) is required. OBFT (like TBFT) gives a unique cluster-wide *output* via cryptographic safety; honest operators may locally observe different intermediate states without affecting the output.

## Setting

- A cluster of `n` participants with byzantine bound `f` such that `n ≥ 3f+1` (standard BFT). The running example is `n = 4, f = 1`; algebra generalizes.
- Two threshold BLS keypairs from independent DKGs run once at cluster init:
  - **V-signing keypair** at threshold `qV = 2f+1`. Used to produce the per-validator signature on `V` (e.g. an Ethereum block in the SSV proposer-duty application). Reconstructing a full `V` signature requires `qV` partial sigs. At `n = 4`, `qV = 3`.
  - **IBE keypair** at threshold `qEnc = 2f+1`. Used (a) as the threshold-signing scheme for no-quorum / skip tags and (b) as the decryption oracle for threshold identity-based encryption (IBE) / signature-based witness encryption (SWE), the same primitive used by `drand/tlock`. Decryption of a ciphertext under tag `T` requires `qEnc` partial sigs on `T` from this keypair. The two keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), but share the same threshold — see "Fault tolerance / Safety". At `n = 4`, `qEnc = 3`.
- A leader-authentication signature scheme (operator-identity key) for candidate broadcasts and protocol-level claims, distinct from the two threshold keypairs. Practical choice: reuse each operator's long-term P2P/SSV identity key.
- **K layers** (`2 ≤ K ≤ n`, configurable) with deterministically-derived leaders: layer 0 with **primary leader** `L_0`, layer 1 with **backup leader** `L_1`, ..., layer `K-1` with `L_{K-1}`. Leaders must be distinct (`L_i ≠ L_j` for `i ≠ j`). For `K = n`, every cluster member is a leader at exactly one layer per slot. The K choice is per-duty: `K = 2` for proposer duty (matches baseline TBFT, fits 4s relay cutoff with margin); `K ≥ 3` for duties with longer budgets (attestation, sync committee) or where non-byzantine multi-failure tolerance is desired.
- **R rounds** (`R ≥ 1`, configurable) with **round timeout** `RT`. Round 1 runs the standard Phase 1 → Phase 2 → Phase 3 sequence (2-RTT healthy path). Rounds 2..R fire on timeout when prior round's reconstruction failed at all layers — they re-flood retained Phase-1 bundles, allow late σ-emit by deferred operators, and emit skip-tag partials when applicable. The final round (`R`) forces NR-emit on any operator still in the Defer state. The R choice is per-duty: `R = 2` for proposer duty (one recovery round fits 4s budget); `R ≥ 3` for non-proposer duties.
- Per-layer leader-fetch deadlines `T_{K-1} < T_{K-2} < ... < T_1 < T_0`, plus a per-round cluster deadline `T_commit_r` for round `r`. (`T_commit_r` is a *view-fix point* for round `r`: each operator commits its stance based on what it observed by `T_commit_r`. Reconstruction and submission happen after.) The asymmetric fetch times let primary leaders fetch high-MEV values late (`T_0` close to `T_commit_1`) while deeper layers' leaders fetch safe early values from deeper-confirmed parents (`T_{K-1}` well before `T_0`).
- A **per-round candidate acceptance cutoff** `T_candidate_accept_r = T_commit_r − (D + δ)` where `D` is the propagation P99/P999 budget and `δ` is the cluster's clock-skew bound. Receivers accept candidate bundles whose first-observation time is at or before `T_candidate_accept_r` for the current round. Bundles first-observed after the cutoff for round `r` may still be retained for round `r+1` acceptance (see "Phase 1 / Retention bounds"). This is OBFT's key timing change vs TBFT: late delivery is not silently dropped — it's deferred to the next round, enabling the deferred-commitment recovery path.
- **K-1 NR tags**: `nr_tag_k = ("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`. Each tag corresponds to a layer-advance unlock — when `qEnc` partials on `nr_tag_k` aggregate, the cluster can decrypt next-layer (`L_{k+1}`) σ partials. The deepest layer (`L_{K-1}`) has no NR tag.

## Protocol

OBFT runs in up to **R rounds**. Round 1 is the standard 2-RTT TBFT-equivalent path; rounds 2..R are recovery rounds that fire on round-end timeout when the prior round's reconstruction failed at all layers. Each round has Phase 1 → Phase 2 → Phase 2.5 (skip + L_C signaling) → Phase 3 (reconstruction). Round 1's Phase 1 is a fresh broadcast; subsequent rounds re-flood retained Phase-1 bundles plus per-round skip/L_C claims.

### Phase 1 — Candidate broadcast

Phase 1 in round 1 has K per-layer windows (driven by the asymmetric fetch times): `[T_{K-1}, T_{K-1} + Δ_1]` for the deepest backup, then progressively `[T_{K-2}, T_{K-2} + Δ_1]`, ..., ending at `[T_0, T_0 + Δ_1]` for the primary. Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces its candidate value `V_{L_k}` and validates it against application-level rules (the leader's local fetch loop — see "Preconditions on the host application").
2. Signs `V_{L_k}` with **two** keys:
   - The V-keypair share — producing the partial threshold signature `σ_{L_k}^V(V_{L_k})`. This counts as one of the `qV` partials needed for cluster-wide reconstruction. **EKM/slashing-protection treats this as a per-slot signing event** (see "Preconditions on the host application").
   - The operator-identity key — producing the leader-auth signature `σ_{L_k}^{op}(envelope)` over a **structured envelope** binding `(protocol_tag = "OBFT-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. The `protocol_tag` + `message_kind` prefix domain-separates OBFT Phase-1 envelopes from any other use of the operator-identity key (P2P-layer messages, baseline TBFT, other OBFT message kinds, etc.). The remaining fields rule out cross-cluster / cross-layer / cross-slot replay. (The host application may bind additional fields into `V_{L_k}` itself; since `value_root = hash(V_{L_k})`, those bindings are transitively covered by `σ_{L_k}^{op}`.) The honest-operator-identity-key is assumed stable for the duration of the slot; a malicious operator may attempt anything within their f-bound, but the cryptographic auth binds each signed message to a specific identity at envelope-construction time, so identity-rotation attacks within a slot don't yield additional attack surface beyond what the f-bound already covers.
3. Gossips the bundle `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` to peers via gossipsub.

In rounds 2..R, no fresh Phase-1 broadcast happens — operators **re-flood** all retained Phase-1 bundles (from any earlier round) at round start. A bundle that was rejected by an operator in round `r` due to `T_candidate_accept_r` cutoff may still be **first-accepted** in round `r+1` if observed before `T_candidate_accept_{r+1}` (the per-round cutoffs widen forward; see "Retention bounds" below).

Receivers run **two layers of validation**, in order:

1. **Protocol-level checks** (BFT-internal): verify both signatures against the leader's known pubkeys, re-derive the envelope from `(slot, layer, V_{L_k})` to confirm it matches what `σ_{L_k}^{op}` signs, and check the first-observation timestamp against the current round's `T_candidate_accept_r` (defer for next round if later than `T_candidate_accept_r` but before final round's cutoff). Bundles failing cryptographic-auth are silently dropped. (A leader who broadcasts `(V, σ^{op})` without `σ^V` is treated as not having broadcast at all.)
2. **Application-level validation** (host-supplied): the host application returns a `valid` / `not-valid` verdict on `V_{L_k}` (e.g., for SSV's proposer duty: slot match, proposer index match, fork/domain, parent root match against the operator's view of the head, etc. — see Application section). The protocol does not interpret the application's reasoning; it only consumes the verdict. A `not-valid` verdict turns the operator's commitment into NV (operationally identical to NR — see "Operator commitments — σ, NR, NV, Defer" below).

If a leader `L_k` fails to broadcast across all R rounds, that layer is unavailable; the cluster falls through to deeper layers. If all K leaders fail, the slot is missed.

**Bundle propagation.** Honest receivers re-flood every bundle via standard gossipsub on first observation. Subsequent rounds re-flood again at round start to maximize cluster-wide reception under sub-partial-synchrony.

**Retention bounds.** Retention state for accepted Phase-1 bundles is keyed by `(slot, layer k, leader_id)`. Per key, an operator retains at most **two distinct `(V, σ_{L_k}^V, σ_{L_k}^{op})` tuples** — the first auth-valid bundle observed, plus, if a second auth-valid bundle with a *distinct* `value_root` is later observed, that one (sufficient for both Phase-2 σ-signing on the chosen V *and* leader-equivocation evidence). Further auth-valid bundles for the same `(slot, layer, leader_id)` are dropped silently. Bundles whose cryptographic-auth checks fail are dropped without retention. Bundles first-observed past the **final round's cutoff** `T_candidate_accept_R` are rejected entirely. Retention lifetime: until the operator's local end of round `R`'s Phase 3 (i.e., until reconstruction halts or the slot is declared missed); slot state is then cleared. This caps memory at `O(K · n)` bundles per slot in the worst case (every leader equivocates).

**Per-round acceptance widening.** OBFT's per-round cutoffs are nested: `T_candidate_accept_1 ≤ T_candidate_accept_2 ≤ ... ≤ T_candidate_accept_R`. A bundle first-observed at time `t` is accepted in the smallest round `r` such that `t ≤ T_candidate_accept_r`. Bundles auth-valid but received late for round `r` are retained in **auth-only** state until they pass an `r' > r` cutoff; once accepted in round `r'`, they may be σ-signed via Phase 2 in round `r'`. This is OBFT's structural mechanism for partition recovery: bundles late by one round get a fresh chance in the next.

**Why both signatures.** Including `σ_{L_k}^V(V_{L_k})` in the Phase-1 bundle gives the cluster a *head start* of one real threshold partial on `V_{L_k}` as soon as Phase 1 succeeds anywhere. Combined with honest threshold partials produced in Phase 2 by operators who received the leader's bundle (or recovered it via gossipsub re-flooding across rounds), the cluster reaches `qV` real partials on `V_{L_k}` — closing the byzantine-leader selective-delivery grief under partial synchrony (see "Fault tolerance / Liveness").

**Equivocation handling.** If a participant observes two distinct `σ_V` partials from the same leader at the same slot/layer, that's leader equivocation. The pair of signed bundles is self-contained slashable evidence. Local protocol response: the operator does not σ-sign on either V at that layer; instead, they emit a `skip_tag_k` partial (see "Phase 2.5 / Skip mechanism") to enable cluster-wide advancement past the equivocated layer. They may *additionally* emit NR (per the standard NR rule) — equivocation is positive evidence the layer cannot be trusted. Once an operator emits skip on equivocation evidence, they are committed: per the EKM rules, they cannot subsequently emit σ on either V at this layer. The leader is required to sign σ_V exactly once per slot/layer (refreshes in the host's fetch loop are pre-signing only — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance); any second σ_V from the same leader is a protocol violation regardless of intent.

**Operator commitments — σ, NR, NV, Defer.** OBFT extends TBFT's three-state commitment model with a fourth state, **Defer**, which is what enables multi-round recovery without breaking cross-phase exclusivity. For each layer, an operator's commitment falls into one of four buckets:

- **σ (sign-on-V)**: the operator received the leader's bundle on time, both protocol-level and application-level checks passed. Materializes as a σ partial in the Phase-2 onion (or as the leader's Phase-1 σ for the layer's own leader). Once σ-emitted, the operator is **σ-locked** at this layer for the entire slot (cross-phase + cross-round exclusivity).
- **NR (non-receipt, evidence-driven)**: the operator received cryptographic evidence that the layer cannot succeed:
  - **NR-silent**: cutoff for the *current round* passed AND no peer σ-emit on this layer's V is observed cluster-wide (the leader is presumed silent). May only be emitted in the **final round** R, since earlier rounds defer NR-emit when V might still arrive via re-flood.
  - **NR-equivocation**: equivocation evidence at this layer observed (operator additionally emits skip — see Phase 2.5).
  - **NV (non-validity)**: host application returned `not valid` for V_{L_k}.
- **Defer (uncommitted)**: cutoff for the current round passed, peer σ-emit on this layer's V is observed cluster-wide (so the leader is *not* silent — V exists somewhere in the cluster), but the operator does not have V locally. The operator stays uncommitted at this layer through this round, hoping re-flood will deliver V before the next round's cutoff. Defer is **not visible on the wire** (no message is broadcast for Defer state — it's pure local state).

**NR and NV are operationally interchangeable for the protocol.** Both materialize on the wire as a single message kind: a partial `σ_i^{IBE}(nr_tag_k)` from the IBE keypair on the layer's NR tag. The protocol counts NR and NV uniformly toward the same no-σ-side pool (referred to throughout as "NR-quorum" or "no-σ quorum" for short). The distinction between NR-silent, NR-equivocation, and NV is **local-only diagnostic** (an operator may log *why* it didn't sign σ — for telemetry — but the cluster-wide message and counting logic are identical). All references to "NR" in the rest of this document encompass NR-silent + NR-equivocation + NV unless stated otherwise.

**Defer is the key OBFT addition.** It lets the cluster distinguish "the leader is silent (commit NR fast, fall through)" from "I just haven't received V yet (wait, V might still arrive)". The discriminator is **observed peer σ-emit cluster-wide**: if any honest peer σ-emitted on this layer's V (visible directly off the wire — layer-0 σ partials are plaintext; deeper layers carry an explicit "σ-emit signal" alongside the encrypted partial), the cluster knows V exists, so an operator without V locally should defer rather than NR-emit. This rule preserves baseline TBFT's fast L_0-silent fall-through (no peer σ-emit ⇒ NR-emit immediately) while enabling partition recovery (peer σ-emit observed ⇒ Defer until re-flood completes in next round).

### Phase 2 — Onion broadcast `[T_commit_r, T_commit_r + Δ_2]` (per round r)

Each participant `i` constructs a K-layer onion. Layer 0's σ partial is plaintext; deeper layers' σ partials are wrapped in **chained-OR encryption** — a layer-`k` σ partial is encrypted such that decryption requires advancement past every prior layer (`L_0, ..., L_{k-1}`), with each prior advancement satisfiable via *either* NR-quorum or skip-quorum at that layer:

```
layer 0:  σ_i^V(V_{L_0})                                                       # plaintext
layer 1:  E_{(nr_tag_0 OR skip_tag_0)}( σ_i^V(V_{L_1}) )
layer 2:  E_{(nr_tag_0 OR skip_tag_0)}( E_{(nr_tag_1 OR skip_tag_1)}( σ_i^V(V_{L_2}) ) )
...
layer k:  E_{(nr_tag_0 OR skip_tag_0)}( ... E_{(nr_tag_{k-1} OR skip_tag_{k-1})}( σ_i^V(V_{L_k}) ) ... )
```

where:

- `σ_i^V(x)` is `i`'s partial signature on `x` from the V-signing share (threshold `qV`).
- `E_{(nr_tag_k OR skip_tag_k)}(·)` is threshold IBE under the cluster's IBE keypair (threshold `qEnc`) such that decryption succeeds iff `qEnc` partials on **either** `nr_tag_k` **or** `skip_tag_k` exist (chained-OR; see "Cryptographic primitive — chained-OR IBE" below for the construction).
- The chain depth at layer `k` is `k`, applied in order outermost-first when constructing (innermost-first when decrypting outer→inner).

Each operator emits their commitment per layer based on the four-state model from Phase 1:

- **σ-state**: include the (encrypted) σ partial in the onion at this layer.
- **NR-state** (silent-leader, equivocation, or NV): emit a partial `σ_i^{IBE}(nr_tag_k)` separately from the onion. These IBE partials are the witnesses that may unlock the next layer.
- **Defer-state**: omit the layer from the onion AND do not emit NR. (No wire artifact for Defer state — it's purely local.) Defer is permitted in rounds 1..R-1; at round R, all Defer operators must transition to either σ (if V received) or NR (final-round timeout).
- **σ-emit signal**: alongside encrypted layers (`k ≥ 1`), an operator who σ'd at layer `k` includes a plaintext signal `σ_i^{op}("σ-claim", slot, layer k, value_root)` — an operator-identity-key-signed claim that they σ'd at layer `k` on this V. The signal is *not* a σ partial (the partial stays encrypted); it just lets peers observe σ-emit at deeper layers without decrypting. This makes the Defer rule (defer NR-emit if peer σ-emit observed) apply uniformly across all layers, not just layer 0.

`i` gossips its onion together with all NR partials and σ-emit signals.

**Per-operator commitment is exclusive across phases AND rounds.** OBFT extends TBFT's cross-phase exclusivity to also span across all R rounds. The commitment is *one decision per operator per layer, spanning Phase 1, Phase 2, Phase 2.5, and rounds 1..R*:

- An operator who emitted `σ_i^V(V_{L_k})` at layer `k` (in any round) has σ-side committed at this layer; they may **not** subsequently broadcast an NR/NV partial on `nr_tag_k`.
- An operator who emitted an NR/NV partial on `nr_tag_k` (in any round) has NR-side committed at this layer; they may **not** subsequently emit σ on V_{L_k}.
- The layer-`k` **leader**'s Phase-1 σ_V counts as their σ-side commitment at layer `k`. They may not subsequently emit NR/NV on `nr_tag_k`.
- Across layers, commitments are **independent**: an operator's σ-or-NR commitment at layer `k` does not constrain their commitment at layer `j ≠ k`. Hedging across layers is preserved (an operator may σ at multiple layers if they validated multiple V's).

EKM enforces these exclusivities cryptographically (slashing-protection log keyed on `(slot, layer, commitment-side)`); see "Preconditions on the host application / Slashing-protection scope".

**Skip emission is a separate dimension** (see Phase 2.5). An operator may emit a `skip_tag_k` partial in addition to their σ-or-NR commitment at layer `k`, conditioned on observed evidence — without violating cross-phase exclusivity, because skip is not σ-or-NR. The aggregator's pool-counting rules (see "Fault tolerance / Safety / Effective σ-pool") subtract self-revoking skip-emitters from the σ-pool; this is what makes the skip mechanism safe under chained-OR encryption.

A byzantine operator that publishes both σ and NR on the same `(slot, layer)` is publicly attributable (see "Fault tolerance / Cross-signing detection" — under `qEnc = qV`, cross-signing has no safety impact regardless of honest aggregation behavior). For each layer's leader specifically, "cross-signing" includes any Phase-1 σ + later NR pair on the same slot; the rule applies uniformly across phases and rounds.

### Phase 2.5 — Skip emission and L_C consensus `[T_commit_r + Δ_2, T_commit_r + Δ_2 + Δ_2.5]` (per round r)

Phase 2.5 is OBFT's structural addition to TBFT. It runs in parallel with the latter half of Phase 2 (overlapping window — each operator runs Phase 2.5 logic continuously as they observe peer broadcasts). Two new wire message kinds are emitted here:

#### Skip mechanism — `KindSkipClaim`

An operator emits a partial signature `σ_i^{IBE}(skip_tag_k)` paired with **trigger evidence** at layer `k` if and only if one of two conditions holds (cryptographically verifiable by receivers):

1. **Equivocation trigger**: the operator has observed two distinct auth-valid Phase-1 bundles from `L_k`'s leader at the same `(slot, layer k)` with different `value_root`s. The skip claim carries the equivocation evidence — the pair of `(σ^op, σ^V, V)` triples — for receivers to verify.
2. **Persistent-divergence trigger**: at end of round `r`, the operator locally observes that σ-pool at `L_k` is short of `qV` AND NR-pool at `L_k` is short of `qEnc`, AND the operator additionally observes `qV` operator-signed `KindLCClaim` messages (see below) from peers all reporting that they're stuck at `L_k`. This trigger fires only at round-end, not mid-round, to give Defer operators time to receive V via re-flood. The skip claim carries the `qV` `KindLCClaim` witnesses for receivers to verify.

The wire format:

```
KindSkipClaim {
  protocol_tag = "OBFT-v1",
  message_kind = "skip-claim",
  cluster_id, slot, layer k, round r,
  partial: σ_i^{IBE}(skip_tag_k),
  trigger: { kind: "equivocation" | "persistent-divergence", evidence: ... },
  σ^op: operator-identity-key signature over the above
}
```

EKM gates `skip_tag_k` partial signing on the trigger evidence: the operator's IBE-share will refuse to sign `skip_tag_k` without verifiable evidence in the local observation log. A byzantine operator could emit a `skip_tag_k` partial without true evidence, but their isolated partial counts as `1` toward `skip-quorum` of `qEnc` — at `f = 1`, they cannot reach skip-quorum alone, and forging evidence is computationally infeasible (forging equivocation requires forging another operator's V-share signature).

When `qEnc` valid `skip_tag_k` partials accumulate cluster-wide, aggregating them yields the IBE decryption key for `skip_tag_k` — equivalent in unlocking power to NR-quorum at layer `k` (the chained-OR encryption accepts either). This is how the cluster advances past a layer that is structurally deadlocked (σ-locked operators blocking NR-quorum).

#### L_C consensus — `KindLCClaim`

An operator emits a `KindLCClaim` at end of each round to inform peers of their local view of the cluster's frontier layer `L_C` — the deepest layer the operator has observed advancement past:

```
KindLCClaim {
  protocol_tag = "OBFT-v1",
  message_kind = "lc-claim",
  cluster_id, slot, round r,
  observed_L_C: int,                         # operator's local L_C, in [0, K-1]
  pool_witness: {                            # evidence supporting the claim
    for each layer j in [0, observed_L_C):
      one of:
        - {kind: "nr-quorum", partials: [σ_a^{IBE}(nr_tag_j) for qEnc operators a]}
        - {kind: "skip-quorum", partials: [KindSkipClaim from qEnc operators]}
  },
  σ^op: operator-identity-key signature over the above
}
```

L_C is the smallest layer index `≥ 0` such that the operator has *not* observed either NR-quorum or skip-quorum at `L_C` yet (i.e., the cluster hasn't been able to advance past `L_C`). Initially `L_C = 0`; advances each time a layer's NR or skip quorum reaches.

When `qV` operators agree on `observed_L_C = X` (received cluster-wide via gossipsub), the cluster considers `L_C = X` **promoted**. The next round (round `r+1`) starts with `L_C = X` as its frontier — operators focus their re-flood and Phase-2 emissions on `L_X` and deeper, knowing layers `0..X-1` are dead (cannot reach σ-quorum, by Pigeonhole 1 — see "Fault tolerance / Safety").

Promotion accelerates round transitions: instead of waiting for the round-end timer (`RT`), an operator who observes `qV` `KindLCClaim` messages with the same `observed_L_C` can immediately fire round `r+1`'s start.

**Why promote?** L_C consensus does two things: (a) bandwidth savings — round `r+1` doesn't re-flood retained partials at layers `0..L_C-1` since those are dead; (b) faster round transitions — round `r+1` fires on observed cluster-consensus rather than timer expiry, saving up to one re-flood hop (~D) of latency. It does not unlock new recovery scope beyond what skip + Defer already provide; it's a coordination primitive.

### Phase 3 — Local decryption and reconstruction `[T_commit_r + Δ_2 + Δ_2.5, T_round_r_end]` (per round r)

Each operator attempts a K-layer reconstruction walk. At each layer, the cluster has three possible outcomes: σ-quorum reaches (output produced), or NR-quorum or skip-quorum reaches (advance to next layer), or neither (stay at this layer for next round).

The walk uses two pool-counting rules that incorporate the skip mechanism:

- **Effective σ-pool on V at layer k**: `{σ partials on V at L_k} ∖ {operators who emitted skip_tag_k}`. Operators who self-revoked via skip_tag emission are subtracted from the σ-pool count. The leader's Phase-1 σ_V counts here when valid.
- **Advance-pool at layer k**: `{NR partials on nr_tag_k} ∪ {skip partials on skip_tag_k}` — but counted as **two distinct quorums** (NR-quorum or skip-quorum reaching independently is sufficient to unlock the next layer; the chained-OR encryption accepts either decryption key).

The walk:

```
L_C = 0                                        # current frontier; advances per loop iteration
sigs[k] for k in [0, K)                        # σ pools, indexed by layer (initially empty)
nrs[k]  for k in [0, K)                        # NR pools, indexed by nr_tag_k (initially empty)
skips[k] for k in [0, K)                       # skip pools, indexed by skip_tag_k (initially empty)

for k in [0, K):
    # Aggregate σ pool at layer k from all rounds' broadcasts so far.
    sigs[k] = {σ_{L_k}^V(V_{L_k}) from Phase 1, if valid}
            ∪ {σ_j^V(V_{L_k}) from received layer-k onion contents}
              (decrypted via accumulated decryption keys at layers 0..k-1, if k > 0)
            # deduplicated per operator: leader's Phase-1 σ and onion σ from
            # the same operator collapse to one partial.

    nrs[k]  = {σ_j^{IBE}(nr_tag_k) partials, deduplicated per operator}
    skips[k] = {σ_j^{IBE}(skip_tag_k) partials with valid trigger evidence,
                deduplicated per operator}

    # Effective σ-pool at L_k: subtract self-revoking skip-emitters.
    eff_sigs_on_V[k] = {σ partials on V at L_k} ∖ {operators in skips[k]}
                       for each distinct V observed at L_k

    # Reconstruction attempt:
    if exists V such that |eff_sigs_on_V[k]| ≥ qV:
        S = reconstruct V signature on V from eff_sigs_on_V[k]
        output (V, S); halt

    # Advance attempt: either NR-quorum or skip-quorum unlocks next layer.
    if |nrs[k]| ≥ qEnc:
        decryption_key_nr = aggregate(nrs[k])      # threshold sig on nr_tag_k
        # decryption_key_nr unlocks any ciphertext under (nr_tag_k OR skip_tag_k)
        # at the chained-OR layer for layer k+1
    elif |skips[k]| ≥ qEnc:
        decryption_key_skip = aggregate(skips[k])  # threshold sig on skip_tag_k
        # decryption_key_skip unlocks the same chained-OR layer
    else:
        # Neither NR nor skip quorum reached at L_k. Stay at L_C = k for next round.
        break    # exit the layer-walk; round r ends here

    # Apply decryption to advance L_C → k+1.
    L_C = k + 1
    # Continue the walk with the next layer.

if L_C == K and no σ-quorum reached:
    # Walked all layers; no output. Stay or end depending on round number.
    pass

# End of round r's reconstruction.
# If output produced, halt.
# If round r < R and no output: round r+1 fires (re-flood + late commit + retry).
# If round r == R and no output: slot misses.
```

**`T_round_r_end`** for the deadline rule is the cutoff by which the operator must have received all Phase-2 onions, NR partials, and skip claims they intend to count for round `r`. Practically, `T_round_r_end = T_commit_r + Δ_2 + Δ_2.5 + Δ_3` where Δ_3 is the reconstruction window. The deadline rule (caveat 3) bounds the gap between phases against propagation P99/P999 and clock skew.

Multiple operators may reconstruct and submit independently; the downstream system de-duplicates.

See "Fault tolerance / Cross-signing detection" for the attribution treatment of operators that publish both σ and NR at the same layer.

#### Final-certificate gossip

Once an operator successfully reconstructs `(V, S)`, it gossips a **final certificate** to peers: `KindCertificate(slot, V, S)`. The certificate carries the agreed value and the full reconstructed BLS signature on it.

Receivers verify `S` against the cluster's V-keypair pubkey on `V`. A valid certificate gives any operator (whether or not they reconstructed locally) what they need to submit `(V, S)` downstream — protecting against the "lone-reconstructor's beacon path fails" failure mode where the cluster had a quorum but only one operator assembled it and that operator's submit failed.

Receivers SHOULD re-run host application validity on `V` before submitting downstream. A valid certificate proves cluster σ-quorum was reached on `V`, which is the protocol's safety guarantee — but it does not eliminate the possibility of post-quorum changes in application state that affect submission viability (e.g., a beacon-chain reorg between the cluster's σ-quorum and a receiver's certificate-submit can render the cluster-agreed `V` rejected by the relay or beacon node). The re-validation is a host concern and does not affect protocol-level safety; submitting an application-invalid `V` is a slot-miss outcome, not a safety violation.

Operators broadcast the certificate in parallel with their own submission attempt; the downstream system continues to de-duplicate.

### Treatment of missing onions

A participant that hasn't received `j`'s onion at decryption time treats `j` as not having contributed at that layer: no σ partial, no NR partial, no skip partial. Standard threshold cryptography — only signed messages count. Re-flooding across rounds maximizes the chance that all honest broadcasts eventually reach all honest receivers within the partial-synchrony envelope.

Liveness is bounded by the standard `3f+1` byzantine assumption plus partial synchrony within `T_round_R_end` (see "Fault tolerance / Liveness"). If more than `f` operators are offline or byzantine combined, neither σ nor NR/skip quorums reach their thresholds and the slot is missed. Within the f-bound, OBFT recovers from the full set of single-deadlock failure modes (network partition with bounded re-flood delay, byzantine-leader equivocation, host-validity divergence) given enough rounds within the slot's total budget.

### Round structure

OBFT runs up to **R rounds** with timeout **RT** per round. Round `r ∈ {1, ..., R}` proceeds as follows:

1. **Round start**: at time `T_round_r_start`, the round begins. Round 1 starts at `slot_start + T_pre` (after host-application pre-fetch); round `r > 1` starts at `T_round_{r-1}_end` (round-end timer expiry) OR upon observing cluster-promoted L_C consensus (`KindLCClaim`-quorum) — whichever happens first.
2. **Phase 1 (round 1 only)**: K leaders broadcast their Phase-1 bundles per their per-layer fetch windows (`[T_{K-1}, T_{K-1} + Δ_1]`, ..., `[T_0, T_0 + Δ_1]`). Round `r > 1` skips fresh Phase 1 — operators re-flood retained Phase-1 bundles instead.
3. **Phase 2** `[T_commit_r, T_commit_r + Δ_2]`: each operator emits their K-layer onion and any NR partials based on their current per-layer commitment state. Operators in **σ-state** emit σ; operators in **NR-state** emit NR; operators in **Defer-state** emit nothing for that layer. Operators who newly received V via re-flood since last round σ-emit on V (transitioning from Defer to σ).
4. **Phase 2.5** `[T_commit_r + Δ_2, T_commit_r + Δ_2 + Δ_2.5]`: operators emit `KindSkipClaim` partials at any layer where the skip-trigger conditions are met (equivocation evidence or persistent-divergence). Operators emit `KindLCClaim` reporting their local `L_C` view at end of round `r`.
5. **Phase 3** `[T_commit_r + Δ_2 + Δ_2.5, T_round_r_end]`: each operator runs the K-layer reconstruction walk. If σ-quorum reaches at any layer, output the V; halt. If only NR/skip quorum reached up to some layer `L_C < K`, advance L_C and continue the walk. If neither σ-quorum nor advance unlock at any layer, end of round `r`.

**Round transitions**:
- If round `r < R` ends with no output, round `r+1` fires.
- If round `r == R` ends with no output, slot misses.

**Final-round force-commit (round R)**:
- Operators in Defer state at round `R`'s Phase 2 are forced to commit: σ-emit if V received locally, else NR-emit (per the silent-leader rule, since round R's cutoff is the slot's hard deadline). This guarantees all honest at every layer have transitioned out of Defer by round R's Phase 2 — making NR-quorum reachable cluster-wide if the leader was genuinely silent.
- Final-round NR-emit by an operator may flip the cluster's outcome at that layer from σ-side to NR-side fall-through. This is acceptable: by round R, the protocol is converging on a final answer, and the trade-off is "fall-through to a deeper layer in round R" vs "miss the slot entirely".

**Round timing**: `RT = Δ_1 + Δ_2 + Δ_2.5 + Δ_3` for round 1 (full Phase 1 → 2 → 2.5 → 3); `RT = Δ_reflood + Δ_2 + Δ_2.5 + Δ_3` for rounds 2..R, where `Δ_reflood ≈ D + δ` is the re-flood window. The slot's total budget is `R · RT` (approximately; round 1 is longer due to fresh Phase 1).

## Preconditions on the host application

OBFT is application-agnostic: the protocol reaches consensus on a value `V` proposed by a leader, with the host application providing a `valid` / `not-valid` verdict on each `V_{L_k}` at the moment the operator considers committing σ to it. The protocol does not interpret the application's reasoning; it only consumes the verdict.

The host application is responsible for:

- **Producing `V_{L_k}` at the leader role**: the leader's local fetch loop runs entirely application-internal — fetch a candidate, validate against application-level rules, refetch on application-state changes if needed, then commit the final `V_{L_k}` to be signed (the σ_V locks it). See "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific instance.
- **Validating `V_{L_k}` at the receiver role**: each honest receiver invokes the host's validity check on `V_{L_k}` before committing σ. A `not-valid` verdict turns the operator's commitment into NV (see "Phase 1 / Operator commitments — σ, NR, NV, Defer").

The protocol-level checks (cryptographic auth, envelope re-derivation, per-round timing cutoff `T_candidate_accept_r`) are protocol-internal and do not depend on the application.

For SSV's Ethereum proposer duty, application-level checks include slot match, proposer index match, fork/domain match, parent root match against the operator's view of the head, relay metadata validity, doppelganger / slashing-protection checks, and block encoding well-formedness. See the Application section for details. The protocol body does not enumerate or interpret these — they're host concerns.

**Slashing-protection scope.** Each operator's V-signing share signs *multiple* values per slot (potentially up to K, one per layer they validated). Constraints per (slot, layer):

- **Each layer's leader signs σ_V exactly once per (slot, layer)** — on the final V they commit to, after any pre-signing refreshes during the host's fetch loop. Refreshes update V via re-fetch but do **not** re-sign σ_V; the leader's σ_V is locked once produced. EKM enforces this single-σ-V-per-(slot, layer) constraint cryptographically: a second signing attempt at the same (slot, layer) is rejected. This is what makes Pigeonhole 2 (see "Fault tolerance / Safety") cap the leader's contribution to one V's σ-pool, not both — see "Application: SSV Ethereum proposer duty / Head-change handling" for the SSV-specific operational workflow.
- **Each operator commits σ-or-NR exclusively per (slot, layer), across phases AND across rounds.** Honest who include σ in their Phase-2 onion at layer `k` (in any round) may not subsequently broadcast NR/NV on `nr_tag_k`; honest who broadcast NR/NV may not subsequently include σ on V_{L_k}. Each layer's leader's Phase-1 σ counts as their σ-side commitment — they cannot emit NR/NV for that layer. EKM enforces this cross-phase + cross-round exclusivity by coordinating across the operator's V-signing and IBE-signing shares (distinct keys, but slashing-protection log keys on (slot, layer)): an NR/NV partial on `nr_tag_k` is rejected if the same operator has previously signed σ on V_{L_k}, and vice versa. Pigeonhole 1 below relies on this rule.
- **Skip emission is independent of σ-or-NR commitment but gated on observed evidence.** An operator may emit `σ_i^{IBE}(skip_tag_k)` regardless of their σ-or-NR commitment at layer `k`, provided cryptographically-verifiable trigger evidence is present (equivocation evidence or qV `KindLCClaim` witnesses). EKM gates skip_tag_k signing on the trigger evidence; without evidence, signing is rejected. Skip emission is recorded in the slashing-protection log keyed on (slot, layer, "skip").
- Every operator signs each layer's V_{L_k} they consider valid (host returns `valid`) in their Phase-2 onion. The Defer state (no σ, no NR) means the operator hasn't decided yet — they may still σ-emit in a later round if V is received.

EKM/slashing-protection must permit the operator's per-layer per-round Phase-2 σ signings (one σ per layer per slot, but possibly across multiple rounds — round 1's σ partial is the same partial re-emittable in later rounds) plus the leader's Phase-1 σ_V (exactly one per slot/layer the operator is leading), plus skip_tag_k signings (gated on evidence), without flagging duplicates — the cluster's safety property collapses these to a single output, but the per-share signing log shows multiple block sigs at the same slot. The gating points are **candidate signing** (Phase-1 leader and Phase-2 onion alike), **no-σ signing** (Phase-2 IBE partial on `nr_tag_k`), and **skip signing** (Phase-2.5 IBE partial on `skip_tag_k`).

**Cross-round σ partial dedup.** When an operator σ-emits in round 1 and the slot rolls to round 2, the operator's σ partial is re-flooded but not re-signed — the same partial is reused. Phase 3's reconstruction walk deduplicates per-operator: `σ_i^V(V_{L_k})` from any round counts as `1` partial in the σ-pool, regardless of how many rounds the partial appears in.

## Fault tolerance

This section consolidates everything the protocol guarantees — and doesn't — under the byzantine + partial-synchrony fault model. Operational rules that handle specific faults (Defer-state deferral, equivocation-to-skip, persistent-divergence skip, cross-signing detection, head-change refresh) are described inline in the relevant Phase sections; the analyses of *what those rules buy* and *under what conditions* live here.

### Trust model

- **Byzantine bound `f`** with cluster size `n ≥ 3f+1`: up to `f` operators may be arbitrarily malicious (collude, equivocate, cross-sign, withhold, lie about bids, etc.). `2f+1` honest are guaranteed. Running example: `n = 4, f = 1` (3 honest guaranteed).
- **Partial synchrony for liveness**: messages eventually deliver within bounded delay `D` (propagation P99/P999) and clock skew `δ`. The per-round cutoffs `T_candidate_accept_r = T_commit_r − (D + δ)` and `T_round_r_end = T_commit_r + Δ_2 + Δ_2.5 + Δ_3` operationalize this bound. Safety holds against arbitrary network adversaries; only liveness depends on synchrony. OBFT's R-round structure extends the effective propagation tolerance to `R · D` (each round absorbs one D worth of re-flood delay).

### Safety (cryptographic, unconditional)

**Claim:** at most one full `V` signature is ever produced per OBFT instance per slot — across any layer, on any value, across any combination of σ sources, across any round 1..R — cluster-wide, against an offline-aggregating byzantine, regardless of which honest aggregation rules are followed.

The proof rests on three pigeonhole arguments. Pigeonholes 1 and 2 hold at every layer; Pigeonhole 3 covers cross-layer safety under the chained-OR encryption.

The pool definitions used in the arguments below:

- **σ-pool on V at L_k**: `{σ_i^V(V) partials at L_k}`, deduplicated per operator. Includes the leader's Phase-1 σ_V when valid.
- **Effective σ-pool on V at L_k**: σ-pool on V minus operators who emitted `skip_tag_k` partials (self-revocation via skip).
- **NR-pool at L_k**: `{σ_i^{IBE}(nr_tag_k) partials}`, deduplicated per operator.
- **Skip-pool at L_k**: `{σ_i^{IBE}(skip_tag_k) partials with valid trigger evidence}`, deduplicated per operator.

**Pigeonhole 1 — effective-σ-vs-NR-vs-skip at the same layer.** At each layer `k`, at most one of {effective-σ-quorum on a single V, NR-quorum, skip-quorum} reaches. (NR-quorum and skip-quorum may both reach simultaneously — both unlock the next layer's chained-OR encryption equivalently — but neither can coexist with effective-σ-quorum.)

*Sub-claim (a): effective-σ-quorum and NR-quorum can't both reach.*

- Effective-σ-quorum on V: `(h_σ_V − h_σ_V_skip) + (byz_σ_V − byz_σ_V_skip) ≥ qV = 2f+1`.
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase + cross-round exclusivity (per "Slashing-protection scope"): `h_σ_V + h_NR ≤ 2f+1`. (Each honest commits σ-or-NR per layer at most once, including the layer's leader: their Phase-1 σ counts as σ-side commitment, EKM-prevented from later NR/NV.)
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f` (each byz contributes at most 1 to each pool).
- If both quorums reached: `(h_σ_V − h_σ_V_skip) + h_NR ≥ 2f+1 + 2f+1 − (byz_σ_V − byz_σ_V_skip) − byz_NR ≥ 4f+2 − 2f = 2f+2`.
- But `h_σ_V + h_NR ≤ 2f+1`, so `(h_σ_V − h_σ_V_skip) + h_NR ≤ 2f+1`. With `2f+2 > 2f+1`: contradiction. ∎

*Sub-claim (b): effective-σ-quorum on V and skip-quorum can't both reach (`f = 1`, `n = 4` shown; general `n ≥ 3f+1` follows from same algebra).*

Partition honest operators into four cells per `(σ_V on V, skip)`:
- `h_σ_no_skip` (σ on V, no skip): contributes to effective σ-pool on V.
- `h_σ_skip` (σ on V AND skip): contributes to skip-pool, not to effective σ-pool (self-revoked).
- `h_not_σ_skip` (not σ on V, skip): contributes to skip-pool only.
- `h_not_σ_no_skip` (not σ on V, no skip): contributes to neither.

Honest constraint: `h_σ_no_skip + h_σ_skip + h_not_σ_skip + h_not_σ_no_skip = 2f+1`.

Byzantine: at most `f` byz nodes. Each byz contributes ≤ 1 to effective-σ-pool-on-V (if they σ on V without skipping) or ≤ 1 to skip-pool (if they skip), but NOT both simultaneously (a byz that σ on V AND skips contributes 0 to effective-σ-pool, 1 to skip-pool). So `byz_σ_V_effective + byz_skip ≤ f`.

For both quorums:
- Effective-σ on V ≥ `2f+1`: `h_σ_no_skip + byz_σ_V_eff ≥ 2f+1`.
- Skip ≥ `2f+1`: `h_σ_skip + h_not_σ_skip + byz_skip ≥ 2f+1`.

Sum: `h_σ_no_skip + h_σ_skip + h_not_σ_skip + (byz_σ_V_eff + byz_skip) ≥ 4f+2`.
- LHS ≤ `(2f+1) + f = 3f+1` (honest count + byz contribution cap).
- `4f+2 > 3f+1` iff `f > -1`. Always true. **Contradiction.** ∎

So at f=1 n=4: `4f+2 = 6 > 3f+1 = 4`. Safety holds. The argument generalizes: at `n = 3f+1`, `4f+2 > 3f+1` for all `f ≥ 0`, so Pigeonhole 1(b) holds at every supported cluster size.

*Sub-claim (c): NR-quorum and skip-quorum may both reach.*

NR-emission and skip-emission are independent dimensions — an honest may emit both (e.g., observed equivocation evidence ⇒ emit skip; observed local timing-late V-bundle that flipped to invalid ⇒ also emit NR). Both pools fill independently. Both unlock the next layer's chained-OR encryption equivalently. No safety issue: cluster reconstructs exactly one V at exactly one layer.

**Pigeonhole 2 — two effective-σ-quorums on different values at the same layer.** Two distinct `V`'s cannot both reach effective-σ-quorum at the same layer:

- `(eff_h_σ_V + eff_h_σ_V') + (eff_byz_σ_V + eff_byz_σ_V') ≥ 2 · qV = 2(2f+1)`.
- Honest sign at most one V per layer: `h_σ_V + h_σ_V' ≤ 2f+1`. Effective: `eff_h_σ_V + eff_h_σ_V' ≤ h_σ_V + h_σ_V' ≤ 2f+1`. (Skip-revocation only reduces effective counts.)
- Byzantine can sign multiple V's: `byz_σ_V + byz_σ_V' ≤ 2f` (one partial per byz per V at f-bound; with skip-revocation effective ≤ 2f).
- Bound: `(2f+1) + 2f = 4f+1 < 4f+2 = 2(2f+1)`. Contradiction. ∎

The leader is a special case: they sign σ_V exactly once per (slot, layer) per the protocol's single-σ-V rule. An honest leader contributes to one V's σ-pool, never both. A byzantine leader violating the rule contributes at most one σ partial per V; bounded by the f-byz cap.

**Pigeonhole 3 — cross-layer safety under chained-OR encryption.** Two distinct V signatures (V_k and V_{k+m} for any `m ≥ 1`) cannot both be reconstructed cluster-wide.

- For V_k sig: effective-σ-quorum on V_k at L_k must reach.
- For V_{k+m} sig: σ partials at L_{k+m} must decrypt, which requires NR-quorum or skip-quorum at L_k (and at every layer between L_k and L_{k+m}).
- By Pigeonhole 1(a)+(b): if effective-σ-quorum at L_k reaches, neither NR-quorum nor skip-quorum at L_k reaches. So L_k's chained-OR encryption layer at L_{k+1}, ..., L_{k+m} stays sealed.
- Therefore V_{k+m} sig is unreconstructable when V_k sig is reconstructable. ∎

The argument is symmetric: if V_{k+m} sig is reconstructable, then NR-quorum or skip-quorum reached at L_k (to allow decryption), so by Pigeonhole 1 effective-σ-quorum at L_k did not reach, so V_k sig is unreconstructable.

This argument applies inductively across any pair of layers. So at most one V signature reconstructs cluster-wide across all K layers.

**Cryptographic primitive — chained-OR IBE.** Layer-`k` σ partials are encrypted under `(nr_tag_0 OR skip_tag_0) ∧ (nr_tag_1 OR skip_tag_1) ∧ ... ∧ (nr_tag_{k-1} OR skip_tag_{k-1})`. Decryption requires either NR-quorum or skip-quorum at each prior layer. Implementation: each level of the chain is a standard threshold-IBE ciphertext (drand/tlock) using a tag that itself is the OR-witness — i.e., the encrypter prepares two parallel ciphertexts at each level (one under `nr_tag_j`, one under `skip_tag_j`), and the decrypter peels with whichever decryption key aggregates first. At K=2 the chain has only one level (one OR-tag pair); at K=3 there are two levels nested; etc. See "Practical caveats / Chained-OR encryption cost" for size implications.

The same arguments above apply symmetrically to all K layers. None of the proofs depends on honest operators excluding cross-signers from their aggregation — they're properties of the cluster-wide signed messages, holding against an offline-aggregating adversary that ignores any honest aggregation rule. Skip-revocation is enforced at the aggregator level (effective-σ-pool calculation), not at the operator level.

### Liveness (synchrony-conditional)

OBFT's liveness is **partial-synchrony-conditional within `T_round_R_end`** — the protocol's total slot budget. The R-round structure absorbs network-induced failures (re-flood completing across rounds), byzantine equivocation (skip mechanism advancing past equivocated layers), and host validity divergence (persistent-divergence skip trigger).

If propagation between honest operators stays bounded by `R · D`, the protocol terminates with a V signature on some layer (the first layer where σ-quorum reaches under cluster-wide V receipt, or the deepest layer reachable via NR/skip fall-through with a valid backup leader). If propagation exceeds `R · D`, or more than `f` operators are byzantine/offline, the slot is missed. **Safety holds in either case.**

**Best case (round 1 healthy at L_0)**: all honest receive V_{L_0} within `D + δ`; all 3 honest σ-emit + leader's Phase-1 σ = 4 ≥ qV. Slot succeeds in 2 RTTs (Phase 1 + Phase 2). Same as baseline TBFT.

**Aggressive-marginal recovery (round 2 covers 2-of-3-honest missing re-flood)**: 1 honest received V at round-1 cutoff; 2 honest didn't (re-flood incomplete by `T_candidate_accept_1`). Per Defer rule (peer σ-emit observed by the 2 missing honest), they don't NR-emit in round 1 — they Defer. Round 1 ends with σ-pool = 1 + leader = 2 < qV, NR-pool = 0. Round 2 fires. Re-flood completes within `Δ_reflood = D + δ` from round-1 end; the 2 honest receive V; they σ-emit. σ-pool = 3 + leader = 4 ≥ qV. Slot succeeds in ~3 RTTs. **OBFT recovers what baseline TBFT misses.**

**Equivocation single-V receivers recovery (skip mechanism advances past L_0)**: byzantine `L_0` equivocates, broadcasting V and V' to disjoint honest subsets. Honest A σ on V (saw V only); B σ on V' (saw V' only); C NR (saw both, equivocation evidence). Round 1 ends with σ-pool on V = 2 (A + byz σ_V on V), σ-pool on V' = 2 (B + byz σ_V on V'), NR-pool = 1 + maybe byz = 1 or 2 < qEnc. Round 2 fires. Re-flood delivers equivocation evidence to A and B; per the equivocation rule, they emit `skip_tag_0` partials (without retracting σ — skip is a separate dimension). All 3 honest now have skip_tag_0 partials → skip-pool = 3 ≥ qEnc. Skip-quorum reaches; cluster advances past L_0 to L_1. Effective-σ-pool on V at L_0 = 0 (A revoked via skip) + maybe byz = ≤ 1 < qV. Effective-σ-pool on V' similar. Pigeonhole 1 holds: only skip-quorum reached. L_1 σ partials decrypt; if L_1 honest with all 3 honest validating V_1, slot succeeds at L_1. **OBFT recovers what baseline TBFT misses.**

**Validity-divergence recovery (persistent-divergence skip trigger advances past L_k)**: under strict host policy, honest receivers' validity verdicts on V_{L_0} diverge (some σ, some NV). Layer 0 deadlocks: σ-pool short of qV, NR-pool capped at honest-NV-count. Round 1 ends without resolution. In round 2 (or later), honest operators emit `KindLCClaim` reporting their stuck-at-L_0 status. When `qV` `KindLCClaim` messages with `observed_L_C = 0` are received, the persistent-divergence trigger fires for any operator. They emit `skip_tag_0` partials. With qV = 3 honest (the entire honest set) emitting skip, skip-pool = 3 ≥ qEnc. Skip-quorum reaches; cluster advances to L_1. **OBFT recovers what baseline TBFT misses, given enough rounds.**

**Sub-partial-synchrony (real propagation > R · D)**: if propagation exceeds the cluster's R-round budget, late honest don't σ-emit by round R and slot misses. **No safety violation.** R is a tunable knob (via the practical caveats); larger R extends tolerance at the cost of more pessimistic timing.

**Multi-failure fall-through (K ≥ 3)**: with `K > 2` layers, the cluster can fall through past multiple deadlocked layers. E.g., at K=4 with one equivocating leader and one non-byzantine-failure leader at deeper layers: skip past L_0 (equivocation), NR past L_1 (silent), σ at L_2 if leader honest. Recovery scope grows linearly with K within the slot's total budget.

### Liveness recovery scope

| Scenario | Round 1 | Round 2 | Outcome |
|---|---|---|---|
| Healthy (all honest receive V_{L_0}) | σ-quorum reaches | — | Succeeds at L_0 in 2 RTTs ✓ |
| L_0 silent (byz withholds) | 0 σ-emits → all honest NR (silent-leader rule) → NR-quorum reaches | — | Fall through to L_1 in 2 RTTs ✓ |
| Aggressive marginal (2-of-3 missed re-flood) | σ-pool short, Defer state holds | Re-flood delivers V; σ-quorum reaches | Succeeds at L_0 in ~3 RTTs ✓ |
| Equivocation single-V receivers | σ split; no quorum | Equivocation evidence re-flooded; skip-quorum reaches | Advance past L_0; succeed at L_1 (if honest) ✓ |
| Validity divergence under strict host | σ-pool short, NR-pool capped | Persistent-divergence trigger; skip-quorum reaches | Advance past L_0; succeed at L_1 (if honest validates) ✓ |
| Multi-failure (K=4, L_0 equivocates, L_1 silent) | Multi-layer deadlocks | Skip past L_0, NR past L_1; σ-quorum at L_2 | Succeeds at L_2 ✓ |
| Sustained partition (propagation > R · D) | No quorum | No quorum | Slot misses (no safety violation) |
| > f byzantine/offline | Standard 3f+1 trust violation | — | Slot misses (no safety violation) |

**This recovery scope matches QBFT's** (round-change with new leader) within a single round, but at flat ~250ms-per-round overhead vs QBFT's ~2s round-change. See Appendix A for the side-by-side.

### Equivocation handling

If a participant observes two distinct `σ_V` partials from the same leader at the same `(slot, layer)`, that's leader equivocation. Local protocol response:

1. The operator does not σ-sign on either V at that layer (cross-phase exclusivity prevents σ-emit on either V; the equivocation evidence locks the operator out of σ-side commitment for this layer).
2. The operator emits a `KindSkipClaim` with the equivocation evidence as the trigger, contributing to skip-pool.
3. The operator additionally emits an NR partial on `nr_tag_k` (the equivocation rule treats the layer as non-receipt — the leader is byzantine).
4. The pair of equivocating bundles is a self-contained slashable fault proof against the leader, gossipped for out-of-band slashing.

The leader is required to sign σ_V *exactly once per (slot, layer)*; refreshes in the host's fetch loop are pre-signing only (see "Application: SSV Ethereum proposer duty / Head-change handling") and don't surface multiple σ_V partials on the wire. Any second σ_V from the same leader is a protocol violation.

The skip mechanism is what makes equivocation single-V receivers recoverable in OBFT (vs unrecoverable in baseline TBFT): once equivocation evidence reaches σ-locked honest via re-flood in a later round, they emit skip_tag_k partials (without violating cross-phase exclusivity — skip is independent of σ-or-NR). Effective-σ-pool decays as σ-locked operators self-revoke via skip; meanwhile skip-quorum reaches and unlocks the next layer. See "Liveness / Equivocation single-V receivers recovery".

**Cross-onion (operator-side) equivocation — counting and suppression.** If an operator `i` is observed in two distinct Phase-2 onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different V, the dual partials are slashable evidence on the same logic as leader equivocation. For aggregation: `i` contributes **at most 1 σ-claim per distinct V** to that V's σ-quorum count regardless of how many onions they produced — counting is per-distinct-sender per-V, not per-onion. The byzantine f-bound on per-V σ-pool contributions therefore holds without explicit suppression. Honest receivers MAY additionally elect to fully suppress `i`'s partials from σ- and NR-aggregation upon observing the equivocation evidence — this is symmetric with the optional cross-signer filter described under "Cross-signing detection" and is similarly not load-bearing for safety.

### Cross-signing detection

Any operator whose published messages contain *both* a σ partial at a layer AND an NR/NV attestation on the layer's `nr_tag_k` is a slashable cross-signer. The pair is detected uniformly across phases and rounds:

- **σ from Phase 1 + NR/NV from Phase 2** — applies to a layer's leader specifically: a `σ_{L_k}^V(V_{L_k})` from the Phase-1 bundle paired with an `σ_{L_k}^{IBE}(nr_tag_k)` from the same operator at the same slot.
- **σ from Phase 2 onion + NR/NV from Phase 2** (any round) — any operator who included σ in their onion *and* broadcast a no-σ attestation, in any round.

Skip emission is **not** cross-signing: an operator who emits `skip_tag_k` (with valid trigger evidence) plus σ or NR is NOT a cross-signer. The aggregator's effective-σ-pool calculation handles σ-side skip-revocation cleanly; NR-side + skip is allowed (see "Pigeonhole 1(c)").

Detection is straightforward — the dual partials are public. Under `qEnc = qV`, cross-signing has no safety impact (Pigeonhole 1(a) above proves it). The detection is purely for **attribution** and out-of-band punishment. Honest aggregation may filter cross-signers, but doing so is not load-bearing for safety.

### Slashing evidence

Four rules surface byzantine fault evidence for *attribution and punishment* (out-of-band slashing, reputation, monitoring). Under the cryptographic safety guarantees above, none of them is load-bearing for safety; they exist to make byzantine misbehavior accountable.

- **Self-contradiction (σ + NR/NV at the same layer).** If operator `i` emitted `σ_i^V(V_{L_k})` *and* `σ_i^{IBE}(nr_tag_k)`, the dual partials are slashable evidence (cross-signing).
- **Leader equivocation.** Two distinct `σ_V` partials from the same leader at the same (slot, layer) are a self-contained slashable fault proof. Any observable double-signing is protocol-violating regardless of the leader's stated intent.
- **Cross-onion partial-sig equivocation.** Operator `i` appearing in two onions with `σ_i^V(V)` and `σ_i^V(V')` at the same layer for different `V` is detectable from the partial sigs alone. Slashable on the same logic.
- **False skip emission.** Operator `i` emitting `σ_i^{IBE}(skip_tag_k)` without valid trigger evidence (forged equivocation evidence or insufficient `KindLCClaim` witnesses) is detectable on the wire — receivers verify the trigger evidence as part of skip-claim acceptance. False emission is slashable. Honest aggregators discard such partials from skip-pool counting and may flag them for slashing.

Each piece of evidence is verifiable in isolation (signed by the offending operator's own keys), so attribution doesn't require cluster-wide coordination — any observer with the published partials can produce the slashing case.

### Failure modes

The slot misses (no V signature is produced) under any of the following:

- **Sustained partition (real propagation > R · D)** — re-flood doesn't complete within the cluster's R-round budget. Honest who didn't receive V at any round cutoff stay in Defer; final-round force-NR transitions them to NR. If even forced NR-pool is short of qEnc (because σ-locked honest at deeper layers block fall-through), slot misses cleanly. **No safety violation.** The R parameter is tunable; larger R extends propagation tolerance at the cost of slot-budget consumption.
- **More than `f` faults** — more than `f` operators offline/byzantine combined. Standard `3f+1` trust bound. Slot misses regardless of round structure.
- **Backup-leader cascade failure** — every leader at every layer fails (silent OR equivocates with no honest who σ-locked + skip-recovers, OR application validity divergence at every layer). Possible only when many independent failures coincide; rare in practice. The K parameter controls the fall-through depth.
- **Application-validity-divergence at all K layers** — if every layer's V causes divergent host verdicts, the persistent-divergence skip mechanism advances through layers but no layer reaches σ-quorum. Slot misses. Mitigation: stagger fetch times (`T_{K-1} < ... < T_0`) so deeper layers fetch from progressively-deeper-confirmed parents, reducing divergence likelihood at deeper layers.

## Cryptographic primitive

The encryption-conditional-on-threshold-signature primitive is **threshold identity-based encryption (IBE)**, equivalently called signature-based witness encryption (SWE) in recent literature.

Production-grade implementations exist:

- **`drand/tlock`** (Go), audited by Kudelski 2023, deployed on Drand mainnet since 2023.
- **Shutter Network** uses the same family of primitives in production on Gnosis Chain.

OBFT uses **`2(K-1)` IBE tags per slot**: K-1 NR tags and K-1 skip tags (for K layers, the deepest layer has no NR/skip). Each tag is a standard threshold-IBE tag using the cluster's IBE keypair. The chained-OR encryption at each layer-transition is implemented as **two parallel IBE ciphertexts** — one under `nr_tag_k`, one under `skip_tag_k` — encrypted around the same plaintext (the σ partial at the next layer). Decryption succeeds iff either NR-quorum or skip-quorum at L_k aggregates; the cluster takes whichever decryption key arrives first.

The DKG for the V-signing keypair reuses SSV's existing operator-share setup. The IBE keypair requires a separate DKG at threshold `qEnc = 2f+1`, run once at cluster init. Long-lived, no per-slot rotation. The IBE keypair is distinct from the V-keypair (different cryptographic backend so the IBE primitive can use its expected DST), even though the threshold is the same.

**Chained-OR encryption cost.** At layer K-1 (deepest), each σ partial is wrapped in `K-1` levels of IBE encryption, each level being a parallel pair of ciphertexts. Per-onion size grows as `O(K^2)` plaintext bytes and `O(K^2)` ciphertext bytes (`K-1` levels × 2 parallel ciphertexts × ~32-64 bytes ciphertext per level). At K=2 the chain has 1 level; at K=4 it has 3 levels; at K=n=4 (max), 3 levels. Concrete sizes: ~2 KB per onion at K=2, ~6 KB at K=4. Within practical SSV bandwidth budgets.

## Properties summary

| Property | OBFT |
|---|---|
| Safety (no contradictory outputs) | Yes — cryptographic via `qEnc = qV = 2f+1`, unconditional |
| Validity (output ∈ proposed values, application-valid) | Yes, conditional on host-application precondition |
| Termination (output guaranteed) | Conditional: terminates within `R · RT` if propagation ≤ `R · D` and ≤ f operators byzantine/offline. Configurable R lets operators tune termination guarantee per duty's deadline budget. |
| Equivocation detection | Yes — leaders sign candidates over a structured envelope; conflicting signed candidates trigger skip emission at that layer; pair forms slashable evidence |
| Byzantine-leader-grief resistance | **Closed under partial synchrony** via leader-σ-V-in-Phase-1 + gossipsub re-flooding + R-round retry. Extends from baseline TBFT's "1 of 3 honest missing re-flood" coverage to "any honest who eventually receive V within R · D propagation budget" via Defer state. Equivocation single-V receivers and validity-divergence are recoverable via skip mechanism. |
| Operators reach the same decision | Not necessarily — only the *output* is unique cluster-wide |
| Built-in leader fallback | Yes (K-layer fall-through, K configurable) |
| Round-change recovery | Yes — R rounds with re-flood + skip-mechanism. ~Δ_round per round (~250ms typical), vs QBFT's ~2s round-change. |
| Recovery scope vs QBFT | Equivalent within partial synchrony (same single-deadlock failure modes covered); strictly better latency profile (~10× faster recovery rounds) |

## Application: SSV Ethereum proposer duty

For an SSV cluster proposing an Ethereum block, the recommended OBFT configuration is `K = 2, R = 2` — matches baseline TBFT's K=2 onion structure, adds one recovery round to absorb partition / equivocation / validity-divergence cases within the 4s relay cutoff.

| OBFT concept | SSV mapping |
|---|---|
| `n` participants | 4 |
| `f` byzantine bound | 1 |
| `K` layers | 2 (primary + backup) — same as baseline TBFT |
| `R` rounds | 2 (one initial + one recovery) |
| `RT` round timeout | ~1.5s for round 1 (full Phase 1+2+2.5+3); ~250ms for round 2 (Δ_reflood + Δ_2 + Δ_2.5 + Δ_3) |
| Slot | Ethereum slot for which the cluster is proposer |
| V-signing keypair | the validator's split BLS key (already exists in SSV) |
| IBE keypair | new per-cluster key from a separate DKG at cluster init |
| Operator-identity key | existing SSV operator key |
| `L_0` (primary leader) | designated MEV proposer for the slot (e.g. round-1 leader from existing rotation) |
| `V_{L_0}` | MEV-optimized block fetched late from the relay |
| `L_1` (backup leader) | a separately designated operator (e.g. round-2 leader; required ≠ `L_0`) |
| `V_{L_1}` | safe early-fetched block from a vanilla beacon-node payload, refreshed on head changes (per the leader's pre-signing fetch loop) |
| `T_1` | early backup window (e.g. `slot_start + 1s`) |
| `T_0` | late primary window (e.g. `slot_start + 2s`) |
| `T_commit_1` | round 1 commit / view-fix deadline (e.g. `slot_start + 3s`) |
| `T_round_1_end` | round 1 reconstruction deadline (e.g. `slot_start + 3.5s`); if no output, round 2 fires |
| `T_round_2_end` | round 2 reconstruction deadline (e.g. `slot_start + 3.75s`); leaves headroom against the relay 4s cutoff |

Phase timeline (round 1):

- Phase 1 layer-1: ~`slot_start + 1s` (backup fetch and broadcast).
- Phase 1 layer-0: ~`slot_start + 2s` to `slot_start + 3s` (primary fetch and broadcast; gossipsub re-flooding window between this and Phase 2 closes selective delivery).
- Phase 2: `slot_start + 3s` to `slot_start + 3.25s` (onion + NR broadcast).
- Phase 2.5: `slot_start + 3.25s` to `slot_start + 3.4s` (skip claims + L_C claims).
- Phase 3: `slot_start + 3.4s` to `slot_start + 3.5s` (reconstruct + submit + certificate gossip if successful).

Phase timeline (round 2 — fires only if round 1 didn't produce output):

- Re-flood: `slot_start + 3.5s` to `slot_start + 3.6s` (re-broadcast retained Phase-1 bundles).
- Phase 2': `slot_start + 3.6s` to `slot_start + 3.65s` (late σ-emit by Defer operators who newly received V; supplementary onion partials).
- Phase 2.5': `slot_start + 3.65s` to `slot_start + 3.7s` (skip claims if equivocation/divergence; final-round forced NR for remaining Defer operators).
- Phase 3': `slot_start + 3.7s` to `slot_start + 3.75s` (reconstruct retry; final certificate gossip).
- Submission: `slot_start + 3.75s` to `slot_start + 4s`.

Cryptographic safety (`qEnc = qV` + chained-OR encryption with skip-tag) ensures only one block can ever get a valid validator signature, regardless of round structure. R-round retry only enables more recovery scenarios; it cannot produce two outputs.

### Timing budget

The end-to-end budget for a proposer slot must fit inside the relay submission cutoff (~4s after `slot_start`). The structure:

```
slot_start
  + pre-consensus              (RANDAO partial-sig collection, ~T_pre)
  + round 1                    (~3.5s — Phase 1 fetch + Phase 2 + Phase 2.5 + Phase 3)
  + [round 2 if round 1 failed] (~250ms — re-flood + Phase 2' + Phase 2.5' + Phase 3')
  + downstream submission      (relay round-trip, ~T_submit)
≤ slot_start + 4s              (relay cutoff)
```

Concrete numbers for each leg should come from production telemetry (P99 / P999 tails of gossip propagation, EKM signing latency, beacon submit latency, relay submission latency). Until that lands, the values above are placeholder defaults; tighten per cluster as data arrives.

The deadline-tuning rule from caveat 3 below applies per round: `T_round_r_end − T_commit_r > D + δ + Δ_2.5` (i.e., the per-round window must exceed the propagation budget plus clock skew plus Phase 2.5 window).

### Head-change handling

For SSV's proposer duty, the host application's `valid` / `not-valid` verdict on `V_{L_k}` includes a `parent_root`-vs-current-head check (parent root of the proposed block must match the operator's view of the canonical chain). Because the head can move, this verdict can change over the consensus window even on the same `V_{L_k}`. The protocol consumes the verdict; the host is responsible for the head-tracking and validation logic.

**Leader pre-signing fetch loop.** If the head changes during a Phase-1 fetch window, candidate values fetched from the previous head are stale. The leader's fetch process is a loop: fetch → validate → check head → on head-change, re-fetch → repeat. The loop runs **before σ_V signing** — internal to the leader's local fetch state. The leader signs `σ_{L_k}^V(V_{L_k})` *exactly once per slot/layer*, on the final `V_{L_k}` they commit to after the loop terminates, then broadcasts the bundle. Refreshes are pre-signing only; the per-share signing log shows exactly one V-share signature per slot/layer (the final V).

**No refresh after signing.** Once the leader has broadcast `(V_{L_k}, σ_{L_k}^V, σ_{L_k}^{op})`, they do not produce a second `σ_V` partial for the same slot/layer. If the head changes after signing, `V_{L_k}` is locked on the originally-signed value. The layer's leader cannot subsequently emit NR/NV on `nr_tag_k` — the Phase-1 σ is their σ-side commitment per cross-phase + cross-round exclusivity.

**Receiver-side application-validity divergence — recovery in OBFT.** Honest non-leader operators run the host's validity check on received candidates at acceptance time. If a head change between Phase 1 and Phase 2 causes the verdict to flip from `valid` to `not-valid` for some honest receivers, those operators commit NV. **Unlike baseline TBFT**, OBFT recovers from this scenario via the persistent-divergence skip trigger (see "Fault tolerance / Liveness / Validity-divergence recovery"): if σ-pool and NR-pool both stay short of their quorums by round 1's end, the cluster's `KindLCClaim`-quorum on `observed_L_C = 0` triggers skip emission cluster-wide. By round 2, skip-quorum at L_0 unlocks fall-through to L_1, where the backup leader's earlier-fetched V is more re-org-resistant and likely passes validity uniformly.

**Operational implications for the host:**

- Re-orgs at slot boundaries are rare events; the deadlock window is bounded by re-org rate. OBFT's R-round structure recovers from divergence within the same slot — no need to wait for the next slot.
- The host controls the receiver-side validity behavior. With OBFT's skip mechanism, the strict host-policy choice (re-validate `parent_root` against the current head at acceptance) is no longer at deadlock-risk: divergence-induced deadlock is recoverable via skip in round 2. The host may default to strict for maximum re-org-correctness.
- Backup leader L_1 should still fetch from a deeper-confirmed parent (the asymmetric `T_1 < T_0` schedule already accommodates this), reducing divergence likelihood at L_1 even if L_0 diverges.

Implementation notes:

- Each operator must track the current head locally and validate `parent_root` of received candidates against it as part of the host's validity verdict.
- The host's EKM/slashing-protection enforces "exactly one σ_V per (slot, layer)" — a second signing attempt at the same (slot, layer) is rejected. This is what makes byzantine-leader double-signing detectable cluster-wide.
- EKM additionally permits skip_tag_k signing under the trigger-evidence guard: equivocation evidence (cryptographic) or qV `KindLCClaim` witnesses (cluster-consensus) for persistent-divergence.

## Practical caveats

1. **DKG cost.** Two threshold keypairs per cluster — V-signing at `qV = 2f+1` and IBE at `qEnc = 2f+1` — one DKG each at cluster init. Long-lived, no per-slot rotation. The keypairs are distinct (different cryptographic backends so the IBE primitive can use its expected DST), even though the threshold is the same. **Same DKG cost as baseline TBFT**; no additional ceremonies for skip mechanism (it reuses the IBE keypair with a new tag).

2. **Per-round deadline coordination.** Clock skew across operators must be bounded by `δ` and known. Per-round cutoffs derived from `D` (propagation P99/P999) and `δ`:

   - **`T_candidate_accept_r = T_commit_r − (D + δ)`** for round `r`'s Phase-1-bundle acceptance. Receivers defer (not reject) bundles first-observed past round `r`'s cutoff but before round `r+1`'s — they're retained in auth-only state and may be promoted to accepted in round `r+1`. Final round `R`'s cutoff is the slot's hard rejection deadline.
   - **`Δ_2 > D + δ`** for each round's Phase-2 window: every honest's Phase-2 onion / NR broadcast at the start of the window must arrive at every other honest by `T_commit_r + Δ_2` (the start of Phase 2.5).
   - **`Δ_2.5 > D + δ`** for each round's Phase-2.5 window: skip claims and L_C claims must propagate cluster-wide before Phase 3 reconstruction.
   - **`Δ_reflood > D + δ`** between rounds: re-flood at round `r+1` start must complete before round `r+1`'s cutoff.

   All are *liveness* requirements only; safety is unaffected by skew or propagation breakdown (see "Fault tolerance / Safety").

3. **Choosing K (layer count).** K is per-duty:

   - `K = 2`: minimum (one primary + one backup). Matches baseline TBFT's onion structure. Fits SSV proposer duty's 4s budget with margin. Recovery scope: handles 1 byzantine leader at L_0, with L_1 as backup.
   - `K = 3..n`: larger K provides additional fall-through layers for non-byzantine multi-failure (rare events: relay timeouts, network jitter, validity divergence at multiple layers). At `n = 4`, max useful K is 4 (one layer per operator). Each extra layer adds a level of chained-OR encryption (~1 KB per onion at K=3, ~2 KB at K=4 — within practical bandwidth).

   Recommended: `K = 2` for proposer duty (baseline-equivalent + R-round recovery suffices); `K = 3` or `K = 4` for non-proposer duties (attestation, sync committee, DKG) where multi-failure tolerance is desired.

4. **Choosing R (round count) and RT (round timeout).** R is per-duty, governed by the duty's deadline budget:

   - **Proposer duty** (4s relay cutoff): R = 2 fits cleanly. Round 1 = ~3.5s (full Phase 1 → 3); round 2 = ~250ms (re-flood + Phase 2'+2.5'+3'). Submission window: ~250ms.
   - **Attestation duty** (12s slot, 16s aggregation cutoff): R = 3..5 fits comfortably. More rounds extend partial-synchrony tolerance.
   - **DKG / non-time-critical duties**: R can be very large (e.g., R = 10) since deadline budget is generous.

   The `R · D` propagation tolerance is the protocol's effective resilience knob. Increasing R extends recovery scope at the cost of slot-budget consumption.

5. **Tag construction and replay.** Each NR tag and skip tag must uniquely bind `(slot, cluster, layer)` to prevent replay across slots/layers/clusters. Structures:
   - NR tags: `("slot", N, "cluster", C, "layer", k, "no-quorum")` for `k ∈ {0, ..., K-2}`.
   - Skip tags: `("slot", N, "cluster", C, "layer", k, "skip")` for `k ∈ {0, ..., K-2}`.

6. **"At most one full sig" is per-instance.** "At most one full V signature per slot" is true within one OBFT instance and assumes:
   - Single OBFT instance per slot (no parallel signing path against the same V-signing share).
   - Domain separation between OBFT and any other path that signs against the V-signing share.
   - Slashing protection gates **candidate signing** (Phase-1 leader signing and Phase-2 onion construction across all rounds), **no-σ signing** (Phase-2 IBE partial on `nr_tag_k` across all rounds), and **skip signing** (Phase-2.5 IBE partial on `skip_tag_k` across all rounds), not just submission.

7. **σ-emit signal at deeper layers.** Layer-`k` σ partials at `k ≥ 1` are encrypted under chained-OR — peers cannot directly observe σ-emit at deeper layers. To make the Defer-state rule (defer NR if peer σ-emit observed) work uniformly across layers, operators include a **plaintext σ-emit signal** alongside the encrypted partial: `σ_i^{op}("σ-claim", slot, layer k, value_root)`, an operator-identity-key-signed claim. The signal is not the σ partial itself — it just lets peers count "who σ'd at layer k" without decrypting. Slot-bandwidth cost: ~96 bytes per signal × n operators × K-1 deeper layers, well below practical limits.

8. **Implementation note: skip trigger evidence size.** Equivocation evidence is small (~256 bytes — the pair of equivocating bundles' signatures + value_roots). Persistent-divergence evidence is larger (~qV × 256 bytes — qV `KindLCClaim` messages + their σ_op signatures). At qV=3 n=4: ~768 bytes per skip claim with persistent-divergence trigger, ~256 bytes with equivocation trigger. Within practical sizes; aggregating qEnc skip claims = ~2-3 KB cluster-wide per layer-skip event.

## Where this came from

OBFT is the result of a design exploration starting from baseline [TBFT](TBFT.md), prompted by the question: can TBFT be extended with rounds to recover from network partitions, byzantine equivocation, and validity divergence — *without* QBFT-style leader rotation (which is heavy: ~2s round-change overhead) and *without* Phase 2.5-style cross-signing (which requires EKM relaxation and cross-signer-exclusion logic)?

The first-principles answer was three additions to baseline TBFT:

1. **Deferred-NR commitment** (Defer state): operators don't NR-emit on cutoff if they've observed peer σ-emit cluster-wide — they wait, hoping re-flood completes in a later round. This recovers aggressive-marginal failures (>1 of n−f honest missing re-flood at round-1 cutoff) without breaking cross-phase exclusivity.
2. **L_C cluster-consensus** (`KindLCClaim`): operators broadcast their local view of the cluster's frontier layer; qV agreement promotes L_C cluster-wide, accelerating round transitions and saving bandwidth on dead layers.
3. **Skip mechanism** (`KindSkipClaim` + skip_tag_k + chained-OR encryption): an alternative to NR-quorum for advancing past a layer that's structurally deadlocked (σ-locked operators blocking NR-quorum). Triggered by equivocation evidence (cryptographic) or persistent-divergence (qV `KindLCClaim`-witness consensus). This recovers equivocation single-V receivers and validity divergence under strict host policy — both of which are unrecoverable in baseline TBFT.

The result is a protocol that **matches QBFT's recovery scope** (all single-deadlock failure modes within partial synchrony at f=1) at TBFT's healthy-path latency (2 RTTs), with configurable R (round count) for partial-synchrony tolerance and configurable K (layer count) for multi-failure resilience.

OBFT generalizes baseline TBFT (`R=1, K=2, skip-disabled`) and incorporates structural ideas from TBFTR's chained encryption (which OBFT extends with the OR-tag for skip), Phase 2.5 unlock (which OBFT replaces with cleaner skip-tag mechanism — no cross-signing), and bid-routing variants (which OBFT can compose with as host-supplied leader-determination).

## Appendix A — Protocol comparisons

This appendix gives high-level structural comparisons against the protocols OBFT relates to: baseline [TBFT](TBFT.md) (the special case `R=1, K=2, skip-disabled`), [TBFTR](TBFTR.md) (the K-generic generalization with Phase-2-split machinery), and QBFT (SSV's existing consensus protocol).

### A.1 — Comparison with baseline TBFT

OBFT is a **strict superset** of baseline TBFT: at `R=1, K=2, skip-disabled`, OBFT reduces exactly to TBFT. The added machinery (Defer state, R-round retry, L_C consensus, skip mechanism, chained-OR encryption) extends recovery scope without affecting baseline's properties.

| Aspect | Baseline TBFT | OBFT (R=2, K=2, skip-enabled) |
|---|---|---|
| Phase 1 bundle | `(V, σ^V_L, σ^op_L(envelope))` | Same |
| Phase 2 onion at layer k | `E_{nr_tag_0}(σ_i^V(V_{L_k}))` (single-tag IBE at K=2) | `E_{(nr_tag_0 OR skip_tag_0)}(σ_i^V(V_{L_k}))` — chained-OR IBE |
| Operator commitment states | σ, NR, NV (3 states) | σ, NR-silent, NR-evidence, NV, **Defer** (4 states; Defer is OBFT's addition) |
| Tag count per slot | 1 (`nr_tag_0`) | 2 (`nr_tag_0`, `skip_tag_0`) |
| Phase 2.5 (skip + L_C) | n/a | `KindSkipClaim` + `KindLCClaim` message kinds |
| Round structure | Single-shot (R = 1) | R rounds with re-flood retry |
| Equivocation recovery | Slot misses if single-V receivers split σ | Recovered via skip mechanism in round 2+ |
| Validity-divergence recovery | Slot misses under strict host policy | Recovered via persistent-divergence skip trigger in round 2+ |
| Aggressive marginal (>1 of 3 honest miss) | Slot misses | Recovered via Defer state in round 2 |
| Healthy-path latency | 2 RTTs | 2 RTTs (unchanged) |
| Failure-recovery latency | n/a (slot misses) | ~Δ_round per round (~250 ms typical) |
| Bandwidth (healthy, n=4) | ~21 KB | ~22 KB (+~1 KB for skip+L_C signaling, only on demand) |
| Bandwidth (round 2, n=4) | n/a | +~21 KB for round 2 re-flood + Phase 2'+2.5'+3' |
| DKG ceremony | 2 keypairs (V-share, IBE-share) | Same |

**Migration path**: a cluster running baseline TBFT can adopt OBFT incrementally by enabling the optional features: (1) Defer-state rule via wire signal `σ-emit signal at deeper layers`; (2) round-2 retry; (3) skip mechanism. Each is independently useful; together they provide full QBFT-equivalent recovery. The chained-OR encryption requires a Phase-1 protocol-tag bump (`OBFT-v1` vs `TBFT-v1`) for envelope domain separation.

### A.2 — Comparison with TBFTR

[TBFTR](TBFTR.md) is the K-generic spec with V-plaintext + Phase-2-split machinery at `n ≥ 7`. OBFT and TBFTR share the K-layer onion structure and chained encryption, but diverge on:

| Aspect | TBFTR | OBFT |
|---|---|---|
| K | K-generic (typically `K = ⌈n/2⌉`) | K-generic, configurable per-duty (recommended K=2 for proposer, K≥3 for non-proposer) |
| Phase 2 split | 2a (onion only) + 2b (late σ + NR) | Single Phase 2 + Phase 2.5 (skip + L_C) |
| V-plaintext at deeper layers | Yes — onion carries `V ‖ C_k(σ_partial)` | No — only σ partial encrypted (V_{L_k} learned via Phase-1 broadcast retention; not in onion) |
| Recovery mechanism | Phase 2b late σ-emit (operators who recovered V via peer onions) | Defer state + R-round retry + skip mechanism |
| Equivocation single-V | Single-V receivers handled by re-flood in Phase 2b window; same fundamental limit at f=1 | Recovered via skip mechanism (extends recovery beyond TBFTR) |
| Round structure | Single-shot per slot (no rounds) | R rounds (configurable) |
| Bandwidth | Larger (V-plaintext per layer × n operators) | Smaller per-onion; +R-round retry overhead on demand |

**OBFT replaces TBFTR's Phase-2-split with rounds + skip**, achieving similar recovery scope with cleaner spec structure. The R-round structure subsumes Phase-2b's "late σ-emit" via the same mechanism (re-flood + Defer transition to σ across rounds). The skip mechanism additionally recovers equivocation single-V receivers and validity divergence — both of which TBFTR doesn't cover.

For new SSV deployments, OBFT supersedes TBFT and TBFTR. TBFTR remains a useful reference for the K-generic onion structure analysis.

### A.3 — Comparison with QBFT

QBFT is SSV's existing consensus protocol. The key structural difference: **QBFT separates "decide on a value" (consensus) from "sign the decided value" (post-consensus partial-sig collection)**; OBFT (and TBFT) fuses the two by embedding partial signatures inside Phase-2 onions. Most observable trade-offs trace back to this structural difference.

| Aspect | QBFT | OBFT (R=2, K=2 for proposer) |
|---|---|---|
| Protocol shape | Multi-round (PROPOSE → PREPARE → COMMIT) with per-round leader rotation; round change on timeout | R-round, K-layer onion fall-through; round transitions via timer or L_C consensus |
| Consensus and signing | Decoupled: consensus reaches a decided value, then a separate post-consensus phase collects 2f+1 partial sigs | Fused: Phase-2 onions carry σ partials directly, Phase 3 reconstructs |
| RTTs to signed output (min, healthy) | 3 (consensus) + 1 (post-consensus) ≈ 4 | 2 (Phase 1 + Phase 2) |
| Round-change recovery | Yes — view-change protocol on timeout; ~2s per round at SSV's tuning | Yes — re-flood + late σ-emit + skip; ~250 ms per round |
| Recovery scope | All single-deadlock failure modes within partial synchrony (network partition, equivocation, validity divergence) | Same scope — equivocation and validity divergence recoverable via skip mechanism |
| Termination guarantee | Eventually-terminating across rounds (under partial synchrony) | Conditional on `R · D ≥ real_propagation`; tunable via R |
| Safety | Honest-majority (`2f+1` honest) + correct quorum-certificate verification | Cryptographic via `qEnc = qV = 2f+1` + chained-OR IBE — unconditional, holds against arbitrary network adversary regardless of honest aggregation rules |
| Byzantine-leader-grief resistance | Round-change recovers (slow — ~2s per round timeout) | Closed at flat ~250 ms via re-flood + skip mechanism |
| Equivocation handling | Detected as conflicting prepare/commit votes; round change | Cluster-wide skip emission on equivocation evidence; advances past equivocated layer in next round |
| Validity divergence under strict host | Round-change with new leader at moved head | Persistent-divergence skip trigger; advances past diverged layer in next round |
| Bandwidth (1 round, healthy n=4) | ~14 KB | ~22 KB |
| Bandwidth (1 round failure n=4) | +12 KB per round + a full additional round on top | +~21 KB for round 2 re-flood (only if round 1 failed) |
| Latency (healthy, n=4) | ~750 ms | ~250 ms |
| Latency (1 round failure, n=4) | ~3.0 s (round-1 timeout 2s + round-2 success ~750 ms) | ~500 ms (round 1 fail at ~250 ms + round 2 succeed at ~250 ms) |
| Latency (2 round failures, n=4) | Misses 4s relay cutoff | ~750 ms (round 1, 2 fail; round 3 succeed if R ≥ 3) |
| Slashing-protection scope | Single block sig per slot, gated at submission time | Multiple per-share sigs per slot at candidate-signing (Phase-1 leader, Phase-2 onion across rounds, Phase-2.5 skip emission) gated by EKM cross-keypair coordination |
| Application contract | Decides on any value the application proposes | K-layer with primary + backup-leader fetches |

**OBFT vs QBFT trade-off summary:**

- **Latency**: OBFT wins on every dimension. Healthy: 250ms vs 750ms (3x). Round-1-failure recovery: 500ms vs 3s (6x). Round-2-failure recovery: 750ms vs >4s (QBFT misses relay cutoff).
- **Recovery scope**: Equivalent within partial synchrony at f=1. Both handle network partition, equivocation, validity divergence within their respective per-round budgets.
- **Termination**: OBFT terminates within `R · RT`; QBFT terminates eventually across unbounded rounds (under partial synchrony). For time-bounded duties (proposer's 4s cutoff), both are bounded by the duty's deadline, not by the protocol's intrinsic guarantee.
- **Cryptography**: QBFT only needs BLS threshold signatures. OBFT additionally needs threshold IBE / SWE (drand/tlock-style) and chained-OR encryption (parallel IBE ciphertexts). The IBE primitive is more novel; for risk-averse deployments, this is a real consideration. For green-field deployments where the IBE primitive is acceptable, OBFT's latency profile is decisive.
- **Bandwidth**: Comparable healthy-path; OBFT slightly higher (~50%) due to chained-OR overhead at K=2. Round-failure bandwidth: OBFT lower (~21 KB per recovery round vs QBFT's ~12 KB per round + full additional consensus round).
- **Spec surface**: OBFT is meaningfully larger spec than baseline TBFT (new states, new message kinds, new chained-OR encryption, R-round structure). Comparable spec surface to QBFT once you account for QBFT's view-change protocol, prepared-certificate verification, etc.

**Where QBFT genuinely wins for proposer duty:**

- **Cryptographic primitive simplicity.** QBFT only needs BLS threshold signatures. OBFT needs threshold IBE + chained-OR encryption. The IBE primitive is `drand/tlock`-grade (audited, deployed), but adds a security argument step. For deployments where minimal cryptographic surface is paramount, QBFT is simpler.
- **Production maturity.** QBFT is what SSV runs today. Implementation is exercised, corner cases have been hit, bug fixes have accumulated. OBFT means deriving that confidence on a new codebase. This is not a permanent disadvantage (OBFT can mature too), but for "should we switch?" decisions it's meaningful.

**Where OBFT wins:**

- **Latency.** ~10x faster recovery rounds (~250 ms vs ~2 s). For SSV proposer duty's 4s relay cutoff, this is the difference between catching every recoverable failure within budget vs missing the cutoff on first round failure.
- **Round budget.** With 250 ms rounds, R = 4-5 fits cleanly in the 4s budget — vs QBFT's hard cap at R = 2 (round-1 timeout consumes 2s).
- **Flat failure-mode latency.** OBFT's recovery rounds add ~250 ms each, regardless of failure type (partition, equivocation, divergence). QBFT's round-change adds ~2 s.
- **Configurable per duty.** OBFT's R and K knobs let operators tune per-duty (proposer = `R=2, K=2`; attestation = `R=4, K=3`; DKG = `R=10, K=n`). QBFT has a single round-timeout knob (RT = 2s).

For SSV's proposer duty under the 4s relay cutoff, OBFT decisively wins on latency and recovery budget. For other duties (attestation, sync-committee, DKG) with more generous budgets, the choice depends on the deployment's threat model and complexity tolerance.

## Appendix B — Composable extensions

OBFT's K-generic structure composes cleanly with **host-supplied leader-determination extensions** — selecting which operator gets which layer based on application-supplied criteria (rather than just a deterministic rotation table). The extensions live outside the OBFT core: they affect *which* leaders get assigned to layers L_0, L_1, ..., L_{K-1}, while OBFT's safety/liveness machinery (Defer state, R-round retry, L_C consensus, skip mechanism, chained-OR encryption) operates uniformly across whatever leader assignment the host supplies.

Three example extensions, originally sketched for baseline TBFT and applicable to OBFT with the natural K-generic adaptation. Full design sketches live in [TBFT.md](TBFT.md) Appendix B; this section summarizes their composition with OBFT.

**B.1 — Bid-ordered leader selection.** Each leader attaches a bid value to their Phase-1 envelope; operators commit to whichever layer's bid is highest among locally-validated candidates. Originally specified at K=2 with per-operator commit-tags replacing OBFT's σ-or-NR machinery. Composes with OBFT by using commit-tags as the per-layer commitment primitive (replacing nr_tag_k); OBFT's Defer state, R-round retry, L_C consensus, and skip mechanism apply uniformly. Trade-off: per-operator hedging across layers is sacrificed (each operator commits to *exactly one* layer per slot, the argmax-bid one). For SSV proposer duty, the bid is the relay's `SignedBuilderBid` value — see [TBFT.md](TBFT.md) Appendix B.1 for the full sketch including bid-equivocation handling and post-hoc attribution.

**B.2 — Parent-root-based "ordering" (negative result).** A natural-sounding alternative — letting operators commit to whichever layer's `parent_root` matches their canonical chain — turns out to be a non-extension at K=2 (collapses to baseline behavior) and fragments under head-divergence (the very scenario it would purportedly address). [TBFT.md](TBFT.md) Appendix B.2 has the full negative analysis. The key takeaway carries to OBFT: parent-root-as-filter (used to filter envelopes within a bid layer's input set, as in B.3 below) is productive; parent-root-as-ordering (per-operator routing rule) is not.

**B.3 — L_Bid-prepended OBFT (bid-routing as a top layer).** Prepends an opportunistic bid-routing layer (`L_Bid`) on top of OBFT's rotation-determined K layers, producing a `K' = K + 1` configuration. Layer 0 is bid-determined (highest-bid envelope from any operator who broadcasted in Phase 1); layers 1..K' are baseline OBFT's rotation-determined leaders. Composes with OBFT by using OBFT's chained-OR encryption uniformly across the K'+1 layers. The bid layer's σ-eligibility is conditioned on a cluster-state predicate (saw all bidders' envelopes with parent-root majority filter, OR saw `n-1` with parent-root unanimity), making σ-side participation cluster-consistent. With OBFT's R-round retry + skip mechanism, L_Bid composition inherits the same liveness-recovery scope as the rotation layers. [TBFT.md](TBFT.md) Appendix B.3 has the full sketch including relay-attestation bid binding, cluster-recognition rules for trusted builders, and timing implications.

Under OBFT's application-agnostic framing, these extensions are best read as **examples of plugging an application-supplied selection criterion into OBFT's leader-determination slot**. The criterion (B.1: bid via commit-tags; B.2: parent-root via commit-tags — ruled out; B.3: bid via L_Bid prepended layer) is host-supplied. OBFT's protocol body doesn't enumerate or interpret the criteria; it consumes the resulting layer-to-leader mapping and runs uniformly.


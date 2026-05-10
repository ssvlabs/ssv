# OBFT — Formal Verification

This document specifies the formal verification effort for the OBFT protocol family — bare OBFT, OBFT + L_Bid, and OBFT + L_Bid_New. It defines the threat model, honest behavior, properties under verification, and the TLA+ encoding approach. Verification results are recorded in [§7](#7--verification-results) as TLC runs are performed.

The document is structured to be self-contained: a future contributor (or future you) should be able to pick this up cold and re-run or extend the verification without prior context. Methodology choices are documented inline with rationale so the assumptions can be revisited if deployment conditions change.

---

## 1 — Methodology

### 1.1 — Goals

We want to verify two properties for each of the three OBFT protocol variants:

- **Safety** (`SAFETY`): At most one V signature reconstructs cluster-wide per slot, regardless of byzantine behavior. Holds under both grief and non-grief byz behavior. This is the core cryptographic guarantee — Pigeonholes 1, 2, 3 in the OBFT spec.

- **Liveness — Class A closure** (`LIVENESS_NON_GRIEF`): Under valid OBFT assumptions (1–6) AND no active byzantine grief, every reachable slot terminates with an output (no deadlock). Equivalently: any deadlock that does occur must be attributable to either an assumption violation or active byzantine grief — there are no "hidden" Class A deadlocks the spec failed to document.

These are the two structural properties that matter most for production confidence:

- Safety must hold absolutely; any failure is a critical bug in the design.
- Liveness under non-grief tells us the protocol is fundamentally sound under valid operation; remaining deadlocks (Class B) are the documented byzantine-grief cost the protocol explicitly accepts via assumption 4 (rational-byzantine deterrent).

### 1.2 — Approach

The verification uses **bounded model checking** via TLA+ and TLC:

1. **Specify the protocol formally**: encode each variant's honest state machine, EKM rules, network behavior, and adversary model in TLA+.
2. **Define the properties**: encode `SAFETY` and `LIVENESS_NON_GRIEF` as TLA+ temporal formulas / invariants.
3. **Enumerate reachable states**: use TLC to exhaustively explore the state space at small `n` (= 4) and one production-relevant size (= 7).
4. **Check the properties**: TLC reports either property-holds (verified up to the bounded model) or counterexample (specific execution trace violating the property).

Bounded model checking is preferred over proof-assistant approaches (Coq, Isabelle) because:

- The state space at `n=4, f=1` is small enough to enumerate exhaustively.
- TLA+ specification is more accessible than proof-assistant code; spec is easier to inspect/critique.
- Counterexamples (when found) are concrete execution traces — directly actionable.
- The properties we want are reachability/temporal, not deeply algebraic; TLC's explicit-state model checking is well-suited.

For larger `n` (10, 13), full TLC enumeration is intractable. We argue scalability by symmetry — see [§1.3](#13--scope-and-limitations).

### 1.3 — Scope and limitations

**In scope**:
- `n ∈ {4, 7}`, `f ∈ {1, 2}` cluster sizes.
- Bare OBFT (current spec, no extensions).
- OBFT + L_Bid (current Appendix B extension).
- OBFT + L_Bid_New (Appendix F extension).
- Full Phase 1 / Phase 2 / Phase 3 protocol structure including EKM enforcement of slashing-protection rules.

**Out of scope**:
- Cluster sizes `n ∈ {10, 13}` (state-space explosion). We rely on a structural-symmetry argument: the protocol mechanics are uniform across cluster sizes; if the property holds at `n=4` and `n=7`, the algebraic floor (Lemma F.7 of OBFT.md) generalizes to higher `n` modulo verifiable reasoning. This is a justified extrapolation, not a strict formal proof.
- Multi-slot dynamics (assumption 4's rational-byzantine deterrent operates across slots). We verify per-slot properties only.
- The `Δ_minicon`, `Δ_2`, `Δ_3` specific values (formal verification is at the protocol-logic level, not at concrete timing).
- Cryptographic primitive correctness (we assume BLS threshold and IBE/SWE primitives work as specified).

**Verification confidence level**:
- TLC at `n=4, f=1`: exhaustive within the encoded state space. **High confidence** that any property that holds is actually true at this `n` (modulo encoding bugs).
- TLC at `n=7, f=2`: exhaustive at this size. Same confidence level.
- Extrapolation to `n ∈ {10, 13}`: structural argument. **Medium confidence**. Should be revisited with proof-assistant work if production deployment uses higher `n`.

### 1.4 — Re-running / extending the verification

If deployment assumptions change (different `n`, different f, additional adversarial powers, modified protocol rules), this verification should be re-run. The structure of the work is:

1. **Update the threat model** ([§2](#2--threat-model)) to reflect new assumptions.
2. **Update the honest state machine** ([§3](#3--honest-state-machine)) if protocol rules changed.
3. **Update the TLA+ encoding** in `tla/` (specs already in place; see [§6.1](#61--file-structure)).
4. **Re-run TLC** at the new parameters via `make verify-bare` (or appropriate target).
5. **Update [§7 verification results](#7--verification-results)** with new outputs.

The methodology choices documented in this section should be revisited if:

- The properties under verification change (e.g., wanting to verify Class B exposure characterization in addition to Class A closure).
- The verification tool choice changes (e.g., moving to Coq for parametric proofs).
- The threat model fundamentally changes (e.g., new cryptographic primitives).

---

## 2 — Threat model

### 2.1 — Network model

OBFT operates under **partial synchrony** ([assumption 2 of OBFT.md](OBFT.md#assumed)). Specifically:

- Cluster operates over a libp2p gossipsub mesh.
- All messages broadcast to gossipsub eventually deliver to all subscribed peers within propagation budget `P99 + δ = 1 BTT`.
- **Within a single slot**, mesh propagation can be asymmetric: a message broadcast at time `t` may reach peer A by `t + ε` but peer B by `t + 2 BTT`. The protocol's per-layer staggered budgets `B_k` set the absorption ceiling per layer.
- Beyond the absorption ceiling, slot misses cleanly (Class A — assumption 2 violation).

**Network non-determinism**: For verification, we model the network as choosing (within partial-synchrony constraints) which messages reach which peers by which time. Specifically, for each broadcast message `m` and each peer `p`, the network non-deterministically chooses a delivery time `t_m,p ∈ [t_broadcast, t_broadcast + B_layer]` — or `∞` if `m` is not delivered to `p` within this slot's budget.

This captures both honest mesh asymmetries (broken links, peer-score pruning) and the byzantine-broadcast-via-gossipsub case (byz broadcasts to gossipsub; network determines per-peer delivery).

### 2.2 — Adversary model

We assume up to `f` byzantine operators in a cluster of size `n = 3f + 1`. The byzantine adversary is:

- **Computationally bounded**: cannot break cryptographic primitives (BLS threshold, IBE/SWE, hash, signature schemes).
- **Static**: byzantine operators are fixed at slot start; no Sybil attacks within a slot.
- **Fully observant**: byzantine operators can read all messages on the wire (gossipsub broadcasts, including those between honest peers). This is the standard adversarial-observer model.
- **Coordinated**: the `f` byzantine operators may coordinate their actions (act as a single coalition).

Byzantine operators may take any actions in the action space defined in [§2.3](#23--grief-vs-non-grief) below.

### 2.3 — Grief vs non-grief

We distinguish two byzantine behavior classes:

**Active byzantine grief** (signing-time deviations from honest behavior):

1. **Equivocation**: signing distinct messages at the same `(slot, layer)` — leader equivocation, bid equivocation, verdict equivocation, cross-onion partial-sig equivocation.
2. **Cross-phase exclusivity violation**: σ-side AND NR-side commitment at the same `(slot, layer)`. Bare OBFT enforces strict cross-phase exclusivity per (slot, layer) per operator — any cross-sign is grief and slashable (see [OBFT.md §Slashing evidence Rule 1](OBFT.md#slashing-evidence)).
3. **Fake / garbage signatures**: plaintext σ on V not retained by any honest (Rule 5 in OBFT.md); encrypted partials that decrypt to garbage (Rule 4).
4. **EKM bypass**: signing actions an honest EKM would block — multiple distinct V's at same layer; σ-NR cross-signing within the protocol; etc.
5. **Bid-value lying** (when relay-attestation extension is active): a Phase-1 bundle's bid metadata `bid_value` not matching the relay's attestation.

**Non-grief byzantine behavior** (observable as "operator absent" or "operator following protocol"):

- Silent / offline (no signing, no broadcast).
- Following the honest state machine's transition rules exactly (= behaving honestly even though byzantine).
- Choosing to broadcast or not broadcast honestly-formed messages (= honest behavior + selective broadcast that propagates as gossipsub determines).

Note that **selective delivery** is *not* in either category — it's a network property. The byzantine adversary cannot directly control which honest peers receive which messages from gossipsub broadcasts; that's network non-determinism per [§2.1](#21--network-model).

### 2.4 — Assumption set

OBFT's six explicit assumptions (from OBFT.md `Assumed` section):

| ID | Assumption | Verification handling |
|---|---|---|
| 1 | Standard BFT trust bound (`n = 3f+1`, ≤ f byz) | Modeled directly: byzantine count parameter |
| 2 | Partial synchrony for liveness (`P99 + δ` propagation) | Modeled in network non-determinism (§2.1) |
| 3 | Host validity unanimous at decision time | Modeled by honest validity-locking at first-observation; we assume no re-org during slot for the Class A closure property |
| 4 | Persistent operator set + rational-byzantine deterrent | Out of scope (multi-slot property) |
| 5 | Coordinated EKM across keypair shares | Modeled directly: honest operators have honest EKM enforcing slashing-protection rules |
| 6 | Independent V/IBE keypair DKGs at threshold qV = qEnc = 2f+1 | Modeled directly: threshold parameters |

For the **Class A closure property** verification, we verify that under (assumptions 1, 2, 3, 5, 6 hold) AND (no operator engages in active grief per §2.3), the protocol terminates.

### 2.5 — Verification strategy summary

Putting it together, what we verify is:

```
SAFETY (always):
  ∀ executions of variant V:
    at most one V signature reconstructs cluster-wide per slot.

LIVENESS_NON_GRIEF (under assumption-1-6 + non-grief):
  ∀ executions of variant V where:
    - assumptions 1, 2, 3, 5, 6 hold,
    - no operator performs an action in {grief actions per §2.3},
  the protocol terminates with output ≠ ⊥ (no deadlock).
```

The negation of `LIVENESS_NON_GRIEF` is: "there exists an execution where assumptions hold, no grief, but protocol deadlocks". TLC will surface such executions as counterexamples if they exist.

---

## 3 — Honest state machine

The honest state machine for each protocol variant defines exactly what messages an honest operator emits at each protocol step, given their local state. The complement is the grief action space (§2.3).

This section specifies the honest state machine in TLA+-ready pseudocode. The precise TLA+ encoding lives in `tla/` (see [§6.1 file structure](#61--file-structure)).

### 3.1 — Common state (all variants)

Each operator maintains:

```
operator_id ∈ {1, ..., n}
slot ∈ ℕ
local_view: map(slot → SlotState)

SlotState ::= {
    received_bundles: map(layer → Phase1Bundle) -- Phase-1 bundles retained
                                                  -- (each bundle carries V_{L_k} + bid metadata
                                                  -- in L_Bid variants — no separate KindBid)
    received_verdicts: map(operator → KindBidVerdict) -- only for L_Bid variants
    ekm_log: set((slot, layer, side, value_root)) -- slashing-protection log
    sigma_emitted: map(layer → option(V)) -- σ commitments per layer
    nr_emitted: set(layer) -- NR commitments
    output: option((V, S))
}
```

### 3.2 — Bare OBFT honest state machine

**Phase 1 — Honest leader L_k:**

```
ACTION leader_broadcast(operator i, layer k):
    PRECONDITION: i is the rotation leader for L_k AND
                  current_time ≥ T_{k} AND current_time ≤ T_broadcast_max_k AND
                  not yet broadcast for (slot, k).
    
    1. Run host fetch loop until V_k is determined (host application).
    2. Validate V_k against host rules.
    3. Sign σ_i^V(V_k) via EKM at (slot, k, "σ", value_root(V_k)).
       EKM rejects if any prior (slot, k, _, _) entry exists.
    4. Sign σ_i^op(envelope) via operator-identity key.
    5. Broadcast (V_k, σ_i^V, σ_i^op) to gossipsub.
    6. Update sigma_emitted[k] := Some(V_k).
```

**Phase 1 — Honest receiver:**

```
ACTION receive_bundle(operator i, message (V, σ_L^V, σ_L^op)):
    PRECONDITION: first observation of this bundle for (slot, k, leader_id) AND
                  current_time ≤ T_commit AND
                  envelope auth verifies AND σ_L^V verifies as valid threshold partial AND
                  no second distinct V observed for this (slot, k, leader_id) [retention rule].
    
    1. Run host validity check on V against locked head snapshot.
    2. If valid: retain (V, σ_L^V) in local_view.received_bundles[k].
    3. If invalid: mark V as NV (operationally identical to NR).
    
    Equivocation handling: if a distinct V' observed for same (slot, k, leader_id),
    retain both V and V' in retention (slashable evidence) but mark layer as
    "equivocation observed" → operator will NR at this layer at T_commit.
```

**Phase 2 — Honest commit at T_commit:**

```
ACTION emit_kindcommit(operator i):
    PRECONDITION: current_time = T_commit AND not yet emitted KindCommit for slot.
    
    For each layer k ∈ [0, K):
        If sigma_emitted[k] = Some(V_k) AND not equivocation-observed at k:
            Include σ_i^V(V_k) in onion (encrypted under nr_tag chain for k > 0).
        Else:
            Sign σ_i^IBE(nr_tag_k) via EKM at (slot, k, "NR", null).
            EKM rejects if any prior (slot, k, _, _) entry exists.
            Include in NR-partials section.
    
    Include sigma_L_witnesses section: a list of (layer_k, value_root_k,
    σ_{L_k}^V) tuples for each layer k where i retained the Phase-1 σ_L^V
    partial. Per-witness ≈ 145 bytes; cluster-wide bandwidth at K=4, n=4
    ≈ 2.3 KB. See [OBFT.md §Phase 2 / Wire format](OBFT.md#phase-2--onion-broadcast-t_commit-t_commit--%CE%94_2).
    No new EKM event, no new signing obligation (operators forward bytes
    they already received from Phase 1).
    
    Sign envelope auth via operator-identity key.
    Broadcast KindCommit to gossipsub.
```

**Phase 3 — Honest reconstruction:**

```
ACTION reconstruct(operator i):
    PRECONDITION: current_time ≥ T_commit + Δ_2.
    
    For k from 0 to K-1:
        sigs[k] := union of (
            σ_L^V from retained Phase-1 bundle at k (if any),
            σ_L^V extracted from peer KindCommit sigma_L_witnesses sections at k,
            decrypted σ partials from peer KindCommit onion contents at k 
              (decryptable via accumulated NR-quorum keys at layers 0..k-1)
        ), deduplicated per operator.
        
        nrs[k] := union of (
            σ_j^IBE(nr_tag_k) partials from peer KindCommit NR-partials sections
        ), deduplicated per operator.
        
        IF ∃V: |{partials on V in sigs[k]}| ≥ qV:
            S := reconstruct V signature from those partials.
            output := Some((V, S)).
            Broadcast KindCertificate; halt.
        
        ELSE IF |nrs[k]| ≥ qEnc:
            decryption_key_k := aggregate(nrs[k]).
            Continue to k+1.
        
        ELSE:
            Halt (deadlock; slot misses for this operator).
```

### 3.3 — OBFT + L_Bid extensions to the honest state machine

L_Bid extends bare OBFT's Phase-1 bundle with bid metadata and adds the mini-consensus sub-phase. Per [Appendix B of OBFT.md](OBFT.md#appendix-b--l_bid-mini-consensus-extension), bid data lives inside Phase-1 bundles — there is no standalone `KindBid` wire kind. Only rotation leaders bid (one bid per layer leader); non-rotation operators participate in mini-consensus by validating retained eligible bundles and broadcasting verdicts.

**Phase 1 — Honest leader L_k broadcasts Phase-1 bundle with bid metadata:**

```
ACTION leader_broadcast_with_bid(operator i, layer k):
    PRECONDITION: i is the rotation leader for L_k AND
                  current_time ≥ T_{k} AND current_time ≤ T_broadcast_max_k AND
                  not yet broadcast for (slot, k).
                  (T_broadcast_max_k = T_commit − B_k for bare OBFT;
                   T_broadcast_max_k = T_0_arrival − B_k_LBid for L_Bid.)
    
    1. Run host fetch loop until V_k is determined; validate against host rules.
    2. Determine bid_value and obtain relay_attestation (or empty if extension off).
    3. Sign σ_i^V(V_k) via EKM at (slot, k, "σ", value_root(V_k)).
    4. Sign Phase-1 envelope via operator-identity key, including the bid metadata
       (bid_value, relay_attestation) for the same V_k.
    5. Broadcast (V_k, σ_i^V, σ_i^op, bid_value, relay_attestation) — a single
       Phase-1 bundle that is both the L_k candidate and the L_Bid bid.
```

**Mini-consensus — verdict broadcast:**

```
ACTION emit_kindbidverdict(operator i):
    PRECONDITION: current_time = T_verdict = T_commit − Δ_verdict AND
                  not yet emitted verdict for slot.
    
    1. Compute bid_set_i := L_Bid-eligible Phase-1 bundles first-observed before
       T_verdict that pass:
       - bundle cryptographic checks (σ^V partial verifies, op-identity envelope verifies),
       - bid metadata is well-formed (bid_value, relay_attestation),
       - host validity-locked verdict on V is valid,
       - optional filters (parent-root, relay-attestation extension if active).
    
    2. Compute predicted_LBid_i:
       IF |bid_set_i| ≥ qBid (= K − f for K = n; configured otherwise) AND
          optional filters pass:
           predicted_LBid_i := argmax_{V in bid_set_i} bid_value
                               (with (layer, leader_id) tiebreak)
       ELSE:
           predicted_LBid_i := null
    
    3. Construct KindBidVerdict binding the prediction.
    4. Sign envelope via operator-identity key.
    5. Broadcast.
```

**Phase 2 — L_Bid commit:**

```
ACTION emit_kindcommit (extended for L_Bid):
    Same as bare OBFT Phase 2 + L_Bid layer:
    
    For L_Bid:
        IF verdict_pool[V_X] ≥ qV (computed locally) AND
           operator retains V_X locally (= retained the Phase-1 bundle whose
                                          metadata bid V_X) AND
           operator's locked validity verdict for V_X is valid:
            Include σ_i^V(V_X) plaintext at L_Bid.
        ELSE:
            Sign σ_i^IBE(nr_tag_LBid) via EKM at (slot, "L_Bid", "NR", null).
    
    For L_0..L_{K-1}: same as bare OBFT, but onion encryption now also wraps
    rotation layers under nr_tag_LBid (outermost gate).
```

### 3.4 — OBFT + L_Bid_New extensions

L_Bid_New differs from L_Bid in three places (per [Appendix F of OBFT.md](OBFT.md#appendix-f--obft--l_bid_new-deep-bid-mini-consensus)):

**Per-layer broadcast deadlines split by `T_deep_arrival` vs primary's bare-OBFT deadline:**

```
ACTION leader_broadcast_with_bid (L_Bid_New, layer k):
    For deep layers k ≥ 1: T_broadcast_max_k = T_deep_arrival − B_k.
                            (T_deep_arrival = T_commit − Δ_minicon.)
    For primary k = 0:      T_broadcast_max_0 = T_broadcast_max_0^bare
                            (= bare OBFT's primary deadline; no Δ_minicon shift).
    
    Otherwise identical to L_Bid's leader_broadcast_with_bid: Phase-1 bundle
    carries bid metadata for the same V_k; σ_{L_k}^V signed via EKM; envelope
    signed via op-identity key.
```

**Mini-consensus runs over deep bids only:**

```
ACTION emit_kindbidverdict (L_Bid_New):
    Same as L_Bid but bid_set_i_deep := L_Bid-eligible Phase-1 bundles for
    layers L_1..L_{K-1} only — primary's bundle (bid_1 = V_{L_0}) is intentionally
    excluded.
    
    predicted_LBid_i computed over bid_set_i_deep with visibility threshold
    qBid_deep (= (K − 1) − f for K = n; configured otherwise).
```

**Phase 2 — Priority-inverted onion:**

```
ACTION emit_kindcommit (L_Bid_New):
    Onion structure:
        layer L_Bid (V_early, plaintext): σ_i^V(V_early) if rule below
        layer L_0 (bid_1, encrypted under nr_tag_LBid): σ_i^V(bid_1)
        layer L_k (k ≥ 1): chained-encrypted as bare OBFT
    
    L_Bid σ-or-NR rule (σ-when-uncertain variant):
        σ on V_early IF: verdict_quorum_V_early reached AND
                         operator retains V_early AND
                         locked validity verdict valid AND
                         (operator does NOT have bid_1 OR bid_1 ≤ V_early).
        
        NR on nr_tag_LBid otherwise.
    
    L_0 σ-or-NR rule: bare OBFT (σ on bid_1 if received and host-valid; else NR).
```

### 3.5 — Grief actions (complement of honest state machine)

A byzantine operator MAY perform any of the actions in §3.2–§3.4 (= behave honestly), or any of the following grief actions:

```
GRIEF_LeaderEquivocate(i, k): emit two distinct Phase-1 bundles at (slot, k) with
                              different value_root, both signed with σ_i^V.
                              In L_Bid variants, this also covers bundle-level
                              equivocation that produces distinct V's at the same
                              (slot, layer, leader).

GRIEF_BidMetadataEquivocate(i, k): emit two distinct Phase-1 bundles at (slot, k)
                                    that carry the SAME V_k but different bid metadata
                                    (bid_value and/or relay_attestation). L_Bid-specific
                                    Rule 7 grief; not applicable to bare OBFT.

GRIEF_VerdictEquivocate(i): emit two distinct KindBidVerdicts at slot. Applicable
                            only to L_Bid variants.

GRIEF_CrossSign(i, k): emit σ_i^V at L_k AND σ_i^IBE(nr_tag_k) at same (slot, k).
                       Bare OBFT enforces strict cross-phase exclusivity per
                       (slot, layer) per operator; any cross-sign is grief and
                       slashable (Rule 1).

GRIEF_FakePartial(i, k, V_garbage): emit σ_i^V on a V not retained by any honest
                                     (at the cluster's plaintext layer).

GRIEF_FakeEncryptedPresence(i, k): emit encrypted onion entry at L_k that decrypts
                                    to garbage when chained encryption unlocked.

GRIEF_BidValueLie(i, k): emit Phase-1 bundle whose bid metadata bid_value does not
                         match the relay's attestation (if attestation extension active).

GRIEF_EKMBypass(i, action): perform any signing action an honest EKM would reject.
```

**Class A closure property formally**: under the constraint that no operator performs any GRIEF_* action AND assumptions 1-6 hold, the protocol does not deadlock.

---

## 4 — Section A: Safety property

### 4.1 — Statement

```
SAFETY: ∀ slot S, ∀ executions E of variant V (including any byz grief actions):
    at most one V signature reconstructs cluster-wide per slot.
```

Equivalently, per Pigeonholes 1, 2, 3 in OBFT.md:

- **Pigeonhole 1** (σ vs NR at same layer): σ-quorum on any V at L_k AND NR-quorum on nr_tag_k cannot both reach.
- **Pigeonhole 2** (two σ-quorums on different V's at same layer): for any layer L_k, at most one V satisfies σ-quorum.
- **Pigeonhole 3** (cross-layer chained-encryption): for any pair of layers L_j, L_k with j ≠ k, V_j and V_k cannot both reconstruct cluster-wide.

### 4.2 — Verification approach

For each variant V ∈ {bare OBFT, OBFT+L_Bid, OBFT+L_Bid_New}:

1. Encode the protocol state machine including byzantine action space (honest actions + grief actions per §3.5).
2. Encode the cluster pool as `{ op : op signed σ on (k, v) }`, representing the worst-case offline aggregator's set (a byzantine that retains all observed partials cluster-wide).
3. Define `SAFETY` as the conjunction of three Pigeonhole invariants:
   ```
   Pigeonhole1 ≡ ∀ k:
     ¬ ( (∃v: |SigmaPool(k, v)| ≥ qV)
         ∧ |NRPool(k)| ≥ qEnc )
   Pigeonhole2 ≡ ∀ k:
     |{v : |SigmaPool(k, v)| ≥ qV}| ≤ 1
   Pigeonhole3 ≡
     |{(k, v) : Reconstructable(k, v)}| ≤ 1
   SAFETY ≡ Pigeonhole1 ∧ Pigeonhole2 ∧ Pigeonhole3
   ```
   where `Reconstructable(k, v)` requires σ-quorum on `v` at `k` AND NR-quorum at every layer `0..k-1` (chained-encryption gating).
4. Run TLC with `INVARIANT SAFETY` at `n=4, f=1` (base case) and partial-coverage at higher K and `n=7, f=2`.

**Pigeonhole 1 union-bound argument.** Bare OBFT enforces strict cross-phase exclusivity per (slot, layer) per honest operator (single σ-or-NR commitment, EKM-gated). At `n = 3f+1`, the cluster has at most `2f+1` honest contributors and at most `f` byzantine. For any layer `k`:

- If σ-quorum on some `v` reaches qV = 2f+1, then ≥ f+1 honest signed σ on `v` at `k` (since byz cap ≤ f). Those f+1 honest cannot also contribute NR at `k` by cross-phase exclusivity. NR-pool at `k` ≤ (remaining honest) + (byz cap) = `(2f+1 - (f+1)) + f = 2f < qEnc`.

So σ-quorum and NR-quorum cannot co-exist at the same layer. Pigeonhole 2 (single-V σ-quorum per layer) holds by single-σ-V EKM exclusivity plus byz cap (`≤ 4f+1 < 2·qV`). Pigeonhole 3 (cross-layer single-output) follows by chained-encryption induction over Pigeonhole 1 at each layer.

Expected result: TLC verifies SAFETY for bare OBFT at K=2 (full coverage at small `|Values|`) and partial-coverage at higher K and `n=7, f=2` with no counterexample. If counterexample found: spec body's algebraic argument has a flaw (critical bug).

### 4.3 — Cryptographic-primitive abstraction

We model BLS threshold reconstruction as: a V signature reconstructs cluster-wide iff `≥ qV` partials on V exist in the cluster signed-message-set. The TLA+ model tracks partials as abstract signed-message structures; it does NOT verify the BLS scheme itself (we trust that as a primitive).

Similarly for IBE/SWE chained encryption: layer L_{k+1}'s σ partials are "decryptable" iff `≥ qEnc` NR-partials on nr_tag_k exist in the cluster signed-message-set.

This abstraction is justified because:
- BLS threshold and IBE/SWE primitives are independently audited (drand/tlock).
- The OBFT spec's Pigeonhole proofs are at the partial-counting level, not at cryptographic-primitive level.
- TLC verifies the partial-counting algebra under cluster-pool aggregation against unrestricted byzantine action (honest + grief), which is what determines safety.

---

## 5 — Section B: Liveness property (Class A closure)

### 5.1 — Statement

```
LIVENESS_NON_GRIEF: ∀ slot S, ∀ executions E of variant V where:
    - assumption 1 holds (≤ f byz operators in cluster of size n = 3f+1),
    - assumption 2 holds (all messages propagate within budget P99 + δ per layer),
    - assumption 3 holds (host validity unanimous at decision time),
    - assumption 5 holds (honest EKM correctly enforces),
    - assumption 6 holds (DKGs valid),
    - no operator performs any GRIEF_* action (per §3.5),
  
  the protocol terminates with output ≠ ⊥ at slot S (no deadlock).
```

This is the **Class A closure**: every deadlock must be either (a) attributable to assumption violation OR (b) caused by an active grief action. There are no "honest-operation" deadlocks.

### 5.2 — Verification approach

For each variant V:

1. Encode the protocol state machine WITHOUT grief actions in the byzantine action space — byzantine operators are restricted to either following the honest state machine or being silent/offline. Honest leaders broadcast their bundle to all under within-budget propagation (Assumption 2).
2. Encode network non-determinism (mesh asymmetry within partial-synchrony bound).
3. Define `LIVENESS_NON_GRIEF` as a TLA+ temporal formula:
   ```
   LIVENESS_NON_GRIEF ≡
     □(assumptions_hold ∧ no_grief) ⇒ ◇ ∀ i ∈ Honest: output_set[i] = TRUE
   ```
4. Run TLC with the temporal property at `n=4, f=1, K=4` and (where tractable) `n=7, f=2`.

Expected result (per the OBFT spec's Class A list, [OBFT.md §Liveness](OBFT.md#liveness-synchrony-conditional)): TLC verifies for all three variants — no Class A leakage that the spec body fails to characterize. If a counterexample surfaces, treat it as a Class A failure mode that needs to be either documented (assumption refinement) or fixed (protocol change).

**Verification result for bare OBFT at n=4, f=1, K=4** (see §7.1): TLC verified `LIVENESS_NON_GRIEF` across the reachable state space (64,152 distinct states, depth 12, ~10s runtime). Under non-grief byz behavior + within-budget partial-synchrony, every honest operator reaches `output_set[i] = TRUE`.

**What the verified property covers** (= what bare OBFT recovers under non-grief + within-budget partial-synchrony):

- Healthy slots at any layer where the leader's bundle reaches all honest by `T_commit` (σ-quorum reaches at that layer).
- Silent leader at any layer (NR-quorum reaches → fall-through to next layer in the Phase-3 walk).
- Honest leader's late bundle that nonetheless reaches all honest before `T_commit` (within-budget propagation tail, absorbed by per-layer budget `B_k`).

**What's out of scope of this property** (excluded by precondition; surfaced under SAFETY's grief-byz model or §5.4's relaxed-Assumption-2 model):

- **`h_V = 1` selective-Phase-1 delivery** (byz leader unicasts `V_L` to exactly one honest non-leader). At `n=4, f=1` this gives σ-pool = 1 honest + leader's σ_L^V = 2 < qV; NR-pool = 2 < qEnc — algebraic deadlock. **Excluded as Class B grief** (byz selective broadcast is grief category 1); deterred via Assumption 4 across slots.
- **Equivocation σ-locked split patterns** (1-1-1, 1-1-NR): some honest σ-lock on different V's before observing equivocation; neither σ-quorum nor NR-quorum reaches; no fall-through. **Excluded as Class B grief** (byz equivocation is grief category 1); equivocation is cryptographically slashable (Rule 2).
- **Validity-divergence beyond the host's stabilization window**: **excluded as Class A** (Assumption 3 violation).

See [OBFT.md §Failure modes](OBFT.md#failure-modes) for the full taxonomy and [§5.4](#54--beyond-liveness_non_grief-class-a-failure-mode-exploration) for a TLA exploration that surfaces a related Class A pattern by relaxing Assumption 2 (honest leader's bundle reaches a strict subset of honest — same algebraic shape as the byz-grief h_V=1 above, but caused by network tail rather than byz selective delivery).

**Scope of the n=4 verification result.** The verified configuration is `n=4, f=1, K=4`. The verification space includes:
- All injective leader-assignment patterns (byz at any rotation position),
- Honest σ / NR / silent choices per layer,
- Byzantine non-grief actions (silent / honest-mimicking / σ-or-NR-or-silent commitment).

At this configuration, every honest operator reaches an output under non-grief + within-budget propagation. The verification does not extend to grief actions (which is the SAFETY spec's domain) and does not extend to beyond-budget partial-synchrony (assumption 2 violation, out of scope). Cluster slot success at `n ≥ 7` extends by structural symmetry: the union-bound argument for σ-vs-NR mutex (§4.2) is independent of `n`, and the protocol's per-layer fall-through mechanics scale uniformly across honest non-leader counts.

### 5.3 — Mesh asymmetry modeling

The network non-determinism in §2.1 must be carefully bounded for the verification to be meaningful:

- **Within-budget asymmetry**: each message reaches each subscribed peer within `B_layer` of broadcast. This is the protocol-spec-allowed asymmetry; the protocol must terminate under this.
- **Beyond-budget asymmetry**: messages take longer than `B_layer`. This is assumption-2 violation; out of scope for the Class A closure property.

The TLA+ model exposes a parameter `mesh_asymmetry ∈ {within_budget, beyond_budget}` and verifies the property only under `within_budget`. Beyond-budget executions are excluded by the assumption-2 precondition.

### 5.4 — Beyond LIVENESS_NON_GRIEF: Class A failure mode exploration

The verified `LIVENESS_NON_GRIEF` property holds under within-budget partial-synchrony (Assumption 2) + non-grief byz. Real-world deployments will see Assumption 2 violations — propagation tails past `B_k` budget at one or more honest receivers, mesh churn, peer-score pruning. This subsection documents a complementary TLA exploration that *relaxes* Assumption 2 and surfaces the Class A deadlock patterns the spec explicitly does not close.

**Methodology.** A sibling spec `tla/BareOBFT_Liveness_NoBudget.tla` is identical to `BareOBFT_Liveness.tla` except for one change — `HonestLeaderBroadcast(k)` takes an additional parameter `S \subseteq Operators` (with `leader_of[k] \in S`) instead of hardcoding `delivered_to[k] = Operators`. All other actions, properties, and assumptions are unchanged. The property `LIVENESS_NON_GRIEF` is checked unmodified — but **expected to fail**, with each counterexample serving as a diagnostic trace of one Class A failure mode.

The relaxation generalizes the network model:

- `S = Operators` recovers the within-budget case (= `BareOBFT_Liveness.tla`'s axiom).
- `leader ∈ S ⊊ Operators` models propagation tail (= one or more honest didn't retain within budget).
- `S = {leader}` models effective silence past `T_commit` (= the leader retains their own bundle, no peer received in time).

Byz behavior remains unchanged (non-grief: all-or-silent broadcast for byz leader, σ/NR/none commitment, no equivocation/cross-sign/fake/EKM-bypass). This isolates any counterexample's *cause* to honest-leader-propagation issues compounded with byz non-grief options (silence, NR), not to byz grief.

**Run command**:

```bash
cd tla && ./scripts/tlc-run.sh BareOBFT_Liveness_NoBudget
```

**Expected outcome**: TLC finds counterexamples (= traces leading to deadlock). Each counterexample classifies as one of the documented Class A patterns from [OBFT.md §Failure modes](OBFT.md#failure-modes). A counterexample that does *not* match a documented pattern is a spec-coverage gap to investigate.

**Findings at n=4, f=1, K=2** (2026-05-10): TLC found a counterexample at depth 6 in 7 seconds (~183K states generated; 86,166 distinct states explored before the violation surfaced). The trace is a documented partial-propagation deadlock from the broader `h_V=1` family (per [OBFT.md §Liveness fat warning (b)](OBFT.md#liveness-synchrony-conditional)): an honest leader's bundle reaches a strict subset of honest peers, leaving σ-pool < qV and NR-pool < qEnc with no fall-through. Cause is honest-network propagation tail (Assumption 2 violation), not byz selective delivery.

**Trace** (Operators = {op1, op2, op3, op4}, Byzantine = {op4}, exact specifics may vary slightly across TLC runs but the algebraic shape is invariant):

```
State 1 (Init):  leader_of = (L_0 → op1, L_1 → op2)
                 byz_commit = (op4 → "sigma" at L_0, "sigma" at L_1)
State 2: HonestLeaderBroadcast — L_0's bundle delivered to {op1 (leader), op3}
                 → propagation tail: op2, op4 didn't retain V_0
State 3: HonestLeaderBroadcast — L_1's bundle delivered to {op2 (leader), op3, op4}
                 → propagation tail: op1 didn't retain V_1
State 4-6: HonestEmitKindCommit — op1, op2, op3 emit KindCommit
State 7: STUTTER. No action enabled. output_set = ⟨all FALSE⟩.
                 Byz op4 chose σ-commit at Init but never emitted KindCommit
                 (silent — non-grief option allowed).
```

Final pool composition at L_0 (the load-bearing layer):

- `SigmaPoolEmitted(0) = {op1, op3}` (size 2 < qV=3 — the 2 honest who retained, both emitted)
- `NRPoolEmitted(0) = {op2}` (size 1 < qEnc=3 — the only honest non-retainer who emitted)
- `SilentEmitted = {op4}` (size 1 — byz chose σ but never emitted)

Neither σ-quorum nor NR-quorum reaches at L_0; chained encryption stays sealed; the cluster cannot fall through to L_1. Slot misses cleanly with no double-sign (Pigeonhole 1 holds — the deadlock is a liveness loss, not a safety loss).

This trace is one instance of the broader partial-propagation deadlock family the OBFT.md §Liveness fat-warning callout describes (the layer's leader bundle reaches a strict subset of honest, leaving each side of the σ-vs-NR split below its quorum threshold — receivers ∈ (n−qEnc, qV) at f=1, n=4 means exactly 2 receivers). The byz-silent variant surfaced here is one of several blocking shapes — close cousins include the classical `h_V=1` (byz leader unicasts to 1 honest, leader's σ_L^V completes σ-pool to 2; remaining 2 honest NR but byz can NR or stay silent), and the byz-NR-mimicking variant (byz emits NR to look like a non-retainer). All deadlock by the same algebra.

**What this exploration validates**:

- The "bare OBFT does not close partial-propagation deadlocks in-protocol" claim is verified mechanically — the TLA model under relaxed Assumption 2 surfaces exactly the algebraic deadlock the spec body describes.
- The Class A scope as documented in OBFT.md is the right boundary — TLC at K=2 found exactly that pattern, no surprises.

**What this exploration does NOT exhaustively validate** (future work):

- **Multi-layer compound failures at K ≥ 3.** The K=2 trace shows L_0 deadlock; L_1 also deadlocked but is moot since the cluster outputs at the first quorum-reaching layer. K ≥ 3 might surface compound failures where multiple layers' fall-through paths simultaneously block.
- **Validity-divergence (re-org during slot)**. Modeling re-org would require extending the spec with per-honest validity verdicts. Out of scope for this iteration; deserves a separate spec.
- **Sustained partition (real propagation > slot budget for ALL layers)**. The current spec's relaxed delivery covers per-layer propagation tails but not multi-layer total partitions; this would correspond to all `delivered_to[k] = {leader_of[k]}` consistently across all layers — a degenerate case the spec already documents as Class A.
- **Iterative pattern enumeration**. TLC halts at the first counterexample. To enumerate all distinct Class A patterns, we would iteratively block found patterns (state constraint or property modification) and re-run.

**State-space caveat**. Relaxed delivery adds 2^n choices per honest leader broadcast. At n=4, K=2 the model explored 86K distinct states before the first violation; full coverage at higher K may need state-constraint pruning analogous to the SAFETY spec's `StateConstraint`. K=4 is conjectured tractable with state pruning but not yet run.

---

## 6 — TLA+ encoding sketch

This section sketches the structure of the TLA+ models. Actual `.tla` files live in `tla/` directory (see [`tla/README.md`](../tla/README.md) for run instructions).

### 6.1 — File structure

```
tla/
├── BareOBFT_Safety.tla       -- bare OBFT SAFETY (Pigeonholes 1, 2, 3) under
│                                cluster-pool aggregation with full byz grief.
├── BareOBFT_Safety.cfg       -- K=2, |Values|=2 base case.
├── BareOBFT_Liveness.tla     -- bare OBFT LIVENESS_NON_GRIEF (Class A closure)
│                                under non-grief byz + within-budget partial-
│                                synchrony.
├── BareOBFT_Liveness.cfg     -- K=4 (full layer count for 4-op cluster).
├── BareOBFT_Liveness_NoBudget.tla -- sibling of BareOBFT_Liveness.tla with
│                                relaxed Assumption 2 (honest leader broadcast
│                                may deliver to any subset). Used for Class A
│                                failure-mode exploration (§5.4).
├── BareOBFT_Liveness_NoBudget.cfg -- K=2.
├── LBid_Safety.tla           -- OBFT + L_Bid SAFETY (Pigeonholes + verdict
│                                pigeonhole).
├── LBid_Safety.cfg
├── LBidNew_Safety.tla        -- OBFT + L_Bid_New SAFETY, same invariants
│                                encoded for L_Bid_New's structural differences.
├── LBidNew_Safety.cfg
├── Makefile                  -- declarative verify-* targets.
├── scripts/
│   └── tlc-run.sh            -- runner script (captures log + summary file).
├── README.md                 -- methodology + run instructions.
└── runs/                     -- per-run logs and summaries (gitignored).
```

### 6.2 — Key TLA+ idioms

**Cluster pool.** Safety verification tracks the cluster signed-message-set as `sigma_partials` and `nr_partials` — each entry is a per-(operator, layer, value) or per-(operator, layer) tuple representing one published partial. The cluster pool at layer `k` for value `v` is `{op : (op, k, v) ∈ sigma_partials}`. This represents the worst-case offline aggregator's view (a byz that retains every observed partial cluster-wide).

**Byzantine action space.** Byzantine operators may perform any honest action (= mimicking honest behavior), be silent, or perform any of the GRIEF_* actions per §3.5 — equivocation, cross-signing, fake partials, EKM bypass. The model encodes both the protocol-faithful and the grief actions; SAFETY must hold under the union of both spaces.

**Honest emission.** Honest operators sign at most once per (slot, layer) per side, EKM-gated (cross-phase exclusivity per layer; single-σ-V per layer). Honest emissions append to the cluster pool deterministically.

**Byzantine partition**: `Byzantine ⊆ Operators` (constant), `Cardinality(Byzantine) = F = (Cardinality(Operators) - 1) / 3`.

**Symmetry reductions**: `SYMMETRY Permutations(Honest)` reduces the canonical state count by `|Honest|!` (= 6× at n=4, f=1). Byzantine operators are NOT permuted (they're distinguished by Byzantine designation). Values are NOT permuted (Pigeonhole 2's "two distinct V's reach qV" check needs to count per-(layer, V) σ commitments). Symmetry is enabled for SAFETY but disabled for LIVENESS (TLC warns symmetry under liveness checking can miss violations).

**State-space cap**: `StateConstraint` bounds each pool at its quorum threshold (`Cardinality(SigmaPool(k, v)) ≤ qV` and `Cardinality(NRPool(k)) ≤ qEnc`). Provably safe for SAFETY because the Pigeonhole invariants are stated as "pool size ≥ threshold" predicates, so they're already detectable when a pool first reaches the threshold; states with pool size > threshold add no new safety information.

### 6.3 — TLC configuration

For each `(n, f, K)` triple, TLC config specifies:
- `CONSTANTS`: `Operators`, `Byzantine`, `K`, `Values` (for Safety; Liveness omits Values since each layer has implicit V_k).
- `INVARIANT`: `TypeOK ∧ SAFETY` (Safety specs).
- `PROPERTY`: `LIVENESS_NON_GRIEF` (Liveness specs).
- `SPECIFICATION`: `Spec = Init ∧ □[Next]_vars`.
- `SYMMETRY`: `Permutations(Honest)` (Safety only).
- `CONSTRAINT`: `StateConstraint` (Safety only).
- `CHECK_DEADLOCK FALSE` (specs naturally terminate; the property of interest is the invariant).

Verified state-space sizes (per [§7.1](#71--bare-obft)):
- bare OBFT Safety, `n=4, f=1, K=2, |Values|=2`: 262,144 distinct states, ~10s.
- bare OBFT Liveness, `n=4, f=1, K=4`: 64,152 distinct states, depth 12, ~10s.
- bare OBFT Liveness (relaxed Assumption 2), `n=4, f=1, K=2`: 86,166 distinct states, counterexample at depth 6, ~7s.
- L_Bid SAFETY at `n=4, f=1, K=2`: 76.8M distinct states (partial at 20-min budget). L_Bid_New SAFETY: 78.8M distinct states (partial). Algebraic argument from bare OBFT extends.
- `n=7, f=2`: not yet run; tractability conjectured via cap-at-quorum + symmetry, runtime estimated as hours rather than minutes.

### 6.4 — Counterexample handling

If TLC finds a counterexample to either property:

1. Inspect the trace — sequence of states leading to violation.
2. Determine: real bug vs encoding error.
3. If real bug: file as critical and update OBFT.md spec.
4. If encoding error: refine TLA+ model and re-run.

Counterexamples are stored in `tla/counterexamples/<variant>-<property>-<config>.txt` for archival.

---

## 7 — Verification results

This section is updated as TLC runs are performed.

### 7.1 — Bare OBFT

| Property | Config | Status | Date | Notes |
|---|---|---|---|---|
| SAFETY | n=4, f=1, K=2, \|Values\|=2 | ✓ verified | 2026-05-08 | TLC explored 262,144 distinct states (1.96M total) in 10s; no counterexamples. All three Pigeonholes hold for bare OBFT. |
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (with state constraint capping pool sizes at quorum thresholds) | ✓ verified | 2026-05-08 | Re-run with the cap-at-quorum state constraint as a safety sanity check before applying the constraint to L_Bid / L_Bid_New. TLC explored 250,000 distinct states (1.91M total) in 7s; same outcome (no counterexamples). |
| LIVENESS_NON_GRIEF | n=4, f=1, K=4 | ✓ verified | 2026-05-10 | TLC verified Class A closure under non-grief byz + within-budget partial-synchrony: every honest operator eventually reaches `output_set[i] = TRUE`. 139,482 states generated, **64,152 distinct states found**, depth 12, ~10s runtime. **Scope**: under within-budget propagation and non-grief byz, every layer either reaches σ-quorum or NR-quorum, so the Phase-3 walk completes with output. **Does NOT recover** (out of property scope, surfaced as documented Class A/B in OBFT.md §Failure modes): `h_V = 1` selective Phase-1 delivery, equivocation σ-locked split patterns, validity-divergence beyond stabilization. See [§5.2](#52--verification-approach) for full scope. |
| LIVENESS_NON_GRIEF | n=4, f=1, K=2 (**relaxed Assumption 2** — honest leader broadcast may deliver to any subset; sibling spec `BareOBFT_Liveness_NoBudget.tla`) | ✗ counterexample (expected) | 2026-05-10 | TLC found Class A deadlock at depth 6 in 7s (~183K states generated, **86,166 distinct states found** before first violation). Trace replays a documented partial-propagation deadlock from the `h_V=1` family ([OBFT.md §Liveness](OBFT.md#liveness-synchrony-conditional) (b)): honest L_0 leader's bundle delivered to a strict subset of honest; remaining honest NR-emit; σ-pool < qV, NR-pool < qEnc; algebraic deadlock. **Validates the "bare OBFT does not close partial-propagation deadlocks in-protocol" claim mechanically** by surfacing the algebraic deadlock when Assumption 2 is relaxed. See [§5.4](#54--beyond-liveness_non_grief-class-a-failure-mode-exploration) for the methodology and trace classification. |
| SAFETY | n=7, f=2 | _to be run_ | — | State-space cap at quorum + symmetry over Honest expected to make this tractable; would extend the n=4 base-case coverage to a larger cluster. |
| LIVENESS_NON_GRIEF | n=7, f=2 | _to be run_ | — | Same scope as n=4 K=4 above, scaled. |

### 7.2 — OBFT + L_Bid

| Property | Config | Status | Date | Notes |
|---|---|---|---|---|
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (with TLC symmetry over Honest × Values) | ◐ partial | 2026-05-08 | TLC explored 98M+ distinct canonical states up to depth 15 without finding a counterexample, before run was halted. All four invariants (Pigeonholes 1, 2, 3 + PigeonholeVerdicts) held across all explored states. |
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (with symmetry + cap-at-quorum state constraint, `-Xmx24g`) | ◐ partial | 2026-05-08 | Re-run with state constraint capping pool sizes at quorum thresholds (provably safe; see `LBid_Safety.tla`) and 24GB heap. TLC explored 76.8M distinct canonical states up to depth 16 in the 20-min budget without finding any counterexample — ~45% reduction in state count vs unconstrained run, deeper exploration (depth 16 vs 15). All four invariants held. |
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (full coverage) | _follow-up_ | — | Run unattended (overnight or hours-long budget) with constraint + 24GB heap. Should complete ~30-60 min based on remaining queue size at the partial-coverage halt point. |
| SAFETY | n=7, f=2 | _to be run_ | — | Will need symmetry reductions to be tractable. |
| LIVENESS_NON_GRIEF | n=4, f=1 | _to be run_ | — | — |
| LIVENESS_NON_GRIEF | n=7, f=2 | _to be run_ | — | — |

### 7.3 — OBFT + L_Bid_New

| Property | Config | Status | Date | Notes |
|---|---|---|---|---|
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (with TLC symmetry over Honest × Values) | ◐ partial | 2026-05-08 | TLC explored 139M+ distinct canonical states up to depth 16 without finding a counterexample, before the run was halted at the 20-min user-budget cap. All four invariants (Pigeonholes 1, 2, 3 + PigeonholeVerdicts) held across all explored states. Spec encoding validated by SANY parse + millions of states explored. The L_Bid_New SAFETY spec is a structural refinement of L_Bid SAFETY at the algebraic level (per-operator σ-or-NR commitments + threshold pools + chained encryption); the L_Bid_New-specific σ-when-uncertain rule and deep-only verdict scope are honest-side tightenings that don't affect Pigeonhole invariants. |
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (with symmetry + cap-at-quorum state constraint, `-Xmx24g`) | ◐ partial | 2026-05-08 | Re-run with state constraint capping pool sizes at quorum thresholds (provably safe; see `LBidNew_Safety.tla`) and 24GB heap. TLC explored 78.8M distinct canonical states up to depth 15 in the 20-min budget without finding any counterexample — ~43% reduction in state count vs unconstrained run. All four invariants held. (Slightly shallower exploration than L_Bid's depth 16 reflects the additional state-variable complexity from L_Bid_New's priority-inverted structure.) |
| SAFETY | n=4, f=1, K=2, \|Values\|=2 (full coverage) | _follow-up_ | — | Re-run with longer time budget; same recommendation as L_Bid SAFETY. |
| SAFETY | n=7, f=2 | _to be run_ | — | Will need symmetry reductions to be tractable. |
| LIVENESS_NON_GRIEF | n=4, f=1 | _to be run_ | — | Would model bid_1 explicitly per-operator to verify F.5.2 corner cases (σ-when-uncertain residual at bid_1 > V_early; recovery at V_early > bid_1). |
| LIVENESS_NON_GRIEF | n=7, f=2 | _to be run_ | — | — |

### 7.4 — Counterexample log

#### CE-1: Phase-2.5 NR-flip + observable trigger — Pigeonhole-1 violation under byz σ-withholding cross-sign (2026-05-09)

**Spec**: `tla/BareOBFT_Safety.tla` at `n=4, f=1, K=2, |Values|=2` with state constraint, Phase-2.5 `HonestNRFlip` action, observable trigger `Cardinality(SigmaPool(k, v)) < QV ∧ Cardinality(NRPool(k)) ≥ F+1`.

**Outcome**: SAFETY (Pigeonhole 1 conjunct) violated. Counterexample found at depth 8; 25,577 distinct states explored before the violation surfaced.

**Trace** (Operators = {op1, op2, op3, op4}, Byzantine = {op4}, Layer = 0, V = v1):

```
State 1: Init                        sigma_partials={}              nr_partials={}
State 2: HonestSigma(op1, 0, v1)     sigma_partials={(op1,0,v1)}    nr_partials={}
State 3: HonestSigma(op3, 0, v1)     sigma_partials += (op3,0,v1)   nr_partials={}
State 4: HonestNR(op2, 0)            sigma_partials unchanged        nr_partials={(op2,0)}
State 5: ByzNR(op4, 0)               sigma_partials unchanged        nr_partials += (op4,0)
                                     ─── trigger conditions met:
                                         |NRPool(0)| = 2 ≥ F+1 = 2 ✓
                                         |SigmaPool(0, v1)| = 2 < QV = 3 ✓
State 6: HonestNRFlip(op1, 0)        sigma_partials unchanged        nr_partials += (op1,0)
                                     ─── NR-quorum reached:
                                         |NRPool(0)| = 3 = qEnc ✓
State 7: ByzSigma(op4, 0, v1)        sigma_partials += (op4,0,v1)   nr_partials unchanged
                                     ─── σ-quorum on v1 reached:
                                         |SigmaPool(0, v1)| = 3 = qV ✓
                                     ─── BOTH QUORUMS REACH AT LAYER 0
                                     ─── PIGEONHOLE 1 VIOLATED
```

**Real-protocol interpretation**: at honest L_0 leader with asymmetric propagation (1 honest's V_0 delivery slips past `T_commit`), 2 honest σ on V_0 (op1, op3) and 1 honest NR (op2). Byzantine op4 is non-leader at L_0; it cross-signs by publishing NR (visible in op4's KindCommit) while WITHHOLDING its σ partial on V_0 (= signs σ but doesn't include it in KindCommit's onion). At trigger time, every honest operator observes `σ-pool[V_0] = 2 < qV`, `NR-pool = 2 ≥ f+1`, satisfying the observable Phase-2.5 trigger. Honest σ-er op1 NR-flips. After NR-quorum reaches, op4 publishes the previously-withheld σ partial (e.g., as a "late KindCommit re-emission" or via offline aggregation). σ-pool[V_0] now reaches qV concurrent with NR-quorum. Both quorums at L_0 → cluster's BLS validator signature reconstructs on V_0 AND chain unlocks to L_1 where another reconstruction happens → cluster validator emits TWO distinct signatures at the same slot → **slashable double-sign on the validator's BLS pubkey**.

**Root cause**: the Phase-2.5 trigger's observable form (`SigmaPool(k, v) < qV`) does not bound future σ-pool growth. Specifically, byz can withhold σ partials at trigger time and publish them post-flip, growing σ-pool past qV after NR-pool has already reached qEnc via the flip. Operators cannot cryptographically detect "byz withholding σ" — every published partial has the same form, and the absence of a partial is indistinguishable from byz silence. This is a fundamental gap in Phase-2.5's design at OBFT's "single Phase-2 with cross-phase exclusivity" structural point, not a fixable spec encoding issue.

**Methodology lesson**: an earlier version of this spec used `Cardinality(HonestSigmaOnVAt(k, v)) ≤ F` in the trigger — `HonestSigmaOnVAt` filters σ-pool members by the `Honest` set, which is a model parameter accessible to TLC but represents oracle knowledge unavailable to real-world operators. Verifying with the oracle trigger confirmed safety of an idealized protocol that the implementation cannot enforce. The corrected spec uses only operator-observable conditions; CE-1 is what TLC finds under the corrected model. **TLA+ specs of distributed protocols should restrict each role's action preconditions to what's locally observable to that role; using model-parameter sets like `Honest` in role-local triggers is a methodology bug that produces unsound verification results.**

**Resolution status**: ✅ **RESOLVED** (2026-05-10) by spec rollback — the Phase-2.5 σ-flip / NR-flip mechanism (with all variants explored: oracle trigger, observable trigger, snapshot-based with per-operator views) was removed entirely from bare OBFT after a cost-benefit review concluded that the safety-preserving symmetric design's narrow recovery scope (3 specific Class B patterns at f=1, n=4) did not justify the spec/EKM/wire-format complexity, and that the `h_V=1` selective-delivery deadlock — the original motivation for Phase-2.5 — remains unclosed regardless. Bare OBFT now treats the patterns Phase-2.5 attempted to address (`h_V=1`, equivocation σ-locked splits, validity-divergence) as Class B grief deterred via Assumption 4, with [2abOBFT](2abOBFT.md) as the natural recovery-scope extension at +1 RTT cost when in-protocol closure is required.

**Why the rollback closes CE-1.** With Phase-2.5 removed, the trigger that fired in CE-1 (`SigmaPool < qV ∧ NRPool ≥ f+1` enabling NR-flip from honest σ-er) no longer exists in the protocol. Honest operators commit exactly once at `T_commit` on the 3-state (σ, NR, NV) lattice with strict cross-phase exclusivity per layer; there is no post-snapshot cross-sign emission of any kind. The Pigeonhole-1 union-bound argument (§4.2) returns to the simple form: σ-quorum on `v` at `k` requires ≥ f+1 honest σ-on-`v`; those honest are cross-phase-exclusive at `k` so cannot also contribute NR; remaining honest + byz cap ≤ 2f < qEnc.

**Cost of rollback.** The narrow Class B recovery patterns Phase-2.5 closed (R1/R2/R3 in earlier iterations) are no longer in scope; bare OBFT relies on K-layer fall-through under non-grief byz and on Assumption 4 for grief patterns. See [OBFT.md §Failure modes](OBFT.md#failure-modes) for the full taxonomy.

---

## 8 — Future work

Items deferred from this verification effort:

1. **Parametric verification at n ∈ {10, 13}**: Use Coq/Isabelle for proof-assistant work that scales beyond TLC's bounded model checking. Would extend the Class A closure property to all SSV cluster sizes.

2. **Multi-slot dynamics**: assumption 4 (rational-byzantine deterrent) is a multi-slot property. A separate verification effort could model slot-to-slot byzantine reputation accrual and verify the deterrent's expected-value bound.

3. **Class B characterization**: the current effort verifies Class A closure (no undocumented Class A failures) but does not exhaustively characterize Class B exposure. A follow-up could enumerate per-variant Class B trigger surfaces precisely.

4. **Cryptographic-primitive integration**: this verification abstracts BLS threshold and IBE/SWE primitives. A more rigorous effort could integrate primitive-level proofs (e.g., from drand/tlock's audited specification) for end-to-end verification.

5. **Variant-comparison probabilistic analysis**: stochastic simulation with realistic mesh-asymmetry distributions and adversarial-byz behavior models would estimate empirical recovery probability per variant, complementing the formal Class A closure result.

---

## 9 — Glossary and references

- **Class A**: assumption-violation failures (out of recovery scope by spec design).
- **Class B**: byzantine-grief failures within the f-bound under valid assumptions.
- **qV, qEnc**: threshold for V-signing and IBE-signing reconstruction; both = 2f+1.
- **L_k**: rotation layer k (0 ≤ k < K).
- **L_Bid**: bid-routing layer in L_Bid extension.
- **σ-pool, NR-pool**: aggregated threshold partials per layer (cluster signed-message-set).
- **`sigma_L_witnesses`**: optional witness section in `KindCommit` carrying retained Phase-1 σ_L^V partials paired with `value_root` for cross-reference; protects σ_L^V against bundle drop at peer receivers who DID receive V (see [OBFT.md §Phase 2 / Wire format](OBFT.md#phase-2--onion-broadcast-t_commit-t_commit--%CE%94_2)).
- **EKM**: Eth-Key-Manager-equivalent; the slashing-protection-aware signing service. Enforces single-σ-V-per-(op, k) and cross-phase exclusivity per layer.
- **GRIEF_***: byzantine actions that deviate from honest behavior (§3.5).
- **CE-N**: counterexample N in the verification log (§7.4). CE-1 is resolved by spec rollback (Phase-2.5 removed entirely from bare OBFT).

References:
- [docs/OBFT.md](OBFT.md) — main protocol specification.
- [docs/OBFT.md Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) — current L_Bid design.
- [docs/OBFT.md Appendix F](OBFT.md#appendix-f--obft--l_bid_new-deep-bid-mini-consensus) — L_Bid_New design.

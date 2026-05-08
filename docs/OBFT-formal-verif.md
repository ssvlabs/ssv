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
3. **Update the TLA+ encoding** in `tla/` (separate directory, to be created when verification work begins).
4. **Re-run TLC** at the new parameters.
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
- **Within a single slot**, mesh propagation can be asymmetric: a message broadcast at time `t` may reach peer A by `t + ε` but peer B by `t + 2 BTT`. The protocol's per-layer staggered budgets `B_k + slack` set the absorption ceiling per layer.
- Beyond the absorption ceiling, slot misses cleanly (Class A — assumption 2 violation).

**Network non-determinism**: For verification, we model the network as choosing (within partial-synchrony constraints) which messages reach which peers by which time. Specifically, for each broadcast message `m` and each peer `p`, the network non-deterministically chooses a delivery time `t_m,p ∈ [t_broadcast, t_broadcast + B_layer + slack]` — or `∞` if `m` is not delivered to `p` within this slot's budget.

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
2. **Cross-phase exclusivity violation**: σ-side AND NR-side commitment at the same `(slot, layer)`.
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

This section specifies the honest state machine in TLA+-ready pseudocode. The precise TLA+ encoding lives in `tla/` (to be created).

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
    
    Include σ_L^V witness section: byte-for-byte copies of all σ_L^V partials
    extracted from retained Phase-1 bundles for which i is NOT the leader.
    
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
            σ_L^V from peer KindCommit witness sections at k,
            decrypted σ partials from peer KindCommit onion contents at k 
              (decryptable via accumulated NR-quorum keys at layers 0..k-1)
        ), deduplicated per operator.
        
        nrs[k] := σ_j^IBE(nr_tag_k) partials, deduplicated per operator.
        
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
                  (T_broadcast_max_k = T_0_arrival − B_k for L_Bid, where T_0_arrival
                   replaces bare OBFT's Ls_arrival.)
    
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

1. Encode the protocol state machine including byzantine action space (honest + grief).
2. Define `SAFETY` invariant in TLA+:
   ```
   SAFETY ≡
     ∀ s ∈ slots:
       Cardinality({V : reconstructed(V, slot s)}) ≤ 1
   ```
3. Run TLC with `INVARIANT SAFETY` at `n=4, f=1` and `n=7, f=2`.

Expected result: TLC verifies `SAFETY` for all three variants. If counterexample found: critical safety bug.

### 4.3 — Cryptographic-primitive abstraction

We model BLS threshold reconstruction as: a V signature reconstructs cluster-wide iff `≥ qV` partials on V exist on the wire. The TLA+ model tracks partials as abstract signed-message structures; it does NOT verify the BLS scheme itself (we trust that as a primitive).

Similarly for IBE/SWE chained encryption: layer L_k+1's σ partials are "decryptable" iff `≥ qEnc` NR-partials on nr_tag_k exist on the wire.

This abstraction is justified because:
- BLS threshold and IBE/SWE primitives are independently audited (drand/tlock).
- The OBFT spec's Pigeonhole proofs are at the partial-counting level, not at cryptographic-primitive level.
- TLC verifies the partial-counting algebra, which is what determines safety.

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

1. Encode the protocol state machine WITHOUT grief actions in the byzantine action space — byzantine operators are restricted to either following the honest state machine or being silent/offline.
2. Encode network non-determinism (mesh asymmetry within partial-synchrony bound).
3. Define `LIVENESS_NON_GRIEF` as a TLA+ temporal formula:
   ```
   LIVENESS_NON_GRIEF ≡
     □(assumptions_hold ∧ no_grief) ⇒ ◇terminated
   ```
   Or equivalently as an invariant:
   ```
   INVARIANT NoSilentDeadlock ≡
     (assumptions_hold ∧ no_grief ∧ at_T_round_end) ⇒ output ≠ ⊥
   ```
4. Run TLC with the invariant at `n=4, f=1` and `n=7, f=2`.

Expected result (per the OBFT spec's Class A list): TLC verifies for all three variants — no Class A leakage. If counterexample found: surfaces a NEW Class A failure mode.

### 5.3 — Mesh asymmetry modeling

The network non-determinism in §2.1 must be carefully bounded for the verification to be meaningful:

- **Within-budget asymmetry**: each message reaches each subscribed peer within `B_layer + slack` of broadcast. This is the protocol-spec-allowed asymmetry; the protocol must terminate under this.
- **Beyond-budget asymmetry**: messages take longer than `B_layer + slack`. This is assumption-2 violation; out of scope for the Class A closure property.

The TLA+ model exposes a parameter `mesh_asymmetry ∈ {within_budget, beyond_budget}` and verifies the property only under `within_budget`. Beyond-budget executions are excluded by the assumption-2 precondition.

---

## 6 — TLA+ encoding sketch

This section sketches the structure of the TLA+ models. Actual `.tla` files live in `tla/` directory (to be created when implementation begins).

### 6.1 — File structure

```
tla/
├── Common.tla          -- shared types, constants, network model
├── HonestStateMachine.tla -- honest operator behavior (per §3)
├── GriefActions.tla    -- grief action set (per §3.5)
├── BareOBFT.tla        -- bare OBFT-specific extensions
├── LBid.tla            -- L_Bid extension
├── LBidNew.tla         -- L_Bid_New extension
├── Safety.tla          -- SAFETY invariants (Pigeonhole 1, 2, 3)
├── Liveness.tla        -- LIVENESS_NON_GRIEF invariant
└── configs/
    ├── n4_f1.cfg       -- TLC config for n=4, f=1
    └── n7_f2.cfg       -- TLC config for n=7, f=2
```

### 6.2 — Key TLA+ idioms

**Operator state**: a record per operator capturing local state.

**Network**: a set of broadcast messages with per-peer delivery times.

**Adversarial choice**: `\E action ∈ AllowedActions(operator): ...` — TLC explores all adversarial choices.

**Byzantine partition**: `Cardinality(byzantine_operators) ≤ f` (constant constraint).

**Symmetry reductions**: use TLA+ `SYMMETRY` declarations on operator-id permutations (operators are symmetric in role except for byzantine designation).

### 6.3 — TLC configuration

For each `(n, f)` pair, TLC config specifies:
- `CONSTANTS`: n, f, K, qV, qEnc.
- `INVARIANT`: SAFETY ∧ NoSilentDeadlock.
- `SPECIFICATION`: protocol next-state relation.
- `SYMMETRY`: operator-id symmetry.
- `VIEW`: state abstraction for state-space reduction.

Estimated state-space sizes (rough):
- `n=4, f=1, K=4`: ~10^6 states. TLC runtime ~minutes.
- `n=7, f=2, K=4`: ~10^9 states. TLC runtime ~hours; may need symmetry reductions and bounded depth.

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
| SAFETY | n=4, f=1, K=2, \|Values\|=2 | ✓ verified | 2026-05-08 | TLC explored 262,144 distinct states (1.96M total) in 10s; no counterexamples. All three Pigeonholes hold. |
| SAFETY | n=4, f=1, K=4, \|Values\|=4 | _to be run_ | — | Larger config — verifies safety with full layer count |
| SAFETY | n=7, f=2 | _to be run_ | — | — |
| LIVENESS_NON_GRIEF | n=4, f=1 | _to be run_ | — | Liveness module pending |
| LIVENESS_NON_GRIEF | n=7, f=2 | _to be run_ | — | — |

### 7.2 — OBFT + L_Bid

| Property | Config | Status | Date | Notes |
|---|---|---|---|---|
| SAFETY | n=4, f=1 | _to be run_ | — | — |
| SAFETY | n=7, f=2 | _to be run_ | — | — |
| LIVENESS_NON_GRIEF | n=4, f=1 | _to be run_ | — | — |
| LIVENESS_NON_GRIEF | n=7, f=2 | _to be run_ | — | — |

### 7.3 — OBFT + L_Bid_New

| Property | Config | Status | Date | Notes |
|---|---|---|---|---|
| SAFETY | n=4, f=1 | _to be run_ | — | — |
| SAFETY | n=7, f=2 | _to be run_ | — | — |
| LIVENESS_NON_GRIEF | n=4, f=1 | _to be run_ | — | — |
| LIVENESS_NON_GRIEF | n=7, f=2 | _to be run_ | — | — |

### 7.4 — Counterexample log

(empty until any counterexample is found)

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
- **σ-pool, NR-pool**: aggregated threshold partials per layer.
- **EKM**: Eth-Key-Manager-equivalent; the slashing-protection-aware signing service.
- **GRIEF_***: byzantine actions that deviate from honest behavior (§3.5).

References:
- [docs/OBFT.md](OBFT.md) — main protocol specification.
- [docs/OBFT.md Appendix B](OBFT.md#appendix-b--l_bid-mini-consensus-extension) — current L_Bid design.
- [docs/OBFT.md Appendix F](OBFT.md#appendix-f--obft--l_bid_new-deep-bid-mini-consensus) — L_Bid_New design.

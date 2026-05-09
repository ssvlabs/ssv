---------------------------- MODULE BareOBFT_Safety ----------------------------
(***************************************************************************)
(* Safety verification for bare OBFT — σ-flip variant.                     *)
(*                                                                         *)
(* Tests the σ-flip + leader-NR-doesn't-count + full-V-in-onion design as  *)
(* an alternative to the previous NR-flip mechanism.  Specifically:        *)
(*                                                                         *)
(*   - σ-flip: an honest operator who already NR'd at layer k may emit an  *)
(*     additional σ partial on V if (a) ≥ f+1 NR partials observed at k    *)
(*     (excluding leader-k's NR), (b) σ-quorum not yet reached on any V at *)
(*     k, and (c) V is V-available (= some operator already has σ on V,    *)
(*     so V propagates cluster-wide via the full-V-in-onion plumbing).     *)
(*                                                                         *)
(*   - leader-NR-doesn't-count: the layer-k leader's NR is excluded from   *)
(*     the cluster-wide NR-pool used for fallthrough quorum and σ-flip     *)
(*     trigger evidence.  Cryptographically implementable since leader_of  *)
(*     is publicly known per OBFT's deterministic rotation.                *)
(*                                                                         *)
(*   - full-V-in-onion: V is propagated cluster-wide once any σ partial on *)
(*     V exists at the layer (modeled here as VAvailable predicate).       *)
(*                                                                         *)
(* Verifies the three Pigeonhole invariants from `docs/OBFT.md`:           *)
(*                                                                         *)
(*   - Pigeonhole 1: at any layer L_k, σ-quorum on any V and NR-quorum     *)
(*     (excluding leader-k's NR) cannot both reach.                        *)
(*   - Pigeonhole 2: at any layer L_k, at most one V can reach σ-quorum.   *)
(*   - Pigeonhole 3: across all K layers under chained encryption, at      *)
(*     most one V signature reconstructs cluster-wide.                     *)
(*                                                                         *)
(* The spec models per-operator σ-or-NR commitments per layer with         *)
(* honest XOR enforcement (σ-after-NR is allowed only via the σ-flip       *)
(* action under valid trigger evidence) and unrestricted byzantine action. *)
(* The next-state relation lets honest add new commitments respecting XOR  *)
(* (or σ-flip's amended-EKM rule) and lets byzantine add any commitments   *)
(* freely; TLC explores all reachable states and checks the invariants.    *)
(*                                                                         *)
(* leader_of is a state variable initialized non-deterministically as any  *)
(* injective function [Layers -> Operators] (= distinct leader per layer,  *)
(* matching OBFT's deterministic rotation), so TLC explores all leader-    *)
(* assignment patterns including byz-at-any-position.                      *)
(*                                                                         *)
(* Out of scope: phase progression, network non-determinism, fetch loops,  *)
(* mini-consensus.  These are not relevant for the algebraic safety        *)
(* property — Pigeonholes are properties of the cluster-wide signed-      *)
(* message set under EKM enforcement, independent of timing or network.    *)
(*                                                                         *)
(* See `docs/OBFT-formal-verif.md` §4 for the verification approach and    *)
(* `tla/README.md` for run instructions.                                   *)
(***************************************************************************)

EXTENDS Naturals, FiniteSets

CONSTANTS
    Operators,    \* set of operator IDs (e.g., {1, 2, 3, 4})
    Byzantine,    \* subset of operator IDs that are byzantine
    K,            \* number of layers (≥ 2)
    Values        \* set of possible V values (≥ K so each layer has a candidate)

\* Derived constants
Honest == Operators \ Byzantine
N == Cardinality(Operators)
F == Cardinality(Byzantine)
QV == 2 * F + 1
QEnc == 2 * F + 1

\* Layer indices: 0..K-1
Layers == 0..(K - 1)

ASSUME
    /\ Operators # {}
    /\ Byzantine \subseteq Operators
    /\ N = 3 * F + 1                    \* BFT-tight setting
    /\ K \in 2..N                       \* 2 ≤ K ≤ n
    /\ Cardinality(Values) >= K         \* enough distinct values for layer leaders

(***************************************************************************)
(* State variables                                                         *)
(*                                                                         *)
(* sigma_partials: set of (operator, layer, value) tuples.  Each tuple     *)
(* represents that `operator` has signed σ on `value` at `layer`.         *)
(*                                                                         *)
(* nr_partials: set of (operator, layer) tuples.  Each tuple represents    *)
(* that `operator` has signed NR on nr_tag_{layer}.                        *)
(*                                                                         *)
(* leader_of: function [Layers -> Operators].  Initialized non-            *)
(* deterministically as any injective assignment (= distinct leader per    *)
(* layer, matching OBFT's deterministic rotation); constant after Init.    *)
(* TLC explores all valid leader assignments including byz-at-any-position *)
(* — directly relevant to the σ-flip design since leader-NR-doesn't-count  *)
(* depends on leader_of[k].                                                *)
(***************************************************************************)

VARIABLES sigma_partials, nr_partials, leader_of

vars == <<sigma_partials, nr_partials, leader_of>>

\* All injective leader assignments [Layers -> Operators].
InitLeaderAssignments ==
    {f \in [Layers -> Operators] :
        \A k1, k2 \in Layers : k1 # k2 => f[k1] # f[k2]}

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Has operator op committed σ at layer k (on any V)?
HasSigma(op, k) == \E v \in Values: <<op, k, v>> \in sigma_partials

\* Has operator op committed NR at layer k?
HasNR(op, k) == <<op, k>> \in nr_partials

\* σ pool at layer k for value v: operators who signed σ on v at k
SigmaPool(k, v) == {op \in Operators : <<op, k, v>> \in sigma_partials}

\* === Leader-NR-doesn't-count rule ========================================
\* The layer-k leader's NR partial is excluded from the cluster-wide NR-pool
\* used for fallthrough quorum and σ-flip trigger evidence.  Cryptographic-
\* ally implementable because leader_of[k] is publicly known per OBFT's
\* deterministic rotation: every operator (and every observer) verifies
\* "this NR is from the layer-k leader" and discards it from quorum count.
\*
\* Rationale: if the layer-k leader could contribute to the layer-k NR-pool,
\* a byzantine leader could selectively broadcast σ_L^V to a subset of
\* honest peers (causing some honest to retain → σ, others to NR), then
\* itself NR'd at k to push NR-pool past qEnc and force fallthrough
\* (= deadlock-engineered grief).  Excluding leader's NR closes that path.
NRPool(k) == {op \in Operators : <<op, k>> \in nr_partials /\ op # leader_of[k]}

\* σ-quorum reached on V at layer k iff |SigmaPool(k, v)| ≥ qV
SigmaQuorumReached(k, v) == Cardinality(SigmaPool(k, v)) >= QV

\* NR-quorum reached at layer k iff |NRPool(k)| ≥ qEnc
NRQuorumReached(k) == Cardinality(NRPool(k)) >= QEnc

\* Chained-encryption unlock: layer k accessible iff NR-quorum at all layers 0..k-1
\* (For k=0, no preceding layers — always unlocked.)
ChainUnlocked(k) == \A j \in 0..(k - 1): NRQuorumReached(j)

\* V at layer k is reconstructable cluster-wide iff σ-quorum reached AND chain unlocked
Reconstructable(k, v) ==
    /\ SigmaQuorumReached(k, v)
    /\ ChainUnlocked(k)

(***************************************************************************)
(* Initial state: no commitments yet                                       *)
(***************************************************************************)

Init ==
    /\ sigma_partials = {}
    /\ nr_partials = {}
    /\ leader_of \in InitLeaderAssignments

(***************************************************************************)
(* Honest actions — XOR enforcement                                        *)
(*                                                                         *)
(* An honest operator may add a σ partial at layer k iff they have not     *)
(* yet committed at k (neither σ nor NR).  Similarly for NR.  This         *)
(* models EKM cross-phase exclusivity for honest operators.                *)
(*                                                                         *)
(* Single-σ-V exclusivity: an honest operator who signs σ on V at k may   *)
(* not subsequently sign σ on V' ≠ V at k.  Enforced by ¬HasSigma in the *)
(* precondition.                                                           *)
(***************************************************************************)

HonestSigma(op, k, v) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)              \* not yet σ at k (single-σ-V)
    /\ ~ HasNR(op, k)                 \* not yet NR at k (cross-phase exclusivity)
    /\ sigma_partials' = sigma_partials \cup {<<op, k, v>>}
    /\ UNCHANGED <<nr_partials, leader_of>>

HonestNR(op, k) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)              \* not yet σ at k
    /\ ~ HasNR(op, k)                 \* not yet NR at k
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED <<sigma_partials, leader_of>>

(***************************************************************************)
(* Byzantine actions — unrestricted (controls own EKM)                     *)
(*                                                                         *)
(* Byzantine operators may add any σ or NR partial at any time, including  *)
(* violating XOR (cross-signing σ + NR at same layer) and single-σ-V      *)
(* (signing σ on multiple distinct V's at same layer).  Both produce      *)
(* slashable evidence per Rules 1, 2, 3 of OBFT.md but we don't model      *)
(* slashing here — only check whether safety holds against this adversary. *)
(***************************************************************************)

ByzSigma(op, k, v) ==
    /\ op \in Byzantine
    /\ <<op, k, v>> \notin sigma_partials   \* not already signed (idempotent)
    /\ sigma_partials' = sigma_partials \cup {<<op, k, v>>}
    /\ UNCHANGED <<nr_partials, leader_of>>

ByzNR(op, k) ==
    /\ op \in Byzantine
    /\ <<op, k>> \notin nr_partials         \* not already signed
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED <<sigma_partials, leader_of>>

(***************************************************************************)
(* Honest σ-flip — Phase-2.5 deadlock recovery (σ-flip variant).           *)
(*                                                                         *)
(* When honest who already NR'd at layer k observes deadlock evidence:     *)
(*   (a) ≥ f+1 NR partials at k (excluding leader's NR via leader-NR-      *)
(*       doesn't-count rule),                                              *)
(*   (b) σ-quorum not yet reached on any V at this layer,                  *)
(*   (c) V is V-available (= some operator already has σ on V at k,        *)
(*       propagated cluster-wide via the full-V-in-onion plumbing),        *)
(* they emit an additional σ partial on V.  This is a Phase-2.5            *)
(* KindSigmaFlip cross-signing under valid trigger evidence.  EKM          *)
(* amendment: σ-after-NR is allowed iff trigger evidence is valid.         *)
(*                                                                         *)
(* Effect: adds σ partial.  NR partial unchanged (= the σ-flipper still    *)
(* contributes its NR via its KindCommit emission, AND now also            *)
(* contributes its σ via KindSigmaFlip — i.e., it appears in both pools    *)
(* simultaneously).                                                        *)
(*                                                                         *)
(* Design intent: in the user's concrete trace, byz_leader broadcasts σ_L^V*)
(* to a strict subset of honest (1 retainer, 2 non-retainers).  Without σ- *)
(* flip, σ-pool[V] = leader + 1 retainer = 2 < qV=3, deadlocked.  With σ-  *)
(* flip + full-V-in-onion, the 2 non-retainers learn V (via cluster's σ-   *)
(* witness section) and emit σ on V → σ-pool[V] reaches qV.  Leader-NR-    *)
(* doesn't-count rule is what prevents byz_leader from gaming the          *)
(* fallthrough quorum: byz can't push NR-pool past qEnc by adding its own  *)
(* leader NR.                                                              *)
(*                                                                         *)
(* Open safety question this verification addresses: σ-flip lets honest    *)
(* contribute to BOTH σ-pool[V] AND NR-pool simultaneously, breaking the   *)
(* original Pigeonhole-1 algebraic bound (which assumed honest XOR ≤ 1).   *)
(* The leader-NR-doesn't-count rule reduces the NR side by ≤1, but         *)
(* honest σ-flippers' double-counting adds up to f+1 (or more).  TLC will  *)
(* find a concrete counterexample if the bound breaks.                     *)
(***************************************************************************)

\* === Operator-observable conditions only ================================
\*
\* This trigger uses only state observable to operators in the real protocol —
\* the count of distinct operators with σ partial / NR partial at layer k.
\* No oracle knowledge of who's honest vs byzantine.
\*
\* User's exact trigger (per chat clarification):
\*   F = number of OTHER operators (excl self, excl leader) who NR'd.
\*   Trigger: F ∈ [f+1, 2f].
\*     Lower bound f+1: ensures > byz could have produced (= ≥ 1 honest NR
\*       evidence), since byz contributes ≤ f to NR-pool.
\*     Upper bound 2f:  keeps the σ-flip window pre-fallthrough; NR-pool incl
\*       self ≤ 2f+1 = qEnc.
\*   σ-pool < qV (σ-quorum not yet reached on any V).
\*   |Silent| ≤ f: silent ops consume the f-byz budget; until we have ≥ n-f
\*     explicit emissions, we wait (= can't be sure deadlock is real).

\* An op has emitted iff they have a σ or NR partial at this layer.
HasEmitted(op, k) == HasSigma(op, k) \/ HasNR(op, k)

\* Silent ops at layer k = operators we haven't received any signed partial from.
SilentAt(k) == {op \in Operators : ~ HasEmitted(op, k)}

DeadlockObservedFor(op, k) ==
    LET NRedOthers == NRPool(k) \ {op}     \* NRs excl self (NRPool already excl leader)
    IN /\ Cardinality(NRedOthers) >= F + 1
       /\ Cardinality(NRedOthers) <= 2 * F
       /\ \A v \in Values : Cardinality(SigmaPool(k, v)) < QV
       /\ Cardinality(SilentAt(k)) <= F

\* V-availability: some operator already has σ on V at this layer.  Models
\* the full-V-in-onion plumbing — once any σ partial on V exists in the
\* cluster's signed-message set, V is propagated cluster-wide via the
\* onion broadcast, allowing non-retainers to emit σ on V via the σ-flip.
VAvailable(k, v) == \E op \in Operators : <<op, k, v>> \in sigma_partials

HonestSigmaFlip(op, k, v) ==
    /\ op \in Honest
    /\ HasNR(op, k)                       \* honest already NR'd at k
    /\ ~ HasSigma(op, k)                  \* not yet σ'd at k (single flip per layer)
    /\ DeadlockObservedFor(op, k)         \* user's exact trigger (F ∈ [f+1, 2f] excl self, silent ≤ f)
    /\ VAvailable(k, v)                   \* V learned via full-V-in-onion plumbing
    /\ sigma_partials' = sigma_partials \cup {<<op, k, v>>}
    /\ UNCHANGED <<nr_partials, leader_of>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E op \in Honest, k \in Layers, v \in Values: HonestSigma(op, k, v)
    \/ \E op \in Honest, k \in Layers: HonestNR(op, k)
    \/ \E op \in Honest, k \in Layers, v \in Values: HonestSigmaFlip(op, k, v)
    \/ \E op \in Byzantine, k \in Layers, v \in Values: ByzSigma(op, k, v)
    \/ \E op \in Byzantine, k \in Layers: ByzNR(op, k)

Spec == Init /\ [][Next]_vars

(***************************************************************************)
(* Type invariant — sanity check on state structure                        *)
(***************************************************************************)

TypeOK ==
    /\ sigma_partials \subseteq (Operators \X Layers \X Values)
    /\ nr_partials \subseteq (Operators \X Layers)
    /\ leader_of \in [Layers -> Operators]
    /\ \A k1, k2 \in Layers : k1 # k2 => leader_of[k1] # leader_of[k2]

(***************************************************************************)
(* SAFETY invariants — Pigeonholes 1, 2, 3                                 *)
(***************************************************************************)

\* Pigeonhole 1: σ-quorum on any V at L_k AND NR-quorum (excluding leader's
\* NR per leader-NR-doesn't-count) at L_k cannot both reach.
\*
\* Algebraic basis with σ-flip + leader-NR-doesn't-count:
\*   Honest σ-flippers contribute 1 to σ-pool[V] AND 1 to NR-pool simul-
\*   taneously (= violate the original honest-XOR ≤ 1 bound).  Worst case,
\*   all 2f+1 honest are σ-flippers → σ-pool gets 2f+1 from honest, NR-pool
\*   gets 2f+1 from honest.  Plus byz contribute ≤ f to each pool.
\*   Plus leader-NR-doesn't-count subtracts up to 1 from NR-pool.
\*
\*   For both quorums to reach (≥ 2f+1 each, total 4f+2):
\*     σ ≥ 2f+1: honest_σ + byz_σ ≥ 2f+1
\*     NR ≥ 2f+1: (honest_NR - leader_correction) + byz_NR ≥ 2f+1
\*   Max joint: (2f+1 + f) + (2f+1 - 1 + f) = 6f+1.
\*   Need 4f+2 minimum to violate, i.e., the sum must hit ≥ 4f+2.
\*   With the σ-flip + leader-NR-exclusion design, the bound 6f+1 ≥ 4f+2
\*   for all f ≥ 0.  So the algebraic lower bound DOES NOT prevent both
\*   quorums from reaching — Pigeonhole 1 is no longer guaranteed.
\*
\* This invariant therefore tests an OPEN safety hypothesis: does the σ-flip
\* design close the deadlock recovery (which it does, per the user's trace)
\* WHILE preserving Pigeonhole 1 in all reachable states (which the algebra
\* doesn't guarantee)?  TLC will give a definitive answer.
Pigeonhole1 ==
    \A k \in Layers:
        ~ ( (\E v \in Values: SigmaQuorumReached(k, v)) /\ NRQuorumReached(k) )

\* Pigeonhole 2: at most one V σ-quorums at any layer
\*
\* Algebraic basis:
\*   Honest contribute h_σ_V + h_σ_V' ≤ 2f+1 (single-σ-V).
\*   Byz contribute byz_σ_V + byz_σ_V' ≤ 2f.
\*   For both to reach 2f+1:
\*     ≥ 4f+2 total partials. Honest ≤ 2f+1, byz ≤ 2f, total ≤ 4f+1.
\*   Contradiction. ∎
Pigeonhole2 ==
    \A k \in Layers:
        Cardinality({v \in Values : SigmaQuorumReached(k, v)}) <= 1

\* Pigeonhole 3: across all K layers, at most one V reconstructs cluster-wide
\*
\* Cross-layer safety under chained encryption: V_j at layer j and V_k at layer k
\* (j ≠ k) cannot both reconstruct, because reconstructing V_k requires NR-quorum
\* at all prior layers (chained encryption gates), but Pigeonhole 1 forbids
\* σ-quorum at j AND NR-quorum at j to coexist.
\*
\* The proof in OBFT.md proceeds by induction on the layer gap m.  Here we
\* express it directly: at most one (k, v) pair satisfies Reconstructable.
Pigeonhole3 ==
    Cardinality({<<k, v>> \in Layers \X Values : Reconstructable(k, v)}) <= 1

\* Combined SAFETY property
SAFETY == Pigeonhole1 /\ Pigeonhole2 /\ Pigeonhole3

(***************************************************************************)
(* State constraint — bound state space for TLC by capping each pool at    *)
(* its quorum threshold.                                                   *)
(*                                                                         *)
(* Provably safe for the SAFETY property at f=1, n=4: Pigeonhole 1, 2, 3   *)
(* are all stated as "pool size ≥ threshold" predicates, so they're        *)
(* already detectable when a pool first reaches the threshold.  States     *)
(* with pool size > threshold add no new information for SAFETY            *)
(* evaluation.                                                             *)
(*                                                                         *)
(* Furthermore, pool sizes only grow (actions never remove tuples), so any *)
(* state pruned by this constraint has predecessors at the threshold       *)
(* boundary that are explored normally.  TLC checks INVARIANTs on every    *)
(* visited state, including states that fail the CONSTRAINT — the          *)
(* CONSTRAINT only determines whether to expand successors.  Combined,     *)
(* this means zero risk of missing a counterexample for the four checked   *)
(* invariants.                                                             *)
(*                                                                         *)
(* Same constraint applies to LBid_Safety and LBidNew_Safety with their    *)
(* additional pools (lbid_sigma, lbid_nr, verdicts).                       *)
(***************************************************************************)

StateConstraint ==
    /\ \A k \in Layers, v \in Values:
        Cardinality(SigmaPool(k, v)) <= QV
    /\ \A k \in Layers:
        Cardinality(NRPool(k)) <= QEnc

================================================================================

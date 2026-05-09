---------------------------- MODULE BareOBFT_Safety ----------------------------
(***************************************************************************)
(* Safety verification for bare OBFT.                                      *)
(*                                                                         *)
(* Verifies the three Pigeonhole invariants from `docs/OBFT.md`:           *)
(*                                                                         *)
(*   - Pigeonhole 1: at any layer L_k, σ-quorum on any V and NR-quorum     *)
(*     on nr_tag_k cannot both reach.                                      *)
(*   - Pigeonhole 2: at any layer L_k, at most one V can reach σ-quorum.   *)
(*   - Pigeonhole 3: across all K layers under chained encryption, at      *)
(*     most one V signature reconstructs cluster-wide.                     *)
(*                                                                         *)
(* The spec models per-operator σ-or-NR commitments per layer with         *)
(* honest XOR enforcement and unrestricted byzantine action.  The next-    *)
(* state relation lets honest add new commitments respecting XOR and       *)
(* lets byzantine add any commitments freely; TLC explores all reachable   *)
(* states and checks the invariants.                                       *)
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
(***************************************************************************)

VARIABLES sigma_partials, nr_partials

vars == <<sigma_partials, nr_partials>>

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Has operator op committed σ at layer k (on any V)?
HasSigma(op, k) == \E v \in Values: <<op, k, v>> \in sigma_partials

\* Has operator op committed NR at layer k?
HasNR(op, k) == <<op, k>> \in nr_partials

\* σ pool at layer k for value v: operators who signed σ on v at k
SigmaPool(k, v) == {op \in Operators : <<op, k, v>> \in sigma_partials}

\* NR pool at layer k: operators who signed NR at k
NRPool(k) == {op \in Operators : <<op, k>> \in nr_partials}

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
    /\ UNCHANGED nr_partials

HonestNR(op, k) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)              \* not yet σ at k
    /\ ~ HasNR(op, k)                 \* not yet NR at k
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED sigma_partials

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
    /\ UNCHANGED nr_partials

ByzNR(op, k) ==
    /\ op \in Byzantine
    /\ <<op, k>> \notin nr_partials         \* not already signed
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED sigma_partials

(***************************************************************************)
(* Honest NR-flip — Phase-2.5 deadlock recovery (NEW, user proposal).      *)
(*                                                                         *)
(* When honest σ-er observes ≥ f+1 NR partials at layer k AND no V's σ    *)
(* pool reaches qV at this layer (= deadlock detected), they emit an       *)
(* additional NR partial — a Phase-2.5 KindNRFlip cross-signing under      *)
(* valid trigger evidence.  EKM amendment: NR-after-σ is allowed iff       *)
(* trigger evidence is valid.                                              *)
(*                                                                         *)
(* Trigger condition includes `AllHonestCommittedAt(k)` to model that      *)
(* Phase-2.5 fires AFTER all honest have settled their primary Phase-2     *)
(* commitments — i.e., σ pool composition is final from the honest side    *)
(* by the time NR-flip evaluates.  Without this, the spec's free action    *)
(* interleaving would allow late HonestSigma to push σ past qV after NR-   *)
(* flips already pushed NR past qEnc, which can't happen in the actual     *)
(* protocol where T_commit is a hard cluster-wide deadline.                *)
(*                                                                         *)
(* Effect: adds NR partial.  σ pool unchanged.                             *)
(*                                                                         *)
(* Safety basis: at deadlock, σ pool[V] for any V is bounded by `k_V + f`  *)
(* where k_V ≤ |Honest| who σ'd on V (= retained the leader's bundle).     *)
(* Under deadlock σ pool[V] < qV → k_V + b_σ_V < 2f+1 → k_V ≤ 2f - b_σ_V.  *)
(* Honest XOR (σ-after-NR still blocked) means k_V doesn't grow post-flip. *)
(* Byz σ contribution bounded by f.  So σ pool[V] ≤ 2f < qV stays after    *)
(* the flip.  Thus σ-quorum doesn't reach at the deadlock layer — even     *)
(* though NR-quorum does — preserving Pigeonhole 1.                        *)
(***************************************************************************)

\* === Operator-observable conditions only ================================
\*
\* CRITICAL VERIFICATION HYGIENE NOTE.  Earlier versions of this trigger used
\* `HonestSigmaOnVAt` (= honest-only σ count, computed from the Honest set)
\* and `AllHonestCommittedAt` (= honest-set commit check).  Both rely on
\* knowledge of the byz/honest partition that operators do NOT have at
\* runtime: every σ partial has the same form, and operators cannot
\* cryptographically distinguish honest from byzantine signers.
\*
\* Verifying with oracle-trigger conditions confirmed safety of an idealized
\* protocol that's not directly implementable; doc review then surfaced a
\* real attack pattern (byz cross-signing with σ withholding) that the
\* oracle-trigger spec missed.  This was a CONCRETE METHODOLOGY BUG: TLC
\* verified safety against a trigger that the real protocol can't enforce.
\*
\* Lesson: TLA+ specs of distributed protocols should restrict each role's
\* local view to what's observable to that role.  Operator triggers should
\* use only observable σ-pool / NR-pool sizes (no Honest-set filter), and
\* "phase end" predicates should use observable timing (= all operators
\* committed, or explicit phase variable) — not honest-set membership.
\*
\* This rewrite uses only operator-observable conditions.  Under TLC, this
\* trigger model produces a safety counterexample reproducing the doc-review
\* attack (byz cross-signs at non-leader layer, withholds σ; honest σ-er
\* flips on observed deadlock; byz publishes withheld σ post-flip; both
\* σ-quorum and NR-quorum reach → P1 violated → cluster double-signs).
\* See docs/OBFT-formal-verif.md §7.4 for the trace.

\* NR-flip trigger using observable conditions only.  Liberal form: σ-quorum
\* not yet reached on any V (= what an honest operator verifies locally).
\* "Liberal" because no honest/byz distinction in σ-pool counting — this is
\* what's observable at the wire level, regardless of the safety proof's
\* desired condition.
NRFlipTriggered(k) ==
    /\ Cardinality(NRPool(k)) >= F + 1               \* ≥ f+1 NR partials observed
    /\ \A v \in Values :                             \* ∀V: σ-quorum not yet reached
        Cardinality(SigmaPool(k, v)) < QV

HonestNRFlip(op, k) ==
    /\ op \in Honest
    /\ HasSigma(op, k)                    \* honest already σ'd at k
    /\ ~ HasNR(op, k)                     \* not yet NR'd at k (single flip per layer)
    /\ NRFlipTriggered(k)                 \* deadlock detected
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED sigma_partials

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E op \in Honest, k \in Layers, v \in Values: HonestSigma(op, k, v)
    \/ \E op \in Honest, k \in Layers: HonestNR(op, k)
    \/ \E op \in Honest, k \in Layers: HonestNRFlip(op, k)
    \/ \E op \in Byzantine, k \in Layers, v \in Values: ByzSigma(op, k, v)
    \/ \E op \in Byzantine, k \in Layers: ByzNR(op, k)

Spec == Init /\ [][Next]_vars

(***************************************************************************)
(* Type invariant — sanity check on state structure                        *)
(***************************************************************************)

TypeOK ==
    /\ sigma_partials \subseteq (Operators \X Layers \X Values)
    /\ nr_partials \subseteq (Operators \X Layers)

(***************************************************************************)
(* SAFETY invariants — Pigeonholes 1, 2, 3                                 *)
(***************************************************************************)

\* Pigeonhole 1: σ-quorum on any V at L_k AND NR-quorum at L_k cannot both reach
\*
\* Algebraic basis (from OBFT.md):
\*   Honest contribute h_σ + h_NR ≤ n - f = 2f+1 (XOR-bounded).
\*   Byzantine contribute byz_σ + byz_NR ≤ 2f (≤ f to each pool, allowing dual).
\*   For both quorums to reach simultaneously:
\*     (h_σ + byz_σ) ≥ 2f+1 AND (h_NR + byz_NR) ≥ 2f+1
\*   Sum: ≥ 4f+2 partials. But honest contribute ≤ 2f+1 + byz ≤ 2f, total ≤ 4f+1.
\*   Contradiction. ∎
\*
\* Note: this property may NOT hold under fully-unrestricted byzantine action
\* (where byz contributes 1 to BOTH σ and NR pools), in which case the bound
\* is 2f+1 + 2f = 4f+1 < 4f+2 — so still holds. Actually, with byz cross-signing
\* (counted once per pool per byz), the inequality still works.
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

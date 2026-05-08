---------------------------- MODULE LBidNew_Safety ----------------------------
(***************************************************************************)
(* Safety verification for OBFT + L_Bid_New (priority-inverted bid-routing).*)
(*                                                                         *)
(* Verifies the same four invariants as LBid_Safety at K' = K + 1 layers,  *)
(* encoded for L_Bid_New's structural differences from current L_Bid:      *)
(*                                                                         *)
(*   - Pigeonhole 1: at any layer (L_Bid or rotation L_k), σ-quorum on     *)
(*     any V and NR-quorum cannot both reach.                              *)
(*   - Pigeonhole 2: at any layer, at most one V can reach σ-quorum.       *)
(*   - Pigeonhole 3: across L_Bid + K rotation layers under chained        *)
(*     encryption (L_Bid outermost, L_0 next under nr_tag_LBid, deeper     *)
(*     rotation layers further chained), at most one V reconstructs.       *)
(*   - PigeonholeVerdicts: at most one V reaches verdict_quorum.           *)
(*                                                                         *)
(* L_Bid_New differences from current L_Bid (per docs/OBFT.md Appendix F): *)
(*                                                                         *)
(*   1. Mini-consensus scope: deep bids only (L_1..L_{K-1}), not the       *)
(*      primary's bid (V_{L_0} = bid_1).  In this spec, the verdict pool   *)
(*      is over an abstract V; modeling the deep-only restriction would    *)
(*      tighten the verdict pool but not affect Pigeonhole verification    *)
(*      (which counts pool sizes, not V identities), so we use the same    *)
(*      abstract-V verdict model as LBid_Safety.                            *)
(*                                                                         *)
(*   2. Onion priority: V_early (mini-consensus winner over deep bids) is  *)
(*      outermost plaintext at L_Bid; bid_1 lives at the inner L_0 layer   *)
(*      encrypted under nr_tag_LBid; deeper rotation layers chained        *)
(*      further.  The encryption layout is structurally IDENTICAL to       *)
(*      current L_Bid (per F.6) — only the *content* of the layers         *)
(*      differs (V_early vs V_X at L_Bid; primary's bid placed at L_0      *)
(*      vs being a candidate for V_X).  Same chained-encryption gates →    *)
(*      same Pigeonhole 3 algebra.                                         *)
(*                                                                         *)
(*   3. σ-when-uncertain rule at L_Bid: honest σ on V_early iff            *)
(*      verdict_quorum_V_early AND operator retains V_early AND            *)
(*      (operator does NOT have bid_1, OR bid_1 ≤ V_early).  The bid_1-vs- *)
(*      V_early comparison is modeled abstractly here — for Pigeonhole      *)
(*      verification, the rule TIGHTENS the honest σ-pool contribution at  *)
(*      L_Bid (a strict subset of "verdict_quorum_V holds → may σ"), which *)
(*      cannot violate Pigeonholes 1, 2 (smaller honest pool only makes   *)
(*      the safety bounds easier to satisfy).  We encode the abstract     *)
(*      "may σ if verdict_quorum holds" precondition; the σ-when-          *)
(*      uncertain refinement is a strict subset in the LIVENESS-relevant  *)
(*      F.5.2 corner cases (bid_1 > V_early).                              *)
(*                                                                         *)
(* Reduction to LBid_Safety: at the algebraic level (per-operator σ-or-NR  *)
(* commitments + threshold pools + chained encryption), LBidNew_Safety is  *)
(* a refinement of LBid_Safety with strictly tighter honest σ rules at     *)
(* L_Bid.  Since refinement preserves SAFETY (Pigeonholes bound by σ-pool  *)
(* and NR-pool contributions, refinement only shrinks the σ-pool), any    *)
(* SAFETY violation in LBidNew_Safety would also violate LBid_Safety.      *)
(* Verifying LBidNew_Safety independently confirms the encoding and        *)
(* provides a base for LIVENESS verification of F.5.2 corner cases (which *)
(* would model bid_1 explicitly per-operator).                             *)
(*                                                                         *)
(* See docs/OBFT.md Appendix F for the L_Bid_New protocol; docs/OBFT-     *)
(* formal-verif.md §3.4 for the honest state machine; tla/README.md for   *)
(* run instructions.                                                       *)
(***************************************************************************)

EXTENDS Naturals, FiniteSets, TLC

CONSTANTS
    Operators,    \* set of operator IDs (e.g., {op1, op2, op3, op4})
    Byzantine,    \* subset of operator IDs that are byzantine
    K,            \* number of rotation layers (≥ 2)
    Values        \* set of possible V values (≥ K so each rotation layer has a candidate)

\* Derived constants
Honest == Operators \ Byzantine
N == Cardinality(Operators)
F == Cardinality(Byzantine)
QV == 2 * F + 1
QEnc == 2 * F + 1

\* Rotation layer indices: 0..K-1.  L_Bid is a separate layer modeled
\* via its own state variables (lbid_sigma, lbid_nr, verdicts).
RotationLayers == 0..(K - 1)

ASSUME
    /\ Operators # {}
    /\ Byzantine \subseteq Operators
    /\ N = 3 * F + 1                 \* BFT-tight setting
    /\ K \in 2..N                    \* 2 ≤ K ≤ n (rotation layer count)
    /\ Cardinality(Values) >= K      \* enough distinct values for layer leaders

(***************************************************************************)
(* State variables                                                         *)
(*                                                                         *)
(* Same shape as LBid_Safety — algebraic-level model:                      *)
(*   sigma_partials: rotation-layer σ partials.                            *)
(*   nr_partials:    rotation-layer NR partials.                           *)
(*   lbid_sigma:     L_Bid σ partials (on V_early in L_Bid_New terms).     *)
(*   lbid_nr:        L_Bid NR partials.                                    *)
(*   verdicts:       KindBidVerdict envelopes (deep-bids-only in L_Bid_New *)
(*                   semantics, but Pigeonhole verification doesn't depend *)
(*                   on which V's are eligible; modeled as any V).         *)
(***************************************************************************)

VARIABLES sigma_partials, nr_partials, lbid_sigma, lbid_nr, verdicts

vars == <<sigma_partials, nr_partials, lbid_sigma, lbid_nr, verdicts>>

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Rotation-layer state predicates
HasSigma(op, k) == \E v \in Values: <<op, k, v>> \in sigma_partials
HasNR(op, k) == <<op, k>> \in nr_partials

\* L_Bid state predicates
HasLBidSigma(op) == \E v \in Values: <<op, v>> \in lbid_sigma
HasLBidNR(op) == op \in lbid_nr

\* Verdict state predicate
HasVerdict(op) == \E v \in Values: <<op, v>> \in verdicts

\* Pools
SigmaPool(k, v) == {op \in Operators : <<op, k, v>> \in sigma_partials}
NRPool(k) == {op \in Operators : <<op, k>> \in nr_partials}
LBidSigmaPool(v) == {op \in Operators : <<op, v>> \in lbid_sigma}
LBidNRPool == lbid_nr
VerdictPool(v) == {op \in Operators : <<op, v>> \in verdicts}

\* Quorums
SigmaQuorumReached(k, v) == Cardinality(SigmaPool(k, v)) >= QV
NRQuorumReached(k) == Cardinality(NRPool(k)) >= QEnc
LBidSigmaQuorumReached(v) == Cardinality(LBidSigmaPool(v)) >= QV
LBidNRQuorumReached == Cardinality(LBidNRPool) >= QEnc
VerdictQuorumReached(v) == Cardinality(VerdictPool(v)) >= QV

\* Chain unlocked at rotation layer k.  Same chained-encryption gates as
\* in current L_Bid: L_Bid is outermost; L_0 is next under nr_tag_LBid;
\* deeper layers chained further.  Reaching any rotation layer requires
\* L_Bid NR-quorum first, then NR-quorum at all preceding rotation layers.
\* This is structurally IDENTICAL to LBid_Safety's chain (per Appendix F.6
\* "Number of onion encryption layers" comparison: same encryption layout).
ChainUnlocked(k) ==
    /\ LBidNRQuorumReached
    /\ \A j \in 0..(k - 1): NRQuorumReached(j)

\* V reconstructable at L_Bid (always unlocked — outermost layer)
LBidReconstructable(v) == LBidSigmaQuorumReached(v)

\* V reconstructable at rotation layer k
RotationReconstructable(k, v) ==
    /\ SigmaQuorumReached(k, v)
    /\ ChainUnlocked(k)

(***************************************************************************)
(* Initial state: no commitments yet                                       *)
(***************************************************************************)

Init ==
    /\ sigma_partials = {}
    /\ nr_partials = {}
    /\ lbid_sigma = {}
    /\ lbid_nr = {}
    /\ verdicts = {}

(***************************************************************************)
(* Honest actions                                                          *)
(*                                                                         *)
(* Same XOR + single-σ-V + single-verdict-per-operator constraints as in  *)
(* LBid_Safety.  The L_Bid σ-rule is encoded as the abstract precondition *)
(* "verdict_quorum_V holds"; the σ-when-uncertain refinement (no bid_1 OR *)
(* bid_1 ≤ V_early) tightens this further but doesn't affect Pigeonholes  *)
(* (the smaller honest σ-pool only strengthens the bounds).                *)
(***************************************************************************)

\* Verdict broadcast: at most one verdict per honest operator
HonestVerdict(op, v) ==
    /\ op \in Honest
    /\ ~ HasVerdict(op)
    /\ verdicts' = verdicts \cup {<<op, v>>}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_sigma, lbid_nr>>

\* L_Bid σ on V (= V_early in L_Bid_New terms): requires cluster-wide
\* verdict_quorum on V.  σ-when-uncertain refinement is a strict subset of
\* this precondition (see comment block at top).
HonestLBidSigma(op, v) ==
    /\ op \in Honest
    /\ ~ HasLBidSigma(op)              \* single-σ-V at L_Bid
    /\ ~ HasLBidNR(op)                  \* cross-phase exclusivity at L_Bid
    /\ VerdictQuorumReached(v)          \* verdict-gated σ-rule
    /\ lbid_sigma' = lbid_sigma \cup {<<op, v>>}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_nr, verdicts>>

\* L_Bid NR
HonestLBidNR(op) ==
    /\ op \in Honest
    /\ ~ HasLBidSigma(op)
    /\ ~ HasLBidNR(op)
    /\ lbid_nr' = lbid_nr \cup {op}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_sigma, verdicts>>

\* Rotation-layer σ at L_k (same as bare OBFT and LBid_Safety)
HonestSigma(op, k, v) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)
    /\ ~ HasNR(op, k)
    /\ sigma_partials' = sigma_partials \cup {<<op, k, v>>}
    /\ UNCHANGED <<nr_partials, lbid_sigma, lbid_nr, verdicts>>

\* Rotation-layer NR at L_k (same as bare OBFT and LBid_Safety)
HonestNR(op, k) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)
    /\ ~ HasNR(op, k)
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED <<sigma_partials, lbid_sigma, lbid_nr, verdicts>>

(***************************************************************************)
(* Byzantine actions — unrestricted                                        *)
(*                                                                         *)
(* Same byzantine action set as LBid_Safety.  L_Bid_New's structural       *)
(* differences (deep-bids-only verdict scope, σ-when-uncertain rule) are   *)
(* honest-side — byzantine remains unrestricted on all variables.          *)
(***************************************************************************)

ByzVerdict(op, v) ==
    /\ op \in Byzantine
    /\ <<op, v>> \notin verdicts
    /\ verdicts' = verdicts \cup {<<op, v>>}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_sigma, lbid_nr>>

ByzLBidSigma(op, v) ==
    /\ op \in Byzantine
    /\ <<op, v>> \notin lbid_sigma
    /\ lbid_sigma' = lbid_sigma \cup {<<op, v>>}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_nr, verdicts>>

ByzLBidNR(op) ==
    /\ op \in Byzantine
    /\ op \notin lbid_nr
    /\ lbid_nr' = lbid_nr \cup {op}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_sigma, verdicts>>

ByzSigma(op, k, v) ==
    /\ op \in Byzantine
    /\ <<op, k, v>> \notin sigma_partials
    /\ sigma_partials' = sigma_partials \cup {<<op, k, v>>}
    /\ UNCHANGED <<nr_partials, lbid_sigma, lbid_nr, verdicts>>

ByzNR(op, k) ==
    /\ op \in Byzantine
    /\ <<op, k>> \notin nr_partials
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED <<sigma_partials, lbid_sigma, lbid_nr, verdicts>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E op \in Honest, v \in Values: HonestVerdict(op, v)
    \/ \E op \in Honest, v \in Values: HonestLBidSigma(op, v)
    \/ \E op \in Honest: HonestLBidNR(op)
    \/ \E op \in Honest, k \in RotationLayers, v \in Values: HonestSigma(op, k, v)
    \/ \E op \in Honest, k \in RotationLayers: HonestNR(op, k)
    \/ \E op \in Byzantine, v \in Values: ByzVerdict(op, v)
    \/ \E op \in Byzantine, v \in Values: ByzLBidSigma(op, v)
    \/ \E op \in Byzantine: ByzLBidNR(op)
    \/ \E op \in Byzantine, k \in RotationLayers, v \in Values: ByzSigma(op, k, v)
    \/ \E op \in Byzantine, k \in RotationLayers: ByzNR(op, k)

Spec == Init /\ [][Next]_vars

(***************************************************************************)
(* Type invariant                                                          *)
(***************************************************************************)

TypeOK ==
    /\ sigma_partials \subseteq (Operators \X RotationLayers \X Values)
    /\ nr_partials \subseteq (Operators \X RotationLayers)
    /\ lbid_sigma \subseteq (Operators \X Values)
    /\ lbid_nr \subseteq Operators
    /\ verdicts \subseteq (Operators \X Values)

(***************************************************************************)
(* SAFETY invariants — Pigeonholes 1, 2, 3 + Pigeonhole on verdicts        *)
(*                                                                         *)
(* Identical to LBid_Safety: Pigeonhole 1, 2 hold by per-layer XOR +       *)
(* cross-sign algebra; Pigeonhole 3 by chained-encryption gates;           *)
(* PigeonholeVerdicts by the verdict-pool counting argument.  L_Bid_New's *)
(* structural differences (σ-when-uncertain, deep-only verdict scope)     *)
(* don't affect any of these invariants.                                   *)
(***************************************************************************)

\* Pigeonhole 1 at any layer (rotation or L_Bid)
Pigeonhole1Rotation ==
    \A k \in RotationLayers:
        ~ ( (\E v \in Values: SigmaQuorumReached(k, v)) /\ NRQuorumReached(k) )

Pigeonhole1LBid ==
    ~ ( (\E v \in Values: LBidSigmaQuorumReached(v)) /\ LBidNRQuorumReached )

Pigeonhole1 == Pigeonhole1Rotation /\ Pigeonhole1LBid

\* Pigeonhole 2 at any single layer
Pigeonhole2Rotation ==
    \A k \in RotationLayers:
        Cardinality({v \in Values : SigmaQuorumReached(k, v)}) <= 1

Pigeonhole2LBid ==
    Cardinality({v \in Values : LBidSigmaQuorumReached(v)}) <= 1

Pigeonhole2 == Pigeonhole2Rotation /\ Pigeonhole2LBid

\* Pigeonhole 3: across L_Bid + K rotation layers under chained encryption
\* (L_Bid outermost, L_0 next under nr_tag_LBid, deep layers chained
\* further), at most one V signature reconstructs cluster-wide.
Pigeonhole3 ==
    Cardinality({v \in Values : LBidReconstructable(v)})
    + Cardinality({<<k, v>> \in RotationLayers \X Values : RotationReconstructable(k, v)})
    <= 1

\* Pigeonhole on verdicts: at most one V reaches verdict_quorum.
PigeonholeVerdicts ==
    Cardinality({v \in Values : VerdictQuorumReached(v)}) <= 1

\* Combined SAFETY property
SAFETY == Pigeonhole1 /\ Pigeonhole2 /\ Pigeonhole3 /\ PigeonholeVerdicts

(***************************************************************************)
(* State constraint — bound state space for TLC by capping each pool at    *)
(* its quorum threshold.                                                   *)
(*                                                                         *)
(* Provably safe for SAFETY at f=1, n=4: same argument as in LBid_Safety   *)
(* (Pigeonholes are "pool size ≥ threshold" predicates; pool sizes only    *)
(* grow; predecessors at threshold are explored normally; TLC checks       *)
(* INVARIANTs on every visited state including those pruned by             *)
(* CONSTRAINT).                                                            *)
(***************************************************************************)

StateConstraint ==
    /\ \A k \in RotationLayers, v \in Values:
        Cardinality(SigmaPool(k, v)) <= QV
    /\ \A v \in Values:
        Cardinality(LBidSigmaPool(v)) <= QV
    /\ \A v \in Values:
        Cardinality(VerdictPool(v)) <= QV
    /\ \A k \in RotationLayers:
        Cardinality(NRPool(k)) <= QEnc
    /\ Cardinality(LBidNRPool) <= QEnc

(***************************************************************************)
(* Symmetry reduction                                                      *)
(*                                                                         *)
(* Same as LBid_Safety: honest operators are role-symmetric; values are    *)
(* symmetric.  Reduces state space by Cardinality(Honest)! ×               *)
(* Cardinality(Values)! (= 12 at n=4, |Values|=2).                          *)
(***************************************************************************)

Symmetry == Permutations(Honest) \cup Permutations(Values)

================================================================================

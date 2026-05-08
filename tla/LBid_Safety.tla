---------------------------- MODULE LBid_Safety ----------------------------
(***************************************************************************)
(* Safety verification for OBFT + L_Bid mini-consensus extension.          *)
(*                                                                         *)
(* Verifies the three Pigeonhole invariants at K' = K + 1 layers (L_Bid    *)
(* prepended as outermost gate to K rotation layers), plus the auxiliary   *)
(* Pigeonhole on verdicts:                                                  *)
(*                                                                         *)
(*   - Pigeonhole 1: at any layer (L_Bid or rotation L_k), σ-quorum on     *)
(*     any V and NR-quorum cannot both reach.                              *)
(*   - Pigeonhole 2: at any layer, at most one V can reach σ-quorum.       *)
(*   - Pigeonhole 3: across L_Bid + K rotation layers under chained        *)
(*     encryption (L_Bid outermost), at most one V signature reconstructs. *)
(*   - PigeonholeVerdicts: at most one V reaches verdict_quorum            *)
(*     (load-bearing for Pigeonhole 2 at L_Bid under verdict-gated σ-rule).*)
(*                                                                         *)
(* Model:                                                                  *)
(*   - Rotation layers 0..K-1 with bare OBFT σ-or-NR XOR rule.             *)
(*   - L_Bid as outermost gate layer with verdict-gated σ-rule:            *)
(*     honest σ at L_Bid on V iff cluster-wide verdict_quorum on V.        *)
(*   - Verdicts: each honest operator broadcasts at most one verdict per   *)
(*     slot; byzantine may equivocate (verdict on multiple V's).           *)
(*   - Byzantine actions otherwise unrestricted (controls own EKM).        *)
(*                                                                         *)
(* Out of scope: per-receiver verdict-pool views (cluster-wide              *)
(* abstraction is sufficient for SAFETY because Pigeonholes 1, 2 hold by   *)
(* XOR + cross-sign algebra at each layer regardless of verdict gating;    *)
(* the gating only tightens honest σ-pool contributions at L_Bid).          *)
(*                                                                         *)
(* See `docs/OBFT.md` Appendix B for the L_Bid protocol; `docs/OBFT-       *)
(* formal-verif.md` for the verification methodology; `tla/README.md` for  *)
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
(* sigma_partials: (op, layer, value) — rotation-layer σ partials.        *)
(* nr_partials:    (op, layer)        — rotation-layer NR partials.       *)
(* lbid_sigma:     (op, value)        — L_Bid σ partials.                 *)
(* lbid_nr:        operator           — L_Bid NR partials.                *)
(* verdicts:       (op, value)        — KindBidVerdict envelopes.         *)
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

\* Chain unlocked at rotation layer k:
\*   L_Bid is outermost — reaching any rotation layer requires L_Bid
\*   NR-quorum first, then NR-quorum at all preceding rotation layers.
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
(* Honest operators respect:                                                *)
(*   - Single verdict per operator per slot.                                *)
(*   - Cross-phase exclusivity (σ XOR NR) per (operator, layer).            *)
(*   - Single-σ-V (at most one V σ-signed) per (operator, layer).           *)
(*   - At L_Bid: σ on V only if cluster-wide verdict_quorum on V.           *)
(*                                                                         *)
(* EKM enforces these for honest operators implicitly (cross-phase keys     *)
(* are mutually exclusive); modeled here via action preconditions.          *)
(***************************************************************************)

\* Verdict broadcast: at most one verdict per honest operator
HonestVerdict(op, v) ==
    /\ op \in Honest
    /\ ~ HasVerdict(op)
    /\ verdicts' = verdicts \cup {<<op, v>>}
    /\ UNCHANGED <<sigma_partials, nr_partials, lbid_sigma, lbid_nr>>

\* L_Bid σ on V: requires cluster-wide verdict_quorum on V (verdict-gated rule)
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

\* Rotation-layer σ at L_k (same as bare OBFT)
HonestSigma(op, k, v) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)
    /\ ~ HasNR(op, k)
    /\ sigma_partials' = sigma_partials \cup {<<op, k, v>>}
    /\ UNCHANGED <<nr_partials, lbid_sigma, lbid_nr, verdicts>>

\* Rotation-layer NR at L_k (same as bare OBFT)
HonestNR(op, k) ==
    /\ op \in Honest
    /\ ~ HasSigma(op, k)
    /\ ~ HasNR(op, k)
    /\ nr_partials' = nr_partials \cup {<<op, k>>}
    /\ UNCHANGED <<sigma_partials, lbid_sigma, lbid_nr, verdicts>>

(***************************************************************************)
(* Byzantine actions — unrestricted                                        *)
(*                                                                         *)
(* Byzantine operators may:                                                 *)
(*   - Verdict-equivocate (broadcast distinct KindBidVerdicts on multiple   *)
(*     V's; Rule 8).                                                        *)
(*   - Cross-sign σ on multiple V's per layer (single-σ-V violation).       *)
(*   - Cross-phase σ + NR at the same layer (XOR violation).                *)
(*                                                                         *)
(* All of these produce slashable evidence per Rules 1-8 in OBFT.md, but   *)
(* slashing isn't modeled — only safety holds against the adversary.        *)
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
(***************************************************************************)

\* Pigeonhole 1: σ-quorum on any V at any layer AND NR-quorum at same
\* layer cannot both reach.  Algebra at each layer:
\*   honest σ + honest NR ≤ n - f (cross-phase XOR bounded)
\*   byz σ + byz NR ≤ 2f (each byz contributes ≤ 1 to each pool)
\*   Sum ≥ 2*qV = 4f+2 needed for both quorums; total ≤ 4f+1.
\*   Contradiction.  ∎
Pigeonhole1Rotation ==
    \A k \in RotationLayers:
        ~ ( (\E v \in Values: SigmaQuorumReached(k, v)) /\ NRQuorumReached(k) )

Pigeonhole1LBid ==
    ~ ( (\E v \in Values: LBidSigmaQuorumReached(v)) /\ LBidNRQuorumReached )

Pigeonhole1 == Pigeonhole1Rotation /\ Pigeonhole1LBid

\* Pigeonhole 2: at most one V σ-quorums at any single layer.  Same
\* algebra as Pigeonhole 1 but on (σ on V, σ on V') instead of (σ, NR).
Pigeonhole2Rotation ==
    \A k \in RotationLayers:
        Cardinality({v \in Values : SigmaQuorumReached(k, v)}) <= 1

Pigeonhole2LBid ==
    Cardinality({v \in Values : LBidSigmaQuorumReached(v)}) <= 1

Pigeonhole2 == Pigeonhole2Rotation /\ Pigeonhole2LBid

\* Pigeonhole 3: across L_Bid + K rotation layers under chained encryption
\* (L_Bid outermost), at most one V signature reconstructs cluster-wide.
\* Counts L_Bid-reconstructable V's plus rotation-reconstructable (k, v)
\* pairs; sum ≤ 1.  No V can reconstruct at multiple layers
\* simultaneously (Pigeonhole 1 blocks σ-quorum + NR-quorum coexistence
\* at any layer, so chained-encryption gates can't unlock above an
\* already-σ-quorum'd layer).
Pigeonhole3 ==
    Cardinality({v \in Values : LBidReconstructable(v)})
    + Cardinality({<<k, v>> \in RotationLayers \X Values : RotationReconstructable(k, v)})
    <= 1

\* Pigeonhole on verdicts: at most one V reaches verdict_quorum.
\* Each honest contributes ≤ 1 verdict envelope to one V's pool; byz
\* can equivocate but adds ≤ 1 envelope per V per byz operator.
\* Sum ≥ 2*qV = 4f+2 needed for two V's; total verdict envelopes
\* ≤ (n-f) + |Values|*f ≤ 4f+1 at f=1, |Values|=2.  Contradiction.  ∎
\* Load-bearing for Pigeonhole 2 at L_Bid since honest σ at L_Bid is
\* verdict-gated.
PigeonholeVerdicts ==
    Cardinality({v \in Values : VerdictQuorumReached(v)}) <= 1

\* Combined SAFETY property
SAFETY == Pigeonhole1 /\ Pigeonhole2 /\ Pigeonhole3 /\ PigeonholeVerdicts

(***************************************************************************)
(* Symmetry reduction                                                      *)
(*                                                                         *)
(* Honest operators are role-symmetric: any permutation of Honest yields  *)
(* an equivalent spec.  Values are similarly symmetric (no V is special). *)
(* Byzantine operators are NOT symmetric with honest (different action     *)
(* rules); rotation layers are NOT symmetric (chained-encryption gates    *)
(* depend on layer ordering).                                              *)
(*                                                                         *)
(* Use Symmetry as the SYMMETRY directive in the .cfg file to reduce      *)
(* state space exploration by `Cardinality(Honest)! × Cardinality(Values)!*)
(* (= 3! × 2! = 12 at n=4, |Values|=2).                                   *)
(***************************************************************************)

Symmetry == Permutations(Honest) \cup Permutations(Values)

================================================================================

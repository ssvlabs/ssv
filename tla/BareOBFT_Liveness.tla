--------------------------- MODULE BareOBFT_Liveness ---------------------------
(***************************************************************************)
(* Liveness verification for bare OBFT (v1) — no Phase-2.5 σ-flip /        *)
(* NR-flip machinery.                                                      *)
(*                                                                         *)
(* Verifies: under (a) BFT trust bound (≤ f byz of n=3f+1), (b) within-    *)
(* budget partial-synchrony, (c) honest EKM (strict cross-phase            *)
(* exclusivity), AND (d) no GRIEF byzantine actions, the protocol          *)
(* terminates with `output[i] = TRUE` for every honest operator i (= no    *)
(* Class A deadlock under non-grief + within-budget).                       *)
(*                                                                         *)
(* See `docs/OBFT-formal-verif.md` §5 for the property statement and §2.3  *)
(* for the GRIEF / non-grief classification.                               *)
(*                                                                         *)
(* === Adversarial action space (non-grief) ================================ *)
(*                                                                         *)
(* Honest operators follow the §3.2 honest state machine (= bare OBFT v1:  *)
(* σ-or-NR commit at T_commit, strict cross-phase exclusivity, K-layer    *)
(* fall-through via Phase 3 reconstruction walk).                          *)
(*                                                                         *)
(* Byzantine operators may, at each layer where they're the leader,        *)
(* non-deterministically choose `delivered_to ⊆ Operators` from a          *)
(* non-grief restricted set: {Operators (broadcast to all), {} (silent)}. *)
(* Selective broadcast (= proper subset of Operators) is GRIEF and         *)
(* excluded from this verification.                                        *)
(*                                                                         *)
(* Byzantine operators may also, at each layer (whether or not they're     *)
(* leader), non-deterministically choose their own commitment from         *)
(* {"sigma", "nr", "none"}, subject to honest XOR (no σ AND NR at same     *)
(* layer; that's GRIEF category 2).                                        *)
(*                                                                         *)
(* Excluded GRIEF actions (per §2.3):                                      *)
(*   - equivocation (byz signs distinct V's at same layer),                *)
(*   - cross-phase exclusivity violation (σ AND NR at same layer),         *)
(*   - fake/garbage signatures,                                            *)
(*   - EKM bypass.                                                         *)
(*                                                                         *)
(* === Network model (within-budget) ====================================== *)
(*                                                                         *)
(* For honest leader broadcasts (within their honest [T_k, T_broadcast_max *)
(* _k] window), §2.1's within-budget guarantee applies: all peers receive  *)
(* by t_broadcast + B_layer ≤ T_commit.  Modeled as `delivered_to[k] =     *)
(* Operators` for honest leaders.                                          *)
(*                                                                         *)
(* For byzantine leader broadcasts: byz can be silent (delivered_to = ∅)   *)
(* OR broadcast to all (delivered_to = Operators).  Selective is GRIEF.    *)
(*                                                                         *)
(* === Out of scope ======================================================= *)
(*                                                                         *)
(* - Per-peer delivery time variation within retention window (the spec    *)
(*   only cares whether peer retained or not).                             *)
(* - Beyond-budget delivery (= ∞), which is assumption-2 violation per     *)
(*   §2.1.  See `BareOBFT_Liveness_NoBudget.tla` for the relaxed-network  *)
(*   sibling spec that explores Class A failure modes outside Assumption 2.*)
(* - Bounded liveness (terminate within slot budget) — verified property   *)
(*   is "eventually terminates" per LIVENESS_NON_GRIEF formulation.        *)
(* - Witness-section semantics — at reconstruct time, σ pool counts        *)
(*   distinct operators who σ'd, regardless of source (own retention or    *)
(*   peer's witness section).  Witness sections only spread the LEADER's   *)
(*   signature; non-retainers can't σ on V they didn't retain.             *)
(*                                                                         *)
(* === Expected outcome =================================================== *)
(*                                                                         *)
(* Bare OBFT v1 closes Class A under non-grief + within-budget partial-    *)
(* synchrony via K-layer fall-through.  TLC verifies LIVENESS_NON_GRIEF.   *)
(***************************************************************************)

EXTENDS Naturals, FiniteSets, TLC

CONSTANTS
    Operators,    \* set of operator IDs (e.g., {op1, op2, op3, op4})
    Byzantine,    \* subset of operator IDs that are byzantine
    K             \* number of layers (≥ f+1 for Class A closure to be possible)

\* Derived constants
Honest == Operators \ Byzantine
N == Cardinality(Operators)
F == Cardinality(Byzantine)
QV == 2 * F + 1
QEnc == 2 * F + 1

\* Layer indices: 0..K-1
Layers == 0..(K - 1)

\* === V values: implicit ================================================== *
\*                                                                          *
\* Each layer k has a unique value V_k proposed by leader_of[k].  Under     *
\* non-grief, byz cannot equivocate (= GRIEF category 1), so V_k is         *
\* uniquely determined by the leader's choice and identical across all     *
\* honest views.  σ pools are keyed by layer (= implicitly by V_k); we     *
\* don't need to track V explicitly because:                                *
\*   - σ at layer k counts toward pool[k] (= pool[k][V_k] implicitly),     *
\*   - cross-layer V identity doesn't matter for liveness pool sizes,      *
\*   - byz equivocation (signing distinct V's at same layer) is GRIEF and  *
\*     excluded from the verified action space.                             *
\*                                                                          *
\* This is in contrast to the SAFETY spec where V's are explicit because   *
\* Pigeonhole 2 ("at most one V σ-quorums at any layer") needs to count    *
\* per-(layer, V) σ commitments.  Liveness is V-agnostic.                  *

ASSUME
    /\ Operators # {}
    /\ Byzantine \subseteq Operators
    /\ N = 3 * F + 1                    \* BFT-tight setting
    /\ K \in (F + 1)..N                  \* K ≥ f+1 (necessary for ≥1 honest leader)

(***************************************************************************)
(* State variables                                                         *)
(***************************************************************************)

VARIABLES
    leader_of,           \* [Layers -> Operators]: layer-k leader (set at Init)
    phase1_decided,      \* [Layers -> BOOLEAN]: has leader-k made their broadcast decision
    delivered_to,        \* [Layers -> SUBSET Operators]: subset of operators who
                         \* retained the layer-k bundle (= will σ on V_k)
    byz_commit,          \* [Byzantine -> [Layers -> {"sigma", "nr", "none"}]]:
                         \* byz operator's commitment per layer
    kindcommit_emitted,  \* [Operators -> BOOLEAN]: has operator emitted KindCommit
    output_set           \* [Operators -> BOOLEAN]: has operator's reconstruction succeeded

vars == <<leader_of, phase1_decided, delivered_to, byz_commit,
          kindcommit_emitted, output_set>>

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Honest σ pool at layer k: honest who retained the leader's bundle.
HonestSigmaAt(k) == delivered_to[k] \cap Honest

\* Honest NR pool at layer k: honest non-retainers (excludes leader since
\* honest leaders broadcast their own bundle and so always retain).
HonestNRAt(k) == {op \in Honest : op \notin delivered_to[k]}

\* Byz contribution at layer k: based on byz_commit[i][k] choice.
ByzSigmaAt(k) == {i \in Byzantine : byz_commit[i][k] = "sigma"}
ByzNRAt(k) == {i \in Byzantine : byz_commit[i][k] = "nr"}

\* Aggregate σ pool at layer k = honest retainers ∪ byz σ
SigmaPool(k) == HonestSigmaAt(k) \cup ByzSigmaAt(k)

\* Aggregate NR pool at layer k = honest non-retainers ∪ byz NR
NRPool(k) == HonestNRAt(k) \cup ByzNRAt(k)

\* σ-quorum reached at layer k iff |SigmaPool(k)| ≥ qV
SigmaQuorumReached(k) == Cardinality(SigmaPool(k)) >= QV

\* NR-quorum reached at layer k iff |NRPool(k)| ≥ qEnc
NRQuorumReached(k) == Cardinality(NRPool(k)) >= QEnc

\* Chain unlock for layer k: NR-quorum at all layers 0..k-1 (k=0 trivially unlocked).
ChainUnlocked(k) == \A j \in 0..(k - 1) : NRQuorumReached(j)

\* Layer k can be reconstructed iff σ-quorum AND chain unlocked.
LayerReconstructable(k) ==
    /\ SigmaQuorumReached(k)
    /\ ChainUnlocked(k)

\* All Phase-1 leader decisions made.
AllPhase1Decided == \A k \in Layers : phase1_decided[k]

(***************************************************************************)
(* Initial state                                                           *)
(***************************************************************************)

InitLeaderAssignments ==
    \* All injective functions from Layers to Operators.
    {f \in [Layers -> Operators] :
        \A k1, k2 \in Layers : k1 # k2 => f[k1] # f[k2]}

InitByzCommits ==
    \* All functions from Byzantine to [Layers -> {"sigma", "nr", "none"}].
    [Byzantine -> [Layers -> {"sigma", "nr", "none"}]]

Init ==
    /\ leader_of \in InitLeaderAssignments
    /\ phase1_decided = [k \in Layers |-> FALSE]
    /\ delivered_to = [k \in Layers |-> {}]
    /\ byz_commit \in InitByzCommits
    \* Non-grief constraint: byz at their OWN leader layer cannot NR.  Per
    \* §Phase-2 cross-phase exclusivity, the leader's Phase-1 σ_L^V counts
    \* as their σ-side commitment; subsequently emitting NR is grief
    \* (Rule 1 cross-signing slashable evidence).  For LIVENESS_NON_GRIEF
    \* verification we exclude this case.  Byz at their leader layer is
    \* either "sigma" (broadcast a valid bundle) or "none" (silent).
    /\ \A b \in Byzantine, k \in Layers:
        leader_of[k] = b => byz_commit[b][k] \in {"sigma", "none"}
    /\ kindcommit_emitted = [i \in Operators |-> FALSE]
    /\ output_set = [i \in Operators |-> FALSE]

(***************************************************************************)
(* Honest leader broadcast: full delivery (within-budget, all peers retain).*)
(***************************************************************************)

HonestLeaderBroadcast(k) ==
    LET ldr == leader_of[k] IN
    /\ ldr \in Honest
    /\ ~ phase1_decided[k]
    /\ phase1_decided' = [phase1_decided EXCEPT ![k] = TRUE]
    /\ delivered_to' = [delivered_to EXCEPT ![k] = Operators]
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, output_set>>

(***************************************************************************)
(* Byzantine leader broadcast — NON-GRIEF only.                            *)
(*                                                                         *)
(* Under non-grief, byz leader is constrained to all-or-nothing broadcast: *)
(*   - byz_commit = "sigma" → S = Operators (broadcast to all → h_V = n).  *)
(*   - byz_commit = "none"  → S = ∅ (didn't sign → no retainers, h_V = 0). *)
(*                                                                         *)
(* Selective broadcast (= S a proper subset, h_V ∈ {1, ..., n-1}) is       *)
(* RECLASSIFIED AS GRIEF in this liveness model.  Liveness need not hold   *)
(* under selective broadcast — it's the byz attack pattern that engineers  *)
(* the bare-OBFT Class A deadlock.  Excluded from non-grief verification.  *)
(***************************************************************************)

ByzLeaderBroadcast(k, S) ==
    LET ldr == leader_of[k] IN
    /\ ldr \in Byzantine
    /\ ~ phase1_decided[k]
    /\ \/ /\ byz_commit[ldr][k] = "sigma"
          /\ S = Operators                  \* broadcast to all (non-grief)
       \/ /\ byz_commit[ldr][k] = "none"
          /\ S = {}                          \* silent (no σ_L^V signed)
    /\ phase1_decided' = [phase1_decided EXCEPT ![k] = TRUE]
    /\ delivered_to' = [delivered_to EXCEPT ![k] = S]
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, output_set>>

(***************************************************************************)
(* Honest KindCommit emission.  Precondition: all phase-1 decisions made   *)
(* (= honest waited for T_commit).                                         *)
(***************************************************************************)

HonestEmitKindCommit(i) ==
    /\ i \in Honest
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[i]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit, output_set>>

(***************************************************************************)
(* Byzantine KindCommit emission.  Same precondition as honest.            *)
(***************************************************************************)

ByzEmitKindCommit(b) ==
    /\ b \in Byzantine
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[b]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![b] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit, output_set>>

(***************************************************************************)
(* Honest reconstruction.  Precondition: i has emitted, and either all     *)
(* contributing operators have emitted (or byz committed to silence at     *)
(* every layer).  This models "honest reconstructs at T_commit + Δ_2 with  *)
(* whatever KindCommits arrived".                                          *)
(***************************************************************************)

\* σ pool at layer k restricted to operators whose σ partial is available
\* (= they emitted KindCommit).  Byz σ available via own KindCommit or
\* leader's σ_L^V witness section (if byz is the layer leader and any
\* honest retained the bundle).
SigmaPoolEmitted(k) ==
    {op \in (Honest \cap delivered_to[k]) : kindcommit_emitted[op]}
    \cup
    {b \in ByzSigmaAt(k) :
        \/ kindcommit_emitted[b]
        \/ /\ leader_of[k] = b
           /\ \E i \in (Honest \cap delivered_to[k]) : kindcommit_emitted[i]}

NRPoolEmitted(k) ==
    {op \in (Honest \ delivered_to[k]) : kindcommit_emitted[op]}
    \cup
    {b \in ByzNRAt(k) : kindcommit_emitted[b]}

SigmaQuorumReachedEmitted(k) == Cardinality(SigmaPoolEmitted(k)) >= QV
NRQuorumReachedEmitted(k) == Cardinality(NRPoolEmitted(k)) >= QEnc

ChainUnlockedEmitted(k) ==
    \A j \in 0..(k - 1) : NRQuorumReachedEmitted(j)

LayerReconstructableEmitted(k) ==
    /\ SigmaQuorumReachedEmitted(k)
    /\ ChainUnlockedEmitted(k)

HonestReconstruct(i) ==
    /\ i \in Honest
    /\ kindcommit_emitted[i]
    /\ ~ output_set[i]
    /\ \E k \in Layers : LayerReconstructableEmitted(k)
    /\ output_set' = [output_set EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E k \in Layers : HonestLeaderBroadcast(k)
    \/ \E k \in Layers, S \in SUBSET Operators : ByzLeaderBroadcast(k, S)
    \/ \E i \in Honest : HonestEmitKindCommit(i)
    \/ \E b \in Byzantine : ByzEmitKindCommit(b)
    \/ \E i \in Honest : HonestReconstruct(i)

(***************************************************************************)
(* Fairness                                                                *)
(*                                                                         *)
(* Honest actions: weak fairness — eventually fire if continuously enabled.*)
(* Byz actions: NO fairness — byz may stay silent (= choose not to act).  *)
(***************************************************************************)

Fairness ==
    /\ \A k \in Layers : WF_vars(HonestLeaderBroadcast(k))
    /\ \A k \in Layers :
        WF_vars(\E S \in SUBSET Operators : ByzLeaderBroadcast(k, S))
    /\ \A i \in Honest : WF_vars(HonestEmitKindCommit(i))
    /\ \A i \in Honest : WF_vars(HonestReconstruct(i))
    \* Note: NO fairness on ByzEmitKindCommit — byz may stay silent at
    \* Phase 2 indefinitely (= valid non-grief behavior).  Honest reconstruct
    \* must succeed without relying on byz emissions.

Spec == Init /\ [][Next]_vars /\ Fairness

(***************************************************************************)
(* LIVENESS_NON_GRIEF property                                              *)
(*                                                                         *)
(* Every honest operator eventually has output set.                        *)
(*                                                                         *)
(* TLC verifies this as a TEMPORAL property under Spec's fairness clauses.  *)
(* If TLC finds a counterexample, it surfaces an execution where some      *)
(* honest operator never reaches output_set = TRUE — i.e., a Class A      *)
(* deadlock.                                                                *)
(***************************************************************************)

LIVENESS_NON_GRIEF == \A i \in Honest : <>(output_set[i])

(***************************************************************************)
(* Type invariant — sanity check on state structure                        *)
(***************************************************************************)

TypeOK ==
    /\ leader_of \in [Layers -> Operators]
    /\ phase1_decided \in [Layers -> BOOLEAN]
    /\ delivered_to \in [Layers -> SUBSET Operators]
    /\ byz_commit \in [Byzantine -> [Layers -> {"sigma", "nr", "none"}]]
    /\ kindcommit_emitted \in [Operators -> BOOLEAN]
    /\ output_set \in [Operators -> BOOLEAN]

(***************************************************************************)
(* Symmetry — TLC explores one canonical state per equivalence class       *)
(* under permutations of Honest operators.  Byzantine operators are NOT    *)
(* permuted (they're distinguished by Byzantine designation).              *)
(*                                                                         *)
(* CAVEAT: TLC docs warn symmetry under liveness checking can miss        *)
(* violations.  For initial exploration we keep symmetry off (cfg-side     *)
(* choice); enable cautiously after the encoding is validated.            *)
(***************************************************************************)

Symmetry == Permutations(Honest)

================================================================================

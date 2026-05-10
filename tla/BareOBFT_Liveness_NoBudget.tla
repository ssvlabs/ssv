--------------------- MODULE BareOBFT_Liveness_NoBudget ----------------------
(***************************************************************************)
(* Liveness exploration for bare OBFT (v1) under RELAXED network           *)
(* assumptions — sibling of BareOBFT_Liveness.tla.                          *)
(*                                                                         *)
(* === What this spec is for =============================================*)
(*                                                                         *)
(* `BareOBFT_Liveness.tla` verifies LIVENESS_NON_GRIEF under (a) BFT trust *)
(* bound, (b) within-budget partial-synchrony (Assumption 2), (c) honest   *)
(* EKM, and (d) non-grief byz.  Under those assumptions, every honest      *)
(* operator eventually has output_set[i] = TRUE.                            *)
(*                                                                         *)
(* This spec **relaxes Assumption 2** for honest leader broadcasts —      *)
(* allows the leader's bundle to deliver to ANY subset of operators        *)
(* (including the leader themselves only).  Models real-world propagation  *)
(* tails past `B_k` budget that the verified spec excludes by construction.*)
(*                                                                         *)
(* === Property =========================================================*)
(*                                                                         *)
(* The verified property `LIVENESS_NON_GRIEF` is checked *as-is* — but    *)
(* here it is EXPECTED TO FAIL.  Each counterexample TLC surfaces is a    *)
(* Class A deadlock the OBFT.md spec explicitly does not close (h_V=1     *)
(* selective Phase-1 delivery, asymmetric propagation past T_commit, etc.).*)
(* Counterexample traces are the diagnostic output: each should match an  *)
(* already-documented Class A failure pattern; any pattern that doesn't   *)
(* match is a spec-coverage gap to investigate.                           *)
(*                                                                         *)
(* === What changes from BareOBFT_Liveness.tla ==========================*)
(*                                                                         *)
(* SINGLE MODIFICATION: `HonestLeaderBroadcast(k)` → `HonestLeaderBroadcast*)
(* (k, S)`.  S is now an existentially-quantified subset of Operators      *)
(* (subject to the leader being in S — signer's-own-view invariant: a     *)
(* leader always retains their own bundle).                                *)
(*                                                                         *)
(* Old: `delivered_to' = [delivered_to EXCEPT ![k] = Operators]`           *)
(* New: `S \subseteq Operators ∧ leader \in S`                             *)
(*      `delivered_to' = [delivered_to EXCEPT ![k] = S]`                   *)
(*                                                                         *)
(* This generalizes:                                                       *)
(*   - S = Operators                 → within-budget (current verified)    *)
(*   - {leader} ⊆ S ⊊ Operators      → propagation tail (Class A)          *)
(*   - S = {leader}                  → effective silent broadcast          *)
(*                                                                         *)
(* Byz behavior unchanged: still non-grief (all-or-silent for leader      *)
(* broadcast, σ/NR/none commit, no equivocation/cross-sign/fake/EKM-      *)
(* bypass).  This isolates the source of any deadlock to honest-leader-   *)
(* propagation issues, not byz behavior.                                   *)
(*                                                                         *)
(* === State space caveat ================================================*)
(*                                                                         *)
(* Relaxed delivery adds 2^n choices per honest leader broadcast.  At     *)
(* n=4, K=2 each honest leader has 8 valid S values (subsets containing  *)
(* the leader).  Total state space at K=2 expected ~1-10M states; K=4    *)
(* may not be tractable without symmetry / state-constraint pruning.      *)
(*                                                                         *)
(* TLC will halt on the FIRST counterexample for liveness checking, not  *)
(* exhaustively enumerate all.  To find multiple distinct patterns, run  *)
(* repeatedly with different state-constraint cuts or modified configs.  *)
(*                                                                         *)
(* See `docs/OBFT-formal-verif.md` §5.4 for the documented Class A scope *)
(* and §7.1 for the verified within-budget result this spec extends.     *)
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
(* Helper definitions — IDENTICAL to BareOBFT_Liveness.tla.                *)
(***************************************************************************)

HonestSigmaAt(k) == delivered_to[k] \cap Honest
HonestNRAt(k) == {op \in Honest : op \notin delivered_to[k]}

ByzSigmaAt(k) == {i \in Byzantine : byz_commit[i][k] = "sigma"}
ByzNRAt(k) == {i \in Byzantine : byz_commit[i][k] = "nr"}

SigmaPool(k) == HonestSigmaAt(k) \cup ByzSigmaAt(k)
NRPool(k) == HonestNRAt(k) \cup ByzNRAt(k)

SigmaQuorumReached(k) == Cardinality(SigmaPool(k)) >= QV
NRQuorumReached(k) == Cardinality(NRPool(k)) >= QEnc
ChainUnlocked(k) == \A j \in 0..(k - 1) : NRQuorumReached(j)

LayerReconstructable(k) ==
    /\ SigmaQuorumReached(k)
    /\ ChainUnlocked(k)

AllPhase1Decided == \A k \in Layers : phase1_decided[k]

(***************************************************************************)
(* Initial state — IDENTICAL to BareOBFT_Liveness.tla.                     *)
(***************************************************************************)

InitLeaderAssignments ==
    {f \in [Layers -> Operators] :
        \A k1, k2 \in Layers : k1 # k2 => f[k1] # f[k2]}

InitByzCommits ==
    [Byzantine -> [Layers -> {"sigma", "nr", "none"}]]

Init ==
    /\ leader_of \in InitLeaderAssignments
    /\ phase1_decided = [k \in Layers |-> FALSE]
    /\ delivered_to = [k \in Layers |-> {}]
    /\ byz_commit \in InitByzCommits
    /\ \A b \in Byzantine, k \in Layers:
        leader_of[k] = b => byz_commit[b][k] \in {"sigma", "none"}
    /\ kindcommit_emitted = [i \in Operators |-> FALSE]
    /\ output_set = [i \in Operators |-> FALSE]

(***************************************************************************)
(* HONEST LEADER BROADCAST — RELAXED (this is the only change from         *)
(* BareOBFT_Liveness.tla).                                                 *)
(*                                                                         *)
(* Models honest leader broadcast under relaxed Assumption 2: the bundle   *)
(* may reach ANY subset of operators (S), with the only constraint that   *)
(* the leader retains their own bundle (signer's-own-view).                *)
(*                                                                         *)
(* This subsumes:                                                          *)
(*   - within-budget delivery (S = Operators)                              *)
(*   - propagation tail (leader ∈ S ⊊ Operators)                           *)
(*   - effective silence (S = {leader})                                    *)
(***************************************************************************)

HonestLeaderBroadcast(k, S) ==
    LET ldr == leader_of[k] IN
    /\ ldr \in Honest
    /\ ~ phase1_decided[k]
    /\ S \subseteq Operators
    /\ ldr \in S                          \* signer's-own-view: leader retains
    /\ phase1_decided' = [phase1_decided EXCEPT ![k] = TRUE]
    /\ delivered_to' = [delivered_to EXCEPT ![k] = S]
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, output_set>>

(***************************************************************************)
(* Byzantine leader broadcast — IDENTICAL to BareOBFT_Liveness.tla.        *)
(* Non-grief: all-or-silent (no selective delivery — that's grief).        *)
(***************************************************************************)

ByzLeaderBroadcast(k, S) ==
    LET ldr == leader_of[k] IN
    /\ ldr \in Byzantine
    /\ ~ phase1_decided[k]
    /\ \/ /\ byz_commit[ldr][k] = "sigma"
          /\ S = Operators
       \/ /\ byz_commit[ldr][k] = "none"
          /\ S = {}
    /\ phase1_decided' = [phase1_decided EXCEPT ![k] = TRUE]
    /\ delivered_to' = [delivered_to EXCEPT ![k] = S]
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, output_set>>

(***************************************************************************)
(* KindCommit emission — IDENTICAL to BareOBFT_Liveness.tla.               *)
(***************************************************************************)

HonestEmitKindCommit(i) ==
    /\ i \in Honest
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[i]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit, output_set>>

ByzEmitKindCommit(b) ==
    /\ b \in Byzantine
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[b]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![b] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit, output_set>>

(***************************************************************************)
(* Reconstruction — IDENTICAL to BareOBFT_Liveness.tla.                    *)
(***************************************************************************)

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
(* Next-state relation — UPDATED: HonestLeaderBroadcast now takes S param. *)
(***************************************************************************)

Next ==
    \/ \E k \in Layers, S \in SUBSET Operators : HonestLeaderBroadcast(k, S)
    \/ \E k \in Layers, S \in SUBSET Operators : ByzLeaderBroadcast(k, S)
    \/ \E i \in Honest : HonestEmitKindCommit(i)
    \/ \E b \in Byzantine : ByzEmitKindCommit(b)
    \/ \E i \in Honest : HonestReconstruct(i)

(***************************************************************************)
(* Fairness — UPDATED for parameterized HonestLeaderBroadcast.             *)
(***************************************************************************)

Fairness ==
    /\ \A k \in Layers :
        WF_vars(\E S \in SUBSET Operators : HonestLeaderBroadcast(k, S))
    /\ \A k \in Layers :
        WF_vars(\E S \in SUBSET Operators : ByzLeaderBroadcast(k, S))
    /\ \A i \in Honest : WF_vars(HonestEmitKindCommit(i))
    /\ \A i \in Honest : WF_vars(HonestReconstruct(i))

Spec == Init /\ [][Next]_vars /\ Fairness

(***************************************************************************)
(* LIVENESS_NON_GRIEF property — IDENTICAL to BareOBFT_Liveness.tla.        *)
(*                                                                         *)
(* Under relaxed Assumption 2, this property is EXPECTED TO FAIL.  Each   *)
(* counterexample is a Class A deadlock — should match a documented       *)
(* failure pattern from OBFT.md §Failure modes / §Liveness.                *)
(***************************************************************************)

LIVENESS_NON_GRIEF == \A i \in Honest : <>(output_set[i])

(***************************************************************************)
(* Type invariant.                                                         *)
(***************************************************************************)

TypeOK ==
    /\ leader_of \in [Layers -> Operators]
    /\ phase1_decided \in [Layers -> BOOLEAN]
    /\ delivered_to \in [Layers -> SUBSET Operators]
    /\ byz_commit \in [Byzantine -> [Layers -> {"sigma", "nr", "none"}]]
    /\ kindcommit_emitted \in [Operators -> BOOLEAN]
    /\ output_set \in [Operators -> BOOLEAN]

(***************************************************************************)
(* Symmetry — TLC explores one canonical state per equivalence class      *)
(* under permutations of Honest operators.                                 *)
(***************************************************************************)

Symmetry == Permutations(Honest)

================================================================================

--------------------- MODULE BareOBFT_Liveness_NoBudget ----------------------
(***************************************************************************)
(* Liveness exploration for bare OBFT under RELAXED network assumptions —  *)
(* sibling of BareOBFT_Liveness.tla.                                        *)
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
(* See `docs/OBFT-formal-verif.md` §5.2 for the documented Class A scope *)
(* and §7.1 for the verified within-budget result this spec extends.     *)
(*                                                                         *)
(* === TLC findings (2026-05-10, n=4, f=1, K=2) =========================*)
(*                                                                         *)
(* TLC found a counterexample at depth 6 in 9 seconds (138,682 states     *)
(* generated, 69,462 distinct, 10,820 left on queue when first violation  *)
(* surfaced).  Trace classification: documented `h_V=1 selective Phase-1  *)
(* delivery` Class A pattern with the **byz-silent sub-case** that the    *)
(* OBFT.md §Liveness narrative didn't fully describe.                      *)
(*                                                                         *)
(*   Trace summary:                                                        *)
(*   - leader_of = (L_0 → op1, L_1 → op2);  Byzantine = {op4}              *)
(*   - HonestLeaderBroadcast(L_0, S = {op1, op2})                          *)
(*       → propagation tail: op3, op4 didn't retain V_0                    *)
(*   - HonestLeaderBroadcast(L_1, S = {op2})                               *)
(*       → propagation tail: op1, op3, op4 didn't retain V_1               *)
(*   - All 3 honest emit KindCommit; byz op4 stays silent (non-grief OK).  *)
(*   - State stutters: no flip enabled; output_set = ⟨all FALSE⟩.         *)
(*                                                                         *)
(*   Why σ-flip blocked at L_0 (lone NR-er op3):                           *)
(*       nr_nl  = |{op3}| = 1     →  1 < f+1 = 2  ✓  (first cond passes)  *)
(*       s_post = |{op2}| + 1 = 2                                          *)
(*       a_count = |{op4 silent}| = 1                                      *)
(*                                  →  2 ≥ 1 + 2 = 3 ✗  (second cond fails)*)
(*                                                                         *)
(*   This blocks on σ-flip's *second* trigger condition (s_post ≥ A + 2f),*)
(*   distinct from OBFT.md §Liveness's discussion of h_V=1 (which focuses *)
(*   on the *first* condition `snap_NR_nl < f+1` blocked by 2 honest      *)
(*   NR-ers).  Both sub-cases have the same outcome (slot misses); the    *)
(*   blocking mechanism within Phase-2.5 differs by byz behavior.         *)
(*                                                                         *)
(* Result interpretation: this is the documented Class A scope (h_V=1     *)
(* not closed by Phase-2.5).  The byz-silent sub-case complements the    *)
(* classical sub-case OBFT.md describes; both are part of the same        *)
(* "Phase-2.5 doesn't close h_V=1" claim.  No spec change is needed; the  *)
(* OBFT.md narrative for h_V=1 should be updated to acknowledge both     *)
(* sub-cases.                                                              *)
(*                                                                         *)
(* Future work: re-run at K=3, K=4 with state-constraint pruning;        *)
(* iteratively block this pattern to surface other distinct Class A      *)
(* traces (multi-layer compound failures, etc.).                          *)
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
    flipped_to,          \* [Honest -> [Layers -> {"none", "sigma", "nr"}]]:
                         \* "none" = no flip; "sigma" = σ-flipped (NR → σ);
                         \* "nr" = NR-flipped (σ → NR).  Single flip per layer.
    output_set           \* [Operators -> BOOLEAN]: has operator's reconstruction succeeded

vars == <<leader_of, phase1_decided, delivered_to, byz_commit,
          kindcommit_emitted, flipped_to, output_set>>

(***************************************************************************)
(* Helper definitions — IDENTICAL to BareOBFT_Liveness.tla.                *)
(***************************************************************************)

HonestSigmaAt(k) ==
    (delivered_to[k] \cap Honest) \cup
    {op \in Honest : op \notin delivered_to[k] /\ flipped_to[op][k] = "sigma"}

HonestNRAt(k) ==
    {op \in Honest :
        \/ op \notin delivered_to[k]
        \/ (op = leader_of[k] /\ flipped_to[op][k] = "nr")}

ByzSigmaAt(k) == {i \in Byzantine : byz_commit[i][k] = "sigma"}
ByzNRAt(k) == {i \in Byzantine : byz_commit[i][k] = "nr"}

SigmaPool(k) == HonestSigmaAt(k) \cup ByzSigmaAt(k)
NRPool(k) == HonestNRAt(k) \cup ByzNRAt(k)
NRPoolNonLeader(k) == NRPool(k) \ {leader_of[k]}

SigmaQuorumReached(k) == Cardinality(SigmaPool(k)) >= QV
NRQuorumReached(k) == Cardinality(NRPool(k)) >= QEnc
ChainUnlocked(k) == \A j \in 0..(k - 1) : NRQuorumReached(j)

LayerReconstructable(k) ==
    /\ SigmaQuorumReached(k)
    /\ ChainUnlocked(k)

AllPhase1Decided == \A k \in Layers : phase1_decided[k]

ByzWillEmit(b) == \E k \in Layers : byz_commit[b][k] # "none"

AllKindCommitsSettled ==
    /\ \A i \in Honest : kindcommit_emitted[i]
    /\ \A b \in Byzantine :
        \/ kindcommit_emitted[b]
        \/ ~ ByzWillEmit(b)

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
    /\ flipped_to = [i \in Honest |-> [k \in Layers |-> "none"]]
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
(*                                                                         *)
(* The original BareOBFT_Liveness's "honest leader delivers to all"       *)
(* axiom is recovered when TLC happens to choose S = Operators for every  *)
(* layer; but TLC also explores all the other S choices, surfacing the    *)
(* spec's documented Class A scope.                                       *)
(***************************************************************************)

HonestLeaderBroadcast(k, S) ==
    LET ldr == leader_of[k] IN
    /\ ldr \in Honest
    /\ ~ phase1_decided[k]
    /\ S \subseteq Operators
    /\ ldr \in S                          \* signer's-own-view: leader retains
    /\ phase1_decided' = [phase1_decided EXCEPT ![k] = TRUE]
    /\ delivered_to' = [delivered_to EXCEPT ![k] = S]
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, flipped_to,
                   output_set>>

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
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, flipped_to,
                   output_set>>

(***************************************************************************)
(* KindCommit emission — IDENTICAL to BareOBFT_Liveness.tla.               *)
(***************************************************************************)

HonestEmitKindCommit(i) ==
    /\ i \in Honest
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[i]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   flipped_to, output_set>>

ByzEmitKindCommit(b) ==
    /\ b \in Byzantine
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[b]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![b] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   flipped_to, output_set>>

(***************************************************************************)
(* Phase-2.5 σ-flip / NR-flip — IDENTICAL to BareOBFT_Liveness.tla.        *)
(* These triggers will (correctly) NOT fire in many of the relaxed-       *)
(* delivery cases — that's the spec's documented behavior.                 *)
(***************************************************************************)

SigmaPoolEmitted(k) ==
    {op \in (Honest \cap delivered_to[k]) : kindcommit_emitted[op]}
    \cup
    {op \in (Honest \ delivered_to[k]) :
        flipped_to[op][k] = "sigma" /\ kindcommit_emitted[op]}
    \cup
    {b \in ByzSigmaAt(k) :
        \/ kindcommit_emitted[b]
        \/ /\ leader_of[k] = b
           /\ \E i \in (Honest \cap delivered_to[k]) : kindcommit_emitted[i]}

NRPoolEmitted(k) ==
    {op \in (Honest \ delivered_to[k]) : kindcommit_emitted[op]}
    \cup
    {op \in (Honest \cap delivered_to[k]) :
        flipped_to[op][k] = "nr" /\ kindcommit_emitted[op]}
    \cup
    {b \in ByzNRAt(k) : kindcommit_emitted[b]}

SigmaQuorumReachedEmitted(k) == Cardinality(SigmaPoolEmitted(k)) >= QV
NRQuorumReachedEmitted(k) == Cardinality(NRPoolEmitted(k)) >= QEnc

ChainUnlockedEmitted(k) ==
    \A j \in 0..(k - 1) : NRQuorumReachedEmitted(j)

LayerReconstructableEmitted(k) ==
    /\ SigmaQuorumReachedEmitted(k)
    /\ ChainUnlockedEmitted(k)

SilentEmitted == {op \in Operators : ~ kindcommit_emitted[op]}

SigmaPoolNonLeaderEmitted(k) == SigmaPoolEmitted(k) \ {leader_of[k]}

SigmaFlipTriggered(k) ==
    LET nr_nl   == Cardinality(NRPoolEmitted(k) \ {leader_of[k]})
        s_post  == Cardinality(SigmaPoolNonLeaderEmitted(k)) + 1
        a_count == Cardinality(SilentEmitted)
    IN
       /\ nr_nl < F + 1
       /\ s_post >= a_count + 2 * F

NRFlipTriggered(k) ==
    LET s_nl    == Cardinality(SigmaPoolNonLeaderEmitted(k))
        nr_nl   == Cardinality(NRPoolEmitted(k) \ {leader_of[k]})
        a_count == Cardinality(SilentEmitted)
    IN
       /\ s_nl < F
       /\ nr_nl >= a_count + 2 * F

VAvailable(k) == SigmaPoolEmitted(k) # {}

HonestSigmaFlip(i, k) ==
    /\ AllKindCommitsSettled
    /\ i \in Honest
    /\ i # leader_of[k]
    /\ kindcommit_emitted[i]
    /\ i \notin delivered_to[k]
    /\ flipped_to[i][k] = "none"
    /\ SigmaFlipTriggered(k)
    /\ VAvailable(k)
    /\ flipped_to' = [flipped_to EXCEPT ![i][k] = "sigma"]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted, output_set>>

HonestNRFlip(i, k) ==
    /\ AllKindCommitsSettled
    /\ i \in Honest
    /\ i = leader_of[k]
    /\ kindcommit_emitted[i]
    /\ flipped_to[i][k] = "none"
    /\ NRFlipTriggered(k)
    /\ flipped_to' = [flipped_to EXCEPT ![i][k] = "nr"]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted, output_set>>

HonestReconstruct(i) ==
    /\ i \in Honest
    /\ kindcommit_emitted[i]
    /\ ~ output_set[i]
    /\ \E k \in Layers : LayerReconstructableEmitted(k)
    /\ output_set' = [output_set EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted, flipped_to>>

(***************************************************************************)
(* Next-state relation — UPDATED: HonestLeaderBroadcast now takes S param. *)
(***************************************************************************)

Next ==
    \/ \E k \in Layers, S \in SUBSET Operators : HonestLeaderBroadcast(k, S)
    \/ \E k \in Layers, S \in SUBSET Operators : ByzLeaderBroadcast(k, S)
    \/ \E i \in Honest : HonestEmitKindCommit(i)
    \/ \E b \in Byzantine : ByzEmitKindCommit(b)
    \/ \E i \in Honest, k \in Layers : HonestSigmaFlip(i, k)
    \/ \E i \in Honest, k \in Layers : HonestNRFlip(i, k)
    \/ \E i \in Honest : HonestReconstruct(i)

(***************************************************************************)
(* Fairness — UPDATED for parameterized HonestLeaderBroadcast.             *)
(*                                                                         *)
(* Weak fairness: leader will eventually broadcast (with SOME delivery     *)
(* outcome).  TLC explores each S choice as a separate exploration branch. *)
(***************************************************************************)

Fairness ==
    /\ \A k \in Layers :
        WF_vars(\E S \in SUBSET Operators : HonestLeaderBroadcast(k, S))
    /\ \A k \in Layers :
        WF_vars(\E S \in SUBSET Operators : ByzLeaderBroadcast(k, S))
    /\ \A i \in Honest : WF_vars(HonestEmitKindCommit(i))
    /\ \A i \in Honest, k \in Layers : WF_vars(HonestSigmaFlip(i, k))
    /\ \A i \in Honest, k \in Layers : WF_vars(HonestNRFlip(i, k))
    /\ \A i \in Honest : WF_vars(HonestReconstruct(i))
    \* No fairness on ByzEmitKindCommit — byz may stay silent at Phase 2.

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
    /\ flipped_to \in [Honest -> [Layers -> {"none", "sigma", "nr"}]]
    /\ output_set \in [Operators -> BOOLEAN]

(***************************************************************************)
(* Symmetry — TLC explores one canonical state per equivalence class      *)
(* under permutations of Honest operators.                                 *)
(*                                                                         *)
(* CAVEAT: TLC docs warn symmetry under liveness checking can miss        *)
(* violations.  For initial exploration we keep symmetry off (cfg-side     *)
(* choice); enable cautiously after the encoding is validated.            *)
(***************************************************************************)

Symmetry == Permutations(Honest)

================================================================================

--------------------------- MODULE BareOBFT_Liveness ---------------------------
(***************************************************************************)
(* Liveness verification for bare OBFT — `LIVENESS_NON_GRIEF` (Class A     *)
(* closure).                                                               *)
(*                                                                         *)
(* Verifies: under (a) BFT trust bound (≤ f byz of n=3f+1), (b) within-    *)
(* budget partial-synchrony, (c) honest EKM, AND (d) no GRIEF byzantine    *)
(* actions, the protocol terminates with `output[i] = TRUE` for every      *)
(* honest operator i (= no Class A deadlock).                              *)
(*                                                                         *)
(* See `docs/OBFT-formal-verif.md` §5 for the property statement and §2.3  *)
(* for the GRIEF / non-grief classification.                               *)
(*                                                                         *)
(* === Adversarial action space (non-grief) ================================ *)
(*                                                                         *)
(* Honest operators follow the §3.2 honest state machine.                  *)
(*                                                                         *)
(* Byzantine operators may, at each layer where they're the leader,        *)
(* non-deterministically choose any `delivered_to ⊆ Operators` — the      *)
(* subset of peers who receive the bundle in time to retain.  This         *)
(* subsumes:                                                               *)
(*   - silent (delivered_to = ∅),                                          *)
(*   - honest-mimicking (delivered_to = Operators),                        *)
(*   - mesh-asymmetric broadcast (any proper subset),                      *)
(*   - late broadcast (= effective subset due to delivery times falling    *)
(*     past T_commit per §2.1's within-budget delivery model).             *)
(*                                                                         *)
(* Byzantine operators may also, at each layer (whether or not they're     *)
(* leader), non-deterministically choose their own commitment from {σ,    *)
(* NR, none} — subject to honest XOR (no σ AND NR at same layer; that's    *)
(* GRIEF category 2).  Under non-grief honest-mimicking, byz's σ choice    *)
(* matches "would honest σ here" (= they retained).  But the spec lets     *)
(* byz σ even if the spec's model says they didn't retain — this is a     *)
(* non-grief byz-controls-own-EKM option.                                  *)
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
(* For byzantine leader broadcasts: byz can broadcast at any time,         *)
(* including past T_broadcast_max_k — peers' delivery times can fall       *)
(* past T_commit and they don't retain.  Modeled as `delivered_to[k] =     *)
(* any subset of Operators`.                                               *)
(*                                                                         *)
(* === Out of scope ======================================================= *)
(*                                                                         *)
(* - Per-peer delivery time variation within retention window (the spec    *)
(*   only cares whether peer retained or not).                             *)
(* - Beyond-budget delivery (= ∞), which is assumption-2 violation per     *)
(*   §2.1.                                                                 *)
(* - Bounded liveness (terminate within slot budget) — verified property   *)
(*   is "eventually terminates" per LIVENESS_NON_GRIEF formulation.        *)
(* - Witness-section semantics — at reconstruct time, σ pool counts        *)
(*   distinct operators who σ'd, regardless of source (own retention or    *)
(*   peer's witness section).  Witness sections only spread the LEADER's   *)
(*   signature; non-retainers can't σ on V they didn't retain.             *)
(*                                                                         *)
(* === Expected outcome =================================================== *)
(*                                                                         *)
(* If liveness verifies: bare OBFT closes Class A under non-grief +        *)
(* within-budget partial-synchrony.                                        *)
(*                                                                         *)
(* If TLC finds a counterexample: surfaces a Class A deadlock under non-   *)
(* grief, requiring either a real protocol fix (re-design σ/NR coupling)   *)
(* or a re-classification of the trigger action as GRIEF.                  *)
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
    nr_flipped,          \* [Honest -> [Layers -> BOOLEAN]]: has honest σ-er
                         \* emitted an NR-flip (KindNRFlip message) at this layer
                         \* — Phase-2.5 cross-signing under valid trigger evidence
    output_set           \* [Operators -> BOOLEAN]: has operator's reconstruction succeeded

vars == <<leader_of, phase1_decided, delivered_to, byz_commit,
          kindcommit_emitted, nr_flipped, output_set>>

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Honest σ pool at layer k = honest operators in delivered_to[k] (= retained V_k).
\* Honest NR pool at layer k = honest operators NOT in delivered_to[k] (= didn't
\* retain), PLUS honest σ-ers who NR-flipped at k (Phase-2.5 cross-sign under
\* valid trigger evidence — added by HonestNRFlip action below).
HonestSigmaAt(k) == delivered_to[k] \cap Honest
HonestNRAt(k) ==
    (Honest \ delivered_to[k]) \cup
    {i \in (Honest \cap delivered_to[k]) : nr_flipped[i][k]}

\* Byz contribution at layer k: based on byz_commit[i][k] choice.
ByzSigmaAt(k) == {i \in Byzantine : byz_commit[i][k] = "sigma"}
ByzNRAt(k) == {i \in Byzantine : byz_commit[i][k] = "nr"}

\* Aggregate σ pool at layer k = honest who retained ∪ byz who chose σ
SigmaPool(k) == HonestSigmaAt(k) \cup ByzSigmaAt(k)

\* Aggregate NR pool at layer k = honest who didn't retain ∪ byz who chose NR
NRPool(k) == HonestNRAt(k) \cup ByzNRAt(k)

\* σ-quorum reached at layer k iff |SigmaPool(k)| ≥ qV
\* (Pool grows monotonically as KindCommits emit; this is the "final" pool
\* assuming all relevant emissions happened — see "ChainUnlockedFromEmitted"
\* below for the realtime version that gates reconstruct.)
SigmaQuorumReached(k) == Cardinality(SigmaPool(k)) >= QV

\* NR-quorum reached at layer k iff |NRPool(k)| ≥ qEnc
NRQuorumReached(k) == Cardinality(NRPool(k)) >= QEnc

\* Chain unlock for layer k: NR-quorum at all layers 0..k-1 (k=0 trivially unlocked).
ChainUnlocked(k) == \A j \in 0..(k - 1) : NRQuorumReached(j)

\* Layer k can be reconstructed iff σ-quorum AND chain unlocked.
\* (For honest reconstruction, also need that the KindCommits providing the
\* partials have been emitted; see HonestReconstruct precondition.)
LayerReconstructable(k) ==
    /\ SigmaQuorumReached(k)
    /\ ChainUnlocked(k)

\* All Phase-1 leader decisions made.
AllPhase1Decided == \A k \in Layers : phase1_decided[k]

\* All operators who will ever emit KindCommit have done so.
\* In our model: honest always emit (under WF); byz may or may not.  This
\* predicate is true once the set of emitters has stabilized.  Modeled as
\* "all honest have emitted AND each byz has either emitted or is permanently
\* silent (= byz_commit indicates `none` at every layer)".  We approximate:
\* a byz with byz_commit = ⟨all "none"⟩ is silent and won't emit.
ByzWillEmit(b) == \E k \in Layers : byz_commit[b][k] # "none"

AllKindCommitsSettled ==
    /\ \A i \in Honest : kindcommit_emitted[i]
    /\ \A b \in Byzantine :
        \/ kindcommit_emitted[b]
        \/ ~ ByzWillEmit(b)

(***************************************************************************)
(* Initial state                                                           *)
(*                                                                         *)
(* leader_of: chosen non-deterministically as any function from Layers to  *)
(* Operators, subject to:                                                  *)
(*   - injective (distinct leader per layer; matches OBFT rotation)        *)
(*                                                                         *)
(* TLC will explore all valid leader assignments (with symmetry collapsing *)
(* equivalent ones).  This includes byz-at-any-position.                   *)
(*                                                                         *)
(* byz_commit: chosen non-deterministically per byz-operator-per-layer in  *)
(* {"sigma", "nr", "none"}, with the XOR constraint enforced (already via  *)
(* the type — at most one of σ-pool / NR-pool gets each operator).         *)
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
    \* §Phase-2 cross-phase exclusivity, the leader's Phase-1 σ_L^V counts as
    \* their σ-side commitment; subsequently emitting NR is grief (Rule 1
    \* cross-signing slashable evidence).  For LIVENESS_NON_GRIEF verification
    \* we exclude this case.  Byz at their leader layer is either "sigma"
    \* (broadcast a valid bundle) or "none" (silent — didn't broadcast).
    /\ \A b \in Byzantine, k \in Layers:
        leader_of[k] = b => byz_commit[b][k] \in {"sigma", "none"}
    /\ kindcommit_emitted = [i \in Operators |-> FALSE]
    /\ nr_flipped = [i \in Honest |-> [k \in Layers |-> FALSE]]
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
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, nr_flipped,
                   output_set>>

(***************************************************************************)
(* Byzantine leader broadcast decision: delivered_to[k] non-deterministic   *)
(* subset of Operators.  Subsumes silent (∅), honest-mimicking (full),     *)
(* and partial / late delivery (any proper subset).                        *)
(***************************************************************************)

ByzLeaderBroadcast(k, S) ==
    LET ldr == leader_of[k] IN
    /\ ldr \in Byzantine
    /\ ~ phase1_decided[k]
    /\ S \subseteq Operators
    \* Coherence: if byz_commit at this layer is "sigma", byz signed σ_L^V
    \* and may broadcast to any subset (incl. ∅ for "signed but withheld",
    \* though that produces no retainers).  If "none", byz didn't sign σ_L^V
    \* — per §Phase-1 "leader who broadcasts (V, σ^op) without σ^V is treated
    \* as not having broadcast at all", so S must be ∅.
    /\ byz_commit[ldr][k] = "none" => S = {}
    /\ phase1_decided' = [phase1_decided EXCEPT ![k] = TRUE]
    /\ delivered_to' = [delivered_to EXCEPT ![k] = S]
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, nr_flipped,
                   output_set>>

(***************************************************************************)
(* Honest KindCommit emission.  Precondition: all phase-1 decisions made   *)
(* (= honest waited for T_commit).  Effect: kindcommit_emitted[i] = TRUE.  *)
(* The σ/NR contribution of i is implicit via SigmaPool / NRPool which     *)
(* derive from delivered_to ∩ Honest.                                      *)
(***************************************************************************)

HonestEmitKindCommit(i) ==
    /\ i \in Honest
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[i]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   nr_flipped, output_set>>

(***************************************************************************)
(* Byzantine KindCommit emission.  Same precondition as honest.  byz_commit *)
(* was chosen at Init (non-deterministic over all per-layer σ/NR/none      *)
(* choices); emission flips kindcommit_emitted[b] to TRUE.                 *)
(***************************************************************************)

ByzEmitKindCommit(b) ==
    /\ b \in Byzantine
    /\ AllPhase1Decided
    /\ ~ kindcommit_emitted[b]
    /\ kindcommit_emitted' = [kindcommit_emitted EXCEPT ![b] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   nr_flipped, output_set>>

(***************************************************************************)
(* Honest reconstruction.  Precondition: i has emitted, and either all     *)
(* contributing operators have emitted (or byz committed to silence at     *)
(* every layer).  This models "honest reconstructs at T_commit + Δ_2 with  *)
(* whatever KindCommits arrived".                                          *)
(*                                                                         *)
(* Outputs if some layer k has σ-quorum AND chain unlock, restricted to    *)
(* the partials from emitted operators (those who haven't emitted contrib- *)
(* ute nothing).                                                           *)
(***************************************************************************)

\* σ pool at layer k restricted to operators whose σ partial is available.
\* Honest σ at k requires honest emitted KindCommit (their σ is in their onion).
\* Byz σ at k available via either:
\*   (a) byz emitted their own KindCommit (σ in onion at k>0; plaintext at k=0); OR
\*   (b) byz is the layer-k leader AND their σ_L^V from Phase-1 was retained by
\*       some honest, who then includes it in their KindCommit's witness section
\*       per §Phase-2 wire format.  This makes σ_L^V available to all peers
\*       independent of whether byz emitted KindCommit themselves.
SigmaPoolEmitted(k) ==
    {i \in HonestSigmaAt(k) : kindcommit_emitted[i]} \cup
    {b \in ByzSigmaAt(k) :
        \/ kindcommit_emitted[b]
        \/ /\ leader_of[k] = b
           /\ \E i \in (Honest \cap delivered_to[k]) : kindcommit_emitted[i]}

\* NR pool at layer k restricted to operators who have emitted.
\* Honest NR-ers contribute via their KindCommit (gate: kindcommit_emitted[i]).
\* Honest σ-ers who NR-flipped contribute via the separate KindNRFlip emission
\* (gate: nr_flipped[i][k] — implicitly requires they had emitted KindCommit
\* first as a precondition of the flip).
NRPoolEmitted(k) ==
    {i \in (Honest \ delivered_to[k]) : kindcommit_emitted[i]} \cup
    {i \in (Honest \cap delivered_to[k]) : nr_flipped[i][k]} \cup
    {b \in ByzNRAt(k) : kindcommit_emitted[b]}

SigmaQuorumReachedEmitted(k) == Cardinality(SigmaPoolEmitted(k)) >= QV
NRQuorumReachedEmitted(k) == Cardinality(NRPoolEmitted(k)) >= QEnc

ChainUnlockedEmitted(k) ==
    \A j \in 0..(k - 1) : NRQuorumReachedEmitted(j)

LayerReconstructableEmitted(k) ==
    /\ SigmaQuorumReachedEmitted(k)
    /\ ChainUnlockedEmitted(k)

(***************************************************************************)
(* Honest NR-flip — Phase-2.5 deadlock recovery (NEW).                     *)
(*                                                                         *)
(* When honest σ-er observes ≥ f+1 NR partials at layer k AND σ pool there *)
(* is below qV (= deadlock at layer k), they emit an additional NR partial *)
(* — a KindNRFlip message with trigger evidence (the f+1 NR partials they  *)
(* observed).  This is a Phase-2.5 cross-signing under valid trigger; the  *)
(* protocol's amended EKM allows it without slashing.                      *)
(*                                                                         *)
(* Effect on pools:                                                        *)
(*   - σ pool unchanged (their σ partial still in KindCommit).             *)
(*   - NR pool grows by 1 (the NR-flip partial).                           *)
(*                                                                         *)
(* Safety basis: σ pool at the deadlock layer was bounded by 2f < qV       *)
(* before the flip (cryptographic — honest EKM blocks σ-after-NR for non-  *)
(* retainers; honest who retained are at most f-bounded by the deadlock    *)
(* condition).  NR-flip doesn't add to σ pool, so σ pool stays sub-qV      *)
(* after the flip too.  The deadlock layer doesn't reconstruct; the chain  *)
(* unlocks to a deeper layer where σ-quorum reaches naturally.             *)
(***************************************************************************)

\* === Operator-observable conditions only ================================
\*
\* This trigger uses only state observable to operators in the real protocol —
\* the count of distinct operators with σ partial / NR partial at layer k.
\* No oracle knowledge of who's honest vs byzantine.  Matches the trigger
\* in BareOBFT_Safety.tla (where TLC catches a P1 violation under this same
\* trigger model — see docs/OBFT-formal-verif.md §7.1 / §7.4).
NRFlipTriggered(k) ==
    /\ Cardinality(NRPoolEmitted(k)) >= F + 1          \* ≥ f+1 NR partials observed
    /\ Cardinality(SigmaPoolEmitted(k)) < QV           \* σ-quorum not yet reached

HonestNRFlip(i, k) ==
    /\ i \in Honest
    /\ kindcommit_emitted[i]              \* must have emitted KindCommit first
    /\ i \in delivered_to[k]              \* i is a σ-er at layer k (had V_k)
    /\ ~ nr_flipped[i][k]                 \* not yet NR-flipped at this layer
    /\ NRFlipTriggered(k)                 \* deadlock detected (strict trigger)
    /\ nr_flipped' = [nr_flipped EXCEPT ![i][k] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted, output_set>>

HonestReconstruct(i) ==
    /\ i \in Honest
    /\ kindcommit_emitted[i]
    /\ ~ output_set[i]
    /\ \E k \in Layers : LayerReconstructableEmitted(k)
    /\ output_set' = [output_set EXCEPT ![i] = TRUE]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted, nr_flipped>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E k \in Layers : HonestLeaderBroadcast(k)
    \/ \E k \in Layers, S \in SUBSET Operators : ByzLeaderBroadcast(k, S)
    \/ \E i \in Honest : HonestEmitKindCommit(i)
    \/ \E b \in Byzantine : ByzEmitKindCommit(b)
    \/ \E i \in Honest, k \in Layers : HonestNRFlip(i, k)
    \/ \E i \in Honest : HonestReconstruct(i)

(***************************************************************************)
(* Fairness                                                                *)
(*                                                                         *)
(* Honest actions: weak fairness — eventually fire if continuously enabled.*)
(* Byz actions: NO fairness — byz may stay silent (= choose not to act).  *)
(*                                                                         *)
(* Network: under within-budget, all honest broadcasts are full delivery — *)
(* no fairness needed since the action effect is atomic.  Byz selective    *)
(* delivery is captured by the non-deterministic S in ByzLeaderBroadcast,  *)
(* not by network fairness.                                                *)
(***************************************************************************)

Fairness ==
    /\ \A k \in Layers : WF_vars(HonestLeaderBroadcast(k))
    \* Byz leader must eventually make their broadcast decision (silent or
    \* otherwise) — at protocol level, T_broadcast_max_k forces this.  The
    \* existential lets TLC explore all S choices; WF only forces SOME S to
    \* eventually fire, which models "byz makes a choice in time".
    /\ \A k \in Layers :
        WF_vars(\E S \in SUBSET Operators : ByzLeaderBroadcast(k, S))
    /\ \A i \in Honest : WF_vars(HonestEmitKindCommit(i))
    /\ \A i \in Honest, k \in Layers : WF_vars(HonestNRFlip(i, k))
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
    /\ nr_flipped \in [Honest -> [Layers -> BOOLEAN]]
    /\ output_set \in [Operators -> BOOLEAN]

(***************************************************************************)
(* Symmetry — TLC explores one canonical state per equivalence class       *)
(* under permutations of Honest operators.  Byzantine operators are NOT    *)
(* permuted (they're distinguished by Byzantine designation).              *)
(*                                                                         *)
(* Values are NOT permuted because of the layer ↔ value bijection (V_k for *)
(* layer k is distinguished by its layer index).                           *)
(***************************************************************************)

Symmetry == Permutations(Honest)

================================================================================

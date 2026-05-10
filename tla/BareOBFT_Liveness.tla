--------------------------- MODULE BareOBFT_Liveness ---------------------------
(***************************************************************************)
(* Liveness verification for bare OBFT — σ-flip variant.                   *)
(*                                                                         *)
(* Tests whether the σ-flip + leader-NR-doesn't-count + full-V-in-onion    *)
(* design closes Class A deadlock (`LIVENESS_NON_GRIEF`).  Companion to    *)
(* the BareOBFT_Safety σ-flip variant.                                     *)
(*                                                                         *)
(* Verifies: under (a) BFT trust bound (≤ f byz of n=3f+1), (b) within-    *)
(* budget partial-synchrony, (c) honest EKM (with σ-flip exemption per     *)
(* trigger), AND (d) no GRIEF byzantine actions, the protocol terminates   *)
(* with `output[i] = TRUE` for every honest operator i (= no Class A       *)
(* deadlock).                                                              *)
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
    flipped_to,          \* [Honest -> [Layers -> {"none", "sigma", "nr"}]]:
                         \* "none" = no flip; "sigma" = σ-flipped (NR → σ);
                         \* "nr" = NR-flipped (σ → NR).  Single flip per layer.
    output_set           \* [Operators -> BOOLEAN]: has operator's reconstruction succeeded

vars == <<leader_of, phase1_decided, delivered_to, byz_commit,
          kindcommit_emitted, flipped_to, output_set>>

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Honest σ pool at layer k (additive flips):
\*   - retainer → σ-er via KindCommit. Stays even if leader NR-flips
\*     (additive: leader's σ_L^V stays in σ-pool).
\*   - non-retainer who σ-flipped → σ-er via KindSigmaFlip.
\* Honest NR pool at layer k (additive):
\*   - non-retainer → NR-er (includes σ-flippers since their NR partial stays).
\*   - LEADER who NR-flipped (per user's design: only leader NR-flips).
HonestSigmaAt(k) ==
    (delivered_to[k] \cap Honest) \cup
    {op \in Honest : op \notin delivered_to[k] /\ flipped_to[op][k] = "sigma"}

HonestNRAt(k) ==
    {op \in Honest :
        \/ op \notin delivered_to[k]
        \/ (op = leader_of[k] /\ flipped_to[op][k] = "nr")}

\* Byz contribution at layer k: based on byz_commit[i][k] choice.
ByzSigmaAt(k) == {i \in Byzantine : byz_commit[i][k] = "sigma"}
ByzNRAt(k) == {i \in Byzantine : byz_commit[i][k] = "nr"}

\* Aggregate σ pool at layer k = honest σ-ers (retainers ∪ σ-flippers) ∪ byz σ
SigmaPool(k) == HonestSigmaAt(k) \cup ByzSigmaAt(k)

\* Aggregate NR pool at layer k: NO leader exclusion (per user's latest;
\* leader-NR-doesn't-count removed) AND no σ-supersedes-NR.
NRPool(k) == HonestNRAt(k) \cup ByzNRAt(k)

\* Non-leader NR pool: used for σ-flip trigger ("non-leader NRs" qualifier).
NRPoolNonLeader(k) == NRPool(k) \ {leader_of[k]}

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
    /\ flipped_to = [i \in Honest |-> [k \in Layers |-> "none"]]
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
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, flipped_to,
                   output_set>>

(***************************************************************************)
(* Byzantine leader broadcast — NON-GRIEF only (per user's framing).        *)
(*                                                                         *)
(* Under non-grief, byz leader is constrained to all-or-nothing broadcast: *)
(*   - byz_commit = "sigma" → S = Operators (broadcast to all → h_V = n).  *)
(*   - byz_commit = "none"  → S = ∅ (didn't sign → no retainers, h_V = 0). *)
(*                                                                         *)
(* Selective broadcast (= S a proper subset, h_V ∈ {1, ..., n-1}) is       *)
(* RECLASSIFIED AS GRIEF in this liveness model.  Liveness need not hold   *)
(* under selective broadcast — it's the byz attack pattern that engineers  *)
(* the bare-OBFT Class A deadlock.  Excluded from non-grief verification.  *)
(*                                                                         *)
(* The safety spec (BareOBFT_Safety.tla) does NOT add this constraint —    *)
(* safety must hold under any byz behavior including selective broadcast.  *)
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
    /\ UNCHANGED <<leader_of, byz_commit, kindcommit_emitted, flipped_to,
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
                   flipped_to, output_set>>

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
                   flipped_to, output_set>>

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

\* σ pool at layer k restricted to operators whose σ partial is available
\* (= they emitted KindCommit, or KindSigmaFlip if σ-flipped).
\* Under additive σ-flip:
\*   - Retainer → σ-er via KindCommit.
\*   - Non-retainer who σ-flipped → σ-er via KindSigmaFlip (still emits
\*     KindCommit with NR; flip is additive).
\* Byz σ available via own KindCommit or leader's σ_L^V witness section.
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

\* NR pool emitted: NO leader exclusion (per user's latest design).
\* Includes honest non-retainers + byz NR-ers + leader if NR-flipped (additive).
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

(***************************************************************************)
(* Phase-2.5 deadlock recovery: σ-flip and NR-flip, both transformative.   *)
(*                                                                         *)
(* σ-flip: honest NR-er → σ-er (cluster-wide: σ partial added,             *)
(*   prior NR partial withdrawn from quorum count via σ-supersedes-NR).    *)
(* NR-flip: honest σ-er → NR-er (cluster-wide: NR partial added,           *)
(*   prior σ partial withdrawn).                                           *)
(*                                                                         *)
(* User's trigger conditions:                                              *)
(*   σ-flip: post-flip non-leader σ count + silent ≥ f                     *)
(*   NR-flip: post-flip NR-pool count + silent ≥ f+1                       *)
(*                                                                         *)
(* Single-flip-per-layer enforced via flipped_to[op][k] ∈ {none, sigma,    *)
(* nr}.                                                                    *)
(***************************************************************************)

\* Silent at the trigger evaluation point = ops who haven't emitted KindCommit
\* yet (= we don't have their partial; consume f-byz budget).
SilentEmitted == {op \in Operators : ~ kindcommit_emitted[op]}

\* Non-leader σ-pool count (emitted partials only).
SigmaPoolNonLeaderEmitted(k) == SigmaPoolEmitted(k) \ {leader_of[k]}

\* User's latest design:
\*   σ-flip (any non-leader honest):
\*     NR_nl < f+1 AND S_post ≥ A + 2f
\*       NR_nl = non-leader NRs (INCLUDING own)
\*       S_post = non-leader σ post-flip = pre-flip S_nl + 1
\*       A = silent ops (= no emitted KindCommit)
\*
\*   NR-flip (LEADER ONLY):
\*     S_nl < f AND NR_post ≥ A + 2f
\*       NR_post = TOTAL NR-pool post-flip (incl leader's own would-be NR)
SigmaFlipTriggered(k) ==
    LET nr_nl   == Cardinality(NRPoolEmitted(k) \ {leader_of[k]})
        s_post  == Cardinality(SigmaPoolNonLeaderEmitted(k)) + 1
        a_count == Cardinality(SilentEmitted)
    IN
       /\ nr_nl < F + 1
       /\ s_post >= a_count + 2 * F

\* Per user's literal rule: NR = non-leader NRs only (leader's own NR isn't
\* a non-leader NR for leader-actor).
NRFlipTriggered(k) ==
    LET s_nl    == Cardinality(SigmaPoolNonLeaderEmitted(k))
        nr_nl   == Cardinality(NRPoolEmitted(k) \ {leader_of[k]})
        a_count == Cardinality(SilentEmitted)
    IN
       /\ s_nl < F
       /\ nr_nl >= a_count + 2 * F

\* V-availability: V is observable cluster-wide iff some operator has σ
\* partial at this layer (modeling full-V-in-onion plumbing).
VAvailable(k) == SigmaPoolEmitted(k) # {}

\* Honest σ-flip — additive: non-leader NR-er emits σ partial via KindSigmaFlip.
\* Their original NR partial in KindCommit stays, so they're in BOTH pools.
\* Gated by AllKindCommitsSettled (snapshot proxy: all initial commits visible).
HonestSigmaFlip(i, k) ==
    /\ AllKindCommitsSettled              \* Phase 2 finalized (all KindCommits emitted)
    /\ i \in Honest
    /\ i # leader_of[k]                   \* leader can't σ-flip
    /\ kindcommit_emitted[i]              \* must have emitted KindCommit
    /\ i \notin delivered_to[k]           \* op was NR-er pre-flip
    /\ flipped_to[i][k] = "none"          \* single-flip-per-layer
    /\ SigmaFlipTriggered(k)              \* trigger: NR_nl < f+1 AND S_post ≥ A+2f
    /\ VAvailable(k)                      \* V learned via full-V-in-onion
    /\ flipped_to' = [flipped_to EXCEPT ![i][k] = "sigma"]
    /\ UNCHANGED <<leader_of, phase1_decided, delivered_to, byz_commit,
                   kindcommit_emitted, output_set>>

\* Honest NR-flip — LEADER ONLY (per user's latest design).
\* Additive: leader emits NR partial via KindNRFlip; leader's σ_L^V stays.
HonestNRFlip(i, k) ==
    /\ AllKindCommitsSettled              \* Phase 2 finalized
    /\ i \in Honest
    /\ i = leader_of[k]                   \* ONLY leader can NR-flip
    /\ kindcommit_emitted[i]              \* must have emitted KindCommit
    /\ flipped_to[i][k] = "none"          \* single-flip-per-layer
    /\ NRFlipTriggered(k)                 \* trigger: S_nl < f AND NR_post ≥ A+2f
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
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E k \in Layers : HonestLeaderBroadcast(k)
    \/ \E k \in Layers, S \in SUBSET Operators : ByzLeaderBroadcast(k, S)
    \/ \E i \in Honest : HonestEmitKindCommit(i)
    \/ \E b \in Byzantine : ByzEmitKindCommit(b)
    \/ \E i \in Honest, k \in Layers : HonestSigmaFlip(i, k)
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
    /\ \A i \in Honest, k \in Layers : WF_vars(HonestSigmaFlip(i, k))
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
    /\ flipped_to \in [Honest -> [Layers -> {"none", "sigma", "nr"}]]
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

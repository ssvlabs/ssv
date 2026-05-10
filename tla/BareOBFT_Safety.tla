---------------------------- MODULE BareOBFT_Safety ----------------------------
(***************************************************************************)
(* Safety verification for bare OBFT with Phase-2.5 σ-flip / NR-flip,      *)
(* per-operator-view model.                                                *)
(*                                                                         *)
(* User's design:                                                          *)
(*   - σ-flip (any non-leader honest who NR'd):                            *)
(*       trigger: snap_NR_nl < f+1 AND snap_S_post ≥ A + 2f                *)
(*       effect:  add σ partial on V (additive, prior NR stays).           *)
(*                                                                         *)
(*   - NR-flip (LEADER ONLY, when honest):                                 *)
(*       trigger: snap_S_nl < f AND snap_NR_nl ≥ A + 2f                    *)
(*       effect:  add NR partial (additive, prior σ_L^V stays).            *)
(*       restriction: leader-only prevents non-leader-NR-flip safety       *)
(*       attack surface (CE-1..12 in earlier iterations).                  *)
(*                                                                         *)
(* Where (all evaluated from the actor's own snapshot):                    *)
(*   NR_nl  = non-leader NRs (incl own for σ-flipper as actor)             *)
(*   S_nl   = non-leader σ partials                                        *)
(*   S_post = non-leader σ post-flip = pre-flip S_nl + 1                   *)
(*   A      = silent ops (no partial in actor's snapshot)                  *)
(*                                                                         *)
(* === Per-operator-view model =========================================== *)
(*                                                                         *)
(* This spec models per-operator local views of σ/NR partials, not a       *)
(* single global pool.  Each operator has their own sigma_view[op] and     *)
(* nr_view[op] tracking what THEY have observed.                           *)
(*                                                                         *)
(* Honest emissions (within-budget partial-synchrony, Assumption 2):       *)
(* delivered to ALL operators' views.  Honest views thus agree on honest  *)
(* contributions cluster-wide.                                             *)
(*                                                                         *)
(* Byzantine emissions: byz chooses any subset S ⊆ Operators per emission *)
(* (selective delivery, full grief).  byz's own view always contains its   *)
(* own signed messages (byz knows what they signed).  Honest views may    *)
(* disagree on byz contributions.                                          *)
(*                                                                         *)
(* Snapshot semantics: each honest operator independently snapshots their  *)
(* own view at FinalizePhase2.  σ-flip / NR-flip trigger evaluation uses   *)
(* the actor's own snapshot.  Different operators may evaluate triggers    *)
(* differently (per-operator snap divergence on byz contributions).        *)
(*                                                                         *)
(* Cluster-wide pool semantics (for safety invariants): the cluster pool   *)
(* on (k, v) = { op : op signed σ on (k, v) } = { op : <<op, k, v>> ∈      *)
(* sigma_view[op] } (signer's own view always contains their signature).   *)
(* This represents the worst-case offline-aggregator's set: byz can        *)
(* aggregate any signed partial regardless of which honest saw it.         *)
(*                                                                         *)
(* === Safety basis ====================================================== *)
(*                                                                         *)
(* The σ-flip and NR-flip triggers are mutex per (k) under within-budget   *)
(* honest propagation (Assumption 2), via algebraic-cardinality argument:  *)
(*   - σ-flip from honest non-leader X requires snap_NR_nl_X ≤ f.  X sees  *)
(*     all honest non-leader NRs (within-budget) ⇒ nr_h ≤ f ⇒ s_h ≥ f.    *)
(*   - NR-flip from honest leader requires snap_S_nl_leader < f.  Leader  *)
(*     sees all honest non-leader σs (within-budget) ⇒ s_h < f.            *)
(*   - At n=3f+1, honest non-leaders = 2f, so s_h + nr_h = 2f.  Both       *)
(*     constraints (s_h ≥ f AND s_h < f) cannot hold.  Mutex.              *)
(*                                                                         *)
(* Under σ-flip path: NR-pool ≤ snap_NR + byz_NR_total ≤ (nr_h + 1) + f ≤  *)
(* (f + 1) + f = 2f+1 = qEnc, but the +1 from leader's NR only applies if  *)
(* leader byz-NR'd in snap.  At f=1, NR-pool ≤ 2 < 3 = qEnc.               *)
(*                                                                         *)
(* Under NR-flip path: σ-pool[V_L] ≤ s_h + leader_σ_L^V + byz_σ_V ≤ (f-1) *)
(* + 1 + f = 2f < 2f+1 = qV.                                                *)
(*                                                                         *)
(* === Verifies the three Pigeonhole invariants from docs/OBFT.md ========*)
(*   - Pigeonhole 1: σ-quorum and NR-quorum don't both reach at any layer. *)
(*   - Pigeonhole 2: at most one V σ-quorums per layer.                    *)
(*   - Pigeonhole 3: at most one V reconstructs cluster-wide across K      *)
(*     layers under chained encryption.                                    *)
(*                                                                         *)
(* Honest behavior:                                                        *)
(*   - HonestSigma / HonestNR fire pre-FinalizePhase2 (= per-op flag).     *)
(*   - Honest leader doesn't NR pre-snap (leader's job is σ_L^V).          *)
(*   - Honest non-leader can σ or NR freely (= retention non-determinism). *)
(*                                                                         *)
(* Byzantine behavior — unrestricted (FULL GRIEF):                         *)
(*   - ByzSigma, ByzNR fire any time (pre or post finalize) with any       *)
(*     delivery subset S ⊆ Operators.                                      *)
(*   - Byz can equivocate, cross-sign, late-publish, or selectively-      *)
(*     deliver.                                                            *)
(*                                                                         *)
(* leader_of is non-deterministically chosen as any injective function     *)
(* [Layers -> Operators], so TLC explores all leader-assignment patterns   *)
(* including byz-at-any-position.                                          *)
(*                                                                         *)
(* See `docs/OBFT-formal-verif.md` §4 for the verification approach and    *)
(* `tla/README.md` for run instructions.                                   *)
(***************************************************************************)

EXTENDS Naturals, FiniteSets, TLC

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
    /\ K \in 1..N                       \* 1 ≤ K ≤ n.  K=1 is the minimal
                                        \* per-layer safety case (P1 mutex);
                                        \* P3's inductive argument doesn't
                                        \* require K ≥ 2 to verify since the
                                        \* induction is algebraic on top of P1.
    /\ Cardinality(Values) >= K         \* enough distinct values for layer leaders

(***************************************************************************)
(* State variables                                                         *)
(*                                                                         *)
(* sigma_view: [Operators -> SUBSET (Operators × Layers × Values)].        *)
(* Per-operator local view of σ partials observed.  An operator's own σ    *)
(* signatures are always in their own view (signer's invariant).           *)
(*                                                                         *)
(* nr_view: [Operators -> SUBSET (Operators × Layers)].  Per-operator      *)
(* local view of NR partials observed.  Same signer's invariant.           *)
(*                                                                         *)
(* leader_of: function [Layers -> Operators], distinct leader per layer.   *)
(* Constant after Init; TLC explores all valid assignments.                *)
(*                                                                         *)
(* has_flipped: per-operator-per-layer flag tracking single-flip-per-layer.*)
(*                                                                         *)
(* snap_sigma_view, snap_nr_view: per-operator snapshots taken at          *)
(* FinalizePhase2 (simultaneously across all honest).                      *)
(*                                                                         *)
(* phase2_finalized: per-operator flag indicating snapshot taken.  Set     *)
(* TRUE for all honest simultaneously by FinalizePhase2.                   *)
(***************************************************************************)

VARIABLES
    sigma_view,            \* [Operators -> SUBSET (Operators \X Layers \X Values)]
    nr_view,               \* [Operators -> SUBSET (Operators \X Layers)]
    leader_of,
    has_flipped,
    snap_sigma_view,       \* [Operators -> SUBSET (Operators \X Layers \X Values)]
    snap_nr_view,          \* [Operators -> SUBSET (Operators \X Layers)]
    phase2_finalized       \* BOOLEAN — global, since FinalizePhase2 is a
                           \* single simultaneous-cluster transition

vars == <<sigma_view, nr_view, leader_of, has_flipped,
          snap_sigma_view, snap_nr_view, phase2_finalized>>

\* All injective leader assignments [Layers -> Operators].
InitLeaderAssignments ==
    {f \in [Layers -> Operators] :
        \A k1, k2 \in Layers : k1 # k2 => f[k1] # f[k2]}

(***************************************************************************)
(* Helper definitions                                                      *)
(***************************************************************************)

\* Has operator op signed σ at layer k (any V)?  Signer's own view check.
HasSignedSigma(op, k) == \E v \in Values : <<op, k, v>> \in sigma_view[op]

\* Has operator op signed NR at layer k?  Signer's own view check.
HasSignedNR(op, k) == <<op, k>> \in nr_view[op]

\* === Cluster-wide pool (= signed-message-set) =============================
\* op is in cluster σ-pool on (k, v) iff op signed σ on (k, v), checked via
\* op's own view (signer's invariant: signer's view always contains their
\* own signatures).  Equivalent to "exists any view that contains the tuple"
\* under the invariant, but using own view is cleaner.
ClusterSigmaPool(k, v) ==
    {op \in Operators : <<op, k, v>> \in sigma_view[op]}

ClusterNRPool(k) ==
    {op \in Operators : <<op, k>> \in nr_view[op]}

\* σ-quorum on V at layer k iff cluster pool ≥ qV
ClusterSigmaQuorumReached(k, v) ==
    Cardinality(ClusterSigmaPool(k, v)) >= QV

\* NR-quorum at layer k iff cluster NR-pool ≥ qEnc
ClusterNRQuorumReached(k) ==
    Cardinality(ClusterNRPool(k)) >= QEnc

\* Chained-encryption unlock: layer k accessible iff NR-quorum at all
\* layers 0..k-1 (k=0 trivially unlocked).
ChainUnlocked(k) == \A j \in 0..(k - 1) : ClusterNRQuorumReached(j)

\* V at layer k is reconstructable cluster-wide iff σ-quorum AND chain unlocked.
Reconstructable(k, v) ==
    /\ ClusterSigmaQuorumReached(k, v)
    /\ ChainUnlocked(k)

(***************************************************************************)
(* Snapshot helpers (per-viewer)                                           *)
(*                                                                         *)
(* All evaluated from the perspective of `viewer` (the operator computing  *)
(* the trigger condition).  Each honest operator independently checks      *)
(* their own snapshot to decide whether to flip.                           *)
(***************************************************************************)

SnapHasSigma(viewer, op, k) ==
    \E v \in Values : <<op, k, v>> \in snap_sigma_view[viewer]

SnapHasNR(viewer, op, k) ==
    <<op, k>> \in snap_nr_view[viewer]

SnapHasEmitted(viewer, op, k) ==
    SnapHasSigma(viewer, op, k) \/ SnapHasNR(viewer, op, k)

\* Silent-from-viewer's-view: ops not yet observed to have committed.
SnapSilentAt(viewer, k) ==
    {op \in Operators : ~ SnapHasEmitted(viewer, op, k)}

\* NR-pool from viewer's snap.
SnapNRPool(viewer, k) ==
    {op \in Operators : SnapHasNR(viewer, op, k)}

\* Non-leader NR pool from viewer's snap.
SnapNRPoolNonLeader(viewer, k) ==
    SnapNRPool(viewer, k) \ {leader_of[k]}

\* Non-leader σ-pool from viewer's snap (σ on any V counted).
SnapSigmaPoolNonLeader(viewer, k) ==
    {op \in Operators : SnapHasSigma(viewer, op, k) /\ op # leader_of[k]}

(***************************************************************************)
(* Flip triggers                                                           *)
(*                                                                         *)
(* Both triggers evaluate against viewer's own snapshot.  Different        *)
(* viewers may compute different snap_S_nl etc. due to byz selective       *)
(* delivery, but honest views agree on honest contributions (within-       *)
(* budget propagation, Assumption 2).                                      *)
(***************************************************************************)

\* σ-flip trigger: NR_nl < f+1 AND S_post ≥ A + 2f, all from viewer's snap.
SigmaFlipTriggered(viewer, k) ==
    LET nr_count == Cardinality(SnapNRPoolNonLeader(viewer, k))
        s_post   == Cardinality(SnapSigmaPoolNonLeader(viewer, k)) + 1
        a_count  == Cardinality(SnapSilentAt(viewer, k))
    IN
       /\ nr_count < F + 1
       /\ s_post >= a_count + 2 * F

\* NR-flip trigger: S_nl < f AND NR_nl ≥ A + 2f.
NRFlipTriggered(viewer, k) ==
    LET s_nl    == Cardinality(SnapSigmaPoolNonLeader(viewer, k))
        nr_nl   == Cardinality(SnapNRPoolNonLeader(viewer, k))
        a_count == Cardinality(SnapSilentAt(viewer, k))
    IN
       /\ s_nl < F
       /\ nr_nl >= a_count + 2 * F

\* V-availability from viewer's snap: viewer needs to have observed V to
\* know what to sign on for σ-flip.  Under within-budget + leader-bundle
\* re-flood in KindCommit (per OBFT.md §Phase 2 Wire format), this holds
\* for V_L of every retained layer.  Byz V's might or might not be visible.
VAvailable(viewer, k, v) ==
    \E op \in Operators : <<op, k, v>> \in snap_sigma_view[viewer]

(***************************************************************************)
(* Initial state                                                           *)
(***************************************************************************)

Init ==
    /\ sigma_view = [op \in Operators |-> {}]
    /\ nr_view = [op \in Operators |-> {}]
    /\ leader_of \in InitLeaderAssignments
    /\ has_flipped = [op \in Operators |-> [k \in Layers |-> FALSE]]
    /\ snap_sigma_view = [op \in Operators |-> {}]
    /\ snap_nr_view = [op \in Operators |-> {}]
    /\ phase2_finalized = FALSE

(***************************************************************************)
(* Honest actions                                                          *)
(*                                                                         *)
(* Honest emissions broadcast to ALL operators' views (within-budget       *)
(* delivery per Assumption 2).  This is what makes honest contributions    *)
(* cluster-consistent across honest snaps at FinalizePhase2.               *)
(***************************************************************************)

\* Honest σ at layer k on V.  XOR per (op, k); single-σ-V per (op, k);
\* pre-finalize on op only.
HonestSigma(op, k, v) ==
    /\ ~ phase2_finalized
    /\ op \in Honest
    /\ ~ HasSignedSigma(op, k)              \* not yet σ at k (single-σ-V)
    /\ ~ HasSignedNR(op, k)                 \* not yet NR at k (XOR)
    /\ sigma_view' = [op2 \in Operators |->
                          sigma_view[op2] \cup {<<op, k, v>>}]
    /\ UNCHANGED <<nr_view, leader_of, has_flipped,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

\* Honest NR at layer k.  Honest leader doesn't NR pre-snap (leader's job
\* is σ_L^V; NR-flip is the leader's only NR path post-snap).
HonestNR(op, k) ==
    /\ ~ phase2_finalized
    /\ op \in Honest
    /\ op # leader_of[k]                    \* honest leader doesn't NR
    /\ ~ HasSignedSigma(op, k)
    /\ ~ HasSignedNR(op, k)
    /\ nr_view' = [op2 \in Operators |->
                       nr_view[op2] \cup {<<op, k>>}]
    /\ UNCHANGED <<sigma_view, leader_of, has_flipped,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

(***************************************************************************)
(* FinalizePhase2 — all honest snapshot simultaneously                     *)
(*                                                                         *)
(* All honest operators take their snapshots at the same global moment    *)
(* (= cluster-wide T_commit + Δ_2).  Each captures THEIR OWN view at that  *)
(* moment.  Under within-budget propagation, all honest views agree on    *)
(* honest contributions.  Views may disagree on byz contributions          *)
(* (selectively delivered).                                                *)
(*                                                                         *)
(* Single-shot: phase2_finalized is a global boolean.  Once FinalizePhase2 *)
(* fires, no honest can emit pre-snap (HonestSigma / HonestNR have         *)
(* `~ phase2_finalized`); honest can flip post-snap (HonestSigmaFlip /     *)
(* HonestNRFlip have `phase2_finalized`).                                  *)
(*                                                                         *)
(* Why global (not per-operator).  Cluster-wide T_commit + Δ_2 is a single *)
(* design moment; all honest snapshot together.  This is what implements   *)
(* the no-flip-cascade design intent: an honest flip emitted post-finalize *)
(* doesn't land in any other honest's snap (all snaps were taken before).  *)
(* Per-operator finalize would let op_A finalize-and-flip while op_B is    *)
(* still pre-finalize, polluting op_B's snap with A's flip — exactly the   *)
(* cascade the protocol forbids.                                           *)
(*                                                                         *)
(* Byz operators don't snapshot — they have no flips to fire.  Byz         *)
(* emissions can fire any time, before or after FinalizePhase2.            *)
(***************************************************************************)

FinalizePhase2 ==
    /\ ~ phase2_finalized
    /\ snap_sigma_view' = [op \in Operators |->
                              IF op \in Honest THEN sigma_view[op]
                              ELSE snap_sigma_view[op]]
    /\ snap_nr_view' = [op \in Operators |->
                           IF op \in Honest THEN nr_view[op]
                           ELSE snap_nr_view[op]]
    /\ phase2_finalized' = TRUE
    /\ UNCHANGED <<sigma_view, nr_view, leader_of, has_flipped>>

(***************************************************************************)
(* Byzantine actions — split pre/post snapshot                             *)
(*                                                                         *)
(* PRE-SNAP (`~ phase2_finalized`): byz signs any σ/NR partial and selects *)
(* any subset S ⊆ Operators for delivery.  This is the load-bearing case  *)
(* for safety analysis — selective delivery to honest snaps (visible to    *)
(* some honest, withheld from others) is what byz uses to attempt to       *)
(* engineer per-operator snap divergence.                                  *)
(*                                                                         *)
(* POST-SNAP (`phase2_finalized`): byz signs but no S choice — modeled as  *)
(* "byz keeps own copy only".  Rationale: post-snap emissions cannot       *)
(* affect any snap (snaps are frozen) or VAvailable (uses snap).  They     *)
(* only contribute to the cluster pool (= worst-case offline aggregator's  *)
(* set).  Whether byz delivers post-snap to all-or-none-of-honest does     *)
(* not affect any safety-relevant decision; the cluster-pool count is the *)
(* same.  This eliminates the post-snap byz S-branching factor (a major   *)
(* state-space reducer).                                                   *)
(*                                                                         *)
(* In both cases, byz's own view always contains their own signatures      *)
(* (op ∈ S enforced for pre-snap; trivially true for post-snap S = {byz}). *)
(***************************************************************************)

\* Pre-snap byz σ — selective delivery via S.
ByzSigmaPreSnap(op, k, v, S) ==
    /\ ~ phase2_finalized
    /\ op \in Byzantine
    /\ S \subseteq Operators
    /\ op \in S                              \* byz always knows their own sig
    /\ <<op, k, v>> \notin sigma_view[op]    \* not already signed (idempotent)
    /\ sigma_view' = [op2 \in Operators |->
                          IF op2 \in S THEN sigma_view[op2] \cup {<<op, k, v>>}
                          ELSE sigma_view[op2]]
    /\ UNCHANGED <<nr_view, leader_of, has_flipped,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

\* Post-snap byz σ — own copy only (S = {op}).  Cluster-pool effect
\* identical to broadcast; eliminates the post-snap S-branching.
ByzSigmaPostSnap(op, k, v) ==
    /\ phase2_finalized
    /\ op \in Byzantine
    /\ <<op, k, v>> \notin sigma_view[op]
    /\ sigma_view' = [sigma_view EXCEPT ![op] = sigma_view[op] \cup {<<op, k, v>>}]
    /\ UNCHANGED <<nr_view, leader_of, has_flipped,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

\* Pre-snap byz NR — selective delivery via S.
ByzNRPreSnap(op, k, S) ==
    /\ ~ phase2_finalized
    /\ op \in Byzantine
    /\ S \subseteq Operators
    /\ op \in S
    /\ <<op, k>> \notin nr_view[op]
    /\ nr_view' = [op2 \in Operators |->
                       IF op2 \in S THEN nr_view[op2] \cup {<<op, k>>}
                       ELSE nr_view[op2]]
    /\ UNCHANGED <<sigma_view, leader_of, has_flipped,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

\* Post-snap byz NR — own copy only.
ByzNRPostSnap(op, k) ==
    /\ phase2_finalized
    /\ op \in Byzantine
    /\ <<op, k>> \notin nr_view[op]
    /\ nr_view' = [nr_view EXCEPT ![op] = nr_view[op] \cup {<<op, k>>}]
    /\ UNCHANGED <<sigma_view, leader_of, has_flipped,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

(***************************************************************************)
(* Phase-2.5 deadlock recovery: σ-flip (non-leader) and NR-flip (leader).  *)
(* Both ADDITIVE — signed messages cannot be withdrawn cryptographically;  *)
(* the prior σ or NR partial stays in the cluster signed-message set.      *)
(*                                                                         *)
(* σ-flip: an honest non-leader NR-er at layer k emits a KindSigmaFlip     *)
(*   adding a σ partial.  Their original NR partial stays.  Post-flip, op  *)
(*   has BOTH σ and NR partials (cross-signed via amended-EKM trigger).    *)
(*                                                                         *)
(* NR-flip: ONLY the LEADER (if honest) at layer k emits a KindNRFlip      *)
(*   adding an NR partial.  Their original σ_L^V partial stays.  Post-     *)
(*   flip, leader has BOTH σ and NR partials.  Restricted to leader to    *)
(*   prevent a non-leader-NR-flip safety attack surface.                   *)
(*                                                                         *)
(* Triggers evaluated against ACTOR'S OWN snapshot — different actors may  *)
(* see different snaps under byz selective delivery.  The algebraic mutex  *)
(* (s_h ≥ f from σ-flip ⇒ s_h < f from NR-flip impossible at n=3f+1)      *)
(* holds because honest views agree on honest contributions under within-  *)
(* budget propagation.                                                     *)
(***************************************************************************)

\* Honest σ-flip — additive, broadcast to all (within-budget).
HonestSigmaFlip(op, k, v) ==
    /\ phase2_finalized                 \* op has snapped
    /\ op \in Honest
    /\ op # leader_of[k]                    \* leader can't σ-flip
    /\ SnapHasNR(op, op, k)                 \* op was NR-er in own snap
    /\ ~ SnapHasSigma(op, op, k)            \* op had no σ in own snap
    /\ ~ has_flipped[op][k]                 \* single-flip-per-layer
    /\ SigmaFlipTriggered(op, k)            \* trigger via op's own snap
    /\ VAvailable(op, k, v)                 \* V was visible in op's snap
    /\ sigma_view' = [op2 \in Operators |->
                          sigma_view[op2] \cup {<<op, k, v>>}]
    /\ has_flipped' = [has_flipped EXCEPT ![op][k] = TRUE]
    /\ UNCHANGED <<nr_view, leader_of,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

\* Honest NR-flip — LEADER ONLY, additive, broadcast to all.
HonestNRFlip(op, k) ==
    /\ phase2_finalized
    /\ op \in Honest
    /\ op = leader_of[k]                    \* ONLY leader can NR-flip
    /\ SnapHasSigma(op, op, k)              \* leader was σ-er (signed σ_L^V)
    /\ ~ SnapHasNR(op, op, k)               \* leader hadn't NR'd
    /\ ~ has_flipped[op][k]
    /\ NRFlipTriggered(op, k)
    /\ nr_view' = [op2 \in Operators |->
                       nr_view[op2] \cup {<<op, k>>}]
    /\ has_flipped' = [has_flipped EXCEPT ![op][k] = TRUE]
    /\ UNCHANGED <<sigma_view, leader_of,
                   snap_sigma_view, snap_nr_view, phase2_finalized>>

(***************************************************************************)
(* Next-state relation                                                     *)
(***************************************************************************)

Next ==
    \/ \E op \in Honest, k \in Layers, v \in Values: HonestSigma(op, k, v)
    \/ \E op \in Honest, k \in Layers: HonestNR(op, k)
    \/ FinalizePhase2
    \/ \E op \in Honest, k \in Layers, v \in Values: HonestSigmaFlip(op, k, v)
    \/ \E op \in Honest, k \in Layers: HonestNRFlip(op, k)
    \/ \E op \in Byzantine, k \in Layers, v \in Values, S \in SUBSET Operators:
            ByzSigmaPreSnap(op, k, v, S)
    \/ \E op \in Byzantine, k \in Layers, S \in SUBSET Operators:
            ByzNRPreSnap(op, k, S)
    \/ \E op \in Byzantine, k \in Layers, v \in Values:
            ByzSigmaPostSnap(op, k, v)
    \/ \E op \in Byzantine, k \in Layers:
            ByzNRPostSnap(op, k)

Spec == Init /\ [][Next]_vars

(***************************************************************************)
(* Type invariant — sanity check on state structure                        *)
(***************************************************************************)

TypeOK ==
    /\ sigma_view \in [Operators -> SUBSET (Operators \X Layers \X Values)]
    /\ nr_view \in [Operators -> SUBSET (Operators \X Layers)]
    /\ leader_of \in [Layers -> Operators]
    /\ \A k1, k2 \in Layers : k1 # k2 => leader_of[k1] # leader_of[k2]
    /\ has_flipped \in [Operators -> [Layers -> BOOLEAN]]
    /\ snap_sigma_view \in [Operators -> SUBSET (Operators \X Layers \X Values)]
    /\ snap_nr_view \in [Operators -> SUBSET (Operators \X Layers)]
    /\ phase2_finalized \in BOOLEAN

(***************************************************************************)
(* SAFETY invariants — Pigeonholes 1, 2, 3                                 *)
(***************************************************************************)

\* Pigeonhole 1: σ-quorum on any V at L_k AND NR-quorum at L_k cannot
\* both reach.
\*
\* Algebraic basis (per docs/OBFT.md §Safety / Pigeonhole 1):
\*   - Case A (no flip fired at k): bare-OBFT cross-phase exclusivity gives
\*     h_σ + h_NR ≤ 2f+1, byz cap ≤ 2f, joint max ≤ 4f+1 < 2·qV.
\*   - Case B (σ-flip fired by some honest non-leader X): σ-flip trigger
\*     ⇒ snap_NR_nl_X ≤ f.  Within-budget ⇒ X's view of honest NRs = honest
\*     NR count nr_h.  So nr_h ≤ f.  Honest non-leaders = 2f at n=3f+1, so
\*     s_h ≥ f.  NR-flip trigger needs snap_S_nl_leader < f, but leader sees
\*     all honest non-leader σs (within-budget) ⇒ snap_S_nl_leader ≥ s_h ≥ f.
\*     Mutex.  NR-pool ≤ honest_NR_in_snap + byz_NR_total ≤ nr_h + f ≤ 2f
\*     < qEnc.
\*   - Case C (NR-flip fired by honest leader): symmetric.  σ-pool[V_L]
\*     ≤ s_h + leader_σ + byz_σ_V ≤ (f-1) + 1 + f = 2f < qV.
Pigeonhole1 ==
    \A k \in Layers:
        ~ ( (\E v \in Values : ClusterSigmaQuorumReached(k, v))
            /\ ClusterNRQuorumReached(k) )

\* Pigeonhole 2: at most one V σ-quorums at any layer.
\*
\* Algebraic basis: honest contribute h_σ_V + h_σ_V' ≤ 2f+1 (single-σ-V
\* exclusivity); byz contribute byz_σ_V + byz_σ_V' ≤ 2f.  Total ≤ 4f+1 <
\* 2·qV = 4f+2.  Holds under both flip mechanisms (σ-flip's σ goes onto a
\* single V the flipper observed, doesn't split honest contribution across
\* V's; single-σ-V still EKM-enforced).
Pigeonhole2 ==
    \A k \in Layers:
        Cardinality({v \in Values : ClusterSigmaQuorumReached(k, v)}) <= 1

\* Pigeonhole 3: across all K layers, at most one V reconstructs cluster-
\* wide.
\*
\* Cross-layer safety under chained encryption: V_j at L_j and V_k at L_k
\* (j ≠ k) cannot both reconstruct, because reconstructing V_k requires
\* NR-quorum at all prior layers (chained encryption gates), but Pigeonhole
\* 1 forbids σ-quorum at j AND NR-quorum at j to coexist.
Pigeonhole3 ==
    Cardinality({<<k, v>> \in Layers \X Values : Reconstructable(k, v)}) <= 1

\* Combined SAFETY property
SAFETY == Pigeonhole1 /\ Pigeonhole2 /\ Pigeonhole3

(***************************************************************************)
(* State constraint — bound state space for TLC by capping each pool at    *)
(* its quorum threshold.                                                   *)
(*                                                                         *)
(* Provably safe for the SAFETY property: Pigeonhole 1, 2, 3 are stated as *)
(* "pool size ≥ threshold" predicates, so they're already detectable when  *)
(* a pool first reaches the threshold.  States with pool size > threshold  *)
(* add no new information for SAFETY evaluation.                           *)
(*                                                                         *)
(* Furthermore, pool sizes only grow (actions never remove tuples), so any *)
(* state pruned by this constraint has predecessors at the threshold       *)
(* boundary that are explored normally.  TLC checks INVARIANTs on every    *)
(* visited state, including states that fail the CONSTRAINT — the          *)
(* CONSTRAINT only determines whether to expand successors.  Combined,     *)
(* this means zero risk of missing a counterexample for the four checked   *)
(* invariants.                                                             *)
(***************************************************************************)

StateConstraint ==
    /\ \A k \in Layers, v \in Values:
        Cardinality(ClusterSigmaPool(k, v)) <= QV
    /\ \A k \in Layers:
        Cardinality(ClusterNRPool(k)) <= QEnc

(***************************************************************************)
(* Symmetry — TLC explores one canonical state per equivalence class       *)
(* under permutations of Honest operators.  Byzantine operators are NOT    *)
(* permuted (they're distinguished by Byzantine designation).              *)
(*                                                                         *)
(* Values are NOT permuted because Pigeonhole 2's "two distinct V's reach  *)
(* qV" check needs to count per-(layer, V) σ commitments.  Permutation     *)
(* over values would conflate distinct-V configurations.                   *)
(***************************************************************************)

Symmetry == Permutations(Honest)

================================================================================

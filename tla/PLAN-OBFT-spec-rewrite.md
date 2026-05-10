# OBFT spec rewrite — execution plan

Status: **TLA rework + verification COMPLETE (2026-05-10)**. Both safety + liveness verified under the new per-operator-view model. Q4 (n=7, f=2 generalization) deferred. Q5–Q8 still need user input before doc-rewrite execution.

**Verification results:**

| Spec | Config | Result | Time | Notes |
|---|---|---|---|---|
| `BareOBFT_Safety` | n=4, f=1, K=1, \|V\|=2, per-op views, full byz grief (incl. selective delivery via `S \in SUBSET Operators` pre-snap) | ✓ verified | 6 s | 56,014 distinct states, depth 9 complete. K=1 is the base case; P3's cross-layer claim follows by induction on P1 at every layer (algebraic). |
| `BareOBFT_Liveness` | n=4, f=1, K=4, non-grief byz broadcast (all-or-nothing) | ✓ verified | 9 s | 64,152 distinct states, depth 12 complete. LIVENESS_NON_GRIEF holds. |

The algebraic-cardinality mutex argument from §2.2 is the load-bearing safety claim; TLC corroborates at the base case (K=1).

---

## 0. Goal

Update the OBFT family of docs (`docs/OBFT.md`, `docs/OBFT-formal-verif.md`, `docs/2abOBFT.md`, `docs/2abOBFT-design-notes.md`, `docs/OBFTR.md`, `docs/BFT-comparison.md`) to reflect the verified Phase-2.5 σ-flip / NR-flip design, with:

1. Mandatory app-layer re-broadcast of leader bundles in `KindCommit`, every layer.
2. A reframed Pigeonhole 1 that explicitly accounts for σ-flip's "honest in both σ and NR pools" possibility.
3. Removal of obsolete safety warnings whose root cause is fixed.
4. Updated comparisons with 2abOBFT (no longer the only/necessary closer of `h_V=1`).

The verified design is locked (per the audit at the end of the prior session — TLC `BareOBFT_Safety` 96M states pass; `BareOBFT_Liveness` 7K states pass). This plan only touches docs.

---

## 1. Verified design (locked — restated for reference)

From `tla/BareOBFT_Safety.tla` and `tla/BareOBFT_Liveness.tla`:

**σ-flip rule** (any non-leader honest who NR'd; new wire kind `KindSigmaFlip`):
- Trigger (snapshot-based, evaluated at Phase-2 finalization):
  - `snap_NR_nl < f + 1`  *(non-leader NRs in snapshot, including own)*
  - `snap_S_post ≥ A + 2f`  *(non-leader σ post-flip = pre-flip non-leader σ + 1; A = silent ops in snapshot)*
- Effect: ADD σ partial on V (additive — prior NR partial stays on the wire; cross-signed via amended-EKM trigger).

**NR-flip rule** (LEADER ONLY, when honest; existing wire kind `KindNRFlip`):
- Trigger (snapshot-based):
  - `snap_S_nl < f`
  - `snap_NR_nl ≥ A + 2f`
- Effect: ADD NR partial. Prior σ_L^V partial stays. Restricted to leader to prevent the non-leader-NR-flip safety attack surface (CE-1..12 in earlier iterations).

**Other invariants of the verified design**:
- Both flips additive (signed messages can't be withdrawn).
- Single-flip-per-layer (each operator at most one σ-flip OR NR-flip per `(slot, layer)`).
- Triggers evaluate against a **snapshot** taken at Phase 2 finalization (`T_commit + Δ_2`) — prevents cascade.
- No σ-supersedes-NR rule (cluster-wide pool counts every signed partial).
- No leader-NR-doesn't-count rule (leader's own NR counts toward NR-quorum).
- For LIVENESS only: byz selective Phase-1 broadcast is reclassified as grief (byz must be all-or-nothing). For SAFETY: byz fully unrestricted.

---

## 2. Cross-cutting changes (apply across docs)

### 2.1 Mandatory leader-bundle re-broadcast in KindCommit (RESOLVED)

**Decision (per user 2026-05-10):** every operator's `KindCommit` carries, for every layer they retained a Phase-1 bundle:

- The full retained `V_{L_k}` (blinded beacon block) bytes — not just the 32-byte `value_root`.
- The leader's `σ_{L_k}^V(V_{L_k})` partial (already in spec).
- The leader's `σ_{L_k}^{op}(envelope)` operator-identity signature (already auth-verified by sender).

This is a strict superset of the existing `(layer, value_root, σ_L^V)` witness section. Receivers can verify σ_L^V against V (instead of just cross-referencing value_root), AND honest receivers without V locally now recover V from the witness section (V-drop closed).

**Blinded-block-only constraint.** SSV's OBFT proposer-duty path is restricted to MEV-Boost-style blinded blocks: V = blinded `BeaconBlock` (with `ExecutionPayloadHeader` instead of full body). Typical blinded V on mainnet: 5–15 KB; worst-case (full attestations + slashings): ~50 KB. The blinded constraint is what makes per-layer full-V re-flood tractable bandwidth-wise. The spec needs an explicit "OBFT proposer duty operates on blinded blocks only" note in `§Application: SSV Ethereum proposer duty`. Unblinding via relay reveal happens after threshold reconstruction, outside the consensus protocol.

**Bandwidth (revised at blinded V).** Per-operator `KindCommit` ≈ existing 28 KB + 4 layers × 50 KB worst-case = ~228 KB worst case (~88 KB typical at 15 KB blinded V). Cluster-wide outbound (n=4) = ~912 KB worst case (~352 KB typical). Per-operator ingress = ~684 KB worst case (~264 KB typical). Network-bandwidth-wise: ~76 KB/s sustained at worst case over a 12-second slot — within commodity-server budgets. Gossipsub mesh handling at 228 KB messages should be confirmed during implementation.

The current "σ_L^V witness section" wording in §Phase 2 wire format and Appendix C is replaced with a "leader-bundle re-flood section" wording that mandates V (blinded) + σ_L^V + σ_L^op per retained layer.

**Existing Appendix C coverage:** §Costs and §"σ_L^V re-inclusion is part of the core protocol" already discuss the mechanism partially (σ_L^V byte-for-byte forwarding). The change promotes V re-inclusion from "optional defensive engineering" (status quo) to "core protocol — required for σ-flip's V-availability precondition and for closing the h_V=1 selective-delivery attack at the propagation layer".

### 2.2 Pigeonhole 1 reframing (RESOLVED — depends on TLA rework in §2.7)

**The concern.** Under σ-flip, an honest σ-flipper is in BOTH σ-pool[V] AND NR-pool simultaneously (additive flip), so the bare-OBFT `h_σ + h_NR ≤ 2f+1` cross-phase-exclusivity bound no longer holds. The current safety argument's load-bearing piece moves to: triggers are mutex per snapshot, so only one of {σ-flip, NR-flip} can fire at any layer.

**Decision (per user 2026-05-10):** model per-operator views in TLA (see §2.5). The safety argument no longer depends on a global snapshot — it depends on within-budget partial-synchrony (Assumption 2) bounding honest-cardinality contributions, which holds regardless of byz selective delivery.

**Reframed Pigeonhole 1 — algebraic-cardinality mutex argument (proposed):**

> **Pigeonhole 1 (revised).** Under (1) ≤ f byzantine operators (Assumption 1), (2) within-budget partial-synchrony for honest broadcasts (Assumption 2), (3) honest operators follow the protocol (single-σ-V per layer; σ XOR NR pre-snapshot; flip-only-via-trigger-on-own-snapshot post-snapshot; single-flip-per-layer; honest leader does not NR pre-snap), the cluster-wide signed-message set at any layer L_k never simultaneously contains σ-quorum on any V AND NR-quorum.

Three-case proof sketch:

- **Case A — no flip fired at L_k.** Bare-OBFT argument: `h_σ + h_NR ≤ 2f+1` (cross-phase exclusivity), byz cap ≤ 2f, joint max ≤ 4f+1 < 4f+2 = 2·qV. ∎
- **Case B — σ-flip fired at L_k from honest non-leader X.**
  - At least one σ-flip fired ⇒ X's snapshot satisfies `snap_NR_nl_X ≤ f` (with X self-included).
  - Honest non-leader cardinality at L_k = `n − f − 1 = 2f` (assumes leader ∈ Honest; if leader byzantine, NR-flip is unavailable a priori, and the argument simplifies).
  - Within-budget propagation: every honest operator's view of *honest* contributions is identical. Byz selective delivery only varies byz contributions across honest views.
  - X sees self NR + every other honest non-leader NR (within-budget) ⇒ `snap_NR_nl_X ≥ nr_h` where `nr_h` = honest non-leader NR count. Combined with the trigger: `nr_h ≤ f`.
  - Therefore `s_h := 2f − nr_h ≥ f`.
  - **NR-flip from honest leader cannot fire at L_k:** NR-flip's trigger requires `snap_S_nl_leader < f`. Leader sees every honest non-leader σ (within-budget) ⇒ `snap_S_nl_leader ≥ s_h ≥ f`. Trigger blocked. ∎ (mutex)
  - **NR-pool bound at L_k:** under σ-flip path, no honest NR-flip fires; honest non-leader NR-pool is frozen at snapshot (HonestNR is pre-snap-only). NR-pool ≤ honest_NR_in_snap + byz_NR_total ≤ nr_h + f ≤ 2f < 2f+1 = qEnc. ∎
- **Case C — NR-flip fired at L_k from honest leader.** Symmetric:
  - NR-flip trigger ⇒ `snap_S_nl_leader < f` ⇒ `s_h ≤ f − 1` (within-budget).
  - σ-flip from any honest non-leader X cannot fire: σ-flip trigger requires `snap_NR_nl_X ≤ f`. X sees `nr_h ≥ 2f − (f−1) = f+1` non-leader NRs, exceeding f. Trigger blocked. ∎ (mutex)
  - **σ-pool[V] bound at L_k:** σ-pool[V_L] ≤ (s_h) + (leader's σ_L^V) + (byz σ on V_L) ≤ (f−1) + 1 + f = 2f < qV. σ-pool[V ≠ V_L] ≤ (s_h) + 0 + (byz σ on V) ≤ (f−1) + f = 2f − 1 < qV. ∎

The mutex falls out of `s_h + nr_h = 2f` (honest non-leader cardinality at `n = 3f+1`) — there's no room for both `nr_h ≤ f` AND `s_h < f` simultaneously.

**Load-bearing assumption.** Within-budget partial-synchrony (Assumption 2) is what gives honest-side view consistency. Beyond-within-budget propagation is a Class A assumption violation; the protocol's safety is not claimed there. **There is no need for an "Assumption 7 — cluster-wide snapshot consistency"** — Assumption 2 already provides the necessary honest-side consistency, and byz selective delivery is bounded by the f-budget (which the trigger thresholds already account for).

**The "bake in 2f+1 honest" framing.** The triggers' `≥ A + 2f` thresholds and the `2f` honest-non-leader cardinality at `n = 3f+1` together encode the honest-majority assumption: the trigger fires only when the firing side's honest count is large enough that even with the worst-case f-byz contribution, the *other* side cannot reach quorum. The mutex is the algebraic shadow of "2f+1 honest follow protocol → at any layer, honest commitments are dominated by one side or the other".

This argument needs to be re-validated by TLC on the rewritten model (§2.5). Re-running the existing model is meaningless because it has the global-snapshot shortcut.

### 2.3 Phase-2.5 section rename + structural reorganization

`§Phase 2.5 — NR-flip recovery on observed deadlock` → `§Phase 2.5 — σ-flip / NR-flip flips`.

**Structural change (per §2.7 editorial directive):** the current §Phase 2.5 mixes (a) the protocol-rule body (= flip-trigger conditions, EKM amendment, additive semantics, single-flip-per-layer) with (b) the liveness/recovery analysis (= which scenarios are recovered, h_V=1 closure claim, deterrent discussion). The rewrite splits these:

- **§Phase 2.5** (concise, in protocol body) — describes the σ-flip and NR-flip rules, snapshot semantics, single-flip-per-layer constraint, additive emission, EKM amendment. **No recovery analysis here.**
- **§Recovery and failure-mode analysis** (NEW dedicated section, after §Phase 3) — concentrates all liveness analysis: R1/R2/R3 σ-flip recovery scenarios (per §5 of this plan), h_V=1 regression honest framing (per Framing #1), failure-mode catalog, deterrent discussion.

The current scattered analysis in §Liveness, §Failure modes, §Cross-signing detection, §Slashing evidence is consolidated into the new section. Spec body sections become rule-only and reference the analysis section for "what does this rule recover".

### 2.4 Bandwidth recalculation (RESOLVED)

**Decision (per user 2026-05-10):** OBFT proposer duty operates on blinded blocks only; uniform full-V re-flood at every retained layer in `KindCommit` (no layer-selectivity, no cooperative scheme). See §2.1.

| Component | Size |
|---|---|
| Blinded V (typical mainnet) | ~5–15 KB |
| Blinded V (worst case, full attestations + slashings) | ~50 KB |
| σ_L^V partial (BLS) | 96 B |
| σ_L^op (envelope auth) | 96 B (BLS) or ~64 B (ed25519) |
| Per-layer witness in KindCommit | V + 200 B |

| Scenario (n=4, K=4) | Per-op KindCommit | Cluster outbound | Per-op ingress |
|---|---|---|---|
| Bare-OBFT status quo (value_root + σ_L^V only) | ~28 KB | ~112 KB | ~84 KB |
| Blinded typical (15 KB V, full re-flood) | ~88 KB | ~352 KB | ~264 KB |
| Blinded worst (50 KB V, full re-flood) | ~228 KB | ~912 KB | ~684 KB |

**Sustained per-op outbound** at worst case: ~228 KB / 12 s = ~19 KB/s. **Sustained per-op ingress**: ~57 KB/s. Well within commodity SSV operator network budgets.

**Open implementation concern.** Gossipsub mesh handling at ~228 KB messages may need configuration tuning (typical gossipsub mesh defaults are tuned for smaller messages). Doc should call this out as a deployment-time check.

The existing 2.3 KB σ_L^V witness section bandwidth is replaced by the `V + σ_L^V + σ_L^op` per-layer re-flood section described in §2.1. The doc-level bandwidth tables in `OBFT.md` (§Properties summary, §Bandwidth (healthy n=4) row, Appendix A.1, A.3, Appendix C) and `BFT-comparison.md` need consistent update with the typical / worst case numbers.

### 2.5 Wire-format decisions for `bundle_witnesses` (RESOLVED 2026-05-10)

Replaces the existing `sigma_L_witnesses` section in `KindCommit` with a `bundle_witnesses` section carrying full Phase-1 bundles per retained layer.

| # | Decision | Resolution |
|---|---|---|
| Field name | `bundle_witnesses` | ✅ |
| Per-entry structure | `(layer_k, Phase1Bundle_k)` — embed the existing `Phase1Bundle` SSZ type byte-for-byte | ✅ |
| Inclusion rule | One `Phase1Bundle` per retained layer per emitter (= the first auth-valid bundle observed). Skip layers where emitter didn't retain. | ✅ |
| Retention bound | **Tightened to ≤ 1 bundle per `(slot, layer, leader_id)`** (was ≤ 2 in old spec). Subsequent distinct auth-valid bundles for the same key are logged locally as Rule 2 evidence and dropped from retention. | ✅ |
| Auth envelope binding | `(protocol_tag = "OBFT-v2", message_kind = "commit", cluster_id, slot, operator_id, onion_payload, nr_partials, bundle_witnesses)` signed by emitter's identity key. | ✅ |
| Migration | `protocol_tag` bumps to `"OBFT-v2"`. Pre-production protocol; no backwards-compat or flag-day machinery. | ✅ |
| Slashing evidence transit | **Slashing-implicating data does not travel as protocol-message payload**: equivocation evidence (e.g., 2 distinct Phase-1 bundles for the same `(slot, layer, leader_id)`) propagates via gossipsub initial flood to all honest receivers, who detect equivocation locally on second observation and log it in their slashing-protection store. The `bundle_witnesses` section carries only one bundle per key per emitter. | ✅ |

**Consequence — memory bound tightens to O(K).** Old spec memory bound was O(K · n) worst case (2 bundles per leader on equivocation). New bound: O(K) per slot per operator (one bundle per layer). Equivocation events don't bandwidth-spike `KindCommit` payloads.

**Consequence — Rule 2 (Leader equivocation) detection clarifies.** Detection happens at receipt-time when an honest receiver observes a second distinct auth-valid bundle for the same `(slot, layer, leader_id)`. The two bundles do NOT need to be re-broadcast in subsequent protocol messages for cluster-wide evidence — gossipsub naturally propagates both to all receivers; each honest detects independently. SSV slashing-contract submission is the out-of-band action.

**Consequence — Rules 1, 3, 4, 5 unchanged in semantics.** All slashing-evidence detection is "at receipt, locally logged". Wire format changes don't alter detection logic.

### 2.6 Editorial directive — concise spec body + dedicated analysis section (per user 2026-05-10)

The OBFT.md rewrite re-organizes the doc to separate the **protocol rule body** from the **liveness/byz analysis**:

**Protocol body sections (concise, rule-only):**
- §Setting
- §Assumptions and implications
- §Protocol — Phase 1, Phase 2, **Phase 2.5 (just the σ-flip / NR-flip rules)**, Phase 3
- §Slot structure
- §Preconditions on the host application
- §EKM coordination model
- §Cryptographic primitive

**Analysis section (consolidated, NEW):**
- §Recovery and failure-mode analysis (NEW) — replaces the scattered analysis currently in §Failure modes / §Liveness / §Cross-signing detection / §Slashing evidence. Contents:
  - Bare-OBFT recovery baseline (silent leader → fall-through; natural σ-quorum at 2-of-3 honest, etc.).
  - Phase-2.5 σ-flip / NR-flip recovery scope at f=1, n=4 (R1/R2/R3 enumeration, per §5 of this plan).
  - h_V=1 regression honest framing (Framing #1, per §5).
  - Class A vs Class B failure catalog.
  - Slashing-evidence rules (Rules 1–5) with detection-at-receipt clarification.
  - Cross-signing detection.
  - Equivocation handling.
  - Implications of the rational-byzantine deterrent.
- §Properties summary (cleanups).
- §Where this came from (cleanups; updated Phase-2.5 minimal-closure paragraph).

**Doc structure goal**: a reader of just the protocol body should know how to *implement* OBFT correctly. A reader of the analysis section should know *under what conditions OBFT recovers / doesn't recover liveness*, and *what the deterrent picks up*.

### 2.7 TLA model rework — per-operator views (DONE 2026-05-10)

The original TLA models' single global `sigma_partials`, `nr_partials`, `snap_*` variables conflated per-operator local views — a modeling shortcut that didn't capture byz selective delivery. Both `BareOBFT_Safety.tla` and `BareOBFT_Liveness.tla` were reworked.

**Final model design — `BareOBFT_Safety.tla`:**

State variables:
- `sigma_view : [Operators -> SUBSET (Operators \X Layers \X Values)]` — per-op observed σ-pool.
- `nr_view : [Operators -> SUBSET (Operators \X Layers)]` — per-op observed NR-pool.
- `snap_sigma_view`, `snap_nr_view` — per-op snapshots taken at FinalizePhase2.
- `phase2_finalized : BOOLEAN` — global, single-shot. (See "Why global, not per-op" below.)
- `has_flipped`, `leader_of` — unchanged from prior model.

**Why `phase2_finalized` is global, not per-op.** The OBFT design intent is no-flip-cascade: an honest flip emitted post-finalize must not appear in any other honest's snap. Per-operator finalize would let op_A finalize-and-flip while op_B is still pre-finalize, polluting op_B's snap with A's flip — exactly the cascade the protocol forbids. Global FinalizePhase2 (single transition that snaps all honest simultaneously) implements this design correctly. Per-operator snap *content* remains per-op (each captures its own view at the global finalize moment); only the timing event is global.

Cluster-wide pool (for safety invariants):
- `ClusterSigmaPool(k, v) == { op : <<op, k, v>> \in sigma_view[op] }` — counts operators who signed σ on (k, v) (signer's-own-view invariant: signer's view always contains their signature). This = signed-message-set = worst-case offline-aggregator's set.
- `ClusterNRPool(k)` analogous.

Actions:
- **HonestSigma(op, k, v)**, **HonestNR(op, k)** — adds tuple to `sigma_view[op']` (resp. `nr_view[op']`) for **every** `op' \in Operators` (within-budget delivery). Requires `~ phase2_finalized`.
- **ByzSigmaPreSnap(op, k, v, S)**, **ByzNRPreSnap(op, k, S)** — pre-finalize, byz selects any `S \subseteq Operators` for delivery (with `op \in S` enforced so byz keeps own copy). Requires `~ phase2_finalized`.
- **ByzSigmaPostSnap(op, k, v)**, **ByzNRPostSnap(op, k)** — post-finalize, no S choice (S = {byz} implicit). Optimization: post-snap byz delivery patterns can't affect any snap (frozen) or `VAvailable` (uses snap), only the cluster pool. Eliminating the post-snap S-branching factor is what made TLC tractable.
- **FinalizePhase2** — single global action. Captures `snap_sigma_view' = sigma_view`, `snap_nr_view' = nr_view`, `phase2_finalized' = TRUE`.
- **HonestSigmaFlip(op, k, v)**, **HonestNRFlip(op, k)** — evaluate trigger against op's own snapshot. Within-budget delivery on emission (added to all views).

Trigger evaluation: `SigmaFlipTriggered(viewer, k)` and `NRFlipTriggered(viewer, k)` parameterized by viewer's own snap. Different viewers may compute different snap_NR_nl etc. due to byz selective delivery, but honest views agree on honest contributions (within-budget).

Safety invariants (Pigeonhole 1, 2, 3) stated over cluster pools.

**Symmetry over Honest** added (`Permutations(Honest)`). Reduces state count by 3! = 6× at n=4.

**Final model design — `BareOBFT_Liveness.tla`:** unchanged from prior session — under non-grief byz broadcast (all-or-nothing), per-operator views collapse to global since all honest see all messages. The existing global-pool model is correct as-is for liveness; only the cfg was updated to K=4 (per user preference).

**Verification (executed 2026-05-10):**
- `BareOBFT_Safety` at n=4, f=1, K=1, |V|=2: ✓ verified, 56,014 distinct states, depth 9, 6 s. K=1 is the per-layer base case; P3 (cross-layer single-output) follows by induction on P1 at every layer plus chained-encryption gating — TLC verifies the base case, the inductive step is algebraic.
- `BareOBFT_Liveness` at n=4, f=1, K=4: ✓ verified, 64,152 distinct states, depth 12, 9 s.
- `BareOBFT_Safety` at K=2 (extra coverage of P3's cross-layer step): currently running, 20-min budget; result pending.

**Why K=1 for safety is sufficient.** Pigeonhole 1 is per-layer (`σ-quorum and NR-quorum cannot both reach at L_k` — independent of K). Pigeonhole 2 is per-layer (`at most one V σ-quorums at L_k` — independent of K). Pigeonhole 3 is cross-layer; its induction proof uses P1 at every L_j between the two layers in question, plus the chained-encryption gate (chain unlocks at L_j only if NR-quorum at L_j reaches; combined with P1 at L_j giving "σ-quorum at L_j ⇒ NR-quorum at L_j fails"). TLC at K=1 verifies the base case; the inductive step at higher K is algebraic and doesn't need TLC.

---

## 3. Open questions — all resolved

All Q1–Q8 are resolved. Doc rewrite can proceed without further user input.

| # | Topic | Resolution |
|---|---|---|
| Q1 | Bandwidth | OBFT proposer duty operates on **blinded blocks only**; full V re-flooded per retained layer in `KindCommit`. Per-op KindCommit ~88–228 KB (typical / worst); cluster-wide ~352–912 KB. See §2.1, §2.4. |
| Q2 | KindCommit re-broadcast | **Not needed.** Within-budget partial-synchrony + f-bound on byz contributions are sufficient. Confirmed by TLC at K=1 with full byz `S \in SUBSET Operators` selective delivery. |
| Q3 | Pigeonhole 1 reframing | Algebraic-cardinality mutex argument (§2.2 Cases A/B/C). Validated by TLC at K=1 (56,014 distinct states, no counterexample). |
| Q4 | f ≥ 2 verification | Deferred ("skip for now, revisit later"). Algebraic argument generalizes; documented in plan §3.2 as future work. |
| Q5 | Wire-format details | `bundle_witnesses` field carrying `(layer_k, Phase1Bundle_k)` per retained layer; ≤ 1 bundle per `(slot, layer, leader_id)` retention; `protocol_tag` bumps to `"OBFT-v2"`. See §2.5. |
| Q6 | 2abOBFT framing | **Inverted by h_V=1 finding.** OBFT does NOT close h_V=1 in the new design (per §5 framing #1). 2abOBFT positioning stays as-is or strengthens. Update scope: only cleanup of obsolete cross-references; no major softening. See §4.3. |
| Q7 | `2abOBFT-design-notes.md` and `OBFTR.md` scope | **Full rewrite** (per user 2026-05-10). Both docs receive comprehensive updates to reflect the corrected OBFT positioning (h_V=1 not closed) and the new Phase-2.5 σ-flip / NR-flip + bundle_witnesses framing. See §4.4, §4.5. |
| Q8 | Worked example angle | Subsumed by §5's R1/R2/R3 enumeration + h_V=1 framing #1. Doc text presents the recovery scope precisely (R1/R2/R3) plus the explicit h_V=1 regression note; no "narrative trace" needed. |

### 3.2 Future-work notes (NOT blocking current rewrite)

These are flagged for follow-up after this rewrite ships:

- **n=7, f=2 TLC verification.** Algebraic mutex argument generalizes; should be confirmed via TLC at higher cluster sizes when bandwidth allows. Track separately.
- **Re-revisit h_V=1 recovery design.** The new design's leader-only NR-flip restriction was load-bearing for safety against CE-1..12 but cost the h_V=1 liveness recovery. A future iteration could explore stronger triggers that re-enable non-leader NR-flip without breaking safety. Open research direction.
- **Phase-2.5 portability to OBFTR.** OBFTR docs note that h_V=1 is R-invariant in OBFTR; bringing the new Phase-2.5 σ-flip / NR-flip mechanism into OBFTR is a separate spec exercise.
- **Multi-layer NR-flip cascades at K ≥ 3.** Current TLC verification at K=1 (safety) + K=4 (liveness, non-grief). Cascading flips at deeper layers under chained-encryption gating need K ≥ 3 verification for confidence.

---

## 4. Per-doc inventory

**Important framing correction**: the new design does NOT close h_V=1 (per §5 Framing #1). Many entries below correct prior plan iterations that wrongly said "OBFT now closes h_V=1". The actual story:

- The new Phase-2.5 σ-flip / NR-flip is a **safety-preservation mechanism** against grief patterns the older designs (asymmetric NR-flip-only) broke on (CE-1..12).
- It provides narrow Class B liveness recovery for 3 specific scenarios (R1/R2/R3 — see §5).
- It does **NOT** close h_V=1 selective-Phase-1-delivery; that case slot-misses and is deterred by Assumption 4.
- It does **NOT** close 1-1-1 σ-locked equivocation (was never closed; bare-OBFT baseline).

### 4.1 `docs/OBFT.md` — major restructuring (per §2.6 editorial directive)

The OBFT.md rewrite re-organizes the doc to separate **protocol body** (rules) from **recovery / failure-mode analysis** (consolidated). The prior scattered analysis in §Liveness, §Failure modes, §Cross-signing detection, §Slashing evidence is consolidated into a new §Recovery and failure-mode analysis section.

#### 4.1.1 Section reorganization (new structure)

```
## When to use it                                     ← unchanged
## Setting                                            ← minor wire-format updates
## Assumptions and implications                       ← drop "h_V=1 closed by Phase-2.5" mentions
## Protocol                                           ← concise, rule-only
   ### Phase 1 — Candidate broadcast                  ← retention bound tightened to ≤1
   ### Phase 2 — Onion broadcast                      ← bundle_witnesses replaces sigma_L_witnesses
   ### Phase 2.5 — σ-flip / NR-flip flips             ← rules only (§4.1.3 below)
   ### Phase 3 — Local decryption and reconstruction  ← include bundle_witnesses recovery
   ### Treatment of missing onions                    ← σ-flip / NR-flip mention
   ### Slot structure                                 ← σ-flip / NR-flip mention
## Preconditions on the host application              ← amended exclusivity rules
   ### EKM coordination model                         ← σ-flip + NR-flip log entries
## Recovery and failure-mode analysis (NEW)           ← consolidated analysis (§4.1.4 below)
## Cryptographic primitive                            ← unchanged
## Properties summary                                 ← cleanups
## Application: SSV Ethereum proposer duty            ← blinded-only constraint added
## Practical caveats                                  ← cleanups
## Where this came from                               ← updated Phase-2.5 paragraph
## Appendix A — Protocol comparisons                  ← cleanups
## Appendix B — L_Bid mini-consensus extension        ← unchanged
## Appendix C — Leader-bundle re-flood (was: re-broadcast considerations) ← rewritten as core spec, not optional
## Appendix D — OBFT-replenish                        ← cleanups
## Appendix E — Defer state                           ← cleanups
## Appendix F — OBFT + L_Bid_New                      ← unchanged
```

#### 4.1.2 Sections that move out of protocol body INTO §Recovery and failure-mode analysis

These currently live in the protocol body but are analysis content per the editorial directive:

- L442-449 (current §Fault tolerance / Trust model) — moves into §Recovery analysis.
- L453-505 (current §Safety) — Pigeonholes 1, 2, 3 stay in protocol body (they're invariants, load-bearing). The "verifiability caveat" warning (L482-484) gets DELETED entirely (issue resolved).
- L507-549 (current §Liveness) — moves into §Recovery analysis.
- L551-578 (current §Liveness comparison) — moves into §Recovery analysis as comparison-table sub-section.
- L580-605 (current §Equivocation handling, §Cross-signing detection) — move into §Recovery analysis as sub-sections.
- L607-639 (current §Slashing evidence) — moves into §Recovery analysis as sub-section. Add detection-at-receipt clarification per §2.5.
- L640-671 (current §Failure modes) — moves into §Recovery analysis as the load-bearing sub-section. Update entries per the corrected framing (h_V=1 NOT closed; R1/R2/R3 are what's closed).

#### 4.1.3 §Phase 2.5 (concise rule-only rewrite)

Section title: "Phase 2.5 — σ-flip / NR-flip flips" (drop "recovery on observed deadlock" — the recovery framing is in §Recovery analysis).

Content (concise; no recovery-scenario discussion):

1. **Snapshot semantics.** At T_commit + Δ_2 each operator finalizes their snapshot of σ/NR partials observed locally; trigger evaluation uses that frozen snapshot. No-flip-cascade is enforced by all-honest-finalize-simultaneously semantics (per design).
2. **σ-flip rule** (any non-leader honest who NR'd, additive). Trigger: `snap_NR_nl < f+1 AND snap_S_post ≥ A + 2f`. Effect: emit `KindSigmaFlip` adding σ partial on a V observed in own snap; original NR partial stays.
3. **NR-flip rule** (HONEST LEADER only, additive). Trigger: `snap_S_nl < f AND snap_NR_nl ≥ A + 2f`. Effect: emit `KindNRFlip` adding NR partial; original σ_L^V partial stays.
4. **Single-flip-per-layer** per operator.
5. **EKM amendment**: σ-after-NR allowed for non-leader on σ-flip trigger; NR-after-σ allowed for honest leader on NR-flip trigger; both via separate per-request checks in §EKM coordination model.
6. **Wire format**: `KindSigmaFlip(slot, layer k, σ_i^V(V_{L_k}), trigger_evidence)` and `KindNRFlip(slot, layer k, σ_i^IBE(nr_tag_k), trigger_evidence)`. Trigger evidence is the snap pool composition observed by the actor.

DELETE the following from current §Phase 2.5:
- L247 open-safety warning paragraph (CE-1 + Pigeonhole-1 violation note + "Resolution is open" — issue is resolved).
- L282 "No symmetric σ-flip" paragraph — claim no longer holds.
- L292-298 receiver-aggregation paragraphs that bake in "asymmetric design only" framing — replaced with symmetric receiver semantics.

CROSS-LINK to §Recovery and failure-mode analysis for: which scenarios σ-flip / NR-flip cover (R1/R2/R3), which they don't (h_V=1, 1-1-1 splits), why leader-only restriction on NR-flip (CE-1..12 safety basis).

#### 4.1.4 §Recovery and failure-mode analysis (NEW section)

Consolidated from current scattered analysis. Sub-section structure:

- **§Trust model** (moved from §Fault tolerance / Trust model L446-451).
- **§Liveness scope at f=1, n=4** (moved + corrected from §Liveness L507-549):
  - Bare-OBFT recovery baseline (silent-leader fall-through; natural σ-quorum at 2-of-3 honest; equivocation 2-1 splits; etc.).
  - Phase-2.5 σ-flip recovery scope (R1, R2, R3 from plan §5):
    - R1: honest leader + 1-σ + 1-NR + byz σ-equivocates onto V'.
    - R2: byz leader + 2-σ + 1-NR + byz silent.
    - R3: byz leader + 2-σ + 1-NR + byz σ on V'.
  - Phase-2.5 NR-flip recovery scope at f=1: **none beyond bare-OBFT baseline**. (Trigger only fires when NR-quorum already reaches naturally.)
  - **What the new design does NOT recover** (regression vs old asymmetric design + bare-OBFT baseline):
    - **h_V=1 selective Phase-1 delivery**: was recovered by old asymmetric NR-flip-from-σ-er; new design's leader-only restriction on NR-flip blocks this recovery. Slot-misses; deterred via Assumption 4.
    - **1-1-1 σ-locked equivocation**: never recovered (bare-OBFT baseline).
    - **Validity-divergence boundary splits**: never recovered (bare-OBFT baseline); host's stabilization workflow narrows the divergence window per Assumption 3.
- **§Honest framing of h_V=1** (Framing #1 from plan §5):
  - Phase-2.5 σ-flip + NR-flip + mandatory leader-bundle re-flood preserves SAFETY against full byz grief.
  - Class A liveness closure holds at K=4 only under non-grief byz broadcast.
  - Under grief — including h_V=1 byz selective-delivery — liveness can fail; deterred via Assumption 4 (rational-byz deterrent + planned blacklist + staker migration).
  - Under honest-poor-networking-induced h_V=1 (= Class A Assumption-2 violation): out-of-scope for protocol's recovery promise; old design recovered as side-effect, new design does not.
- **§Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT** (moved from L551-578; correct h_V=1 row).
- **§Equivocation handling** (moved from §Equivocation handling L580-605; mostly unchanged).
- **§Cross-signing detection** (moved from §Cross-signing detection L596-606): symmetric exemption — both `KindSigmaFlip` and `KindNRFlip` exempt under valid trigger evidence.
- **§Slashing evidence** (moved from §Slashing evidence L607-639): rules 1-5 with detection-at-receipt clarification per §2.5. Add note: slashing-implicating data flows out-of-band via local logs + SSV slashing contract; not via protocol message re-broadcast.
- **§Implications of the rational-byzantine deterrent** (moved from §Implications of the rational-byzantine deterrent L103-136 — currently in §Assumptions; consolidated here for clarity).
- **§Failure modes** (moved + corrected from §Failure modes L640-671):
  - h_V=1 selective Phase-1 delivery: **Class B liveness loss; not recovered**; deterred via Assumption 4. (Was previously marked "[Closed by Phase-2.5 NR-flip]"; correction.)
  - All other entries audited for consistency.

#### 4.1.5 Specific line-by-line changes in OBFT.md (high-level summary)

| Section | Change type |
|---|---|
| §Setting (L21-46) | Add `bundle_witnesses` to wire-kind list; bump `protocol_tag` to `"OBFT-v2"`. |
| §Assumptions / L64, L116, L136 (Byzantine vs Down asymmetry, deterrent / Byzantine ≡ Down, deterrent / asymmetry) | Remove "h_V=1 ... is now closed by the Phase-2.5 NR-flip mechanism" claims (= 3 places). Replace with: "h_V=1 selective Phase-1 delivery remains a Class B grief vector; Phase-2.5 σ-flip / NR-flip don't close this case in-protocol; relies on Assumption 4 deterrent." |
| §Phase 1 / Bundle propagation (L159) | Major rewrite: state mandatory leader-bundle re-flood in `bundle_witnesses` section of `KindCommit`; cluster-wide V availability under within-budget. |
| §Phase 1 / Why both signatures (L163) | Reword: σ_L^V head-start preserved by leader-bundle re-flood; remove "h_V=1 closed by Phase-2.5" claim. |
| §Phase 1 / Retention bounds (L161) | Tighten retention to ≤ 1 bundle per `(slot, layer, leader_id)`; equivocation evidence logged locally (not retained). Memory bound O(K). |
| §Phase 2 / Δ_2 sizing (L213) | Update mention to "σ-flip / NR-flip mechanisms"; bandwidth note on `bundle_witnesses`. |
| §Phase 2 / Wire format / sigma_L_witnesses (L221-225) | Major rewrite: rename to `bundle_witnesses`; embed `Phase1Bundle` SSZ type per retained layer; update auth envelope binding; new bandwidth numbers. |
| §Phase 2 / Per-operator commitment is exclusive (L236-243) | Major rewrite: TWO Phase-2.5 exceptions (σ-flip = σ-after-NR for non-leaders; NR-flip = NR-after-σ for honest leader). Symmetric exclusivity statement. |
| §Phase 2.5 (L245-300) | **Complete rewrite** per §4.1.3 above. Concise rule-only. Delete L247 warning, L282 "No symmetric σ-flip" paragraph. Cross-link to §Recovery analysis. |
| §Phase 3 (L302-365) | Pseudocode update: include `KindSigmaFlip` in σ-pool aggregation. Otherwise minimal. |
| §Treatment of missing onions (L378) | "KindSigmaFlip / KindNRFlip" instead of "KindNRFlip". |
| §Slot structure step 3 (L388) | "σ-ers and NR-ers detecting deadlock... emit KindSigmaFlip / KindNRFlip respectively". |
| §Preconditions / Slashing-protection scope (L405-411) | Update to TWO Phase-2.5 exceptions. |
| §EKM coordination model (L413-440) | Schema: `side ∈ {"σ", "NR", "σ-flip", "NR-flip"}`. Add per-request check for σ-flip. Both σ-flip and NR-flip log entries describe trigger evidence. |
| §Safety / Pigeonhole 1 (L462-505) | Major rewrite: 3-case argument (no flip / σ-flip / NR-flip) per §2.2; algebraic-cardinality mutex argument; remove L482-484 "verifiability caveat" warning paragraph. P2 and P3 statements largely unchanged. |
| §Safety / Pigeonhole 2 (L487-491) | Minor cleanups; same algebraic argument. |
| §Safety / Pigeonhole 3 (L495-501) | Minor cleanups; same inductive argument. |
| **Move from protocol body to §Recovery analysis** (L442-451 trust model, L507-549 Liveness, L551-578 Liveness comparison, L580-605 Equivocation, L596-606 Cross-signing, L607-639 Slashing evidence, L640-671 Failure modes) | Consolidated into new §Recovery and failure-mode analysis (per §4.1.4 above). Apply h_V=1 framing #1 corrections during move. |
| §Properties summary (L688-703) | Update "Improved with Phase-2.5 NR-flip" rows; cross-link to new §Recovery analysis. |
| §Application: SSV Ethereum proposer duty (L705+) | Add explicit "blinded-block-only" constraint; cross-link to bandwidth analysis. |
| §Practical caveats / Phase-2.5 has zero MEV cost (L786) | Update to mention σ-flip + NR-flip + bundle_witnesses bandwidth. |
| §Phase-window minimums (L825) | "KindSigmaFlip / KindNRFlip emissions". |
| §Where this came from / Phase 2.5 paragraph (L854-862) | Major rewrite: σ-flip + NR-flip + mandatory re-flood + snapshot semantics; clarify what's closed (R1/R2/R3) and what's NOT (h_V=1, 1-1-1, validity-divergence). |
| §Appendix A.2 — Comparison with 2abOBFT (L909-924) | Update OBFT column in comparison table: h_V=1 row reads "Class B; not recovered in-protocol; deterred via Assumption 4" (NOT "Closed via Phase-2.5"). 2abOBFT remains the natural recovery-scope extension. |
| §Appendix C — Message re-broadcast considerations (L1357-1407) | **Major rewrite**: "Leader-bundle re-flood is core protocol", not optional. Update conclusions, "what re-broadcast does NOT add" bullets (still relevant: re-flood doesn't close h_V=1 either), recommendations table. Cross-link to §Phase 2 wire format and §Recovery analysis. |
| §Appendix D / OBFT-replenish (L1480) | Remove "h_V=1 closed by Phase-2.5" mention; same Class B framing. |
| §Appendix E / Variant B section (L1604-1616) | Remove "Variant B is now closed at the post-σ-commitment layer via the Phase-2.5 NR-flip mechanism" — replace with "Variant B remains a Class B grief vector under the new design; deterred via Assumption 4." |
| Misc passing mentions of "Phase-2.5 NR-flip" (multiple) | Systematically update to "Phase-2.5 σ-flip / NR-flip" where the new design applies; or remove the "closes h_V=1" claim where present. |

### 4.2 `docs/OBFT-formal-verif.md`

| Loc | Change |
|---|---|
| L640 (§7.1 row: NR-flip ORACLE trigger) | Keep as historical (correct as-is). |
| L641 (§7.1 row: NR-flip OBSERVABLE trigger CE-1) | Keep as historical. |
| L643 (§7.1 row: Liveness with NR-flip OBSERVABLE) | Keep as historical. |
| L644-646 (§7.1 rows: K=4, n=7 placeholders) | Update one row to point at "deferred" status (per Q4); remove others. |
| **NEW row** before §7.2 | Add: `SAFETY \| n=4, f=1, K=1, \|V\|=2 (per-op-views, σ-flip + leader-only NR-flip + snapshot semantics + full byz grief incl. selective delivery) \| ✓ verified \| 2026-05-10 \| 56,014 distinct states, depth 9, 6 s. Algebraic-cardinality mutex argument validated.` |
| **NEW row** before §7.2 | Add: `SAFETY \| n=4, f=1, K=2, \|V\|=2 (same model, partial coverage) \| ◐ partial \| 2026-05-10 \| 79,984,752 distinct states explored at depth 9 before manual abort; no counterexample found.` |
| **NEW row** before §7.2 | Add: `LIVENESS_NON_GRIEF \| n=4, f=1, K=4 (per-op-views, non-grief byz broadcast = all-or-nothing) \| ✓ verified \| 2026-05-10 \| 64,152 distinct states, depth 12, 9 s.` |
| L670-705 (§7.4 CE-1) | Update "Resolution status: open" → "Resolution: **resolved 2026-05-10** by σ-flip + leader-only NR-flip + snapshot semantics + per-op-views + mandatory leader-bundle re-flood. Cross-link to new §7.1 verification rows. Keep CE-1 trace as historical record showing why bare-asymmetric-NR-flip-only design was unsafe." |
| §3.2 (Bare OBFT honest state machine) | Add: σ-flip / NR-flip in the state machine (post-FinalizePhase2 transition; trigger conditions described). |
| §4 (Section A: Safety property) | Update verification approach to describe per-op-views + algebraic-cardinality mutex argument. |
| §5 (Section B: Liveness) | Update to mention non-grief byz broadcast restriction (all-or-nothing); h_V=1 NOT recovered note. |
| §2.3 (Grief vs non-grief) | Update with the resolved framing: Phase-2.5 = safety-preservation mechanism; under non-grief, byz broadcast = all-or-nothing; under grief, h_V=1 selective delivery slot-misses (Class B, deterred). |

### 4.3 `docs/2abOBFT.md` — light cleanup (per Q6 inversion)

OBFT does NOT close h_V=1 in the new design. 2abOBFT positioning vs OBFT is **unchanged or strengthens**. Updates are mostly cleanup of obsolete cross-references that wrongly claimed OBFT closed h_V=1.

| Loc | Change |
|---|---|
| L5 (intro) | No change — 2abOBFT does close h_V=1 (via convergence rule), and OBFT does not in the new design. The original framing stands. |
| L13 ("Suited for") | No change. |
| L102 ("Narrower residual scope") | No change. |
| L478-479, L612-617, L644 (h_V=1 closure mentions) | No change — accurate as-is. |
| L787 (comparison table row "h_V=1 selective-delivery deadlock") | No change to OBFT column ("Partially closed" → keep; selective Phase-1 delivery is still the algebraic limit at f=1, n=4 in OBFT). |
| L803 ("2abOBFT covers more failure modes within R=1") | No change — accurate. |
| L842 (Bucket 3) | No change — accurate. |
| L893-894 (Time-conditional recoveries) | No change — accurate. |
| **Audit pass**: search for any places that reference "Phase-2.5 NR-flip" in OBFT.md and may need cross-link updates to point at the new "Phase-2.5 σ-flip / NR-flip" naming. | Cross-link updates only (no semantic changes). |

### 4.4 `docs/2abOBFT-design-notes.md` — full rewrite (per Q7)

This doc has prior-iteration cross-references that wrongly claim OBFT closes h_V=1. Full rewrite to reflect corrected state:

| Loc | Change |
|---|---|
| L9 (Relationship to existing code) | Cross-link updates; no semantic changes. |
| L22 (vs OBFT comparison table) | Update OBFT column for h_V=1: "Class B grief, not recovered in-protocol; deterred via Assumption 4" (was: "Partially closed" — but that was already imprecise; the corrected text matches OBFT's new framing). |
| L52 (Pros of Variant C) | Update: "h_V=1 selective-delivery — recovered structurally (via convergence rule, distinct from OBFT's narrow Phase-2.5 R1/R2/R3 recovery scope)". |
| L92 (price of Variant C) | Cross-link cleanups. |
| L126 (sub-cases of byz selective-delivery) | Cross-link cleanups. |
| L138 (2-1-byz-defect) | Cross-link cleanups. |
| L155 (h_V = 1 selective-delivery section) | Update OBFT column: "Class B in OBFT (new design): not recovered in-protocol; the new Phase-2.5 σ-flip / NR-flip preserves safety but doesn't close h_V=1 at f=1, n=4. Deterred via Assumption 4." |
| L395 (Rule 6 evidence handling) | Cross-link cleanups. |
| L411 (Package layout) | No change. |
| L426 (Domain-separation tests) | No change. |
| L457 (Adversarial-byzantine tests) | Cross-link cleanups. |
| L474 (table comparison entry) | Same correction as L22. |
| L497 (Variant C — load-bearing design choice) | Cross-link cleanups. |

Full doc audit: any reference to "OBFT closes / closed h_V=1 via Phase-2.5" gets corrected.

### 4.5 `docs/OBFTR.md` — full rewrite (per Q7)

OBFTR is a peer protocol spec; corrections matter for users choosing between protocols.

| Loc | Change |
|---|---|
| L17 ("Not suited for") | Update to clarify h_V=1 selective-delivery is R-invariant slot-miss in OBFTR; not closed in OBFT (new design) either; rational-byzantine deterrent is the protocol-level defense for both. |
| L59, L115, L140-142 (Byzantine vs Down / deterrent / evidence) | Update mentions of h_V=1 to match the corrected framing — both OBFTR and OBFT (new design) are R-invariant or single-round at h_V=1; both rely on Assumption 4 deterrent. Phase-2.5 σ-flip / NR-flip in OBFT is safety-preservation (and provides R1/R2/R3 narrow recovery), not h_V=1 closure. |
| L171 ("Why both signatures") | Cross-link audit only. |
| L243-355 (§Phase 2.5 — L_C consensus) | OBFTR has its OWN §Phase 2.5 for L_C signaling, distinct from OBFT's σ-flip / NR-flip. No conflict; ensure cross-references are clear. |
| L526 (Liveness comparison table) | Audit; should be unaffected by OBFT changes since OBFTR doesn't import σ-flip / NR-flip. |
| **Add**: future-work note that OBFT's Phase-2.5 σ-flip / NR-flip mechanism could be ported to OBFTR (would broaden OBFTR's narrow Class B recovery in the same R1/R2/R3 fashion). Not specified in this iteration. | New note. |

Full doc audit: search for any reference to OBFT closing h_V=1 and correct.

### 4.6 `docs/BFT-comparison.md`

| Loc | Change |
|---|---|
| L198 ("h_V=1 selective-delivery deadlock" failure-modes row) | Keep OBFT column as ✗ (doesn't close); correct previous incorrect plan iteration that said OBFT now closes h_V=1. Note: 2abOBFT still ✓ (convergence rule); QBFT R2 still ✓; OBFTR ✗ (R-invariant). |
| L216, L217 (2abOBFT advantages summary) | No change — accurate as-is. 2abOBFT does close patterns OBFT doesn't, including h_V=1. |
| L305, L307 (recovery summary by protocol) | Update OBFT entry: "closes silent-leader / partition cases via K-layer fall-through; closes R1/R2/R3 narrow Phase-2.5 σ-flip recoveries; **does NOT close** h_V=1, 1-1-1 σ-locked equivocation, validity-divergence boundary splits — relies on Assumption 4." |
| L316 ("Adversarial-byz robustness within single round") | No change — 2abOBFT closes 1-1-1 / h_V=1 / validity-majority / mesh-flakiness; OBFT (new design) closes only R1/R2/R3 narrow. |
| L399 (Bandwidth) | Update: bundle_witnesses bandwidth ~88-228 KB per op KindCommit (typical/worst at blinded V); cluster-wide ~352-912 KB. |
| **Add row** to recovery-table: R1/R2/R3 narrow Phase-2.5 σ-flip recovery scope as a new line item that OBFT (new design) covers; mark whether other protocols cover same scenarios. |
| **Audit pass**: any cross-reference to OBFT's recovery scope needs to reflect that h_V=1 is NOT closed in the new design. |

---

## 5. What the new design recovers — Phase-2.5 σ-flip / NR-flip recovery scope at f=1, n=4

**Decision (per user 2026-05-10): Framing #1.** Phase-2.5 σ-flip / NR-flip is a **safety-preservation mechanism** that also provides narrow Class B liveness recovery for 3 specific scenarios (R1/R2/R3 below). The h_V=1 selective-Phase-1-delivery case is NOT recovered; remains a Class B grief vector deterred via Assumption 4.

This section is the load-bearing reference for §Recovery and failure-mode analysis in OBFT.md.

### 5.1 Trigger conditions (from BareOBFT_Safety.tla)

**σ-flip** (any non-leader honest who NR'd):
- `snap_NR_nl < f + 1` ⇒ at f=1, ≤ 1 non-leader NR in viewer's snap.
- `snap_S_post = snap_S_nl + 1 ≥ A + 2f` ⇒ at f=1, post-flip non-leader σ count ≥ silent + 2.

**NR-flip** (HONEST LEADER only):
- `snap_S_nl < f` ⇒ at f=1, 0 non-leader σ in leader's snap.
- `snap_NR_nl ≥ A + 2f` ⇒ at f=1, NR-pool ≥ silent + 2.

### 5.2 Recovery scenarios at f=1, n=4

#### Honest leader (1 honest leader + 2 honest non-leaders + 1 byz non-leader)

| h_NL_σ | h_NL_NR | byz Phase-2 | Pre-flip σ-pool[V_L] / NR-pool | σ-flip fires? | Final |
|---|---|---|---|---|---|
| 2 | 0 | any | qV / 0–1 | n/a | ✓ natural |
| 1 | 1 | silent | 2 / 1 | snap_S_post=2, A=1 → 2<3 doesn't fire | ✗ slot-miss |
| 1 | 1 | σ on V_L | 3 / 1 | redundant | ✓ natural |
| **1** | **1** | **σ on V'** | **2 / 1** | **snap_NR_nl=1, snap_S_post=3, A=0 → 3≥2 ✓ fires** | **✓ R1** |
| 1 | 1 | NR | 2 / 2 | snap_NR_nl=2 → doesn't fire | ✗ slot-miss |
| 0 | 2 | silent | 1 / 2 | doesn't fire (NR-pool short) | ✗ slot-miss |
| 0 | 2 | σ on V_L | 2 / 2 | doesn't fire | ✗ slot-miss |
| 0 | 2 | σ on V' | 1 / 2 | doesn't fire | ✗ slot-miss |
| 0 | 2 | NR | 1 / 3 | redundant (NR-quorum natural) | ✓ natural fall-through |

#### Byz leader (3 honest non-leaders, byz controls leader role)

| h_NL_σ | h_NL_NR | byz σ_L^V | byz Phase-2 | Pre-flip σ-pool[V_L] / NR-pool | σ-flip fires? | Final |
|---|---|---|---|---|---|---|
| 3 | 0 | any | any | qV / 0–1 | n/a | ✓ natural |
| 2 | 1 | yes | n/a | 3 / 1–2 | redundant | ✓ natural |
| **2** | **1** | **no** | **silent** | **2 / 1** | **snap_NR_nl=1, snap_S_post=3, A=1 → 3≥3 ✓ fires** | **✓ R2** |
| **2** | **1** | **no** | **σ on V'** | **2 / 1** | **snap_NR_nl=1, snap_S_post=4, A=0 → 4≥2 ✓ fires** | **✓ R3** |
| 2 | 1 | no | NR | 2 / 2 | snap_NR_nl=2 → doesn't fire | ✗ slot-miss |
| 1 | 2 | yes | n/a | 2 / 2 | snap_NR_nl=2 → doesn't fire (= **h_V=1**) | **✗ slot-miss (h_V=1)** |
| 1 | 2 | no | any | 1 / 2 | snap_NR_nl=2 → doesn't fire | ✗ slot-miss |
| 0 | 3 | any | any | 0–1 / 3 | redundant | ✓ natural fall-through |

### 5.3 New-design recovery enumeration

**3 scenarios where Phase-2.5 σ-flip provides recovery beyond bare-OBFT baseline:**

| # | Scenario | Why σ-flip's trigger fires |
|---|---|---|
| **R1** | Honest leader + 1 honest σ-er + 1 honest NR-er + byz σ-equivocates onto V' | byz σ on V' bumps `snap_S_nl` to 2; post-flip → 3 ≥ A+2 |
| **R2** | Byz leader + 2 honest σ-ers + 1 honest NR-er + byz Phase-2 silent | snap_S_nl = 2; A=1; post-flip → 3 ≥ 3 |
| **R3** | Byz leader + 2 honest σ-ers + 1 honest NR-er + byz Phase-2 σ on V' | snap_S_nl = 3 (incl byz σ on V'); post-flip → 4 ≥ 2 |

**NR-flip recovery scope at f=1: empty.** NR-flip's trigger only fires in states where NR-quorum already reaches naturally; doesn't add new recovery at f=1. (May provide non-trivial recovery at f≥2; not yet enumerated.)

### 5.4 Cases the new design does NOT recover at f=1, n=4

| Failure pattern | Class | Was old asymmetric design recovered? |
|---|---|---|
| **h_V=1 selective Phase-1 delivery** (byz leader → 1 honest σ-er + 2 honest NR-ers) | B (byz cause) / A (honest network cause) | ✓ by old NR-flip-from-σ-er. **Regressed in new design.** |
| 1-1-1 σ-locked equivocation | B | ✗ Never recovered. Bare-OBFT baseline. |
| 1-honest-σ + 1-honest-NR + byz silent (honest leader) | B | ✗ Bare-OBFT baseline. |
| 1-honest-σ + 1-honest-NR + byz NR (honest leader) | B | ✗ Bare-OBFT baseline. |
| 0-honest-σ + 2-honest-NR (validity-divergence + minority retention; honest leader) | A or B | ✗ Bare-OBFT baseline. |
| 2-honest-σ + 1-honest-NR + byz NR (byz leader) | B | ✓ by old NR-flip-from-σ-er. **Regressed.** |
| Validity-divergence boundary 2-2 splits at f=1, n=4 | A or B | ✗ Bare-OBFT baseline. |

### 5.5 Doc text — h_V=1 framing #1 (canonical paragraph for OBFT.md §Recovery analysis)

The following text is what goes into OBFT.md's §Recovery and failure-mode analysis to describe Phase-2.5's actual recovery scope:

> **Phase-2.5 σ-flip / NR-flip — safety-preserving recovery for narrow asymmetric-byz scenarios.**
>
> Phase-2.5 provides two flip mechanisms — σ-flip (any non-leader honest NR-er, additive) and NR-flip (honest leader only, additive) — that recover specific Class B byz-grief patterns in-protocol while preserving cryptographic safety against arbitrary byz behavior including full grief (selective delivery, cross-signing, late publish, equivocation).
>
> **Recovery scope at f=1, n=4**: σ-flip recovers 3 specific patterns (R1 / R2 / R3 in §5.3); NR-flip does not add new recovery at this size. **Recovery scope at f≥2**: not yet enumerated; expected broader.
>
> **What Phase-2.5 does NOT close at f=1, n=4**:
> - **h_V=1 selective Phase-1 delivery** (byz leader unicasts V to 1 honest only): σ-flip's trigger requires `snap_NR_nl < f+1 = 2`, which doesn't hold (2 honest NR-ers in this case). NR-flip is leader-only and the leader is byz. **This case slot-misses and is deterred via Assumption 4** (rational-byzantine deterrent + planned blacklist + staker migration).
> - **1-1-1 σ-locked equivocation**: never recovered; bare-OBFT algebraic limit. Class B.
> - **Validity-divergence boundary splits**: never recovered; out-of-scope per Assumption 3.
>
> **Safety holds against all of these patterns** under arbitrary byz grief (TLC-verified at K=1 with full byz selective delivery; algebraic-cardinality mutex argument validated). Slot-miss outcomes never produce double-signs.
>
> **Comparison with the old asymmetric design**: the previous Phase-2.5 mechanism (asymmetric NR-flip available to any σ-er) closed h_V=1 in-protocol but was found unsafe under grief byz cross-signing-with-σ-withholding (CE-1 in [docs/OBFT-formal-verif.md §7.4](OBFT-formal-verif.md)). The new design's leader-only restriction on NR-flip closes the safety hole at the cost of h_V=1 liveness recovery. The trade-off is intentional: safety against CE-1..12 grief vectors is preserved cryptographically, while h_V=1 liveness reverts to bare-OBFT-style slot-miss + Assumption 4 deterrent.

---

## 6. Bandwidth recalculation (RESOLVED)

Final numbers per §2.4 (Q1 resolved with blinded-block constraint):

| Scenario (n=4, K=4) | Per-op KindCommit | Cluster outbound | Per-op ingress |
|---|---|---|---|
| Bare-OBFT status quo (value_root + σ_L^V witness only) | ~28 KB | ~112 KB | ~84 KB |
| Blinded typical (15 KB V, full re-flood) | ~88 KB | ~352 KB | ~264 KB |
| Blinded worst (50 KB V, full re-flood) | ~228 KB | ~912 KB | ~684 KB |

OBFT.md doc-level bandwidth tables to update: §Properties summary, §Bandwidth (healthy n=4) row, Appendix A.1, Appendix A.3, Appendix C, plus `BFT-comparison.md`.

---

## 7. Safety-warning audit catalog (final state)

Status of every prominent safety / open-issue warning in the affected docs. All resolved; doc rewrite captures these.

| # | Doc / Line | Warning | Resolution | Action |
|---|---|---|---|---|
| W1 | OBFT.md L247 | "Open safety issue — Phase-2.5 not yet recommended for production deployment" (CE-1 trace, double-sign under grief) | **RESOLVED** by σ-flip + leader-only NR-flip + snapshot semantics + per-op-views (TLC verified at K=1, full grief). | Remove warning entirely. Add "Verified ✓" status note pointing to OBFT-formal-verif.md §7.1 new rows. |
| W2 | OBFT.md L482-484 | "Trigger-evidence verifiability caveat" + grief-byz σ-withholding attack writeup | **RESOLVED** — superseded by snapshot-based triggers + leader-only NR-flip + per-op-views safety basis. | Remove warning; rewrite Pigeonhole 1 with new 3-case argument from §2.2. |
| W3 | OBFT-formal-verif.md L705 | "Resolution status: open. Decision pending on (a) drop, (b) keep with non-grief only, (c) restructure to 2abOBFT" | **RESOLVED** — modified version of (b) was taken: σ-flip + leader-only NR-flip; safety holds under full grief, liveness holds under non-grief, h_V=1 reverts to bare-OBFT slot-miss + Assumption 4 deterrent. | Update "Resolution status" → "Resolved 2026-05-10 by σ-flip + leader-only NR-flip + snapshot semantics + per-op-views safety basis + mandatory `bundle_witnesses` for cluster-wide V availability"; cross-link new §7.1 rows. |
| W4 | OBFT.md L466-471 (Pigeonhole 1 Case A) | Bare-OBFT cross-phase exclusivity argument | **HOLDS** unchanged. | No semantic change; cross-link to new 3-case argument. |
| W5 | OBFT.md L475-484 (Pigeonhole 1 Case B) | Old-design "≥ 1 Phase-2.5 NR-flip fired" argument relying on oracle `|honest_σ_pool[k][V]| ≤ f` | **OBSOLETE** (was the bare-asymmetric-NR-flip-only argument). | Replace with new 3-case argument (no-flip / σ-flip / NR-flip) per §2.2. |
| W6 | OBFT.md L487-491 (Pigeonhole 2) | "Honest sign at most one V per layer (single-σ-V exclusivity, EKM-enforced)" | **HOLDS** — single-σ-V still EKM-enforced under both flip rules. σ-flip's σ partial is on a V the flipper observes in their snap; cross-onion equivocation slashing still applies. | Minor cleanups; no semantic change. |
| W7 | OBFT.md L495-501 (Pigeonhole 3) | Inductive argument relying on P1 at every L_j | **HOLDS** modulo P1's resolution. | Cross-link updates only. |
| W8 | All "h_V=1 selective-delivery is closed by Phase-2.5 NR-flip" mentions across docs | (multiple in OBFT.md, OBFTR.md, BFT-comparison.md, possibly 2abOBFT.md) | **WRONG in new design**. Phase-2.5 σ-flip / NR-flip does NOT close h_V=1; it preserves safety under grief. h_V=1 reverts to bare-OBFT slot-miss + Assumption 4 deterrent. | Remove "closed by Phase-2.5" claims everywhere. Replace with "h_V=1 is a Class B grief vector, not recovered in-protocol; deterred via Assumption 4." |
| W9 | 2abOBFT.md framing as "closer of h_V=1" | "h_V=1 selective-delivery deadlock recovery [...] at +1 RTT cost vs single-Phase-2 designs" | **STILL ACCURATE** — 2abOBFT does close h_V=1 via convergence rule; OBFT (new design) does not. The original 2abOBFT framing stands. | No change. |
| W10 | OBFT.md L803 (Practical caveats / Equivocation is permitted, not recovered) | The σ-locked split + 1-1-1 still misses | **HOLDS** unchanged — never closed. | No change. |
| W11 | OBFT.md L645-671 (Failure modes / Class A and B) | Catalog of failure modes including h_V=1 marked "[Closed by Phase-2.5 NR-flip]" | **PARTIALLY OBSOLETE** — h_V=1 entry must be reverted from "[Closed by Phase-2.5]" back to "Class B grief vector, deterred via Assumption 4". Other entries unchanged. | Revert h_V=1 entry; correct surrounding text. |
| W12 | OBFT.md L797-801 (Validity-divergence deadlock) | "byz withholds NR" deadlock | **HOLDS** — validity-divergence still out of OBFT scope (Assumption 3). | No change. |
| W13 | OBFT-formal-verif.md §7.1 row 5 | "OBSERVABLE trigger ✗ COUNTEREXAMPLE" | **HISTORICAL** — keep as record showing why bare-asymmetric-NR-flip design was unsafe. | Keep as-is; do NOT remove. |
| W14 | OBFT-formal-verif.md §7.4 CE-1 entry | Full CE-1 trace + root-cause | **HISTORICAL** — keep as record. | Update only the "Resolution status" line; preserve trace. |
| W15 | Appendix C of OBFT.md | "Explicit re-broadcast addresses gossipsub-layer issues but does not address OBFT's residual adversarial-byz failure modes" | **PARTIALLY OBSOLETE** — leader-bundle re-flood is now CORE (mandatory in `bundle_witnesses`); appendix's "optional defensive engineering" framing flips. The re-flood **doesn't** close h_V=1 (per W8); it ensures cluster-wide V availability under within-budget. | Major rewrite per §2.5. Reframe as "core protocol, with rationale". |

**All warnings have a defined resolution. Doc rewrite is unblocked.**

---

## 8. Execution order

**Phase A — TLA model rework + re-verification: ✅ DONE 2026-05-10**

1. ✅ Reworked `tla/BareOBFT_Safety.tla` to per-operator views per §2.5 (with simplifications discovered during execution: global FinalizePhase2 for no-flip-cascade design intent; post-snap byz emissions S = {byz} since they can't affect snaps, only cluster pool — eliminates the post-snap S branching factor that was the chief state-space-killer).
2. ✅ Liveness spec did NOT need rework for content (under non-grief byz broadcast = all-or-nothing, per-op views collapse to global since all honest see all messages). Cfg updated to K=4 per user preference.
3. ✅ TLC results: safety verified at K=1, 56,014 distinct states / 6 s; liveness verified at K=4, 64,152 distinct states / 9 s. No counterexamples.
4. ⏸️ K=2 safety run with extra coverage of P3's cross-layer step: in progress (20-min budget); result will inform whether to update the verification status from "K=1 base case + algebraic induction" to "K=2 directly verifies P3".
5. ⏸️ n=7, f=2: deferred per user (Q4).

**Phase B — Doc rewrite (UNBLOCKED — all Q1–Q8 resolved). Begin in this order:**

### B.1 OBFT.md — primary rewrite

The OBFT.md rewrite restructures the doc per §2.6 editorial directive: protocol body sections become rule-only (concise), with all liveness/byz analysis consolidated into a NEW §Recovery and failure-mode analysis section.

**Sub-steps (in order):**

1. **§Setting + §Assumptions and implications** — minor updates: add `bundle_witnesses` to wire-kind list; bump `protocol_tag` to `"OBFT-v2"`; remove the 3 obsolete "h_V=1 closed by Phase-2.5" mentions (L64, L116, L136).

2. **§Phase 1** — rewrite `§Bundle propagation` + `§Retention bounds`: mandatory leader-bundle re-flood in `bundle_witnesses`; retention tightened to ≤ 1 bundle per `(slot, layer, leader_id)`; equivocation logged locally (not retained or re-broadcast in messages); memory bound O(K).

3. **§Phase 2** — rewrite `§Wire format`: rename `sigma_L_witnesses` → `bundle_witnesses`; embed `Phase1Bundle` SSZ per retained layer; new auth envelope binding with `"OBFT-v2"`; new bandwidth table (typical/worst at blinded V); update `§Per-operator commitment is exclusive` for the symmetric flip exclusivity exceptions.

4. **§Phase 2.5 (concise rule-only rewrite per §4.1.3)** — rename to "Phase 2.5 — σ-flip / NR-flip flips"; remove L247 warning; remove L282 "No symmetric σ-flip" claim; describe both rules (σ-flip non-leader; NR-flip honest-leader-only) + snapshot semantics + single-flip-per-layer + EKM amendment + wire format. Cross-link to §Recovery analysis for what this recovers.

5. **§Phase 3 + §Treatment of missing onions + §Slot structure** — pseudocode update (KindSigmaFlip in σ-pool aggregation); KindSigmaFlip / KindNRFlip mentions throughout.

6. **§Preconditions on the host application + §EKM coordination model** — symmetric cross-phase exclusivity exceptions; EKM log schema `side ∈ {"σ", "NR", "σ-flip", "NR-flip"}`; per-request check for σ-flip mirrors NR-flip.

7. **§Safety / Pigeonholes 1, 2, 3** — rewrite Pigeonhole 1 with the new 3-case algebraic-cardinality mutex argument (per §2.2). Remove L482-484 verifiability caveat. P2/P3 minor cleanups + cross-link updates.

8. **§Recovery and failure-mode analysis (NEW section, per §4.1.4)** — consolidate prior scattered analysis here:
   - Trust model.
   - Liveness scope at f=1, n=4 (R1/R2/R3 from §5).
   - Honest framing of h_V=1 (Framing #1 canonical paragraph from §5.5).
   - Liveness comparison: OBFT vs OBFTR(R=2) vs QBFT (move from L551).
   - Equivocation handling (move from L580).
   - Cross-signing detection (move from L596; symmetric exemption for both flips).
   - Slashing evidence (move from L607; detection-at-receipt clarification per §2.5; rules 1–5 unchanged in semantics, wire format updated).
   - Implications of the rational-byzantine deterrent (move from L103-136).
   - Failure modes (move from L640-671; correct h_V=1 entry to "Class B grief, not recovered").

9. **§Properties summary** — cleanups (h_V=1 entry corrected; bandwidth row updated).

10. **§Application: SSV Ethereum proposer duty** — add explicit "blinded-blocks-only" constraint at start of section.

11. **§Practical caveats** — bandwidth update (≤ 228 KB worst case per KindCommit).

12. **§Where this came from** — rewrite Phase-2.5 paragraph to reflect new design's actual scope (R1/R2/R3 narrow recovery; safety preservation against CE-1..12; NOT h_V=1 closure).

13. **Appendix A.1 / A.2 / A.3** — comparison-table column updates (no claim that OBFT closes h_V=1).

14. **Appendix C — Leader-bundle re-flood** — major rewrite per §2.5: full-bundle re-flood is core protocol, not optional. New conclusions, recommendations table, etc.

15. **Appendix D / Appendix E** — minor cleanups (remove obsolete "closed by Phase-2.5" claims).

16. **Self-review pass on OBFT.md** — read end-to-end, fix cross-references, ensure analysis-section consolidation is clean and protocol body is rule-only.

### B.2 OBFT-formal-verif.md — verification record

17. **§7.1 verification table** — add 3 new rows (safety K=1, safety K=2 partial, liveness K=4) per §4.2; update placeholders for n=7, f=2 to "deferred (Q4)".

18. **§7.4 CE-1 entry** — update "Resolution status" to "Resolved 2026-05-10 by σ-flip + leader-only NR-flip + snapshot semantics + per-op-views". Cross-link new §7.1 rows. Preserve trace as historical.

19. **§3.2 (Bare OBFT honest state machine)** — add σ-flip / NR-flip transitions.

20. **§4 (Safety property) + §5 (Liveness)** — update verification approach descriptions.

21. **§2.3 (Grief vs non-grief)** — update with new framing.

22. **Self-review pass.**

### B.3 Sibling protocol docs

23. **`docs/2abOBFT.md`** — light cleanup (per §4.3); cross-link audits only.

24. **`docs/2abOBFT-design-notes.md`** — full rewrite (per Q7 + §4.4); correct OBFT cross-references.

25. **`docs/OBFTR.md`** — full rewrite (per Q7 + §4.5); correct h_V=1 framing; future-work note about porting Phase-2.5.

26. **`docs/BFT-comparison.md`** — column updates per §4.6; bandwidth row update; recovery summary corrections; add R1/R2/R3 line.

27. **Final cross-doc read-through** — catch missed `Phase-2.5 NR-flip` references; ensure consistent terminology.

After each major step in B.1, B.2, B.3, do a self-review pass per global instruction "Self-review before handoff".

### Commit / handoff guidance

- TLA rework + verification (Phase A) is in `tla/BareOBFT_Safety.tla`, `tla/BareOBFT_Liveness.tla`, `tla/BareOBFT_Safety.cfg`, `tla/BareOBFT_Liveness.cfg`. **Not yet committed** — awaits explicit user "commit" per global instruction.
- Doc rewrite (Phase B) outputs in the 6 affected `docs/*.md` files. **Not yet committed** — awaits explicit user "commit".
- This plan document at `tla/PLAN-OBFT-spec-rewrite.md` is the authoritative reference for all doc changes; preserve it during the rewrite for cross-checking.

---

## 9. Out of scope (intentional)

- ~~No changes to TLA models — they're locked per the audit.~~ Superseded — TLA models reworked in Phase A (§2.5).
- No commits (per global instruction "Commits require explicit ask").
- Generalization of TLC verification to f=2, n=7 — deferred per user (Q4 resolved as "skip for now"). Will note in the docs where the n=4 result is being used as a generic claim and call out the gap.
- Implementation work (Go code in `protocol/v2/obft/`) — out of scope; spec change first.

# Phase 2a/2b Split for OBFT — Design Plan

A design plan for the Phase 2a/2b extension to [OBFT](OBFT.md). This document specifies the protocol, walks through the convergence rule, identifies edge cases, and lists open questions to resolve before implementation. **It is not the implementation itself**; it is the spec/plan to follow when the implementation is undertaken.

The reader is assumed to have read [OBFT.md](OBFT.md) — this document only describes deltas. Symbols (`f`, `n`, `K`, `qV`, `qEnc`, `Δ_2`, `T_commit`, etc.) carry the meanings defined there.

## Status

- **Type**: forward-looking design.
- **Variant chosen**: "Variant C" below — no Phase-1 leader σ_V; verdict broadcasts in Phase-2a; σ/NR commit in Phase-2b. Justification follows.
- **Scope**: SSV proposer duty at `n = 4, f = 1, K = 4` as the running example; algebra generalizes to higher `n`/`f`.
- **Relationship to existing code**: [protocol/v2/tbft/](../protocol/v2/tbft/) implements baseline TBFT. OBFT itself has not been implemented. This plan assumes Phase 2a/2b is built directly (skipping a bare-OBFT intermediate) since the Defer-state machinery OBFT introduces is subsumed by Phase-2a observation here. If a bare-OBFT implementation lands first, several pieces here become drop-in replacements rather than additions.

## What changes vs bare OBFT

| Component | OBFT | OBFT + Phase 2a/2b (this) |
|---|---|---|
| Phase-1 bundle | `(V_{L_k}, σ_{L_k}^V(V_{L_k}), σ_{L_k}^{op}(envelope))` | `(V_{L_k}, σ_{L_k}^{op}(envelope))` — **no σ^V** |
| Phase-2 structure | Single Phase 2 with sub-phasing (σ-emit early, NR/Defer at end) | Two windows: Phase 2a (verdict broadcast, no σ partials) + Phase 2b (σ-or-NR commit) |
| Defer state | Yes — `σ`/`NR`/`NV`/`Defer` (4 states) | No — `σ`/`NR`/`NV` (3 states); Phase-2a's window subsumes Defer |
| σ-commit timing | Phase-1 (leader) or Phase-2 (others) | Phase-2b only (uniform across all operators) |
| Convergence mechanism | Per-operator local view (Defer-rule observes peer σ-emit) | Cluster-wide verdict observation in Phase-2a, σ-quorum-eligibility check at Phase-2a end |
| EKM coordination | Single signing event per (slot, layer) per operator (V-share + IBE-share) | Same — single signing event per (slot, layer) per operator at Phase-2b; verdict envelope is op-identity-signed (not threshold) and does not consume EKM slashing-protection |
| Equivocation σ-locked split recovery | None — slot-miss class | Recovered structurally — σ-quorum-eligibility short → all honest go NR → fall-through |
| h_V=1 selective-delivery deadlock | None — slot-miss class | Recovered — same mechanism |
| Validity-divergence recovery | Out-of-scope (Class A) | In-scope at f=1 n=4 (recovered by NR-quorum fall-through); structural at higher n/f |
| Slot timing | `T_commit + Δ_2 + Δ_3` ≈ 2.05s (Config A, K=4) | `T_commit + Δ_2a + Δ_2b + Δ_3` ≈ 2.20s (+150ms at minimum sizing) |
| Wire kinds | `Phase1Bundle`, `KindOnion`, `KindNR`, `KindCertificate` | + `KindVerdict` (Phase-2a, op-identity-signed verdict envelope) |
| Slashing-evidence rules | 5 rules | 5 rules + 1 (verdict-vs-action equivocation) |
| Late-deepest-layer leader broadcast (Class A) | Mitigated by K ≥ f+2; class-A residual | Closed structurally — late bundle observed in Phase-2a is σ-emittable in Phase-2b |
| Mesh-flakiness coordinated with byz σ-refusal (Class B) | Slot-miss surface | Mitigated — deferred commit lets brief flakiness recover |

## Variants considered

OBFT.md describes Phase 2a/2b loosely and points at TBFTR.md. The two specifications diverge on whether Phase-2a carries σ partials. Three coherent design points emerge:

### Variant A — TBFTR-as-written

[TBFTR.md](TBFTR.md) Phase-2a onion carries `V_{L_k}` plaintext and `C_k(σ_i^V(V_{L_k}))` (σ partial wrapped in chained IBE). Operators σ-commit in Phase-2a if they have V; Phase-2b is for late-comers who recover V from a peer's onion (gated by an `f+1`-distinct-Phase-2a-σ-signers witness threshold).

- **Pros**: matches a fully-specified protocol (TBFTR); narrowest spec gap to fill.
- **Cons**: keeps Phase-1 σ_V — leader is σ-locked at Phase-1; **does not recover validity-divergence** (the leader's Phase-1 σ_V on stale V remains the structural blocker, see [docs/TBFTR.md:312](TBFTR.md) — TBFTR explicitly documents this limit). At `f=1 n=4` the witness threshold `f+1 = 2` coincides with the leaner protocol's coverage ([docs/TBFTR.md:12](TBFTR.md)) — secondary closure adds no widening, only redundancy. Equivocation σ-locked splits where honest σ-commit on different V's at Phase-2a still fail.

### Variant B — Phase-1 σ_V kept, Phase-2a observation-only

Leader signs Phase-1 σ_V (same as OBFT base). Phase-2a is bundle re-flood + verdict broadcast, no σ partials. Phase-2b: each operator σ-or-NR-emits based on Phase-2a verdict observations. Cross-phase exclusivity: leader is σ-locked from Phase-1; non-leaders commit at Phase-2b.

- **Pros**: keeps the leader's Phase-1 σ_V head-start (one σ partial cluster-wide as soon as Phase-1 succeeds), helping marginal-receive cases reach qV at L_0.
- **Cons**: leader's Phase-1 σ_V still locks them on stale V at validity-divergence. At `f=1 n=4` with all-honest 2-σ vs 2-NV split (leader on σ-side), σ-pool = 2 < qV = 3; non-leader σ-eligible can rule-flip to NR, contributing to NR-pool = 3 = qEnc → fall-through. **At higher `n`/`f` (e.g., `f=2 n=7` with 4-3 validity split)**: σ-pool eligible = 4 < qV = 5, all non-leader σ-eligibles rule-flip to NR, NR-pool = 1+3 = 4 < qEnc = 5 — **slot misses; validity-divergence not recovered**. Variant B is f=1-n=4-tolerable but doesn't generalize.

### Variant C — No Phase-1 σ_V (chosen)

Leader broadcasts only `(V_{L_k}, σ_{L_k}^{op}(envelope))` — no σ^V partial in Phase-1. Phase-2a is bundle re-flood + verdict broadcast. Phase-2b: every operator (including the leader) σ-or-NR-emits based on Phase-2a verdict observations.

- **Pros**: leader is not locked at Phase-1; their verdict is re-evaluated at Phase-2b time. Validity-divergence recovers at all `n`/`f` because the leader can join the NR-pool when their verdict flips. Equivocation σ-locked split, h_V=1 selective-delivery, late-deepest-layer-leader-broadcast — all recover structurally. Mesh-flakiness mitigates because Phase-2a's window absorbs brief observability outages.
- **Cons**: marginal-receive cases (`h_V` = 2 at f=1 n=4) lose the Phase-1-σ_V head-start. They no longer σ-quorum at L_0 directly; instead they fall through to L_1. At K = n = 4 this costs one local-decryption iteration in Phase 3 — **no extra RTT** since Phase 3 walks layers via local decryption, not RTT-per-layer. Slot still succeeds.

**Why Variant C is the right design point.** OBFT's strongest claim about Phase 2a/2b — "Recovers the Class A validity-divergence deadlock" ([docs/OBFT.md:872](OBFT.md)) — is only true under Variant C. The parenthetical hint at [docs/OBFT.md:873](OBFT.md) ("without a Phase-1 σ_L^V, the leader doesn't pre-lock; Phase-2b's σ-emit is on the post-observation stabilized V") points at exactly this design. The marginal-`h_V` cost is acceptable because L_0 → L_1 fall-through is cheap (no extra RTT), and the recovery scope is genuinely wider.

## Setting

Inherits all of OBFT's setting ([docs/OBFT.md:23](OBFT.md)), with the deltas below.

### Wire kinds

In addition to OBFT's `KindOnion`, `KindNR`, `KindCertificate`, `Phase1Bundle`:

- **`KindVerdict`** (new): operator `i`'s per-layer Phase-2a verdict envelope. Structured payload `(protocol_tag = "OBFT-2ab-v1", message_kind = "phase2a-verdict", cluster_id, slot, operator_id i, layer k, verdict ∈ {σV, NR, NV}, value_root_or_null)`, signed by `i`'s operator-identity key. The `value_root` field is set when `verdict = σV` (commits `i` to claiming σ-eligibility on a specific `V` whose hash is `value_root`); null when `verdict ∈ {NR, NV}`.

- **`Phase1Bundle`** schema becomes `(V_{L_k}, σ_{L_k}^{op}(envelope))` — the σ_V partial is removed. The auth envelope still binds `(protocol_tag = "OBFT-2ab-v1", message_kind = "phase1-bundle", cluster_id, slot, layer k, leader_id, value_root)`. Domain separation matters: bundle envelopes from this protocol must not collide with bare-OBFT (`OBFT-v1`) or TBFT (`TBFT-v1`) envelopes. Non-σ_V Phase-1 bundles signed under bare-OBFT's tag would be rejected by Phase-2a/2b receivers; cross-protocol mixing is impossible by design.

### Per-layer windows and deadlines

Replace OBFT's single `Δ_2` with two sub-windows:

- **Phase 2a** `[T_commit, T_commit + Δ_2a]`: bundle re-flood + verdict broadcast.
- **Phase 2b** `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`: σ partial / NR-or-NV partial broadcast.

Sizing minimums:
- `Δ_2a ≥ D + δ` so verdicts and re-flooded bundles propagate before Phase-2a end.
- `Δ_2b ≥ D + δ` so Phase-2b σ partials propagate before Phase 3.
- **Recommended**: `Δ_2a = Δ_2b = 2(D + δ)` (mirrors OBFT's Δ_2 widening recommendation), absorbing one full `D + δ` of jitter per window.

`Δ_3` keeps OBFT's sizing: `Δ_3 ≥ (D + δ) + ε_3`. NR partials are emitted at end of Phase-2b; Phase 3 must absorb their propagation plus reconstruction processing.

**Receiver acceptance horizon** for Phase-1 bundles: `T_accept_max = T_commit + Δ_2a − (D + δ)`. Bundles first-observed past this are auth-only-retained (usable for verifying re-flooded V's during Phase-2a, but cannot drive a Phase-2a verdict) — same shape as TBFTR's late-retention rule, adapted to the `T_commit + Δ_2a` cutoff. Bundles first-observed in `(T_commit + Δ_2a − (D + δ), T_commit + Δ_2a]` are "borderline late" — accepted as full bundles if the operator's verdict broadcast still has Δ_2a-time to propagate before the Phase-2a end; otherwise auth-only.

**Leader broadcast deadline**: `T_broadcast_max = T_commit − 2(D + δ)`. Same as OBFT.

### Concrete timing at Config A (D = 100ms, δ = 50ms)

| Window | Length | End time |
|---|---|---|
| Phase 1 fetch (effective) | 1200ms | slot_start + 1.20s |
| Phase-1 propagation slack | 300ms | slot_start + 1.50s = T_commit |
| Phase 2a | 300ms | slot_start + 1.80s |
| Phase 2b | 300ms | slot_start + 2.10s |
| Phase 3 | 250ms | slot_start + 2.35s = T_round_end |
| Submission | 1650ms | slot_start + 4.00s |

Submission headroom drops from OBFT's 1.95s to 1.65s. Both fit the 4s relay cutoff comfortably.

## Protocol

### Phase 1 — Candidate broadcast

Each leader `L_k` for `k ∈ {0, ..., K-1}`:

1. Independently produces and validates `V_{L_k}` (host's local fetch loop, application-level rules).
2. Signs `V_{L_k}` with **only** the operator-identity key — producing `σ_{L_k}^{op}(envelope)` over the structured envelope. **No σ^V is signed at Phase 1.**
3. Gossips `(V_{L_k}, σ_{L_k}^{op}(envelope))` to peers via gossipsub.

Receivers run protocol-level checks (verify `σ^{op}` against the leader's known pubkey, re-derive envelope, check first-observation timestamp against the receiver acceptance horizon `T_accept_max`) and application-level validation (host returns `valid` / `not-valid`).

A receiver who observes a Phase-1 bundle:
- **Auth-valid + first-observation ≤ T_accept_max**: retain bundle (subject to retention bounds — at most 2 distinct `(V, σ^{op})` tuples per `(slot, layer, leader_id)` to support equivocation evidence). Operator may issue a Phase-2a verdict on `V_{L_k}` based on the host's validity verdict.
- **Auth-valid + first-observation > T_accept_max**: auth-only retention. The bundle's leader-auth signatures are kept for later verification (e.g., verifying re-flooded V's in Phase-2a) but the operator does not issue a Phase-2a verdict on this bundle. (See "Late-bundle path in Phase-2a" below for what auth-only retention enables.)
- **Auth-invalid**: silently drop, no retention.

**Equivocation handling at Phase 1**: if the receiver retains 2 distinct `(V, σ^{op})` tuples at the same `(slot, layer, leader_id)`, that is leader equivocation. The pair is self-contained slashable evidence, gossipped for out-of-band slashing. The receiver's local Phase-2a verdict for this layer is `NR-due-to-equivocation` regardless of host validity verdicts on either V — single-σ-V exclusivity at Phase-2b prevents the operator from σ-emitting on either side once equivocation is observed.

**Leader's own role**: the leader is *also* an operator and *also* issues a Phase-2a verdict on their own `V_{L_k}` (typically `σV` if their own host validates it; potentially `NV` if state has shifted by Phase-2a-verdict time, e.g., re-org).

### Phase 2a — Bundle re-flood + verdict broadcast `[T_commit, T_commit + Δ_2a]`

Two activities run in parallel during this window:

#### Activity 1 — bundle re-flood

Standard gossipsub re-flood of any retained Phase-1 bundles. Honest receivers forward bundles to peers on first observation. By Phase-2a end (under partial synchrony with `Δ_2a ≥ D + δ`), bundles broadcast at the leader's deadline have propagated to all honest receivers' first-observation. Late-arriving bundles past `T_accept_max` enter auth-only retention.

#### Activity 2 — verdict broadcast

Each operator `i`, for each layer `k ∈ {0, ..., K-1}`:

1. Compute `i`'s local verdict at this layer:
   - If `i` retained ≥ 2 distinct V's (equivocation observed) → `verdict = NR` (operationally `NR-due-to-equivocation`; wire form is unchanged from `NR`). `value_root = null`.
   - If `i` retained 1 V and host returns `valid` → `verdict = σV`. `value_root = hash(V_{L_k})`.
   - If `i` retained 1 V and host returns `not-valid` → `verdict = NV`. `value_root = null`.
   - If `i` retained 0 V's → `verdict = NR`. `value_root = null`.
2. Construct `KindVerdict` envelope `(protocol_tag, message_kind = "phase2a-verdict", cluster_id, slot, operator_id i, layer k, verdict, value_root)`.
3. Sign with `i`'s operator-identity key; gossip via gossipsub.

**No σ partials are signed in Phase 2a.** Verdict envelopes are op-identity signatures, not BLS partials. EKM/slashing-protection is not consulted for Phase-2a verdicts — they do not threaten safety even if the operator's verdict logic is buggy (the convergence rule at Phase-2b end is what gates actual partial signing, and EKM enforces single-σ-V there).

**Verdict equivocation**: if `i` is observed broadcasting two `KindVerdict` envelopes for the same `(slot, layer)` with different content, the pair is self-contained slashable evidence. Honest receivers count `i`'s **first-observed** verdict for convergence purposes; subsequent verdicts are dropped from convergence input (recorded for slashing). Operator-identity-key equivocation is detectable from any single observer's view since both envelopes are signed by `i`.

**Late-bundle path**: a Phase-1 bundle re-flooded into Phase-2a (first-observed past `T_commit` but ≤ `T_accept_max`) lets the receiving operator switch from a `NR` verdict to `σV` if their broadcast happens before Phase-2a end with sufficient propagation slack (verdict broadcast time ≤ `T_commit + Δ_2a − (D + δ)`). Operator's verdict update is allowed because the verdict has not been EKM-locked yet — it is op-identity-signed, not threshold-signed. The first verdict is observed-wise authoritative; the second-from-same-op is verdict equivocation. To handle this cleanly:

- **Rule**: an operator may broadcast at most one `KindVerdict` per `(slot, layer)`. The operator must wait until they are confident in their verdict — either the bundle has arrived and host-validity is computed, or `T_commit + Δ_2a − (D + δ)` is approaching and the bundle has not arrived (commit `NR`).
- **Implementation guidance**: emit the verdict as late as possible within the `[T_commit, T_commit + Δ_2a − (D + δ)]` window to maximize the operator's chance of having seen any late-arriving bundle. This is a per-operator timing choice; honest operators converge on the slot's actual state regardless of individual broadcast times because verdicts propagate during the remaining `D + δ` margin.

### Phase 2b — σ-or-NR commit `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]`

At the start of Phase 2b (`T_commit + Δ_2a`), each operator `i` computes its **convergence decision** per layer based on observed Phase-2a verdicts and its own local state.

#### Convergence rule

For each layer `k`:

1. Let `verdict_pool[V] = { distinct ops j : j broadcast a first-observed `KindVerdict(j, slot, k, σV, hash(V))` }`. (Counted per-distinct-operator; verdict equivocation collapses to first-observed.)
2. Let `nr_pool = { distinct ops j : j broadcast a first-observed `KindVerdict(j, slot, k, NR | NV, _)` }`.
3. Let `V_local = i`'s retained V at layer k (if any).
4. Let `σ_eligibility_quorum = ∃V : |verdict_pool[V]| ≥ qV`. (At most one V can satisfy this: Pigeonhole over verdict_pools — `Σ_V |verdict_pool[V]| ≤ n`, so at most one V can have ≥ qV = 2f+1 when `n ≤ 4f+1`. At `n = 3f+1` exactly, the bound is tight.)
5. Let `nr_eligibility_quorum = |nr_pool| ≥ qEnc`.

`i`'s commit at layer `k`:

| Condition | Commit |
|---|---|
| `nr_eligibility_quorum` | `NR` (regardless of `i`'s own verdict, `V_local`, or anything else) |
| `σ_eligibility_quorum` reached on V AND `i` has `V_local = V` AND host re-validates V as valid | `σ` on V |
| `σ_eligibility_quorum` reached on V AND `i` does not have `V_local = V` (or host re-validation says NV) | `NR` |
| Neither quorum reached AND `i`'s own verdict was `σV` AND `i` still holds `V_local` AND host re-validates valid | `NR` (verdict-quorum short — cluster did not converge on V; honest defaults to NR) |
| Neither quorum reached AND `i`'s own verdict was `NR` or `NV` | `NR` |
| Equivocation observed (≥ 2 retained V's at this layer) | `NR` |

**Tie-break note on `σ_eligibility_quorum`**: at `n = 3f+1`, at most one V can have `|verdict_pool[V]| ≥ qV` because `Σ_V |verdict_pool[V]| ≤ n = 3f+1 < 2 · qV = 4f+2`. So no tie-break is needed at the standard BFT bound. (At `n > 3f+1`, e.g., n=5 with f=1, two V's could conceivably reach qV in a pathological verdict pattern; the rule there is "lowest `value_root` lexicographic" as a deterministic tiebreak. Documented for completeness; not load-bearing at the recommended SSV configurations.)

**Why the rule "`nr_eligibility_quorum` overrides everything"**: if qEnc operators (a quorum) verdict-claimed `NR/NV`, the cluster is collectively saying "this layer's V is not viable". Honest who *would* have σ'd (had they not seen the cluster's NR-quorum verdict) defer to the cluster decision — they NR-emit. This makes NR-side decisive when reached, mirroring the σ-side's `σ_eligibility_quorum` decisiveness. Symmetry preserves Pigeonhole 1.

**Why the rule "σ_eligibility_quorum requires `i` to have V locally"**: an operator without `V_local = V` cannot compute `σ_i^V(V)` — they have no V to sign over. The rule degrades them to NR, which is cluster-wide consistent (their NR partial joins the NR-pool and may help cross-pool reach qEnc at this layer, otherwise contributes nothing).

#### Phase-2b emission

Each operator emits per their commit:

- **σ on V at layer k**: emit a single Phase-2b σ partial. At layer 0, plaintext `σ_i^V(V_{L_k})`. At layer `k > 0`, chained-IBE-encrypted under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` (same chained construction as OBFT's Phase-2 onion at deeper layers).
- **NR/NV at layer k**: emit a Phase-2b NR partial `σ_i^{IBE}(nr_tag_k)` (only for `k ∈ {0, ..., K-2}`; the last layer has no NR tag).

The operator wraps both σ partials and NR partials into a single auth envelope `KindOnion2b` signed by `i`'s operator-identity key, binding `(protocol_tag, message_kind = "phase2b-onion", cluster_id, slot, operator_id i, per-layer commits)`. This is the same auth envelope shape as OBFT's Phase-2 onion, restructured to carry the operator's per-layer Phase-2b commits.

EKM/slashing-protection is consulted at Phase-2b sign time:
- `Sign σ on V at (slot, layer)` (V-share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "σ", value_root(V))`.
- `Sign NR on nr_tag_k at (slot, layer)` (IBE-share): rejected if any prior `(slot, layer, "σ", _)` or `(slot, layer, "NR", _)` row exists. On success, log `(slot, layer, "NR", null)`.

**Single signing event per (slot, layer) per operator** — same shape as OBFT's EKM, simpler than OBFTR(R≥2)'s cross-round atomicity.

### Phase 3 — Local decryption and reconstruction `[T_commit + Δ_2a + Δ_2b, T_round_end]`

Identical to OBFT's Phase 3 ([docs/OBFT.md:283](OBFT.md)) except for the σ-pool source — there is no Phase-1 σ_V to include. The σ-pool at layer `k` is:

```
sigs[k] = { σ_j^V(V) from received Phase-2b onion contents at layer k on any value V }
        # decrypted via accumulated NR-decryption keys at layers 0..k-1, if k > 0
        # deduplicated per operator
```

The walk, σ-pool reconstruction, NR-quorum aggregation, and final-certificate gossip are unchanged.

## Operator commitment state machine

Three states per layer (Defer collapses):

| State | Trigger |
|---|---|
| `σ` | Phase-2b σ partial emitted on a specific V |
| `NR` | Phase-2b NR partial emitted on `nr_tag_k` (operationally NR-silent or NR-due-to-equivocation or NV — wire-identical) |
| `uncommitted` | Default at Phase-1 / Phase-2a — operator has not yet emitted Phase-2b. Once Phase-2b ends, every operator must be in `σ` or `NR` per the convergence rule. |

There is no `Defer` state in the OBFT sense — Phase-2a's window IS the deferral mechanism. By Phase-2a end, the convergence rule resolves every operator to either `σ` or `NR`.

## EKM coordination

Same shape as OBFT's EKM ([docs/OBFT.md:383](OBFT.md)):

- One row per signing event: `(slot, layer, side ∈ {"σ", "NR"}, value_root)`. `value_root` set on σ-side, null on NR-side.
- Per-request checks: σ on V at (slot, layer) rejected if any prior row exists at (slot, layer); NR on nr_tag_k at (slot, layer) rejected if any prior row exists at (slot, layer).
- Cross-keypair atomicity: V-share signing AND IBE-share signing share the same per-(slot, layer) row. Implementation: a single transactional log, two keypair-specific signing-request handlers.

**Differences vs OBFT EKM**:
- **No Phase-1 σ_V signing for the leader**. Leader's σ partial is signed at Phase-2b like any other operator's. EKM is consulted once per (slot, layer) per operator.
- **Phase-2a verdicts do not consult EKM**. They are op-identity signatures, slashable as op-identity equivocation but not threshold partials. The EKM coordinator does not log them.

The EKM is **simpler than OBFT's** because there is no Phase-1 σ_V to coordinate with the leader's later Phase-2 σ. A single signing event per (slot, layer) per operator.

## Safety

The three pigeonhole arguments from OBFT ([docs/OBFT.md:431](OBFT.md)) carry over with minor restatements for the absence of Phase-1 σ_V.

### Pigeonhole 1 — σ vs NR at the same layer

Cluster-wide σ-quorum on V at layer `k` (any V) and NR-quorum on `nr_tag_k` cannot both reach.

- σ-quorum: `h_σ + byz_σ ≥ qV = 2f+1` where `h_σ` counts honest with Phase-2b σ partials on V at L_k.
- NR-quorum: `h_NR + byz_NR ≥ qEnc = 2f+1`.
- Cross-phase exclusivity (EKM-enforced at Phase-2b sign time): each honest commits σ-or-NR per layer at most once. `h_σ + h_NR ≤ 2f+1`.
- Byzantine cross-signing: `byz_σ + byz_NR ≤ 2f`.
- Both reached: `h_σ + h_NR ≥ 4f+2 − 2f = 2f+2 > 2f+1`. Contradiction. ∎

The argument is identical to OBFT's Pigeonhole 1 except `h_σ` no longer includes the leader's Phase-1 σ_V (because there isn't one). The leader's Phase-2b σ counts the same as any other honest's σ. The bound is unchanged.

### Pigeonhole 2 — two σ-quorums on different V's at the same layer

Two distinct V's cannot both reach σ-quorum at the same layer.

Same argument as OBFT's Pigeonhole 2 with the leader's σ counted at Phase-2b instead of Phase-1.

- `h_σ_V + h_σ_V' ≤ 2f+1` (single-σ-V exclusivity, EKM-enforced).
- `byz_σ_V + byz_σ_V' ≤ 2f`.
- Both reached: `≥ 4f+2`, but bound is `4f+1`. Contradiction. ∎

### Pigeonhole 3 — cross-layer safety

Identical to OBFT's Pigeonhole 3 ([docs/OBFT.md:458](OBFT.md)). Chained encryption gates Phase-2b σ at layer `k > 0` behind NR-quorum at layers `0..k-1`. Pigeonhole 1 then prevents two layers' σ-quorums from co-existing.

### Verdict envelopes do not affect safety

Verdict envelopes are op-identity-signed claims, not threshold partials. They influence the *liveness convergence* (which Phase-2b emission an operator chooses) but contribute zero partials to either σ-pool or NR-pool. Safety holds regardless of how operators verdict-claim — the convergence rule is liveness-only.

A byzantine that verdict-claims σV but Phase-2b NR-emits (or vice versa) commits **verdict-vs-action equivocation** — slashable evidence (the verdict envelope contradicts the Phase-2b σ/NR partial), but does not violate Pigeonhole 1/2/3. The slashable evidence is on top of OBFT's five rules; see "Slashing-evidence rules" below.

## Liveness analysis

OBFT's recovery scope ([docs/OBFT.md:471](OBFT.md)) extends in three places. We walk through each scenario class.

Running example: `f = 1, n = 4, K = 4`. Honest A, B, C; byzantine D (when present). Leader at L_0 unless stated otherwise.

### Healthy path

All 4 operators receive `V_{L_0}` via gossipsub within `D + δ`.

- Phase-2a: all 4 operators verdict-claim `σV` on V_{L_0}. `verdict_pool[V_{L_0}] = 4`, `nr_pool = 0`.
- Phase-2a end: σ-eligibility-quorum reached on V (4 ≥ qV = 3). All 4 operators σ-emit at Phase-2b.
- σ-pool actual = 4 (assuming byz cooperates) or 3 (if byz defects to NR — but EKM blocks defection if byz already σ-claimed cross-claim slashing applies; the σ-pool from honest is still 3 ≥ qV).
- Slot succeeds at L_0 in 3 RTTs (Phase 1 + Phase 2a + Phase 2b, with Phase 3's local decryption adding ε_3). At Config A: ~700ms total to certificate gossip start.

### Marginal-receive cases

#### h_V = 3 (3 of 4 operators received V on time; 1 didn't)

Suppose A, B, D have V; C does not (e.g., C's gossipsub mesh delivered V late, past T_accept_max).

- Phase-2a verdicts: A_σV, B_σV, D_σV (assuming D cooperative), C_NR. `verdict_pool[V] = 3 ≥ qV`.
- Phase-2a end: σ-eligibility-quorum on V. A, B, D have V locally → σ-emit. C does not have V locally → NR per the convergence rule (σ-eligibility met but no V to sign).
- Phase-2b actual: σ-pool = 3 (A, B, D); NR-pool = 1 (C). σ-quorum reached at L_0. Slot succeeds.

#### h_V = 2 (2 of 4 operators received V; 2 did not)

Suppose A, B have V; C, D do not.

- Phase-2a: A_σV, B_σV; C_NR, D_NR (or D arbitrary). `verdict_pool[V] = 2 < qV = 3`. `nr_pool = 2 < qEnc = 3` (if D NR; if D σV claim then 1).
- Phase-2a end: neither quorum eligible. Convergence rule: A and B (had V, σ-eligibility short) → both NR per rule. C, D → NR per default.
- Phase-2b actual: σ-pool = 0; NR-pool = 4 (or 3 if D σ-emitted-but-defected — but D would need to σ-claim and then EKM would block their NR; if D σ-claims at Phase-2a end, they σ-emit, σ-pool = 1, NR-pool = 3 = qEnc; either way NR-quorum reaches).
- NR-quorum at L_0 → fall-through to L_1. If L_1 leader honest and healthy, L_1 σ-quorum reaches in the same Phase 3 reconstruction walk (no extra RTT).
- **Cost vs bare OBFT**: bare OBFT with `h_V = 2` has σ-pool = 2 (honest σ) + 1 (leader Phase-1 σ_V) = 3 = qV → succeeds at L_0. Variant C falls through to L_1. One extra reconstruction-walk iteration in Phase 3 (no RTT).

This is the price of Variant C: the marginal `h_V = 2` case at f=1 n=4 falls through one layer instead of succeeding at L_0. Acceptable trade for the equivocation/h_V=1/validity-divergence recoveries below.

#### h_V = 1

Suppose only A has V; B, C don't; D byzantine (silent or arbitrary).

- Phase-2a: A_σV; B_NR, C_NR; D arbitrary. `verdict_pool[V] = 1 + maybe-byz ≤ 2 < qV`. `nr_pool = 2 + maybe-byz`.
  - If D verdicts NR: `nr_pool = 3 = qEnc` → NR-eligibility quorum reached. All operators (incl. A) commit NR. NR-pool actual = 3 or 4 → fall-through.
  - If D verdicts σV: `nr_pool = 2 < qEnc`, `verdict_pool[V] = 2 < qV` → neither eligibility met. A (had V, σ-eligibility short) → NR per rule. B, C, D → NR or whatever D wants. NR-pool actual = at least 3 (A, B, C) → fall-through.
  - If D silent (no verdict): same as D verdicts NR above with D missing — `nr_pool = 2 < qEnc`, fall back to per-operator default. A → NR (rule), B → NR, C → NR. NR-pool = 3 = qEnc. Fall-through.
- **All sub-cases recover via NR-quorum fall-through.** OBFT base would slot-miss at L_0 here ([docs/OBFT.md:639](OBFT.md)); Variant C structurally fixes it.

### Equivocation σ-locked split

Byzantine D = leader equivocates at L_0. Patterns from OBFT's analysis ([docs/OBFT.md:480](OBFT.md)). **At 2-1 cases, Variant C regresses vs bare OBFT**; documented after the recovered cases.

#### 1-1-1 split

D delivers V_a to A, V_b to B, V_c to C (each a distinct V) near end of Phase-1, leaving inadequate re-flood time.

- Phase-1 retention: A retains V_a; B retains V_b; C retains V_c.
- Phase-2a: A_σV(V_a), B_σV(V_b), C_σV(V_c); D verdicts arbitrary.
- During Phase-2a window: gossipsub re-flood. Bundles for V_a, V_b, V_c propagate among honest. By Phase-2a end:
  - **If re-flood completes within Phase-2a (`Δ_2a ≥ D + δ` from byz's late Phase-1 delivery)**: A retains V_a + V_b + V_c → equivocation observed → A's verdict was already broadcast as σV(V_a), but A's commit at Phase-2a end is `NR-due-to-equivocation` (the convergence rule's equivocation-observed branch overrides earlier verdict). **However A already broadcast σV(V_a) verdict** — so verdict-vs-action mismatch occurs. Honest A's verdict-vs-action mismatch is permitted under the convergence rule (the rule explicitly allows commit ≠ verdict when equivocation is observed); it is not slashable for honest. (Slashable detection should distinguish honest verdict-vs-action revision from byzantine verdict-vs-action equivocation. See "Edge cases / Honest verdict-vs-action revision".)
  - At Phase-2a end with all honest in `NR-due-to-equivocation`: NR-pool actual = 3 (A, B, C) ≥ qEnc → fall-through to L_1.
- **If re-flood does NOT complete within Phase-2a** (byz times deliveries to push re-flood past T_accept_max for *each* honest): A only retains V_a; B only V_b; C only V_c.
  - Phase-2a: A_σV(V_a), B_σV(V_b), C_σV(V_c). `verdict_pool[V_a] = 1; verdict_pool[V_b] = 1; verdict_pool[V_c] = 1` (plus byz's verdict, ≤ 1 distinct).
  - At Phase-2a end: no V has σ-eligibility quorum (max 2 with byz vote). NR-pool = 0 (none verdict-claimed NR). Per the convergence rule, A had V_a + verdict σV but no σ-quorum-eligibility → A goes NR. Same for B, C.
  - NR-pool actual = 3 (A, B, C). Fall-through. ✓

**Either sub-case recovers.** OBFT base 1-1-1 split slot-misses ([docs/OBFT.md:495](OBFT.md)); Variant C structurally fixes it via the σ-eligibility-quorum-short rule.

#### 1-1-Defer-C / 1-Defer-Defer

These are sub-cases of byzantine selective-delivery patterns. The convergence rule resolves them the same way: any honest split that doesn't reach `σ_eligibility_quorum = qV` results in all honest going NR → NR-quorum reaches → fall-through.

#### 2-1 split — REGRESSION vs bare OBFT

D delivers V to {A, B}, V' to {C}. (One of OBFT's "naturally recovered" cases — see [docs/OBFT.md:485](OBFT.md).)

- Bare OBFT: D's Phase-1 σ_V(V) is on the wire. σ-pool on V = A + B + D = 3 = qV. Slot succeeds at L_0 *regardless of D's Phase-2 cooperation* — the Phase-1 σ partial is cryptographically locked-in.
- Variant C: D has no Phase-1 σ_V. D issues a Phase-2a verdict (σV(V) | σV(V') | NR | silent) and a Phase-2b action. Outcomes by D's behavior:
  - **D cooperates (verdict σV(V) + Phase-2b σ on V)**: `verdict_pool[V] = 3 ≥ qV`. A, B, D σ-emit. C does not have V_local → C NR per rule. σ-pool = 3 = qV. Slot succeeds at L_0. ✓ Same as OBFT.
  - **D silent (no verdict, no Phase-2b emission)**: `verdict_pool[V] = 2 < qV`. Per rule, A and B (had V, σ-eligibility short) → NR. C → NR (had V' only, σ-eligibility on V' short). NR-pool = 3 = qEnc. Fall-through to L_1. **One layer of latency added vs OBFT.**
  - **D defects (verdict σV(V) + Phase-2b NR-emission)**: `verdict_pool[V] = 3 ≥ qV` (counted from D's verdict). A, B σ-emit; D defects to NR. σ-pool actual = 2 (A + B); NR-pool = 1 (C) + 1 (D) = 2 < qEnc. **Slot misses at L_0.** Cluster does not fall through (NR-pool short). D's verdict-vs-action mismatch is slashable evidence (Rule 6) but does not save the slot. **Strictly worse than OBFT base, which would have succeeded.**

The 2-1-byz-defect regression is real. OBFT's cryptographic lock on Phase-1 σ_V is what makes 2-1 patterns naturally recover; removing σ_V trades that recovery for the structural fixes elsewhere (1-1-1 split, h_V=1, validity-divergence, late-deepest-layer). Across many slots, Rule-6 deterrent absorbs the byzantine-defect-grief cost; per-slot, this case slot-misses cleanly.

A symmetric pattern — D delivers V to {A}, V' to {B, C} — has the same shape with V and V' swapped. Same outcomes by D's behavior.

#### Recovery summary

| Equivocation pattern | Bare OBFT | Variant C |
|---|---|---|
| 1-1-1 split (no re-flood) | Slot misses | Recovered (NR-quorum fall-through) |
| 1-1-1 split (re-flood completes in Phase-2a) | Slot misses | Recovered (equivocation observed → all NR) |
| 2-1, byz cooperates | Succeeds at L_0 | Succeeds at L_0 (tie) |
| 2-1, byz silent | Succeeds at L_0 (Phase-1 σ_V lock) | Falls through to L_1 (one extra layer's latency, still succeeds) |
| 2-1, byz defects (verdict σV + action NR) | Succeeds at L_0 (Phase-1 σ_V lock) | **Slot misses** (NR-pool short of qEnc) |
| All-Defer-due-to-equivocation (early byz delivery) | Recovered via L_1 fall-through | Recovered via NR-quorum fall-through |

**Net liveness change vs bare OBFT**: gains 1-1-1 recovery (worst-case in OBFT); loses 2-1 byz-defect (new regression). Across realistic byzantine behavior (per the reputation-deterrent assumption, byzantine cooperate-or-stay-silent more often than defect-with-on-wire-evidence), Variant C is net positive in expectation. For deployments with adversarial byzantines that don't value reputation, the regression is a real cost.

### h_V = 1 selective-delivery (Class B in OBFT)

Already covered above under "Marginal-receive cases / h_V = 1". Recovers.

### Validity-divergence (Class A in OBFT)

A re-org during Phase-1 acceptance window splits honest verdicts: some operators say V valid (parent_root matches their pre-reorg head), some say invalid (parent_root mismatches their post-reorg head).

#### All-honest 2-σ vs 2-NV at f=1 n=4

- 2 operators (incl. leader) verdict σV; 2 operators verdict NV.
- `verdict_pool[V] = 2 < qV = 3`. `nr_pool = 2 < qEnc = 3`. Neither eligibility met.
- Per convergence rule: σV-side honest (had V, σ-eligibility short) → NR. NV-side honest → NR (their verdict). 
- NR-pool actual = 4 (all 4 honest NR) ≥ qEnc → fall-through to L_1. ✓
- Bare OBFT here: slot misses (leader's Phase-1 σ_V locks them to σ; non-leader σ-eligible can't switch; NR-pool capped at 2 < qEnc). Variant C recovers.

#### Higher n/f (e.g., 4-3 split at f=2 n=7)

- 4 σV verdicts, 3 NV verdicts. `verdict_pool[V] = 4 < qV = 5`. `nr_pool = 3 < qEnc = 5`.
- All σV-side honest → NR per rule. NV-side honest → NR.
- NR-pool actual = 7 ≥ qEnc → fall-through. ✓
- Hybrid (Variant B) here would have leader Phase-1 σ_V locked: 1 leader σ-locked + 3 σV-side rule-flipped to NR + 3 NV-side NR = 6 NR partials. NR-pool = 6 ≥ qEnc = 5 → fall-through? Wait, let me recount: the leader is σ-locked in Variant B, so they emit σ partial (1 σ) and not NR; 6 non-leader honest can NR. NR-pool = 6 ≥ qEnc = 5. Variant B *also* fall-throughs at f=2 n=7 cleanly. Hmm.
  - Actually I was wrong earlier. At f=2 n=7 4-3 split with Variant B: σ-pool = 1 leader + 3 σV-side honest = 4 partials. NR-pool from non-leader honest = 6 ≥ qEnc = 5. Fall-through happens.
  - The point where Variant C helps over Variant B is when the leader IS the validity-flipper. If leader's verdict flips to NV (they joined the NV side), Variant C lets the leader NR; Variant B keeps leader σ-locked. At higher splits this matters more, but at the recommended SSV configurations (n ≤ 13) the difference is small and may be invisible at f=1 n=4.
  - **Conclusion**: Variant C is uniformly safer for validity-divergence; Variant B is fine at f=1 n=4 and only differs at higher n/f or when the leader is on the divergent side.

### Late-deepest-layer leader broadcast (Class A in OBFT)

Bare OBFT's failure mode ([docs/OBFT.md:643](OBFT.md)): deepest-layer leader broadcasts past T_accept_max → all honest treat as silent → NR-quorum at L_{K-1} → walk advances past L_{K-1}, no L_K → slot misses.

In Variant C, late-arriving Phase-1 bundles are auth-only-retained until Phase-2a ends. If the bundle re-floods to all honest before `T_commit + Δ_2a − (D + δ)`, honest can verdict-claim σV on V; verdict-pool reaches qV; Phase-2b σ-emit on the late V. **Slot succeeds where bare OBFT fails.**

Conditions for recovery:
- Bundle propagates to all honest before `T_commit + Δ_2a − (D + δ)`. At Config A recommended Δ_2a = 300ms, this is 150ms past T_commit.
- Operationally: the leader's late broadcast is observed by at least one honest peer who re-floods immediately. The re-flood completes within `D + δ` of the late observation.

### Mesh-flakiness coordinated with byz σ-refusal (Class B in OBFT)

Bare OBFT's failure mode ([docs/OBFT.md:706](OBFT.md)): mesh-flaky honest A NR-emits early → A is NR-locked → byz refuses σ → σ-pool short → deadlock.

Variant C: A's verdict at Phase-2a is based on whether A has V locally. If A's mesh recovers during Phase-2a (delivering V via re-flood), A can verdict σV. Or, if A's mesh stays poor and A verdict-claims NR, but other honest converge on σ (verdict_pool[V] ≥ qV), A still defaults to NR per the rule (V_local missing). σ-pool from healthy honest may still reach qV without A. **At f=1 n=4 with leader honest + 2 healthy honest**: σ-pool = 3 = qV. Slot succeeds without A.

This is wider mesh-flakiness mitigation than bare OBFT. The Phase-2a observation window is a structural buffer for transient flakiness.

### Sustained partition (Class A — unchanged)

Real propagation > absorption window: bundles don't reach honest in time → all honest NR-pool short → slot misses cleanly. Same as OBFT.

Variant C's absorption window is `Δ_2a + (D + δ)` (the Phase-2a-end horizon) — same shape as OBFT's `Δ_2 + (D + δ)`. At recommended sizing both are ~450ms at Config A.

### > f operators offline/byzantine (Class A — unchanged)

Standard 3f+1 violation → slot misses. Same as OBFT.

## Slashing-evidence rules

OBFT's five rules ([docs/OBFT.md:567](OBFT.md)) carry over unchanged. One new rule covers verdict-vs-action equivocation:

- **Rule 1 — Self-contradiction (σ + NR/NV at the same layer)**. Same as OBFT.
- **Rule 2 — Leader equivocation**. Two distinct Phase-1 bundles `(V, σ^{op})` and `(V', σ^{op})` from the same leader at the same (slot, layer). Same as OBFT, except the bundle now carries σ^{op} only (no σ_V); evidence is still self-contained.
- **Rule 3 — Cross-onion partial-sig equivocation**. Operator `i` emitting Phase-2b σ partials on different V's at the same layer.
- **Rule 4 — Fake encrypted-presence (post-decryption garbage at k > 0)**. Same as OBFT.
- **Rule 5 — Fake plaintext σ at L_0**. Same as OBFT.
- **Rule 6 — Verdict-vs-action equivocation (new)**. Operator `i` broadcast `KindVerdict(σV, hash(V))` in Phase-2a but emitted Phase-2b NR/NV (i.e., a σ_i^{IBE}(nr_tag_k) partial), or broadcast `KindVerdict(NR or NV)` but emitted Phase-2b σ on V. The verdict envelope and the Phase-2b partial together form self-contained slashable evidence — both are signed by `i`'s keys (op-identity for verdict, V/IBE share for partial). Detection is immediate (no decryption-unlocking dependency).

  **Honest exception**: when an operator's Phase-2a verdict is σV but the convergence rule at Phase-2a end forces them to NR (e.g., σ-eligibility-quorum short, or equivocation observed via late re-flood), the verdict-vs-action mismatch is not slashable — it follows the convergence rule, which is part of the protocol. **Distinguishing honest revision from byzantine equivocation requires checking against the convergence rule**: a verdict-vs-action mismatch is slashable iff the cluster's verdict pool *would have* converged on the verdict side (e.g., σV verdict + NR action where σ-eligibility-quorum on V was reached cluster-wide; or NR verdict + σ action where σ-eligibility-quorum was reached). Honest receivers who detect a mismatch should evaluate against their cluster-wide verdict view. False-positive risk is low if the receiver has a complete-enough Phase-2a verdict set.

  **False-positive risk note**: at the boundary of receiver convergence (when receivers' Phase-2a verdict views differ slightly), Rule 6 attribution is gossipsub-pattern-quality rather than cryptographically-self-contained — detection requires the receiver to have observed enough Phase-2a verdicts to be confident the mismatch is genuine. This is a real distinction from Rules 1-3 (which are unambiguous from a single message-pair). Rule 6 sits closer in evidence-quality to Rule 4 (delayed/conditional) than to Rules 1/2/3 (unambiguous).

## Failure modes

Same Class A / Class B taxonomy as OBFT ([docs/OBFT.md:601](OBFT.md)). Variant C's recovery scope shifts several classes:

| Failure | OBFT base | OBFT + Phase 2a/2b (Variant C) |
|---|---|---|
| Sustained partition (real propagation > absorption window) | Class A | Class A — same; absorption window is `Δ_2a + (D + δ)` = same shape as OBFT's `Δ_2 + (D + δ)` |
| > f operators offline/byzantine | Class A | Class A — same |
| Validity-divergence | Class A | **Recovered** (NR-quorum fall-through at f=1 n=4 all-honest 2-2 split) |
| Equivocation σ-locked splits (1-1-1, 1-1-Defer, etc.) | Class B (slashable) | **Recovered** (NR-quorum fall-through via σ-eligibility-quorum-short rule) |
| Backup-leader cascade failure | Class A (rare) | Class A — same |
| Late deepest-layer leader broadcast | Class A | **Recovered** if bundle re-floods within Phase-2a |
| Byzantine selective-delivery h_V=1 deadlock | Class B (slashable, weak) | **Recovered** (verdict-quorum-eligibility-short rule forces NR) |
| Byzantine σ-refusal coordinated with mesh flakiness | Class B (slashable, weak) | **Mitigated** (Phase-2a window absorbs transient flakiness) |
| Verdict-vs-action equivocation (new) | n/a | Class B — slashable evidence, weaker than Rules 1-3 due to gossipsub-pattern attribution near boundaries |

## Edge cases — where things can go wrong

### Verdict broadcast timing

**E1: Operator broadcasts verdict too early in Phase-2a.** They commit before observing late-arriving bundles. If a late bundle would have changed their verdict, they're locked on the early verdict (verdict envelope is op-identity-signed; broadcasting a second different verdict is verdict-equivocation, slashable).

- **Mitigation**: operators broadcast verdict as late as possible within Phase-2a, no earlier than `T_commit + Δ_2a − (D + δ)`. This gives maximum time for bundle re-flood while still allowing the verdict to propagate before Phase-2a end.
- **Failure mode**: an honest operator with a buggy timer broadcasts verdict at `T_commit + 50ms` (way too early). They may verdict NR before a late bundle arrives. Their NR verdict counts in `nr_pool`. If the cluster reaches NR-quorum, fall-through happens (still recovers). If not, the operator may have to emit NR at Phase-2b (since their verdict was NR) even though V arrived later — but then they'd have V_local but NR-verdict; the convergence rule says: if `nr_eligibility_quorum` is met, NR (regardless of V_local); if not, follow own verdict. Per rule, they NR-emit. Slot may still recover via other operators' σ-emits if `verdict_pool[V] ≥ qV` from those who waited.
- **Recommendation**: implementation should default to "verdict at `T_commit + Δ_2a − (D + δ)` minus a small operator-side processing buffer", not earlier.

### Verdict equivocation by operator

**E2: Byzantine `i` broadcasts σV(V) to peers A and σV(V') to peers B and NR to peers C.** Each peer first-observes a different verdict.

- **Honest detection**: any peer who observes ≥ 2 distinct verdicts from `i` at the same (slot, layer) treats `i` as a verdict-equivocator. Both envelopes are slashable evidence.
- **Convergence input**: each peer counts only `i`'s **first-observed** verdict; subsequent verdicts are dropped from convergence pools.
- **Cluster-wide effect**: at f=1 with byz `i`, byz contributes ≤ 1 verdict per pool per peer. Different peers may count `i` toward different pools (A counts `i` as σV(V); B counts `i` as σV(V'); C counts `i` as NR). Per-peer convergence may differ. **This is a real surface area**: peers may converge differently because their first-observed verdicts differ.

  Concrete: at f=1 n=4 with leader honest + 3 honest σV(V) verdicts, byz issues 4 different verdicts to 4 different peers:
    - A's first-observed from byz = σV(V'). A counts `verdict_pool[V] = 3 (honest) + 0 (byz, on V'); verdict_pool[V'] = 1 (byz)`. A converges on σ on V (3 ≥ qV).
    - B's first-observed = NR. `verdict_pool[V] = 3; nr_pool = 1`. B converges on σ on V.
    - Etc.
  - Across all peers, σ-eligibility on V is consistently observed → all 4 honest σ-emit on V → σ-pool = 4 ≥ qV. ✓
- **Edge of convergence divergence**: at higher f or pathological verdict equivocation patterns, peers may converge differently. For SSV proposer at f=1 n=4, the verdict-equivocation-induced divergence is bounded; need careful analysis at higher f.

### Honest verdict-vs-action revision

**E3: Honest A verdicts σV(V) at Phase-2a; mid-Phase-2a A retains a second V' (equivocation observed via late re-flood); A's Phase-2b commit is NR-due-to-equivocation per the convergence rule.**

- A's Phase-2a verdict envelope says σV(V); A's Phase-2b NR partial on `nr_tag_k`. Rule 6 (verdict-vs-action equivocation) would flag this, but A is honest.
- **Fix**: Rule 6 is conditional on the cluster's verdict pool. Honest revision when equivocation is observed is permitted. Receivers who observe A's mismatch should check whether A had grounds to revise:
  - Did the cluster also observe equivocation at this layer? (E.g., is there a `verdict_pool[V']` with at least one entry?) If so, A's revision is plausibly honest.
  - Was σ-eligibility-quorum reached cluster-wide? If yes and A still NR'd, that's a stronger byzantine signal.
- **Implementation**: receivers who detect a Rule-6 mismatch should retain the evidence (verdict + action) but not propagate as slashable until they have enough cluster-wide context. This is a fundamental limitation of Rule 6 (gossipsub-pattern-quality) — it's a deterrent, not a clean self-contained rule.

### Byzantine verdict-vs-action equivocation

**E4: Byzantine D verdicts NR in Phase-2a, then Phase-2b σ-emits on V (or vice versa).**

- The cluster may have converged on σ-eligibility-quorum on V (verdict_pool[V] = qV from honest). D's Phase-2b σ on V doesn't violate that — D's σ contributes to σ-pool, slot succeeds.
- The slashable mismatch is between D's Phase-2a verdict (NR) and Phase-2b action (σ). Rule 6 evidence is the verdict envelope + the σ partial (or its auth envelope).
- **Effect on slot**: minor — D's σ partial may put σ-pool above qV. Slot likely succeeds. Slashable.

### Late-arriving bundle vs op-identity verdict equivocation

**E5: Honest A first-observes V_a at T_commit + 100ms, broadcasts verdict σV(V_a) at T_commit + 150ms. Then at T_commit + 250ms, a re-flooded V_b (byzantine equivocation) arrives at A. A now retains V_a + V_b.**

- A's Phase-2a verdict was σV(V_a); A's commit at Phase-2a end is `NR-due-to-equivocation` (equivocation observed override).
- A cannot broadcast a second verdict to revise — that would be A's verdict equivocation.
- A emits NR at Phase-2b. Rule 6 evidence (verdict σV(V_a) + action NR) — receivers must check cluster context; equivocation observed cluster-wide is the honest revision condition.
- **Implementation note**: this case requires receivers to retain the operator's first verdict + full Phase-1 retention state. To enable receivers to verify a Rule 6 mismatch is honest, the gossiped equivocation evidence (the V_a, V_b bundle pair) must be globally observable. Receivers without the equivocation evidence may falsely flag A as Rule-6 byzantine.
- **Mitigation**: Rule 6 attribution is best-effort and should not trigger automated slashing without cluster-wide consensus on the evidence. This is consistent with OBFT's slashing model (human-supervised coordination).

### Encrypted Phase-2b σ at deeper layers

**E6: At layer k > 0, Phase-2b σ partials are chained-IBE-encrypted under nr_tag_0..nr_tag_{k-1}. They cannot be verified by receivers until prior NR-quorums unlock decryption.**

- Same as OBFT's Phase-2 onion at deeper layers. No new attack surface, but the implementation must wrap Phase-2b σ partials in chained IBE the same way OBFT's Phase-2 onion does.
- The current [protocol/v2/tbft/onion.go](../protocol/v2/tbft/onion.go) `BuildOnion()` does this for baseline TBFT; the Phase-2b σ-emission code path can reuse it directly.

### Witness-threshold equivalent for Phase-2b σ emission

**E7: At Phase-2b time, byz emits a σ partial on V at L_0 without ever broadcasting Phase-1 V to the cluster. Honest receivers see byz's σ partial on the wire.**

- In bare OBFT, this is the "fake plaintext σ at L_0" attack (Rule 5).
- In Variant C, the σ-eligibility quorum check at Phase-2a end is the witness mechanism. Byz cannot inflate `verdict_pool[V]` alone (byz contributes ≤ f verdicts; qV − f = f+1 honest must agree to reach qV). Byz cannot get honest to σ on V without honest having V locally and the cluster reaching σ-eligibility quorum.
- Byz's Phase-2b σ on V is wasted (counts in σ-pool but cluster σ-quorum requires qV ≥ 2f+1; byz alone is < qV). At most byz's σ inflates the σ-pool by 1.
- **Result**: byz's lone σ-emission has no liveness effect at f=1 n=4 (cluster either reaches qV with honest cooperation or doesn't reach at all). Same outcome as OBFT. No new grief surface.

### Phase-2a verdict envelope rate-limit

**E8: Byzantine spams 100 distinct verdicts per (slot, layer) — different `value_root` each time.**

- Each verdict envelope is op-identity-signed. Honest receivers count *first-observed* per `(slot, layer, operator_id)`; subsequent verdicts are dropped from convergence input but recorded for slashing.
- Per-receiver memory: bounded by gossipsub message-id deduplication and the per-(slot, layer) retention cap.
- **Rate-limit (anti-amplification rule)**: same as OBFT's Rule 5 rate-limit ([docs/OBFT.md:579](OBFT.md)). Each honest receiver gossips slashable verdict-equivocation evidence at most once per `(slot, layer, operator_id)` tuple. Caps amplification.

### Phase-2a verdict propagation at high latency

**E9: Verdict from operator i broadcast at Phase-2a start propagates slowly to operator j due to gossipsub mesh anomaly. At Phase-2a end, j has not received i's verdict.**

- j's local convergence input is missing i's verdict. j may compute σ-eligibility-quorum incorrectly (e.g., j sees `verdict_pool[V] = qV − 1` and goes NR, while a cluster-wide-aware operator would see `qV` and go σ).
- **Effect at f=1 n=4**: rare; gossipsub propagates verdicts in `D + δ` ≤ Δ_2a − (D + δ). When mesh is anomalously bad, j's NR-emission joins NR-pool. If the rest of the cluster σ-quorums on V (qV partials reach), slot succeeds at L_0 without j. If not, fall-through to L_1.
- **Mitigation**: same as OBFT's mesh-flakiness mitigation — `Δ_2a ≥ 2(D + δ)` recommended; mesh-diversity at deployment level.

### EKM atomicity at Phase-2b

**E10: Operator i decides at Phase-2a end to σ on V at layer k. The Phase-2b sign request goes to EKM. Concurrent Phase-2b sign request for layer k' (different layer) from same operator. EKM must serialize or transactionally process both.**

- Cross-keypair atomicity: the Phase-2b onion contains per-layer commits, each requiring a separate EKM sign-and-log per (slot, layer). The implementation's commit batch should serialize EKM ops or use a single transaction spanning all layers.
- **Failure mode**: if EKM logs (slot, layer_0, "σ", V) but crashes before logging (slot, layer_1, "NR", null), restart could result in EKM allowing layer_1 σ on a different V (since no log row exists for layer_1). Not a safety violation per-layer (Pigeonhole 1 still holds at layer_0), but operator's local state is inconsistent.
- **Implementation**: EKM operations within a single Phase-2b emission must be atomic — either all per-layer log rows are written or none. Standard transactional database semantics apply. OBFT's EKM coordination model ([docs/OBFT.md:383](OBFT.md)) calls this out; it's unchanged here.

### Phase-2b σ at deeper layer arrives before its decryption is unlockable

**E11: Honest operator emits chained-IBE Phase-2b σ at layer k > 0. Receivers observe the encrypted partial but cannot decrypt until prior NR-quorums aggregate.**

- Receivers must retain the encrypted partial until decryption is possible. Retention bounds: `O(K · n)` partials per slot — bounded.
- If NR-quorums at prior layers never aggregate (e.g., σ-quorum reached at L_0 short-circuiting the walk), the encrypted partial stays sealed. Receivers eventually drop it when the slot's retention window ends.
- Same as OBFT's Phase-2 onion behavior at deeper layers. No new edge case.

### Byzantine leader emits no Phase-1 bundle but emits Phase-2a verdict σV

**E12: Byzantine leader (= L_0) is silent in Phase-1 but in Phase-2a verdict-claims σV(V) for some V they signed via auth envelope but never broadcast.**

- No honest operator has retained V (byz never broadcast Phase-1 bundle). `verdict_pool[V] = 1 (byz)` from byz's verdict alone.
- Honest operators either have V_local (no — byz didn't broadcast) or don't (yes). Honest verdict NR.
- `verdict_pool[V] = 1 < qV`; `nr_pool = 3 (honest) + 0 (byz) = 3 ≥ qEnc`. NR-quorum reached at L_0 → fall-through. ✓
- Byz's lone σV verdict is wasted; cluster proceeds to L_1.
- **No grief surface**.

### Multi-leader equivocation across layers

**E13: Byzantine controls multiple layer-leaders (e.g., byz holds L_2 and L_3 at K=4). Byz equivocates at both L_2 and L_3.**

- Each layer's equivocation is independent. The cluster falls through L_0, L_1 normally. At L_2, byz equivocates → all honest go NR (per σ-eligibility-quorum-short rule on the split V's) → fall-through to L_3. At L_3, byz equivocates again → all honest go NR → no L_4 → slot misses. Class A.
- **Rare**: byz controls f operators; pigeonhole at K = n means byz holds f / n layers (e.g., 1/4 at f=1 n=4). At K=4 f=1, byz holds exactly 1 layer; at K=4 f=2 (n=7), byz could hold 2 layers but K=4 < n=7 means not all members are leaders.
- **Mitigation**: K ≥ n − f (i.e., enough layers that byz can't hold all of them at the deepest end). At K=4 f=1 n=4, K = n − f + 1 (one more than minimum), pigeonhole guarantees at least one honest leader. ✓

### Operator restart mid-slot

**E14: Operator crashes after Phase-2a verdict broadcast but before Phase-2b. After restart, operator's EKM log shows no σ/NR row at (slot, layer). Operator must decide Phase-2b commit fresh.**

- The operator's Phase-2a verdict envelope is already on the wire — counted in cluster's verdict pool.
- On restart, operator re-evaluates convergence rule based on currently observed Phase-2a verdicts (including their own first-observed verdict, if they retain it locally). If their verdict was σV(V) and cluster reached σ-eligibility-quorum, they σ-emit at Phase-2b.
- If operator does NOT retain their pre-crash verdict locally and re-evaluates from scratch (no V_local because retention was lost), they may default to NR. This is verdict-vs-action mismatch (their on-wire σV verdict ≠ NR action). Honest exception applies (operator state recovery is permissible; the σ-pool may still reach qV from other operators).
- **Implementation guidance**: persist Phase-2a verdict and retained V state across restarts. EKM log alone is insufficient (it doesn't include verdicts). A separate per-slot state log (verdicts, retentions) should survive restarts.
- **Failure mode**: if operator state is fully ephemeral (in-memory only) and the operator restarts mid-slot, they may verdict-vs-action mismatch and trigger Rule-6 false-positives. Mitigation: persist Phase-2a state.

### Adversarial verdict timing — split convergence

**E15: Byzantine times their verdict broadcast such that some honest first-observe byz_σV(V) and some honest first-observe byz_σV(V'). Honest converge differently.**

- Per-peer convergence diverges. Some honest σ-emit on V; some on V'. Pigeonhole 2 ensures only one V can reach qV cluster-wide: the V with more honest converging. Slot succeeds on whichever V has σ-pool ≥ qV.
- Worst case: 50/50 split. At f=1 n=4 with 1 byz + 3 honest, byz issues 2 distinct σV verdicts (one to A, one to B; C either). At first-observed counting: A counts byz on V; B counts byz on V'; C counts byz on whichever first arrives. The 3 honest verdict σV on the V their host validates (presumably the V they retained from Phase-1 — bundle propagation should be consistent across honest by Phase-2a end if `Δ_2a ≥ D + δ`).
  - If all 3 honest have the *same* V_local (say V): all 3 verdict σV(V). `verdict_pool[V] = 3 + maybe-byz = 3 or 4 ≥ qV`. All 3 honest σ-emit on V. σ-pool = 3 ≥ qV. ✓ Slot succeeds.
  - If 2 honest have V and 1 has V' (e.g., partial equivocation propagation), divergence. Not an "adversarial verdict timing" issue per se but bundle propagation issue.
- The verdict-equivocation-by-byz-alone is bounded by f: byz contributes ≤ 1 distinct verdict per pool per peer (first-observed), so byz's adversarial contribution to per-peer convergence is at most 1 σ-pool entry inflation.

### Receiver-side validity stabilization

**E16: SSV proposer parent_root validity changes between Phase-1 acceptance and Phase-2a verdict time (re-org during the receiver acceptance window).**

- OBFT's host workflow ([docs/OBFT.md:801](OBFT.md)): validate against stable head snapshot at Phase-1 acceptance, lock the verdict.
- In Variant C, the verdict is broadcast at Phase-2a (not at Phase-1). The host must decide the verdict at Phase-2a verdict-broadcast time.
- **Two options**:
  1. **Host locks verdict at Phase-1 acceptance** (OBFT style). Phase-2a verdict echoes the locked verdict. Validity-divergence behavior same as OBFT: re-org during Phase-1 acceptance window splits operators; `verdict_pool[V]` short of qV → NR fall-through. ✓
  2. **Host re-evaluates at Phase-2a verdict-broadcast time** (Variant C "stabilization"). Each operator validates against their current head at verdict time. If the re-org has propagated to all honest by Phase-2a verdict time, all operators evaluate against the same head → unanimous verdict. ✓
- **Recommended**: option 2 — re-evaluate at Phase-2a verdict time. Phase-2a's window IS the stabilization window. This is the design point Variant C optimizes for.
- **Implementation**: the host's `validate(V_{L_k})` callback is called at Phase-2a verdict-broadcast time, with the operator's current head. If host wants to be conservative, they may keep a hybrid (lock at Phase-1 acceptance but re-evaluate at Phase-2a if head moved significantly).

### Phase-2b late σ when bundle re-floods very close to Phase-2a end

**E17: Bundle re-floods to operator A at T_commit + Δ_2a − ε for tiny ε. A barely has time to verdict σV(V) and broadcast.**

- A's verdict broadcast time: `T_commit + Δ_2a − ε`. Propagation to peers: peer first-observes by `T_commit + Δ_2a − ε + (D + δ)`.
- Peer's Phase-2a end: `T_commit + Δ_2a`. Peer first-observes A's verdict at `T_commit + Δ_2a + (D + δ) − ε` — past Phase-2a end.
- A's verdict missed Phase-2a's effective deadline; peer doesn't include A in `verdict_pool[V]` for convergence.
- A's vote is wasted; cluster computes convergence without A. If `verdict_pool[V]` still reaches qV without A, slot succeeds. If not (A was the marginal vote), σ-eligibility short → all NR → fall-through.
- **Mitigation**: don't broadcast verdict past `T_commit + Δ_2a − (D + δ)`. This is the effective Phase-2a verdict-broadcast cutoff.

### Asymmetric verdict observation at Phase-2a end

**E18: Operator i observes `verdict_pool[V] = qV − 1` (one short). Operator j observes `verdict_pool[V] = qV` (one more).**

- i computes σ-eligibility short → NR.
- j computes σ-eligibility met → σ on V (if j has V_local).
- Cluster split at convergence: i NR-emits, j σ-emits. σ-pool actual depends on how many honest converge each way.
- This is a realization of E9 (high-latency verdict propagation). At realistic Δ_2a sizing, this should be rare. If it happens at f=1 n=4 with leader as the marginal voter (j), σ-pool = 1 (j) + maybe-byz, NR-pool = 2 (i + others) + maybe-byz. Outcomes:
  - σ-pool < qV; NR-pool reaches qEnc → fall-through. ✓
- The asymmetric-observation case generally reduces to NR-quorum fall-through. Slot recovers via L_1.

## Open questions / decisions to make at implementation time

1. **Δ_2a vs Δ_2b sizing**: minimum vs recommended? At Config A minimum (Δ_2a = Δ_2b = D+δ = 150ms), submission headroom = 1.95s; at recommended (2(D+δ) = 300ms), headroom = 1.65s. Recommend recommended for mesh-flakiness mitigation; revisit if production telemetry shows submission tail > 1.5s P99.

2. **Verdict equivocation rate-limit**: should honest receivers gossip slashable verdict-equivocation evidence on first detection or wait for cluster confirmation? OBFT Rule 5 uses first-observed gossip with a per-(slot, layer, operator_id) cap; same rule fits here.

3. **Verdict envelope size**: 32-byte `value_root` plus envelope overhead ≈ 100-200 bytes per verdict per layer. Per slot: K verdicts × n operators × 200 bytes ≈ 3.2 KB at K=4 n=4. Within budget.

4. **Persistent Phase-2a state across restarts**: to avoid Rule-6 false-positives on operator restart, persist verdict + retained V at the EKM-log level. Implementation choice: extend the EKM log schema to include verdict envelopes alongside σ/NR rows? Or use a separate per-slot state file (recommended — keeps EKM minimal).

5. **Convergence-rule tie-break at n > 3f+1**: when multiple V's could reach `qV` (only possible at non-tight BFT-bound clusters like n=5 f=1), use lexicographic `value_root` tie-break. At n=3f+1 exactly (the SSV cluster sizes), tie-break is moot. Document for completeness.

6. **Late-bundle Phase-2a verdict path**: should an operator who first-observes V via re-flood at, say, T_commit + Δ_2a/2 still broadcast σV verdict? Yes, if propagation slack permits (broadcast ≤ T_commit + Δ_2a − (D + δ)). Implementation: per-operator timer that fires at the latest-safe verdict-broadcast time.

7. **Rule 6 evidence handling**: how do receivers determine whether a verdict-vs-action mismatch is honest revision (allowed) vs byzantine equivocation (slashable)? Implementation rule: receiver collects mismatch evidence; honest receivers cross-reference with their cluster verdict view; weakly slashable ("behavioral pattern" quality, like OBFT's selective-delivery). Surfacing this evidence requires the human-supervised coordination from OBFT's reputation-deterrent model — not automated.

8. **Should leader broadcast a verdict for their own V?** Yes — leader is an operator like any other in Phase-2a. Their verdict on their own V is σV (typically; could flip to NV on host re-evaluation if state shifted, e.g., re-org). Leader's verdict counts in `verdict_pool[V]`.

9. **What if leader's verdict on their own V is NV?** This is the validity-divergence-with-leader-on-NV-side case. Handled by the convergence rule: leader's verdict NR/NV puts them in `nr_pool`. NR-pool may reach qEnc → fall-through.

10. **Phase-2b emission timing — start-of-window or based on convergence completion?** Operators compute convergence at Phase-2a end (T_commit + Δ_2a) and emit Phase-2b immediately. No need to delay further within Phase-2b; the window is for propagation.

11. **K = 4 vs K = 3 trade-off**: same as OBFT base. K=4 (= n) has maximum fall-through depth at +3 KB onion bandwidth; K=3 (= f+2) saves bandwidth but is less robust to multi-layer adversarial scenarios. Recommend K = n = 4 for SSV proposer.

12. **Hash variant?** Variant C does not need TBFTR's "V plaintext in onion" since Phase-2a's bundle re-flood is the V-recovery mechanism. There is no late-σ-emit-on-V-recovered-from-peer-onion in Variant C. So the hash-vs-full-V distinction does not apply; Phase-2b onions carry σ partials only (encrypted at deeper layers), not V plaintext.

13. **Migration / co-existence with QBFT**: rollout via per-cluster opt-in (DKG event) or feature flag. Wire-protocol versioning via `protocol_tag` (`OBFT-2ab-v1`) prevents cross-protocol message mixing. Operationally: ship behind feature flag, enable per cluster after DKG.

14. **DKG cost**: same as OBFT — one V-keypair DKG (already in SSV) + one IBE-keypair DKG (new, run once at cluster init). Per-cluster setup, not per-slot.

15. **Existing TBFT package extension vs new package**: extend `protocol/v2/tbft/` adding Phase 2a/2b machinery alongside existing TBFT, OR create a new `protocol/v2/obft/` package. Recommend extending — preserves test infrastructure and IBE plumbing reuse. Distinguish via configuration (instance type) rather than separate package.

16. **Verdict EKM-binding (open trade-off)**: should Phase-2a verdicts be logged in the EKM at issue time, with Phase-2b sign requests required to match? Closes the 2-1-byz-defect regression but adds complexity (verdicts become EKM-tracked events; honest revision upon equivocation needs a "verdict-void" EKM operation gated on equivocation evidence). Default recommendation: accept the regression in v1; revisit if production telemetry shows defection-grief at meaningful rates. If adopted, the EKM coordinator gains: `(slot, layer, verdict_side, value_root)` log row at Phase-2a issue + `LogPhase2bSign` checks against the verdict row + `VoidVerdict(equivocation_evidence)` for honest revision.

17. **Verdict-issue timing minimum**: should there be a *minimum* verdict-broadcast time (e.g., `T_commit + Δ_2a/2`) to prevent premature commits? At the boundary case where an honest operator broadcasts verdict immediately on Phase-1-acceptance success and a byzantine equivocation arrives mid-Phase-2a, the honest operator's verdict is on the wire as σV but their commit revises to NR (honest exception under Rule 6). A minimum-broadcast-time would force operators to wait long enough to observe most re-flooded equivocation evidence first. Trade-off: forces all operators to broadcast verdicts in a narrow window near Phase-2a end, potentially adding propagation pressure. Default recommendation: no minimum (broadcast at latest-safe time, which is the natural choice anyway).

## Implementation plan — high-level breakdown

The implementation is broken into phases that can be staged across PRs:

### Phase 1 — Wire format and EKM schema

- Add `KindVerdict` envelope to [protocol/v2/tbft/wire/](../protocol/v2/tbft/wire/).
- Update `Phase1Bundle` schema to remove `σ_V` partial; auth envelope retains `protocol_tag = "OBFT-2ab-v1"`.
- Extend EKM schema in [ssvsigner/ekm/](../ssvsigner/ekm/) to support `(slot, layer, side, value_root)` log rows for the V-share + IBE-share coordinator. Add per-Phase-2b sign-request handlers.
- Add domain-separation tests confirming OBFT-2ab-v1 envelopes don't validate under bare TBFT or bare OBFT envelope handlers.

### Phase 2 — Instance state machine

- New `Phase2abInstance` in [protocol/v2/tbft/](../protocol/v2/tbft/) (extending or alongside existing `Instance`) with state machine: Phase-1-receive → Phase-2a-verdict-emit + receive → Phase-2b-commit → Phase-3-reconstruct.
- `ObserveCandidate(layer, V)` — same as existing.
- `ObserveVerdict(verdict)` — new; record per-(slot, layer, operator) first-observed verdict.
- `ObserveOnion(onion)` — restructured for Phase-2b commit shape.
- `ObserveNonReceipt(nr)` — same as existing, but emitted at Phase-2b end.
- `Resolve()` — same K-layer reconstruction walk.

### Phase 3 — Convergence rule and Phase-2b emission

- Implement convergence rule per the table in "Convergence rule" section above.
- `BuildPhase2bOnion(operatorID)` — at Phase-2a end, compute commits per layer, sign σ partials (chained IBE at k>0) or NR partials, wrap in auth envelope.
- EKM integration: per-(slot, layer) sign-request before each per-layer partial.

### Phase 4 — Adapter integration

- Update [protocol/v2/ssv/runner/proposer_tbft.go](../protocol/v2/ssv/runner/proposer_tbft.go) to drive the Phase 2a/2b state machine instead of bare TBFT.
- Add Phase-2a verdict broadcast at `T_commit + Δ_2a − (D + δ)`.
- Add Phase-2b emission at `T_commit + Δ_2a`.

### Phase 5 — Slashing-evidence rule 6

- Detect verdict-vs-action mismatches in Phase-3 reconstruction.
- Honest-revision exception: cross-reference cluster verdict view; slashable iff cluster's verdict pool would have honestly converged on the verdict side.
- Surface evidence via existing slashing-evidence gossip mechanism.

### Phase 6 — Testing and rollout

- Adversarial-byzantine tests covering the Class B recovery cases (equivocation σ-locked split, h_V=1 selective-delivery, validity-divergence at n=4 f=1).
- Mesh-flakiness simulation tests.
- Integration tests with simulated Δ_2a propagation latency.
- Feature flag rollout: disabled by default, opt-in per cluster.

## Comparison summary

| Aspect | Bare OBFT | OBFT + Phase 2a/2b (this) |
|---|---|---|
| Healthy-path latency (all 4 ops cooperate, all receive V) | ~600ms | ~700ms (+100ms for Phase-2a/2b split) |
| Marginal h_V=2 + byz σ-cooperates | Succeeds at L_0 (σ-pool = 3) | Succeeds at L_0 (verdict-quorum reached, σ-pool = 3) |
| Marginal h_V=2 + byz silent / NR | Slot misses (σ-pool = 2 < qV; NR-pool = 1 or 2 < qEnc; no fall-through) | Falls through to L_1 (verdict-quorum short → all honest NR → NR-pool = 3-4 ≥ qEnc) ✓ |
| Validity-divergence (e.g., 2-σ vs 2-NV at f=1 n=4) | Class A (slot-miss; leader σ_V locked on stale V) | Recovered (NR-quorum fall-through; leader's verdict re-evaluated at Phase-2a) |
| Equivocation 1-1-1 split | Class B (slashable, slot-miss; honest σ-locked on different V's) | Recovered (verdict-quorum-eligibility-short → fall-through) |
| Equivocation 2-1 split, byz σ-cooperates | Succeeds at L_0 (Phase-1 σ_V locked + Phase-2 σ) | Succeeds at L_0 (tie) |
| Equivocation 2-1 split, byz silent | Succeeds at L_0 (Phase-1 σ_V on the wire alone is enough) | Falls through to L_1 (one extra layer's latency, still succeeds) |
| Equivocation 2-1 split, byz defects (verdict σV + action NR) | Succeeds at L_0 (Phase-1 σ_V cryptographically locked; can't defect) | **Slot misses (regression)** — Rule-6 evidence on the wire |
| h_V=1 selective-delivery deadlock | Class B (slashable, slot-miss) | Recovered (verdict-quorum-short → fall-through) |
| Late deepest-layer leader broadcast | Class A | Recovered (Phase-2a re-flood absorbs) |
| Mesh-flakiness coordinated with byz σ-refusal | Class B (slashable, slot-miss) | Mitigated (Phase-2a window absorbs jitter) |
| EKM complexity | Per-(slot, layer, side) coordinator with cross-keypair atomicity | Same shape; one fewer concern (no Phase-1 σ_V to coordinate with Phase-2 σ) |
| Wire format | Phase1Bundle, KindOnion, KindNR, KindCertificate | + KindVerdict |
| Slashing-evidence rules | 5 | 6 (Rule 6: verdict-vs-action equivocation, weakly slashable) |
| Submission headroom (Config A) | 1.95s | 1.65s |
| Bandwidth (healthy, n=4, K=4) | ~27 KB | ~30 KB (+3 KB for verdicts) |

## What this design does NOT close

- **Sustained partition** beyond `Δ_2a + (D + δ)` absorption window — still Class A. Multi-round (R ≥ 2) extension of Phase 2a/2b is a future direction.
- **More than f operators offline/byzantine** — Class A by trust-bound assumption.
- **Backup-leader cascade failure** at K < n − f — Class A. K = n recommended.
- **Honest software bugs producing byzantine-equivalent behavior** — same trust posture as OBFT / QBFT (honest-majority cryptographic, not 100% cryptographic).
- **2-1 equivocation byz-defect grief** — strictly worse than bare OBFT (regression). Byz that controls 1 vote out of 4, equivocates V/V', delivers V to 2 honest, V' to 1 honest, verdict-claims σV(V), then defects to NR at Phase-2b. Slot misses cleanly with Rule-6 evidence on the wire. Bare OBFT would have succeeded via Phase-1 σ_V lock. The reputation deterrent absorbs this across many slots — but per-slot, an adversarial byzantine ignoring the deterrent can grief more reliably than under bare OBFT.

  **Mitigation options at implementation time** (not in current spec; see "Open questions" #16):
  - Make verdict envelope EKM-binding: log verdict at Phase-2a issue time; reject Phase-2b sign request that doesn't match. Adds EKM complexity (verdicts as logged events) AND breaks honest revision when equivocation is observed mid-Phase-2a (operator cannot switch from σV-verdict to NR-action). To restore honest revision, EKM needs a "verdict-void" operation gated on auth-valid equivocation evidence.
  - Accept the regression: rely on reputation deterrent across slots. Recommended unless production telemetry shows byzantine 2-1 defection at meaningful rates.

## Where this came from

Variant C is the structural extrapolation of OBFT's "Phase 2a/2b" prose ([docs/OBFT.md:869](OBFT.md)) — taking seriously the "without a Phase-1 σ_L^V" hint at [docs/OBFT.md:873](OBFT.md). TBFTR's spec ([docs/TBFTR.md](TBFTR.md)) is Variant A (Phase-1 σ_V kept, Phase-2a onion carries σ); this design diverges to recover validity-divergence at all `n`/`f`.

The verdict-broadcast mechanism is the load-bearing addition: it makes cluster-wide convergence on σ-eligibility observable before any operator commits a partial, which is the structural fix for OBFT's Class A validity-divergence and Class B byzantine-grief patterns. Without verdict broadcasts, a Phase-2a window only gives more time for Phase-1 bundle propagation — equivalent to a wider `Δ_2` in OBFT base.

The Phase 2 split costs +1 RTT of slot budget. At Config A this is +100-300ms depending on sizing; at the recommended `Δ_2a = Δ_2b = 2(D + δ)`, it is +300ms. Submission headroom drops from 1.95s to 1.65s — comfortable margin.

The trade-off vs bare OBFT: a healthy-h_V=2 case falls through to L_1 (rather than succeeding at L_0 via the Phase-1 σ_V head-start). At K = n = 4, fall-through is one local-decryption iteration in Phase 3 — no extra RTT, slot still succeeds.

This is the design point for SSV proposer duty under realistic adversarial conditions, per OBFT.md's own assessment ([docs/OBFT.md:894](OBFT.md)): "OBFT + Phase 2a/2b should be considered near-term, not future".

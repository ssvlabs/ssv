# 2abOBFT — Redesign Plan (v4)

Working document for the protocol redesign discussed in conversation.

After this plan is reviewed and refined, the changes are applied to
[2abOBFT.md](2abOBFT.md). This document is the scratchpad — the spec is
the source of truth post-rewrite.

The design has evolved across several iterations during the discussion:
- **v1** = current [2abOBFT.md](2abOBFT.md): verdict envelope at Phase 2a (op-id-signed, no partial), σ partials at Phase 2b (`KindOnion2b`, L_0 plaintext + chained-IBE for L_k>0). Hard wall at `T_commit` forces all honest to NR-default if no σ-quorum reaches.
- **v2** (intermediate, abandoned): σ partials moved into Phase 2a `KindSigned`. Removed RefloodDelay dependency but opened regressions on 1-1-1 equivocation, 2-1-byz-defect, and validity-divergence.
- **v3** (intermediate, abandoned): σ partials at Phase 2a but **encrypted under a new IBE tag `σ_final_tag_k`**, with a Phase 2a-late upgrade window + 5-row convergence rule + hard wall at `T_commit`. Restored v1's recovery surface at the cost of a new σ_final_tag_k IBE family + cross-share EKM coordination. Considered unnecessarily heavy after reviewing the existing v1 twoab implementation — the encryption was doing a job that simpler timing structure achieves naturally.
- **v4** (this plan): σ partials **plaintext at Phase 2b commit time** (matches v1's existing `KindOnion2b` structure for L_0; chained-IBE for L_k>0 unchanged from v1). Phase 2a as **pure coordination** (`KindValue` / `KindNoValue`), Phase 2a-late upgrade window (`KindNoValue` → `KindValue`), **no `T_commit` hard wall** — ops wait for natural trigger-based convergence under partial-synchrony, bounded only by the slot's relay-submission deadline (runner-level). Eliminates the non-uniform mesh-tail boundary that v3-with-hard-wall has. Aligns naturally with the existing v1 twoab implementation — migration is a focused refactor, not a from-scratch rewrite.

## Goals

1. **Make mesh-tail tolerance configurable via `SafetyBuffer`, not pre-allocated structurally**. OBFT bakes `RefloodDelay` into `B_0 = 2·BTT + RefloodDelay` — the gossipsub HeartbeatInterval is a *network-layer* constant that becomes a *protocol-layer* structural budget. v4 introduces `SafetyBuffer` as a *protocol-level* configurable (default = `RefloodDelay`) that deployments can tune independently of the gossipsub HeartbeatInterval. At default SafetyBuffer, v4 and OBFT have the same total post-broadcast structural budget; the v4 win on this axis is **separability of configuration**, not slot-budget reclamation. v4's `SafetyBuffer` is structurally placed in the post-Phase-2a cascade window (not in `B_k`) — see §Setting "Where SafetyBuffer lives in the timing budget" for the rationale, which differs from OBFT's `B_0` placement.
2. **Eliminate honest deadlocks** at every h_V_honest ∈ [0, 4] under partial-synchrony + assumption 3 (host validity unanimous at decision time). The non-uniform mesh-tail boundary that affects any hard-wall design (v1 2abOBFT, v3, AND bare OBFT — the (b) deadlock pattern) is closed by letting gossipsub IHAVE/IWANT do its job — no premature commit forces ops into a stale-view split.
3. **Faster fall-through in clean NR cases** (h_V_honest=0, byz-leader-silent). v1 / OBFT: NR-side ops wait for T_commit hard wall before emitting NR-side commits → fall-through at T_commit + Δ_2 ≈ 1.3s post-broadcast. v4: NR-eligibility trigger fires as soon as `novalue_pool ≥ qEnc` (~3·BTT post-broadcast under healthy mesh) → fall-through ~700ms faster.
4. **Honest framing of healthy-path latency**: in the **healthy h_V=4 success case**, bare OBFT (with early-commit) completes in ~2·BTT post-broadcast (~400 ms at Config A); v4 completes in ~3·BTT (~600 ms) due to the extra Phase 2a coordination step. **v4 is ~1·BTT slower than OBFT on the pure healthy path.** The v4 net win comes from mesh-tail robustness (Goal 2), faster clean-NR fall-through (Goal 3), 1-1-1 byz equivocation recovery, and configurable SafetyBuffer — not from healthy-path speedup or MEV-fetch reclamation.
5. **Bandwidth comparable to bare OBFT, slightly lighter** — v4 ≈ 8–9 KB cluster-wide at K=2 n=4; bare OBFT (per its own spec) ≈ 9–10 KB. The gap vs OBFT is small (~1 KB saved by v4 not carrying σ_L^V witnesses, since v4 has no Phase-1 σ). The 2abOBFT v1 spec's ~20 KB figure was inflated; the genuine bandwidth picture is "v4 ≈ OBFT − σ_L^V witnesses."
6. **Preserve safety** (Pigeonhole 1/2/3) and **leverage existing v1 implementation structure** — the migration is a focused refactor.
7. **Minimize cryptographic complexity** — keep the IBE tag family at `nr_tag_k` only (no σ_final_tag_k); keep the EKM coordination model unchanged from v1 (V-share single-σ-V + IBE-share single-nr_tag-per-layer, no new exclusivity rules).

Acceptable regressions vs v1 (per discussion):
- Phase-2 equivocation surface (renamed verdict equivocation): same shape as v1's verdict-equivocation, byz-induced, deterred via Assumption 4.

Residual boundaries:
- **Validity-divergence 2-σV vs 2-NV with assumption-3 violation**: cluster stalls at L_0 (no fall-through to L_1 since there's no premature NR commit at a hard wall). Slot misses at relay deadline. Inherited algebraic limit at f=1 n=4 (Pigeonhole 1). v3-with-hard-wall could attempt L_1 fall-through here, but v4 trades that for **non-uniform mesh-tail recovery** (judged a net positive given SSV's mesh-flakiness vs mid-slot re-org rates).
- **Real propagation > slot deadline (Class A)**: out of envelope by definition.

## Design decisions (agreed)

| # | Decision | Rationale |
|---|---|---|
| 1 | **K-generic spec** (`f+1 ≤ K ≤ n`; K=2 recommended default at SSV n=4) | Symmetry with v1; K-extensibility composes naturally |
| 2 | **Three wire kinds: `KindValue` / `KindNoValue` / `KindCommit`** | Phase 2a coordination: `KindValue` (has V_0 + host valid) / `KindNoValue` (otherwise) — both op-identity-signed, no threshold partials. `KindCommit` at Phase 2b: side flag distinguishes σ-side (with plaintext σ partial) / NR-side (with nr_tag partial) / NR-direct-at-Phase-2a (equivocation observed) |
| 3 | **σ partials plaintext at `KindCommit-Signed` time** (no encryption) | Matches v1's existing structure (L_0 σ partials plaintext in Onion2b). Decoupling σ-partial-emission from σ-direction-commitment is achieved by **emitting late** (at trigger time), not by encrypting. No σ_final_tag_k machinery needed |
| 4 | **Trigger-based dynamic emission** (not 5-row table) | Each op emits `KindCommit` when one of three triggers fires on the cluster-observed pool: (a) **σ-eligibility trigger**: `value_pool[V_k] ≥ qV` — op emits `KindCommit-Signed` if it has V_local + host re-validates valid, ELSE `KindCommit-NR` (this covers V-drops at value-quorum time AND host-flip pivots); (b) **NR-eligibility trigger**: `novalue_pool ≥ qEnc` AND op cannot σ at L_k (no V_local OR host re-validates as NV) → `KindCommit-NR`; (c) **equivocation trigger**: op retained ≥ 2 distinct V_k → `KindCommit-NR`. The σ-eligibility trigger is **fires-on-pool, side-on-state** — it fires uniformly once the cluster reaches value-quorum, but each op's emission side is decided per-op at commit time. Ops **wait** for triggers — no default emission. **Normative emission ordering** (per §Emission ordering): on each per-tick state delta, ops MUST process upgrade conditions before evaluating commit triggers |
| 5 | **No `T_commit` hard wall** (REMOVED from v3) | Ops wait until trigger fires naturally. Under partial-synchrony, gossipsub IHAVE/IWANT eventually delivers slow-edge messages (within ~RefloodDelay ≈ 700 ms), well within slot budget. The **slot's relay-submission deadline (runner-level)** is the only true hard wall. Closes the non-uniform mesh-tail boundary at the cost of validity-divergence-2-2 fall-through (assumption-3 violation; rare) |
| 6 | **Multi-layer Phase 2a emission** carries all-K layers' state | Single Phase 2a emission per op covers all layers; L_0 has Value/NoValue/NR-direct; L_k>0 has σ-chained / NR-plaintext / empty |
| 7 | **L_k>0 σ entries: chained-IBE-encrypted partial only** (no V_k fulltext, no leader-auth-sig) | V_k has ~T_0_broadcast − T_k of propagation budget via Phase-1 broadcast; per-message duplication wastes bandwidth |
| 8 | **V_0 fulltext kept in `KindValue`** (for peer-reflood-V) | No 2-BTT propagation assumption; KindNoValue-path ops who missed the Phase 1 bundle can recover V_0 from any peer's `KindValue` |
| 9 | **Host validity re-check at commit time** | When the σ-eligibility trigger fires (value_pool reached qV), each op evaluates emission side locally. If host re-validates valid AND op has V_local → emit `KindCommit-Signed`. If host now NV (or op has no V_local) → emit `KindCommit-NR` instead. KindValue is coordination-only, so the KindValue → KindCommit-NR pivot doesn't equivocate any prior threshold partial — only the op-id-signed KindValue is on the wire from Phase 2a, and the op never committed to σ-direction cryptographically until KindCommit-Signed. This pivot is part of the **authorized Phase-2 emission pairs** (see §Authorized Phase-2 emission pairs below) |
| 10 | **Leader-auth-sig copied into non-leader `KindValue`** | Authenticates V_0 is the leader's V_0 (byz V-injection prevention) using the Phase 1 bundle's op-identity sig |
| 11 | **No fixed `T_commit` timing anchor** (REMOVED from v3) | The runner schedules `T_0_broadcast` and `T_phase_2a` from slot start; Phase 2b emissions are dynamic per trigger. The slot's relay submission cutoff (`T_relay_cutoff − T_submit`) is the only deadline — and it's runner-level, not protocol-level |
| 12 | **Protocol tag bumped: `2abOBFT`** | Wire incompatible with v1; domain separation against v1 instances |
| 13 | **Full rewrite of docs/2abOBFT.md** (no v1-vs-v4 comparison text) | The new spec IS the spec; cleanliness over compat-narrative |
| 14 | **Explicit Phase 2a-late upgrade window**: `KindNoValue`-path ops who later have V_0 + host valid emit a follow-up `KindValue` (the "upgrade KindValue"). The `KindNoValue` → `KindValue` transition from the same op is one of the authorized Phase-2 emission pairs and is explicitly NOT Rule 6a equivocation. Receivers tolerate either arrival order. The full set of authorized pairs is enumerated in §Authorized Phase-2 emission pairs below — Rule 6a / Rule 3 fire only on pairs NOT in that set | Decouples late σ-eligibility signaling from σ-direction-commitment; populates σ_eligibility view with late KindValues before triggers fire cluster-wide |
| 15 | **Implementation alignment with existing v1 twoab package** | v4 is intentionally close to v1's structural shape: verdict ≈ KindValue, Onion2b ≈ KindCommit, L_0 σ plaintext, L_k>0 σ chained-IBE under nr_tag_k. Migration is rename + simplify + add upgrade trigger + remove hard wall. No new IBE tag family. EKM coordination model unchanged |

### Open decisions / TBD

- **Spec name**: keep "2abOBFT" with internal version bump. Wire tag `2abOBFT`. Go package name TBD (likely `protocol/v2/obft/twoab` refactored in-place rather than a sibling package).
- **Slot deadline handling**: confirm the runner-level slot deadline → "Instance abandons slot" rather than "Instance emits default commit." The existing twoab Instance is a passive state machine driven by the runner; this should align naturally.
- **`ε_proc` treatment**: keep lumped into ε_3 (no longer a separate phase-window argument since commits are dynamic).
- **Equivocation rule fire timing**: confirm that "≥ 2 distinct V_0 observed" triggers `KindCommit-NR` emission immediately (Phase 2a if op was about to emit KindValue → emit NR-direct; Phase 2b if op had already emitted KindValue → pivot via KindCommit-NR).

## Protocol summary (v4)

### Phases

### Timing parameters

| Parameter | Type | Meaning | Default at SSV (Config A) |
|---|---|---|---|
| `T_soft_end` | Configurable | Soft convergence target — when the protocol is *expected* to have produced its output. **NOT enforced by the protocol** (no hard wall). Used by the runner to derive `T_0_broadcast`. | ~3.5s (close to but well inside `T_relay_cutoff − T_submit − ε_3`) |
| `SafetyBuffer` | Configurable | Mesh-tail tolerance budget. Time the protocol allows for IHAVE/IWANT recovery beyond healthy-path minimum. Independent of gossipsub's HeartbeatInterval (which is a network-layer constant). | `RefloodDelay = 700ms` |
| `ε_3` | Constant | Local reconstruction processing time (BLS aggregation + IBE walk + cert construction). | ~50ms |
| `T_relay_cutoff` | Slot-level | Slot's hard relay-submission deadline (runner-level; the only true hard wall). | `slot_start + 4.0s` |
| `T_submit` | Constant | Reserved time for relay submission. | ~100ms |

**Derived offsets:**

```
T_0_broadcast   = T_soft_end − 3·BTT − SafetyBuffer − ε_3      (L_0 leader broadcast)
T_phase_2a      = T_0_broadcast + 1·BTT                        (Phase 2a fire instant)
                = T_soft_end − 2·BTT − SafetyBuffer − ε_3
B_k_shallow     = (k+2)·BTT                                    (per-layer broadcast budget,
                                                                NO SafetyBuffer term)
fetchAt[k]      = T_0_broadcast − B_k_shallow                  (transitively shifted earlier
                                                                by SafetyBuffer via T_0_broadcast)
T_resolve       = T_phase_2a + 2·BTT + SafetyBuffer + ε_3      (scheduled Resolve sweep,
                                                                cascade window includes SB)
                = T_soft_end
```

**Where SafetyBuffer lives in the timing budget — CRITICAL FOR IMPLEMENTATION**: SafetyBuffer
structurally shifts `T_phase_2a` (and transitively `T_0_broadcast` and every `fetchAt[k]`)
earlier in the slot, AND widens the cascade-window deadline `T_resolve` by the same amount.
Net effect: the post-Phase-2a window between `T_phase_2a` and `T_resolve` grows from `2·BTT + ε_3`
(no SafetyBuffer) to `2·BTT + SafetyBuffer + ε_3`. SafetyBuffer does **NOT** live inside `B_k`
(the per-layer leader broadcast budget) — `B_k = (k+2)·BTT` stays at the structural minimum.

This differs from OBFT's `RefloodDelay`, which lives inside `B_0 = 2·BTT + RefloodDelay`.
The rationale for the different placement is the protocols' different critical paths after
bundle arrival: OBFT is one hop (early-commit fires immediately on L_0 retention + host valid,
then propagates), so structural mesh-tail tolerance belongs in `B_0` (giving the bundle more
time to reach everyone before commit fires). v4 is two hops (peer KindValue propagates →
σ-eligibility trigger fires Commit-Signed → that propagates), so structural mesh-tail
tolerance belongs in the cascade window where those two hops execute. A v4 implementation
that follows OBFT's `B_k_shallow = (k+2)·BTT + SafetyBuffer` placement misallocates the
budget to a window (pre-Phase-2a) that's not the actual mesh-tail bottleneck under degraded
networks — the IHAVE/IWANT recovery the SafetyBuffer was supposed to absorb happens during
the cascade, not the leader broadcast.

**Phase 3 completion timing**:
- Healthy h_V=4: completes at `T_0_broadcast + 3·BTT + ε_3` ≈ `T_soft_end − SafetyBuffer` (well before `T_soft_end`).
- Mesh-tail recovery: completes at `T_0_broadcast + 3·BTT + SafetyBuffer + ε_3` = `T_soft_end` (at or before).
- If recovery exceeds SafetyBuffer (e.g., propagation > 1 HeartbeatInterval): completion drifts past `T_soft_end` but slot still succeeds if before `T_relay_cutoff − T_submit`.

**Hard wall**: only `T_relay_cutoff − T_submit` is enforced (runner-level). The Instance is not aware of this; the runner abandons the slot if no certificate has been produced by then.

`SafetyBuffer` profile spectrum (deployment configurable):

| Profile | `SafetyBuffer` | Total post-L_0 budget | Mesh-tail tolerance |
|---|---|---|---|
| **Lean** (healthy-mesh-only) | `0` | 3·BTT ≈ 600ms | none — slot misses if any IHAVE/IWANT cycle needed |
| **Recommended default** (mesh-tail tolerant, SSV defaults) | `RefloodDelay = 700ms` | 3·BTT + RefloodDelay ≈ 1300ms | one IHAVE/IWANT cycle |
| **Loose** (high mesh-flakiness) | `RefloodDelay + 1·BTT` or wider | 3·BTT + RefloodDelay + 1·BTT ≈ 1500ms | one cycle + jitter tail |

The configurable `SafetyBuffer` separates the protocol's mesh-tolerance budget from the gossipsub network constant — a deployment can run with `SafetyBuffer = 0` (lean, MEV-fetch maximized, no mesh-tail tolerance) without changing the gossipsub HeartbeatInterval, or vice versa.

### Phases

1. **Phase 1 — Candidate broadcast**.
   - For each layer k ∈ [0, K-1], leader broadcasts `(V_k, leader-auth-sig)` at `T_k`.
   - Schedule: `T_{K-1} ≤ ... ≤ T_1 ≤ T_0` (asymmetric — deeper layers fetch earlier from deeper-confirmed parents).
   - `T_0 = T_0_broadcast` is derived from `T_soft_end − 3·BTT − SafetyBuffer − ε_3`. Under healthy operation V_0 propagates within 1·BTT.
2. **Phase 2a — Coordination broadcast at fire-instant `T_phase_2a = T_0_broadcast + 1·BTT`**.
   - Every operator emits exactly one of:
     - **`KindValue`**: op has V_0 from bundle + host valid at Phase 1. Carries:
       - V_0 fulltext
       - leader-auth-sig (non-leader; copied from Phase 1 bundle envelope)
       - L_k>0 entries: σ partial on V_k chained-IBE-encrypted under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` (k levels) OR plaintext NR partial on `nr_tag_k` (for k ∈ [0, K-2]) OR empty
     - **`KindCommit` NR-direct**: op observed L_0 leader equivocation (≥ 2 distinct V_0). Carries:
       - L_0 plaintext NR partial on `nr_tag_0`
       - L_k>0 entries as in `KindValue`
     - **`KindNoValue`**: op doesn't have V_0 (or host says NV at Phase 1). Carries:
       - L_k>0 entries as in `KindValue`
       - No L_0 payload
   - Phase 2a emissions are **op-identity-signed envelopes only** — they carry no L_0 threshold partials. The L_0 σ partial appears only in Phase 2b `KindCommit-Signed`.
3. **Phase 2a-late — `KindNoValue` → `KindValue` upgrade window `(T_phase_2a, slot_deadline)`**.
   - A `KindNoValue`-path op MAY emit an upgrade `KindValue` iff both:
     - **Op now has V_0** (either received via gossipsub reflood of a peer's `KindValue`/Phase-1-bundle, OR already had V_0 from Phase 1 bundle but was host-NV at Phase 1 and host has since re-validated valid).
     - **Host re-validates V_0 as valid** at the moment of upgrade emission.
   - The upgrade `KindValue` has wire shape identical to the Phase 2a `KindValue`. L_k>0 entries are identical to those in the op's earlier `KindNoValue`.
   - **`KindNoValue` → `KindValue` from the same op is one of the authorized Phase-2 emission pairs** (the full enumeration is in §Authorized Phase-2 emission pairs). Rule 6a / Rule 3 fire only on emission pairs NOT in that set.
   - **Receiver ordering tolerance**: the upgrade is recognized by *presence* of both `KindNoValue` and `KindValue` from the same op, regardless of arrival order at the receiver.
   - Op may emit the upgrade `KindValue` at any time after Phase 2a fire. **The upgrade MUST precede that op's own `KindCommit-Signed` emission** (so other ops' σ_eligibility view includes this op's σ-eligibility at trigger-firing time).
4. **Phase 2b — Dynamic commit emission, trigger-driven, no hard wall**.
   - Each operator who emitted `KindValue` (Phase 2a or upgrade) or `KindNoValue` (without upgrade) emits exactly one `KindCommit`, triggered by **one of**:
     - **2f+1 KindValue observed (cluster-wide, including upgrades)** AND op has V_0 + host re-validates valid → emit `KindCommit-Signed`. Carries plaintext σ partial on V_0 at L_0 + L_k>0 commit entries.
     - **2f+1 KindNoValue observed** AND op cannot σ (no V_0 OR host re-validates NV) → emit `KindCommit-NR`. Carries nr_tag_0 partial at L_0 + L_k>0 commit entries.
     - **Equivocation observed at L_0** (op retained ≥ 2 distinct V_0 via reflood) → emit `KindCommit-NR` (pivots from KindValue if op had emitted one; or emits as the first commit if op was on KindNoValue path).
   - Phase 2a `KindCommit-NR-direct` emitters do NOT emit a second `KindCommit`.
   - **Emission is dynamic and reactive** to observed state. The Instance is a passive state machine — it processes incoming messages and emits `KindCommit` as soon as a trigger condition is locally satisfied. There is **no protocol-level deadline** that forces emission.
   - **The slot's relay-submission deadline (`T_relay_cutoff − T_submit`) is the only hard wall.** It is a runner-level concern: if no certificate is produced by then, the runner abandons the slot. The Instance is not aware of this deadline.
5. **Phase 3 — Reconstruction (observer-on-arrival from Phase 2a onward)**.
   - Run on every state delta.
   - For each layer k in ascending order from 0:
     - **σ-pool[V_k] aggregation**:
       - At **L_0**: aggregate plaintext σ partials from `KindCommit-Signed` messages.
       - At **L_k>0**: aggregate chained-encrypted σ partials from `KindValue` / `KindNoValue` / `KindCommit-NR-direct` L_k entries. Decrypt via accumulated nr_tag_0..nr_tag_{k-1} keys (peel outermost-first).
       - If σ-pool[V_k] ≥ qV on some V_k, reconstruct V_k signature; output (k, V_k, S_k); halt.
     - **`nr_tag_k`-pool aggregation** (for k ∈ [0, K-2]): aggregate nr_tag partials from `KindCommit-NR-direct` (Phase 2a), `KindCommit-NR` (Phase 2b), and L_k NR-plaintext entries in Phase-2a emissions. If ≥ qEnc, derive nr_tag_k key, unlock L_{k+1} chained encryption; advance.
     - Else: walk halts; wait for next state delta or slot deadline.
   - **Healthy h_V=4 completion**: ~3·BTT post-L_0-broadcast. **Worst-case under mesh-tail recovery**: bounded by slot deadline (~3.85s post-slot-start at Config A).

### Key timing changes vs v1 and bare OBFT

At default SafetyBuffer (= RefloodDelay in both protocols for fair comparison):

| Aspect | v1 (current 2abOBFT) | bare OBFT (with early-commit) | v4 (this) |
|---|---|---|---|
| Phase 1 broadcast target for L_0 | `T_verdict_start − (2·BTT + RefloodDelay)` | `T_commit − B_0` where `B_0 = 2·BTT + RefloodDelay`; SafetyBuffer = 1·BTT + RefloodDelay (pre-allocated structurally) | `T_soft_end − 3·BTT − SafetyBuffer − ε_3`; SafetyBuffer = RefloodDelay (configurable knob) |
| Phase 2a structure | `Δ_2a = 1·BTT + ε_proc` (verdict broadcast window) | n/a (no Phase 2a in OBFT) | single fire-instant at `T_phase_2a = T_0_broadcast + 1·BTT` (coordination, no threshold partial) |
| Phase 2a-late upgrade window | n/a (v1 has no Defer state) | n/a (no Defer state) | Open-ended `(T_phase_2a, T_relay_cutoff − T_submit)`; opportunistic emission |
| Phase 2 commit | `Δ_2b = 1·BTT + ε_proc` (hard wall at T_commit) | KindCommit early-emit at `T_L_0_observed` (typically `T_0_broadcast + 1·BTT`); hard wall at T_commit | Dynamic per-trigger; **no hard wall** at the protocol level |
| **Healthy h_V=4 post-broadcast latency** | ~4·BTT + RefloodDelay ≈ 1.5s | **~2·BTT ≈ 400ms** (with early-emit) | ~3·BTT ≈ 600ms |
| **Total post-broadcast structural budget** (at default SafetyBuffer) | `B_0 + Δ_2 = 3·BTT + RefloodDelay` ≈ 1.3s | `B_0 + Δ_2 = 3·BTT + RefloodDelay` ≈ 1.3s | `3·BTT + SafetyBuffer = 3·BTT + RefloodDelay` ≈ 1.3s |
| **MEV-fetch headroom at same T_soft_end + default SafetyBuffer** | narrower (v1 has older timing model) | **same as v4** at OBFT-parity SafetyBuffer | **same as OBFT** at default SafetyBuffer |
| Fall-through to L_1 (h_V_honest=0 / byz silent leader) | T_commit + Δ_2 ≈ 1.3s post-broadcast | T_commit + Δ_2 ≈ 1.3s post-broadcast (NR-side waits for T_commit fallback) | ~3·BTT ≈ 600ms post-broadcast (NR-eligibility trigger fires as soon as novalue_pool reaches qEnc) |
| Non-uniform mesh-tail recovery | misses at T_commit if propagation > B_0 budget | misses at T_commit if propagation > B_0 budget AND peer-reflood-V doesn't complete in time (the (b) deadlock pattern) | recovers via natural IHAVE/IWANT within slot deadline ✓ |
| SafetyBuffer configurability | structural (RefloodDelay coupled to gossipsub HeartbeatInterval) | structural (same coupling) | **independently configurable** at the protocol level (decoupled from gossipsub HeartbeatInterval) |

**Key insight**: at default SafetyBuffer = RefloodDelay, v4 and bare OBFT have the **same total post-broadcast structural budget (3·BTT + RefloodDelay)** and the **same T_0_broadcast / MEV-fetch headroom**. The difference is in *what the protocol does with that budget*:

- OBFT spends ~2·BTT of it on healthy-path message exchange (faster) + ~1·BTT + RefloodDelay on mesh-tail mechanisms (peer-reflood-V + IHAVE/IWANT).
- v4 spends ~3·BTT on healthy-path message exchange (Phase 2a coordination step costs 1·BTT) + RefloodDelay on IHAVE/IWANT recovery.

**Both end at the same wall-clock time** under default SafetyBuffer. v4 is ~1·BTT slower healthy completion but **gains** the configurability + non-uniform-mesh-tail robustness + 1-1-1 equivocation recovery wins.

### Worked timing at Config A (BTT=200ms, n=4, K=2, SafetyBuffer=RefloodDelay=700ms)

```
Hard wall (runner-level):
  T_relay_cutoff           = slot_start + 4.0s
  T_submit                 = 100ms
  ε_3                      = 50ms

Configurable:
  SafetyBuffer             = 700ms   (default = RefloodDelay)
  T_soft_end               = slot_start + 3.5s   (configured; ≤ T_relay_cutoff − T_submit − ε_3 = 3.85s)

Derived:
  T_0_broadcast            = T_soft_end − 3·BTT − SafetyBuffer − ε_3
                           = 3.5 − 0.6 − 0.7 − 0.05 = 2.15s
  T_phase_2a               = T_0_broadcast + 1·BTT = 2.35s

Healthy h_V=4 completion  ≈ T_0_broadcast + 3·BTT + ε_3 = 2.15 + 0.6 + 0.05 = 2.80s
                            (well before T_soft_end = 3.5s; effectively T_soft_end − SafetyBuffer)
Worst-case mesh-tail      ≈ T_0_broadcast + 3·BTT + SafetyBuffer + ε_3 = 2.15 + 0.6 + 0.7 + 0.05 = 3.50s
                            (at T_soft_end)

Submission slack (healthy)     = 4.0 − 2.80 − 0.1 = 1.10s
Submission slack (worst-case)  = 4.0 − 3.50 − 0.1 = 0.40s
```

L_0 MEV-fetch headroom: **2.15s** at typical Config A anchor with default SafetyBuffer.

**At lean SafetyBuffer = 0** (no mesh-tail tolerance, healthy-mesh-only deployments):
- `T_0_broadcast = T_soft_end − 3·BTT − ε_3 = 3.5 − 0.65 = 2.85s`. MEV-fetch headroom **~2.85s**.
- Healthy completion at 2.85 + 0.65 = 3.50s. Cluster has no buffer for IHAVE/IWANT — any mesh-tail miss = slot miss.

**At loose SafetyBuffer = RefloodDelay + 1·BTT** (high mesh-flakiness):
- `T_0_broadcast = T_soft_end − 3·BTT − (RefloodDelay + 1·BTT) − ε_3 = 3.5 − 0.6 − 0.9 − 0.05 = 1.95s`. MEV-fetch headroom **~1.95s**.
- Cluster tolerates one IHAVE/IWANT cycle + jitter tail.

The slot's relay submission deadline (`T_relay_cutoff − T_submit ≈ 3.9s`) is the **only protocol-relevant hard wall**. Under partial-synchrony + healthy mesh, convergence completes well before `T_soft_end`; under mesh-tail (with default SafetyBuffer), convergence completes at or near `T_soft_end`. If real propagation exceeds `T_soft_end`, the slot may still succeed if it lands before `T_relay_cutoff − T_submit` — `T_soft_end` is soft, not enforced.

### Wire format

Refer to current [protocol/v2/obft/twoab/wire](../protocol/v2/obft/twoab/wire/).

Changes from v1:
- `ProtocolTag` set to `"2abOBFT"` + 9 NUL bytes (16 B). _(See §Execution plan / Confirmed decisions for the rationale: no `-vN` suffix since v4 is the canonical 2abOBFT going forward; the previous "v4 tag" was a planning artifact.)_
- `KindPhase1Bundle` unchanged (still `V + leader-auth-sig`).
- `KindVerdict` → restructured as **`KindValue`** + **`KindNoValue`**:
  - `KindValue` (0x02): V_0 fulltext + leader-auth-sig (non-leader) + L_k>0 entries. Emitted at Phase 2a fire OR as a Phase 2a-late upgrade.
  - `KindNoValue` (0x03): L_k>0 entries only; no L_0 payload.
  - Drops v1's verdict kind enum (`{σV, NV, NR, NR-due-to-equivocation}`) in favor of the binary Value/NoValue split. The NV path collapses into KindNoValue; equivocation collapses into KindCommit-NR-direct.
- `KindOnion2b` → renamed **`KindCommit`** (0x04) with side flag {Signed, NR, NR-direct}:
  - `KindCommit-Signed`: plaintext σ partial on V_0 at L_0 + L_k>0 commit entries.
  - `KindCommit-NR`: nr_tag_0 partial at L_0 + L_k>0 commit entries.
  - `KindCommit-NR-direct`: same shape as `KindCommit-NR` but emitted at Phase 2a (equivocation observed); carries L_k>0 entries directly (since op skips Phase 2a KindValue/NoValue emission).
- `KindCertificate` unchanged.

Inner kind bytes:
- 0x01: Phase1Bundle (unchanged)
- 0x02: Value
- 0x03: NoValue
- 0x04: Commit
- 0x05: Certificate

#### IBE tags per slot

- `nr_tag_k` for k ∈ [0, K-2]: unlocks L_{k+1} chained encryption when `nr_tag_k`-pool reaches qEnc.
- Total: **K-1 tags** per slot. **No σ_final_tag** (v3-specific, removed).

For K=2: 1 tag (`nr_tag_0`). For K=4: 3 tags.

This matches v1's IBE tag count exactly.

#### Chained-encryption structure at L_k>0

```
σ_partial(V_k)                                  # innermost; plaintext at L_0
  → E_{nr_tag_{k-1}}(...)                        # innermost wrap
  → E_{nr_tag_{k-2}}(...)
  → ...
  → E_{nr_tag_0}(...)                            # outermost; peels first when L_0 NR-quorum reaches
```

Decryption order: outermost (nr_tag_0) → nr_tag_1 → ... → nr_tag_{k-1} → innermost σ_partial.

At **L_0**: σ partial is plaintext (no chain — this matches v1's existing behavior).
At **L_1**: 1 level (encrypted under nr_tag_0).
At **L_k**: k levels.

Chain depth is **one less than v3 at every layer** (v3 added an outer σ_final_tag_k wrap that v4 doesn't need).

#### `KindValue` wire shape (Phase 2a fire OR Phase 2a-late upgrade)

```
[1]  version
[16] ProtocolTag
[1]  inner kind = 0x02
[32] ClusterID
[8]  OperatorID
[8]  Height
[4]  V_0 length
[V_0 bytes]
[1]  leader-auth-sig present flag (0x00 if op IS the leader, 0x01 otherwise)
if present:
  [4] leader-auth-sig length
  [leader-auth-sig bytes]
[4]  L_k entry count (= K − 1; for k ∈ [1, K-1])
for each k ∈ [1, K-1]:
       [1] L_k entry kind (0x00 = empty, 0x01 = σ-chained, 0x02 = NR-plaintext)
       if σ-chained:
         [4] chained ciphertext length
         [ciphertext bytes]
       if NR-plaintext (k < K-1):
         [4] NR partial length
         [NR partial bytes]
```

#### `KindNoValue` wire shape (Phase 2a)

```
[1]  version
[16] ProtocolTag
[1]  inner kind = 0x03
[32] ClusterID
[8]  OperatorID
[8]  Height
[4]  L_k entry count (= K − 1)
for each k ∈ [1, K-1]:
       [1] L_k entry kind
       ... (same as KindValue's L_k entries)
```

No L_0 payload — L_0 commitment deferred (op was a V-drop or host-NV at Phase 1).

#### `KindCommit` wire shape (Phase 2a NR-direct OR Phase 2b finalization)

```
[1]  version
[16] ProtocolTag
[1]  inner kind = 0x04
[32] ClusterID
[8]  OperatorID
[8]  Height
[1]  L_0 commit side flag (0x01 = Signed, 0x02 = NR, 0x03 = NR-direct-at-Phase-2a)
[4]  L_0 partial length
[L_0 partial bytes]                  # σ partial on V_0 (plaintext at L_0) OR nr_tag_0 partial
[4]  L_k commit count (K − 1 entries for k ∈ [1, K-1])
for each k ∈ [1, K-1]:
       [1] L_k commit side flag (0x01 = Signed; 0x02 = NR for k ∈ [0, K-2])
       [4] partial length
       [partial bytes]
[1]  L_k entries present flag (for Phase 2a NR-direct: 0x01 = present; for Phase 2b: 0x00 = absent)
if present:
  [4] L_k entry count
  ... (same L_k entries as KindValue)
```

L_k entries are present in `KindCommit` only at Phase 2a NR-direct (op's only Phase 2 emission). Phase 2b `KindCommit` carries only commitments, referencing earlier `KindValue`/`KindNoValue` for σ partials at L_k>0.

The L_0 σ partial is **plaintext** in `KindCommit-Signed` — matches v1's `Onion2b` L_0 behavior.

#### Sizes at K=2

- `KindValue`, non-leader: ~17 B header + 32 B cluster + 16 B IDs/height + 4 B V_0 length + ~1 KB V_0 + 1 B auth-sig flag + 4+96 B leader-auth-sig + 4 B L_k count + 1 B L_1 kind + 4+~150 B L_1 chained-encrypted σ ≈ **~1.3 KB**. (Lighter than v3's KindSigned by ~150 B — no encrypted σ partial at L_0.)
- `KindValue`, leader: ~96 B less ≈ **~1.2 KB**.
- `KindNoValue`: ~17 B header + 32 + 16 + 4 + 1 + 4+~150 B L_1 ≈ **~220 B**.
- `KindCommit` Phase-2b (Signed or NR): ~17 B header + 32 + 16 + 1 + 4+96 B L_0 partial + 4 + 1 + 4+96 B L_1 partial + 1 B no-L_k-entries flag ≈ **~270 B**.
- `KindCommit` Phase-2a NR-direct: ~17 B + 32 + 16 + 1 + 4+96 B L_0 NR + 4 + 1 + 4+96 B L_1 + 1 + 4 + 1 + 4+~150 B L_1 entry ≈ **~430 B**.

#### Bandwidth at K=2, n=4

Healthy slot (h_V=4: all 4 emit KindValue at Phase 2a + KindCommit-Signed at Phase 2b):
- Phase 1 bundles: 2 × ~1.1 KB = ~2.2 KB
- 4 KindValues: 1 × 1.2 KB + 3 × 1.3 KB = ~5.1 KB
- 4 KindCommits (Phase 2b Signed): 4 × 270 B = ~1.1 KB
- **Total: ~8.4 KB.**

Worst-case σ-recovery slot (h_V_honest=1: 1 leader KindValue + 2 V-drop honest emit KindNoValue + upgrade KindValue + 1 byz):
- Phase 1 bundles: ~2.2 KB
- 1 leader KindValue: ~1.2 KB
- 3 KindNoValues: 3 × 220 B = ~660 B
- 2 upgrade KindValues (Phase 2a-late): 2 × 1.3 KB = ~2.6 KB
- 4 KindCommits (Phase 2b Signed): 4 × 270 B = ~1.1 KB
- **Total: ~7.8 KB.** (Slightly less than healthy because KindNoValue is much smaller than KindValue; the 2 upgrade KindValues replace what would have been 3 additional Phase 2a KindValues in healthy.)

Fall-through slot (h_V=0, all-NoValue, fall through to L_1):
- Phase 1 bundles: ~2.2 KB
- 4 KindNoValues: 4 × 220 B = ~880 B
- 4 KindCommits (NR): 4 × 270 B = ~1.1 KB
- Total: **~4.2 KB**.

#### Comparison to bare OBFT bandwidth

Per [OBFT.md §Application](OBFT.md) (`per-op KindCommit ≈ 1.5–2 KB at K=2 n=4 default — ... cluster-wide ≈ 6–8 KB across 4 operators`), bare OBFT's healthy bandwidth is:

- Phase 1 bundles: 2 × ~1.2 KB = ~2.4 KB (OBFT bundles carry σ_L^V partial; slightly heavier than v4's).
- 4 KindCommits: 4 × ~1.5–2 KB = ~6–8 KB (each carries V_0 fulltext in σ-side L_0 onion entry + σ_L^V witness section + chained-encrypted onion + NR partials + auth envelope).
- Certificate: ~150 B.
- **Total: ~9–10 KB.**

**v4 vs OBFT bandwidth differs by ~1–2 KB** — driven mainly by v4 not carrying σ_L^V witness sections (~290 B/op × 4 ops ≈ 1.2 KB saved at K=2 n=4). Both protocols carry V_0 in two places (Phase 1 bundle + σ-side Phase-2 message); the gap is the witness overhead and small wire-envelope differences, not a 2-3× reduction.

The earlier claim of "v4 ~8 KB vs OBFT ~20 KB" in the 2abOBFT v1 spec was inflated and has been corrected here.

#### V_0-redundancy: timing is roughly equivalent

With OBFT's early-emit, a σ-side op fires `KindCommit` as soon as `T_L_0_observed` triggers (typically `T_0_broadcast + 1·BTT`, right after bundle propagation). So OBFT's second V_0 carrier reaches receivers at roughly the same time as v4's KindValue (both ~T_0_broadcast + 2·BTT after one propagation hop from emission). The V_0-redundancy timing is not a v4 win.

#### What v4 actually wins at the same SafetyBuffer

At default SafetyBuffer (= RefloodDelay), v4 and bare OBFT have the same total structural budget and same T_0_broadcast / MEV-fetch headroom (corrected from earlier framing). The genuine v4 wins are:

- **Mesh-tail robustness past the soft target**: under non-uniform mesh-tail where peer-reflood-V doesn't complete by T_commit, OBFT's hard wall forces ops into stale-view commits → (b) deadlock pattern → slot misses. v4 has no hard wall; ops wait for gossipsub IHAVE/IWANT recovery within the slot deadline → recovers at L_0.
- **Faster clean-NR fall-through**: in h_V_honest=0 (byz leader withholds; all V-drops), OBFT NR-side waits for T_commit fallback (≈ T_0_broadcast + B_0 ≈ 1.3s); v4's NR-eligibility trigger fires as soon as novalue_pool ≥ qEnc (~3·BTT = 600ms). v4 ~700ms faster on fall-through.
- **1-1-1 byz equivocation recovery**: closed in v4 via equivocation-trigger → L_1 fall-through; bare OBFT slot-misses (σ-locked split with no fall-through).
- **Configurable SafetyBuffer**: v4 decouples mesh-tolerance budget from gossipsub HeartbeatInterval. Deployments can choose Lean (SafetyBuffer = 0; max MEV-fetch, no mesh-tail tolerance) or Loose (SafetyBuffer = RefloodDelay + 1·BTT; max tolerance, narrower MEV-fetch) independently. OBFT couples mesh-tolerance to the network-layer HeartbeatInterval.

These wins come at the cost of **+1·BTT healthy-path latency vs OBFT** (3·BTT vs 2·BTT post-broadcast in the h_V=4 success case) — the extra Phase 2a coordination step costs ~1 RTT relative to OBFT's direct early-commit.

## Pool aggregation rules

v4's wire kinds form natural "strength chains" — stronger messages (later in the chain) carry more commitment information than weaker ones. Receivers infer pool membership from the **strongest observed message** per op: a stronger message implies the weaker ones existed even if not directly observed (e.g., from message loss on weaker-message-edges of the gossipsub mesh).

**The natural strength chains:**

σ-direction chain:
```
Phase 1 bundle (V_0 + leader-auth-sig)
  ↓ "this V_0 exists; from the leader"
KindValue (V_0 + leader-auth-sig + op X's op-id-signed σ-eligibility claim)
  ↓ "op X has V_0 + host valid + σ-eligible"
KindCommit-Signed (V-share σ partial on V_0)
  ↓ "op X committed σ-direction (EKM-locked)"
```

NR-direction chain (V-drop path):
```
KindNoValue (op X's op-id-signed claim of no V_0 / host NV)
  ↓ "op X is on NR-side path"
KindCommit-NR (IBE-share nr_tag_0 partial)
  ↓ "op X committed NR-direction (EKM-locked)"
```

NR-direction chain (equivocation-observed, standalone):
```
KindCommit-NR-direct (Phase 2a, IBE-share nr_tag_0 partial; equivocation observed)
```

Plus cross-chain authorized pivots (A1, A3, A4, A7 from §Authorized Phase-2 emission pairs).

### Inference rules

The pools are indexed by layer k. `value_pool[V_k]` / `novalue_pool[L_k]` are **claim pools** (used for trigger evaluation in §Trigger rules); `σ-pool[V_k]` / `nr_tag_k-pool` are **threshold pools** (used for reconstruction in Phase 3). For each observed message from op X, a receiver infers contributions per the two tables below — one for the L_0 commitment (carried by message kind) and one for L_k>0 entry contents (carried inside Phase-2a emissions per the wire format at [line 109](docs/2abOBFT-REDESIGN-PLAN.md:109)).

**L_0 contributions** (from message kind):

| Observed from X | value_pool[V_0] | novalue_pool[L_0] | σ-pool[V_0] (threshold) | nr_tag_0-pool (threshold) |
|---|---|---|---|---|
| `KindValue` on V_0 | ✓ X added | — | — | — |
| `KindNoValue` | — | ✓ X added (provisional; removed if later upgrade KindValue is observed from X per §Receiver-side robustness) | — | — |
| `KindCommit-Signed` on V_0 | **✓ X added (inferred — KindCommit-Signed implies KindValue existed)** | — | ✓ X added (plaintext σ partial extracted) | — |
| `KindCommit-NR` (Phase 2b) | unchanged (X may or may not have emitted KindValue — depends on whether KindValue from X has also been observed) | ✓ X added (NR-direction commitment) | — | ✓ X added (nr_tag_0 partial extracted) |
| `KindCommit-NR-direct` (Phase 2a) | unchanged (X did NOT emit KindValue per authorized pair A8) | ✓ X added | — | ✓ X added |

**L_k>0 entry contributions** (from L_k entries carried inside `KindValue` / `KindNoValue` / `KindCommit-NR-direct` per [§Wire format](#wire-format); each entry is one of: σ-chained, NR-plaintext, empty):

| L_k entry kind in Phase-2a emission from X | value_pool[V_k] (k>0) | novalue_pool[L_k] (k>0) | σ-pool[V_k] (threshold, after layer unlock) | nr_tag_k-pool (threshold) |
|---|---|---|---|---|
| σ-chained (chained-IBE-encrypted σ partial under nr_tag_0..nr_tag_{k-1}) | ✓ X added (op claims σ-direction at L_k) | — | ✓ X added once nr_tag_0..nr_tag_{k-1} keys derived and ciphertext decrypted | — |
| NR-plaintext (nr_tag_k partial; k ∈ [0, K-2]) | — | ✓ X added (op claims NR-direction at L_k) | — | ✓ X added (partial extracted) |
| empty | — | — | — | — |

Each op emits exactly one L_k entry per layer per (slot, op) — σ-XOR-NR EKM rule. The L_k>0 pool memberships for an op are consistent with their L_0 emission direction in conformant impls; receivers don't need to verify cross-layer consistency for safety (Pigeonhole holds per-layer independently).

**Key inferences**:
- `KindCommit-Signed` from X implies X also emitted `KindValue` (or an upgrade KindValue) — that's the only authorized path to σ-direction commitment at L_0 (per pair A2 / A6). Receivers add X to value_pool[V_0] from observing `KindCommit-Signed` alone, even if X's `KindValue` was lost in propagation.
- `KindCommit-NR` is more ambiguous — X may have arrived via the KindValue → KindCommit-NR pivot (A3 / A4 / A7) OR via the KindNoValue → KindCommit-NR path (A5). Receivers add X to novalue_pool[L_0] unconditionally; value_pool[V_0] membership depends on whether X's KindValue has been observed independently.
- σ-pool[V_k] for k>0 only becomes meaningful once `nr_tag_0..nr_tag_{k-1}` keys are all derived (qEnc reached at each earlier layer). Until then, chained-encrypted L_k σ entries are uninterpretable and just sit in the receiver's mcache.

### Receiver-side robustness properties

**Monotonicity**: pools only grow. Receivers never remove an op from a pool except for the one explicit case (KindNoValue → KindValue authorized upgrade, where the KindNoValue contribution to novalue_pool is replaced by the KindValue's contribution to value_pool).

**Arrival-order tolerance**: receivers tolerate any arrival order of messages from the same op. Pool membership is the union of inferred contributions. If a stronger message arrives before a weaker one (e.g., gossipsub reorder), the receiver adds X to the relevant pools from the stronger message; the later weaker message just re-confirms (no incremental contribution).

**Retention**: receivers retain all observed messages for the full slot duration (gossipsub re-flood + slashing evidence). No active "discard the weaker message" semantics — both messages stay in the mcache and are re-flooded.

**Dual-pool membership** (authorized pivots): an op that emitted KindValue then KindCommit-NR (pivot A3/A4) appears in **both** value_pool[V_0] (from KindValue) AND novalue_pool[L_0] (from KindCommit-NR). This is intentional and doesn't break safety — Pigeonhole 1 applies to the threshold-partial pools (σ-pool[V_0] XOR nr_tag_0-pool, EKM-enforced), not to the claim-pools. The claim-pools (value_pool, novalue_pool) are signals for trigger evaluation; the threshold pools are for reconstruction.

## Trigger rules (replaces v3's 5-row convergence rule)

Definitions (from the op's local view; built from observed messages per the pool aggregation rules above):
- **V_local^k**: op's retained V_k at L_k (if op has it).
- **value_pool[V_k]**: distinct ops in value_pool per the inference rules.
- **novalue_pool[L_k]**: distinct ops in novalue_pool per the inference rules.

For each layer k, each operator MAY emit `KindCommit` when **any** of the following triggers fires locally. The triggers are **fires-on-pool, side-on-state** — they fire on cluster-pool conditions and dictate WHICH KindCommit side gets emitted based on op-local state at commit time.

| Trigger | Fires when | Emission |
|---|---|---|
| **σ-eligibility trigger** | `|value_pool[V_k]| ≥ qV` (for some V_k) — the cluster has reached σ-side eligibility on V_k | Op-local side decision: <br>• If op has V_local^k = V_k AND host re-validates valid → `KindCommit-Signed` (plaintext σ partial on V_k; chained-encrypted at L_k>0). If op is currently on KindNoValue path, the upgrade `KindValue` MUST be emitted first per §Emission ordering (A6 sequence). <br>• Else (no V_local, OR V_local ≠ V_k, OR host re-validates as NV) → `KindCommit-NR` with nr_tag_k partial. |
| **NR-eligibility trigger** | `|novalue_pool[L_k]| ≥ qEnc` (k < K-1) — the cluster has reached NR-side eligibility **AND op cannot σ at L_k** (no V_local^k OR host re-validates as NV) | `KindCommit-NR` with nr_tag_k partial |
| **Equivocation trigger** | Op retained ≥ 2 distinct V_k at L_k | `KindCommit-NR` (pivot from KindValue if applicable; or `KindCommit-NR-direct` if at Phase 2a) |

**Why σ-eligibility is fires-on-pool, side-on-state**: this is v1's row 3 + row 4 unified. Once the cluster reaches `value_pool[V_k] ≥ qV` (the σ-direction signal), every honest op decides their own side based on whether they can actually contribute a σ partial (V_local + host valid). Ops that can't (V-drop, host flipped, V_local mismatch) emit `KindCommit-NR` instead, contributing to nr_tag_k-pool — which enables fall-through to L_{k+1} when the cluster reaches σ-eligibility cluster-wide but most ops are NR-side locally (e.g., mid-slot host re-org affecting majority).

**Why NR-eligibility has the cannot-σ gate**: a σ-eligible op (V_local + host valid) that observes `novalue_pool ≥ qEnc` before its own σ-eligibility view crosses qV (e.g., its KindValue hasn't yet propagated to other V-drops, or V-drops haven't yet upgraded) must NOT emit `KindCommit-NR` on the NR-eligibility trigger. Doing so would (a) forfeit the op's σ contribution and (b) under realistic byz withholding at h_V_honest=1, break the L_0 σ-quorum that the upgrade path would otherwise complete. Concretely: at h_V_honest=1 with 1 byz V-drop refusing to cooperate (silent at Phase 2b), if the σ-eligible leader L emits NR via un-gated NR-eligibility, σ-pool[V_0] reaches only `{2 honest V-drops who upgraded} = 2 < qV`, and the slot misses despite the cluster having sufficient honest σ-side capacity. The gate routes σ-eligible ops to wait for σ-eligibility (which fires once V-drops upgrade), preserving the worked case at [line 752](docs/2abOBFT-REDESIGN-PLAN.md:752). The gate also lets receivers stay sequence-only on Rule 6a (the cannot-σ self-policing isn't externally verifiable, but honest impls follow it; see §evidence.go).

**Trigger priority** (if multiple fire simultaneously on the same state delta): equivocation trigger > σ-eligibility trigger > NR-eligibility trigger. Equivocation always routes to NR (cross-phase exclusivity); σ-eligibility comes second because once the cluster has agreed on σ-direction at L_k, that's the primary signal. NR-eligibility is the "no V at all" fallback.

**There is no default emission.** If no trigger fires for op X at layer k by the slot deadline, op X simply doesn't emit `KindCommit` at L_k. The cluster's outcome depends on other operators' emissions and natural convergence.

**Tie-break at n = 3f+1**: at most one V_k can have `|value_pool[V_k]| ≥ qV` cluster-wide because `Σ_V |pool[V]| ≤ n = 3f+1 < 2·qV = 4f+2`. No σ-side tie-break needed at SSV configurations.

**Upgrade trigger** (separate from KindCommit triggers): a `KindNoValue`-path op emits an upgrade `KindValue` when:
- Op now has V_0 (via Phase 1 reflood, OR via peer's `KindValue` reflood, OR was host-NV at Phase 1 and host has flipped to valid).
- Host re-validates V_0 as valid.

### Emission ordering (normative)

On each incoming-message state delta (i.e., each tick of the local state machine after applying received messages), an operator MUST evaluate emission opportunities in the following order:

1. **Upgrade check first**: if **(a)** op's only Phase-2 emission so far is `KindNoValue` (no `KindCommit` of any side has been emitted yet) AND **(b)** op now has V_0 AND **(c)** host re-validates V_0 as valid → emit upgrade `KindValue` (per A1). This emission MUST be delivered to gossipsub before step 2 executes. Once an op has emitted `KindCommit-NR` (committed NR-side), the upgrade is no longer available — emitting a later `KindValue` would produce the slashable sequence `KindNoValue → KindCommit-NR → KindValue` (see §evidence.go).
2. **Commit trigger check second**: evaluate equivocation trigger, σ-eligibility trigger, NR-eligibility trigger (in that priority order). If any fires AND op has not yet emitted `KindCommit` AND the trigger's gate conditions are satisfied, emit the corresponding `KindCommit`.

**Why ordering is normative, not "opportunistic"**: without upgrade-first, the race at the tick when a V-drop X first receives both V_0 (via leader's `KindValue` / Phase 1 reflood) AND enough other `KindNoValue`s to push novalue_pool to qEnc would have X observe a fired commit trigger before X processes the upgrade opportunity. Two failure modes follow:

- **NR-eligibility fires first (no cannot-σ gate honored)**: X emits `KindCommit-NR`, EKM-locking NR-side. L_0 σ-quorum loses X's potential σ contribution. Combined with similar races at peer V-drops, h_V_honest=1 L_0 recovery fails under byz withholding.
- **σ-eligibility fires first (cluster value_pool ≥ qV from leader's KindValue + peers' upgrades) and X has V_local + host valid**: X emits `KindCommit-Signed` directly without first emitting upgrade `KindValue`. Emission history `KindNoValue → KindCommit-Signed` is NOT in A1-A8 (A6 requires the upgrade interpolation). Receivers may pool X correctly via inference, but X's emission is structurally non-conformant.

Upgrade-first ordering **structurally closes** the impl-conformance failure modes: the emission shape at each honest op is fully spec-determined by the trigger conditions + ordering, with zero residual on the protocol's per-op behavior.

At the cluster-level timing race, the gate + ordering **largely address** the slow-view recovery: every op's individual response is deterministic, so under partial-synchrony assumption 2 (V_0 reflood reaches enough V-drops within slot deadline) the cluster converges. The residual is the partial-synchrony assumption itself — if reflood doesn't deliver V_0 to enough ops in time, the slot misses cleanly without forcing a premature NR-default. This residual was already present in v1 / OBFT (assumption 2 is shared across the protocol family); v4 doesn't introduce a new failure mode here.

### Deepest layer L_{K-1}

No `nr_tag_{K-1}` exists. NR-eligibility trigger doesn't fire at deepest layer (no nr_tag_{K-1} to sign — also reflected in the trigger table's `k < K-1` constraint). σ-eligibility trigger applies, but its NR-side emission branch can't fire (no nr_tag to sign). If σ-eligibility trigger fires AND op has V_local + host valid → emit `KindCommit-Signed` at L_{K-1}. Else → no emission at L_{K-1}. If no σ-quorum at L_{K-1}: slot misses with no further fall-through.

## Authorized Phase-2 emission pairs

An honest op may emit multiple Phase-2 messages per (slot, layer) ONLY when the sequence matches one of the explicitly authorized patterns below. **Any pair NOT enumerated here is a Rule 6a (Phase-2 equivocation) or Rule 3 (cross-σ-V) violation.**

| # | Sequence (same op, same slot, L_0) | Fired by | Rationale |
|---|---|---|---|
| A1 | `KindNoValue` → `KindValue` | Phase 2a-late upgrade (op now has V_0 + host valid) | KindValue is op-id-signed coordination only; no threshold partial was emitted in KindNoValue. Upgrade adds V_0 to value_pool. |
| A2 | `KindValue` → `KindCommit-Signed` | σ-eligibility trigger + op has V_local + host valid at commit time | Normal σ-direction commit. |
| A3 | `KindValue` → `KindCommit-NR` (host-flip pivot) | σ-eligibility trigger + host re-validates NV at commit time | KindValue is coordination only; no σ partial was on the wire from Phase 2a. Op pivots to NR direction based on local host re-check. |
| A4 | `KindValue` → `KindCommit-NR` (equivocation pivot) | Equivocation trigger (op observed ≥ 2 distinct V_0 via reflood after emitting KindValue) | Op observed equivocation post-KindValue-emission. Cross-phase exclusivity forces NR. |
| A5 | `KindNoValue` → `KindCommit-NR` | NR-eligibility trigger (cannot-σ gate satisfied — no V_local OR host re-validates NV) OR σ-eligibility trigger (op-local side decision routes to NR because no V_local at commit time) | V-drop op (or NV-at-Phase-1 op) commits NR. |
| A6 | `KindNoValue` → `KindValue` → `KindCommit-Signed` | A1 then A2 | Defer-then-upgrade then σ-commit. |
| A7 | `KindNoValue` → `KindValue` → `KindCommit-NR` | A1 then A3/A4 | Upgrade then NR pivot (rare; host flips after upgrade, OR equivocation observed after upgrade). |
| A8 | `KindCommit-NR-direct` (only) | Phase 2a equivocation observed pre-emission | Op skips KindValue/KindNoValue entirely; emits direct NR. No second emission. |

**Slashable Phase-2 equivocation (Rule 6a)** fires when the sequence is NOT one of A1-A8. Concretely: two `KindValue`s on different V_0 (also Rule 3, cross-σ-V); `KindValue` followed by `KindNoValue` (downgrade is not authorized); `KindCommit-Signed` followed by `KindCommit-NR` (or vice versa); two `KindCommit-Signed` on different V_0; two `KindCommit-NR-direct`; `KindCommit-NR-direct` followed by any other emission; `KindNoValue` followed by `KindCommit-Signed` (missing the upgrade `KindValue` interpolation that A6 requires); `KindNoValue → KindCommit-NR → KindValue` (post-commit upgrade — op was EKM-locked NR by KindCommit-NR; the subsequent KindValue would re-claim σ-direction). See §evidence.go for the full sequence enumeration with classification.

**Receiver ordering tolerance**: receivers recognize the authorized sequence by the *presence* of the constituent messages from the same op, regardless of gossipsub arrival order. A `KindValue` + `KindNoValue` pair received in either order is interpreted as A1 (KindNoValue first, KindValue upgrade) provided the KindValue carries V_0 + leader-auth-sig (which it must, per wire format). Same for `KindValue` + `KindCommit-NR` (interpreted as A3 or A4, both authorized).

**Crisp restatement**: KindValue is coordination-only (no threshold partial); KindCommit carries the threshold partial. The "authorized pivot" rule is structurally simple: **once a threshold partial is on the wire (in KindCommit), the op is cryptographically committed; before that (only KindValue/KindNoValue on the wire), the op can change direction without equivocating any threshold partial.** Rule 6a fires on threshold-partial equivocation (different V or different side from the same op via KindCommit) OR on duplicate Phase-2a coordination from the same op (two KindValues on different V_0, etc.).

## Section-by-section changes to docs/2abOBFT.md

### Top matter (lines 1–10)

- Rewrite opening: 2abOBFT v4 is a redesign that removes the Phase-2a verdict envelope structure + RefloodDelay + T_commit hard wall, replacing them with `KindValue`/`KindNoValue` coordination + `KindCommit` with side flag + trigger-driven dynamic Phase 2b emission. σ partials live in `KindCommit-Signed` (plaintext at L_0, chained-IBE at L_k>0). The only deadline is the slot's relay-submission cutoff (runner-level).
- The "2ab" name retained for continuity but now refers to Phase 2a (coordination broadcast) + Phase 2b (binding commit). Both phases are dynamic / event-driven.

### When to use it (lines 9–22)

- Update "Suited for" / "Not suited for" buckets:
  - Suited for **clusters that want to tune mesh-tolerance independently of the gossipsub HeartbeatInterval** — v4's SafetyBuffer is a protocol-level configurable, decoupled from the network constant. Deployments that want max MEV-fetch can set SafetyBuffer to 0 (lean); deployments that want max robustness can set it to RefloodDelay + 1·BTT or wider, without touching the gossipsub layer.
  - Suited for **clusters with mesh-flakiness** — v4 recovers at L_0 under non-uniform mesh-tail via natural gossipsub IHAVE/IWANT (no hard wall forces stale-view commits).
  - Suited for **clusters concerned with 1-1-1 byz leader equivocation** (closed via equivocation-trigger; bare OBFT misses this case).
  - Suited for **clusters where clean-NR fall-through latency matters** (h_V_honest=0 / byz silent leader): v4's NR-eligibility trigger fires ~1·BTT after Phase 2a, ~700ms ahead of OBFT/v1's T_commit-gated fall-through.
  - **Not** suited for clusters where validity-divergence 2-σV vs 2-NV mid-slot is a primary concern AND L_1 commonly has a different parent — v4 stalls here (no fall-through to L_1). v1 (with hard wall) attempts L_1 fall-through.
  - **Not** suited for clusters that optimize for pure healthy-path latency above all else — v4's healthy path is ~3·BTT (vs bare OBFT's ~2·BTT with early-commit). The ~1·BTT extra cost is the Phase 2a coordination step.
- Update OBFT comparison: not a strict "v4 faster than OBFT in healthy mesh" win — bare OBFT with early-commit is ~1·BTT faster on the pure healthy path. **At default SafetyBuffer, v4 and OBFT have the same total structural budget and same MEV-fetch headroom.** v4's structural wins are in mesh-tail robustness (no hard wall), faster clean-NR fall-through, 1-1-1 equivocation recovery, and configurable SafetyBuffer.

### Assumptions and implications (lines 71–120)

- Assumption 2 (partial-synchrony for liveness): now bounded only by the slot's relay-submission deadline rather than a per-phase budget. Mesh-tail tolerance extends up to the slot deadline.
- Assumption 3 (host validity best-effort unanimous at decision time): restored at `KindCommit-Signed` emission time. Host-NV ops emit `KindCommit-NR` instead.
- Assumption 5 (EKM coordination): updated for v4's two signing events: V-share at `KindCommit-Signed` (σ partial), IBE-share at `KindCommit-NR` / `KindCommit-NR-direct` (nr_tag partial). Single-σ-V + σ-XOR-NR per layer — same as v1.

### Setting (lines 23–70)

- Drop `RefloodDelay` from formulas.
- Drop `Δ_2a`, `Δ_2b`, `T_commit` from deadlines.
- Add Phase 2a fire-instant `T_phase_2a = T_0_broadcast + 1·BTT`.
- Add Phase 2a-late upgrade window `(T_phase_2a, slot_deadline)` — open-ended.
- Phase 2b emission is dynamic, no protocol-level hard wall.
- IBE tags: `nr_tag_k` for k ∈ [0, K-2]; total K-1 tags. **No σ_final_tag**.
- Update K-bounds discussion: K-generic (`f+1 ≤ K ≤ n`), K=2 default.

### Phase 1 — Candidate broadcast (lines 125–149)

- Bundle format unchanged.
- `T_0_broadcast = T_soft_end − 3·BTT − SafetyBuffer − ε_3` (runner-derived; e.g., ~2.15s at Config A with T_soft_end = 3.5s and default SafetyBuffer = RefloodDelay). V_0 has 1·BTT to propagate before Phase 2a fire.
- Receiver gate: "did we observe V_0 by `T_phase_2a` fire-time?" determines whether op emits `KindValue` (yes) or `KindNoValue` (no) at Phase 2a fire. V_0 received *later* via reflood triggers the Phase 2a-late upgrade path.
- Late bundles retained for full slot (Rule 2 detection).
- Drop auth-only-retention.

### Phase 2 — REWRITE entire section

The v1 spec's Phase 2a (verdict broadcast) and Phase 2b (σ-or-NR commit) are reshaped into: Phase 2a fire-instant (coordination) + Phase 2a-late (upgrade window) + Phase 2b (dynamic commit).

#### Phase 2a — Coordination broadcast at T_phase_2a fire-instant

Each operator emits exactly one of `KindValue` / `KindCommit` NR-direct / `KindNoValue` per the Protocol summary above.

L_0 emission selection (per op):
- Op has V_0 from bundle + host valid at Phase 1 → emit `KindValue`.
- Op observed L_0 leader equivocation (≥ 2 distinct V_0) → emit `KindCommit` NR-direct.
- Otherwise (no V_0 OR host says NV at Phase 1) → emit `KindNoValue`.

L_k>0 entry decisions at Phase 2a (per layer, independent per op):
- Op has V_k from L_k's Phase 1 bundle + host valid → σ-chained entry. EKM logs σ partial emission on the V-share.
- Op doesn't have V_k OR host says NV OR observed L_k equivocation → NR plaintext entry on nr_tag_k (for k ∈ [0, K-2]) OR empty (for L_{K-1}).
- Edge case (no decision possible): empty.

`KindValue` and `KindNoValue` are op-identity-signed envelopes; they carry no L_0 threshold partials (the L_0 σ partial appears only in Phase 2b `KindCommit-Signed`).

#### Phase 2a-late — KindNoValue → KindValue upgrade window

A `KindNoValue`-path op MAY emit an upgrade `KindValue` iff both:
- Op now has V_0 (via reflood, OR was host-NV at Phase 1 and host has since re-validated valid).
- Host re-validates V_0 as valid at upgrade-emission time.

The upgrade `KindValue` has the same wire shape as the Phase 2a `KindValue`. L_k>0 entries are identical to those in the op's prior `KindNoValue`.

**KindNoValue → KindValue is one of the authorized Phase-2 emission pairs** (see §Authorized Phase-2 emission pairs for the full enumeration including the host-flip pivot KindValue → KindCommit-NR). Rule 6a / Rule 3 fire only on pairs NOT in the authorized set. Receivers tolerate either arrival order.

#### Phase 2b — Dynamic commit emission

Each operator who emitted `KindValue` (Phase 2a or upgrade) or `KindNoValue` (without upgrade) emits one `KindCommit` when a trigger fires locally (see §Trigger rules for the three triggers — σ-eligibility, NR-eligibility with cannot-σ gate, equivocation — and §Emission ordering for the normative upgrade-first per-tick processing).

The Instance is a passive state machine: it reacts to incoming messages and, on each per-tick state delta, processes upgrade conditions first then evaluates commit triggers, emitting `KindCommit` as soon as a trigger condition is locally satisfied. **No T_commit deadline forces emission**. The slot's relay-submission deadline (runner-level) is the only hard wall — if no trigger fires by then, the slot misses (no protocol-level default emission).

Receivers process all observed messages from each op per §Pool aggregation rules (monotonic accumulation, arrival-order tolerance, dual-pool membership for authorized pivots). The only receiver-side "discard" semantics is the explicit `KindNoValue` → `KindValue` upgrade in §Receiver-side robustness, where X's contribution to novalue_pool[L_0] is replaced by its contribution to value_pool[V_0]. Any emission sequence from the same op NOT matching one of A1–A8 in §Authorized Phase-2 emission pairs is a Rule 6a (Phase-2 equivocation) or Rule 3 (cross-σ-V) violation (see §evidence.go for the enforceable check).

### Phase 3 — Reconstruction (lines 272–336)

- Observer-on-arrival from Phase 2a onward.
- For each layer k in ascending order:
  - **σ-pool[V_k] aggregation**: at L_0, plaintext σ partials from `KindCommit-Signed`. At L_k>0, chained-encrypted σ partials from `KindValue` / `KindNoValue` / `KindCommit-NR-direct` L_k entries (decrypted via accumulated nr_tag_0..nr_tag_{k-1} keys, peeled outermost-first).
  - If σ-pool[V_k] reaches qV, reconstruct V_k signature; output (k, V_k, S_k); halt.
  - **nr_tag_k-pool aggregation** (k < K-1): nr_tag partials from `KindCommit-NR-direct` (Phase 2a), `KindCommit-NR` (Phase 2b), and L_k NR-plaintext entries. If ≥ qEnc, derive nr_tag_k key, advance to L_{k+1}.
  - Else: walk halts; wait for next state delta or slot deadline.
- **Soft completion target**: ~3·BTT post-L_0-broadcast healthy / bounded by slot deadline worst-case.

### Operator commitment states (lines 343–356)

- σ (via plaintext σ partial in `KindCommit-Signed` at L_0; via chained-encrypted σ entry in `KindValue`/`KindNoValue`/`KindCommit-NR-direct` for L_k>0)
- NR (via nr_tag_k partial in `KindCommit-NR` or `KindCommit-NR-direct`, OR NR-plaintext L_k entry in any Phase-2a emission for L_k>0)
- Pending (op emitted `KindValue` or `KindNoValue` at Phase 2a but no `KindCommit` yet)

Remove NV state — host validity check at Phase 1 routes NV ops to `KindNoValue`; host re-check at KindCommit-Signed emission time routes them to `KindCommit-NR`.

### Slot structure (lines 357–366)

- Phase 1: per-layer leader broadcasts at T_k.
- Phase 2a: `KindValue` / `KindCommit` NR-direct / `KindNoValue` at `T_phase_2a`.
- Phase 2a-late: opportunistic `KindNoValue` → `KindValue` upgrades.
- Phase 2b: dynamic `KindCommit` emission per trigger. No protocol-level deadline.
- Phase 3: observer-on-arrival. Slot's relay-submission deadline is the only hard wall.

### Preconditions on the host application (lines 368–413)

- Host validity check at Phase 1 acceptance (drives `KindValue` vs `KindNoValue` choice).
- Host re-check at `KindCommit-Signed` emission time (if host now NV, op emits `KindCommit-NR` instead).
- Slashing-protection scope: σ partial on V-share signed at `KindCommit-Signed` emission; nr_tag partial on IBE-share signed at `KindCommit-NR` / `KindCommit-NR-direct` emission. Single-σ-V + σ-XOR-NR per layer — same as v1.

### Fault tolerance — Safety (lines 416–474)

- Re-derive Pigeonhole 1/2/3 with new pool sources:
  - L_0 σ-pool: plaintext σ partials in `KindCommit-Signed` messages.
  - L_0 nr_tag-pool: nr_tag partials in `KindCommit-NR` / `KindCommit-NR-direct` messages.
  - L_k>0 σ-pool on V_k: chained-encrypted σ partials from Phase-2a emissions, decrypted after nr_tag_0..nr_tag_{k-1} keys derived.
  - L_k>0 nr_tag-pool: nr_tag partials in `KindCommit` messages + L_k NR-plaintext entries in Phase-2a emissions.
- Algebra: each honest emits at most one σ partial on at most one V per (slot, layer) (V-share EKM); each honest emits at most one nr_tag partial per (slot, layer) (IBE-share EKM); single-σ-V XOR nr_tag per (slot, layer) is enforced by the Instance's EKM lock state. Same conclusions as v1.
- No void mechanism — σ partials are emitted plaintext at commit time, by ops that decided σ-direction. The "deniability" of v3's encrypted σ partials is achieved here by **late emission** (after trigger fires) rather than encryption.
- Pigeonhole arguments hold identically to v1.
- Add "structural attack closure" note: in v4, `KindValue` cryptographically couples the σ-side claim with V_0 fulltext + leader-auth-sig in one envelope. A byzantine cannot emit a wire-distinct "I have V_0" claim without publishing V_0. The Variant A withhold-then-fake-σ attack chain cannot execute against v4.

### Fault tolerance — Liveness (lines 476–578)

- Re-walk failure-mode catalog under v4.
- **Class A (uniform partial-synchrony violation)**: all honest see value_pool short → NR-eligibility trigger fires for V-drop / host-NV honest ops (cannot-σ gate satisfied) via natural `KindNoValue` propagation reaching qEnc → fall through to L_1 cleanly. σ-eligible honest ops (V_local + host valid) with value_pool short locally wait (per the cannot-σ gate); the cluster falls through via the V-drop / host-NV majority's NR contributions. (Same recovery shape as v1's row 5 + fall-through, but now driven by trigger rather than hard wall.)
- **Class A (> f faults)**: standard 3f+1 trust bound violation.
- **Class B (Phase-2 equivocation)**: byz emits a pair of Phase-2 emissions NOT in the authorized set (§Authorized Phase-2 emission pairs). Slashable evidence (Rule 6a / Rule 3).
- **Honest deadlocks under partial-synchrony + assumption 3**: **none** — every h_V_honest ∈ [0, 4] succeeds at L_0 or falls through to L_1. The non-uniform mesh-tail boundary is closed by removing the hard wall: ops that see stale σ_eligibility views simply wait for IHAVE/IWANT to deliver missing messages, then fire the trigger.
- **Assumption-3 boundary** (inherited from v1, narrower scope): 2-σV vs 2-NV mid-slot host divergence — cluster stalls at L_0 (no premature NR commit at a hard wall means no fall-through attempt). v1 with hard wall attempts L_1 fall-through here. v4 trades this for non-uniform mesh-tail recovery.
- **Closed in v4 vs v1**: non-uniform mesh-tail at L_0 (recovers via gossipsub IHAVE/IWANT within slot deadline). RefloodDelay budget dependency.

### Equivocation handling (lines 580–593)

- Phase 1 leader equivocation: Rule 2 (unchanged).
- Phase 2 equivocation: any pair of Phase-2 emissions from same op NOT in the **authorized Phase-2 emission pairs** set (A1–A8 in the Authorized Phase-2 emission pairs section) → Rule 6a-equivalent. Authorized pairs include the KindNoValue→KindValue upgrade and the KindValue→KindCommit-NR host-flip / equivocation pivots.
- Cross-phase cross-signing: σ partial + nr_tag partial at the same layer from same op → Rule 1 cross-signing.

### Slashing evidence (lines 595–631)

- Rule 1 (CrossSigning): σ partial in `KindCommit-Signed` + nr_tag partial in `KindCommit-NR`/`KindCommit-NR-direct` from same op at same layer.
- Rule 2 (LeaderEquivocation): unchanged.
- Rule 3 (CrossOnionEquivocation): two σ partials on distinct V_k from same op at L_k (e.g., two distinct `KindCommit-Signed` on different V_0, OR Phase 2a equivocation manifesting as different V_0 in distinct KindValues).
- Rule 4 (FakeEncryptedPresence): unchanged for L_k > 0 (post-decryption garbage).
- Rule 5 (FakePlaintextSigma): unchanged for L_0 (plaintext σ partial that doesn't verify against op's pubshare on V_0). Same as v1.
- Rule 6a (Phase-2 equivocation): any pair of Phase-2 emissions from same op NOT in the **authorized Phase-2 emission pairs** set (see §Authorized Phase-2 emission pairs in the design doc). Authorized pairs include: KindNoValue→KindValue upgrade; KindValue→KindCommit-NR host-flip pivot; KindValue→KindCommit-NR equivocation pivot; KindNoValue→KindCommit-NR; KindValue→KindCommit-Signed; the upgrade-then-commit chains; and stand-alone KindCommit-NR-direct. Rule 6a fires on anything else (two KindValues on different V_0 — also Rule 3 — KindValue followed by KindNoValue, KindCommit-Signed followed by KindCommit-NR or vice versa, etc.).
- Rule 6b (Verdict-vs-action): **dropped**. `KindValue` is coordination-only — pivot to `KindCommit-NR` is legitimate (sequence A3 host-flip or A4 equivocation per §Authorized Phase-2 emission pairs). The pivot may surface via any of the three Phase-2b triggers (σ-eligibility's NR-side branch, NR-eligibility with cannot-σ gate satisfied, or equivocation); all three routes produce the same authorized emission sequence and are sequence-only verifiable at receivers (per §evidence.go's sequence-only Rule 6a check).

### Failure modes (lines 632–662)

- Class A (uniform): partial-synchrony violation exceeding slot deadline. Slot misses.
- Class A (non-uniform): per-peer view divergence — **closed in v4** via natural IHAVE/IWANT recovery within slot deadline.
- Class A (> f faults): standard 3f+1 violation.
- Class B: Phase-2 equivocation (byz emits a pair of Phase-2 emissions NOT in the authorized set — see §Authorized Phase-2 emission pairs).
- **No structural Bucket-4 regressions vs v1.**
- **Honest deadlocks**: none under partial-synchrony + assumption 3.
- **Assumption-3 boundary**: 2-σV vs 2-NV post-Phase-2a host divergence — slot misses at L_0 with no fall-through (no premature NR commit). Inherited algebraic limit; v4 vs v1 trade-off favoring mesh-tail recovery.

### Properties summary (lines 678–696)

- Termination (healthy h_V=4): ~3·BTT post-L_0-broadcast (~600 ms at Config A).
- Termination (mesh-tail recovery): bounded by slot deadline (~3.85 s at Config A); typical ~3·BTT + RefloodDelay ≈ 1.3 s.
- Bandwidth (healthy): ~8.4 KB at K=2 n=4. Roughly on par with bare OBFT (~9–10 KB cluster-wide per its own spec) — v4 saves ~1–2 KB primarily by not carrying σ_L^V witness sections. The 2abOBFT v1 spec's ~20 KB figure was inflated.
- Bandwidth (worst-case h_V_honest=1): ~7.8 KB.
- Validity-divergence majority recovery: matches v1 (host re-check at KindCommit-Signed).
- 1-1-1 equivocation recovery: matches v1 (equivocation-trigger → fall-through).
- **Honest deadlocks**: none under partial-synchrony + assumption 3.
- IBE tags per slot: K-1 (same as v1).
- EKM complexity: 1 cross-keypair exclusivity rule (σ-V XOR nr_tag per layer) — same as v1.

### Cryptographic primitive (lines 663–676)

- Threshold IBE primitive unchanged (drand/tlock or equivalent).
- `nr_tag_k` per layer (K-1 tags). **No σ_final_tag**.
- Chained encryption depth at L_k: k levels (one less than v3 at every layer).

### Application: SSV (lines 698–758)

- Timing budget: ~3·BTT post-L_0-broadcast at recommended sizing (healthy); mesh-tail recovery bounded by slot deadline (~3.9 s).
- MEV-fetch headroom: ~2.15s at typical Config A anchor (T_soft_end = 3.5s, default SafetyBuffer = RefloodDelay) — same as bare OBFT at the same SafetyBuffer level. Tunable via SafetyBuffer (lean: ~2.85s MEV-fetch / no mesh-tail tolerance; loose: ~1.95s / extra tolerance).
- SSV mapping: update message kinds. `KindValue` is emitted at Phase 2a fire OR as a Phase 2a-late upgrade — adapter handles both emission points uniformly.
- No `T_commit` configuration knob; runner schedules `T_0_broadcast` and `T_phase_2a` only.
- Slot-deadline handling: runner abandons the slot if no certificate produced by `T_relay_cutoff − T_submit`.
- Head-change handling: host re-check at Phase 1 AND at `KindCommit-Signed` emission time.
- Phase 2a-late upgrade trigger: implementation hook for "V_0 received via reflood AND host valid AND I emitted KindNoValue earlier this slot".

### Practical caveats (lines 760–786)

- DKG cost: unchanged (V-keypair + IBE-keypair).
- Deadline coordination: only `T_0_broadcast` needs cluster alignment (T_phase_2a is derived). No `T_commit` to align.
- Choosing K: K-generic, K=2 default.
- R fixed at 1.
- Tag construction: `nr_tag_k` per spec naming (same as v1).
- "At most one full sig per instance": same as v1.

### Where this came from (lines 788–801)

- Update relationship table:
  - 2abOBFT v4: R=1, K-generic (K=2 default), Phase 2a/2b split with KindValue/KindNoValue/KindCommit, plaintext σ partials at L_0 in KindCommit-Signed (matches v1), chained-IBE at L_k>0 (matches v1), no hard wall.
- Motivation: "v4 emerged from a need to make mesh-tolerance configurable independent of gossipsub HeartbeatInterval AND eliminate honest deadlocks at every h_V_honest, while keeping the implementation cost low by aligning with the existing v1 twoab structure. Two key design moves: (1) Phase 2a is **pure coordination** (`KindValue`/`KindNoValue` — no threshold partials), with σ partials emitted plaintext at Phase 2b commit time. This matches v1's existing structure for L_0 (plaintext σ in Onion2b) — no new cryptographic primitive needed. (2) **Remove the `T_commit` hard wall**: ops wait for natural trigger-based convergence under partial-synchrony, bounded only by the slot's relay-submission deadline (runner-level). This closes the non-uniform mesh-tail boundary that any hard-wall design (v1, v3, bare OBFT) has — slow-edge messages are absorbed by gossipsub IHAVE/IWANT within slot budget without forcing premature commits. The Phase 2a-late upgrade window (`KindNoValue` → `KindValue`) handles V-drop ops who receive V_0 via reflood. The trade-off: v4 is ~1·BTT slower on the pure healthy-path than bare OBFT with early-commit (3·BTT vs 2·BTT post-broadcast), but gains mesh-tail robustness (no hard wall) + 1-1-1 equivocation recovery + faster clean-NR fall-through + configurable SafetyBuffer (decoupled from network HeartbeatInterval). At default SafetyBuffer, MEV-fetch headroom matches OBFT; deployments can tune SafetyBuffer up or down independently. Net for SSV's MEV-sensitive proposer duty on production mesh: favorable trade."

### Appendix A — Protocol comparisons (lines 803–940)

- A.1 (Comparison with OBFT): update with new wire/structural shape.
- A.2 (Comparison with OBFTR): mostly intact.
- A.3 (Comparison with bare OBFT and QBFT): refresh failure-mode table. Note v4's closure of the (b) deadlock pattern relative to bare OBFT (the partial-propagation `r = 2` shape at f=1 n=4) under non-uniform mesh-tail.

## Safety re-derivation (worked, for the spec)

Honest commitment states per layer:
- L_0: σ-side (via plaintext σ partial in `KindCommit-Signed`), NR-side (via nr_tag_0 partial in `KindCommit-NR` or `KindCommit-NR-direct`), or no-commit (op didn't fire any trigger by slot deadline).
- L_k for k ∈ [1, K-2]: σ-side (via chained-encrypted σ partial in Phase-2a emission), NR-side (via NR-plaintext L_k entry, OR via L_k commit entry in Phase-2b KindCommit), or empty.
- L_{K-1} (deepest): σ-side or empty (no NR-tag at deepest).

EKM rule (cryptographic): each honest signs σ on at most one V per (slot, layer) via V-share; each honest signs at most one nr_tag_k per (slot, layer) via IBE-share; σ-XOR-NR per (slot, layer) enforced by Instance's local lock state (matches v1).

V-share rule: each honest emits at most one σ partial on at most one V_k per (slot, layer). Phase 2a-late upgrades don't emit σ partials (upgrade `KindValue` is op-id-signed coordination, no threshold partial); σ partial is emitted only at `KindCommit-Signed` time.

**Pigeonhole 1 (L_k σ-quorum + L_k NR-quorum cannot both reach, k < K-1)**:
- L_k σ-pool: σ partials emitted by ops.
- L_k NR-pool: nr_tag_k partials emitted by ops.
- Honest: at most one σ OR one nr_tag per (slot, layer) (EKM σ-XOR-NR per layer). `h_σ + h_NR ≤ n − f = 2f+1`.
- Byz: `byz_σ + byz_NR ≤ 2f`.
- If both reach: (σ-pool) + (NR-pool) ≥ 4f+2. But max is (2f+1) + 2f = 4f+1. Contradiction. ✓

**Pigeonhole 2 (L_k two-V σ-quorum cannot both reach)**:
- Honest: at most one σ on any V per layer (single-σ-V EKM). `h_σ_V + h_σ_V' ≤ n − f = 2f+1`.
- Byz: at most 2f total cross-V partials.
- 2·qV = 4f+2 > (2f+1) + 2f = 4f+1. Contradiction. ✓

**Pigeonhole 3 (cross-layer safety under chained encryption)**:
- L_k>0 σ partials chained-encrypted under `nr_tag_0 ∧ ... ∧ nr_tag_{k-1}` (k levels).
- L_k σ-pool decryption requires nr_tag_0 ... nr_tag_{k-1} all reaching qEnc.
- By Pigeonhole 1 at L_j (j < k): nr_tag_j reaches qEnc → σ-quorum at L_j cannot reach. So if nr_tag_j reached for all j < k → σ at L_j never reaches → L_j signature never reconstructs.
- Symmetric: if L_k reconstructs, nr_tag_j reached for all j < k → L_j σ never reaches → L_j doesn't reconstruct.
- At most one V across all layers reconstructs cluster-wide. ✓

**Structural attack closure** (vs OBFT+Defer's Variant A): in v4, `KindValue` cryptographically couples the σ-side claim with V_0 fulltext + leader-auth-sig in one envelope. A byzantine cannot emit a wire-distinct "I have V_0" envelope without publishing V_0; honest gossipsub re-flood propagates V_0 to all receivers. The withhold-then-fake-σ attack chain cannot execute against v4.

## Liveness worked cases (for the spec)

n=4, f=1, K=2. Honest leader at L_0 unless noted.

**Note on Phase 2a emission**: at `T_phase_2a`, every op with V_0 + host valid emits `KindValue`. V-drops emit `KindNoValue`. Equivocation-observed ops emit `KindCommit` NR-direct.

**Note on Phase 2a-late upgrade**: a KindNoValue-path op who now has V_0 + host valid emits an upgrade `KindValue`. This contributes to value_pool[V_0] in receivers' views.

**Note on Phase 2b**: dynamic. Ops emit `KindCommit` when σ-eligibility, NR-eligibility (with cannot-σ gate), or equivocation trigger fires locally. **No T_commit hard wall** — ops without a trigger fire just don't emit `KindCommit`. The σ-eligibility trigger is fires-on-pool, side-on-state: it fires uniformly when value_pool reaches qV; each op's emission side (Signed vs NR) is decided locally based on V_local and host re-check. NR-eligibility additionally requires the op be cannot-σ locally (no V_local OR host NV) — this prevents σ-eligible ops from forfeiting σ-direction contributions to premature NR-side emission. Per-tick: upgrade conditions are evaluated before commit triggers (§Emission ordering).

**Note on h_V_honest=1 worked case below**: the gate + upgrade-first ordering make this case spec-guaranteed. Without the gate, the leader L (σ-eligible) observing 3 KindNoValues before V-drops upgrade would prematurely fire NR-eligibility and emit `KindCommit-NR`. Under byz withholding (1 V-drop honest upgrades + 1 byz refuses to cooperate), σ-pool[V_0] reaches only `{1 honest upgrader} = 1 < qV` and the slot misses. With the gate, L waits; honest V-drops upgrade; σ-eligibility fires cluster-wide; σ-quorum reaches with `{L, ≥1 honest upgrader}`.

| Scenario | Phase 2a emissions | Phase 2a-late upgrades | Phase 2b emissions | Result |
|---|---|---|---|---|
| Healthy h_V=4 | 4 KindValues | none | σ-eligibility trigger fires (value_pool = 4 ≥ qV). All 4 σ-side → 4 `KindCommit-Signed` | σ-pool[V_0] = 4 ≥ qV. **L_0 σ-quorum ✓** |
| h_V_honest=3 (1 V-drop honest, byz arbitrary) | 3 KindValues + 1 KindNoValue + byz | V-drop receives V_0 + host valid → 1 upgrade KindValue | σ-eligibility trigger fires. All 4 σ-side → 4 `KindCommit-Signed` | **L_0 σ-quorum ✓** |
| h_V_honest=2 (2 V-drop honest, byz arbitrary) | 2 KindValues + 2 KindNoValues + byz | both V-drop honest upgrade → 2 upgrade KindValues | σ-eligibility trigger fires. All 4 σ-side → 4 `KindCommit-Signed` | **L_0 σ-quorum ✓** |
| **h_V_honest=1 (only leader has V_0)** | 1 KindValue + 3 KindNoValues | 2 honest V-drops upgrade; byz may or may not | σ-eligibility trigger fires (value_pool = 3+ ≥ qV after upgrades). All 3 honest σ-side → `KindCommit-Signed` | **L_0 σ-quorum ✓** |
| h_V_honest=0 (no honest has V_0; byz leader withheld) | 3 KindNoValues + byz | nobody can upgrade (no V_0 to receive) | NR-eligibility trigger fires (novalue_pool = 3+ ≥ qEnc). All 4 → `KindCommit-NR` | nr_tag_0-pool = 4 = qEnc → **L_0 NR-quorum → L_1 σ-quorum ✓** |
| **Non-uniform mesh-tail at h_V_honest=2** (3 KindValues cluster-wide, but 1 op sees only 2 KindValues at typical timing due to slow edge) | 3 KindValues + 1 KindNoValue + maybe upgrades | varies | The slow-view op waits; IHAVE/IWANT delivers the missing KindValue within ~RefloodDelay; σ-eligibility trigger fires → `KindCommit-Signed` | **L_0 σ-quorum ✓** (closes v3-with-hard-wall's non-uniform mesh-tail miss) |
| 1-1-1 byz leader equivocation | 3 KindValues on distinct V_a/V_b/V_c (each honest's view) | none | Each honest observes ≥ 2 V's via reflood → equivocation trigger fires → 3 `KindCommit-NR` | nr_tag_0-pool = 3 ≥ qEnc → **L_0 NR-quorum → L_1 σ-quorum ✓** |
| 2-1 byz-defect (byz delivers V to {A,B}, V' to {C}, then withholds) | A, B KindValue(V); C KindValue(V'); byz silent | none | All honest observe equivocation via reflood → equivocation trigger fires → 3 `KindCommit-NR` | nr_tag_0-pool = 3 → **L_0 NR-quorum → fall through ✓** |
| Validity-divergence 3-σV vs 1-NV at f=1 n=4 | 3 KindValues on V_0 + 1 KindNoValue (NV-side honest) | NV honest receives V_0 but host still NV → does NOT upgrade | σ-eligibility trigger fires (value_pool[V_0] = 3 ≥ qV). 3 σ-side ops → `KindCommit-Signed`. NV-side op: σ-eligibility trigger fires for them too (cluster value_pool met), op has V_local but host says NV → emits `KindCommit-NR` per the side-on-state rule. (NR-eligibility trigger does NOT fire here — novalue_pool = 1 < qEnc — but σ-eligibility trigger's NR-side branch is what fires NV-side's emission.) | σ-pool[V_0] = 3 ≥ qV. nr_tag_0-pool = 1. **L_0 σ-quorum on V_0 ✓** |
| Validity-divergence 2-σV vs 2-NV at f=1 n=4 (divergence at Phase 1) | 2 KindValues + 2 KindNoValues | NV-side honest don't upgrade (host still NV) | value_pool[V_0] = 2 < qV → σ-eligibility trigger doesn't fire. novalue_pool = 2 < qEnc → NR-eligibility trigger doesn't fire. No equivocation. All wait. No trigger fires by slot deadline. | **Slot misses at relay deadline.** Stall. (v1 with hard wall would have NR-defaulted at T_commit and fallen through to L_1 — v4 trade-off.) |
| Host re-org mid-slot 3-σV vs 1-NV (1 op's host flips between Phase 2a fire and KindCommit emission) | 4 KindValues at Phase 2a | none | σ-eligibility trigger fires (value_pool[V_0] = 4 ≥ qV). 3 still-valid → `KindCommit-Signed`. 1 flipped → `KindCommit-NR` via the σ-eligibility-trigger NR-side branch (KindValue→KindCommit-NR is authorized pivot A3). | σ-pool[V_0] = 3 = qV; nr_tag_0-pool = 1. **L_0 σ-quorum ✓** |
| Host re-org mid-slot 2-σV vs 2-NV (assumption-3 violation) | 4 KindValues at Phase 2a | none | σ-eligibility trigger fires for all 4. 2 still-valid → `KindCommit-Signed`. 2 flipped → `KindCommit-NR` (pivot A3). | σ-pool = 2 < qV; nr_tag_0-pool = 2 < qEnc. **Slot misses at L_0 with no fall-through.** Inherited from v1's algebraic limit. |
| **Host re-org mid-slot 1-σV vs 3-NV** (3 ops' hosts flip post-Phase-2a; newly handled by unified σ-eligibility trigger) | 4 KindValues at Phase 2a | none | σ-eligibility trigger fires for all 4. 1 still-valid → `KindCommit-Signed`. 3 flipped → `KindCommit-NR` (pivot A3). | σ-pool = 1 < qV; nr_tag_0-pool = 3 = qEnc. **L_0 NR-quorum → L_1 σ-quorum ✓** (under the old separate-σ-and-NR-trigger framing, the flipped ops had no firing condition because novalue_pool < qEnc, so they would have stalled — this case was a liveness gap that the unified trigger closes) |
| Host re-org mid-slot 4-NV (all 4 ops' hosts flip post-Phase-2a) | 4 KindValues at Phase 2a | none | σ-eligibility trigger fires for all 4. All 4 → `KindCommit-NR` (pivot A3). | nr_tag-pool = 4 ≥ qEnc → **L_0 NR-quorum → L_1 σ-quorum ✓** |

## EKM coordination (updated rules, for the spec)

Single transactional log keyed on `(slot, layer, side, value_root)`. Matches v1's existing structure.

### V-share signing events

- **Sign σ partial on V at L_k** (V-keypair share, for k ∈ [0, K-1]): rejected if any prior `(slot, L_k, "σ", _)` row with different value_root. On success, log `(slot, L_k, "σ", value_root(V))`. Each op signs at most one σ partial per (slot, layer).
  - σ partials at L_0 are emitted plaintext in `KindCommit-Signed`.
  - σ partials at L_k>0 are emitted chained-encrypted in Phase-2a emissions (`KindValue` / `KindNoValue` / `KindCommit-NR-direct` L_k entries).

### IBE-share signing events

- **Sign nr_tag_k** (IBE-keypair share, for k ∈ [0, K-2]): rejected if any prior `(slot, L_k, "σ", _)` or `(slot, L_k, "nr_tag", _)` row. On success, log `(slot, L_k, "nr_tag", null)`.
- L_{K-1} has no nr_tag side.

### Cross-share coordination

- σ-side emission at any phase → log V-share row.
- nr_tag-side emission (any of `KindCommit-NR`, `KindCommit-NR-direct`, L_k NR-plaintext entry in Phase-2a emission) → log IBE-share row.
- σ at L_k XOR nr_tag_k per (slot, layer) — enforced by the cross-share check in both signing paths.

### Properties

- Phase 2a emission per op per (slot): one of `KindValue` / `KindCommit` NR-direct / `KindNoValue` at `T_phase_2a` fire instant.
- Optional Phase 2a-late upgrade per op per (slot): a `KindNoValue`-path op may emit *one* upgrade `KindValue` (authorized transition; not Rule 6a).
- Single Phase 2b emission per op per (slot): `KindCommit` (for `KindValue`-path and `KindNoValue`-path ops; not for NR-direct ops).
- σ partials at L_0 are plaintext in `KindCommit-Signed`; aggregation is direct (no decryption step at L_0).
- σ partials at L_k>0 are chained-encrypted; aggregation requires nr_tag_0..nr_tag_{k-1} keys (each derived after the respective layer's NR-quorum reaches qEnc).

**Matches v1's EKM coordination model exactly.** No new exclusivity rule; no σ_final_tag tracking.

## Resolutions of open questions

All open questions from prior iterations resolved. Recapped here for traceability.

| # | Question | Resolution |
|---|---|---|
| 1 | Phase 2b "give up" deadline | **Removed.** No protocol-level hard wall. The slot's relay-submission deadline (runner-level) is the only deadline. Ops emit `KindCommit` dynamically when a trigger fires; if no trigger fires by slot deadline, the op doesn't emit. |
| 2 | Auth-only retention semantics | Dropped. Phase 1 bundles retained for entire slot (Rule 2 detection); no auth-only/regular distinction. |
| 3 | EKM rule for leader's first σ emission | No special leader treatment. Leader's σ partial on V_0 is signed at `KindCommit-Signed` emission time (Phase 2b) — same as everyone else. |
| 4 | Late Phase 2a emission | No hard cutoff. Receivers process `KindValue` / `KindNoValue` / `KindCommit` throughout the slot. Convergence per-snapshot. |
| 5 | Bundle re-flood during Phase 2a/2b | Standard gossipsub for Phase 1 bundles and Phase 2a messages (`KindValue` carries V_0 fulltext, doubling as a V_0 reflood vector). KindNoValue-path ops upgrade via late `KindValue` after receiving V_0. |
| 6 | Per-layer fetch timing for L_1+ | `T_{K-1} = BFT_start` (slot start). Asymmetric `T_{K-1} ≤ ... ≤ T_0`. `T_0 = T_0_broadcast` runner-scheduled. |
| 7 | Naming consistency | Keep "Phase 2a" / "Phase 2b" labels. `ProtocolTag = "2abOBFT"` (no version suffix — superseded; see §Execution plan / Confirmed decisions). Full rewrite of docs/2abOBFT.md. |
| 8 | K > 2 extension | K-generic spec. Multi-layer Phase 2a emission carries all-K state. K-1 nr_tag tags (no σ_final_tag). |

### Notable v4-specific decisions

- **σ partials plaintext at L_0** (matches v1; no σ_final_tag_k IBE).
- **No T_commit hard wall** — dynamic trigger-based emission only.
- **`KindNoValue` → `KindValue` upgrade**: authorized Phase 2a-late transition; explicitly not Rule 6a.
- **Equivocation handling**: explicit equivocation-trigger fires `KindCommit-NR` (or `KindCommit-NR-direct` at Phase 2a) on observing ≥ 2 distinct V_0 from leader.
- **Validity-divergence 2-σV vs 2-NV mid-slot host divergence**: slot misses at L_0 with no fall-through (trade vs v1's hard-wall L_1 attempt).
- **Non-uniform mesh-tail at L_0**: **closed** via natural IHAVE/IWANT recovery within slot deadline (key v4 win vs v1 with hard wall).

## Relationship to OBFT family

### Relationship to OBFT+Defer (formerly OBFT.md Appendix E, deleted)

| Aspect | OBFT+Defer | 2abOBFT v4 |
|---|---|---|
| Defer recovery scope | aggressive-marginal partition within `[T_commit, T_accept_max]` at L_0 | same recovery, generalized to all K layers; no T_commit boundary at all |
| Variant A (withhold-then-fake-σ) | **opens** the attack | **closes** structurally (KindValue couples claim + V) |
| K-genericity | per-layer Defer not specced | natural K-extensibility |
| RefloodDelay dependency | yes | none |
| Hard wall | yes | **none** — bounded only by slot deadline |

### Relationship to bare OBFT

| Aspect | bare OBFT | 2abOBFT v4 |
|---|---|---|
| Phase 1 σ_V | yes (in bundle) | no |
| Phase 2 split | no | yes (2a coordination + 2b commit) |
| Phase 2a structure | n/a | KindValue/KindNoValue (op-id-signed, no partials) |
| σ partials timing | Phase 1 (leader) + Phase 2 (others, with early-emit at `T_L_0_observed`) | Phase 2b only (`KindCommit-Signed`) at L_0; Phase 2a chained-encrypted at L_k>0 |
| σ-direction commitment | Phase 2 KindCommit (early-emit gated by `T_L_0_observed`) | Phase 2b KindCommit-Signed (trigger-driven) |
| K-layer fall-through | yes | yes |
| RefloodDelay (structural) | **yes** — pre-allocated in B_0 = 2·BTT + RefloodDelay regardless of healthy/tail | **no** — leader broadcasts at T_commit − 3·BTT |
| Hard wall | yes (T_commit) | **none** (slot deadline is the only deadline) |
| Healthy h_V=4 post-broadcast latency | **~2·BTT** (with early-commit; faster) | ~3·BTT (Phase 2a coordination adds 1·BTT) |
| MEV-fetch headroom at typical anchor (default SafetyBuffer) | ~1.2s | **~1.2s** (same as OBFT at same SafetyBuffer; tunable via SafetyBuffer knob) |
| Clean-NR fall-through latency | T_commit + Δ_2 ≈ 1.3s post-broadcast (NR-side waits for T_commit fallback) | **~3·BTT ≈ 600ms** (NR-eligibility trigger fires when novalue_pool reaches qEnc) |
| Non-uniform mesh-tail at L_0 | (b) deadlock if peer-reflood-V doesn't complete before T_commit | recovers via natural IHAVE/IWANT within slot deadline |
| 1-1-1 byz leader equivocation | slot misses at L_0 (σ-locked split; no fall-through) | recovers via equivocation-trigger → L_1 fall-through |
| 2-σV vs 2-NV mid-slot host divergence | misses (algebraic limit; same as v4) | misses (algebraic limit; same as OBFT) |
| Honest-failure liveness | (b) deadlock + 1-1-1 equivocation + σ-locked patterns can miss | **no honest deadlocks** under partial-synchrony + assumption 3 |

**Net trade-off**: v4 gives up ~1·BTT of healthy-path latency in exchange for non-uniform-mesh-tail robustness + 1-1-1 equivocation recovery + faster clean-NR fall-through + configurable SafetyBuffer (decoupled from network HeartbeatInterval). MEV-fetch headroom is the same as OBFT at default SafetyBuffer; deployments can tune SafetyBuffer up (wider MEV-fetch, less mesh tolerance) or down (more mesh tolerance, narrower MEV-fetch) independently of gossipsub's HeartbeatInterval. Whether this trade is favorable depends on the deployment's MEV-sensitivity and mesh-quality profile. For SSV's proposer duty on production mesh, the v4 trade is favorable.

## Migration / coexistence

- Wire-incompatible with v1 (`2abOBFT` ProtocolTag).
- SSV adapter (not yet built) targets v4 directly.
- Existing v1 twoab implementation at [protocol/v2/obft/twoab](../protocol/v2/obft/twoab) is the migration target — see §Implementation adjustments below.

## Implementation adjustments to twoab (post-spec-rewrite)

After the spec rewrite to v4 lands in docs/2abOBFT.md, the existing implementation at [protocol/v2/obft/twoab/](../protocol/v2/obft/twoab/) needs the following focused refactor. The migration is intentionally close to v1's structure — most changes are renames + simplifications + one new code path (Phase 2a-late upgrade) + removing the hard wall.

**Migration impact estimate**: ~300–500 LoC of focused refactoring across ~6 files. No new cryptographic primitive. No new EKM exclusivity rule.

### File-level changes

#### `wire/wire.go` + `wire/envelope.go`
- Set `ProtocolTag` constant to `"2abOBFT"` + 9 NUL bytes (superseded the planning-time "v4 tag" — see §Execution plan / Confirmed decisions).
- Replace `KindVerdict` (currently 0x02) with two new kinds:
  - `KindValue` (0x02): V_0 fulltext + leader-auth-sig + L_k>0 entries.
  - `KindNoValue` (0x03): L_k>0 entries only (no L_0 payload).
- Rename `KindOnion2b` to `KindCommit` (renumber to 0x04). Add commit-side flag {Signed, NR, NR-direct}.
- Renumber `KindCertificate` to 0x05.
- Drop the v1 verdict kind enum {σV, NV, NR, NR-due-to-equivocation} — binary KindValue/KindNoValue in v4.
- Add wire-format support for the new commit-side flag in `KindCommit`.

#### `instance.go`
- Rename Instance struct fields:
  - `peerVerdicts` / `ownVerdict` → `peerValues` / `peerNoValues` / `ownValue` (or unified `peerCoordMap[OpID]ValueOrNoValue`).
  - `peerOnions` → `peerCommits` (with side flag visibility).
- Track Phase 2a-late upgrade state: a per-op flag indicating whether they emitted `KindNoValue` and later `KindValue` (the authorized upgrade). Receiver-side tolerance for either arrival order (both messages from same op = upgrade).
- EKM locks (`sigmaLocked`, `nrLocked`) **unchanged** — same per-layer arrays.
- **Remove any T_commit-related state**. Phase 2b is purely trigger-driven; the Instance has no notion of a Phase 2b deadline. The runner abandons the slot at the relay-submission deadline.

#### `phase2a.go` + `convergence.go`
- Simplify `ComputeLocalVerdict` (rename to `ComputeLocalValueState`) to binary:
  - Has V_0 + host valid at Phase 1 → `KindValue`.
  - Observed equivocation (≥ 2 distinct V_0) → emit `KindCommit-NR-direct` (skip Phase 2a entirely).
  - Otherwise (no V_0 OR host NV) → `KindNoValue`.
- Rename `BuildVerdict` to `BuildValue` / `BuildNoValue` / `BuildCommitNRDirect` (three separate builders).
- Add `BuildUpgradeValue` for the Phase 2a-late upgrade path: takes the prior `KindNoValue`'s L_k>0 entries (if any), builds `KindValue` with V_0 fulltext + leader-auth-sig + same L_k>0 entries.
- Update Phase 2a fire-time hook in the runner to schedule emission at `T_phase_2a = T_0_broadcast + 1·BTT`.

#### `phase2b.go` (significantly simplified)
- Replace the 5-row convergence-rule evaluation with three trigger predicates (in `convergence.go`). The σ-eligibility trigger is **fires-on-pool, side-on-state** — it fires uniformly when the cluster reaches value-quorum, with emission side decided per-op at commit time:
  - `TryTriggerSigmaEligibility(slot, layer)`: returns `(fire, side)` — fires when `value_pool[V_k] ≥ qV` for some V_k. Side decision: if op has V_local^k = V_k AND host re-validates valid → emit `KindCommit-Signed`; else → emit `KindCommit-NR`. This covers both v1's row 3 (σ-side) and row 4 (NR-side via host-flip pivot, V-drop at value-quorum time, or V_local mismatch).
  - `TryTriggerNREligibility(slot, layer)`: fires when `novalue_pool[L_k] ≥ qEnc` **AND op cannot σ at L_k (no V_local^k OR host re-validates as NV)** — covers the all-NoValue h_V_honest=0 path and the host-flipped subset at h_V_honest>0. The cannot-σ gate is structurally necessary: without it, a σ-eligible op observing other ops' KindNoValues before its own KindValue propagates would EKM-lock NR via this trigger, breaking L_0 σ-quorum under byz withholding at h_V_honest=1. Emit `KindCommit-NR`.
  - `TryTriggerEquivocation(slot, layer)`: fires when op retained ≥ 2 distinct V_k. Emit `KindCommit-NR` (or `KindCommit-NR-direct` at Phase 2a if op hadn't emitted KindValue yet).
- Trigger priority on simultaneous fire: equivocation > σ-eligibility > NR-eligibility.
- **Normative per-tick processing order** (per §Emission ordering in the design doc): each per-tick processing of incoming-message state delta MUST run `MaybeBuildAndBroadcastUpgrade(slot)` to completion (emission delivered to gossipsub) BEFORE `MaybeBuildAndBroadcastCommit(slot)` evaluates triggers. Without this ordering:
  - A V-drop X that just received V_0 + has host valid + observes cluster σ-eligibility would emit `KindCommit-Signed` directly (skipping the upgrade `KindValue`), producing the non-conformant emission sequence `KindNoValue → KindCommit-Signed` (not in A1-A8). Receivers may still pool X correctly via inference but X's emission history is structurally broken.
  - A V-drop X that just received V_0 + has host valid + observes cluster NR-eligibility (qEnc reached on novalue_pool) — the cannot-σ gate on NR-eligibility prevents NR-lock if X has V_local at evaluation time. Combined with upgrade-first, X upgrades and then waits for σ-eligibility.
- `MaybeBuildAndBroadcastUpgrade(slot)`: checks every layer-0 op state; if op is on KindNoValue path AND op has V_0 AND host re-validates V_0 valid → call `BuildUpgradeValue` and broadcast. Returns immediately (synchronous within the tick) so that subsequent commit-trigger evaluation sees the upgraded state.
- `MaybeBuildAndBroadcastCommit(slot)`: checks all three triggers on every state delta. Emits at most one `KindCommit` per (slot, op).
- L_k>0 commit entries: keep the existing chained-IBE encryption logic from v1 (`chainEncryptForLayer`). **No σ_final_tag wrapping** (v3-specific, removed).
- **Drop all T_commit deadline-driven emission paths.** The Instance does not emit a default `KindCommit` if no trigger fires.

#### `convergence.go` (significantly simplified)
- Drop the 5-row table (rows 1–5 of v1/v3).
- Replace with the three trigger predicates above.
- Drop `RowOfConvergence` enum.
- **Rewrite pool aggregation with explicit inference rules** (see §Pool aggregation rules / §Inference rules in the design doc). Both claim pools are inferred from observed messages per the two tables (L_0 contributions from message kind; L_k>0 contributions from inline σ-chained / NR-plaintext / empty entries):
  - **L_0 claim pools:**
    - `value_pool[V_0]` membership inferred from `KindValue` on V_0 OR `KindCommit-Signed` on V_0 (the latter implies KindValue existed via authorized pair A2/A6).
    - `novalue_pool[L_0]` membership inferred from `KindNoValue` (without later upgrade) OR `KindCommit-NR-direct` OR `KindCommit-NR`.
  - **L_k>0 claim pools (per layer k > 0):**
    - `value_pool[V_k]` membership inferred from X having a σ-chained L_k entry in their Phase-2a emission (`KindValue` / `KindNoValue` / `KindCommit-NR-direct`) — the L_0 emission kind is independent of L_k>0 entry direction.
    - `novalue_pool[L_k]` membership inferred from X having an NR-plaintext L_k entry in their Phase-2a emission.
- Ensure receiver-side arrival-order tolerance: if `KindCommit-Signed` arrives before the corresponding `KindValue` (gossipsub reorder), the op is still added to value_pool[V_0] from the KindCommit-Signed observation. Later KindValue arrival is a no-op for pool counting.
- KindNoValue → KindValue upgrade: when the upgrade is observed (or inferred from a later KindCommit-Signed), remove the op from novalue_pool[L_0] and add to value_pool[V_0]. L_k>0 entries from the original KindNoValue carry over identically (per [line 591](docs/2abOBFT-REDESIGN-PLAN.md:591)).
- Dual-pool membership permitted for authorized pivots A3/A4/A7: an op that emitted KindValue then KindCommit-NR is in both value_pool[V_0] and novalue_pool[L_0] simultaneously (Pigeonhole 1 applies to threshold-partial pools σ-pool / nr_tag_0-pool, not to claim-pools).

#### `phase3.go` (minor changes)
- `tryReconstructLayer` at L_0: aggregate plaintext σ partials from `KindCommit-Signed` messages directly (σ-pool[V_0] threshold pool). Same as v1 (no σ_final_tag derivation step needed — v3 would have required this).
- `tryReconstructLayer` at L_k>0: aggregate chained-encrypted σ partials from Phase-2a emissions (`KindValue` / `KindNoValue` / `KindCommit-NR-direct` L_k entries), decrypted via accumulated nr_tag_0..nr_tag_{k-1} keys. Same chain structure as v1.
- Note: σ-pool[V_k] (the threshold pool used for reconstruction) is distinct from value_pool[V_k] (the claim pool used for trigger evaluation in `convergence.go`). σ-pool counts threshold-partial-bearing messages (KindCommit-Signed or L_k chained σ-partial entries); value_pool counts σ-eligibility claims (with inference from KindCommit-Signed per the aggregation rules).
- `tryDeriveNextLayerKey`: unchanged from v1.

#### `evidence.go`
- Rule 6a (Phase-2 equivocation, renamed from Verdict equivocation): receivers check the observed emission **sequence** from each op against the §Authorized Phase-2 emission pairs (A1-A8). The check is **sequence-only** — the triggers that fired locally at the emitter (σ-eligibility, NR-eligibility, equivocation, cannot-σ gate satisfaction) are not externally observable, so Rule 6a enforcement classifies by emission sequence:
  - **NOT slashable (matches an authorized sequence):**
    - A1: `KindNoValue → KindValue` (upgrade).
    - A2: `KindValue → KindCommit-Signed` (normal σ commit).
    - A3 / A4: `KindValue → KindCommit-NR` (host-flip pivot or equivocation pivot — sequence indistinguishable at receiver, so both authorized).
    - A5: `KindNoValue → KindCommit-NR` (NR-side commit).
    - A6: `KindNoValue → KindValue → KindCommit-Signed` (upgrade + σ commit).
    - A7: `KindNoValue → KindValue → KindCommit-NR` (upgrade + NR pivot).
    - A8: `KindCommit-NR-direct` (alone; no preceding or following emission from same op at this layer).
  - **Slashable (sequence NOT in A1-A8):**
    - `KindValue` on V_a + `KindValue` on V_b with V_a ≠ V_b → Rule 3 (cross-σ-V) + Rule 6a (Phase-2 equivocation).
    - `KindNoValue → KindCommit-Signed` (no upgrade interpolation).
    - `KindValue → KindNoValue` (downgrade — protocol doesn't authorize this direction).
    - `KindCommit-Signed → KindCommit-NR` or `KindCommit-NR → KindCommit-Signed` (cross-side commit, EKM violation already caught by Rule 1; Rule 6a backstops).
    - `KindCommit-NR-direct + <any other emission>` from same op (A8 is sole-emission).
    - `KindNoValue → KindCommit-NR → KindValue` (post-commit upgrade — illegal, op already EKM-locked NR).
    - Any other 3+ message sequence not matching A6 or A7.
  - **Multi-message check is over the full sequence**, not just adjacent pairs (e.g., for A6/A7 receivers check that the upgrade `KindValue` exists between `KindNoValue` and `KindCommit-*`). When receivers observe only a subset of an op's emissions (e.g., due to gossipsub drop), they classify based on what they have; later arrivals can re-classify (and may produce slashing evidence retroactively).
- Rule 6b (Verdict-vs-action contradiction): **drop**. `KindValue` is coordination-only; pivot to `KindCommit-NR` is legitimate when op pivots into NR-side at commit time (host re-validates NV per A3, or equivocation observed per A4). Three different local triggers can surface this pivot — σ-eligibility's NR-side branch, NR-eligibility with cannot-σ gate satisfied by host-flip, or the equivocation trigger — but all produce the same emission sequence `KindValue → KindCommit-NR`, which is in A3/A4 of the authorized set. Receivers don't need to (and can't) externally verify which trigger fired; they accept the sequence per Rule 6a's sequence-only check. Rule 6b becomes irrelevant.
- Rules 1, 2, 3, 4, 5: structurally unchanged. Rule 5 (FakePlaintextSigma at L_0) is the same as v1 — L_0 σ partials are plaintext in v4 (matches v1).

#### `validation.go`
- `ValidateVerdict` → split into `ValidateValue` (must carry V_0 + leader-auth-sig + L_k>0 entries) and `ValidateNoValue` (only L_k>0 entries).
- `ValidateOnion2b` → rename to `ValidateCommit` with side-flag-aware validation.
- Existing structural checks (cluster ID, height, layer range, op membership) unchanged.

#### Test files (`sim_test.go`, `scenarios_test.go`, etc.)
- Update scenario expectations:
  - **h_V_honest=0**: expect `KindNoValue`-quorum fall-through to L_1 (was: v1 verdict_pool short → fall-through).
  - **h_V_honest=1 / h_V_honest=2**: expect upgrade `KindValue`s from V-drops, then σ-eligibility trigger fires for `KindCommit-Signed` quorum at L_0.
  - **1-1-1 byz leader equivocation**: expect `KindCommit-NR` (via equivocation-trigger) from honest ops, nr_tag_0-quorum, fall-through to L_1.
  - **Non-uniform mesh-tail (new test class)**: expect eventual recovery at L_0 via IHAVE/IWANT (slower than healthy but well within slot deadline). Add tests that simulate mesh-tail propagation explicitly.
  - **Validity-divergence 2-σV vs 2-NV** (assumption-3 violation): expect **slot miss at relay deadline** (no fall-through). This is a regression vs v1 behavior in this specific scenario — update test assertions accordingly.
- Drop hard-wall-related scenarios (everything that relied on `T_commit` timeout to force NR-default).

#### Runner integration (out of scope for this plan; handled in the impl plan)

The runner-level changes (timing schedule, slot-deadline handling, Instance lifecycle):
- `T_commit` removed as a scheduler input. Runner schedules `T_0_broadcast` and `T_phase_2a` from slot start; Phase 2b emissions are reactive to message arrivals.
- The runner's slot-end handler (`OnSlotDeadline`) calls `Instance.Abandon(slot)` (or equivalent) if no certificate produced. Instance no longer emits a "give-up" `KindCommit-NR`.
- `Instance.OnSlotDeadline()` is a no-op in v4 — the Instance does not act on the slot deadline. Only the runner does.

### Removed code (v3-specific that doesn't apply)

The following v3-specific machinery is NOT needed in v4 and should NOT be added during migration:
- `σ_final_tag_k` IBE tag construction.
- `σ_final_tag_k` XOR `nr_tag_k` EKM exclusivity logic.
- Per-layer σ-direction-confirmation step in Phase 3 walk (decrypting under σ_final_tag_k).
- Multi-row convergence rule (rows 1–5) — replaced by three trigger predicates.

### Migration order (suggested)

1. **Spec rewrite first** (docs/2abOBFT.md → v4). Land separately.
2. **Wire format bump** + protocol tag change in `wire/`. Update domain-separation tests.
3. **Instance struct refactor** in `instance.go`. Rename fields, drop T_commit state.
4. **Phase 2a logic** in `phase2a.go`: binary verdict, add upgrade builder.
5. **Convergence + Phase 2b** in `convergence.go` + `phase2b.go`: drop 5-row table, add three triggers, drop default emission.
6. **Phase 3** in `phase3.go`: minor pool-source updates.
7. **Evidence rules** in `evidence.go`: drop Rule 6b, update Rule 6a exception.
8. **Tests** updated for new scenario expectations.
9. **Runner integration** (separate impl plan).

## Post-spec-rewrite TODOs

### docs/BFT-comparison.md update

After the 2abOBFT spec rewrite lands, `docs/BFT-comparison.md` needs updating:

- **Latency/bandwidth tables**: v4's post-T_0_broadcast latency (~3·BTT healthy, ~3·BTT + SafetyBuffer worst-case mesh-tail; ~600–1300 ms at Config A with default SafetyBuffer = RefloodDelay) and healthy bandwidth (~8.4 KB at K=2 n=4) replace v1 numbers. Note: the v1 spec's ~20 KB bandwidth figure was inflated relative to OBFT's own ~9–10 KB measurement; cross-protocol bandwidth comparisons should use re-derived numbers from each protocol's own size calculations rather than the v1 figure.
- **Failure-mode coverage tables**: v4's recovery surface (closes non-uniform mesh-tail; same recovery scope as v1 otherwise; loses validity-divergence-2-2 fall-through).
- **Recovery scope buckets** (§Bucket 3, §Bucket 4): v4 vs bare OBFT — closes the (b) deadlock pattern.
- **Production-T allocation**: same MEV-fetch headroom as OBFT at default SafetyBuffer; v4 adds configurability knob.
- **Sizing convention**: introduce `T_soft_end` and `SafetyBuffer` as unified naming across protocols. `SafetyBuffer` is the protocol-level configurable for mesh-tolerance budget; `RefloodDelay` stays as the gossipsub network primitive (HeartbeatInterval). Default SafetyBuffer per protocol: OBFT = 1·BTT + RefloodDelay (covers peer-reflood-V hop + IHAVE/IWANT cycle); v4 = RefloodDelay (covers IHAVE/IWANT cycle only).

### docs/OBFT.md cleanup (recommended companion change)

- Promote `T_soft_end` to a first-class derived offset (currently OBFT has `T_commit + Δ_2 + ε_3` as Phase 3 target without naming it). Other params (T_0_broadcast = T_commit − B_0, T_commit = T_soft_end − Δ_2 − ε_3) can be expressed as derived from T_soft_end + SafetyBuffer + B_0/Δ_2.
- Introduce `SafetyBuffer = 1·BTT + RefloodDelay` as a named protocol-level configurable. `B_0 = 2·BTT + SafetyBuffer − Δ_2 = 2·BTT + RefloodDelay` (unchanged numerically, but expressed via SafetyBuffer for cross-protocol consistency).
- Check whether `T_round_end` (referenced in some OBFT variant tables, e.g. L_Bid_New / Appendix E) is actually consumed by anything load-bearing. If not, replace with `T_soft_end` (same role) or drop entirely.

### Implementation plan

After the v4 spec rewrite + implementation adjustments above land, draft a separate impl plan covering:
- Runner-side changes (timing schedule, slot-deadline handling, Instance lifecycle).
- Consensustest adapter updates for the new wire format.
- Integration with the SSV adapter (when built).
- Migration / coexistence testing if v1 and v4 clusters need to interop briefly.

## Execution plan (impl rewrite)

This section captures the concrete plan we agreed for the implementation rewrite. The "v4" codename is purely a planning artifact — it does NOT appear in code, in the rewritten spec (`docs/2abOBFT.md`), in package/type/file names, in comments, or in test names. The implementation is a full rewrite of `protocol/v2/obft/twoab/`; the protocol's external name is just "2abOBFT".

### Confirmed decisions

1. **L_k>0 wire restructure**: full move per §Wire format. L_k>0 chained-σ / NR-plaintext / empty entries live in `Value` / `NoValue` / `Commit-NRDirect` (Phase 2a); Phase-2b `Commit-Signed` / `Commit-NR` carry only the L_0 partial. Larger refactor than pure rename, but produces cleaner code/spec end-state.

2. **ProtocolTag**: drop the `-v1` suffix. New tag is `"2abOBFT"` + 9 NUL bytes (16-byte field). Wire-incompatible with the previous `"2abOBFT-v1"` tag — intentional; no v1/v4 coexistence requirement.

3. **Stresstest variant names**: drop `2abOBFTx2` / `2abOBFTx3` (BTT multiplier variants — no longer needed; the SafetyBuffer knob covers the variation space we actually want to study). Replace with `2abOBFT-tight` (SafetyBuffer = 500ms — lean mesh-tail budget, more MEV-fetch headroom) and `2abOBFT-lean` (SafetyBuffer = 300ms — even leaner, max MEV-fetch headroom at the cost of mesh-tail tolerance). Both variants exercise the SafetyBuffer < RefloodDelay range that's specific to v4.

### Scope 1 — Protocol package (`protocol/v2/obft/twoab/`)

#### `config.go` — timing parameter restructure

- **Drop fields:** `TCommit`, `Delta2a`, `Delta2b`, `Eps3`. (Eps3 is runner/adapter-level CPU reserve in v4, not protocol-level.)
- **Drop helpers:** `TVerdictStart`, `TAcceptMax`, `TVerdictMax`, `Phase2aStartOffset` / `Phase2aEndOffset`, `Phase2bStartOffset` / `Phase2bEndOffset`, `Phase3StartOffset`, `RoundEndOffset`.
- **Add fields:** `TPhase2a time.Duration` (Phase-2a fire-instant), `SafetyBuffer time.Duration` (protocol-level mesh-tail tolerance — replaces `RefloodDelay` in the broadcast-budget formula).
- **Add helpers:** `T0Broadcast()` returns `TPhase2a − BTT`.
- **Update `DefaultBroadcastBudget`** signature: `func DefaultBroadcastBudget(K int, btt, t0Broadcast time.Duration)`. Anchor at `t0Broadcast` (not `tVerdictStart`); formula `B_k_shallow = (k+2)·BTT` for `k ∈ [0, K-2]`, `B_{K-1} = t0Broadcast`. **SafetyBuffer is NOT in this formula** — it lives in the post-Phase-2a cascade window (TPhase2a shift), not in `B_k`. See §Setting "Where SafetyBuffer lives in the timing budget" for the rationale.
- **Update `Validate()`**: drop Delta2a/Delta2b/TCommit/Eps3 checks. Add positivity check on TPhase2a; SafetyBuffer ≥ 0; non-decreasing BroadcastBudget unchanged.

#### `messages.go` — wire-shape redesign

- **Drop:** `Verdict` + `VerdictKind` (and constants), `verdictContentHash`, `Onion2b`, `onion2bContentHash`, `EncryptedLayer`, `NRPartial`.
- **Add:**
  - `Value` (Phase-2a coordination, value-side): `ClusterID, OperatorID, Height, V Value, ValueRoot, LayerEntries []LayerEntry` (K-1 entries for L_1..L_{K-1}).
  - `NoValue` (Phase-2a coordination, no-value side): `ClusterID, OperatorID, Height, LayerEntries []LayerEntry`.
  - `Commit` (Phase-2b binding): `ClusterID, OperatorID, Height, Side CommitSide, L0Partial Signature, L0Value Value` (σ-side only), `LayerEntries []LayerEntry` (only populated when Side==NRDirect).
  - `CommitSide` enum: `CommitSideSigned`, `CommitSideNR`, `CommitSideNRDirect`.
  - `LayerEntry`: `{ Layer int, Kind LayerEntryKind, V Value (σ-side only), Payload []byte }`.
  - `LayerEntryKind`: `LayerEntryEmpty`, `LayerEntrySigmaChained`, `LayerEntryNRPlaintext`.
  - Content-hash helpers per kind for dedup/equivocation detection.

#### `instance.go` — state restructure

- **Drop state:** `ownVerdict`, `peerVerdicts`, `verdictEquivocator`, `peerOnions`, `peerNR`, `peerOnion2bHashes`, `peerFirstOnion2b`, `ownOnion2b`, `l0VerdictReadyCh`, `l0SigmaEligibilityCh`. Drop the four `maybeSignal*` / `*Reached` helpers.
- **Add state:**
  - `valuePool[layer][V_root]map[OperatorID]bool` — σ-direction claims (inferred per §Pool aggregation rules).
  - `noValuePool[layer]map[OperatorID]bool` — NR-direction claims.
  - `sigmaPool[layer][V_root]map[OperatorID]Signature` — actual σ partials (threshold pool, for reconstruction).
  - `nrTagPool[layer]map[OperatorID]Signature` — actual nr_tag_k partials.
  - `peerValue[OperatorID]*Value`, `peerNoValue[OperatorID]*NoValue`, `peerCommit[OperatorID]*Commit` — first-observed message per kind per op (for inference + duplicate/equivocation detection).
  - `peerEmittedNRDirect[OperatorID]bool` — tracks A8 (KindCommit-NRDirect sole-emission).
  - `phase2aFired bool` — gate for `MaybeFirePhase2a()`.
  - `committedLayer []bool` — per-layer "op emitted KindCommit at this layer" (idempotency; EKM locks separate).
- **Drop auth-only retention:** in v4 there's no T_commit hard wall, so the auth-only/regular distinction collapses. All in-slot Phase-1 bundles retained equivalently. Rule 2 still detects equivocation. `retainedBundle.AuthOnly` field dropped. (Simplification.)
- **Per-tick processing**: add `MaybeBuildAndBroadcastUpgrade()` (L_0 only; checks A1 preconditions) and `MaybeBuildAndBroadcastCommit(layer)` (evaluates three triggers in priority order). Per §Emission ordering, callers invoke upgrade-first then commit-check after every state delta. Internally each `Observe*` method calls these in order.

#### `phase1.go`

- Drop auth-only retention semantics. All bundles retained equivalently.
- Drop `observedOffset > TCommit` rejection (`ErrLatePhase1Bundle` → deleted).
- Keep Rule 2 equivocation detection unchanged.
- `RetentionEstablishedAt` field retained for diagnostics (matches v1 idiom).

#### `phase2a.go` — Value/NoValue/NRDirect builder + observers

- Drop: `ComputeLocalVerdict`, `BuildVerdict`, `ObserveVerdict`, `IsEquivocator`, `OwnVerdict`, `PeerVerdicts`, `deepCopyVerdict`.
- Keep: `ApplyHostValidity`, `HostValidity`.
- Add:
  - `ComputeLocalValueState(layer)` → returns one of `KindValue` / `KindNoValue` / `KindCommit-NRDirect` based on retention + host-validity + equivocation.
  - `BuildValue(layer)`, `BuildNoValue(layer)`, `BuildCommitNRDirect(layer)` — the three Phase-2a builders.
  - `BuildUpgradeValue(layer)` — Phase 2a-late upgrade (A1 sequence; only callable on layer 0).
  - `MaybeFirePhase2a()` — fires once per slot at `T_phase_2a`; builds + records the local emission per `ComputeLocalValueState`.
  - `ObserveValue(v *Value)`, `ObserveNoValue(nv *NoValue)` — receiver paths; update pools per inference rules; trigger upgrade-first / commit-check cascade.
  - L_k>0 entry decisions: each Phase-2a emission carries K-1 entries for L_1..L_{K-1}. Entry decision per layer per op: σ-chained if op has V at this layer AND host valid; NR-plaintext if op cannot σ at this layer (no V or host NV); empty at deepest L_{K-1} where no nr_tag exists.

#### `phase2b.go` — three trigger predicates + dynamic emission

- Drop: `BuildOwnOnion2b`, `OwnOnion2b`, `ObserveOnion2b`, `checkRule6b`, `recordVerdictActionEvidence`, `peerSigmaAtL0Verdict`, `retainedL0ValueHashes`, `deepCopyOnion2b`.
- Add:
  - `TryTriggerSigmaEligibility(layer)` → `(fire bool, sideDecision CommitSide, V_root)` — fires when `|value_pool[V_k]| ≥ qV`.
  - `TryTriggerNREligibility(layer)` → `(fire bool)` — fires when `|novalue_pool[L_k]| ≥ qEnc` AND op cannot σ at L_k (the gate).
  - `TryTriggerEquivocation(layer)` → `(fire bool)` — fires when op retained ≥ 2 distinct V_k.
  - `MaybeBuildAndBroadcastCommit(layer)` — evaluates triggers in priority order (equivocation > σ-eligibility > NR-eligibility), emits the appropriate `KindCommit-Signed` / `KindCommit-NR`.
  - `BuildCommitSigned(layer, V)`, `BuildCommitNR(layer)` — actual builders.
  - `ObserveCommit(c *Commit)` — receiver path; updates pools; triggers upgrade-first / commit-check cascade.

#### `convergence.go` — drop 5-row table, add pool aggregation helpers

- Drop: `CommitChoice` enum, `ConvergenceDecision`, `buildConvergencePools`, `chosenVAtLayer`, `RowOfConvergence`.
- Add: pool-aggregation helpers per §Pool aggregation rules:
  - `aggregateValuePool(layer)` — inferred from observed Value/NoValue/Commit per inference rules.
  - `aggregateNoValuePool(layer)`.
  - Inline pool tracking within the Observe* paths (no recomputation; pools incrementally updated on each state delta).

#### `phase3.go` — minor source updates

- `Resolve()` largely unchanged in shape.
- `tryReconstructLayer(0)`: aggregates σ partials from `KindCommit-Signed` messages (was: from `Onion2b.Layers[0]`).
- `tryReconstructLayer(k>0)`: aggregates σ partials from `Value` / `NoValue` / `Commit-NRDirect` `LayerEntries` (was: from `Onion2b.Layers[k]`). Chain-decryption logic unchanged.
- `tryDeriveNextLayerKey(layer=0)`: aggregates from `KindCommit-NR` / `KindCommit-NRDirect` L_0 partials.
- `tryDeriveNextLayerKey(layer=k>0)`: aggregates from L_k NR-plaintext entries in `Value` / `NoValue` / `Commit-NRDirect`, plus L_k NR-plaintext entries in `Commit-NR`/`Commit-NRDirect` if present.

#### `evidence.go` — Rule 6a sequence enumeration, drop 6b

- Rename `EvidenceVerdictEquivocation` → `EvidencePhase2Equivocation`. Payload carries the offending message pair/triple.
- Drop `EvidenceVerdictAction` and `VerdictActionEvidence`.
- Add full A1-A8 NOT-slashable + slashable-sequence catalog per §evidence.go.
- Rules 1-5 unchanged.

#### `validation.go`

- Drop `ValidateVerdict`, `ValidateOnion2b`.
- Add `ValidateValue`, `ValidateNoValue`, `ValidateCommit` (with side-aware checks).
- Keep `ValidatePhase1Bundle`, `ValidateCertificate`.

#### `errors.go`

- Drop: `ErrLatePhase1Bundle`, `ErrOnion2bAlreadyEmitted`.
- Add: `ErrCommitAlreadyEmitted`, `ErrPhase2aAlreadyFired`, `ErrUpgradeNotAvailable`, `ErrUnauthorizedEmissionSequence`.

#### `wire/envelope.go` + `wire/wire.go`

- `ProtocolTag`: change to `"2abOBFT"` + 9 NUL bytes (drop the `-v1` suffix entirely).
- Drop wire kinds: `KindVerdict`, `KindOnion2b`. Drop encoders/decoders, version constants, inner-kind tags for these.
- Add wire kinds: `KindValue` (0x02), `KindNoValue` (0x03), `KindCommit` (0x04). Renumber `KindCertificate` to 0x05. Add encoders/decoders for new shapes (with side flag + variable L_k>0 entries depending on Side).
- Add structural caps for `LayerEntries` array sizing (matches MaxLayers).

### Scope 2 — Consensustest adapter (`protocol/v2/consensustest/twoab/`)

#### `adapter.go`

- Drop `BTTMultiplier` field from `Protocol`.
- Add `SafetyBufferOverride *time.Duration` field. `nil` → default `= cfg.RefloodDelay` (matches OBFT structural budget); non-nil → use this value.
- Rewire timing derivation:
  - `tSoftEnd = RelayCutoff − HeaderSubmitHeadroom − Epsilon3 − Phase3JitterBuffer`
  - `tPhase2a = tSoftEnd − 3·BTT − SafetyBuffer`
  - `t0Broadcast = tPhase2a − BTT`
- Drop the `tCommit` / `tVerdictStart` / `delta2` derivation chain.

#### `events.go`, `byz.go`, `des.go`

- Drop `evtVerdictBroadcastStart` / `evtVerdictArrival` / `evtPhaseTwoBStart` / `evtOnion2bArrival`.
- Add `evtPhase2aFire` (single fire-instant for all ops at `T_phase_2a`) emitting `evtValueArrival` / `evtNoValueArrival` / `evtCommitArrival(NRDirect)` per op's state.
- Add `evtCommitArrival` (Phase-2b dynamic emissions, side flag = Signed/NR).
- Phase-2b emission is dynamic: each `Observe*` callback invokes `MaybeBuildAndBroadcastUpgrade` then `MaybeBuildAndBroadcastCommit` on every state delta, scheduling new arrival events when emissions fire.
- Phase-1 reflood path: when a V-drop op receives V_0 via reflood (`evtPhase1Arrival` past Phase-2a fire), the host-validity update triggers `MaybeBuildAndBroadcastUpgrade` → emits `KindValue` upgrade → schedules `evtValueArrival` to peers.
- `byz.go`: translate adversarial byz scenarios to the new message kinds. Withhold/equivocate/delay primitives stay; the message types change.

#### `stress_test.go`

- Drop `twoabadapter.Protocol{VariantName: "2abOBFTx2", BTTMultiplier: 2}` and `2abOBFTx3` variants.
- Add `twoabadapter.Protocol{VariantName: "2abOBFT-tight", SafetyBufferOverride: ptr(500*time.Millisecond)}` and `twoabadapter.Protocol{VariantName: "2abOBFT-lean", SafetyBufferOverride: ptr(300*time.Millisecond)}`.

### Scope 3 — Makefile

- Update `PROTOCOLS` default: replace `2abOBFTx2,2abOBFTx3` with `2abOBFT-tight,2abOBFT-lean`. Final default: `OBFT,OBFT-RD0,2abOBFT,2abOBFT-tight,2abOBFT-lean,QBFT,QBFT-SSV`.

### Estimated diff size

- ~3500-4500 LoC modified across the protocol package (rewrite of phase2a/phase2b/convergence/messages/wire/evidence/instance + smaller touches to phase1/phase3/validation/errors/config).
- ~600-800 LoC modified across the consensustest adapter (events/adapter/byz/des).
- Test files updated in-place: most existing scenarios still apply with new vocabulary; new tests added for the cannot-σ gate, upgrade-first ordering, mesh-tail recovery, non-conformant sequence detection, h_V_honest=1 + byz-withhold slot-completion via gate.

### Execution order

1. **Config**: timing parameter restructure first (foundational — everything else depends on Config field names).
2. **Wire**: protocol tag change + new message encoders/decoders.
3. **Messages**: new struct definitions.
4. **Errors**: new error vars.
5. **Validation**: new validators.
6. **Instance**: state struct restructure (drops + adds).
7. **Phase 1**: drop auth-only retention.
8. **Phase 2a**: binary value-state builder + upgrade-builder + observers.
9. **Convergence**: drop 5-row table + add pool-aggregation helpers (some merge into Observe* paths).
10. **Phase 2b**: three trigger predicates + dynamic emission.
11. **Phase 3**: pool-source updates.
12. **Evidence**: Rule 6a enumeration, drop 6b.
13. **Tests**: update test files to match new API + add new scenarios.
14. **Build + run unit tests + lint**: green or iterate.
15. **Consensustest adapter**: events/byz/des restructure + adapter rewrite.
16. **Makefile**: stresstest PROTOCOLS update.

## Implementation deviations from this plan

The shipped implementation in `protocol/v2/obft/twoab/` diverges from the plan above in three deliberate ways. Documented here so the plan + impl stay in sync; the impl-side decisions are flagged in the source comments at the call sites.

### 1. `KindValue` carries no leader-auth-sig field

**Plan** (§Wire format / KindValue, §Safety / Structural attack closure): KindValue carries `[1] leader-auth-sig present flag` + `[4] length` + bytes, with the explicit justification that this "cryptographically couples the σ-side claim with V_0 fulltext + leader-auth-sig in one envelope" — closing the Variant-A withhold-then-fake-σ attack at the wire level.

**Impl**: `wire/wire.go` and `messages.go`'s `ValueMsg` omit the leader-auth-sig field entirely. Closure is achieved differently:

1. The outer SignedSSVMessage envelope op-identity-signs the BROADCASTER (the SSV adapter handles this before reaching the protocol layer).
2. The upgrade path (`MaybeBuildAndBroadcastUpgrade` in phase2a.go) requires V_0 to be in `retainedBundles[layer][leaderID]` — populated only by `ObservePhase1Bundle`, which validates the bundle's leader-auth at `ValidatePhase1Bundle` shape time and at the outer envelope-signature check.

Consequence: byz can emit `ValueMsg` envelopes claiming arbitrary fake V_0' values, inflating `value_pool[V_0'_fake]` membership counts. But honest receivers won't have V_0'_fake retained (no leader bundle ever broadcast it), so the commit-side decision never produces a σ partial on V_0'_fake. The σ-pool semantics (threshold partials only, not inferred claim membership) keep the cluster from converging on fake V's. At f=1 n=4, byz alone can't push value_pool[V_0'_fake] past qV=3.

The trade-off: lighter wire format + no separate leader-auth verification path inside KindValue handling, at the cost of relying on layered defense (outer envelope auth + Phase-1 bundle retention) rather than wire-level binding. Acceptable per the Pigeonhole bound.

### 2. Peer-reflood-V via gossipsub Phase-1 reflood only, not via KindValue V extraction

**Plan** (decision #8): "V_0 fulltext kept in `KindValue` (for peer-reflood-V) … KindNoValue-path ops who missed the Phase 1 bundle can recover V_0 from any peer's `KindValue`."

**Impl**: `MaybeBuildAndBroadcastUpgrade` only consults `retainedBundles[layer][leaderID]`. The V_0 inside a peer's `ValueMsg` populates `valuePool[layer][V_root]` but is NOT folded into retention or the upgrade-precondition path.

V_0 reaches a V-drop op only through gossipsub Phase-1 bundle reflood (IHAVE/IWANT delivering the original leader-auth-signed bundle). Without leader-auth-sig in `ValueMsg`, propagating V_0 from observed `ValueMsg` would let any byz broadcaster "synthesize" retention for arbitrary V's — opening an attack vector that the wire-level leader-binding was supposed to close.

Consequence: the catalog scenarios documented at §Liveness worked cases (line 781 h_V_honest=1, line 784 1-1-1 byz equivocation, line 785 2-1 byz-defect) recover only when gossipsub reflood actually delivers the leader's bundle. In direct-delivery test scenarios (no mesh reflood modeled), these cases MISS — which is reflected in the consensustest catalog expectation updates that landed alongside the rewrite (`Equivocate_111`, `Equivocate_SigmaLockedSplit`, `HV1SelectiveDelivery`, etc.).

### 3. Triple-message slashable sequences are unenforceable; pair-only detection in impl

**Plan** (§evidence.go): enumerates `KindNoValue → KindCommit-NR → KindValue` and other 3+-message sequences as slashable.

**Impl**: `Phase2EquivocationEvidence` carries pair-only fields (`{ValueA, NoValueA, CommitA}` and `{ValueB, NoValueB, CommitB}` — no `C` triple). Detection in `Observe*` methods catches only pairs.

Rationale: from a single observer's view, the slashable triple `KindNoValue → KindCommit-NR → KindValue` is **indistinguishable** from the authorized A7 sequence `KindNoValue → KindValue → KindCommit-NR`. Both produce the same set of three observed messages from the same op; gossipsub provides no wire-level emission-ordering metadata that would let the receiver distinguish them. Per §Receiver ordering tolerance, the receiver conservatively defaults to the authorized interpretation. Honest ops are gated against producing the slashable triple at the build path (`MaybeBuildAndBroadcastUpgrade` rejects when `ownCommit != nil`), so the affected case is byz-only, and detection from a single observer's view is impossible without wire-level timestamps. This is documented in-impl at `evidence.go`'s `Phase2EquivocationEvidence` type comment.

The plan's enumeration was overstated. The realistic Rule 6a slashable cases (those actually enforceable at receivers) are:
- Two `KindValue` on different V_0 (also Rule 3 cross-σ-V).
- Two `KindCommit-Signed` on different V_0 (also Rule 3).
- Cross-side commits (`KindCommit-Signed` + `KindCommit-NR`/`NRDirect` from same op — also Rule 1 CrossSigning).
- Two distinct `KindNoValue` (different LayerEntries content).
- `KindCommit-NRDirect` + any other emission from same op (A8 is sole-emission).

## References

- Current spec: [2abOBFT.md](2abOBFT.md)
- Bare OBFT spec: [OBFT.md](OBFT.md)
- OBFTR spec: [OBFTR.md](OBFTR.md)
- BFT comparison: [BFT-comparison.md](BFT-comparison.md)
- Current impl: [protocol/v2/obft/twoab](../protocol/v2/obft/twoab)
- Conversation thread: in-session design discussion (v1 → v2 → v3 → v4 evolution)

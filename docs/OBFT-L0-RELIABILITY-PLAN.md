# OBFT L_0 reliability — peer-reflood V + deepest-broadcast schedule simplification

Two composable design changes to bare OBFT. Together they make L_0's σ-quorum significantly more robust to gossipsub V-drop / selective-delivery patterns while simplifying the per-layer broadcast schedule and giving the deeper-layer fall-through more propagation headroom.

Both compose cleanly with the recent reflood-aware B_k resize, early-commit, and T_commit-tighten work. Neither requires wire-format changes or new cryptographic machinery.

## §1. Peer-reflood V via early commit

### Motivation

Bare OBFT exposes the **h_V=1 selective-delivery deadlock** at L_0: a byzantine leader who delivers V to only 1 honest produces a σ-pool of size 2 (leader's σ_L^V + the 1 honest's σ_i^V) — short of qV=3 at n=4 f=1. The 2 V-drop honest emit NR (no V locally), but NR-pool also caps at 2 < qEnc=3. **Slot misses at L_0 with no fall-through.** Catalogued at [protocol/v2/consensustest/catalog_propagation.go:16](protocol/v2/consensustest/catalog_propagation.go:16) as `HV1SelectiveDelivery` with `OBFT: ExpectMiss`. The only mitigation today is the rational-byzantine deterrent (assumption 4) — across-slot, not per-slot.

[2abOBFT](2abOBFT.md) closes this via the Phase-2a convergence rule (verdict-pool σ-eligibility-quorum-short → flip to NR → fall-through to L_1). The proposal here closes it in *bare* OBFT, at the cost of a modest impl change but no protocol-shape change.

### Mechanism

When an operator early-emits `KindCommit` (per [docs/OBFT.md §Phase 2 emission-timing](OBFT.md)) carrying V at L_0 plaintext + σ_L^V witness, V-drop receivers can:

1. Harvest V plaintext from the sender's σ-onion entry at L_0.
2. Harvest σ_L^V from the sender's witness section, verify against the leader's pubshare on V.
3. Validate V via the host application.
4. σ_i^V on V themselves and include it in their own (later) `KindCommit`.

Net σ-pool gain at L_0 for h_V=1: each V-drop honest contributes a σ_i^V partial they couldn't contribute under the current impl. σ-pool grows from `1 honest σ + leader σ_L^V = 2` to `3 honest σ + leader σ_L^V = 4 ≥ qV=3`. **Slot succeeds at L_0.**

### Wire format — already done

- **V plaintext at L_0** in `EncryptedLayer.Value` ([protocol/v2/obft/base/messages.go:65](protocol/v2/obft/base/messages.go:65)). The σ-side onion at L_0 already carries V.
- **σ_L^V witness** in `Commit.Witnesses` ([protocol/v2/obft/base/messages.go:116](protocol/v2/obft/base/messages.go:116)). Every retained Phase-1 bundle gets a `(layer, leader, value_root, σ_L^V)` triple.

No wire-format change needed.

### Cross-reference logic — partially done

`harvestWitness` ([phase2.go:626](protocol/v2/obft/base/phase2.go:626)) already uses `findVByRoot` ([phase2.go:696](protocol/v2/obft/base/phase2.go:696)) to look up V across both retained Phase-1 bundles AND peer-onion plaintext Value fields. When V is found, σ_L^V is verified and added to `witnessedLeaderSigma`, which Resolve ([phase3.go:135](protocol/v2/obft/base/phase3.go:135)) feeds into the L_0 σ-pool.

**Net contribution to σ-pool today**: leader's σ_L^V (one partial) via peer-V cross-reference. V-drop receivers' own σ_i^V is NOT contributed because the early-commit predicate and `chosenVForLayer` don't consult `peerOnions`.

### Required impl changes — four touch points

All in [protocol/v2/obft/base/](protocol/v2/obft/base/):

1. **Extend `chosenVForLayer`** ([phase1.go:451](protocol/v2/obft/base/phase1.go:451)) to fall back to `peerOnions[layer]` when `bundles[layer]` is empty. Conditions for returning peer-V:
   - **Uniquely-observed across peers** (no equivocation): all `peerOnions[layer][*]` entries carry the same V.
   - **Leader σ_L^V verified for this V**: `witnessedLeaderSigma[layer][ValueRoot(V)]` exists. This gates out byz peers fabricating V's with no valid leader σ.
   - **Host validated V**: `hostVerdict[layer][ValueRoot(V)] == valid`.

   Extension generalizes to all layers in code, but the practical benefit is at L_0 (deeper layers have B_k = T_commit under §2, so selective-delivery at L_k>0 is essentially impossible).

2. **Extend `l0DecisionReady`** ([instance.go:450](protocol/v2/obft/base/instance.go:450)) symmetrically. New shape:
   ```
   if sigmaLocked[0]: true (leader path, unchanged)
   if equivocation observed at L_0: true (NR ready)
     — extended definition: ≥ 2 distinct V's known across (bundles[0] ∪ peerOnions[0] ∪ witnessedLeaderSigma[0])
   if uniquely-retained bundle V_a in bundles[0] AND host validated V_a: true (current σ path, unchanged)
   if uniquely-observed peer-V V_a in peerOnions[0]
      AND witnessedLeaderSigma[0][ValueRoot(V_a)] exists
      AND host validated V_a: true (new σ path via peer-V)
   else: false
   ```
   The equivocation extension covers the case where one V comes via Phase-1 retention and the other via peer-V/witness (today only the bundle-retention equivocation path fires the predicate).

3. **Trigger host validation on first peer-V observation**. Today `ApplyHostValidity` is called by the runner only after observing a Phase-1 bundle. With peer-V, the trigger needs to fire when V first arrives via a peer's σ-onion at L_0.

   Cleanest shape: Instance exposes a new channel `WantsHostValidationCh() <-chan ValidationRequest` where each request is `(layer, V)`. `ObserveCommit` sends a request when a new V is first observed via `peerOnions[layer]` without an existing host verdict AND without an already-pending request for the same V_root. Runner observes the channel and dispatches validation via the host hook, calling `ApplyHostValidity` with the result. Mirrors the existing Phase-1 host-validation flow but decouples the trigger from the message source.

   Channel discipline: buffered (cap ~K), drained on Instance end (`EndInstance` closes the channel). Internal `pendingValidation` map tracks in-flight requests per (layer, V_root) to dedup.

4. **Call `maybeSignalL0Ready` from `ObserveCommit`** ([phase2.go:223](protocol/v2/obft/base/phase2.go:223)) — currently the predicate fires only via `ObservePhase1Bundle` and `ApplyHostValidity`, so peer-V doesn't trigger early-emit even after the predicate extension above. One-line addition at the bottom of ObserveCommit. The signal fires on the equivocation branch immediately (peer-V equivocation = NR ready); the σ-branch fires later via `ApplyHostValidity` once the runner returns the validation verdict.

`BuildOwnCommit` ([phase2.go:26](protocol/v2/obft/base/phase2.go:26)) works as-is — its σ-branch calls `chosenVForLayer(k)`, which after extension (1) returns peer-V when applicable. EKM enforcement (single-σ-V per (slot, layer, value_root)) catches any duplicate-V regression.

### Safety analysis

- **Single-σ-V per (slot, layer) per operator**: EKM-enforced regardless of V's source. Each operator signs at most one V per layer. ✓
- **Pigeonhole 2** (at-most-one σ-quorum per (slot, layer) cluster-wide): depends on what operators sign, not where V was sourced from. Cluster-wide signing log shape unchanged. ✓
- **Cross-phase exclusivity** (σ-XOR-NR per layer): receiver who already emitted NR can't σ later. Early-emit on peer-V must fire BEFORE T_commit fallback — under tightened sizing at Config A this is comfortable (~700ms margin between peer-V arrival ~2900ms and T_commit=3600ms).
- **Equivocation via peer-V**: if peer-V's from different peers expose distinct V's at the same layer, the uniqueness condition in extended `chosenVForLayer` fails → NR per cross-phase exclusivity. Same recovery shape as bundle-equivocation today.
- **Byzantine peer with fake V**: byz peer fabricates V_fake in their σ-onion. σ_L^V on V_fake cannot exist (leader didn't sign it), so `witnessedLeaderSigma[layer][ValueRoot(V_fake)]` stays empty → extension condition fails → peer-V_fake is NOT used. Byz-safe.

### Timing analysis

At Config A K=4 with default RefloodDelay=700ms (post-tighten):
- L_0 broadcast at 2500ms (`T_commit − B_0 = 3600 − 1100`).
- Honest receiver gets V via gossipsub at ~2700ms (one-hop), early-emits with V at ~2700ms.
- V-drop receiver gets sender's early-commit at ~2900ms (one-hop propagation).
- Host validation latency: ~50ms (depends on host; SSV's blinded-block parent-root check is fast).
- V-drop receiver's own early-emit fires at ~2950ms with σ_i^V on V.
- V-drop receiver's commit propagates within 1·BTT, reaching peers by ~3150ms.
- Receivers' Resolve picks up the additional σ_i^V partials well before T_commit + Δ_2 = 3800ms.

Comfortable margin throughout. Even at degraded RefloodDelay/BTT the pipeline fits.

### Spec changes (docs/OBFT.md)

- **§Phase 2 / Emission-timing** — add explicit "Peer-reflood V via early commit" subsection describing the mechanism. Currently the spec covers it indirectly via the σ_L^V witness section + Broadened V-source paragraph; making it explicit as a recovery mechanism is the headline change.
- **§Wire format** — refresh existing prose around `KindCommit`'s onion + witness section. The plaintext V at L_0 + σ_L^V witness combination is the primary V-drop recovery path; KindCertificate gossip is the secondary path (used when σ-quorum couldn't form at all and a faster peer reconstructed).
- **§Failure modes / Equivocation + Class B-grief vectors** — h_V=1 selective-Phase-1-delivery is no longer a slot-miss-without-fall-through. Update from "Class B byz grief vector" to "recovered via peer-reflood V".
- **§Properties summary** — "Byzantine-leader-grief resistance" row tightens (h_V=1 grief vector closes).
- **§Liveness comparison** — h_V=1 table row (if present) flips OBFT outcome from ✗ to ✓.

### Spec changes (docs/BFT-comparison.md)

- No direct changes — h_V=1 is OBFT-specific spec content, not in cross-protocol tables. Cross-scenario failure-mode framing might pick up the "OBFT recovers h_V=1 via peer-reflood V" line in §Adversarial-byz failure modes; review if a row exists.

### Impl-level comment updates

- **[protocol/v2/obft/base/messages.go:82-99](protocol/v2/obft/base/messages.go:82)** `LeaderSigmaWitness` doc-comment currently says "witness alone is unusable; V-drop recovery flows through KindCertificate". Rewrite to describe the peer-V + σ_L^V combination as the recovery path. The KindCertificate path remains as the secondary recovery (when σ-quorum failed and a faster peer reconstructed).

### Test changes (protocol/v2/obft/base/early_commit_test.go)

- `TestL0Ready_PeerVOnly_TriggersReady` — V-drop receiver gets peer commit with V + σ_L^V witness, host validates, L0ReadyCh fires.
- `TestL0Ready_PeerVMissingSigmaL_NoTrigger` — V_fake from byz peer without valid σ_L^V doesn't trigger (byz-safety check).
- `TestL0Ready_PeerVEquivocation_RecordsRule2` — two peers carry distinct V_a / V_b at L_0; equivocation detected, no σ-trigger.
- `TestBuildOwnCommit_PeerVPath_EmitsSigma` — full path: peer commit → harvest V → host validate → BuildOwnCommit emits σ on peer-V.

### Consensustest changes (protocol/v2/consensustest/)

- `catalog_propagation.go` `scenarioHV1SelectiveDelivery`: flip `"OBFT": ExpectMiss` → `"OBFT": ExpectSuccessFastest`. Update the note explaining that bare OBFT now closes this via peer-reflood V.
- Other catalog scenarios unchanged (h_V=1 is the main one affected; equivocation patterns stay as they are because they remain σ-locked).

### Runner changes (protocol/v2/ssv/runner/obft/)

- `runner.go` / `scheduler.go`: select on `WantsHostValidationCh` alongside existing select cases. On a request, invoke the host validity hook and call `ApplyHostValidity` with the result.

### Effort estimate

~200-300 LOC across base + ~50 LOC in runner + ~150 LOC in tests + spec sweep. Non-trivial but contained.

## §2. L_1..L_{K-1} broadcast at BFT_start

### Motivation

The per-layer staggered broadcast schedule trades MEV-fetch headroom against per-layer absorption budget:

| Layer | Broadcast at | MEV-fetch | B_k |
|---|---|---|---|
| L_0 | T_commit − (2·BTT + RD) = 2500ms | ~2350ms | 1100ms |
| L_1 | T_commit − (3·BTT + RD) = 2300ms | ~2150ms | 1300ms |
| L_2 | T_commit − (4·BTT + RD) = 2100ms | ~1950ms | 1500ms |
| L_{K-1} | BFT_start | ~0ms | T_commit = 3600ms |

L_1 / L_2 MEV-fetch budgets are small (~2150 / ~1950ms) and are only meaningful when L_0 fails — which happens in a small fraction of slots under the rational-byzantine deterrent + healthy mesh. The expected-value gain from L_1's marginally fresher MEV vs the deepest-confirmed-parent backup is small.

The proposal: **all L_1..L_{K-1} broadcast at BFT_start with the deepest-confirmed-parent fetch strategy.** Trade L_1+ MEV-fetch (small expected value) for L_1+ absorption budget widening to T_commit (substantial fall-through reliability gain).

### New schedule

| Layer | Broadcast at | MEV-fetch | B_k |
|---|---|---|---|
| L_0 | T_commit − (2·BTT + RD) = 2500ms | ~2350ms | 1100ms (unchanged) |
| L_1..L_{K-1} | BFT_start | ~0ms | T_commit = 3600ms |

Per-layer staggered structure collapses to "primary L_0 + uniform backup ring at BFT_start". Simpler schedule, wider fall-through absorption.

### Trade-off

**Cost**: when L_0 fails, fall-through V is the deepest-confirmed-parent (no MEV from any backup). No graceful MEV-degradation across fall-through cascade.

**Gain**: backup-layer absorption widens to T_commit. Fall-through becomes much more reliable under Class A partition tails / degraded mesh. Schedule simplification: only L_0 has T_broadcast_max < T_commit; all others uniformly broadcast at BFT_start.

### Why this is OK

- L_0 succeeds the vast majority of slots (rational-byz deterrent + healthy mesh, even more so once §1 lands and closes h_V=1).
- When L_0 fails, the value gap "MEV-fresh-L_1 vs deepest-parent-L_1" is dominated by "L_0 failed at all" — incremental MEV from L_1 is small.
- Staker preference: any valid block > missed slot. Fall-through reliability > L_1+ MEV preservation.
- Spec simplification dividend: B_k schedule reduces to "B_0 = 2·BTT + RD; B_1..B_{K-1} = T_commit" — clearer mental model, simpler Validate, simpler tests.

### Composes with §1

§1 makes L_0 dramatically more reliable (closes h_V=1). §2 reduces the cost of L_0 still failing (wider fall-through absorption). Together: L_0 carries virtually every slot's MEV; when L_0 fails (now rare), the cluster falls through cleanly to a deepest-parent V.

### Spec changes (docs/OBFT.md)

- **§Setting / Per-layer leader broadcast deadlines** — B_k formula simplifies. New form: "B_0 = 2·BTT + RefloodDelay (primary, MEV-fresh); B_1..B_{K-1} = T_commit (backups, broadcast at BFT_start with deepest-confirmed-parent fetch)". The reflood-aware formula `(k+2)·BTT + RefloodDelay` applies only at L_0 now.
- **§Application / Timing budget** — V_1, V_2, V_3 broadcast row collapses to "all at slot_start". MEV-fetch table: only V_0 has > 0 MEV-fetch.
- **§Liveness comparison** — fall-through descriptions update ("falls through to V_1 = deepest-confirmed parent").
- **§MEV-freshness ranking** — V_1, V_2 drop off the ranking (≈ 0 MEV); only V_0 carries the BFT-consensus tier.
- **§Properties summary / Partial-synchrony absorption** — update "B_0 = 1100ms primary up to B_{K-1} = T_commit" → "B_0 = 1100ms primary; B_1..B_{K-1} all = T_commit".

### Spec changes (docs/BFT-comparison.md)

- **§MEV-fetch budget table for OBFT** ([line 222-230](docs/BFT-comparison.md)): V_1, V_2 broadcast/MEV-fetch cells collapse to 0ms.
- **§Cross-protocol ranking table** ([line 256-265](docs/BFT-comparison.md)): OBFT V_1, V_2 rows drop from the BFT-consensus tier (still listed at the bottom alongside V_3).
- **§Reading paragraphs** ([line 269-275](docs/BFT-comparison.md)): commentary about deeper-layer fetch budgets simplifies.
- Tables 1a-1e (total-time): **unchanged** — OBFT total time per slot is `3·BTT + RefloodDelay`, not affected by §2 since it's a primary-leader-anchored value.

### Impl changes

- **[protocol/v2/obft/base/types.go](protocol/v2/obft/base/types.go)**: `DefaultBroadcastBudget(K, BTT, RD, T_commit)` simplifies. Drop the multi-tier formula; return `[B_0=2·BTT+RD, T_commit, T_commit, ..., T_commit]`.
- **[protocol/v2/ssv/runner/obft/config.go](protocol/v2/ssv/runner/obft/config.go)**: `defaultLayerSchedules` table simplifies; only L_0 retains a shallow budget. `DefaultBroadcastBudgetSchedule` similarly simplifies. `interpolatedBudgetSchedule` is no longer needed (all backups uniformly = T_commit).
- **`Config.Validate`** ([types.go](protocol/v2/obft/base/types.go)): monotonicity (B_k non-decreasing) still holds trivially. The "B_{K-1} = T_commit" implicit recommendation becomes structural for all backups.
- **`defaultFetchSchedule`**: L_0 retains FetchAt ≈ RANDAO_done; L_1..L_{K-1} all FetchAt = 0 (deepest-confirmed-parent fetch). Strict-decreasing FetchAt convention becomes "L_0 latest, all backups tied at 0".

### Test changes

- `protocol/v2/ssv/runner/obft/config_test.go`: schedule expectations simplify. `TestDefaultBroadcastBudgetSchedule_*` tests update.
- `protocol/v2/consensustest/`: scenarios that rely on L_1 / L_2's staggered budgets may need expectation updates — review `MeshFlakiness`, `AsymmetricPropagation_*`, and any scenario varying B_1/B_2.
- `runner_test.go`: fixture schedules simplify.

### Effort estimate

~100-150 LOC + spec sweep + test updates. Mechanically simpler than §1.

### Backward-compat

No opt-out flag, no preservation of old behavior. The simplified schedule is the new default; the per-layer staggered code paths in `DefaultBroadcastBudget` / `DefaultBroadcastBudgetSchedule` collapse outright. Tests that depended on staggered B_1 / B_2 either get updated expectations or are removed if they tested the staggered shape specifically.

`ConfigOverrides.BroadcastBudget` remains as a generic per-deployment override (existing field, not new). It's not a "preserve old behavior" escape hatch.

## §3. Combined effect

| Aspect | Today (post-tighten) | After §1 | After §1+§2 |
|---|---|---|---|
| L_0 healthy success | ✓ | ✓ | ✓ |
| h_V=1 at L_0 | ✗ (Class B grief) | ✓ (peer-reflood V) | ✓ (peer-reflood V) |
| L_0 silent → L_1 fall-through | ✓ at B_1=1300ms | ✓ at B_1=1300ms | ✓ at B_1=T_commit |
| L_1+ MEV-fetch | ~1750-2150ms | ~1750-2150ms | ~0ms (deepest-parent only) |
| Schedule complexity | per-layer staggered (3 tiers) | unchanged | simplified (L_0 + uniform backup ring) |
| Recovery scope vs 2abOBFT | gap: h_V=1, equivocation-1-1-1 | gap: equivocation-1-1-1 | gap: equivocation-1-1-1 |

After §1+§2, bare OBFT covers more failure modes that previously required 2abOBFT. The remaining 2abOBFT-only recoveries are equivocation σ-locked patterns (1-1-1, etc.) which are byz-driven and out of OBFT's safety scope anyway (slashable evidence).

## §4. Comparison to 2abOBFT

§1 closes h_V=1 in bare OBFT via the same protocol-flow shape 2abOBFT uses (peer observation → cross-reference → late-binding σ commitment). 2abOBFT does this via Phase-2a verdict + Phase-2b convergence; OBFT does this via peer-V + early-commit. **Net cost**: OBFT pays nothing extra in wire format or BTT count; 2abOBFT pays +1 BTT (Phase 2a/2b split + verdict envelopes).

§2 is independently applicable to 2abOBFT (same staggered B_k schedule, same trade-off). Worth a parallel proposal for 2abOBFT after §2 lands for OBFT.

The peer-reflood-V analog for 2abOBFT would be **attaching V to KindVerdict** so V-drop receivers learn V from peer verdicts and σ at Phase-2b. But verdicts are ~200 bytes/layer/op; adding V (~7KB) would substantially inflate the verdict envelope. Diminishing returns since Phase-2a re-flood already covers V-drop in most cases. **Not recommended for 2abOBFT** — its existing mechanisms cover the same ground.

## §5. Execution order

Two separate landings:

1. **§2 first (schedule simplification).** ✅ **Landed.** Schedule defaults changed; no wire change; no protocol-flow change.
2. **§1 second (peer-reflood V).** ✅ **Landed.** Host-validation channel + predicate + BuildOwnCommit + runner integration. h_V=1 deadlock closed in-protocol (production); consensustest framework still uses sync-emit and doesn't yet exercise the recovery path — see §7.

Both compose cleanly with each other and with the recent tighten work.

## §6. Open questions

- **§1 host-validation trigger shape**: async channel-based (Instance → runner via `WantsHostValidationCh`) vs sync callback (host hook embedded in Instance config). Channel is more idiomatic for Instance (currently non-thread-safe; mutex at runner level), sync callback simpler to reason about. **Lean: channel** (matches existing L0ReadyCh / state-delta channel patterns).
- **§1 cross-phase exclusivity timing**: confirm via test that V-drop receiver's L0Ready fires comfortably before T_commit fallback even at degraded BTT (e.g., BTT=600ms). At BTT=600ms, RD=700ms: L_0 broadcast at T_commit − (2·600 + 700) = T_commit − 1900ms. T_commit shifts proportionally (Δ_2 = 600ms post-tighten). Need to verify the timeline still fits.
- **§2 opt-out flag**: agree skip the flag, custom `BroadcastBudget` override is enough. ✓
- **§2 deepest-confirmed-parent fetch semantics for all backups**: should the host application understand that L_1..L_{K-1} all fetch from the same parent (deepest-confirmed), or can each backup fetch from a slightly different parent depth? Spec recommends "deepest" uniformly; host application chooses concrete parent depth. Defer to host config.

## §7. Out of scope for this plan

- L_Bid extension under the new schedule — pending Appendix B re-derivation task (see `OBFT-EARLY-COMMIT-PLAN.md §6`).
- Defer state under the new schedule — same pending task.
- 2abOBFT §2 analog (deepest broadcast schedule simplification for 2abOBFT) — separate proposal once §2 lands for OBFT.
- OBFTR sizing changes — out of scope (no tighten / no peer-reflood-V proposed for OBFTR yet).
- **Consensustest framework upgrade for L0Ready-driven per-op commit emit** — the current `evtPhaseTwoStart` calls `BuildOwnCommit` synchronously for all ops at T_commit, so peer commits arrive after all ops have NR-locked. The §1 peer-reflood-V recovery requires early-emit ordering: the L_0-V-holder emits first, V-drop receivers observe + drain validation + emit later. This is exercised by the focused unit test `TestHV1SelectiveDelivery_PeerVRecovery` in `protocol/v2/obft/base/early_commit_test.go`. A framework upgrade — schedule per-op `evtCommitEmit` events keyed on `L0ReadyCh` firing, with the T_commit fallback as the timeout — would let the consensustest `HV1SelectiveDelivery` scenario exercise the same recovery end-to-end and flip its OBFT expectation from `ExpectMiss` to `ExpectSuccess`. Tracked in the catalog scenario's note. Separate follow-up.

# OBFT implementation — spec deltas

Tracks deltas between [docs/OBFT.md](OBFT.md) and the [protocol/v2/obft](../protocol/v2/obft) implementation (plus the SSV adapter at [protocol/v2/ssv/runner/obft](../protocol/v2/ssv/runner/obft) and the validator setup at [operator/validator/setup_obft.go](../operator/validator/setup_obft.go)).

Findings were originally collected during a faithfulness review and then re-verified against the current spec (post commits `0e8c1e37d` "slashing-evidence change", `75865bd11` "2nd-pass review feedback", `4f501db9f` "lower ε_3 from 100ms to 50ms"). Items the spec changes resolved are dropped; items that remain are listed below with the proposed fix.

## D1 — Rule 1 cross-signing detection is order-dependent (bug)

**Spec.** §Cross-signing detection table line 562: "Immediate (dual partials on the wire)". The two surfaces are σ-from-Phase-1 + NR-from-Phase-2 (leader-specific) and σ-from-Phase-2 + NR-from-Phase-2. The spec doesn't condition detection on arrival order.

**Impl.** [phase2.go](../protocol/v2/obft/phase2.go) `ObserveCommit` fires Rule 1 in two places:
- σ-side branch (line ~402): when a σ onion entry is added and `peerNR[k][op]` already exists.
- NR-side branch (line ~436): when an NR partial is added and either a σ onion entry OR a Phase-1 bundle for the layer's leader already exists.

[phase1.go](../protocol/v2/obft/phase1.go) `ObservePhase1Bundle` has **no** Rule 1 check. Result: if a byzantine layer leader's `KindCommit` (with an NR partial at their own layer) is observed before their Phase-1 bundle — either by deliberate ordering or network jitter — Rule 1 is never recorded even though both halves of the slashable pair are on the wire.

**Fix.** In `ObservePhase1Bundle`, after retention, look up `i.peerNR[b.Layer][b.OperatorID]`; if present and Rule 1 hasn't fired for `(op, layer)`, record `EvidenceCrossSigning` mirroring the existing path in [phase2.go:449](../protocol/v2/obft/phase2.go).

**Test.** Add a unit test variant of `TestObft_Evidence_Rule1_LeaderPhase1SigmaPlusNR` that delivers the byzantine `KindCommit` first and the Phase-1 bundle second; assert Rule 1 evidence is recorded against the leader.

## D2 — Rule 3 leader cross-onion equivocation at L_0 is order-dependent (bug)

**Spec.** Rule 3 detection timing: "Immediate (two σ partials on different V)". For a layer leader specifically, the two σ partials are (a) `σ_L^V(V_a)` from the Phase-1 bundle and (b) `σ_L^V(V_b)` from the L_0 onion entry, with `V_a ≠ V_b`.

**Impl.** [phase2.go:346-363](../protocol/v2/obft/phase2.go) records Rule 3 when an L_0 onion entry from the layer leader differs in `Value` from a retained Phase-1 bundle — but only if the bundle was retained before the onion was observed. When the onion arrives first, the onion entry sits in `peerOnions[0][leader]`; later, when the bundle is retained, `reevaluateL0Sigmas` only fires Rule 5 for `cryptoFake` verdicts. The leader's own onion σ on V_b verifies against the leader's pubshare (verdict = `l0SigmaUnknownV` until V_b is retained, then `l0SigmaVerified` if V_b is retained, or stays `l0SigmaUnknownV` otherwise). No Rule 3 fires.

**Fix.** In `reevaluateL0Sigmas`, when the onion entry's emitter equals the L_0 leader and there is a retained bundle for that leader with `Value` distinct from the onion's `Value`, record `EvidenceCrossOnionEquivocation` for `(leader, layer=0)`. Use the same dedup key as the existing Rule 3 path so the inverse-order case doesn't double-record.

**Test.** New test analogous to `TestObft_Evidence_Rule3_LeaderPhase1SigmaPlusOnion` with reversed delivery order (onion first, bundle second); assert Rule 3 evidence at L_0.

## D3 — Witness section σ_L^V is unused at observation time (spec divergence)

**Spec.** §Phase 2 / Wire format clarifies (post `75865bd11`): "the witness bytes are plaintext at every layer, since they're copies of the leader's Phase-1 partial — not subject to chained encryption … a peer can harvest σ_L^V from `i`'s witness section directly into the layer-`k` σ-pool". Chained encryption gates peer onion σ partials at `k > 0`, not the witness contributions.

**Impl.** `BuildOwnCommit` packs witnesses for every retained bundle ([phase2.go:162-174](../protocol/v2/obft/phase2.go)). `ObserveCommit` silently ignores the witness section ([phase2.go:465-475](../protocol/v2/obft/phase2.go) — the existing comment says "Phase 2 witness observation is a no-op for the σ-pool" and rationalizes via "σ_V co-propagates with V in the Phase-1 bundle"). At Resolve, `tryReconstructLayer` ([phase3.go:93-97](../protocol/v2/obft/phase3.go)) only counts the leader's σ_V from `i.bundles[layer][leaderID]` — never from peer witnesses.

Net effect: a receiver that retained V but not σ_V (e.g. partial-message processing failure, application-level rejection on first observation, peer relaying the bundle in a way that dropped σ_V) cannot recover the leader's σ contribution via witnesses — even though peers shipped it. At `k > 0` the leader's σ_V can never enter the σ-pool except through `bundles[k][leader]`, so V-retained-but-σ_V-dropped receivers underweight the leader's contribution at every fall-through.

**Fix.** In `ObserveCommit`, walk `c.Witnesses` and for each `(layer, leader, value_root, σ_V)`:
1. Cross-reference `value_root` against any V retained in `i.bundles[layer][leader]`. If no match, skip (V-drop — witness is unusable).
2. If a match: verify `σ_V` against the leader's pub-share on the retained V; if valid and not already present in our local witness-derived σ store, retain it.
3. At Resolve, augment `tryReconstructLayer`'s leader-σ_V-loading step to also include witnessed σ_V's seen from peers. Dedup per `(op, V)` so the leader's own σ_V and peer-witnessed copies collapse to one partial in the σ-pool.

**Considerations.**
- Anti-flood: cap retained witness σ_V's per `(slot, layer, leader, value_root)` at 1 (any honest peer's witness yields byte-identical bytes — first-wins dedup is enough; subsequent observations are no-ops).
- Validation-layer integration: `Verifier.VerifyCommitWitnesses` already skips σ_V verification structurally (it can't, without V) — leave that path; the Instance does the V-cross-reference + verify when it has retained V.

**Test.** Two-peer scenario: leader broadcasts bundle to peer A but not peer B. Peer A's `KindCommit` includes a witness for the leader's σ_V. Peer B retains V via a separate path (e.g. via Appendix-C-style re-broadcast in the test harness) but lacks σ_V. Assert peer B's Resolve at this layer reaches qV using the witnessed σ_V.

## D4 — `l0SigmaUnknownV` is never escalated to Rule 5 (spec divergence)

**Spec.** Rule 5 (line 554): "a plaintext σ partial that does not verify against any retained candidate V at that layer (where the receiver has retained at least one such V) … is a slashable byzantine fault. … the receiver can attribute the fault as soon as it observes both `i`'s auth-signed `KindCommit` and any retained candidate V at the plaintext layer."

**Impl.** [phase2.go:478-543](../protocol/v2/obft/phase2.go) `peerSigmaAtL0Verdict` returns `l0SigmaCryptoFake` only if `VerifyPartial(op_pubshare, el.Value, el.Ciphertext)` fails. The `l0SigmaUnknownV` case — where the partial verifies against op's share on op's *claimed* V but op's V is not a retained-leader V — is not fired as Rule 5 ([instance.go:344-361](../protocol/v2/obft/instance.go) Finalize comment explains the deferral).

Per spec, the check is "doesn't verify against any retained V" (verify the partial against each retained V, fire if none match). The impl's check ("does verify against op's claimed V regardless of whether claimed V matches retained") narrows attribution conservatively to avoid false-positives against honest peers reacting to leader equivocation (peer C σ's on V_b after observing the equivocated bundle; receiver R has only V_a retained at that moment — R would falsely flag C). The spec doesn't carve out the equivocation-reaction case.

**Fix.** Two options:

**Option A (spec-literal, narrow).** In `peerSigmaAtL0Verdict`, after the unknownV branch determines `leaderMap` is non-empty AND `el.Value` matches no retained V, additionally require that the op is NOT the layer's leader (the leader-equivocation case is covered by Rule 2 and the σ-on-the-wrong-V is leader-side, not peer-side fake) AND no auth-valid V from the leader matching `el.Value` has *ever* been observed (a peer who σ'd on a V the receiver later retained reclassifies via `reevaluateL0Sigmas`). If both hold, fire Rule 5 immediately.

**Option B (conservative-explicit, document only).** Keep the current behavior, but update spec to acknowledge that legitimate impls may defer Rule 5 attribution in the unknownV case to avoid honest false-positives, and require Rule 5 firing only at slot-end-after-equivocation-window for the unknownV variants. This shifts the divergence from impl to spec.

Recommendation: Option A. The spec's framing ("immediate") is the more useful operational property; the false-positive concern is real but narrower than the current impl makes it.

**Test.** Byzantine non-leader op signs σ on `V_fake` (never broadcast by anyone) while leader L_0 has broadcast V_legit. Honest receiver retains V_legit and observes byz's `KindCommit`. Assert Rule 5 fires against byz at L_0. Companion negative test: under leader equivocation with V_a/V_b, peer C σ's on V_b (the equivocated V), receiver R has retained both V_a and V_b (after Phase-1 retention picks both up via the second-distinct retention path); assert Rule 5 does NOT fire against C.

## D5 — Δ_3 default conflates ε_3 with the residual jitter buffer (numeric/naming)

**Spec.** §Phase 2 line 213 (post `4f501db9f`): "post-`T_commit` budget of 600ms = 3 BTT decomposes as `Δ_2` (400ms) + `Δ_3` (50ms) + `header_submit_headroom` (100ms) + ~50ms residual jitter buffer". `ε_3 ≈ 50ms`.

**Impl.** [config.go:35](../protocol/v2/ssv/runner/obft/config.go) `DefaultDelta3 = 100 * time.Millisecond` and comment "ε_3 ≈ 100ms". The overall budget still derives to `T_commit = 3400ms` because the impl folds the spec's 50ms jitter-buffer into `Δ_3`. So timing is functionally correct, but variable naming and the constant value disagree with the current spec.

**Fix.** Two parts:
1. Lower `DefaultDelta3` to `50 * time.Millisecond` (= spec `ε_3`) and update the comment to drop the "ε_3 ≈ 100ms" claim.
2. Add `DefaultJitterBuffer = 50 * time.Millisecond` constant and include it explicitly in the `T_commit` derivation: `TCommit = RelayCutoff − HeaderSubmitHeadroom − JitterBuffer − Δ_3 − Δ_2`. Default `T_commit` remains 3400ms.

**Test.** Existing `TestConfigForCluster_*` tests should still pass with the same `T_commit = 3400ms`. Add an explicit derivation test asserting the decomposition matches spec.

**Note.** Acknowledged in [docs/OBFT-SPEC-ALIGNMENT-PLAN.md Task 1.1](OBFT-SPEC-ALIGNMENT-PLAN.md) for the consensustest harness; this item applies the same split to the production SSV adapter.

## D6 — Default `BroadcastBudget` fallback when override is unset is uniform 2·BTT (usability)

**Spec.** §Setting line ~45: recommended K=4 schedule `B_0 < B_1 < ... < B_{K-1}` with `B_0 = 1 BTT, B_1 = 1.5 BTT, B_2 = 2.5 BTT, B_3 = 5.5 BTT`. §Setting also says `B_0 ≥ 1 BTT` as the floor.

**Impl.** `obft.Config.Validate` accepts either "all `LayerSpec.BroadcastBudget == 0`" (fallback to single cap `T_commit − 2·BTT` for every layer, [types.go:152-168](../protocol/v2/obft/types.go)) or "all set" (strict-increasing schedule). [setup_obft.go:72-74](../operator/validator/setup_obft.go) wires the recommended schedule for proposer duty, so production deployments are spec-compliant. But the fallback is hit by any caller that constructs a `Config` without explicit `BroadcastBudget` — and the fallback uses a uniform 2·BTT cap for every layer, losing the staggered design entirely.

**Fix.** Make the obft `Config.Validate` REJECT the all-zero fallback (treat it as an explicit error: "BroadcastBudget must be set on every layer"). Move the "default schedule" helper into `obft` as `DefaultBroadcastBudget(K, BTT) []time.Duration` so callers have an obvious-and-correct way to populate it. Update tests / consensustest harness / SSV adapter to use the helper.

Alternative if backwards compat with simpler test configs matters: keep the fallback but issue it as the spec-recommended staggered schedule rather than uniform 2·BTT.

**Test.** Add a `Validate` test asserting that an all-zero `BroadcastBudget` is rejected. Migrate existing tests to use the helper.

## D7 — Stale comments referencing "MUST-gossip per spec" (cosmetic, post spec change)

**Spec change.** Commit `0e8c1e37d` replaced "MUST gossip evidence on the wire" with "honest operators MUST log observed evidence; log format is implementation-defined". The impl's `EvidenceObserver` callback is now spec-compliant (operators log via SSV's zap; out-of-band aggregation is the canonical consumer).

**Impl.** Some files were updated to match the new spec wording ([evidence.go:14-21](../protocol/v2/obft/evidence.go), [instance.go:196-202](../protocol/v2/obft/instance.go), [instance.go:344-361](../protocol/v2/obft/instance.go)). Two files retain stale comments:

- [setup_obft.go:185-201](../operator/validator/setup_obft.go) — comment says "Per spec §Slashing evidence Rule 5, the spec mandates MUST-gossip on the wire" and the WARN log message says "rule MUST-gossip per spec §Slashing-evidence; logged-only impl". The log message is operator-facing and currently incorrectly implies the impl is non-compliant.
- [controller.go:143-148](../protocol/v2/ssv/runner/obft/controller.go) — `EvidenceObserver` field doc says "Spec §Slashing evidence Rule 5 MUST-gossip says receivers MUST gossip evidence on the wire so no-retained-V receivers can also attribute. Logged-only is the impl's substitute …".

**Fix.** Update both comments and the log message to reference the new spec wording ("MUST log observed evidence per spec §Slashing-evidence; log format implementation-defined"). Drop the "logged-only is a deliberate scope choice" framing — it's now the spec contract, not a deviation.

## D8 — Stricter K floor than spec BFT-min (intentional, document only)

**Spec.** §Setting: `K ≥ f+1` is BFT-liveness minimum; `K ≥ f+2` is recommended for late-leader-resilience. Spec allows K=2 at f=1 (BFT-min) but does not recommend it. The implementation rejects K=2 at f=1 entirely.

**Impl.** [types.go:225-232](../protocol/v2/obft/types.go) and [config.go:308-315](../protocol/v2/ssv/runner/obft/config.go) enforce `K ≥ max(3, f+2)`. At f=1, max(3, 3)=3, so K=2 is forbidden. At f=2, max(3, 4)=4, so K=3 (spec BFT-min for f=2) is forbidden.

**Decision.** Keep the stricter floor — spec recommends K ≥ f+2 for everything except the bare BFT-min corner. The comment already cites §Setting; nothing to change in code, but worth recording explicitly here so future reviewers don't re-litigate.

## Items NOT in the diff list (explained)

- **Rule 5 MUST-gossip.** Spec was changed in `0e8c1e37d` to "honest operators MUST log". Logged-only is now the spec contract; impl is compliant. Only stale comments (D7).
- **`localState[K-1]` stays `CommitUndecided` when non-σ.** No on-wire impact (the deepest layer has no NR tag per spec); local diagnostic only; not a spec requirement.
- **`observedTimeOK` uses `<=` not `<`.** Spec says "first-observed past T_commit" = strictly after T_commit. Impl's `<=` is consistent with spec's "past" exclusion. Boundary-inclusive at exactly T_commit is within spec.
- **`RetainedBundles` shares pointers.** API hygiene, not a spec concern.
- **Rule 4 evidence at k > 0 isn't third-party self-contained.** Spec explicitly acknowledges this structural limitation (§Slashing evidence "Rule 4 has a structural detection limit"); the impl matches spec scope.

## Execution order (suggested)

Low-risk, high-confidence first; behavior-changing items behind tests.

1. **D7** (comment / log-message cleanup) — minutes, no behavior change.
2. **D5** (Δ_3 / jitter buffer split) — small, mechanical, regression-tested by existing `T_commit` assertions.
3. **D1** (Rule 1 order-independence) — adds one path in `ObservePhase1Bundle`; small + new unit test.
4. **D2** (Rule 3 order-independence in `reevaluateL0Sigmas`) — adds leader-specific cross-V check; small + new unit test.
5. **D6** (`BroadcastBudget` fallback policy) — touches Config validation; needs a migration sweep of test setups but mechanically clear.
6. **D3** (witness section harvest) — larger; touches `ObserveCommit` retention + `Resolve` pool composition; needs a two-peer test scenario.
7. **D4** (Rule 5 unknownV escalation) — needs the negative test for honest-equivocation-reaction; land last so its behavior shift is isolated.

Each commit should run `make unit-test` for `./protocol/v2/obft/...` + `./protocol/v2/ssv/runner/obft/...` + `./protocol/v2/consensustest/...` + the message-validation suite.

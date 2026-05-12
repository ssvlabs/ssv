# `consensustest` ↔ OBFT.md spec alignment plan

Follow-on to [docs/CONSENSUS-TEST-PLAN.md](CONSENSUS-TEST-PLAN.md). The framework is built; this plan closes the remaining gaps between the suite and [docs/OBFT.md](OBFT.md) identified during the validation pass.

Total estimated effort: ~12–16 hours, split across 12 commits in 4 phases.

## Goals

- Each spec section in OBFT.md that *can* be exercised at the cross-protocol simulation level has a corresponding scenario or assertion.
- Spec parameters (ε_3, Δ_2, B_k, witness sizes) match the spec's numeric values, not approximations.
- Failure-mode classes named in §Failure modes are reachable from scenarios, distinguishing Class A (assumption violation) from Class B (permitted byzantine grief).
- Slashing-evidence Rule 4's structural detection limit is observable (not just the positive path).

## Non-goals

- EKM-level rule enforcement: handled by production unit tests in `protocol/v2/obft/`.
- Wire-format / replay rejection: handled by production wire-validation tests in `protocol/v2/obft/wire/`.
- The "blacklist bitfield" extension: spec marks it as a planned extension; out of scope until specified.
- Real-network integration: framework is virtual-time DES by design.

## Phase 1 — Framework adjustments (foundation)

Land first so phase 2 scenarios can use the new knobs.

### Task 1.1 — Split `Delta3` into `ε_3` and a separate jitter buffer

**Why.** [protocol/v2/consensustest/protocol.go](../protocol/v2/consensustest/protocol.go) line 240 defaults `Delta3 = 100ms`. Spec §Phase 3 / §Timing budget defines `ε_3 ≈ 50ms` (local-CPU Phase 3 cost) plus a separate `~50ms residual jitter buffer` between Phase-3 completion and cert/submit. The conflated constant currently masks drift if either component changes.

**Action.**
- Add `Epsilon3 time.Duration` to `SimConfig` (default 50 ms).
- Add `Phase3JitterBuffer time.Duration` to `SimConfig` (default 50 ms).
- Remove the old `Delta3` field; consumers compute `Delta3 = Epsilon3 + Phase3JitterBuffer` locally where needed (or just split usage).
- Update `T_commit` derivation: `RelayCutoff − Headroom − (Epsilon3 + Phase3JitterBuffer) − Delta2`.
- Wire `obft.Config.Delta3 = cfg.Epsilon3` in `obft/des.go` (production `Delta3` is pure ε_3).

**Accept.** At BTT=200 ms / RelayCutoff=4 s, derived `T_commit = 3400 ms` (unchanged). All existing scenarios still pass.

**Effort.** ~30 min.

### Task 1.2 — Per-leader broadcast-time perturbation knob

**Why.** `DefaultFetchSchedule` fixes broadcast at `T_commit − B_k − BTT/4`. Spec's max-MEV operating point is "broadcast *at* T_broadcast_max" (zero headroom). Current honest leaders always broadcast 50 ms early.

**Action.**
- Add `LeaderBroadcastOffset map[int]time.Duration` to `SimConfig` (per-layer override of fetch buffer; nil → use `BTT/4`).
- Add helper `WithMaxMEVFetch()` for the boundary case (every leader's offset = 0).
- Wire through `obft/des.go` `LayerSpec` so per-layer `FetchAt` honors the override.

**Accept.** New test `TestAdapter_MaxMEVFetch_HealthyAtBoundary`: at BTT=200, offsets all zero, cluster decides at L_0. Bundle arrives at `T_broadcast_max + 1 BTT = T_commit` exactly.

**Effort.** ~45 min.

### Task 1.3 — Late `KindCommit` injection capability

**Why.** Spec §Phase 3 / "Re-running on late KindCommit arrivals" explicitly carves out this recovery path. Framework currently schedules one `evtResolve` at `RoundEndOffset`; later commit arrivals don't trigger a re-resolve.

**Action.**
- Add `internalByz.OverrideOwnCommitDispatchDelay(s, op) time.Duration` method (default 0), symmetric to the existing `OverrideOwnPhase1Delay`. Used to delay an operator's KindCommit emission beyond `T_commit + Δ_2`.
- In `evtPhaseTwoStart`, honor the override for per-op commit dispatch time via `emitToAll`'s new `extraDelay` param.
- Add `evtResolveRerun{op}` event scheduled after each `evtCommitArrival` whose `when` is past `RoundEndOffset` AND the receiver hasn't decided. Production `obft.Instance.Resolve()` is stateless / idempotent given new observed partials.
- Gate behind `SimConfig.EnableLateCommitRerun bool` (default off; 2.3's scenario enables it explicitly).

**Accept.** Existing scenarios unchanged with flag off. Standalone test in 2.3 exercises the new path with flag on.

**Effort.** ~2 h.

### Task 1.4 — Tighten witness size to spec's 145 B/witness

**Why.** `obft/sizes.go` computes `4 + 8 + 32 + 96 = 140 B`. Spec quotes ~145 B including length-prefix overhead.

**Action.** Add `WitnessFramingOverhead = 5` constant in `obft/sizes.go`; include it in per-witness size; reference OBFT.md §Phase 2 / wire format in the comment.

**Accept.** `TestBandwidth_Healthy_OBFT` still passes (loose < 30 KB). Per-witness math now matches the spec.

**Effort.** ~10 min.

## Phase 2 — High-priority scenario additions

Each adds 1–2 named scenarios. After this phase, catalog grows from 21 → ~26 scenarios.

### Task 2.1 — Validity-divergence widened by passive byz f-budget

**Why.** Spec §Failure modes / Validity-divergence deadlock enumerates four configurations that slot-miss under `re-org rate × byz-passivity-rate`. Currently only the all-honest 2-2 / 1-3 / 3-1 patterns are tested.

**Action.** Add three scenarios to `catalog.go` after `scenarioValidityDivergenceNRFallThrough`:

- `scenarioValidityDivergence_PassiveByz_Silent_2NV` — host NV on 2 non-leader honest + byz silent.
- `scenarioValidityDivergence_PassiveByz_Silent_1NV` — host NV on 1 non-leader honest + byz silent.
- `scenarioValidityDivergence_PassiveByz_SigmaOnV_2NV` — host NV on 2 non-leader honest + byz σ-on-V (non-leader byz that emits σ at L_0).

Each generalizes via `cfg.F()` so configurations hold at n ∈ {7, 10, 13}.

Expected outcomes per spec:
- OBFT: MISS (all below qV / qEnc).
- QBFT: `ExpectSuccessOrMiss` (round-2 fresh-V validity host-dependent).

**Accept.** Scenarios land in `Catalog`; `TestComparison_Matrix` asserts expectations; `TestSweep_FullCatalog_LargerN` verifies n>4 generalization.

**Effort.** ~2 h.

### Task 2.2 — Rule-4 sealed-when-chain-stays-locked property

**Why.** Spec §Slashing evidence / Rule 4 has a structural limit: evidence stays sealed when NR-quorum doesn't reach at all prior layers. Current `scenarioFakeEncryptedPresence` only exercises the positive path.

**Action.** Add standalone test `TestAdapter_FakeEncryptedPresence_StaysSealed_WhenL0Decides` in `obft/adapter_test.go`:
- Byz=op2 fakes encrypted-presence at L_2 (op2 leads L_1 by default rotation; faking is at a *different* layer than they lead).
- L_0 leader honest → cluster decides at L_0.
- Assert `EvidenceByRule["OBFT/Rule4/FakeEncryptedPresence"] == 0` for every honest receiver.

**Accept.** Test passes. Spec's Rule 4 surface-ability limit becomes an in-suite property.

**Effort.** ~45 min.

### Task 2.3 — Late `KindCommit` re-resolve scenario

**Why.** Validates Task 1.3's framework support against a real recovery scenario.

**Action.** Add `TestAdapter_LateCommitArrival_ReResolve` in `obft/adapter_test.go`. Concrete setup:
- One honest (op2) host-NV at L_0 (σ-pool reduces).
- Byz (op4) silent at L_0 (no σ contribution).
- σ-pool = leader + op3 = 2 partials by `T_commit + Δ_2` — short of qV=3.
- Delay op3's KindCommit by 200 ms past `T_commit + Δ_2`.
- Without re-resolve: miss (witnessed σ_L^V on op2 unused).
- With re-resolve: late op3 commit pushes σ-pool to qV → decide at L_0.

Set `SimConfig.EnableLateCommitRerun = true` in the test; flip default in this commit.

**Accept.** Test passes only with re-resolve enabled. Removing 1.3's re-schedule logic fails the test.

**Effort.** ~1 h on top of 1.3.

## Phase 3 — Medium-priority scenarios

### Task 3.1 — Mesh-flakiness deadlock scenario

**Why.** Spec Properties summary row "Mesh-flakiness tolerance: Limited" — a flaky honest who NR-emits incorrectly + byz σ-refusal → deadlock. Currently `TestSweep_Jitter` at jitter=200 ms exercises this implicitly but no named scenario.

**Action.** Add `scenarioMeshFlakiness` to `catalog.go`. Compose: per-receiver delay model that adds 1.5×BTT inbound delay for op2 + `ByzSigmaRefusal` on op4.

Outcome: σ-pool = leader + 1 honest = 2 < qV; NR-pool = 1 (flaky) + 0 (byz never NRs) = 1 < qEnc → miss at L_0; may fall through to L_1.

**Accept.** Catalog scenario landed; outcome class matches spec; generalization at n>4 verified.

**Effort.** ~1 h.

### Task 3.2 — Document `MinK = max(3, f+2)` policy — **SUPERSEDED**

**Status.** Superseded by [K2-FIRST-CLASS-PLAN.md](K2-FIRST-CLASS-PLAN.md). The K=f+1 floor is now the spec mandate; the `max(3, f+2)` hard floor was removed and the late-leader-resilience choice was returned to operators per spec §Setting. `MinK` returns `f+1`.

### Task 3.3 — Optional: honest leader without σ_V

**Why.** Spec §Phase 1 wire validation MUST. Production tests cover it.

**Decision.** Skip at consensustest layer (low ROI vs production unit tests). Note in plan completion summary.

**Effort.** 0 (skipped) or ~30 min if pursued.

## Phase 4 — Hygiene

### Task 4.1 — Per-band bandwidth assertions

**Why.** Current test asserts `total < 30 KB`. Spec quotes ~7 KB/op (3 KB onion + 580 B witnesses + NR partials + auth). Without per-band assertions, single-component regressions are invisible.

**Action.** Add per-band assertions in `TestBandwidth_Healthy_OBFT`:
- Per-op witness ≈ 580 B (16 witnesses × ~145 B / 4 ops; loose ±20%).
- Per-op onion bytes loose ±20% of K × per-layer contribution.

**Effort.** ~30 min.

### Task 4.2 — Disambiguate QBFT `ExpectSuccessOrMiss`

**Why.** `scenarioValidityDivergenceAlgebraicLimit` is the only `ExpectSuccessOrMiss` cell. Looseness can hide intermittent QBFT bugs.

**Action.** Run the scenario at 10 seeds via existing seed sweep; if QBFT outcome is identical at every seed, tighten the expectation. Otherwise document the non-determinism.

**Effort.** ~20 min.

### Task 4.3 — Catalog generalization regression test

**Why.** `TestSweep_FullCatalog_LargerN` asserts every catalog scenario matches its n=4 expectation at n>4. Depends on Apply functions scaling correctly. New scenarios with hardcoded operator IDs only crash here when someone runs the test.

**Action.** Add `TestCatalog_AllScenariosGeneralized` in `matrix_test.go`: iterate catalog, run Apply for each cluster size, verify (a) no panic, (b) generated byz IDs within `[1, N]`, (c) ByzOperators length ≤ F.

**Effort.** ~30 min.

## Sequencing and rollout

Suggested commit order:

1. **Phase 1:** 1.4 → 1.1 → 1.2 → 1.3 (1.3 + 2.3 can land together).
2. **Phase 2:** 2.1 → 2.2 → 2.3 (with 1.3's flag default flipped).
3. **Phase 3:** 3.1 → 3.2. (3.3 skipped.)
4. **Phase 4:** 4.1 → 4.2 → 4.3.

Per commit: run `make unit-test` filtered to `./protocol/v2/consensustest/...` plus `TestSweep_FullCatalog_LargerN`. Real-BLS suite (`make consensustest-real-bls`) should also pass after each phase.

Highest-risk task: 1.3 (late-commit re-resolve) — touches the DES event loop. Behind a default-off config flag until 2.3 lands.

## Net result after execution

- 21 → ~26 catalog scenarios.
- 4 new framework knobs: `Epsilon3`, `Phase3JitterBuffer`, `LeaderBroadcastOffset`, `EnableLateCommitRerun`.
- 3 new standalone adapter tests (max-MEV boundary, late-commit re-resolve, Rule-4-stays-sealed).
- 3 new per-band bandwidth assertions.
- 1 catalog-generalization regression test.
- 1 documentation comment clarifying the K-floor policy.

## Explicitly deferred / out of scope

- EKM-level enforcement scenarios (production unit tests).
- Cross-cluster / cross-slot replay rejection (production wire-validation tests).
- Blacklist bitfield extension (spec marks as planned, not yet specified).
- Real-network NetworkModel (framework is virtual-time DES by design).
- LateEquivocate111 — current `byzEquivoc111` already covers slot-miss class; near-end-of-Phase-1 timing precision is marginal benefit.

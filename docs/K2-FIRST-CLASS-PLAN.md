# K=2 First-Class Citizenship Plan (OBFT + 2abOBFT)

## Goal

Lift the hard `K ≥ max(3, F+2)` floor in OBFT/2abOBFT down to the BFT-liveness minimum `K ≥ F+1`. Treat K=2 (at F=1) as a first-class supported configuration in both spec and implementation. Leave the K choice to the operator; do not bake a "recommended" K into the spec.

## Decisions

- **Floor**: `K ≥ F+1` (BFT-liveness minimum, per spec). No `max(3, …)` floor in either protocol's `Validate()`.
- **No prescriptive recommendation** in spec docs. Spec describes the *factual* tradeoffs between K = F+1 and K ≥ F+2; choice is deployment-dependent.
- **SSV adapter** keeps K=4 as its proposer-duty default (deployment choice, not spec choice). K=2 becomes an opt-in adapter tuning.
- **No changes to TLA+ specs or .cfg files** — K=2 is already exercised as the SAFETY base case for both bare OBFT and L_Bid/L_Bid_New.

## Findings backing the change

### Formal verification

- bare OBFT SAFETY at `n=4, f=1, K=2`: ✓ verified (262,144 distinct states, no counterexamples) — `OBFT-formal-verif.md:711`
- L_Bid (2ab) SAFETY at K=2: partial coverage at 76.8M states, no counterexamples — `OBFT-formal-verif.md:721-722`
- L_Bid_New (2ab) SAFETY at K=2: partial coverage at 78.8M states, no counterexamples — `OBFT-formal-verif.md:732-733`
- LIVENESS_NON_GRIEF at K=2 with relaxed Assumption 2 found the documented Class A partial-propagation deadlock — this is the *exact* pathology spec calls out, and it is **out of recovery scope by design**.

Conclusion: no proof relies on K ≥ 3; safety is mechanically verified at K=2.

### Audit

Implementation surface to touch (file:line refs are starting points, not exhaustive):

- `protocol/v2/obft/base/types.go:337-344` — `Validate()` floor in bare OBFT
- `protocol/v2/obft/twoab/config.go:354-361` — `Validate()` floor in 2abOBFT
- `protocol/v2/consensustest/protocol.go:146-166, 333-335` — consensustest `MinK()` and config Validate floor
- `protocol/v2/consensustest/schedule.go:14, 35-37` — `DefaultBkSchedule` K<3 rejection
- `protocol/v2/ssv/runner/obft/config.go:53-56, 104-116, 282-294, 380-389` — `MinKFloor=3` constant, missing K=2 entry in `defaultLayerSchedules`, schedule helpers, adapter Validate
- `protocol/v2/obft/base/sim_test.go:37-46` — `newSim()` panics on K<3
- `protocol/v2/obft/base/config_test.go:43-46` — `TestConfig_Validate_RejectsKTooSmall` asserts K=2 rejection (must flip)
- `protocol/v2/obft/twoab/config_test.go:66-70` — `TestConfig_Validate_RejectsKBelowLateLeaderResilience` asserts K=2 rejection (must flip/rename)
- `docs/OBFT.md:30-36` — spec wording with "strongly recommended" K ≥ f+2
- `docs/2abOBFT.md:30-36` — same wording

Error message strings mentioning "max(3, f+2)" exist at the four Validate sites and need updating.

## Implementation steps

### Step 1 — base + twoab: lift floor, update error messages, flip tests

- `base/types.go`: change `Validate()` to `minK := F+1` (drop the `if minK < 3 { minK = 3 }` block); update error message wording to reference the BFT-liveness minimum, not late-leader-resilience.
- `twoab/config.go`: same change, mirrored.
- `base/config_test.go`: repurpose `TestConfig_Validate_RejectsKTooSmall` — now asserts K=1 rejected at f=1, K=2 accepted. Rename if helpful.
- `twoab/config_test.go`: same shape; rename `TestConfig_Validate_RejectsKBelowLateLeaderResilience` → `TestConfig_Validate_RejectsKBelowBFTLivenessMinimum`.
- `base/sim_test.go`: drop the `K < 3` panic in `newSim()`; allow K=2.
- Add positive Validate test at K=2,f=1,n=4 in each of base/twoab.

### Step 2 — consensustest harness + SSV adapter: lift floor, add K=2 schedule entry

- `consensustest/protocol.go`: change `MinK()` to return `f+1`; update doc comments and Validate error message.
- `consensustest/schedule.go`: drop `K < 3` rejection in `DefaultBkSchedule`; ensure K=2 returns `[1·BTT, T_commit]` (or whatever the existing K=2 branch in protocol-level `DefaultBroadcastBudget` returns).
- `ssv/runner/obft/config.go`:
  - lower `MinKFloor` to 2 (or just remove it entirely if we standardize on `F+1`)
  - add K=2 entry to `defaultLayerSchedules` map: `{fetchAt: [primaryFetchDefault, 0], shallowBudgetBTT100: [100]}` so the SSV adapter has explicit K=2 defaults rather than relying on the interpolation fallback
  - update the doc comment that says "K=2 is intentionally absent"
  - update `Validate()` error message wording

### Step 3 — spec docs: OBFT.md + 2abOBFT.md rewording

Replace the K-recommendation paragraphs at `OBFT.md:30-36` and `2abOBFT.md:30-36` with the factual-tradeoffs rewording (draft below). Also scan for downstream references that quote the "K ≥ f+2 strongly recommended" framing and reconcile.

Draft replacement (applied uniformly to both specs):

> **K layers** (`f+1 ≤ K ≤ n`, configurable) with deterministically-derived distinct leaders L_0 … L_{K-1}.
>
> - `K ≥ f+1` is the **BFT-liveness minimum** — pigeonhole over the f-byz bound guarantees ≥ 1 honest leader. At K < f+1, all leaders could be byzantine and no σ-quorum reaches at any layer.
> - At `K = f+1`, a single late-broadcasting honest leader can foreclose the slot via the deepest-layer NR-lock pathology (only one honest leader exists, no fall-through depth). `K ≥ f+2` lifts this by guaranteeing ≥ 2 honest leaders, providing late-leader-resilience at the cost of one additional layer's leader-broadcast budget.
>
> Choice of K is deployment-dependent — clusters with low-tail propagation, tight operator SLAs, or no expected Byzantine presence may prefer smaller K (fewer leaders, simpler Phase 3 walk, smaller chained-encryption depth). Clusters operating closer to the partial-synchrony tail or with adversarial-byz tolerance as a hard requirement may prefer K ≥ f+2 or K = n.

### Step 4 — K=2 first-class tests

- Sim happy-path test at K=2 in both `base` and `twoab` packages (mirroring the existing K=4 sim shape).
- consensustest catalog: add a K=2 row so reporting tables include K=2 behavior (only at n=4,f=1 since at higher f the BFT-min is already ≥ 3).

## Out of scope / no-op surface

- TLA+ specs and `.cfg` files — already exercise K=2.
- `DefaultBroadcastBudget` K=2 branches in `base/types.go` and `twoab/config.go` — already correct, just become reachable.
- `OBFT-formal-verif.md` — only describes findings, no prescriptions to remove.
- OBFTR (multi-round) — not touched.

## Commit split

1. **base + twoab**: lift floor, update error messages, flip-and-add Validate tests + positive K=2 sim test.
2. **consensustest + SSV adapter**: lift floor, add K=2 schedule entry, update tests.
3. **spec docs**: OBFT.md + 2abOBFT.md rewording (consistent application across both).
4. **K=2 first-class tests**: consensustest catalog K=2 row + any remaining test additions.

## Verification

- `make lint` and `make unit-test` clean after each commit.
- New K=2 positive tests pass.
- All previously-passing K≥3 tests still pass (regression check).

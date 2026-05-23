# QBFT / QBFT-SSV variant split — implementation plan

## Motivation

The consensustest QBFT adapter currently exposes a single `Protocol` struct whose behavior is mode-switched by flags (`UseFixedRT`, `FixedRT`, `VariantName`, `BTTMultiplier`). After making the computed-RT variant a true pristine structural floor (per-round `R1 = 3·BTT`, `R≥2 = 4·BTT`), the two variants now behave fundamentally differently:

- **QBFT** (pristine floor) — RT computed from BTT, no mesh-tail cushion. The zero-margin reference for the stress comparison.
- **QBFT-SSV** (production) — flat 2s round timeout, mirrors SSV's `QuickTimeout`.

The flag-based coupling is confusing and produces a concrete bug: both variants report `Name() == "QBFT"`, so the catalog's per-scenario expectation table can only hold one `"QBFT"` row — which cannot satisfy both a pristine run (misses MeshFlakiness) and a cushioned run (succeeds). 

**Goal:** make QBFT and QBFT-SSV independent, first-class variants — distinct types, distinct names, each with its own correctness coverage and stress-suite registration — and delete the mode-switch flags. Construct the one you mean directly.

## Foundation already in place

The behavioral change is done and verified (QBFT package tests green):

- Per-round RT — `R1 = 3·BTT`, `R≥2 = 3·BTT + 1·BTT` (the round-change hop to the new leader) — in `qbft/adapter.go` + `qbft/timer.go`.
- Event-queue tie-break — at equal virtual time a decision/arrival sorts before the round-timeout, so a round that decides exactly when its timer fires is not spuriously round-changed — in `qbft/events.go` (`evtRoundTimeout` already no-ops once decided).

This plan restructures the *type surface* around that behavior; it does not change the timing logic.

## Target design

Two zero-field exported types in package `qbft`:

```go
qbft.QBFT{}      // Name() == "QBFT"      → pristine per-round RT (3·BTT / 4·BTT)
qbft.QBFTSSV{}   // Name() == "QBFT-SSV"  → flat 2s RT
```

Both implement `ct.Protocol` (`Name()` + `Run()`). They share all internal machinery — DES, byz translation, deadline clip, outcome/attestation — via one unexported helper that takes the RT policy as a parameter; only the ~10-line RT derivation differs. "Independent" means independent identity / tests / registration, not a duplicated stack.

### RT policy

- `QBFT`: `rt = 3·bttEff`, `rtRecoveryExtra = 1·bttEff` (applied by timer.go for rounds > FirstRound).
- `QBFTSSV`: `rt = 2s`, `rtRecoveryExtra = 0` (flat; 2s already dwarfs the per-round structural cost).

### Catalog expectations

`ExpectFor` keeps its variant fallback (`variantBase("QBFT-SSV") == "QBFT"`). So:

- `"QBFT"` rows hold the **pristine** expectation.
- An explicit `"QBFT-SSV"` row is added **only where cushioned diverges from pristine** (non-divergent scenarios fall back to `"QBFT"`).

The existing `"QBFT"` rows were implicitly calibrated for cushioned behavior, so each must be re-verified against pristine; where they differ, the row flips to the pristine outcome and a `"QBFT-SSV"` override carries the cushioned outcome. Known divergence so far: **MeshFlakiness** (`"QBFT"` → `ExpectMiss`, `"QBFT-SSV"` → `ExpectSuccessFastest`). The full set is enumerated by running both variants through the catalog.

## Decisions

- **Type names:** `QBFT` / `QBFTSSV` — direct mapping to the chart labels (`qbftadapter.QBFT{}` at call sites). Trivially renamable if preferred.
- **Drop all four flags:** `UseFixedRT`, `FixedRT`, `VariantName`, `BTTMultiplier`. Survey confirms `BTTMultiplier` is never used by QBFT, `FixedRT`'s configurable value is used by exactly one test, and `VariantName`/`UseFixedRT` only ever spelled "QBFT-SSV".
- **`MaxRounds` → internal constant** (= 4); never overridden externally.
- **Scope is QBFT-only.** OBFT / 2abOBFT / PSigs keep their `VariantName`-based variant patterns unchanged.
- **Shared internals**, per above.

## Implementation steps

1. **Refactor `qbft/adapter.go`** — replace `Protocol` with `QBFT` and `QBFTSSV`; extract an unexported `run(cfg, rtBase, rtRecoveryExtra)` (or an `rtPolicy`) helper carrying today's `Run` body; delete the four flag fields; `MaxRounds` → package const. Move/curate the per-variant doc comments onto each type.
2. **Update call sites** (qbft adapter is used only within the consensustest tree):
   - `stress_test.go` — `qbftadapter.QBFT{}` + `qbftadapter.QBFTSSV{}`.
   - `sweep_test.go`, `mesh_sweep_test.go` — `qbftadapter.QBFTSSV{}`; refresh comments that reference `UseFixedRT`.
   - `correctness_test.go` — register **both** `QBFT{}` and `QBFTSSV{}` (each first-class).
   - `qbft/adapter_test.go`, `qbft/crash_test.go` — `QBFT{}` for pristine cases.
3. **Recalibrate `TestAdapter_PostConsensusQuorumMiss`** onto `QBFTSSV{}` (the configurable `FixedRT` goes away). If the slow-receiver scenario can't be staged at a flat 2s (slow ops may also decide), restage it — e.g. via crashed post-consensus signers to force `< 2f+1` partials. Verify it still pins the intended semantic.
4. **Catalog expectations** — run `QBFT{}` and `QBFTSSV{}` over the full `ModeCorrectness` catalog, diff outcomes; for each divergence set `"QBFT"` to the pristine outcome and add a `"QBFT-SSV"` override. Update the affected scenario `Note`s to current-state (no dev-history).
5. **Verify** — `go build ./...`; `go test ./protocol/v2/consensustest/qbft/`; `TestCorrectness` (both variants); `TestSweep_FullCatalog_LargerN` and the QBFT/QBFT-SSV sweep assertions; `gofmt` / `make format`.

## Out of scope (follow-on, already tracked)

- Regenerating the stress report (`data.js` / the chart) so the UI reflects the pristine floor.
- Doc updates (`BFT-comparison.md`, `OBFT.md`) for the pristine RT convention.

These run *after* this refactor lands, against the final code + regenerated numbers.

## Risks / open items

- **Divergence-set size** is unknown until enumerated (step 4). At n=4 / ConstantDelay only MeshFlakiness diverged; jitter/larger-n may surface more (the LargerN sweep already anticipated "marginal MeshFlakiness-class" shifts).
- **PostConsensusQuorumMiss stage-ability** at flat 2s (step 3) — may require the crash-based restage.

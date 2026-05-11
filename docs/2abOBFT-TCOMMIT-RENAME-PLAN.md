# 2abOBFT T_commit Rename Plan

**Status**: EXECUTED. Retained as a project artifact for review-trail context.

**Final decisions taken before execution** (overriding plan defaults where applicable):
- Phase-1/Phase-2a boundary named **`T_verdict_start`** (not `T_p1_end`).
- Phase 2a range styled as `[T_verdict_start, T_commit]` (named cutoffs at both ends).
- Symbol-glossary row added for `T_verdict_start`.
- "post-T_commit" framing follows the new semantics consistently (so "1050ms post-T_commit" → "650ms post-T_commit"; pre-T_commit Phase 2a is described separately where needed).
- BFT-comparison.md anchor bullet reframed: drop the "anchors not comparable" disclaimer (no longer true post-rename), keep the per-protocol wall-clock anchor values as informational reference.
- No heritage notes — spec is forward-looking.

## Background

In OBFT, OBFTR, and QBFT, `T_commit` denotes the *cryptographic commit point* — the time at which an operator emits their σ-or-NR partial and becomes irrevocably bound. It is the "point of no return" in the protocol's safety argument.

In 2abOBFT (current spec) `T_commit` denotes the *Phase-1 broadcast cutoff* — the boundary between Phase 1 (fetch + broadcast) and Phase 2a (verdict broadcast). The actual cryptographic commit event happens at `T_commit + Δ_2a` (start of Phase 2b).

This is a terminology bug. It forces readers to context-switch when comparing 2abOBFT against the rest of the OBFT family, and it shows up as a real impedance mismatch in [BFT-comparison.md:19](BFT-comparison.md#L19), which has to explicitly disclaim "Per-protocol T_commit anchors differ" because the protocols measure different events under the same name.

## What the rename does

Reassign `T_commit` to the σ-or-NR commit event (start of Phase 2b), consistent with OBFT and QBFT. Introduce a new name for the Phase-1/Phase-2a boundary that is currently called T_commit.

### Naming for the Phase-1/Phase-2a boundary

**Recommendation: `T_p1_end`** ("Phase 1 end"). Reasoning:

- Matches the existing pattern of T_<descriptor> (T_broadcast_max, T_accept_max, T_verdict_max).
- Reads naturally in phase-range expressions: "Phase 1: `[slot_start, T_p1_end]`".
- Concise (avoids "T_phase1_end_boundary" verbosity).
- The semantic is asymmetric (it's both "end of P1" and "start of P2a"), but P1's end is the load-bearing semantics for the spec's deadlines (T_broadcast_max relative to it, T_accept_max relative to T_commit).

**Alternatives considered:**
- `T_verdict_start`: emphasizes Phase 2a's start; reads slightly oddly in "Phase 1: [slot_start, T_verdict_start]"
- `T_obs_start`: similar issue; "observation" not used as the protocol term elsewhere
- No name, inline as `T_commit − Δ_2a`: works in formulas but loses the named-cutoff convention

If the user prefers `T_verdict_start` or another name, change before execution; the rest of the plan is independent.

## Confirmed scope

| File | Change |
|---|---|
| [docs/2abOBFT.md](2abOBFT.md) | Yes — primary edit pass (39 `T_commit` refs, many time-formula occurrences) |
| [docs/2abOBFT-design-notes.md](2abOBFT-design-notes.md) | Yes — 19 `T_commit` refs, mostly scenario walkthroughs |
| [docs/BFT-comparison.md](BFT-comparison.md) | Yes — single bullet at line 19 derives per-protocol T_commit anchors; update 2abOBFT's value from ≈ 1600ms to ≈ 2000ms and reframe "post-T_commit budget" |
| [docs/OBFT.md](OBFT.md) | No — 183 refs all use OBFT semantics (T_commit = crypto commit); no change |
| [docs/OBFTR.md](OBFTR.md) | No — 53 refs all OBFTR-semantic |
| [docs/OBFT-formal-verif.md](OBFT-formal-verif.md) | No — 13 refs all OBFT-semantic |
| [protocol/v2/...](../protocol/v2/) | No — bare-OBFT impl uses OBFT semantics consistently; no 2abOBFT impl yet |
| [protocol/v2/consensustest/](../protocol/v2/consensustest/) | No — references are OBFT-context |

## Time-formula transformation table

This is the canonical translation table. Every appearance of the left form in 2abOBFT.md and 2abOBFT-design-notes.md becomes the right form.

| Old (current spec) | New (after rename) |
|---|---|
| `T_commit` (= Phase-1 cutoff) | `T_p1_end` |
| `T_commit + Δ_2a` (= Phase-2a end / Phase-2b start / σ-or-NR commit) | `T_commit` |
| `T_commit + Δ_2a + Δ_2b` (= Phase-2b end / Phase-3 start) | `T_commit + Δ_2b` |
| `T_commit + Δ_2a + Δ_2b + Δ_3` (= reconstruction target) | `T_commit + Δ_2b + Δ_3` |
| `T_accept_max = T_commit + Δ_2a − 1 BTT` | `T_accept_max = T_commit − 1 BTT` |
| `T_verdict_max = T_commit + Δ_2a − 1 BTT` | `T_verdict_max = T_commit − 1 BTT` |
| `T_broadcast_max = T_commit − 2 BTT` (= 2 BTT before P1 cutoff) | `T_broadcast_max = T_p1_end − 2 BTT` |
| `[slot_start, T_commit]` (Phase 1 range) | `[slot_start, T_p1_end]` |
| `[T_commit, T_commit + Δ_2a]` (Phase 2a range) | `[T_p1_end, T_commit]` *(or equivalently `[T_commit − Δ_2a, T_commit]`)* |
| `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]` (Phase 2b range) | `[T_commit, T_commit + Δ_2b]` |
| "from `T_commit + Δ_2a + Δ_2b`" (Phase 3 start) | "from `T_commit + Δ_2b`" |
| `T_commit + Δ_2a − ε_proc` (verdict broadcast time) | `T_commit − ε_proc` |
| `T_commit + Δ_2a/2` (mid-Phase-2a) | `T_commit − Δ_2a/2` |

**Choice for Phase 2a range:** I recommend `[T_p1_end, T_commit]` (uses named cutoffs at both ends). The alternative `[T_commit − Δ_2a, T_commit]` is also valid and arithmetic-explicit; pick one style and stick with it. Going with the named-cutoff version means readers don't need to compute `T_commit − Δ_2a` mentally each time. (Open for user override.)

## Edit-pass walkthrough — 2abOBFT.md

Targeted edits in document order. Line numbers are post-current-state (after the review-fix commit).

| # | Line(s) | Current text contains | New text |
|---|---|---|---|
| T1 | 47 | `T_accept_max = T_commit + Δ_2a − 1 BTT` | `T_accept_max = T_commit − 1 BTT` |
| T2 | 48 | `T_verdict_max = T_commit + Δ_2a − 1 BTT` ... `before Phase-2a end (T_commit + Δ_2a)` | `T_verdict_max = T_commit − 1 BTT` ... `before Phase-2a end (T_commit)` |
| T3 | 46 | `T_broadcast_max = T_commit − 2 BTT` ... `all honest first-observe by T_commit − 1 BTT` | `T_broadcast_max = T_p1_end − 2 BTT` ... `all honest first-observe by T_p1_end − 1 BTT` |
| T4 | 141 | `### Phase 2a — Bundle re-flood + verdict broadcast [T_commit, T_commit + Δ_2a]` | `### Phase 2a — Bundle re-flood + verdict broadcast [T_p1_end, T_commit]`; also keep the `<a id="phase-2a">` anchor |
| T5 | 149 | `T_verdict_max = T_commit + Δ_2a − 1 BTT` | `T_verdict_max = T_commit − 1 BTT` |
| T6 | 179 | "Verdict propagation budget" paragraph — formulas use `T_commit + Δ_2a` and `T_verdict_max + 1 BTT − ε_proc = T_commit + Δ_2a − ε_proc` | Replace with `T_commit` and `T_verdict_max + 1 BTT − ε_proc = T_commit − ε_proc` |
| T7 | 181 | `Arrival-to-Phase-2a-end slack = (T_commit + Δ_2a) − (T_commit + Δ_2a − ε_proc) = ε_proc` | `= T_commit − (T_commit − ε_proc) = ε_proc` |
| T8 | 191 | `### Phase 2b — σ-or-NR commit [T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]` | `### Phase 2b — σ-or-NR commit [T_commit, T_commit + Δ_2b]`; keep `<a id="phase-2b">` anchor |
| T9 | 193 | `At the start of Phase 2b (T_commit + Δ_2a)` | `At the start of Phase 2b (T_commit)` |
| T10 | 250 | `### Phase 3 — Local decryption and reconstruction (from T_commit + Δ_2a + Δ_2b)` | `### Phase 3 — Local decryption and reconstruction (from T_commit + Δ_2b)` |
| T11 | 291 | `Reconstruction completion target is T_commit + Δ_2a + Δ_2b + Δ_3` | `T_commit + Δ_2b + Δ_3` |
| T12 | 327 | Slot-structure bullet: `Phase 1 [slot_start, T_commit]: ... T_broadcast_max = T_commit − 2 BTT ... T_accept_max = T_commit + Δ_2a − 1 BTT` | `[slot_start, T_p1_end]: ... T_broadcast_max = T_p1_end − 2 BTT ... T_accept_max = T_commit − 1 BTT` |
| T13 | 328 | Phase 2a bullet: `[T_commit, T_commit + Δ_2a]` ... `around T_commit + Δ_2a − 1 BTT` | `[T_p1_end, T_commit]` ... `around T_commit − 1 BTT` |
| T14 | 329 | Phase 2b bullet: `[T_commit + Δ_2a, T_commit + Δ_2a + Δ_2b]` | `[T_commit, T_commit + Δ_2b]` |
| T15 | 330 | Phase 3 bullet: `(from T_commit + Δ_2a + Δ_2b)` | `(from T_commit + Δ_2b)` |
| T16 | 332 | "Slot timing" paragraph: `Phase 1 fetch occupies [slot_start, T_commit]` ... `consensus is expected to complete at T_commit + Δ_2a + Δ_2b + Δ_3` | `[slot_start, T_p1_end]` ... `T_commit + Δ_2b + Δ_3` |
| T17 | 394 | Partial synchrony paragraph: triple-formula list `T_broadcast_max = T_commit − 2 BTT`, `T_accept_max = T_commit + Δ_2a − 1 BTT`, `T_verdict_max = T_commit + Δ_2a − 1 BTT`; "Reconstruction is expected to complete at T_commit + Δ_2a + Δ_2b + Δ_3" | Update to new forms per the transformation table |
| T18 | 684 | Symbol glossary row for `T_accept_max`: `T_commit + Δ_2a − 1 BTT` | `T_commit − 1 BTT` |
| T19 | 700 | Phase 2a timing-row: `Δ_2a = 2 BTT; verdict broadcast horizon = T_commit + Δ_2a − 1 BTT = 1.80s` | Update to renamed semantics. Note: the wall-clock value 1.80s changes meaning — currently it's "1.80s past slot_start"; need to recompute given T_commit shifts |
| T20 | 741 | Glossary entry: `T_accept_max = T_commit + Δ_2a − 1 BTT` | `T_commit − 1 BTT` |
| T21 | 742 | Glossary entry: `T_verdict_max = T_commit + Δ_2a − 1 BTT` | `T_commit − 1 BTT` |
| T22 | new | Add a "T_p1_end" entry to the symbol glossary section | "T_p1_end | Phase-1 broadcast cutoff (= T_commit − Δ_2a). Phase 2a begins here." |

**Sanity-check after edits:** every formula on the right side of the transformation table should resolve to the same wall-clock time it did before (the rename is a renaming of T_commit, not a re-timing of the protocol).

## Edit-pass walkthrough — 2abOBFT-design-notes.md

| # | Line(s) | Edit |
|---|---|---|
| ND1 | 24 | "Slot timing" comparison row: `T_commit + Δ_2 + Δ_3` (OBFT) is fine (OBFT semantics); `T_commit + Δ_2a + Δ_2b + Δ_3 ≈ 1050ms post-T_commit` (2abOBFT) → `T_commit + Δ_2b + Δ_3 ≈ 650ms post-T_commit`. Update the parenthetical "(+600ms for Phase-2a window + Δ_3 difference)" accordingly. |
| ND2 | 185 | `T_commit + Δ_2a − 1 BTT` → `T_commit − 1 BTT` |
| ND3 | 188 | `T_commit + Δ_2a − 1 BTT` → `T_commit − 1 BTT`; "200ms past T_commit" needs recomputation/rewording since T_commit shifts |
| ND4 | 215 | `T_commit + Δ_2a − 1 BTT` → `T_commit − 1 BTT` |
| ND5 | 217 | same as ND4 |
| ND6 | 362 | E17 scenario: `T_commit + Δ_2a − ε` → `T_commit − ε`. Re-check the scenario math under renaming. |
| ND7 | 364 | `T_commit + Δ_2a − ε` → `T_commit − ε`; `T_commit + Δ_2a − ε + 1 BTT` → `T_commit − ε + 1 BTT` |
| ND8 | 365 | `T_commit + Δ_2a` → `T_commit`; `T_commit + Δ_2a + 1 BTT − ε` → `T_commit + 1 BTT − ε` |
| ND9 | 368 | `T_commit + Δ_2a − 1 BTT` → `T_commit − 1 BTT` |
| ND10 | 393 | open question #6: `T_commit + Δ_2a/2` → `T_commit − Δ_2a/2`; `T_commit + Δ_2a − 1 BTT` → `T_commit − 1 BTT` |
| ND11 | 401 | open question #10: `(T_commit + Δ_2a)` → `(T_commit)` |
| ND12 | 417 | open question #17: `T_commit + Δ_2a/2` → `T_commit − Δ_2a/2` |
| ND13 | 448 | implementation-plan bullet: `T_commit + Δ_2a − 1 BTT` → `T_commit − 1 BTT` |
| ND14 | 449 | implementation-plan bullet: `T_commit + Δ_2a` → `T_commit` |

## Edit-pass walkthrough — BFT-comparison.md

| # | Line(s) | Edit |
|---|---|---|
| BC1 | 19 | "Per-protocol T_commit anchors differ" bullet: `2abOBFT ≈ 1600ms (Phase 2a + 2b + Phase 3 ≈ 1050ms)` → `2abOBFT ≈ 2000ms (Phase 2b + Phase 3 ≈ 650ms)`. Reframe the disclaimer: the anchors are now more comparable (all three are "BLS-commit-to-submission" anchors), but per-protocol post-T_commit budgets still differ (OBFT ≈ 450ms vs 2abOBFT ≈ 650ms vs OBFTR_R1 ≈ 1600ms). Keep the comparison-to-T_relay_cutoff framing. |
| BC2 | (scan for) | Audit for any other place that quotes 2abOBFT's T_commit ≈ 1600ms or similar; recompute. |

## Implementation impact

**None.** No 2abOBFT implementation exists yet. The bare-OBFT impl uses OBFT semantics already and is consistent.

## Test impact

**None.** consensustest references to T_commit are OBFT-context (no 2abOBFT scenarios yet).

## Validation steps

After all edits:

1. **Residual scan**: `grep -n "T_commit + Δ_2a" docs/2abOBFT.md docs/2abOBFT-design-notes.md docs/BFT-comparison.md` should return **zero hits** (all such expressions become just `T_commit` or `T_commit + ...` without the `+ Δ_2a`).
2. **`T_p1_end` introduction**: `grep -n "T_p1_end" docs/2abOBFT.md` should return at least one definition site (in §Setting) plus references at phase boundaries.
3. **Semantic preservation**: pick three derived deadlines (T_accept_max, T_verdict_max, T_broadcast_max) and verify the wall-clock time they resolve to under the new naming equals the value they resolved to under the old naming. Sanity table:

   | Quantity | Wall-clock at Config A | Old formula | New formula |
   |---|---|---|---|
   | T_broadcast_max | T_p1_end − 400ms | T_commit − 400ms | T_p1_end − 400ms |
   | T_accept_max | T_p1_end + Δ_2a − 200ms = T_commit − 200ms | T_commit + Δ_2a − 200ms | T_commit − 200ms |
   | Phase-2b end | T_p1_end + Δ_2a + Δ_2b = T_commit + Δ_2b | T_commit + Δ_2a + Δ_2b | T_commit + Δ_2b |

4. **Cross-doc consistency**: grep for any document that references 2abOBFT's specific T_commit value (e.g., "1600ms"). Verify each is recomputed where appropriate.
5. **Anchor/link audit**: the rename doesn't affect anchors (Phase 2a/2b heading anchors stay `phase-2a` / `phase-2b`).
6. **Tests pass**: `go test ./protocol/v2/...` — expected pass with no test changes (impl unaffected).
7. **Lint clean**: `gofmt -l protocol/v2/` and `go vet ./protocol/v2/...`.

## Execution order

1. **2abOBFT.md** (T1-T22, in document order top-to-bottom). Add the T_p1_end glossary entry first (T22), then run the formula updates in document order.
2. **2abOBFT-design-notes.md** (ND1-ND14, document order).
3. **BFT-comparison.md** (BC1-BC2).
4. **Validation** (7 steps in §Validation).
5. **Cleanup tracking review**: present any cleanup notes from the pass.
6. **Commit** (only if user explicitly asks).

## Out-of-scope / explicit non-goals

- **No content changes** — strictly a renaming pass. No protocol behavior changes, no formula derivations re-done from first principles (only mechanical translation), no edits to assumption framing or recovery scope discussions.
- **No implementation changes** (none required).
- **No test changes**.
- **No commit** without explicit user ask.
- **Does not address other terminology bugs**: this pass touches T_commit only. If there are other cross-protocol terminology inconsistencies (`Δ_2 / Δ_2a / Δ_2b`, `B_k` per-layer broadcast budget, etc.), they're separate efforts.

## Open issues for user to confirm before execution

1. **Naming**: confirm `T_p1_end` (or pick alternative). Default proceeding with `T_p1_end`.
2. **Phase 2a range styling**: confirm `[T_p1_end, T_commit]` over `[T_commit − Δ_2a, T_commit]`. Default proceeding with the named-cutoff form.
3. **Symbol glossary**: confirm adding a `T_p1_end` row alongside existing T_* entries in 2abOBFT.md.

Awaiting "proceed" / "execute" before touching files. Other adjustments to plan welcome.

# OBFT K=2 default + (b)-deadlock note + Δ_2a tightening + impl sweep — plan

## Scope

Three doc changes to the OBFT-family spec set, two cleanup-note clarifications, and one impl sweep covering the production runner-adapter and the bare-OBFT / 2abOBFT impl packages.

- **Change A (small, narrow, doc-only):** Add a "Practical mitigations" subsection to the OBFT.md fat warning at [OBFT.md:494–509](OBFT.md#liveness-synchrony-conditional), explaining that the (b) partial-propagation deadlock is structurally narrowed in practice by (i) reflood-aware per-layer budgets and (ii) the peer-reflood-V early-commit mechanism.
- **Change B (large, repository-wide, doc-only initially; impl sweep follows):** Make **`K = f+1` (BFT-minimum)** the default operating point across the OBFT-family spec documents. At SSV's most common cluster size (n=4, f=1) this resolves to K=2; generalizes meaningfully to larger n (n=7 f=2 → K=3; n=13 f=4 → K=5). Motivated by production testing showing K > f+1 doesn't materially improve outcomes once peer-reflood-V closes the `h_V=1` case at L_0.
- **Change C (medium, doc-only initially; impl sweep follows):** Tighten 2abOBFT's `Δ_2a` from the current `2·BTT` recommendation to `1·BTT + ε_proc` under reflood-aware scheduling. Same logic as OBFT's Δ_2 = 1·BTT tightening, applied to verdict propagation instead of KindCommit propagation. The late-bundle absorption portion of the current 1·BTT cushion is redundant with `B_0 = 2·BTT + RefloodDelay`; the verdict-propagation cycle is what remains load-bearing.
- **Cleanup tasks (small, opportunistic):** Two wording clarifications surfaced while drafting the above — see [§Cleanup tasks](#cleanup-tasks).
- **Impl sweep (medium, code):** Sweep over `obft/` and `twoab/` impl packages plus the OBFT runner-adapter to make K=2 a first-class default and carry Change C's tightening into the impl. See [§Impl sweep](#impl-sweep).

Changes A, B, C are logically independent and can ship separately. The impl sweep is downstream of B and C.

## Cleanup tasks

Two clarifications surfaced while drafting the plan. Both are small, both worth doing alongside the change they're closest to.

### Cleanup-1: "receivers" wording at [OBFT.md:498](OBFT.md#L498)

A prior draft of this conversation flagged the `n − qEnc < receivers < qV` formula as off-by-one (claimed it excluded the `h_V=1` case). **Retracted** — the formula is correct under the reading that "receivers" counts the byzantine leader as a receiver of its own V. At f=1, n=4, `h_V=1` ↔ `r=2` (byz leader + 1 honest receiver), which is exactly the formula's deadlock range `{2}`. The narrative at [OBFT.md:667](OBFT.md#L667) ("σ-pool = 2 (recipient + byz σ_L^V), NR-pool = 2") confirms this reading.

**Edit:** clarify the wording inline so the next reader doesn't misread — e.g., `receivers (operators with V; the leader counts trivially as a receiver of its own bundle)`.

**Pair with:** Change A (both edit the same fat-warning block).

### Cleanup-2: Δ_2a "minimum coherent sizing" wording at [2abOBFT.md:199](docs/2abOBFT.md#L199)

The spec currently says:

> The recommended `Δ_2a = 2 BTT` is therefore not a defensive choice but the **minimum coherent sizing** for the protocol's verdict-broadcast schedule. Production deployments MUST size `Δ_2a ≥ 2 BTT`.

The strict structural minimum is `Δ_2a = 1·BTT + ε_proc` (verdict broadcast at exactly `T_verdict_start`). 2·BTT has `1·BTT − ε_proc` of within-Δ_2a slack on top of that floor — slack, not strict structural necessity. The wording conflates the two.

**Edit:** rephrase to distinguish the strict structural minimum (`1·BTT + ε_proc`) from the recommended sizing (whatever Change C lands on). If Change C tightens to `1·BTT + ε_proc`, this clarification falls out naturally as part of that rewrite.

**Pair with:** Change C (the rewording is subsumed if Change C goes through; otherwise standalone).

---

## Change A — Practical-mitigations note in the (b) fat warning

### Target

[OBFT.md:494–509](OBFT.md#L494) — the `> ### ⚠️ In its current form, OBFT's liveness is conditional ...` block.

### Insertion point

Between the existing "Practical causes of (b) failing:" list (ends at [OBFT.md:507](OBFT.md#L507)) and the "Comparison to QBFT" closing paragraph at [OBFT.md:509](OBFT.md#L509).

### Drafted text (subject to review)

```markdown
> **Practical mitigations narrow (b) substantially:**
> - **Reflood-aware per-layer budgets** (`B_0 = 2·BTT + RefloodDelay`; backups at `B_k = T_commit` — see [§Setting](#setting)) absorb a full gossipsub IHAVE/IWANT cycle inside the per-layer budget, making partial propagation past `B_k` rare at L_0 and exceedingly rare at backups.
> - **Peer-reflood V via early Commit** (see §Phase 2 / Peer-reflood V via early commit) largely addresses the worst (b) sub-case — `h_V=1` selective Phase-1 delivery — at L_0: the in-time recipient's `KindCommit` carries V plaintext + σ_L^V witness, V-drop receivers σ on peer-V, σ-pool reaches qV.
```

### Notes on wording choices

- "narrow substantially" rather than "almost fully covered" — accurate about what each mechanism does (reflood-aware reduces *probability*; peer-reflood-V *closes* a specific sub-case).
- Per the user's clarification, intentional byzantine grief residuals are not called out in this block — they're already deterred across slots by Assumption 4 and don't need restating here.
- Cross-link target `#peer-reflood-v-via-early-commit` matches the existing in-doc heading anchor at [OBFT.md:239](OBFT.md#L239); verify the anchor slug at edit time.

### Decision

**Keep** the existing "Practical causes of (b) failing" list ([OBFT.md:502–506](OBFT.md#L502)). The mitigations narrow the probability but don't eliminate the failure modes; operators still benefit from knowing what causes (b) to fail.

---

## Change B — K=2 as the default operating point

### Rationale (per user, to record in commit/PR)

Production testing at SSV's most common cluster size (n=4) indicates K > 2 does not materially improve liveness outcomes once peer-reflood-V closes the `h_V=1` case at L_0. The fall-through value of K=3, K=4 was tied to recovering deeper failure modes that either (i) are already closed at L_0 by peer-reflood-V, or (ii) are byzantine-grief patterns deterred by Assumption 4. The cost of K > 2 (chained-IBE depth, onion size, ε_3 × K Phase-3 walk) is real; the marginal benefit at n=4 is not.

### Doc files in scope

| Doc | K=4/K=2 mention count | Role | Change weight |
|---|---|---|---|
| [docs/OBFT.md](docs/OBFT.md) | 94 | Primary OBFT spec | Heavy |
| [docs/2abOBFT.md](docs/2abOBFT.md) | 36 | 2abOBFT spec | Heavy |
| [docs/OBFTR.md](docs/OBFTR.md) | 53 | OBFTR spec (currently defaults K=3 per [BFT-comparison.md:15](docs/BFT-comparison.md#L15)) | Medium — see Q-B3 |
| [docs/BFT-comparison.md](docs/BFT-comparison.md) | 10 | Side-by-side comparison | Medium |
| [docs/OBFT-formal-verif.md](docs/OBFT-formal-verif.md) | 39 | TLA+ verification scope/results | Light (mostly factual — TLC runs were already at K=2 for SAFETY, K=4 for LIVENESS — see Q-B4) |
| [docs/2abOBFT-design-notes.md](docs/2abOBFT-design-notes.md) | 30 | Internal design notes | Light — see Q-B5 |

### Categorization of K=4 references (apply per-doc)

I expect each K=4 reference falls into one of these buckets. The required action differs.

| Category | Example | Action under K=2 default |
|---|---|---|
| **(i) "Running example" / "default" / "SSV operating point"** | "The protocol description below targets `n = 4` (`f = 1`) as the running example, with `K = 4` (i.e., K = n) ..." [OBFT.md:11](docs/OBFT.md#L11) | Flip to K=2 |
| **(ii) Concrete sizing tables / cost numbers** | "Concrete K=4 sizing for SSV proposer duty ...: `B_0 = 1100ms`, `B_1 = B_2 = B_3 = T_commit = 3600ms`" [OBFT.md:63](docs/OBFT.md#L63); "~3 KB at K=4, ~1 KB at K=2" [OBFT.md:682](docs/OBFT.md#L682) | Replace with K=2 numbers (`B_0`, `B_1 = T_commit`); ~1 KB onion |
| **(iii) Recommendation / opinion statements** | "the recommended OBFT configuration is **`K = 4 = n`**" [OBFT.md:703](docs/OBFT.md#L703); "`K = 2`: BFT-min at f=1; **not recommended for OBFT**" [OBFT.md:835](docs/OBFT.md#L835) | **Flip**: K=2 is now recommended at n=4; K > 2 becomes an option for larger n / higher-tail deployments |
| **(iv) Multi-failure / K ≥ 3 narrative** | "Multi-failure fall-through (K ≥ 3): with `K > 2` layers, the cluster can fall through past multiple silent ..." [OBFT.md:539](docs/OBFT.md#L539); same at [OBFTR.md:512](docs/OBFTR.md#L512) | Keep as a tunable's-eye-view paragraph, but reframe: at the K=2 default this is N/A; K ≥ 3 is the up-tier for deployments that want deeper fall-through |
| **(v) Pigeonhole / structural claims** | "At K = n = 4, every cluster member is a leader exactly once; pigeonhole guarantees ≥3 honest leaders at f=1, providing maximum K-fall-through depth" [OBFT.md:539](docs/OBFT.md#L539) | Reframe for K=2: 2 leaders out of 4 ops, pigeonhole guarantees ≥1 honest leader at f=1 (= 1 fall-through layer covers the silent-leader case) |
| **(vi) Worked failure-mode examples** | "at K=4 with L_0, L_1, L_2 all silent: ..." [OBFT.md:539](docs/OBFT.md#L539); liveness-comparison rows at [OBFT.md:562](docs/OBFT.md#L562) | Replace with K=2 worked example (L_0 silent → L_1) for the default narrative; optionally retain a K=4 example as the "deeper fall-through" up-tier |
| **(vii) General constraints** | "K is tunable per duty within `f+1 ≤ K ≤ n`" [OBFT.md:11](docs/OBFT.md#L11) | Keep unchanged |

### Per-doc impact sketch

#### docs/OBFT.md (heavy)

Primary changes:
1. Introduction / running example ([§Setting at OBFT.md:11](docs/OBFT.md#L11)) — flip running example to K=2.
2. Sizing intuition at [OBFT.md:63](docs/OBFT.md#L63) — K=2 concrete sizing: `B_0 = 2·BTT + RefloodDelay = 1100ms`, `B_1 = T_commit = 3600ms`.
3. K-tuning at [OBFT.md:833–836](docs/OBFT.md#L833) — flip "K=2 not recommended" to recommended-at-n=4; reframe larger-K as up-tier.
4. SSV recommended config at [OBFT.md:703](docs/OBFT.md#L703) — flip "K = 4 = n" → "K = 2".
5. Termination one-liner at [OBFT.md:690](docs/OBFT.md#L690) — update K, drop "K ≥ 3 (late-leader resilience)" condition (vacuous at K=2 default; mention as up-tier).
6. Multi-failure fall-through paragraph at [OBFT.md:539](docs/OBFT.md#L539) — reframe as "up-tier at K ≥ 3"; K=2 narrative becomes single-fall-through (L_0 → L_1).
7. Liveness-comparison table at [OBFT.md:551 onward](docs/OBFT.md#L551) — replace "K=4 SSV operating point" with K=2 in default columns; rerun ε_3 numbers (no `ε_3 × K` multi-walk at K=2; single-layer Phase-3 cost only).
8. All `K=4 Config A` parentheticals — replace with `K=2 Config A`.
9. Bandwidth tables [OBFT.md:885, 932](docs/OBFT.md#L885) — replace with K=2 numbers (smaller onion: ~1 KB/op vs ~7 KB/op at K=4 — onion is the dominant size in `KindCommit`, but σ_L^V witness section also shrinks: 2 witnesses/op × n = 2n witnesses cluster-wide vs 16 at K=4 n=4).
10. Failure modes catalog at [OBFT.md:645 onward](docs/OBFT.md#L645) — update Class-A scope statements that reference K=4 layer counts.

#### docs/2abOBFT.md (heavy)

Similar in shape to OBFT.md. 2abOBFT's recovery story leans more on the verdict-pool mechanism than K-layer fall-through, so the K=2 default *strengthens* the rationale for 2abOBFT-as-companion (Phase-2a closes more at L_1 than bare OBFT recovers at deep K layers).

#### docs/OBFTR.md (light)

OBFTR keeps K=3 as its preferred default (R-round retry substitutes for one layer of K-fall-through, per [BFT-comparison.md:15](docs/BFT-comparison.md#L15)). The OBFTR.md edits are limited to: (a) explicitly noting the divergence from bare OBFT's new K=2 default, with the design rationale; (b) audit any spots that currently default-cite K=4 (now K=3 in OBFTR).

#### docs/BFT-comparison.md (medium)

- [BFT-comparison.md:15](docs/BFT-comparison.md#L15) — the cluster-spec line "`K = 4` across protocols for like-for-like comparison" needs to flip. New framing: bare OBFT defaults K=2 (BFT-min at f=1), OBFTR defaults K=3 (its R-round-substitutes-for-one-layer rationale), 2abOBFT defaults K=2 (same BFT-min logic as bare OBFT).
- "OBFT (K=4)" column header at [BFT-comparison.md:229](docs/BFT-comparison.md#L229) and similar.
- The `K-1 = 3 silent in K=4` row at [BFT-comparison.md:210](docs/BFT-comparison.md#L210) — at K=2 default this becomes "K-1 = 1 silent in K=2"; either rewrite or reframe as "deeper-K up-tier".
- MEV-fetch budget comparisons at [BFT-comparison.md:293](docs/BFT-comparison.md#L293) — these are K-independent (B_0 = 2·BTT + RefloodDelay regardless of K); the framing stays but cross-references shift.

#### docs/OBFT-formal-verif.md (light)

The TLC runs are already split: SAFETY at K=2, LIVENESS at K=4 (see [OBFT-formal-verif.md:685–688](docs/OBFT-formal-verif.md#L685)). Edits are mostly factual/framing — "verified at K=4 (the default)" → "verified at K=4 (deeper-K up-tier)". No TLA+ reruns required for this change.

#### docs/2abOBFT-design-notes.md (skip)

Internal historical notes; not updated as part of this plan.

### Decisions

- **Default framing:** Use `K = f+1` BFT-minimum framing in the spec text. At SSV's most common n=4 cluster this collapses to K=2 as the concrete value. Larger n gets meaningful generalization (K=3 at n=7 f=2; K=5 at n=13 f=4). Encode in the K-tuning section ([OBFT.md:833](docs/OBFT.md#L833)).
- **Worked examples:** **Replace** K=4 worked examples with K=f+1 (= K=2 at n=4) ones. Single-fall-through narrative (L_0 → L_1) becomes the default illustration; K=4 remains available as an up-tier example in the K-tuning section.
- **OBFTR:** Keep OBFTR's K=3 default. R-round retry substitutes for one layer of K-fall-through, so the design rationale is independent of bare OBFT's BFT-min choice. Document the divergence explicitly in [OBFTR.md](docs/OBFTR.md) and [BFT-comparison.md](docs/BFT-comparison.md).
- **TLA+ LIVENESS rerun at K=2:** **Skip.** Existing K=4 LIVENESS verification + K=2 SAFETY verification provides reasonable coverage; rerunning LIVENESS at K=2 isn't a blocker for the spec default flip.
- **Internal notes** (`2abOBFT-design-notes.md`, `*-PHASE-*-PLAN.md`): **Skip by default**; update opportunistically if a passage gets in the way during the spec edit. These are historical reference material, not authoritative spec.

---

## Change C — Δ_2a tightening under reflood-aware scheduling

### Rationale

Same argument that tightened OBFT's Δ_2 from `2·BTT` to `1·BTT`, applied to 2abOBFT's verdict-propagation cycle. Under reflood-aware scheduling, `B_0 = 2·BTT + RefloodDelay` already absorbs a full gossipsub IHAVE/IWANT cycle for the Phase-1 leader bundle *before* Phase-2a begins. The 1·BTT "structural jitter cushion" portion of the current `Δ_2a = 2·BTT` recommendation has two purposes:

1. **Late-Phase-1-bundle absorption** — a receiver who first-observes a bundle in `(T_verdict_start, T_accept_max]` can still emit `σV`. Per-layer effective absorption = `B_k + (Δ_2a − 1·BTT)`. Under reflood-aware scheduling this is structurally redundant with `B_0`'s `RefloodDelay`-baked reflood cycle.
2. **Variance buffer for verdict propagation** — a non-redundant cushion analogous to within-Δ_2 slack in pre-tightening OBFT.

The OBFT tightening dropped (1) because it became redundant; (2) is the residual which Δ_2 = 1·BTT still covers (one propagation cycle for synchronous-fallback emit; early emit in the typical case gives much more cushion in practice). The same argument carries to Δ_2a: drop (1), keep (2), land at `Δ_2a = 1·BTT + ε_proc` — the strict structural minimum where verdict broadcast time `T_verdict_max − ε_proc` lands exactly at `T_verdict_start`.

At Config A this recovers `~1·BTT − ε_proc` ≈ 150ms of slot budget per slot — reallocatable to MEV-fetch headroom or backup absorption.

### Target

[docs/2abOBFT.md](docs/2abOBFT.md), primarily [§Setting at line 63](docs/2abOBFT.md#L63) and [§Phase 2a / Verdict propagation budget at lines 194–199](docs/2abOBFT.md#L194). Secondary touchups in §Assumptions and the timing tables where `Δ_2a` appears as a budget contributor.

### Drafted edits (subject to review)

- **§Setting `Δ_2a ≥ 2 BTT` floor** at [2abOBFT.md:63](docs/2abOBFT.md#L63) — change the recommendation from `Δ_2a ≥ 2 BTT` to `Δ_2a ≥ 1·BTT + ε_proc` (structural minimum). Update the rationale to mirror OBFT.md's reflood-aware-scheduling argument for Δ_2 = 1·BTT.
- **§Phase 2a / Verdict propagation budget** at [2abOBFT.md:194–199](docs/2abOBFT.md#L194) — rewrite to: (i) state the strict structural minimum `1·BTT + ε_proc`; (ii) note that the late-Phase-1-bundle absorption purpose of any cushion above the floor is redundant with `B_0`'s reflood-aware schedule; (iii) keep the synchronous-fallback / early-emit framing analogous to OBFT.md's Δ_2 sizing discussion. This edit also closes Cleanup-2.
- **Per-layer effective absorption formula** at [2abOBFT.md:59](docs/2abOBFT.md#L59) — formula stays the same (`B_k + Δ_2a − 1·BTT`), but the worked numbers at Config A change: L_0 effective = `B_0 + (Δ_2a − 1·BTT) = 1100 + ε_proc ≈ 1150ms` (down from `1300ms`).
- **`MUST size Δ_2a ≥ 2 BTT`** at [2abOBFT.md:199](docs/2abOBFT.md#L199) — flip to the tightened recommendation. This is the structural-minimum clarification (Cleanup-2) plus a new recommendation.
- **§Comparison / "OBFT saves ~600ms"** at [2abOBFT.md:17](docs/2abOBFT.md#L17) — the `Phase 2a window 400ms` term in the cost difference decreases; recompute the gap between OBFT and 2abOBFT slot budgets.
- **Timing tables** — any place that quotes `Δ_2a = 400ms` (2·BTT at Config A) updates to `Δ_2a = 250ms` (= 1·BTT + ε_proc = 200ms + 50ms at Config A). Verify the ε_proc value used in each table at edit time.

### Decisions

- **Sizing target:** Strict structural minimum `Δ_2a = 1·BTT + ε_proc`. Matches OBFT's Δ_2 = 1·BTT philosophy (no within-Δ_2 slack; early emit gives most cushion in practice). Reconsider only if production tail data shows verdict-propagation tails routinely exceeding 1·BTT.
- **OBFTR scope:** Verify during the Change-C sweep that OBFTR's per-round Δ_2 sizing is consistent with the tightened-recommendation philosophy ([BFT-comparison.md:15](docs/BFT-comparison.md#L15) currently lists OBFTR per-round Δ_2 = 1·BTT, suggesting already tightened). OBFTR doesn't have a 2abOBFT-style verdict/commit split, so no analog of Δ_2a tightening is needed — likely a no-op for OBFTR beyond confirmation.

---

## Impl sweep — K=2 first-class default + Δ_2a tightening + protocol-timing API restructure

### Three logical parts

This sweep bundles three related code changes. They share the same files and are easiest to review as a single PR, but split clearly along these axes:

1. **K=2 default flip** — flip `DefaultK = 4` → `DefaultK = 2` in the bare-OBFT runner-adapter; update worked-example comments across packages.
2. **Δ_2a tightening** — lower the `Validate` floor and flip the recommendation comment in `twoab/config.go` to match Change C.
3. **Protocol-timing API restructure** — operators supply BTT only; protocol-internal timings (Δ_2, Δ_2a, Δ_2b, ε_proc, ε_3, B_k) derive from BTT via fixed spec-recommended formulas, so all operators in a cluster compute identical timings deterministically. This is the structural realization of [§Change C](#change-c--δ_2a-tightening-under-reflood-aware-scheduling)'s "same manner for all operators" principle.

### Part 1 — K=2 default flip

| File | Current state | Change |
|---|---|---|
| [protocol/v2/ssv/runner/obft/config.go](protocol/v2/ssv/runner/obft/config.go) | `DefaultK = 4` ([line 56](protocol/v2/ssv/runner/obft/config.go#L56)); rationale comment explains K=4 as "the recommended layer count for SSV proposer duty" | Flip `DefaultK = 2`; rewrite rationale to reference Change B's BFT-min default (K = f+1; K=2 at SSV's n=4 cluster); update the worked-example schedule at [config.go:86–95](protocol/v2/ssv/runner/obft/config.go#L86) to K=2 |
| [protocol/v2/obft/base/types.go](protocol/v2/obft/base/types.go) | K=4 used in worked-example comments at [line 97](protocol/v2/obft/base/types.go#L97), [line 239](protocol/v2/obft/base/types.go#L239) | Reorder doc-comment series so K=2 leads; keep K=3, K=4 as up-tier examples |

### Part 2 — Δ_2a tightening (`twoab/config.go`)

| Location | Current state | Change |
|---|---|---|
| [twoab/config.go:422](protocol/v2/obft/twoab/config.go#L422) | `if c.Delta2a < 2*c.BTT` validate | Lower floor to `if c.Delta2a < c.BTT + c.EpsProc` (or equivalent; see Part 3 below — after restructure this validate may no longer be needed at all if Δ_2a isn't user input) |
| [twoab/config.go:127–130](protocol/v2/obft/twoab/config.go#L127), [twoab/config.go:417–423](protocol/v2/obft/twoab/config.go#L417) | "Recommended for production: Δ_2a = 2 BTT" / "Δ_2a ≥ 2 BTT is the minimum coherent sizing" | Rewrite to: "Δ_2a = 1·BTT + ε_proc (strict structural minimum, recommended per spec §Setting under reflood-aware scheduling — reflood absorption is structurally provided by `B_0 = 2·BTT + RefloodDelay`)" |
| Worked-example comments through `twoab/config.go` (e.g., [line 312](protocol/v2/obft/twoab/config.go#L312)) | K=4 leads | Reorder so K=2 leads |

### Part 3 — Protocol-timing API restructure

The principle (from the conversation that produced this plan): **operators supply BTT only; protocol timings derive from BTT via spec-recommended formulas. All operators in a cluster compute identical values.**

**What stays operator-facing:**
- `BTT` — the deployment's P99 + δ propagation+skew budget. Cluster-wide-agreed.
- Deployment-environment values that aren't "protocol timings" — `RelayCutoff` (slot deadline, chain-constant), `HeaderSubmitHeadroom` (SSV-relay integration headroom), `RefloodDelay` (gossipsub HeartbeatInterval mirror). These are *external* constraints, not protocol-internal sizing — keep operator-facing for now; revisit if any of them turn out to be derivable too.

**What becomes internal (derived from BTT, not exposed in Config):**
- bare OBFT: `Delta2` (= 1·BTT)
- 2abOBFT: `Delta2a` (= 1·BTT + ε_proc per Change C), `Delta2b` (= 1·BTT + ε_proc), `EpsProc` (constant ~50ms)
- Both: `Eps3` (constant ~50ms), per-layer `B_k` schedule (already derived from BTT + RefloodDelay; verify no operator-tuning slips in)

**Impl steps:**
1. Move the now-derived fields out of the public `Config` struct (or keep the field but ignore operator-supplied values, log a deprecation warning, then drop the field in a follow-up release). Decision-pending: hard-remove vs. soft-deprecate (see notes below).
2. Add a `Compute()` / `Derive()` / `WithDefaults()` step in the Config constructor that fills derived fields from `BTT`.
3. Remove `Validate`-side checks on derived fields (they're internal-correct by construction).
4. Update tests that explicitly set the now-derived fields to use the constructor instead.

**Touched files (initial guess; verify during impl):**
- [protocol/v2/ssv/runner/obft/config.go](protocol/v2/ssv/runner/obft/config.go) — `Delta2`, `Eps3` move from Config to derivation.
- [protocol/v2/obft/twoab/config.go](protocol/v2/obft/twoab/config.go) — `Delta2a`, `Delta2b`, `EpsProc`, `Eps3` move from Config to derivation.
- [protocol/v2/obft/base/types.go](protocol/v2/obft/base/types.go) — may need a derivation helper; consider co-locating spec-formula constants here.
- Test files: assertions on the moved fields need updating to use the constructor.

### Consensustest audit (out-of-scope-but-worth-noting)

Consensustest adapters at [protocol/v2/consensustest/obft/](protocol/v2/consensustest/obft/) and [protocol/v2/consensustest/twoab/](protocol/v2/consensustest/twoab/) take K per-scenario — no hardcoded default. After Part 3, consensustest scenarios should also call the same Config constructor (or BTT-only API), so they pick up the same derivations as production. Touchpoints surfaced by `grep`: [catalog_silent.go:53](protocol/v2/consensustest/catalog_silent.go#L53), [reporting/data.go:71–311](protocol/v2/consensustest/reporting/data.go#L71), [obft/events.go:700](protocol/v2/consensustest/obft/events.go#L700), [obft/sizes.go:37](protocol/v2/consensustest/obft/sizes.go#L37) — mostly K=4 commentary. Update opportunistically.

### Decisions

- **Migration story:** Not needed. OBFT-family is still experimental; no production operators to coordinate with. Straightforward default flip.
- **Operator-tunability of protocol timings:** **Removed.** Operators supply BTT only; protocol timings derive deterministically. See Part 3.
- **Test fixes:** Bundle with the impl sweep PR. A default flip + structural API change with red tests is a bad smell.

### Notes / decisions to make at impl time

- **Hard-remove vs. soft-deprecate the now-internal fields** (e.g., `Delta2a` in the public Config struct). Hard-remove is cleanest but breaks any external callers that set these explicitly. Soft-deprecate (ignore + log) is gentler but leaves stale API surface. Decide based on caller audit during the impl PR; given the "still experimental" framing, hard-remove is probably acceptable.
- **`EpsProc` and `Eps3` as constants vs. BTT-derived:** Spec quotes both as ~50ms (Config A). Could be derived as a function of BTT (e.g., `ε_proc = 0.25·BTT`) for cleaner cross-deployment scaling, or fixed constants (50ms regardless of BTT). Spec text leans toward "fixed constant per Config", which suggests they're independent of BTT. Default: fixed constants in the impl, derive only if a tighter Config is requested.

---

## Sequencing recommendation

All open questions are resolved. Each change is independent enough to ship on its own; the natural order minimizes rework:

1. **Change A** (low-risk, narrow, doc-only). Practical-mitigations note in the OBFT.md fat warning + Cleanup-1 wording fix.
2. **Change C** (medium, doc-only). Δ_2a tightening in [2abOBFT.md](docs/2abOBFT.md) + Cleanup-2 wording fix. Independent of Change B (K-default is orthogonal to Δ_2a sizing). Includes a quick verification pass on [OBFTR.md](docs/OBFTR.md) for any Δ_2-related consistency.
3. **Change B** (large, doc-only). K = f+1 BFT-min default. Run in two passes:
   - **Pass 1** — author-facing structural changes: K-tuning recommendation flip, running-example flip, default-config statements, sizing tables. Concentrated in §Setting / §Application / §Liveness sections of OBFT.md and 2abOBFT.md. Document the bare-OBFT-vs-OBFTR default divergence.
   - **Pass 2** — flush parentheticals: every "at K=4 Config A" → "at K=2 Config A" (or "at K = f+1 Config A" where the more general framing reads better). Cross-doc consistency pass.
4. **Impl sweep** — after the relevant spec changes land. Three logical parts (per [§Impl sweep](#impl-sweep--k2-first-class-default--δ_2a-tightening--protocol-timing-api-restructure)) bundled as a single PR:
   - **Part 1** — K=2 default flip across runner-adapter + impl packages.
   - **Part 2** — Δ_2a tightening in `twoab/config.go`.
   - **Part 3** — protocol-timing API restructure (operators supply BTT only).
   - All three are best reviewed together since they touch overlapping files and Part 3 subsumes Part 2's `Validate`-side changes.

## What this plan deliberately does not include

- **Migration / rollout strategy.** Default-value changes only — operators with explicit configs are unaffected. No operator-facing config-migration step.
- **Cross-reference audit beyond the surveyed K=4 mentions.** A `grep -nrE 'K\s*=\s*4|K=4'` should catch the long tail, but every doc has its own framing edges (e.g., bandwidth tables, MEV-fetch comparisons) worth eyeballing during the edit pass.
- **2abOBFT production-duty wiring.** No `runner/twoab/` adapter exists today (confirmed via grep); 2abOBFT is impl-only. Authoring the production runner-adapter is a separate effort, downstream of this plan.
- **Behavioral changes beyond what's documented above.** This plan flips defaults and updates recommendations; it does not introduce new protocol mechanisms.

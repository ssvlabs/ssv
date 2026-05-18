# OBFT-family BFT_start sweep — implementation plan

## Goal

Replace the post-hoc UI filter for the OBFT-family `slot_start` toggle with a real per-`BFT_start` simulation. PSigs and QBFT continue to use the existing UI pipeline-shift semantic — they don't need extra simulation runs. As part of the same change, rename `SimConfig.SlotStart` → `SimConfig.BFTStart` across the consensustest package (sim code only — spec docs distinguish `slot_start` from `BFT_start` meaningfully and stay as-is).

Driver question this addresses: today the UI's `slot_start` picker for OBFT-family cells just filters out samples whose `decidingBroadcastTime < slot_start`, which models "a late-joining operator missing the live broadcast." That's not what most viewers think they're seeing — they read it as "the whole cluster's BFT starts later in the slot." A real per-`BFT_start` sim answers the actual question.

## Design decisions (locked)

| Q | Decision | Rationale |
|---|---|---|
| **Q1: Does T_commit shift with BFT_start?** | **No.** T_commit stays absolute (anchored at `RelayCutoff − headroom − ε_3 − Δ_2`). `BFT_start` only floors per-layer `T_broadcast_max_k = max(BFT_start, T_commit − B_k)`. | Matches production spec formula at [obft/base/types.go:234](../protocol/v2/obft/base/types.go:234). `RelayCutoff` is a wall-clock deadline; cannot shift. `BFT_start > T_commit − B_0` is genuine degraded mode and `BFT_start > T_commit` is a guaranteed miss — both are correct behaviors to surface. |
| **Q2: How many BFT_start values?** | **3 new simulation points: `{2000, 2400, 2800} ms`.** For UI picker values in `[0, 1600]`, the BFT_start=0 cell is reused directly (no filter, no shift). | At BTT=100ms, `T_commit − B_0 ≈ 2700-2800ms`. Below that boundary, BFT_start has no effect on L_0's broadcast time — only L_1's pre-propagation shifts, which only matters for the rare L_1 fall-through case (~0.85% of trials in the observed data). Accepting that small approximation in `[0, 1600]` saves ~7× the sim cost. Above 1600ms, simulate exactly. |
| **Q3: Post-hoc UI filter as fallback?** | **No fallback for OBFT-family.** UI logic is: pick the pre-computed cell, or fall back to BFT_start=0 cell for picker values ≤ 1600ms. | Per Q2: no need. Above 1600ms, only `{2000, 2400, 2800}` picker values are meaningful for OBFT-family; render others (1700, 1800, 1900...) as N/A or snap to nearest covered value. |
| **Q4: Late-joining-operator semantic?** | **Dropped.** Rename UI control "slot_start" → "BFT_start" to match the new semantic. | The existing filter logic wasn't actually modeling late-operator behavior correctly (it just dropped samples by deciding broadcast time). If a real late-operator analysis is wanted later, do it as a proper scenario class (one operator's `evtStartInstance` delayed), not a post-hoc filter. |
| **Q5: Rename `SlotStart` → `BFTStart`?** | **Yes in sim code; NO in spec docs.** Sim's SlotStart was always used as BFT_start (no pre-fetch modeling); the spec distinguishes the two meaningfully. | Sim rename removes the misleading name. Spec leaves `slot_start` as the wall-clock anchor and `BFT_start` as the consensus-start moment (pre-fetch sits between). See [docs/BFT-comparison.md:21](BFT-comparison.md:21), [docs/OBFTR.md:38](OBFTR.md:38). |

### Note on PSigs naming

Renaming PSigs's `SlotStart` field → `BFTStart` is a slight semantic stretch (PSigs isn't a BFT protocol per [docs/BFT-comparison.md:46](BFT-comparison.md:46) — "this is what BFT consensus protocols solve"). For PSigs, the field means "when each op signs". Accepting the stretch because: the underlying concept (when does the protocol's primary activity begin) is the same across protocols, and a single name avoids two-fields confusion.

## Implementation

### 1. Sim adapter changes

#### 1.1 OBFT — [protocol/v2/consensustest/obft/adapter.go](../protocol/v2/consensustest/obft/adapter.go)

At line 141-148, replace the `< 0` clamp with the BFT_start floor:
```go
bftStart := cfg.BFTStart
fetchAt := make([]time.Duration, cfg.K)
for k := 0; k < cfg.K; k++ {
    fa := tCommit - broadcastBudget[k] - fetchBuffer
    if fa < bftStart {
        fa = bftStart
    }
    fetchAt[k] = fa
}
```

At line 184-186, apply the same floor to `DecidingBroadcastTime`:
```go
if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(broadcastBudget) {
    bt := tCommit - broadcastBudget[out.DecidedRound]
    if bt < bftStart {
        bt = bftStart
    }
    out.DecidingBroadcastTime = bt
}
```

Forward `bftStart` into `desConfig` for use by the DES (see 1.4).

#### 1.2 2abOBFT — [protocol/v2/consensustest/twoab/adapter.go](../protocol/v2/consensustest/twoab/adapter.go)

Mirror the OBFT pattern. At line 174-180:
```go
bftStart := cfg.BFTStart
fetchAt := make([]time.Duration, cfg.K)
for k := 0; k < cfg.K; k++ {
    fa := tVerdictStart - broadcastBudget[k]
    if fa < bftStart {
        fa = bftStart
    }
    fetchAt[k] = fa
}
```

At line 217-223:
```go
if out.Decided && out.DecidedRound >= 0 && out.DecidedRound < len(broadcastBudget) {
    bt := tVerdictStart - broadcastBudget[out.DecidedRound]
    if bt < bftStart {
        bt = bftStart
    }
    out.DecidingBroadcastTime = bt
}
```

Forward to `desConfig`.

#### 1.3 QBFT — [protocol/v2/consensustest/qbft/adapter.go:138](../protocol/v2/consensustest/qbft/adapter.go:138)

Replace the dead-code zero with the config read:
```go
bftStart := cfg.BFTStart
```

The QBFT DES at [qbft/des.go:142](../protocol/v2/consensustest/qbft/des.go:142) already honors `BFTStart` correctly; this just unblocks it. (No sweep-matrix expansion for QBFT — UI continues to pipeline-shift QBFT cells from the BFTStart=0 sim.)

#### 1.4 OBFT/2abOBFT DES heartbeat audit — [obft/des.go](../protocol/v2/consensustest/obft/des.go), [twoab/des.go](../protocol/v2/consensustest/twoab/des.go)

Add `BFTStart` to `desConfig`. The DES tick-anchored events that need attention:
- `scheduleInitialHeartbeats` — should fire from `t=0` regardless of BFT_start, since the libp2p mesh is up before BFT (per spec). No code change needed; defensive audit only.
- `evtPhaseTwoStart` and `RoundEndOffset` — anchored on `T_commit` (absolute, per Q1). Unaffected by BFT_start. Verify by inspection.

If anything keys on relative-to-BFT_start time, ensure it's shifted appropriately.

#### 1.5 PSigs — [protocol/v2/consensustest/psigs/adapter.go](../protocol/v2/consensustest/psigs/adapter.go)

Pure rename: `SlotStart` field on internal `desConfig` (line 122) and the forwarding at line 76 become `BFTStart`. Behavior unchanged.

### 2. SimConfig rename — [protocol/v2/consensustest/protocol.go](../protocol/v2/consensustest/protocol.go)

Rename the `SlotStart time.Duration` field at line 61 → `BFTStart`. Update the comment to describe its actual role as the BFT-activity-start anchor (not "slot start"):

```go
// BFTStart is the virtual-time offset at which the protocol's primary
// broadcast pipeline begins (= BFT_start in the spec). Pre-fetch /
// pre-consensus modeling is not in scope for the sim; this field
// captures BFT activity start directly. Defaults to 0 (BFT starts at
// slot start). The OBFT-family schedules apply the spec's runtime
// clamp `T_broadcast_max_k = max(BFTStart, T_commit − B_k)`.
BFTStart time.Duration
```

Update [protocol.go:474](../protocol/v2/consensustest/protocol.go:474) field name in `DefaultProposerDutyConfig`.

Test file [psigs/adapter_test.go:112](../protocol/v2/consensustest/psigs/adapter_test.go:112) — rename usage.

### 3. Sweep matrix — [protocol/v2/consensustest/sweep.go](../protocol/v2/consensustest/sweep.go)

Add a `BFTStarts []time.Duration` axis to `p2pBaselineSweep` (and `DefaultSweeps`). Driver entrypoint reads `BFT_STARTS` env var (mirror `P2P_PROFILES` pattern from line 153). Default: `[0, 2000, 2400, 2800]` (1 + 3 = 4 values).

OBFT-family protocols (OBFT, OBFTx2, OBFTx3, 2abOBFT — anything not in `isPipelineShift`) get the full 4-value sweep. PSigs / QBFT only run at `BFTStart=0` (a wrapper conditional in the loop).

Implementation outline inside `p2pBaselineSweep`:
```go
for _, bftStart := range bftStarts {
    for _, profile := range profiles {
        for _, btt := range btts {
            for _, lvl := range instabilityLevels {
                base := withClusterSize(DefaultProposerDutyConfig(btt), n, k)
                base.BFTStart = bftStart
                ...
                pt := SweepPoint{
                    Label: fmt.Sprintf("BTT=%s profile=%s lvl=%s BFT_start=%dms", ..., bftStart.Milliseconds()),
                    Fields: map[string]float64{
                        ...,
                        "BFT_start": float64(bftStart.Milliseconds()),
                    },
                }
                // Skip non-zero BFTStart for pipeline-shift protocols
                cells := runCells(scenarios, filteredProtocols(protocols, bftStart != 0), ...)
                ...
            }
        }
    }
}
```

Other sweeps (p2pIncreasingBTT, packet_loss, …) do **not** get the new axis. They're already cost-bounded and orthogonal.

### 4. Reporting — [protocol/v2/consensustest/reporting/data.go](../protocol/v2/consensustest/reporting/data.go)

No schema changes. `pointPayload.Fields` already serializes axis values as a generic map (line 425). The merge logic (`fieldsKey`) already keys on the full Fields tuple, so points at distinct `BFT_start` values auto-deduplicate.

### 5. UI — [stresstest-report/app.js](../stresstest-report/app.js)

#### 5.1 Lookup rewrite for OBFT-family

`findBaselinePointAtInstability` at line 400 — extend signature with `bftStart` parameter. Match on `(f.BFT_start ?? 0) === bftStart`.

`shiftedCell` at line 339 — branch on protocol family:
- **OBFT-family**: pick the sibling cell from a point at the requested `BFT_start`. If picker selects ≤ 1600ms, request the `BFT_start=0` point (per Q2 approximation). If picker selects ≥ 2000ms, request the matching point; if missing, render N/A.
- **PSigs / QBFT family**: keep existing pipeline-shift (`shiftCell` call).

#### 5.2 `shiftCell` cleanup at line 1402

Remove the OBFT-family branch entirely (was: filter samples where `broadcasts[i] >= slotStart`). Keep only the pipeline-shift branch for PSigs / QBFT.

#### 5.3 Label rename

Rename UI control "slot_start" → "BFT_start" everywhere it appears in `app.js`. Update tooltip text.

#### 5.4 Picker values

Picker keeps the existing `{0, 400, 800, 1000, 1200, 1400, 1500, 1600, 2000, 2400, 2800}` values. For OBFT-family:
- `[0, 1600]` → reuses BFT_start=0 cell (per Q2).
- `{2000, 2400, 2800}` → uses the matching pre-computed cell.

Add a small UI affordance (footnote or tooltip) noting that OBFT-family values in `[0, 1600]` reuse the BFT_start=0 data (close-to-ground-truth approximation).

## Cleanups (in-scope, per user request)

1. **Drop dead `if fa < 0` clamp** at [obft/adapter.go:144](../protocol/v2/consensustest/obft/adapter.go:144) — subsumed by the new `max(bftStart, …)`.
2. **Drop dead `if fetchAt[k] < 0` clamp** at [twoab/adapter.go:177](../protocol/v2/consensustest/twoab/adapter.go:177) — same.
3. **Drop dead `if bt < 0` clamp** at [twoab/adapter.go:219](../protocol/v2/consensustest/twoab/adapter.go:219) — same.
4. **Fix dead-code zero** at [qbft/adapter.go:138](../protocol/v2/consensustest/qbft/adapter.go:138) — was `bftStart := time.Duration(0)`, becomes `bftStart := cfg.BFTStart`.
5. **Update `Config` comment** at [protocol.go:61](../protocol/v2/consensustest/protocol.go:61) — describe BFTStart as BFT-activity-start anchor, not "slot start" (per §2 above).
6. **Refresh twoab BFT_start comments** at [twoab/adapter.go:115-148](../protocol/v2/consensustest/twoab/adapter.go:115) — these were written aspirationally referencing BFT_start as a runtime parameter; update to describe the actual now-implemented behavior.
7. **Update `Stamp the deciding-layer broadcast deadline` comment** at [obft/adapter.go:179-183](../protocol/v2/consensustest/obft/adapter.go:179) — currently references "the reporting layer's slot_start adjustment to model a late-joining operator", which is no longer the model. Update to reference the BFT_start-aware schedule.

## Verification plan

1. **Regression at BFTStart=0**: existing tests must pass unchanged. Specifically [obft/adapter_test.go](../protocol/v2/consensustest/obft/adapter_test.go) and [twoab/adapter_test.go](../protocol/v2/consensustest/twoab/adapter_test.go) at default config produce identical Outcomes byte-for-byte to the pre-change baseline. Bit-identity verified via the existing seed-fixed test approach.
2. **Boundary test: BFTStart < T_commit−B_0**: add a test at e.g. `BFTStart=500ms` (below the clamp threshold). `T_broadcast_max_0` should be unchanged; `DecidingBroadcastTime` for L_0-decided samples should equal the BFTStart=0 value.
3. **Boundary test: BFTStart > T_commit−B_0**: add a test at e.g. `BFTStart=3000ms` (above the clamp threshold). `T_broadcast_max_0` should clamp to 3000; `DecidingBroadcastTime` should equal `BFTStart`. Cluster should still decide if `BFTStart ≤ T_commit`.
4. **Boundary test: BFTStart > T_commit**: e.g. `BFTStart=4000ms`. Cluster should MISS with an appropriate `MissReason`.
5. **Sweep cost**: `BFT_STARTS=0,2000,2400,2800 make stresstest` should produce ~4× the OBFT-family cells of the baseline run, with all 4 distinguishable in `data.js` by `Fields.BFT_start`.
6. **UI integration**: open `stresstest-report/index.html` and verify:
   - Picker shows "BFT_start" label.
   - For OBFT-family at picker values `[0, 1600]`: cell numbers identical to BFT_start=0.
   - For OBFT-family at picker values `{2000, 2400, 2800}`: cell numbers come from the pre-computed sims (verify in DevTools).
   - PSigs/QBFT cells: pipeline-shift behavior unchanged.

## Migration / backward-compat

Existing data.js files have no `BFT_start` key in their Fields. The UI's lookup should treat absent `BFT_start` as 0 (`(f.BFT_start ?? 0) === bftStart`). For old files, only the BFT_start=0 picker returns a hit; non-zero values for OBFT-family render N/A. Mixed-vintage data.js renders correctly: each `(N, K, BTT, profile, instability, BFT_start)` tuple is its own point.

## Effort estimate

| Area | LOC | Risk |
|---|---|---|
| Sim adapter changes (OBFT, 2abOBFT, QBFT, des audit) | ~80 | Low — formula is well-defined |
| SimConfig rename + PSigs field rename | ~20 | Low — pure rename |
| Sweep matrix + env-var plumbing | ~50 | Low |
| UI shiftedCell rewrite + label rename | ~120 | Medium — UI logic split needs care |
| Cleanups (comments + dead-code drops) | ~30 | Low |
| Boundary tests | ~150 | Low |
| **Total** | **~450** | **Medium** overall |

## Out of scope (deferred)

- True late-joining-operator scenario (would need a new scenario class with per-operator `evtStartInstance` delay; not a UI knob).
- Spec doc rename of `slot_start` → `BFT_start` (the two are meaningfully distinct in spec context).
- Extending the BFT_start sweep to other sweeps beyond `p2p_baseline`.
- QBFT/PSigs sims at non-zero `BFTStart` (unnecessary — UI pipeline-shift covers it).

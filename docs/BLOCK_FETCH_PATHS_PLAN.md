# Block-header fetch paths — plan

## Goal

Split the SSV block-header fetch logic in `getProposalParallel` into three explicit paths, with a new slot-relative `ProposalSoftDeadline` setting replacing the dangerously-large 1800ms default `proposalSoftTimeout` for new operators while preserving full backward compatibility for legacy configurations.

## Motivation

Current code ([`beacon/goclient/proposer.go:176-316`](beacon/goclient/proposer.go)) has two structural problems:
1. **The 1800ms default `proposalSoftTimeout` is dangerously large.** If multi-BN setup returns all-vanilla responses (mev-boost loses relays, no MEV available, etc.), SSV waits the full 1800ms before returning, pushing the worst-case slot budget past the 4000ms deadline.
2. **Early-exit on first blinded defeats multi-BN scoring.** The fastest BN's blinded response wins regardless of bid value; operators intentionally running multiple BNs to cross-compare bids get no benefit from that setup.

## Three-path design

### Path selection algorithm

Step 1 — startup validation (in `cli/operator/node.go`, before `NewOptions` is called so we can see operator-set vs defaulted values):

```
if (ProposerDelay > 0 || ProposalSoftTimeout is operator-set) && ProposalSoftDeadline is operator-set:
    → REJECT startup with a clear error message:
      "ProposalSoftDeadline conflicts with legacy ProposerDelay/ProposalSoftTimeout config —
       remove one. See docs/MEV_CONSIDERATIONS.md for path selection guidance."
```

Step 2 — path selection (after validation passes):

```
if ProposerDelay > 0 || ProposalSoftTimeout is operator-set:
    → Path 0 (legacy)         — preserves current behavior bit-for-bit
    → ALSO: log WARN at startup nudging migration ("There is a better way to opt into MEV;
       see docs/MEV_CONSIDERATIONS.md")
elif ProposalSoftDeadline is operator-set:
    → Path 2 (MEV-optimized)  — no early-exit, wait until deadline
else:
    → Path 1 (safe, default)  — early-exit on first blinded, fallback at DefaultProposalSoftDeadline
```

Selection happens once at startup based on operator-provided config (before defaults are applied).

### Path 0 — legacy (backward-compat)

**Trigger:** operator sets `ProposerDelay > 0` OR explicitly sets `WITH_PROPOSAL_SOFT_TIMEOUT`.

**Behavior:** unchanged from current code. Preserves:
- The `proposalSoftTimeout -= proposerDelay` reduction logic in [`options.go:54-56`](beacon/goclient/options.go).
- The relative-duration semantics of `proposalSoftTimeout`.
- Early-exit on first blinded response.
- Fallback to "wait for first valid response" after soft timeout.
- The 500ms floor on the timeout.

**Defaults:** `ProposalSoftTimeout = 1800ms` (unchanged), reduced by `ProposerDelay`.

**Documentation:** lives in Appendix A of `MEV_CONSIDERATIONS.md` (already framed as legacy).

### Path 1 — safe (new default)

**Trigger:** neither `ProposerDelay` nor `ProposalSoftTimeout` set; `ProposalSoftDeadline` also not set.

**Behavior:**
- (a) Multi-BN parallel fetch, early-exit on first blinded response.
- (b) If no blinded response by `DefaultProposalSoftDeadline`, return best-so-far (or wait for first valid response if nothing yet).

**Defaults:** `DefaultProposalSoftDeadline = 1000ms` (slot-relative).

**Rationale for 1000ms:** Worst-case slot budget under 2-round QBFT is:
```
1000ms (deadline) + 2500ms (QBFT) + 350ms (signing+submission) = 3850ms < 4000ms ✓
```
Reasonable variance margin even if every component runs slow.

### Path 2 — MEV-optimized (opt-in)

**Trigger:** operator explicitly sets `ProposalSoftDeadline` (and `ProposerDelay`/`ProposalSoftTimeout` are not set).

**Behavior:**
- Multi-BN parallel fetch, **no early-exit on first blinded**.
- Collect all BN responses until `ProposalSoftDeadline` (slot-relative).
- At deadline: return highest-value-scored response among all received.
- If nothing received by deadline: fall through to "wait for first valid response," bounded by slot deadline.

**Validation at startup:**
- `ProposalSoftDeadline` must be in `[DefaultProposalSoftDeadline (1000ms), 3600ms]`.
- Reject startup with clear error if out of range.
- Log warning (not error) if `> 1800ms`: "ProposalSoftDeadline > 1800ms leaves no budget for QBFT round 2 — slot will be missed if round 1 fails. See docs/MEV_CONSIDERATIONS.md."

**Rationale:** This is the path advanced operators use after configuring PBS-side timing games (mev-boost ≥ v1.11 or commit-boost). Matching `ProposalSoftDeadline` to the PBS-side `late_in_slot_time_ms` + ~50ms BN→SSV transport gives SSV the full PBS-cutoff value to compare bids across multiple BNs.

## Single-BN behavior

Unchanged. Single-BN goes through the existing direct `fetchProposal` call at [`proposer.go:103-107`](beacon/goclient/proposer.go) regardless of selected path. The path distinction is a no-op for single-BN (no other BNs to compare against; no early-exit decision to make).

## Configuration surface

### New fields

- `ssv.SSVOptions.ProposalSoftDeadline time.Duration` (YAML: `ProposalSoftDeadline`, env: `WITH_PROPOSAL_SOFT_DEADLINE`).
  - Zero value = not set = use path 1 default.
  - Non-zero value = path 2 opt-in.

### Existing fields (preserved)

- `ProposerDelay` — unchanged behavior.
- `WITH_PROPOSAL_SOFT_TIMEOUT` env var / `ProposalSoftTimeout` field — unchanged behavior. Only meaningful in path 0.
- `AllowDangerousProposerDelay` — unchanged. Still gates `ProposerDelay > 1000ms`.

### Validation

At startup, in node config validation:
1. **Reject** if `(ProposerDelay > 0 || ProposalSoftTimeout is operator-set) && ProposalSoftDeadline is operator-set` — see Step 1 above. Validation returns an error; startup fatals.
2. Determine which path applies based on the algorithm above.
3. If path 0: existing validation only (`AllowDangerousProposerDelay` cap); also emit the migration WARN log noted above.
4. If path 2: enforce `[1000ms, 3600ms]` range; warn (log) if `> 1800ms` (round-2 fallback won't fit).
5. Log the selected path at startup: e.g. `"block-fetch path: safe (default)"`, `"block-fetch path: legacy (ProposerDelay=300ms)"`, `"block-fetch path: MEV-optimized (ProposalSoftDeadline=1100ms)"`.

**Single-BN + ProposalSoftDeadline**: no special handling — operator can set the field with a single BN, it just has no effect (single-BN bypasses the parallel-fetch logic that uses the deadline).

## Implementation notes

### Detecting "operator explicitly set" vs "defaulted"

The `cleanenv` library populates the struct from YAML/env, then default values are applied later in `NewOptions`. To detect "operator set" vs "defaulted":

- Option A: track via a pointer (`*time.Duration`) — nil means "not set."
- Option B: track via a separate `bool` field (`ProposalSoftTimeoutWasSet`).
- Option C: do the path selection in `cli/operator/node.go` config validation **before** `NewOptions` is called.

(C) is cleanest — config validation runs first, picks the path, and stores the result for the rest of the code to use.

### Function signatures

`GetBeaconBlock` and `getProposalParallel` either:
- Branch internally based on selected path (single function, conditional logic).
- Get dispatched to separate functions per path (cleaner but more code).

Probably split into `getProposalParallelSafe` (path 1) and `getProposalParallelMEVOptimized` (path 2), with path 0 retaining its current code. Dispatch happens in `GetBeaconBlock`.

### Backward-compat test cases

Unit tests should cover:
- Only `ProposerDelay` set → path 0.
- Only `ProposalSoftTimeout` set → path 0 (with default `ProposalSoftDeadline` ignored).
- Both `ProposerDelay` and `ProposalSoftTimeout` set → path 0.
- Both `ProposerDelay` and `ProposalSoftDeadline` set → path 0 wins (legacy precedence); log notes the ignored `ProposalSoftDeadline`.
- Only `ProposalSoftDeadline` set → path 2.
- Nothing set → path 1.
- `ProposalSoftDeadline` out of range → startup rejected with error.
- `ProposalSoftDeadline > 1800ms` → startup succeeds + warning logged.

## Matching `ProposalSoftDeadline` to PBS config

For operators following the path-2 setup with mev-boost/commit-boost:

```
ProposalSoftDeadline ≈ PBS late_in_slot_time_ms + ~50ms
```

The +50ms covers BN→SSV transport so the deadline lands *after* the latest expected header arrival, giving the scoring loop a chance to collect all BN responses.

Examples (assuming the worked configs in `MEV_CONSIDERATIONS.md`):
- Example A — `late_in_slot_time_ms = 1050ms` → `ProposalSoftDeadline = 1100ms`.
- Example B — `late_in_slot_time_ms = 1800ms` → `ProposalSoftDeadline = 1850ms`.

Both inside the `[1000ms, 3600ms]` validation range; Example B is below the 1800ms warn threshold.

## `MEV_CONSIDERATIONS.md` updates

The current doc references `proposalSoftTimeout` in several places. After this change:

1. **§4 Example A and B**: add `ProposalSoftDeadline` config lines to the SSV-side config snippet (currently shows PBS configs only).
   - Example A: `ProposalSoftDeadline: 1100ms`
   - Example B: `ProposalSoftDeadline: 1850ms`
2. **§6 Interaction with `ProposerDelay`**: rewrite as "Configuration paths" — explain the three-path model and the selection algorithm.
3. **Appendix A**: re-label explicitly as "Path 0 (legacy)." Keep the analysis but make it clear this is no longer the recommended path.
4. **Tuning section** (`Where the auction window should land`): note that the `ProposalSoftDeadline` is now also part of the per-cutoff math, not just the PBS `late_in_slot_time_ms`.
5. **TL;DR**: add a short mention of the path model — "default operators land on path 1 (safe); advanced operators opt into path 2 by setting `ProposalSoftDeadline`."

## Scope of code changes

Files touched:
- `cli/operator/node.go` — add `ProposalSoftDeadline` field, path-selection logic in `validateConfig`, startup logging.
- `beacon/goclient/options.go` — add new field; preserve `ProposalSoftTimeout` and its existing default/reduction logic (path 0); add `DefaultProposalSoftDeadline = 1000ms`.
- `beacon/goclient/proposer.go` — path dispatch in `GetBeaconBlock`; new `getProposalParallelMEVOptimized` function (or equivalent).
- `cli/operator/node_test.go` — backward-compat test cases for path selection.
- `beacon/goclient/proposer_test.go` — behavior tests for paths 1 and 2.
- `config/config.example.yaml` — add `ProposalSoftDeadline` comment block.
- `docs/MEV_CONSIDERATIONS.md` — all the rewrites listed above.

## Resolved decisions

1. **Path 0 + ProposalSoftDeadline both set** → reject at startup (validation error → fatal). No silent precedence.
2. **Path 1 default = 1000ms** — confirmed.
3. **Path 2 lower bound = 1000ms** — confirmed.
4. **Path 2 upper bound = 3600ms with warn-but-allow above 1800ms** — confirmed. No `AllowDangerousProposalSoftDeadline` flag needed.
5. **Telemetry**: not added. Operators self-knowing their setup is sufficient; no SSV-network-wide visibility need.
6. **Migration nudge for path 0**: yes — log a WARN at startup along the lines of *"There is a better way to opt into MEV — see docs/MEV_CONSIDERATIONS.md"*.
7. **Single-BN + ProposalSoftDeadline**: silently accept (no special handling). Setting has no effect since single-BN bypasses parallel fetch; no warning, no rejection.

## Implementation choices

- **Where path selection happens**: `cli/operator/node.go` validation, before `NewOptions` applies defaults. Stores the selected path in the config object for the rest of the code to consume.
- **Code split**: `getProposalParallel` is split into `getProposalParallelSafe` (path 1) and `getProposalParallelMEVOptimized` (path 2). Path 0 retains its existing code at the current `getProposalParallel` (renamed or kept, TBD during implementation). Dispatch happens in `GetBeaconBlock`.

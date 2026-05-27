# Stresstest Progress-Bar Desync Plan

Design plan for fixing the `make stresstest` progress renderer's stacking artefact — where a stale earlier frame remains visible above the current frame, separated by a clean visual seam. Today's `ProgressTracker` in [protocol/v2/consensustest/progress.go](protocol/v2/consensustest/progress.go) is the sole writer to its output stream **only in theory**. In practice, `t.Logf` lines streamed by `go test -v` from [stress_test.go](protocol/v2/consensustest/stress_test.go) interleave between tracker frames, breaking the cursor-rewind math the in-place redraw depends on. This plan researches the failure mode, validates the chosen fix, and lays out the implementation.

## Background — observed symptom

A 16-minute `make stresstest` run produced a terminal showing two distinct progress blocks one above the other:

- **Upper block** (stale): header `11.2% of 51,225,808 sims (elapsed: 9m9s)` + 6 protocol bars (`OBFT-0` through `2abOBFT-300`).
- **Lower block** (current): header `19.9% of 51,225,808 sims (elapsed: 15m37s)` + all 13 protocol bars.

No log text visible between them. Above the upper block: a wrapped `t.Logf` summary line listing every per-point label of one sweep.

The two blocks are ~6.5 minutes apart in elapsed time — they are not adjacent frames; the renderer ticked many times between them. So the redraw is clearly failing to overwrite the upper block, and subsequent ticks correctly overwrite each other (otherwise we'd see N stacked blocks, not 2).

## Diagnosis

Three pieces line up:

1. **Cursor-rewind math in [`redraw`](protocol/v2/consensustest/progress.go:169)**: `\033[<prevLines-1>A` (cursor-up by logical-line count) followed by `\r\033[J` (CR + erase-to-end-of-screen), then write the new frame. Assumes the cursor is exactly where the previous frame's write left it.

2. **`go test -v` streams `t.Logf` live for top-level tests** (subtests buffer; `TestStress` is a top-level test, no `t.Run`). The test binary's stdout is the same terminal device the renderer writes to. Foreign writes between frames move the cursor without the tracker's knowledge.

3. **The sweep-start log line at [stress_test.go:490](protocol/v2/consensustest/stress_test.go:490) is a single logical line containing every `SweepPoint.Label`**:
   ```go
   t.Logf("    sweep %s: %d sweep points [%s] × baseline=%d unstable=%d ...",
       sw.Name, len(sw.Points), strings.Join(pointLabels, ", "), ...)
   ```
   For `p2p_baseline` at default axes the label set is `len(SeverProbValues) × len(bttValues) × len(profiles) × len(InstabilityLevels) × len(faultyNodes)` = 4 × 4 × 8 × 5 × 2 = **1280 labels** of ~60 chars each, joined by `", "` into one logical line that wraps to many physical rows in any normal terminal width.

### Visual reconstruction (mechanism, not pixel-exact)

The mechanism: at some tick, the renderer's `\033[<prevLines-1>A` cursor-up doesn't reach the top of the previous frame because foreign content (one or more t.Logf wrap rows, a resize event, etc.) sits between the renderer's expected cursor position and the actual position. `\r\033[J` then erases only the part of the frame that's reachable; the inaccessible top rows of the frame stay on screen as a permanent artefact, and subsequent ticks redraw the new frame correctly *below* the stale rows.

Precise reconstruction of the user's screenshot is awkward — measuring backward, the wrap-row count for the p2p_baseline sweep-start `t.Logf` at default axes is ~100–400 physical rows (much more than the few rows the artefact would imply if it were a simple "rewind-misses-by-N" desync of that one line). The actual trigger for the observed split may be a smaller secondary write (a different sweep boundary firing while we're already in a desynced state, a terminal resize, etc.) rather than the visible sweep-start label dump alone. **The fix doesn't depend on the exact trigger** — `Log` routing eliminates all interleaved-write desync regardless. The "+N more" rollup mechanism in [progress.go:251-265](protocol/v2/consensustest/progress.go:251) is what produced the 6-bars-without-rollup shape we see in block 1; it shows `budget - 1` bars individually and rolls the rest into one aggregate line, so block 1 at the time of its draw was rendered with that layout, and the rollup line got erased while the bars above didn't.

## Goal

Make `ProgressTracker` the **single coordinated writer** to its output stream during its active lifetime, so any text the test driver wants to emit between frames is funnelled through the tracker and serialised with its redraw cycle. The tracker's `prevLines` invariant — "cursor is at the end of the last block I wrote" — must hold across foreign-write attempts because the tracker performs the foreign writes itself, with explicit before/after coordination.

## Why this approach

The pattern is **shared with `tqdm` (Python)** and similar mature progress libraries: the progress object exposes a `write` / `log` method that interleaves correctly with the live display. Anything not routed through that method is a known weakness, accepted as out of scope.

Other approaches considered (see [§Alternatives](#alternatives-considered) for full reasoning):

- **Alternate-screen mode** doesn't actually isolate writes from the same process — `t.Logf` would still land in the alt buffer. And exit loses the live history users want.
- **Scroll regions (`\033[T;B r`)** have uneven emulator support and complex dynamic-height interactions.
- **DSR cursor-position query (`\033[6n`)** requires a read side on /dev/tty plus async response parsing.
- **Just shorten the verbose `t.Logf`** is the cheapest fix; rejected as fragile (next added log line reintroduces the bug). But still worth doing as an auxiliary cleanup — see [§Auxiliary fix](#auxiliary-fix-shorten-the-sweep-start-log).

## Proposed design

### New API on `ProgressTracker`

```go
// Log writes a line through the tracker so it interleaves cleanly with the
// live progress block: while the renderer is active, it clears the in-place
// block, writes the line as normal scroll history, then re-emits the block
// beneath it. Outside the renderer's active lifetime (before StartRenderer,
// after stop), it falls back to a plain write. Safe for concurrent use; nil
// receiver is a no-op (matches Add).
func (p *ProgressTracker) Log(format string, args ...any)
```

### State additions to `ProgressTracker`

```go
type ProgressTracker struct {
    // ... existing fields ...

    outMu  sync.Mutex   // serialises all writes to w and protects prevLines
    w      io.Writer    // set in StartRenderer
    tty    bool         // set in StartRenderer
    active bool         // true between StartRenderer entry and stop completion
}
```

### Lifecycle invariants

- **Pre-`StartRenderer`**: `w == nil`, `active == false`, `prevLines == 0`. `Log` falls back to `os.Stderr`.
- **Active (between `StartRenderer` and `stop`)**: `w` set, `active == true`. Renderer goroutine emits every tick under `outMu`. `Log` calls clear, write, re-emit under `outMu`.
- **Post-`stop`**: `active == false`, `prevLines == 0` (cleared by `stop` after the trailing newline parks the cursor below the block). `Log` falls back to a plain write to `w`.

### Mirror-to-stderr for captured invocations

Routing logs through `progress.Log` writes to the tracker's `w` (`/dev/tty` or `os.Stderr` fallback). On `make stresstest 2>&1 | tee out.log` style invocations, `/dev/tty` is the live terminal but `os.Stderr` is a pipe to `tee` — Log writes go to `/dev/tty` only, so `tee` doesn't capture the mid-run sweep-boundary lines. To preserve capture without double-printing on the interactive terminal, `Log` *also* writes the line to `os.Stderr` when:

- `w != os.Stderr` (the tracker is using `/dev/tty`, not the stderr fallback), AND
- `!isCharDevice(os.Stderr)` (stderr is redirected away from the terminal — into a pipe / file).

Behaviour by environment:

| Invocation | `progressOut` | stderr | Mirror fires? | Effect |
|---|---|---|---|---|
| `make stresstest` (TTY, no redirect) | `/dev/tty` | char dev (terminal) | no | one write to terminal |
| `make stresstest 2>&1 \| tee out.log` | `/dev/tty` | pipe | yes | terminal sees coordinated write; `out.log` captures via stderr |
| `make stresstest > out.log 2> err.log` | `/dev/tty` if open succeeds | regular file | yes | terminal renders block; `err.log` captures lifecycle lines |
| CI (no TTY) | `os.Stderr` (fallback) | usually pipe / log file | no (first condition fails) | one write to stderr |

Costs nothing in the common case (interactive, no piping) and recovers the tee-capture path the original plan would have lost.

### Concurrency model

One mutex (`outMu`) serialises all output to `w`:

- Renderer goroutine: acquires `outMu` around each tick's `emit`.
- `Log` callers: acquire `outMu` around the clear + write + redraw triple.
- `stop`: acquires `outMu` around the final emit + trailing newline + state reset.

The renderer ticks at 1 Hz on a TTY (30 s on a non-TTY). `Log` calls fire at sweep / pair boundaries — typically O(10-100) per run, well-spaced relative to the tick. Contention is negligible.

## Implementation plan

### [progress.go](protocol/v2/consensustest/progress.go)

1. **Add `outMu`, `w`, `tty`, `active` fields** to `ProgressTracker`.
2. **Refactor write paths to `*Locked` form**: split `emit` and `redraw` so the locking happens in one place. The locked variants assume the caller holds `outMu`; the public-API variants acquire and call the locked form.
3. **`StartRenderer` setup**: under `outMu`, store `w`, `tty`, set `active = true`. Renderer goroutine acquires `outMu` per tick.
4. **`stop` teardown**: idempotent via existing `sync.Once`. Under `outMu`: final `emitLocked`, trailing newline (TTY only), then `active = false`, `prevLines = 0`.
5. **`Log` method**:
   ```go
   func (p *ProgressTracker) Log(format string, args ...any) {
       if p == nil {
           return // matches Add's nil-receiver no-op
       }
       msg := fmt.Sprintf(format, args...)
       p.outMu.Lock()
       defer p.outMu.Unlock()
       if p.w == nil {
           // Renderer never started — fall back so callers aren't required
           // to gate on lifecycle. Stderr is what t.Logf would have used.
           fmt.Fprintln(os.Stderr, msg)
           return
       }
       if p.active && p.tty && p.prevLines > 0 {
           // Erase the live block; cursor lands at its top, ready to scroll
           // the log line into history.
           if p.prevLines > 1 {
               fmt.Fprintf(p.w, "\033[%dA", p.prevLines-1)
           }
           fmt.Fprint(p.w, "\r\033[J")
           p.prevLines = 0
       }
       fmt.Fprintln(p.w, msg)
       // Mirror to stderr when stderr is a captured (non-TTY) FD different
       // from our writer, so `tee` / shell-redirected runs preserve the
       // log line. Skipped when w==stderr (would double-write to the same
       // FD) and when stderr is the user's terminal (would double-print).
       if p.w != os.Stderr && !isCharDevice(os.Stderr) {
           fmt.Fprintln(os.Stderr, msg)
       }
       if p.active {
           p.emitLocked(p.w, p.tty) // redraw the block beneath
       }
   }
   ```

### [stress_test.go](protocol/v2/consensustest/stress_test.go) — route lifecycle logs through `Log`

Replace `t.Logf` with `progress.Log` for every line that fires **between** `progress.StartRenderer(...)` ([line 449](protocol/v2/consensustest/stress_test.go:449)) and the deferred `stopProgress()` returning:

| Location | Current | Replace with |
|---|---|---|
| [:466](protocol/v2/consensustest/stress_test.go:466) | `fmt.Fprintf(progressOut, "\ninterrupt — ...")` | `progress.Log("interrupt — ...")` (drop the leading `\n` — `stop` already parks the cursor) |
| [:477](protocol/v2/consensustest/stress_test.go:477) | `t.Logf("=== %d (n, K) ...", ...)` | `progress.Log("=== ...")` |
| [:483](protocol/v2/consensustest/stress_test.go:483) | `t.Logf("--- [%d/%d] n=%d K=%d", ...)` | `progress.Log("--- ...")` |
| [:490](protocol/v2/consensustest/stress_test.go:490) | `t.Logf("    sweep %s: ...", ...)` | `progress.Log("    sweep ...")` **+ shorten — see [§Auxiliary fix](#auxiliary-fix-shorten-the-sweep-start-log)** |
| [:495](protocol/v2/consensustest/stress_test.go:495) | `t.Logf("        %s wallclock: ...", ...)` | `progress.Log("        ...")` |
| [:524](protocol/v2/consensustest/stress_test.go:524) | `t.Logf("    n=%d K=%d wallclock: ...", ...)` | `progress.Log("    ...")` |
| [:529](protocol/v2/consensustest/stress_test.go:529) | `t.Logf("interrupted: ...", ...)` | `progress.Log("interrupted: ...")` |

**Keep as `t.Logf`** (pre-renderer or post-stop, no desync risk):

- [:228](protocol/v2/consensustest/stress_test.go:228) — `LAYERS_K skipped` (setup, pre-renderer)
- [:257](protocol/v2/consensustest/stress_test.go:257) — `=== ... opted into ModeStress` (setup, pre-renderer)
- [:534-547](protocol/v2/consensustest/stress_test.go:534) — final summary + smoke check (see the explicit-stop step below)

**Add an explicit `stopProgress()` between the main loop and the final summary.** The originally-relied-on `defer stopProgress()` from [stress_test.go:450](protocol/v2/consensustest/stress_test.go:450) fires when the test function *returns* — i.e. **after** the final summary `t.Logf`s execute. Those calls would otherwise interleave with a still-ticking renderer (the same desync class). Insert:

```go
} // end of `for pairIdx, w := range work { ... }` loop

stopProgress() // explicit stop; the deferred call below is idempotent (sync.Once)

t.Logf("Report data written: %s/data.js", dir)
// ... rest of final summary + smoke check stays as t.Logf
```

The deferred `stopProgress()` at [:450](protocol/v2/consensustest/stress_test.go:450) stays as a safety net for early-return panics — the body's `sync.Once` makes a second invocation a no-op.

### Auxiliary fix — shorten the sweep-start log

Even with `Log` routing, the per-point label dump at [stress_test.go:490](protocol/v2/consensustest/stress_test.go:490) is genuine noise: every sweep boundary forces a clear-write-redraw cycle that pushes ~tens of KB of scroll history. Replace with a one-line summary, e.g.:

```
    sweep p2p_baseline: 1280 points × baseline=4000 unstable=1 iter × 34 scenarios × 13 protocols
```

Drop the `strings.Join(pointLabels, ", ")` and the protocol-name dump. Per-point labels are visible in the report UI itself; the running terminal log doesn't need them.

### [progress_test.go](protocol/v2/consensustest/progress_test.go) — coverage

New tests, all following the existing `bytes.Buffer` + direct-method-call style (see [`TestEmitMultiBar`](protocol/v2/consensustest/progress_test.go:199) and [`TestRedrawRewindsByPreviousHeight`](protocol/v2/consensustest/progress_test.go:247) for the template):

- **`TestLog_ClearsAndRedraws`**: with `prevLines=8`, `tty=true`, `active=true`, calling `Log("x")` writes `\033[7A`, `\r\033[J`, `"x\n"`, then a fresh frame. Assert byte sequence on a `bytes.Buffer` writer.
- **`TestLog_NoRedrawPostStop`**: after `stop` returns, `Log("y")` writes only `"y\n"` (no cursor escapes).
- **`TestLog_BeforeStart`**: with `w==nil`, `Log` writes to `os.Stderr` (or a test-substituted writer via a small seam) and doesn't touch tracker state.
- **`TestLog_NilReceiver`**: nil-tracker `Log` is a no-op.
- **`TestLog_MirrorsToStderr`**: when `w != os.Stderr` and stderr is a regular (non-char) file, `Log` writes once to `w` and once to stderr (use a seam — substitute stderr inside the tracker — or factor the `os.Stderr` reference behind a package-level var that tests overwrite). When stderr is a char device, no mirror fires.

No goroutine-concurrency test — the existing tests don't exercise the renderer goroutine at all (only the synchronous build blocks). The mutex invariant is documented; a contrived race test would add complexity without proportional confidence, since the goroutine paths aren't otherwise covered.

## Resolved research questions

All 11 questions from the initial draft have been investigated. The headings are kept for traceability; the resolution is captured under each one. Where a resolution required a plan amendment, the change is noted in-line and folded into [§Implementation plan](#implementation-plan) above.

### 1. Confirm `go test -v` really streams `t.Logf` live for top-level tests

**Resolved.** The screenshot itself proves it: the visible mid-line `t.Logf` content reaches the terminal before the test exits. Go's `testing` package writes top-level `t.Logf` to stdout via `chatty`'s flush on every call when `-v` is set; subtests buffer until their subtree completes, but `TestStress` has no `t.Run` calls. Diagnosis premise holds.

### 2. Verify the sweep-start wrap math against the actual terminal width

**Partially resolved — diagnosis softened.** Computing forward: at default axes the p2p_baseline sweep-start log is ~80 KB (1280 labels × ~62 chars + separators); at the user's likely BTT-restricted run still ~21 KB. Either wraps to ≫7 physical rows at any normal terminal width. The actual trigger for the observed split is likely *not* the visible sweep-start label dump alone — more plausibly a smaller secondary write (a different sweep boundary firing while the renderer is already in a desynced state, a terminal resize, etc.). Mechanism is right; precise pixel match is not. §Diagnosis updated to reflect this.

### 3. Confirm no other writers bypass the tracker

**Resolved — six error-path sites found; accepted as scope-out.** Beyond the planned routing, these `t.Logf` calls fire during the renderer's active lifetime:

| Location | When |
|---|---|
| [runner.go:81](protocol/v2/consensustest/runner.go:81) | adapter `Run` returned an unexpected error |
| [runner.go:98](protocol/v2/consensustest/runner.go:98) | adapter didn't terminate cleanly |
| [batch.go:438](protocol/v2/consensustest/batch.go:438) | per-cell unexpected error class |
| [batch.go:457](protocol/v2/consensustest/batch.go:457) | non-uniform `ErrNotApplicable` (adapter bug) |
| [batch.go:475](protocol/v2/consensustest/batch.go:475) | non-uniform `ErrConfigOutOfEnvelope` (adapter bug) |
| [batch.go:521](protocol/v2/consensustest/batch.go:521) | sim panic stack |

None carries `BatchConfig`; routing would require threading `cfg.Progress` through the worker layer or moving Progress onto `SimConfig`. All fire on error / anomaly paths and should be rare on a clean run. **Decision: leave as `t.Logf`, accept a one-time desync glitch if/when they fire.** When the operator hits one of these they're already debugging — a cosmetic frame stack is acceptable next to the actual error message. Listed in [§Known limitations](#known-limitations) below.

Direct writes besides `t.Logf` in the stress path: only [stress_test.go:466](protocol/v2/consensustest/stress_test.go:466) (interrupt notice), already covered by the routing plan. No others.

### 4. Mutex contention & testability

**Resolved.** [progress_test.go](protocol/v2/consensustest/progress_test.go) uses `bytes.Buffer` writers + direct method calls; no injectable ticker. New `Log` tests follow the same template — see [§progress_test.go coverage](#progress_testgo--coverage). No concurrent-goroutine test (overkill given the existing tests don't exercise the renderer goroutine either).

### 5. Multi-write atomicity vs single syscall

**Resolved.** `outMu` serialises tracker-internal writes. Foreign writes to the same FD outside `Log` can still corrupt the display — that's the class we're fixing. `Log`'s doc-comment will state the invariant: anything routed through `Log` is coordinated; bypasses are on their own.

### 6. Terminal compatibility

**Resolved.** `\033[NA` and `\033[J` are VT100 baseline; supported in Terminal.app, iTerm2, alacritty, kitty, Windows Terminal, VS Code (xterm.js), tmux, screen, ssh. The `isCharDevice` fallback at [progress.go:443](protocol/v2/consensustest/progress.go:443) drops to plain-text periodic mode for non-TTYs (CI / piped). No compatibility concerns; nothing new to research.

### 7. Output destination during CI / piped runs

**Resolved — mirror-to-stderr added to the design.** The naive routing loses the per-sweep log lines for `make stresstest 2>&1 | tee out.log` callers (lines go to `/dev/tty`, not captured by `tee`). Mitigation: `Log` also writes to `os.Stderr` when `w != os.Stderr` and stderr is not a char device. See [§Mirror-to-stderr for captured invocations](#mirror-to-stderr-for-captured-invocations) for the truth table and rationale; the `Log` code sketch in [§Implementation plan](#implementation-plan) includes this.

### 8. Interrupt-handler timing

**Resolved.** Under the fix, `stopProgress()` parks the cursor below the cleared block (via the trailing `Fprintln` in stop). The current notice's leading `\n` at [stress_test.go:466](protocol/v2/consensustest/stress_test.go:466) becomes redundant — drop it. The routing-table entry calls this out.

### 9. Final summary lines stay as `t.Logf`

**Resolved — plan amended.** The `defer stopProgress()` fires when the test function returns, **after** the final summary `t.Logf`s execute. Inserted an explicit `stopProgress()` between the main loop and the final summary (see [§stress_test.go — route lifecycle logs through `Log`](#stress_testgo--route-lifecycle-logs-through-log)). The deferred call stays as a safety net.

### 10. Should `Log` fan out to `t.Logf` too?

**Resolved: no fan-out.** Fanning out would double-print on interactive terminals (terminal sees both the coordinated `/dev/tty` write and the `t.Logf` via stdout). The mirror-to-stderr resolution from Q7 captures the tee-user case without that downside.

### 11. What if a panic happens during a Log call

**Resolved.** `Log`'s body has one panic source: `fmt.Sprintf` on a bad format string. `defer p.outMu.Unlock()` releases the mutex regardless. Format-string bugs are caller errors; no explicit `recover` needed. Will be a one-liner in the doc comment.

## Testing strategy

### Unit tests ([progress_test.go](protocol/v2/consensustest/progress_test.go))

Listed in [§progress_test.go coverage](#progress_testgo--coverage). Each is a small, deterministic check on a `bytes.Buffer` writer; no real TTY required.

### Manual verification

After implementation, run `make stresstest CLUSTER_SIZES_N=4 LAYERS_K=2 BTT_VALUES_MS=300 P2P_PROFILES=prod` (a fast subset) for ~10 minutes, watch the terminal:

- No stacked frames at any point.
- Log lines from sweep boundaries appear in scrollback above the live block, properly formatted, no escape sequences leaking.
- Block redraws smoothly after each Log call.
- Block resizes correctly when terminal is resized mid-run.
- Ctrl-C produces a clean shutdown: live block cleared, interrupt notice printed, partial data saved message printed, prompt returns.

Then re-run with `CLUSTER_SIZES_N=4,7 LAYERS_K=2,4` for ~30 minutes to exercise multi-pair boundaries.

### Regression check — tee capture (mirror path)

Run via `make stresstest 2>&1 | tee /tmp/out.log`. Expected:

- Live terminal: clean in-place block + lifecycle log lines scrolling above it (the `/dev/tty` write path).
- `/tmp/out.log`: every lifecycle log line captured, no escape sequences leaking, no double-prints. Setup + final-summary `t.Logf`s captured via the test framework's stdout as usual.

### Regression check — CI / no TTY

Force the non-TTY branch with a test invocation that has no controlling terminal (e.g. running under `nohup` or a CI-style detached process), or set `progressOut` to a non-TTY file via a small test-only seam. Expected:

- 30 s periodic-line progress mode kicks in.
- Lifecycle log lines appear at proper cadence, no escapes.
- No mirror-to-stderr fires (would double-write to the same FD).

## Alternatives considered

### Alternate-screen mode (`\033[?1049h` / `\033[?1049l`)

Switching to the alternate screen buffer (used by `vim`, `less`, `top`) doesn't isolate writes from the same process — `t.Logf` would still land in the alt buffer. Also, exit returns to the primary buffer, discarding the alt buffer's contents — so the live history users want to see during the run would vanish when the test exits.

**Rejected**: doesn't solve the actual problem (foreign writes from the same process) and worsens the post-run UX.

### Scroll regions (`\033[<top>;<bottom>r`)

Reserve the bottom N rows for the progress block, let the upper region scroll naturally for log output. Used by `tmux`, `screen`. Conceptually clean.

**Rejected**: terminal-emulator support is uneven; dynamic block height (the "+N more" rollup) interacts poorly with region setup; cursor-positioning quirks across emulators (Terminal.app vs iTerm2 vs alacritty) make this fragile.

### DSR cursor-position query (`\033[6n`)

Ask the terminal where the cursor is before each redraw and self-correct. Robust against arbitrary foreign writes.

**Rejected**: requires a read side on `/dev/tty`, async response parsing, and timing handling. Complexity dwarfs the benefit for a self-correcting mechanism — especially when the proactive routing approach achieves the same end with simpler code.

### Just shorten the verbose `t.Logf`

Drop the per-point label list from [stress_test.go:490](protocol/v2/consensustest/stress_test.go:490). Cheapest fix; would massively reduce the wrap-row count of the boundary log line and make desync less visible.

**Rejected as the primary fix** — but **kept as an auxiliary cleanup** (see [§Auxiliary fix](#auxiliary-fix-shorten-the-sweep-start-log)). Any future log line added in the run loop reintroduces the bug; we want the structural guarantee, not just absence of the trigger.

### Redirect `t.Logf` to a file instead of the terminal

Drop `-v` from the Makefile invocation. `t.Logf` writes go to the test framework's internal buffer, only printed on failure. The terminal sees only the progress block.

**Rejected**: loses the live "what sweep is running now" signal that's actually useful for debugging stuck runs. Users expect to see progress text scroll past.

## Scope

In-scope:

- [protocol/v2/consensustest/progress.go](protocol/v2/consensustest/progress.go): add `outMu`, `w`, `tty`, `active` fields; refactor `emit`/`redraw` to `*Locked` forms; add `Log` method (with mirror-to-stderr); teach `StartRenderer` and `stop` to maintain `active` + `prevLines=0`-on-stop.
- [protocol/v2/consensustest/stress_test.go](protocol/v2/consensustest/stress_test.go): route lifecycle `t.Logf` calls through `progress.Log`; insert an explicit `stopProgress()` between the main loop and the final summary; shorten the sweep-start log message; drop the redundant leading `\n` from the interrupt notice.
- [protocol/v2/consensustest/progress_test.go](protocol/v2/consensustest/progress_test.go): new tests for `Log` (clear / no-redraw post-stop / pre-start fallback / nil-receiver / mirror-to-stderr).

Out of scope:

- Any change to `go test`'s stdout plumbing or `-v` semantics.
- Capturing progress lines into the test framework's log (covered in [§10](#10-should-log-fan-out-to-tlogf-too)).
- Foreign writes from outside the test driver (Go runtime panic stacks, test framework FAIL banners). Rare; the run is already failing if they fire; cosmetic glitches acceptable.
- Scroll-region or alternate-screen approaches (see [§Alternatives](#alternatives-considered)).
- Panic safety inside `Log` itself ([§11](#11-what-if-a-panic-happens-during-a-log-call)).

### Known limitations

The fix routes every `t.Logf` from [stress_test.go](protocol/v2/consensustest/stress_test.go) that fires during the renderer's lifetime, but **not** the six error-path `t.Logf`s in [batch.go](protocol/v2/consensustest/batch.go) and [runner.go](protocol/v2/consensustest/runner.go) catalogued in [§3](#3-confirm-no-other-writers-bypass-the-tracker). Threading `cfg.Progress` through those worker functions is a wider refactor than the fix warrants; those sites are reached only on adapter bugs, sim panics, or unexpected error classes — all situations where the operator is already investigating an error message and a single cosmetic stacked frame is acceptable.

If any of them starts firing frequently in practice (it shouldn't on a clean run), revisit the trade-off and either thread `Progress` through or move it onto `SimConfig`.

## Future work / cleanups noticed in passing

(The `prevLines` doc-comment refresh and the interrupt-notice leading-`\n` drop were originally listed here but are now in-scope of the main fix — see [§Implementation order](#implementation-order).)

- The fallback chain `progressOut := os.Stderr` then `/dev/tty` open ([stress_test.go:437-441](protocol/v2/consensustest/stress_test.go:437)) could be a helper if a similar pattern emerges elsewhere; for now keep inline.
- If the batch.go / runner.go error-path `t.Logf`s ([§Known limitations](#known-limitations)) ever fire often enough to matter, the natural next step is to put `Progress` on `SimConfig` rather than `BatchConfig`. That threads it through `RunScenarioOnProtocol` without changing call signatures elsewhere.

## Implementation order

Research is resolved; ready to implement. Suggested commit sequence:

1. **`progress.go` foundation** — add `outMu`, `w`, `tty`, `active`; refactor `emit`/`redraw` into `*Locked` forms; teach `StartRenderer` to record `w`/`tty`/`active=true` under the mutex; teach `stop` to clear `active` and `prevLines` under the mutex. No new public API yet; existing behaviour unchanged. Verifies the locking refactor is correct in isolation.
2. **`Log` method + tests** — add `Log` with the mirror-to-stderr branch; add the five new unit tests in [progress_test.go](protocol/v2/consensustest/progress_test.go). CI-green standalone (no caller yet).
3. **`stress_test.go` routing swap** — replace the seven lifecycle `t.Logf`s with `progress.Log`; insert explicit `stopProgress()` between the main loop and the final summary; drop the leading `\n` from the interrupt notice; shorten the sweep-start log message. Manual verification per [§Testing strategy](#testing-strategy).
4. **Doc-comment refresh** — update the `prevLines` comment at [progress.go:30-38](protocol/v2/consensustest/progress.go:30) to note `outMu` + `Log`-routing as the load-bearing invariants against foreign-write desync. Pure documentation.

Each step is a self-contained commit. No mid-state where the fix is half-applied.

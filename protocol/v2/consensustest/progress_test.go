package consensustest

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestCommaInt(t *testing.T) {
	cases := map[int64]string{
		0:        "0",
		7:        "7",
		999:      "999",
		1000:     "1,000",
		12345:    "12,345",
		689742:   "689,742",
		1000000:  "1,000,000",
		23587200: "23,587,200",
		-12345:   "-12,345",
	}
	for in, want := range cases {
		if got := CommaInt(in); got != want {
			t.Errorf("CommaInt(%d) = %q, want %q", in, got, want)
		}
	}
}

func TestBarCellsForCols(t *testing.T) {
	const nameW = 11
	// On a roomy terminal the rendered line fills the width exactly, leaving
	// blockIndent on the left and blockRightPad on the right.
	for _, cols := range []int{120, 80, 60} {
		cells := barCellsForCols(cols, nameW)
		lineW := blockIndent + nameW + perLineFixed + cells + blockRightPad
		if lineW != cols {
			t.Errorf("cols=%d: rendered line width %d, want %d (cells=%d)", cols, lineW, cells, lineW)
		}
	}
	// A too-narrow terminal clamps to the 8-cell floor.
	if got := barCellsForCols(20, nameW); got != 8 {
		t.Errorf("narrow terminal should clamp to 8, got %d", got)
	}
}

func TestProgressBar(t *testing.T) {
	const cells = fallbackBarCells
	for _, pct := range []float64{-5, 0, 0.1, 12.3, 50, 99.9, 100, 150} {
		bar := progressBar(pct, cells)
		if !strings.HasPrefix(bar, "[") || !strings.HasSuffix(bar, "]") {
			t.Fatalf("pct=%v: bar %q missing brackets", pct, bar)
		}
		inner := strings.TrimSuffix(strings.TrimPrefix(bar, "["), "]")
		// Every cell is exactly one display column, so the rune count is fixed
		// regardless of how the fractional leading edge falls.
		if n := utf8.RuneCountInString(inner); n != cells {
			t.Errorf("pct=%v: inner cells = %d, want %d (%q)", pct, n, cells, bar)
		}
	}
	if got := progressBar(0, cells); strings.ContainsRune(got, '█') {
		t.Errorf("0%% bar should have no full cells: %q", got)
	}
	if got := progressBar(100, cells); strings.ContainsRune(got, '░') {
		t.Errorf("100%% bar should have no empty cells: %q", got)
	}
}

// TestFrameLinesRollup verifies the height-aware layout: every protocol gets its
// own line when the block fits, the overflow collapses into one "+N more"
// aggregate when it doesn't, the block never exceeds the viewport, and the
// roll-up line stays the same display width as a normal bar (so it can't wrap and
// corrupt the in-place redraw).
func TestFrameLinesRollup(t *testing.T) {
	names := []string{"OBFT-0", "OBFT-300", "OBFT-500", "OBFT-700", "2abOBFT-0", "2abOBFT-300"}
	totals := make(map[string]int64, len(names))
	for _, n := range names {
		totals[n] = 100
	}
	p := NewProgressTracker(names, totals)
	for _, n := range names {
		p.Add(n, 50) // 50% each
	}

	// Roomy terminal (and the unknown-height path, rows<=0): one line per
	// protocol plus the header. cols=80 is wide enough that width never forces a
	// roll-up, isolating the height behavior.
	for _, rows := range []int{100, 0, -1} {
		if got := p.frameLines(80, rows); len(got) != len(names)+1 {
			t.Errorf("rows=%d: want %d lines (header + %d bars), got %d: %q",
				rows, len(names)+1, len(names), len(got), got)
		}
	}

	// Short terminal: rows=5 → budget rows-2=3 bar lines → 2 individual + 1
	// roll-up, plus the header = 4 lines. The block must stay within rows-1 so
	// the cursor-up redraw never reaches into scrollback.
	got := p.frameLines(80, 5)
	if len(got) != 4 {
		t.Fatalf("rows=5: want 4 lines (header + 2 bars + roll-up), got %d: %q", len(got), got)
	}
	if len(got) > 5-1 {
		t.Errorf("rows=5: block height %d exceeds rows-1 (would stack on redraw)", len(got))
	}
	last := got[len(got)-1]
	if !strings.Contains(last, "+4 more") {
		t.Errorf("rows=5: last line should roll up the 4 hidden protocols, got %q", last)
	}
	// The roll-up's bar is the aggregate of the rolled-up protocols (all 50%).
	if !strings.Contains(last, "50.0%") {
		t.Errorf("rows=5: roll-up should aggregate to 50.0%%, got %q", last)
	}
	// Same display width as a normal bar line, or it would wrap.
	if normalW, rollupW := utf8.RuneCountInString(got[1]), utf8.RuneCountInString(last); normalW != rollupW {
		t.Errorf("rows=5: roll-up width %d != bar width %d (would wrap and corrupt redraw)", rollupW, normalW)
	}

	// Tiny terminal with no room for even a single bar: header only.
	if got := p.frameLines(80, 2); len(got) != 1 {
		t.Errorf("rows=2: want header-only (1 line), got %d: %q", len(got), got)
	}

	// Short protocol names: the "+N more" label is wider than any name, so the
	// name column must widen to it (maxNameWidth folds the label in) — otherwise
	// the "%-*s" name field overflows and the roll-up line ends up wider than the
	// bars and wraps. rows=3 → budget 1 → all roll up into a single line.
	const shortCols = 60
	short := NewProgressTracker(
		[]string{"a", "b", "c", "d"},
		map[string]int64{"a": 10, "b": 10, "c": 10, "d": 10},
	)
	if w := short.maxNameWidth(); w != len("+4 more") {
		t.Errorf("short: name column should widen to the roll-up label (%d), got %d", len("+4 more"), w)
	}
	got = short.frameLines(shortCols, 3)
	if len(got) != 2 {
		t.Fatalf("short/rows=3: want 2 lines (header + roll-up), got %d: %q", len(got), got)
	}
	if !strings.Contains(got[1], "+4 more") {
		t.Errorf("short/rows=3: want all 4 rolled up, got %q", got[1])
	}
	// With every protocol rolled up there's no normal bar to compare against, so
	// verify the "+4 more" name fit within the widened name column (no overflow):
	// the line is exactly indent + name column + per-line chrome + bar cells.
	shortBarCells := barCellsForCols(shortCols, short.maxNameWidth())
	wantW := blockIndent + short.maxNameWidth() + perLineFixed + shortBarCells
	if got1W := utf8.RuneCountInString(got[1]); got1W != wantW {
		t.Errorf("short/rows=3: roll-up width %d != expected line width %d (name field overflowed → would wrap)", got1W, wantW)
	}
}

// TestFrameLinesWidth verifies the width-aware layout across a sweep of terminal
// widths: no emitted line may reach the viewport's right edge (a line that does
// wraps, which corrupts the in-place redraw), and per-protocol bars appear
// exactly when the width can hold them — otherwise the block degrades to the
// header alone.
func TestFrameLinesWidth(t *testing.T) {
	names := []string{"OBFT-700", "2abOBFT-700", "QBFT-SSV"}
	totals := make(map[string]int64, len(names))
	for _, n := range names {
		totals[n] = 100
	}
	p := NewProgressTracker(names, totals)
	for _, n := range names {
		p.Add(n, 30)
	}
	nameW := p.maxNameWidth() // 11 ("2abOBFT-700")

	// minLineWidth is the boundary: at/above it bars fit, below it they don't.
	minLineWidth := blockIndent + nameW + perLineFixed + minBarCells + blockRightPad
	for _, cols := range []int{10, 20, minLineWidth - 1, minLineWidth, minLineWidth + 1, 50, 80, 200} {
		got := p.frameLines(cols, 100)
		// No line may reach the right edge.
		for i, ln := range got {
			if w := utf8.RuneCountInString(ln); w > cols-blockRightPad {
				t.Errorf("cols=%d: line %d width %d exceeds cols-blockRightPad %d (would wrap): %q",
					cols, i, w, cols-blockRightPad, ln)
			}
		}
		// Bars appear iff the width can hold them.
		if fitsBars(cols, nameW) {
			if len(got) != len(names)+1 {
				t.Errorf("cols=%d fits bars: want %d lines, got %d: %q", cols, len(names)+1, len(got), got)
			}
		} else if len(got) != 1 {
			t.Errorf("cols=%d too narrow: want header-only (1 line), got %d: %q", cols, len(got), got)
		}
	}

	// Unknown width (cols<=0, e.g. output isn't a real terminal): fall back to the
	// fixed bar size with no truncation — one line per protocol.
	if got := p.frameLines(0, 100); len(got) != len(names)+1 {
		t.Errorf("unknown width: want %d lines, got %d: %q", len(names)+1, len(got), got)
	}
}

// TestEmitMultiBar verifies the terminal renderer's frame structure without a
// real TTY: the first frame prints a header plus one line per protocol with no
// cursor movement, per-protocol percentages are independent, and subsequent
// frames move the cursor back up over the whole block before redrawing.
func TestEmitMultiBar(t *testing.T) {
	p := NewProgressTracker(
		[]string{"OBFT-700", "QBFT-SSV"},
		map[string]int64{"OBFT-700": 1000, "QBFT-SSV": 1000},
	)
	p.Add("OBFT-700", 250)
	p.Add("QBFT-SSV", 1000)

	var buf strings.Builder
	p.emit(&buf, true)
	first := buf.String()
	if strings.Contains(first, "\033[") {
		t.Errorf("first frame must not move the cursor: %q", first)
	}
	lines := strings.Split(first, "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines (header + 2 bars), got %d: %q", len(lines), lines)
	}
	if !strings.Contains(lines[0], "of 2,000 sims") {
		t.Errorf("header should show the grand total: %q", lines[0])
	}
	// The header is narrower than the protocol lines, so it must be centered
	// (indented) over the block.
	if !strings.HasPrefix(lines[0], " ") {
		t.Errorf("header should be centered/indented over the block: %q", lines[0])
	}
	if !strings.Contains(lines[1], "OBFT-700") || !strings.Contains(lines[1], "25.0%") {
		t.Errorf("OBFT-700 bar wrong: %q", lines[1])
	}
	if !strings.Contains(lines[2], "QBFT-SSV") || !strings.Contains(lines[2], "100.0%") {
		t.Errorf("QBFT-SSV bar wrong: %q", lines[2])
	}

	buf.Reset()
	p.emit(&buf, true)
	// Second frame moves up over the two lines above the last, then clears.
	if second := buf.String(); !strings.Contains(second, "\033[2A\r\033[J") {
		t.Errorf("subsequent frame should redraw the block in place: %q", second)
	}
}

// TestRedrawRewindsByPreviousHeight is the regression test for the "stacking"
// bug: a terminal resize changes the block's line count between frames (the
// height clamp shows fewer bars on a shorter viewport), and the in-place redraw
// must rewind by the PREVIOUS frame's height, not the new one's. Rewinding by the
// new (smaller) count erases only the bottom of the old block and leaves its top
// on screen as a stale copy. redraw is exercised directly so the frame heights
// can be varied without a real TTY.
func TestRedrawRewindsByPreviousHeight(t *testing.T) {
	var p ProgressTracker
	var buf strings.Builder

	// First frame (5 lines): nothing drawn yet, so no cursor movement.
	p.redraw(&buf, []string{"a", "b", "c", "d", "e"})
	if strings.Contains(buf.String(), "\033[") {
		t.Errorf("first frame must not move the cursor: %q", buf.String())
	}

	// Shrink to 2 lines: rewind must be over the previous 5-line frame (up 4),
	// not the current 2-line one (which would be up 1 and leave 3 stale lines).
	buf.Reset()
	p.redraw(&buf, []string{"x", "y"})
	if !strings.HasPrefix(buf.String(), "\033[4A\r\033[J") {
		t.Errorf("rewind should be up 4 (previous frame 5 lines - 1), got %q", buf.String())
	}

	// Grow back to 3 lines: rewind over the previous 2-line frame (up 1).
	buf.Reset()
	p.redraw(&buf, []string{"p", "q", "r"})
	if !strings.HasPrefix(buf.String(), "\033[1A\r\033[J") {
		t.Errorf("rewind should be up 1 (previous frame 2 lines - 1), got %q", buf.String())
	}

	// A 1-line previous frame: the next rewind must NOT emit a cursor-up, since
	// "\033[0A" moves up one line (param 0 defaults to 1) and would clobber the
	// line above the block — just carriage-return and erase.
	buf.Reset()
	p.redraw(&buf, []string{"solo"}) // prevLines becomes 1
	buf.Reset()
	p.redraw(&buf, []string{"a", "b"})
	if strings.Contains(buf.String(), "\033[0A") {
		t.Errorf("after a 1-line frame, redraw must not emit a cursor-up: %q", buf.String())
	}
	if !strings.HasPrefix(buf.String(), "\r\033[J") {
		t.Errorf("after a 1-line frame, redraw should rewind with just CR+erase: %q", buf.String())
	}
}

// TestLog_ClearsAndRedraws is the core Log coordination test: while the
// renderer is active and a block is on screen, Log must erase the block,
// write the log line as scroll history, and re-emit the block beneath it,
// all as a single coordinated sequence on the output writer.
func TestLog_ClearsAndRedraws(t *testing.T) {
	p := NewProgressTracker(
		[]string{"OBFT-700", "QBFT-SSV"},
		map[string]int64{"OBFT-700": 1000, "QBFT-SSV": 1000},
	)
	p.Add("OBFT-700", 250)
	p.Add("QBFT-SSV", 1000)

	var buf strings.Builder
	p.w = &buf
	p.tty = true
	p.active = true

	// Establish prevLines via an initial emit — frame is 3 lines (header + 2 bars).
	p.emit(&buf, true)
	if p.prevLines != 3 {
		t.Fatalf("after initial emit, prevLines=%d, want 3", p.prevLines)
	}
	buf.Reset()

	p.Log("a log line")
	got := buf.String()

	// The clear must come first: cursor-up by prevLines-1 then CR+erase.
	if !strings.HasPrefix(got, "\033[2A\r\033[J") {
		t.Errorf("Log should emit cursor-up + erase before writing, got %q", got)
	}
	// The line itself must be in there, followed by a newline so it scrolls.
	if !strings.Contains(got, "a log line\n") {
		t.Errorf("Log should write the message + newline into scroll history, got %q", got)
	}
	// And a fresh frame must follow (the redraw beneath). Easiest signal: the
	// progress percentages of the two bars (independent: 25% and 100%).
	if !strings.Contains(got, "25.0%") || !strings.Contains(got, "100.0%") {
		t.Errorf("Log should redraw the block beneath the log line, got %q", got)
	}
	// prevLines should be reset to the new frame's height (3 again).
	if p.prevLines != 3 {
		t.Errorf("after Log, prevLines=%d, want 3 (redraw should re-establish it)", p.prevLines)
	}
}

// TestLog_NoRedrawPostStop verifies that after stop has cleared the tracker's
// active state (and parked the cursor below the final block), Log writes the
// message plainly — no cursor escapes, no redraw.
func TestLog_NoRedrawPostStop(t *testing.T) {
	p := NewProgressTracker(
		[]string{"OBFT-700"},
		map[string]int64{"OBFT-700": 100},
	)
	var buf strings.Builder
	p.w = &buf
	p.tty = true
	p.active = false // post-stop state
	p.prevLines = 0  // stop clears prevLines too

	p.Log("post-stop line")
	got := buf.String()
	if got != "post-stop line\n" {
		t.Errorf("post-stop Log should write only the message + newline, got %q", got)
	}
	if strings.Contains(got, "\033[") {
		t.Errorf("post-stop Log must not emit cursor escapes, got %q", got)
	}
}

// TestLog_BeforeStart verifies that Log called before StartRenderer (w == nil)
// falls back to the package-level stderr writer — so callers don't have to
// gate their Log calls on the renderer lifecycle. Tracker state stays
// untouched.
func TestLog_BeforeStart(t *testing.T) {
	p := NewProgressTracker(
		[]string{"OBFT-700"},
		map[string]int64{"OBFT-700": 100},
	)

	var captured strings.Builder
	origStderr := stderr
	stderr = &captured
	defer func() { stderr = origStderr }()

	p.Log("before start")

	if got := captured.String(); got != "before start\n" {
		t.Errorf("pre-start Log should write to stderr, got %q", got)
	}
	if p.w != nil {
		t.Errorf("pre-start Log should not touch p.w, got %v", p.w)
	}
	if p.active {
		t.Errorf("pre-start Log should not flip active, got true")
	}
}

// TestLog_NilReceiver mirrors Add's nil-receiver no-op contract so callers
// can use `var p *ProgressTracker` without branching.
func TestLog_NilReceiver(t *testing.T) {
	var p *ProgressTracker
	p.Log("nothing to log") // must not panic
}

// TestLog_SingleDestination is the regression test for the
// duplicate-output-via-test-runner-relay bug: when the renderer is active, Log
// must write the message exactly once to p.w and nothing else. Mirroring to
// os.Stderr — as a previous implementation attempted, to capture progress
// lines in `... 2>&1 | tee out.log` runs — caused `go test`'s stderr relay to
// re-emit the line back onto the user's terminal at a position not under
// outMu, corrupting the in-place block's cursor math. See Log's doc-comment
// for the resolution rationale.
func TestLog_SingleDestination(t *testing.T) {
	origStderr := stderr
	defer func() { stderr = origStderr }()

	p := NewProgressTracker(
		[]string{"OBFT-700"},
		map[string]int64{"OBFT-700": 100},
	)
	p.tty = false // non-tty path: plain write, no clear/redraw — keeps the
	// assertion focused on the destination set rather than terminal escape
	// sequences.
	p.active = true

	var primary, sink strings.Builder
	stderr = &sink // would catch a mirror if one were emitted
	p.w = &primary
	p.Log("once")

	if !strings.Contains(primary.String(), "once") {
		t.Errorf("primary write to p.w missing: %q", primary.String())
	}
	if got := sink.String(); got != "" {
		t.Errorf("Log must not mirror to stderr while active (would round-trip through go test's stderr relay and stack on the in-place block), got %q", got)
	}
}

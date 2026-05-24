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

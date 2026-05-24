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

func TestProgressBar(t *testing.T) {
	const cells = progressBarCells
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

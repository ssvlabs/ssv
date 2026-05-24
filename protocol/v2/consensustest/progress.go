package consensustest

import (
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"golang.org/x/term"
)

// ProgressTracker reports stress-run progress as one bar per protocol — each
// bar aggregating all of that protocol's simulations across every sweep,
// scenario, and operating point — under an overall header line.
//
// It is OBSERVABILITY ONLY — it never influences sim outcomes, ordering, or the
// report. Workers call Add (concurrency-safe) as each sim finishes; the driver
// runs StartRenderer to print to a writer (the controlling terminal). A nil
// *ProgressTracker is a valid no-op, so RunBatch can be called without one.
type ProgressTracker struct {
	bars  []*protoBar
	index map[string]int // protocol name -> bars index; read-only after New
	start time.Time
	// drawn tracks whether the multi-line block has been printed once, so
	// subsequent renders move the cursor back up over it before redrawing. Only
	// touched by emit, which is called sequentially (the renderer goroutine,
	// then the final emit in stop after wg.Wait).
	drawn bool
}

// protoBar is one protocol's progress: a fixed total and an atomically-updated
// done count.
type protoBar struct {
	name  string
	total int64
	done  int64 // atomic
}

const (
	// fallbackBarCells is each bar's cell count when the terminal width can't
	// be determined (e.g. output isn't a terminal). On a real terminal the bars
	// stretch to fill the line, minus the side padding — see barCellsForCols.
	fallbackBarCells = 30
	// blockIndent / blockRightPad are the left / right padding (in columns) kept
	// around the progress block so the bars don't run to the terminal edges.
	blockIndent   = 4
	blockRightPad = 4
	// perLineFixed is a protocol line's width excluding its bar cells, its left
	// indent, and its (variable-width) name: "  " + "[" + "]" + " " + "100.0%".
	perLineFixed = 11
)

// NewProgressTracker builds a tracker with one bar per protocol, in `names`
// order, each sized to totals[name] (a name absent from totals gets total 0).
func NewProgressTracker(names []string, totals map[string]int64) *ProgressTracker {
	t := &ProgressTracker{index: make(map[string]int, len(names)), start: time.Now()}
	for _, n := range names {
		t.index[n] = len(t.bars)
		t.bars = append(t.bars, &protoBar{name: n, total: totals[n]})
	}
	return t
}

// Add records n completed sims for protocol `name`. Safe for concurrent use; a
// nil receiver or an unknown name is a no-op so callers needn't branch.
func (p *ProgressTracker) Add(name string, n int64) {
	if p == nil {
		return
	}
	if i, ok := p.index[name]; ok {
		atomic.AddInt64(&p.bars[i].done, n)
	}
}

// grandTotal is the sum of every protocol's total.
func (p *ProgressTracker) grandTotal() int64 {
	var t int64
	for _, b := range p.bars {
		t += b.total
	}
	return t
}

// StartRenderer launches a goroutine that periodically renders progress to w
// until the returned stop func is called (stop also prints a final frame). On a
// terminal it redraws the whole block (header + one bar per protocol) in place
// every second; otherwise it prints a single overall line every 30s
// (log-friendly). Returns a no-op stop for a nil tracker or an empty run.
func (p *ProgressTracker) StartRenderer(w io.Writer) (stop func()) {
	if p == nil || p.grandTotal() <= 0 {
		return func() {}
	}
	tty := isCharDevice(w)
	interval := 30 * time.Second
	if tty {
		interval = 1 * time.Second
	}
	doneCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-doneCh:
				return
			case <-ticker.C:
				p.emit(w, tty)
			}
		}
	}()
	return func() {
		close(doneCh)
		wg.Wait()
		p.emit(w, tty) // final snapshot (typically 100%)
		if tty {
			fmt.Fprintln(w) // move the cursor below the block
		}
	}
}

func (p *ProgressTracker) emit(w io.Writer, tty bool) {
	if !tty {
		// Log-friendly: one plain overall line per emit, no cursor control.
		fmt.Fprintf(w, "stresstest progress: %s\n", p.overallLine())
		return
	}
	lines := p.frameLines(p.barCells(w))
	if p.drawn {
		// Move up over the previously-printed block (every line but the last),
		// return to column 0, and erase to end of screen before redrawing.
		fmt.Fprintf(w, "\033[%dA\r\033[J", len(lines)-1)
	}
	fmt.Fprint(w, strings.Join(lines, "\n"))
	p.drawn = true
}

// overallLine summarizes total progress + elapsed; used as the header and as
// the non-terminal log line.
func (p *ProgressTracker) overallLine() string {
	var done int64
	for _, b := range p.bars {
		done += atomic.LoadInt64(&b.done)
	}
	total := p.grandTotal()
	return fmt.Sprintf("%.1f%%  of %s sims (elapsed: %s)",
		percentOf(done, total), CommaInt(total), shortDur(time.Since(p.start)))
}

// frameLines is the terminal block: one line per protocol (name left-aligned to
// a common width, then a `barCells`-wide bar and percent), with the overall
// header centered above them across the protocol lines' width.
func (p *ProgressTracker) frameLines(barCells int) []string {
	nameW := p.maxNameWidth()
	indent := strings.Repeat(" ", blockIndent)
	protoLines := make([]string, len(p.bars))
	contentW := 0 // widest protocol line excluding the left indent
	for i, b := range p.bars {
		pct := percentOf(atomic.LoadInt64(&b.done), b.total)
		content := fmt.Sprintf("%-*s  %s %5.1f%%", nameW, b.name, progressBar(pct, barCells), pct)
		protoLines[i] = indent + content
		// Display width: the bar's block runes are multi-byte but one column
		// each, so count runes rather than bytes.
		if w := utf8.RuneCountInString(content); w > contentW {
			contentW = w
		}
	}
	header := p.overallLine() // ASCII, so byte length == display width
	lead := blockIndent
	if contentW > len(header) {
		lead += (contentW - len(header)) / 2
	}
	lines := make([]string, 0, len(p.bars)+1)
	lines = append(lines, strings.Repeat(" ", lead)+header)
	lines = append(lines, protoLines...)
	return lines
}

// maxNameWidth is the widest protocol name, for column-aligning the labels.
func (p *ProgressTracker) maxNameWidth() int {
	w := 0
	for _, b := range p.bars {
		if len(b.name) > w {
			w = len(b.name)
		}
	}
	return w
}

// barCells sizes each protocol's bar to fill w's line: terminal columns minus
// the fixed per-line chrome (indent, name, separators, brackets, percent) and a
// one-column margin so the line can't wrap (which would corrupt the in-place
// redraw). Falls back to fallbackBarCells when the width is unknown.
func (p *ProgressTracker) barCells(w io.Writer) int {
	cols := terminalWidth(w)
	if cols <= 0 {
		return fallbackBarCells
	}
	return barCellsForCols(cols, p.maxNameWidth())
}

// barCellsForCols is the bar width for a `cols`-wide terminal and `nameW`-wide
// labels: total columns minus the left/right padding, the name, and the fixed
// per-line chrome. The result keeps blockIndent/blockRightPad columns clear on
// each side (which also prevents wrapping, since wrapping corrupts the in-place
// redraw). Never below 8.
func barCellsForCols(cols, nameW int) int {
	cells := cols - blockIndent - blockRightPad - nameW - perLineFixed
	if cells < 8 {
		cells = 8
	}
	return cells
}

// terminalWidth returns w's column count, or 0 when w isn't a terminal or the
// size can't be read.
func terminalWidth(w io.Writer) int {
	f, ok := w.(*os.File)
	if !ok {
		return 0
	}
	cols, _, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0
	}
	return cols
}

// percentOf is 100*done/total clamped to [0, 100]; 0 when total is non-positive.
func percentOf(done, total int64) float64 {
	if total <= 0 {
		return 0
	}
	return 100 * float64(done) / float64(total)
}

// barPartials are the 1/8..7/8 left-fill block runes, used for the bar's
// leading edge so progress advances smoothly within a cell rather than
// jumping a whole cell at a time.
var barPartials = []rune("▏▎▍▌▋▊▉")

// progressBar renders a `width`-cell bar in brackets with a smooth fractional
// leading edge: full cells are █, the partial cell picks the nearest eighth,
// and the remainder is the ░ track.
func progressBar(pct float64, width int) string {
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	cells := pct / 100 * float64(width)
	full := int(cells)
	var b strings.Builder
	b.Grow(width*3 + 2)
	b.WriteByte('[')
	for i := 0; i < full; i++ {
		b.WriteRune('█')
	}
	rem := width - full
	if rem > 0 {
		if idx := int((cells - float64(full)) * 8); idx > 0 {
			b.WriteRune(barPartials[idx-1])
			rem--
		}
		for i := 0; i < rem; i++ {
			b.WriteRune('░')
		}
	}
	b.WriteByte(']')
	return b.String()
}

// CommaInt formats n as an exact integer with thousands separators
// (e.g. 23587200 -> "23,587,200"), so the progress line shows precise counts
// rather than rounded magnitudes.
func CommaInt(n int64) string {
	s := strconv.FormatInt(n, 10)
	sign := ""
	if s[0] == '-' {
		sign, s = "-", s[1:]
	}
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteByte(s[i])
	}
	return sign + b.String()
}

// shortDur rounds to whole seconds and uses Duration's compact form (e.g.
// "4m12s", "1h3m0s"); sub-second / non-positive durations show "0s".
func shortDur(d time.Duration) string {
	if d < time.Second {
		return "0s"
	}
	return d.Round(time.Second).String()
}

// isCharDevice reports whether w is a terminal-like character device, so the
// renderer can pick in-place redraw vs. log-line mode. Avoids a dependency on
// x/term by checking the file mode directly.
func isCharDevice(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	if err != nil {
		return false
	}
	return st.Mode()&os.ModeCharDevice != 0
}

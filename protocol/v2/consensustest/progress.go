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
	// prevLines is the line count of the block the last emit printed; the next
	// emit rewinds the cursor over exactly that many lines before erasing and
	// redrawing. It must reflect the PREVIOUS frame, not the current one: a
	// terminal resize can change the block's height between frames (the height
	// clamp in frameLines shows fewer bars on a shorter viewport), and rewinding
	// by the current height then misaligns the erase, leaving the old block's top
	// behind as a stale copy ("stacking"). 0 means nothing has been drawn yet.
	//
	// Resize is only one stacking trigger; foreign writes between frames are
	// another (e.g. t.Logf streamed by `go test -v` to the same terminal, which
	// pushes the cursor down without the tracker noticing). The PREVIOUS-frame
	// rewind alone doesn't help there. The structural fix is to route every
	// in-renderer-lifetime write through Log, which acquires outMu and performs
	// a clear + write + redraw triple — so the cursor stays where prevLines
	// says it is. Writes that bypass Log (anything else on the same FD), or
	// writes the test runner relays back to the terminal from a captured FD
	// (which is why Log is single-destination — see Log's doc-comment), can
	// still corrupt the display; the load-bearing invariant is "every write to
	// p.w goes through outMu, and outMu callers leave prevLines accurate".
	//
	// Touched only under outMu — the renderer goroutine, stop's final emit,
	// and Log all acquire outMu before reading or writing it.
	prevLines int

	// outMu serialises every write to w and protects prevLines. The renderer
	// goroutine acquires it on each tick (via emit); stop acquires it around
	// the final emit + state reset; Log acquires it around its clear + write +
	// redraw triple. Without this, foreign writes to w from anywhere outside
	// the tracker can interleave with the cursor-relative redraw and leave the
	// in-place block stacked above the new frame.
	outMu sync.Mutex
	// w / tty record StartRenderer's output destination so Log knows where to
	// write without re-discovering it on every call. nil w means StartRenderer
	// hasn't been called (or the tracker is empty / nil and never will).
	w   io.Writer
	tty bool
	// active is true between StartRenderer's entry and stop's completion. Log
	// uses it to decide whether to do the clear + redraw dance (active) or
	// just plain-write to w (post-stop).
	active bool
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
	// minBarCells is the narrowest a bar may render. Below this the terminal is
	// too narrow to show bars without the line overrunning the width and wrapping
	// (which corrupts the in-place redraw), so the renderer drops to a header-only
	// block — see frameLines' tooNarrow path.
	minBarCells = 8
)

// stderr is Log's fallback destination before StartRenderer has set p.w.
// Held in a package var (not a hard-coded os.Stderr reference) so
// TestLog_BeforeStart can swap in a buffer; production leaves it at os.Stderr.
var stderr io.Writer = os.Stderr

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

// Log writes a line through the tracker so it interleaves cleanly with the
// live progress block: while the renderer is active, it clears the in-place
// block, writes the line as normal scroll history, then re-emits the block
// beneath it. Outside the renderer's active lifetime (before StartRenderer,
// after stop) it falls back to a plain write to the tracker's writer, or to
// the package-level stderr when StartRenderer was never called.
//
// This is the coordination point that prevents the foreign-write desync that
// the in-place cursor math is vulnerable to: anything routed through Log is
// serialised with the renderer's redraw cycle under outMu, and prevLines
// stays accurate. Anything that writes to the same FD without going through
// Log can still corrupt the display.
//
// Single-destination by design: Log writes only to the tracker's writer
// (typically /dev/tty in interactive runs, os.Stderr in CI). It does NOT
// mirror to os.Stderr. Under `go test -v` the test binary's os.Stderr is
// always a pipe to the test runner — regardless of whether the user is in
// an interactive session or piping to tee — so a mirror would cause the test
// runner to re-emit the same line back onto the terminal, after the /dev/tty
// write that already showed it. The relayed write also lands at an
// unpredictable position (the runner buffers and bulk-flushes), bypassing
// outMu and corrupting prevLines on the next redraw. Consequence: in
// `make stresstest 2>&1 | tee out.log` runs the tee'd file does not
// capture mid-run lifecycle lines (setup + final summary still flow through
// t.Logf, which the test runner captures normally); the live terminal does.
//
// Safe for concurrent use; nil receiver is a no-op (matches Add). A bad
// format string panics in fmt.Sprintf — caller error; the deferred Unlock
// releases outMu either way.
func (p *ProgressTracker) Log(format string, args ...any) {
	if p == nil {
		return
	}
	msg := fmt.Sprintf(format, args...)
	p.outMu.Lock()
	defer p.outMu.Unlock()
	if p.w == nil {
		// Renderer never started — fall back to stderr so callers needn't gate
		// on lifecycle. Matches t.Logf's destination in the same situation.
		fmt.Fprintln(stderr, msg)
		return
	}
	if p.active && p.tty && p.prevLines > 0 {
		// Erase the live block; cursor lands at its top, ready to scroll the
		// log line into history below the cleared region.
		if p.prevLines > 1 {
			fmt.Fprintf(p.w, "\033[%dA", p.prevLines-1)
		}
		fmt.Fprint(p.w, "\r\033[J")
		p.prevLines = 0
	}
	fmt.Fprintln(p.w, msg)
	if p.active {
		p.emitLocked(p.w, p.tty) // redraw the block beneath the new log line
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
// every second, collapsing any protocols that don't fit the terminal height into
// a single roll-up line so the block never overflows the viewport; otherwise it
// prints a single overall line every 30s (log-friendly). Returns a no-op stop
// for a nil tracker or an empty run.
//
// While the renderer is active, callers that need to interject text (lifecycle
// log lines from the test driver) MUST go through Log — it serialises with the
// renderer's redraw cycle so the in-place block doesn't desync. Anything that
// writes to w directly bypasses that coordination and risks the stacking bug.
func (p *ProgressTracker) StartRenderer(w io.Writer) (stop func()) {
	if p == nil || p.grandTotal() <= 0 {
		return func() {}
	}
	tty := isCharDevice(w)
	interval := 30 * time.Second
	if tty {
		interval = 1 * time.Second
	}

	// Record the destination + active state before launching the goroutine so
	// Log (which reads p.w / p.tty / p.active under outMu) sees a consistent
	// snapshot. The `go` statement provides a happens-before edge for the
	// goroutine; Log acquires outMu, so external callers are covered too.
	p.outMu.Lock()
	p.w = w
	p.tty = tty
	p.active = true
	p.outMu.Unlock()

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
				p.emit(w, tty) // emit acquires outMu internally
			}
		}
	}()
	var once sync.Once
	return func() {
		// Idempotent: the driver both defers stop and may call it from its
		// signal handler (to clear the live block before printing an interrupt
		// notice). Without the guard the second call would close a closed
		// channel and emit a duplicate final frame.
		once.Do(func() {
			close(doneCh)
			wg.Wait()
			p.outMu.Lock()
			defer p.outMu.Unlock()
			p.emitLocked(w, tty) // final snapshot (typically 100%)
			if tty {
				fmt.Fprintln(w) // move the cursor below the block
			}
			// Clear active + prevLines so any post-stop Log call writes plainly
			// without trying to rewind into the now-parked cursor position.
			p.active = false
			p.prevLines = 0
		})
	}
}

// emit acquires outMu and emits one frame. Used by callers that don't already
// hold the mutex — the renderer goroutine and tests. Internal callers that
// hold outMu (stop's final emit, Log's redraw-after-clear) use emitLocked.
func (p *ProgressTracker) emit(w io.Writer, tty bool) {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	p.emitLocked(w, tty)
}

// emitLocked emits one frame; caller must hold outMu.
func (p *ProgressTracker) emitLocked(w io.Writer, tty bool) {
	if !tty {
		// Log-friendly: one plain overall line per emit, no cursor control.
		fmt.Fprintf(w, "stresstest progress: %s\n", p.overallLine())
		return
	}
	cols, rows := terminalSize(w)
	p.redrawLocked(w, p.frameLines(cols, rows))
}

// redraw acquires outMu and writes the new frame in place. Tests drive the
// redraw directly through this entry point; production callers that hold the
// mutex (emitLocked, Log) call redrawLocked.
func (p *ProgressTracker) redraw(w io.Writer, lines []string) {
	p.outMu.Lock()
	defer p.outMu.Unlock()
	p.redrawLocked(w, lines)
}

// redrawLocked writes one terminal frame in place: it rewinds the cursor over
// the previously drawn block (tracked in prevLines), erases to end of screen,
// prints `lines`, then records the new height for the next call. Rewinding by
// the PREVIOUS frame's height — not this one's — keeps the erase aligned when
// a resize changes the block's line count between frames; rewinding by the
// current height instead leaves the old block's top behind as a stale copy
// ("stacking"). The block is sized to fit the viewport (see frameLines), so on
// a stable size the cursor-up never has to reach past the top of the screen
// into scrollback (which it can't). A 1-line previous frame needs no vertical
// move, and "\033[0A" would wrongly move up one line (param 0 defaults to 1),
// so the cursor-up is guarded.
//
// Caller must hold outMu — every write to p.w runs under that lock so foreign
// writes from elsewhere in the test driver can't interleave with the cursor
// math. Callers that want to interject text scroll a line into history via Log,
// which clears the block, writes, and then re-emits beneath the new line.
func (p *ProgressTracker) redrawLocked(w io.Writer, lines []string) {
	if p.prevLines > 0 {
		if p.prevLines > 1 {
			fmt.Fprintf(w, "\033[%dA", p.prevLines-1)
		}
		fmt.Fprint(w, "\r\033[J")
	}
	fmt.Fprint(w, strings.Join(lines, "\n"))
	p.prevLines = len(lines)
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

// frameLines renders the terminal block for a `cols`×`rows` viewport: a centered
// overall header above one line per protocol (name left-aligned to a common
// width, then a bar and percent). It guarantees the block fits the viewport so
// the in-place cursor-up redraw is always exact — otherwise the redraw can't
// reach lines that scrolled out (vertically) or wrapped (horizontally) and the
// block "stacks" a fresh copy each tick. Two clamps enforce the fit:
//
//   - Height: if the set is taller than rows, the protocols past the budget
//     collapse into a single "+N more" aggregate line. Too short for even one
//     bar → header only.
//   - Width: bars are sized to the columns; if the terminal is too narrow to
//     hold even a minimum bar, the bars are dropped (header only), and every
//     emitted line is finally trimmed to the viewport width so nothing wraps.
//
// cols<=0 / rows<=0 mean that dimension is unknown (output isn't a real
// terminal, e.g. in tests): width falls back to fallbackBarCells and height is
// treated as unlimited (one line per protocol).
func (p *ProgressTracker) frameLines(cols, rows int) []string {
	nameW := p.maxNameWidth()
	indent := strings.Repeat(" ", blockIndent)

	barCells := fallbackBarCells
	if cols > 0 {
		barCells = barCellsForCols(cols, nameW)
	}
	// tooNarrow: the terminal can't hold even a minimum bar without wrapping, so
	// drop the bars and show the header alone. (Unknown width, cols<=0, is never
	// too narrow — it takes the fallback bar size.)
	tooNarrow := cols > 0 && !fitsBars(cols, nameW)

	var protoLines []string
	contentW := 0 // widest protocol line excluding the left indent
	addBar := func(name string, done, total int64) {
		pct := percentOf(done, total)
		content := fmt.Sprintf("%-*s  %s %5.1f%%", nameW, name, progressBar(pct, barCells), pct)
		protoLines = append(protoLines, indent+content)
		// Display width: the bar's block runes are multi-byte but one column
		// each, so count runes rather than bytes.
		if w := utf8.RuneCountInString(content); w > contentW {
			contentW = w
		}
	}

	// budget is how many protocol/roll-up lines fit under the header while keeping
	// the block within the viewport: rows minus the header line and one reserved
	// row (so the trailing newline parked below the block can't scroll it off the
	// top). Only meaningful when rows>0; the rows<=0 case handles unknown height.
	budget := rows - 2
	switch {
	case tooNarrow:
		// Header only; bars would wrap (handled below by emitting just the header).
	case rows <= 0 || budget >= len(p.bars):
		// Unknown height, or everything fits: one line per protocol.
		for _, b := range p.bars {
			addBar(b.name, atomic.LoadInt64(&b.done), b.total)
		}
	case budget <= 0:
		// Terminal too short for even a single bar: header only.
	default:
		// Show the first budget-1 protocols individually and roll the rest into a
		// single aggregate line, so the block is exactly budget lines tall. The
		// roll-up reuses the normal bar format (same width → it can't wrap), with
		// its name carrying the rolled-up count.
		shown := budget - 1
		for _, b := range p.bars[:shown] {
			addBar(b.name, atomic.LoadInt64(&b.done), b.total)
		}
		var done, total int64
		for _, b := range p.bars[shown:] {
			done += atomic.LoadInt64(&b.done)
			total += b.total
		}
		addBar(fmt.Sprintf("+%d more", len(p.bars)-shown), done, total)
	}

	header := p.overallLine() // ASCII, so byte length == display width
	lead := blockIndent
	if contentW > len(header) {
		lead += (contentW - len(header)) / 2
	}
	lines := make([]string, 0, len(protoLines)+1)
	lines = append(lines, strings.Repeat(" ", lead)+header)
	lines = append(lines, protoLines...)

	// Final guard: no line may reach the viewport's right edge, or it wraps and
	// corrupts the cursor-up redraw. Bars are already sized to leave blockRightPad
	// clear; this only ever trims the header (which can be wider than the bars on
	// a mid-narrow terminal), and is a hard backstop for everything else.
	if cols > 0 {
		for i, ln := range lines {
			lines[i] = truncateToWidth(ln, cols-blockRightPad)
		}
	}
	return lines
}

// maxNameWidth is the widest label the name column must hold: the widest
// protocol name, and — when the set can spill into a roll-up line (see
// frameLines) — the widest possible "+N more" label. Folding the roll-up label
// in here keeps emit's bar sizing and frameLines' rendering on the same nameW,
// so the roll-up line is never wider than the others (which would wrap and
// corrupt the in-place redraw).
func (p *ProgressTracker) maxNameWidth() int {
	w := 0
	for _, b := range p.bars {
		if len(b.name) > w {
			w = len(b.name)
		}
	}
	// A roll-up can cover up to every protocol (budget==1 → "+len more").
	if len(p.bars) > 1 {
		if rl := len("+" + strconv.Itoa(len(p.bars)) + " more"); rl > w {
			w = rl
		}
	}
	return w
}

// barCellsForCols is the bar width for a `cols`-wide terminal and `nameW`-wide
// labels: total columns minus the left/right padding, the name, and the fixed
// per-line chrome. The result keeps blockIndent/blockRightPad columns clear on
// each side (which also prevents wrapping, since wrapping corrupts the in-place
// redraw). Never below minBarCells — when even that doesn't fit, the caller
// drops to a header-only block rather than rendering wrapping bars.
func barCellsForCols(cols, nameW int) int {
	cells := cols - blockIndent - blockRightPad - nameW - perLineFixed
	if cells < minBarCells {
		cells = minBarCells
	}
	return cells
}

// fitsBars reports whether a `cols`-wide terminal can hold a `nameW`-labelled
// protocol line at the minimum bar width without wrapping. When it can't, the
// renderer shows the header alone (see frameLines).
func fitsBars(cols, nameW int) bool {
	return cols >= blockIndent+nameW+perLineFixed+minBarCells+blockRightPad
}

// terminalSize returns w's (cols, rows), or (0, 0) when w isn't a terminal or
// the size can't be read. The renderer uses cols to size the bars and rows to
// keep the block within the viewport (rolling overflow protocols into one line).
func terminalSize(w io.Writer) (cols, rows int) {
	f, ok := w.(*os.File)
	if !ok {
		return 0, 0
	}
	c, r, err := term.GetSize(int(f.Fd()))
	if err != nil {
		return 0, 0
	}
	return c, r
}

// truncateToWidth limits s to limit display columns. Every rune the progress
// block emits — block-drawing bar cells, ASCII chrome, spaces — is exactly one
// column, so a rune count is the display width; the cut lands on a rune boundary.
// limit<=0 yields "".
func truncateToWidth(s string, limit int) string {
	if limit <= 0 {
		return ""
	}
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	n := 0
	for i := range s { // ranges by rune; i is the byte offset of each rune
		if n == limit {
			return s[:i]
		}
		n++
	}
	return s
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

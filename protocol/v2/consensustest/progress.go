package consensustest

import (
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// ProgressTracker reports stress-run progress: it counts completed simulations
// against a total known up front and renders a single-line bar (percent, sims
// done/total, elapsed).
//
// It is OBSERVABILITY ONLY — it never influences sim outcomes, ordering, or the
// report. Workers call Add (concurrency-safe) as each sim finishes; the driver
// runs StartRenderer to print to a writer (stderr). A nil *ProgressTracker is a
// valid no-op, so RunBatch can be called without one (unit tests, etc.).
type ProgressTracker struct {
	total int64
	done  int64 // atomic
	start time.Time
}

// NewProgressTracker returns a tracker for `total` simulations, started now.
func NewProgressTracker(total int64) *ProgressTracker {
	return &ProgressTracker{total: total, start: time.Now()}
}

// Add records n completed simulations. Safe for concurrent use; a nil receiver
// is a no-op so callers needn't branch.
func (p *ProgressTracker) Add(n int64) {
	if p == nil {
		return
	}
	atomic.AddInt64(&p.done, n)
}

// StartRenderer launches a goroutine that periodically renders progress to w
// until the returned stop func is called (stop also prints a final line). On a
// terminal it redraws one line in place (\r) every second; otherwise it prints
// a newline-terminated line every 30s (log-friendly). Returns a no-op stop for
// a nil tracker or an empty run.
func (p *ProgressTracker) StartRenderer(w io.Writer) (stop func()) {
	if p == nil || p.total <= 0 {
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
			fmt.Fprintln(w) // terminate the in-place line
		}
	}
}

func (p *ProgressTracker) emit(w io.Writer, tty bool) {
	if tty {
		// \r returns to column 0; \033[K clears to end of line so a shorter
		// line doesn't leave stale characters behind.
		fmt.Fprintf(w, "\r\033[K%s", p.line())
	} else {
		fmt.Fprintf(w, "stresstest progress: %s\n", p.line())
	}
}

// line formats the current progress: bar, percent, done/total, and elapsed.
func (p *ProgressTracker) line() string {
	done := atomic.LoadInt64(&p.done)
	pct := 0.0
	if p.total > 0 {
		pct = 100 * float64(done) / float64(p.total)
	}
	return fmt.Sprintf("%s %5.1f%%  %s/%s sims  (elapsed %s)",
		progressBar(pct, 24), pct,
		HumanCount(done), HumanCount(p.total),
		shortDur(time.Since(p.start)))
}

func progressBar(pct float64, width int) string {
	switch {
	case pct < 0:
		pct = 0
	case pct > 100:
		pct = 100
	}
	filled := int(pct / 100 * float64(width))
	return "[" + strings.Repeat("█", filled) + strings.Repeat("░", width-filled) + "]"
}

// HumanCount renders a count compactly (e.g. 1.5M, 990.0k, 42).
func HumanCount(n int64) string {
	switch {
	case n >= 1_000_000_000:
		return fmt.Sprintf("%.1fB", float64(n)/1e9)
	case n >= 1_000_000:
		return fmt.Sprintf("%.1fM", float64(n)/1e6)
	case n >= 1_000:
		return fmt.Sprintf("%.1fk", float64(n)/1e3)
	default:
		return fmt.Sprintf("%d", n)
	}
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

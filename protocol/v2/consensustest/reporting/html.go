package reporting

import (
	"fmt"
	"html"
	"os"
	"strings"
)

// RenderHTML writes the run as a single self-contained HTML file with an
// outcome matrix table and per-protocol bandwidth + decision-time bar
// charts. Chart.js loads via CDN — works in any modern browser when online;
// the table renders fine offline.
func RenderHTML(r *Run, path string) error {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	sb.WriteString("<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&sb, "<title>%s</title>\n", html.EscapeString(r.Title))
	sb.WriteString(htmlStyles())
	sb.WriteString("<script src=\"https://cdn.jsdelivr.net/npm/chart.js\"></script>\n")
	sb.WriteString("</head>\n<body>\n")

	fmt.Fprintf(&sb, "<h1>%s</h1>\n", html.EscapeString(r.Title))
	if r.Description != "" {
		fmt.Fprintf(&sb, "<p class=\"desc\">%s</p>\n", html.EscapeString(r.Description))
	}
	fmt.Fprintf(&sb, "<p class=\"meta\">Started: %s · Duration: %v · Cells: %d (%d × %d)</p>\n",
		html.EscapeString(r.StartedAt.Format("2006-01-02 15:04:05")),
		r.Duration, len(r.Cells), len(r.Scenarios), len(r.ProtocolNames))

	// Outcome matrix
	sb.WriteString("<h2>Outcome matrix</h2>\n<table>\n<thead><tr><th>Scenario</th>")
	for _, p := range r.ProtocolNames {
		fmt.Fprintf(&sb, "<th>%s</th>", html.EscapeString(p))
	}
	sb.WriteString("</tr></thead>\n<tbody>\n")
	for _, scenario := range r.Scenarios {
		fmt.Fprintf(&sb, "<tr><td class=\"scen\">%s</td>", html.EscapeString(scenario))
		for _, protocol := range r.ProtocolNames {
			cell, ok := r.Cells[CellKey{Scenario: scenario, Protocol: protocol}]
			if !ok {
				sb.WriteString("<td>—</td>")
				continue
			}
			summary := cellSummary(cell)
			class := "ok"
			switch summary {
			case "miss":
				class = "miss"
			case "n/a":
				class = "na"
			case "!err":
				class = "err"
			}
			fmt.Fprintf(&sb, "<td class=\"%s\">%s</td>", class, html.EscapeString(summary))
		}
		sb.WriteString("</tr>\n")
	}
	sb.WriteString("</tbody></table>\n")

	// Bandwidth chart data
	sb.WriteString("<h2>Bandwidth per cell (bytes)</h2>\n")
	sb.WriteString("<canvas id=\"bwChart\" width=\"800\" height=\"400\"></canvas>\n")
	sb.WriteString(bandwidthScript(r))

	// Decision time chart data
	sb.WriteString("<h2>Decision time per cell (ms)</h2>\n")
	sb.WriteString("<canvas id=\"dtChart\" width=\"800\" height=\"400\"></canvas>\n")
	sb.WriteString(decisionTimeScript(r))

	sb.WriteString("</body>\n</html>\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("reporting: write HTML %s: %w", path, err)
	}
	return nil
}

func htmlStyles() string {
	return `<style>
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 1100px; margin: 2em auto; padding: 0 1em; color: #222; }
h1, h2 { color: #1a1a1a; }
.desc { color: #555; font-style: italic; }
.meta { color: #888; font-size: 0.9em; }
table { border-collapse: collapse; margin: 1em 0; width: 100%; }
th, td { padding: 0.4em 0.8em; text-align: left; border-bottom: 1px solid #eee; }
th { background: #f7f7f7; font-weight: 600; }
.scen { font-family: monospace; font-size: 0.9em; }
.ok { color: #1a7f37; }
.warn { color: #b07d00; }
.miss { color: #d1242f; }
.na { color: #888; }
.err { color: #cf222e; font-weight: bold; }
canvas { display: block; margin: 1em 0; }
</style>
`
}

func bandwidthScript(r *Run) string {
	var sb strings.Builder
	sb.WriteString("<script>\nnew Chart(document.getElementById('bwChart'), { type: 'bar', data: { labels: [")
	for i, scenario := range r.Scenarios {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "'%s'", jsEscape(scenario))
	}
	sb.WriteString("], datasets: [")
	for i, protocol := range r.ProtocolNames {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "{ label: '%s', data: [", jsEscape(protocol))
		for j, scenario := range r.Scenarios {
			if j > 0 {
				sb.WriteString(",")
			}
			cell, ok := r.Cells[CellKey{Scenario: scenario, Protocol: protocol}]
			if !ok {
				sb.WriteString("0")
			} else {
				fmt.Fprintf(&sb, "%d", cell.Outcome.Bandwidth.TotalBytes)
			}
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: false, plugins: { title: { display: true, text: 'Bytes per scenario' } } } });\n</script>\n")
	return sb.String()
}

func decisionTimeScript(r *Run) string {
	var sb strings.Builder
	sb.WriteString("<script>\nnew Chart(document.getElementById('dtChart'), { type: 'bar', data: { labels: [")
	for i, scenario := range r.Scenarios {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "'%s'", jsEscape(scenario))
	}
	sb.WriteString("], datasets: [")
	for i, protocol := range r.ProtocolNames {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "{ label: '%s', data: [", jsEscape(protocol))
		for j, scenario := range r.Scenarios {
			if j > 0 {
				sb.WriteString(",")
			}
			cell, ok := r.Cells[CellKey{Scenario: scenario, Protocol: protocol}]
			if !ok || !cell.Outcome.Decided {
				sb.WriteString("0")
			} else {
				fmt.Fprintf(&sb, "%d", cell.Outcome.DecisionTime.Milliseconds())
			}
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: false, plugins: { title: { display: true, text: 'ms to decide (0 = miss)' } } } });\n</script>\n")
	return sb.String()
}

func jsEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

// RenderBatchHTML writes a BatchRun as a single self-contained HTML file
// with four Chart.js comparison charts plus a summary matrix:
//
//  1. Success-rate bar chart (bars per scenario, OBFT vs QBFT).
//  2. Decision-time P50/P90/P99 grouped bars (per scenario, two protocols
//     × three percentiles = six bars).
//  3. Per-kind bandwidth stacked bars (per scenario, one stacked bar per
//     protocol; segments are message-kind contributions).
//  4. Latency-vs-success scatter (X = decision-time P99 in ms,
//     Y = success rate; one dot per (scenario, protocol)).
//
// All charts are populated from the BatchRun's BatchCells. Cells with
// Iterations=0 (scenario n/a for protocol) are skipped per chart.
//
// Chart.js loads via CDN — works in any modern browser when online; the
// summary matrix table renders fine offline.
func RenderBatchHTML(br *BatchRun, path string) error {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	sb.WriteString("<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&sb, "<title>%s</title>\n", html.EscapeString(br.Title))
	sb.WriteString(htmlStyles())
	sb.WriteString("<script src=\"https://cdn.jsdelivr.net/npm/chart.js\"></script>\n")
	sb.WriteString("</head>\n<body>\n")

	fmt.Fprintf(&sb, "<h1>%s</h1>\n", html.EscapeString(br.Title))
	if br.Description != "" {
		fmt.Fprintf(&sb, "<p class=\"desc\">%s</p>\n", html.EscapeString(br.Description))
	}
	fmt.Fprintf(&sb,
		"<p class=\"meta\">Iterations: %d · Wallclock: %v · Generated: %s · Cells: %d (%d × %d)</p>\n",
		br.Iterations, br.Wallclock,
		html.EscapeString(br.GeneratedAt.Format("2006-01-02 15:04:05")),
		len(br.Cells), len(br.Scenarios), len(br.ProtocolNames),
	)

	sb.WriteString(batchSummaryTable(br))
	sb.WriteString("<h2>Success rate</h2>\n<canvas id=\"successChart\" width=\"900\" height=\"420\"></canvas>\n")
	sb.WriteString(batchSuccessRateScript(br))
	sb.WriteString("<h2>Decision time (P50 / P90 / P99)</h2>\n<canvas id=\"latencyChart\" width=\"900\" height=\"420\"></canvas>\n")
	sb.WriteString(batchLatencyScript(br))
	sb.WriteString("<h2>Bandwidth per cell (median, stacked by kind)</h2>\n<canvas id=\"bandwidthChart\" width=\"900\" height=\"420\"></canvas>\n")
	sb.WriteString(batchBandwidthScript(br))
	sb.WriteString("<h2>Trade-off: P99 latency vs success rate</h2>\n<canvas id=\"tradeoffChart\" width=\"900\" height=\"420\"></canvas>\n")
	sb.WriteString(batchTradeoffScript(br))

	sb.WriteString("</body>\n</html>\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("reporting: write batch HTML %s: %w", path, err)
	}
	return nil
}

func batchSummaryTable(br *BatchRun) string {
	var sb strings.Builder
	sb.WriteString("<h2>Summary matrix</h2>\n<table>\n<thead><tr><th>Scenario</th>")
	for _, p := range br.ProtocolNames {
		fmt.Fprintf(&sb, "<th>%s</th>", html.EscapeString(p))
	}
	sb.WriteString("</tr></thead>\n<tbody>\n")
	for _, scenario := range br.Scenarios {
		fmt.Fprintf(&sb, "<tr><td class=\"scen\">%s</td>", html.EscapeString(scenario))
		for _, protocol := range br.ProtocolNames {
			cell, ok := br.CellAt(scenario, protocol)
			if !ok || !Applicable(cell) {
				sb.WriteString("<td class=\"na\">n/a</td>")
				continue
			}
			class := "ok"
			if cell.SuccessRate < 0.5 {
				class = "miss"
			} else if cell.SuccessRate < 1.0 {
				class = "warn"
			}
			p99 := cell.DecisionTime.Percentile(99)
			fmt.Fprintf(&sb, "<td class=\"%s\">%.0f%% · P99 %dms</td>",
				class, cell.SuccessRate*100, int(p99))
		}
		sb.WriteString("</tr>\n")
	}
	sb.WriteString("</tbody></table>\n")
	return sb.String()
}

func batchSuccessRateScript(br *BatchRun) string {
	var sb strings.Builder
	sb.WriteString("<script>\nnew Chart(document.getElementById('successChart'), { type: 'bar', data: { labels: [")
	for i, scenario := range br.Scenarios {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "'%s'", jsEscape(scenario))
	}
	sb.WriteString("], datasets: [")
	for i, protocol := range br.ProtocolNames {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "{ label: '%s', data: [", jsEscape(protocol))
		for j, scenario := range br.Scenarios {
			if j > 0 {
				sb.WriteString(",")
			}
			cell, ok := br.CellAt(scenario, protocol)
			if !ok || !Applicable(cell) {
				sb.WriteString("null")
			} else {
				fmt.Fprintf(&sb, "%.4f", cell.SuccessRate)
			}
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: false, scales: { y: { min: 0, max: 1, title: { display: true, text: 'Success rate' } } }, plugins: { title: { display: true, text: 'Success rate per scenario' } } } });\n</script>\n")
	return sb.String()
}

func batchLatencyScript(br *BatchRun) string {
	var sb strings.Builder
	sb.WriteString("<script>\nnew Chart(document.getElementById('latencyChart'), { type: 'bar', data: { labels: [")
	for i, scenario := range br.Scenarios {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "'%s'", jsEscape(scenario))
	}
	sb.WriteString("], datasets: [")
	// Six datasets: P50 / P90 / P99 × two protocols. Group order:
	// protocol-then-percentile so each protocol's three bars are visually
	// adjacent and easy to compare against the other protocol's group.
	first := true
	for _, protocol := range br.ProtocolNames {
		for _, pct := range []struct {
			label string
			p     float64
		}{{"P50", 50}, {"P90", 90}, {"P99", 99}} {
			if !first {
				sb.WriteString(",")
			}
			first = false
			fmt.Fprintf(&sb, "{ label: '%s %s', data: [", jsEscape(protocol), pct.label)
			for j, scenario := range br.Scenarios {
				if j > 0 {
					sb.WriteString(",")
				}
				cell, ok := br.CellAt(scenario, protocol)
				if !ok || !Applicable(cell) || cell.DecisionTime.Len() == 0 {
					sb.WriteString("null")
				} else {
					fmt.Fprintf(&sb, "%.0f", cell.DecisionTime.Percentile(pct.p))
				}
			}
			sb.WriteString("] }")
		}
	}
	sb.WriteString("] }, options: { responsive: false, scales: { y: { title: { display: true, text: 'ms' } } }, plugins: { title: { display: true, text: 'Decision time percentiles (only successful sims)' } } } });\n</script>\n")
	return sb.String()
}

func batchBandwidthScript(br *BatchRun) string {
	// Collect kind names across all cells (any kind that any cell emits).
	kindSet := make(map[string]bool)
	for _, c := range br.Cells {
		for k := range c.PerKindBandwidth {
			kindSet[k] = true
		}
	}
	kinds := make([]string, 0, len(kindSet))
	for k := range kindSet {
		kinds = append(kinds, k)
	}
	// Stable order for chart legend.
	stableSort(kinds)

	var sb strings.Builder
	sb.WriteString("<script>\nnew Chart(document.getElementById('bandwidthChart'), { type: 'bar', data: { labels: [")
	// X labels: "scenario · protocol" — one bar per cell so kind segments stack within.
	first := true
	for _, scenario := range br.Scenarios {
		for _, protocol := range br.ProtocolNames {
			if !first {
				sb.WriteString(",")
			}
			first = false
			fmt.Fprintf(&sb, "'%s · %s'", jsEscape(scenario), jsEscape(protocol))
		}
	}
	sb.WriteString("], datasets: [")
	// One dataset per kind; each dataset has one data point per (scenario, protocol) cell.
	for i, kind := range kinds {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "{ label: '%s', data: [", jsEscape(kind))
		dfirst := true
		for _, scenario := range br.Scenarios {
			for _, protocol := range br.ProtocolNames {
				if !dfirst {
					sb.WriteString(",")
				}
				dfirst = false
				cell, ok := br.CellAt(scenario, protocol)
				if !ok || !Applicable(cell) {
					sb.WriteString("0")
					continue
				}
				dist, present := cell.PerKindBandwidth[kind]
				if !present || dist.Len() == 0 {
					sb.WriteString("0")
				} else {
					fmt.Fprintf(&sb, "%.0f", dist.Median())
				}
			}
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: false, scales: { x: { stacked: true }, y: { stacked: true, title: { display: true, text: 'Bytes (median per cell)' } } }, plugins: { title: { display: true, text: 'Bandwidth median per cell, stacked by kind' } } } });\n</script>\n")
	return sb.String()
}

func batchTradeoffScript(br *BatchRun) string {
	var sb strings.Builder
	sb.WriteString("<script>\nnew Chart(document.getElementById('tradeoffChart'), { type: 'scatter', data: { datasets: [")
	for i, protocol := range br.ProtocolNames {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "{ label: '%s', data: [", jsEscape(protocol))
		first := true
		for _, scenario := range br.Scenarios {
			cell, ok := br.CellAt(scenario, protocol)
			if !ok || !Applicable(cell) {
				continue
			}
			if !first {
				sb.WriteString(",")
			}
			first = false
			p99 := cell.DecisionTime.Percentile(99)
			fmt.Fprintf(&sb, "{ x: %.0f, y: %.4f, label: '%s' }",
				p99, cell.SuccessRate, jsEscape(scenario))
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: false, scales: { x: { title: { display: true, text: 'P99 decision time (ms)' } }, y: { min: 0, max: 1, title: { display: true, text: 'Success rate' } } }, plugins: { title: { display: true, text: 'Latency vs success rate (one point per scenario/protocol)' }, tooltip: { callbacks: { label: function(ctx) { return ctx.raw.label + ': P99=' + ctx.raw.x + 'ms, success=' + (ctx.raw.y*100).toFixed(0) + '%'; } } } } } });\n</script>\n")
	return sb.String()
}

// stableSort is a small in-place insertion sort. Used to keep the
// reporting package's only dependency on sort to one place; insertion
// sort is fine at chart-label cardinality (≤ 10 kinds typically).
func stableSort(a []string) {
	for i := 1; i < len(a); i++ {
		for j := i; j > 0 && a[j-1] > a[j]; j-- {
			a[j-1], a[j] = a[j], a[j-1]
		}
	}
}

// RenderSweepIndex writes an HTML index page listing entries grouped by
// sweep name. Used by batch driver tests to provide a navigable
// top-level page when many per-(sweep, point) reports are generated.
//
// Entries are grouped by SweepName in the order they appear in the
// input; within each sweep, in the order entries were appended.
func RenderSweepIndex(title, description string, entries []SweepIndexEntry, path string) error {
	var sb strings.Builder
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n")
	sb.WriteString("<meta charset=\"utf-8\">\n")
	fmt.Fprintf(&sb, "<title>%s</title>\n", html.EscapeString(title))
	sb.WriteString(htmlStyles())
	sb.WriteString("</head>\n<body>\n")

	fmt.Fprintf(&sb, "<h1>%s</h1>\n", html.EscapeString(title))
	if description != "" {
		fmt.Fprintf(&sb, "<p class=\"desc\">%s</p>\n", html.EscapeString(description))
	}

	// Group entries by SweepName preserving insertion order.
	type group struct {
		name        string
		description string
		axisLabel   string
		rows        []SweepIndexEntry
	}
	var groups []*group
	byName := make(map[string]*group)
	for _, e := range entries {
		g, ok := byName[e.SweepName]
		if !ok {
			g = &group{name: e.SweepName, description: e.SweepDescription, axisLabel: e.AxisLabel}
			byName[e.SweepName] = g
			groups = append(groups, g)
		}
		g.rows = append(g.rows, e)
	}

	for _, g := range groups {
		fmt.Fprintf(&sb, "<h2>%s</h2>\n", html.EscapeString(g.name))
		if g.description != "" {
			fmt.Fprintf(&sb, "<p class=\"desc\">%s</p>\n", html.EscapeString(g.description))
		}
		if g.axisLabel != "" {
			fmt.Fprintf(&sb, "<p class=\"meta\">Axis: %s</p>\n", html.EscapeString(g.axisLabel))
		}
		sb.WriteString("<table>\n<thead><tr><th>Point</th><th>HTML</th><th>CSV</th><th>Markdown</th></tr></thead>\n<tbody>\n")
		for _, e := range g.rows {
			fmt.Fprintf(&sb, "<tr><td class=\"scen\">%s</td>", html.EscapeString(e.PointLabel))
			fmt.Fprintf(&sb, "<td><a href=\"%s\">HTML</a></td>", html.EscapeString(e.HTMLPath))
			fmt.Fprintf(&sb, "<td><a href=\"%s\">CSV</a></td>", html.EscapeString(e.CSVPath))
			fmt.Fprintf(&sb, "<td><a href=\"%s\">Markdown</a></td>", html.EscapeString(e.MarkdownPath))
			sb.WriteString("</tr>\n")
		}
		sb.WriteString("</tbody></table>\n")
	}

	sb.WriteString("</body>\n</html>\n")
	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("reporting: write sweep index %s: %w", path, err)
	}
	return nil
}

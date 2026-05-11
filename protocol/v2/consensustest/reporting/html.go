// Package reporting renders consensustest comparison results as a single
// self-contained HTML page. The page is laid out as a scrollable SPA: a
// sticky table-of-contents at the top links to one section per sweep, and
// each section either shows the detail-panel set (1-point sweep) or
// per-axis trend line charts (multi-point sweep). Chart.js loads via CDN;
// the summary matrix tables render fine offline.
//
// Usage from a test:
//
//	sweepResults := []ct.SweepResult{ct.RunSweep(t, ct.DefaultSweeps(...)[0]), ...}
//	c := reporting.Comparison{
//	    Title: "OBFT vs QBFT", Description: "...",
//	    Sweeps: sweepResults, Iterations: 100,
//	    Wallclock: elapsed, GeneratedAt: time.Now(),
//	}
//	reporting.RenderComparison(c, "out.html")
package reporting

import (
	"fmt"
	"html"
	"os"
	"sort"
	"strings"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Comparison bundles all data needed to render one consolidated SPA-style
// HTML comparing OBFT vs QBFT across a set of sweeps. Each sweep with a
// single point renders as a detail section (summary matrix + four charts);
// each multi-point sweep renders as a trend section (three line charts
// with the swept axis on X).
type Comparison struct {
	Title       string
	Description string
	Sweeps      []ct.SweepResult
	Iterations  int
	Wallclock   time.Duration
	GeneratedAt time.Time
}

// Applicable reports whether the cell ran ≥ 1 iteration. n/a cells (the
// scenario doesn't translate to that protocol) carry Iterations=0.
func Applicable(c ct.BatchCell) bool { return c.Iterations > 0 }

// RenderComparison writes a single self-contained HTML page with all
// sweeps inline, viewable by scrolling top-to-bottom. Chart.js loads via
// CDN; the summary matrix tables render fine offline.
func RenderComparison(c Comparison, path string) error {
	if len(c.Sweeps) == 0 {
		return fmt.Errorf("reporting: RenderComparison: no sweeps")
	}
	// Sweep names become HTML element IDs (section anchors + canvas IDs).
	// Duplicates would collide silently and produce a broken page.
	seen := make(map[string]bool, len(c.Sweeps))
	for _, sw := range c.Sweeps {
		if seen[sw.Sweep.Name] {
			return fmt.Errorf("reporting: RenderComparison: duplicate sweep name %q", sw.Sweep.Name)
		}
		seen[sw.Sweep.Name] = true
	}
	scenarios := extractScenarioOrder(c.Sweeps)
	protocols := extractProtocolOrder(c.Sweeps)
	colors := buildScenarioPalette(scenarios)

	var sb strings.Builder
	writeDocStart(&sb, c.Title)
	writeStyles(&sb)
	sb.WriteString("<script src=\"https://cdn.jsdelivr.net/npm/chart.js\"></script>\n")
	sb.WriteString("</head>\n<body>\n")

	writeHeader(&sb, c)
	writeTOC(&sb, c.Sweeps)
	sb.WriteString("<main>\n")
	for _, sw := range c.Sweeps {
		writeSweepSection(&sb, sw, scenarios, protocols, colors)
	}
	sb.WriteString("</main>\n")
	sb.WriteString("</body>\n</html>\n")

	if err := os.WriteFile(path, []byte(sb.String()), 0o644); err != nil {
		return fmt.Errorf("reporting: write HTML %s: %w", path, err)
	}
	return nil
}

// ---- top-level scaffolding ---------------------------------------------

func writeDocStart(sb *strings.Builder, title string) {
	sb.WriteString("<!DOCTYPE html>\n<html lang=\"en\">\n<head>\n<meta charset=\"utf-8\">\n")
	fmt.Fprintf(sb, "<title>%s</title>\n", html.EscapeString(title))
}

func writeStyles(sb *strings.Builder) {
	sb.WriteString(`<style>
:root { color-scheme: light; }
* { box-sizing: border-box; }
body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; max-width: 1400px; margin: 0 auto; padding: 0 1.5em 4em; color: #1a1a1a; line-height: 1.5; }
header.page { padding: 2em 0 1em; border-bottom: 1px solid #eee; }
header.page h1 { margin: 0 0 0.3em; font-size: 1.8em; }
.desc { color: #555; font-style: italic; margin: 0.2em 0 0.5em; }
.meta { color: #888; font-size: 0.9em; margin: 0.2em 0; }
nav.toc { position: sticky; top: 0; z-index: 10; background: rgba(255,255,255,0.96); backdrop-filter: blur(6px); padding: 0.7em 0; margin: 0 0 1em; border-bottom: 1px solid #eee; font-size: 0.9em; }
nav.toc a { margin-right: 1em; color: #0969da; text-decoration: none; }
nav.toc a:hover { text-decoration: underline; }
section.sweep { padding-top: 2em; margin-top: 1em; border-top: 1px solid #eee; }
section.sweep h2 { margin: 0 0 0.2em; font-size: 1.4em; }
section.sweep h3 { margin: 1.5em 0 0.4em; font-size: 1.05em; color: #333; font-weight: 600; }
.axis { color: #888; font-size: 0.85em; margin: 0.1em 0 0.5em; }
table.matrix { border-collapse: collapse; margin: 0.6em 0; font-size: 0.9em; }
table.matrix th, table.matrix td { padding: 0.35em 0.7em; text-align: left; border-bottom: 1px solid #eee; }
table.matrix th { background: #f7f7f7; font-weight: 600; }
table.matrix td.scen { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", sans-serif; color: #444; }
.ok { color: #1a7f37; }
.warn { color: #b07d00; }
.miss { color: #d1242f; }
.na { color: #999; }
.chart-wrap { position: relative; width: 100%; max-width: 1300px; margin: 0.5em 0 1.5em; }
canvas { display: block; max-width: 100%; }
.back-to-top { display: inline-block; margin: 1em 0 0; font-size: 0.85em; color: #0969da; text-decoration: none; }
.back-to-top:hover { text-decoration: underline; }
</style>
`)
}

func writeHeader(sb *strings.Builder, c Comparison) {
	sb.WriteString("<header class=\"page\" id=\"top\">\n")
	fmt.Fprintf(sb, "<h1>%s</h1>\n", html.EscapeString(c.Title))
	if c.Description != "" {
		fmt.Fprintf(sb, "<p class=\"desc\">%s</p>\n", html.EscapeString(c.Description))
	}
	fmt.Fprintf(sb,
		"<p class=\"meta\">Iterations per cell: %d · Total wallclock: %v · Generated: %s</p>\n",
		c.Iterations, c.Wallclock, html.EscapeString(c.GeneratedAt.Format("2006-01-02 15:04:05")))
	sb.WriteString("</header>\n")
}

func writeTOC(sb *strings.Builder, sweeps []ct.SweepResult) {
	sb.WriteString("<nav class=\"toc\">\n")
	for _, sw := range sweeps {
		fmt.Fprintf(sb, "<a href=\"#sweep-%s\">%s</a>",
			html.EscapeString(sw.Sweep.Name), html.EscapeString(sw.Sweep.DisplayTitle()))
	}
	sb.WriteString("\n</nav>\n")
}

// ---- per-sweep section ------------------------------------------------

func writeSweepSection(sb *strings.Builder, sw ct.SweepResult, scenarios []ct.Scenario, protocols []string, colors map[string]string) {
	fmt.Fprintf(sb, "<section class=\"sweep\" id=\"sweep-%s\">\n", html.EscapeString(sw.Sweep.Name))
	fmt.Fprintf(sb, "<h2>%s</h2>\n", html.EscapeString(sw.Sweep.DisplayTitle()))
	if sw.Sweep.Description != "" {
		fmt.Fprintf(sb, "<p class=\"desc\">%s</p>\n", html.EscapeString(sw.Sweep.Description))
	}
	if sw.Sweep.AxisLabel != "" {
		fmt.Fprintf(sb, "<p class=\"axis\">Swept axis: %s</p>\n", html.EscapeString(sw.Sweep.AxisLabel))
	}

	if len(sw.Reports) == 1 {
		writeDetailPanels(sb, sw, scenarios, protocols, colors)
	} else {
		writeTrendPanels(sb, sw, scenarios, protocols, colors)
	}

	sb.WriteString("<a class=\"back-to-top\" href=\"#top\">↑ back to top</a>\n")
	sb.WriteString("</section>\n")
}

// ---- detail panels (single-point sweep) -------------------------------

func writeDetailPanels(sb *strings.Builder, sw ct.SweepResult, scenarios []ct.Scenario, protocols []string, colors map[string]string) {
	report := sw.Reports[0]
	id := sw.Sweep.Name

	sb.WriteString("<h3>Summary matrix</h3>\n")
	writeSummaryMatrix(sb, report, scenarios, protocols)

	sb.WriteString("<h3>Success rate per scenario</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_success\" width=\"1200\" height=\"480\"></canvas></div>\n", id)
	writeDetailSuccessRateScript(sb, id, report, scenarios, protocols)

	sb.WriteString("<h3>Decision time per scenario (P50 / P90 / P99)</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_latency\" width=\"1200\" height=\"480\"></canvas></div>\n", id)
	writeDetailLatencyScript(sb, id, report, scenarios, protocols)

	sb.WriteString("<h3>Bandwidth per cell (median, stacked by message kind)</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_bandwidth\" width=\"1200\" height=\"480\"></canvas></div>\n", id)
	writeDetailBandwidthScript(sb, id, report, scenarios, protocols)

	sb.WriteString("<h3>Trade-off: P99 latency vs success rate</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_tradeoff\" width=\"1200\" height=\"480\"></canvas></div>\n", id)
	writeDetailTradeoffScript(sb, id, report, scenarios, protocols, colors)
}

func writeSummaryMatrix(sb *strings.Builder, report ct.BatchReport, scenarios []ct.Scenario, protocols []string) {
	sb.WriteString("<table class=\"matrix\">\n<thead><tr><th>Scenario</th>")
	for _, p := range protocols {
		fmt.Fprintf(sb, "<th>%s</th>", html.EscapeString(p))
	}
	sb.WriteString("</tr></thead>\n<tbody>\n")
	for _, sc := range scenarios {
		fmt.Fprintf(sb, "<tr><td class=\"scen\">%s</td>", html.EscapeString(sc.DisplayTitle()))
		for _, p := range protocols {
			cell, ok := findCell(report.Cells, sc.Name, p)
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
			fmt.Fprintf(sb, "<td class=\"%s\">%.0f%% · P99 %dms</td>",
				class, cell.SuccessRate*100, int(p99))
		}
		sb.WriteString("</tr>\n")
	}
	sb.WriteString("</tbody></table>\n")
}

func writeDetailSuccessRateScript(sb *strings.Builder, id string, report ct.BatchReport, scenarios []ct.Scenario, protocols []string) {
	fmt.Fprintf(sb, "<script>\nnew Chart(document.getElementById('%s_success'), { type: 'bar', data: { labels: [", id)
	for i, sc := range scenarios {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(sb, "'%s'", jsEscape(sc.DisplayTitle()))
	}
	sb.WriteString("], datasets: [")
	for i, p := range protocols {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(sb, "{ label: '%s', backgroundColor: '%s', data: [", jsEscape(p), protocolColor(p))
		for j, sc := range scenarios {
			if j > 0 {
				sb.WriteString(",")
			}
			cell, ok := findCell(report.Cells, sc.Name, p)
			if !ok || !Applicable(cell) {
				sb.WriteString("null")
			} else {
				fmt.Fprintf(sb, "%.4f", cell.SuccessRate)
			}
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: true, maintainAspectRatio: false, scales: { y: { min: 0, max: 1, title: { display: true, text: 'Success rate' } }, x: { ticks: { autoSkip: false, maxRotation: 60, minRotation: 30 } } } } });\n</script>\n")
}

func writeDetailLatencyScript(sb *strings.Builder, id string, report ct.BatchReport, scenarios []ct.Scenario, protocols []string) {
	fmt.Fprintf(sb, "<script>\nnew Chart(document.getElementById('%s_latency'), { type: 'bar', data: { labels: [", id)
	for i, sc := range scenarios {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(sb, "'%s'", jsEscape(sc.DisplayTitle()))
	}
	sb.WriteString("], datasets: [")
	first := true
	for _, p := range protocols {
		for _, pct := range []struct {
			label string
			p     float64
			alpha float64
		}{{"P50", 50, 0.55}, {"P90", 90, 0.75}, {"P99", 99, 1.0}} {
			if !first {
				sb.WriteString(",")
			}
			first = false
			fmt.Fprintf(sb, "{ label: '%s %s', backgroundColor: '%s', data: [",
				jsEscape(p), pct.label, protocolColorAlpha(p, pct.alpha))
			for j, sc := range scenarios {
				if j > 0 {
					sb.WriteString(",")
				}
				cell, ok := findCell(report.Cells, sc.Name, p)
				if !ok || !Applicable(cell) || cell.DecisionTime.Len() == 0 {
					sb.WriteString("null")
				} else {
					fmt.Fprintf(sb, "%.0f", cell.DecisionTime.Percentile(pct.p))
				}
			}
			sb.WriteString("] }")
		}
	}
	sb.WriteString("] }, options: { responsive: true, maintainAspectRatio: false, scales: { y: { title: { display: true, text: 'ms (only successful sims)' } }, x: { ticks: { autoSkip: false, maxRotation: 60, minRotation: 30 } } } } });\n</script>\n")
}

func writeDetailBandwidthScript(sb *strings.Builder, id string, report ct.BatchReport, scenarios []ct.Scenario, protocols []string) {
	kindSet := make(map[string]bool)
	for _, c := range report.Cells {
		for k := range c.PerKindBandwidth {
			kindSet[k] = true
		}
	}
	kinds := make([]string, 0, len(kindSet))
	for k := range kindSet {
		kinds = append(kinds, k)
	}
	sort.Strings(kinds)

	fmt.Fprintf(sb, "<script>\nnew Chart(document.getElementById('%s_bandwidth'), { type: 'bar', data: { labels: [", id)
	first := true
	for _, sc := range scenarios {
		for _, p := range protocols {
			if !first {
				sb.WriteString(",")
			}
			first = false
			fmt.Fprintf(sb, "'%s · %s'", jsEscape(sc.DisplayTitle()), jsEscape(p))
		}
	}
	sb.WriteString("], datasets: [")
	for i, kind := range kinds {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(sb, "{ label: '%s', backgroundColor: '%s', data: [", jsEscape(kind), kindColor(i, len(kinds)))
		dfirst := true
		for _, sc := range scenarios {
			for _, p := range protocols {
				if !dfirst {
					sb.WriteString(",")
				}
				dfirst = false
				cell, ok := findCell(report.Cells, sc.Name, p)
				if !ok || !Applicable(cell) {
					sb.WriteString("0")
					continue
				}
				dist, present := cell.PerKindBandwidth[kind]
				if !present || dist.Len() == 0 {
					sb.WriteString("0")
				} else {
					fmt.Fprintf(sb, "%.0f", dist.Median())
				}
			}
		}
		sb.WriteString("] }")
	}
	sb.WriteString("] }, options: { responsive: true, maintainAspectRatio: false, scales: { x: { stacked: true, ticks: { autoSkip: false, maxRotation: 70, minRotation: 45 } }, y: { stacked: true, title: { display: true, text: 'Bytes (median per cell)' } } } } });\n</script>\n")
}

func writeDetailTradeoffScript(sb *strings.Builder, id string, report ct.BatchReport, scenarios []ct.Scenario, protocols []string, colors map[string]string) {
	fmt.Fprintf(sb, "<script>\nnew Chart(document.getElementById('%s_tradeoff'), { type: 'scatter', data: { datasets: [", id)
	first := true
	for _, p := range protocols {
		for _, sc := range scenarios {
			cell, ok := findCell(report.Cells, sc.Name, p)
			if !ok || !Applicable(cell) {
				continue
			}
			if !first {
				sb.WriteString(",")
			}
			first = false
			p99 := cell.DecisionTime.Percentile(99)
			fmt.Fprintf(sb,
				"{ label: '%s (%s)', backgroundColor: '%s', borderColor: '%s', pointStyle: '%s', data: [{ x: %.0f, y: %.4f }] }",
				jsEscape(sc.DisplayTitle()), jsEscape(p),
				colors[sc.Name], colors[sc.Name], protocolPointStyle(p),
				p99, cell.SuccessRate)
		}
	}
	sb.WriteString("] }, options: { responsive: true, maintainAspectRatio: false, scales: { x: { title: { display: true, text: 'P99 decision time (ms)' } }, y: { min: 0, max: 1, title: { display: true, text: 'Success rate' } } }, plugins: { legend: { display: true, position: 'right', labels: { boxWidth: 12, font: { size: 10 } } } } } });\n</script>\n")
}

// ---- trend panels (multi-point sweep) ---------------------------------

func writeTrendPanels(sb *strings.Builder, sw ct.SweepResult, scenarios []ct.Scenario, protocols []string, colors map[string]string) {
	id := sw.Sweep.Name
	xLabels := make([]string, len(sw.Sweep.Points))
	for i, pt := range sw.Sweep.Points {
		xLabels[i] = pt.Label
	}

	sb.WriteString("<h3>Success rate vs swept axis</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_success\" width=\"1200\" height=\"500\"></canvas></div>\n", id)
	writeTrendLineScript(sb, id+"_success", sw, scenarios, protocols, colors, xLabels,
		"Success rate", 0, 1, metricSuccessRate)

	sb.WriteString("<h3>Decision time P99 vs swept axis</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_p99\" width=\"1200\" height=\"500\"></canvas></div>\n", id)
	writeTrendLineScript(sb, id+"_p99", sw, scenarios, protocols, colors, xLabels,
		"P99 decision time (ms)", -1, -1, metricP99Latency)

	sb.WriteString("<h3>Bandwidth median vs swept axis</h3>\n")
	fmt.Fprintf(sb, "<div class=\"chart-wrap\"><canvas id=\"%s_bw\" width=\"1200\" height=\"500\"></canvas></div>\n", id)
	writeTrendLineScript(sb, id+"_bw", sw, scenarios, protocols, colors, xLabels,
		"Bytes (median per cell)", -1, -1, metricBandwidthMedian)
}

// metricFn extracts one scalar from a cell; returns false to emit `null`
// at that data point (n/a or no samples).
type metricFn func(c ct.BatchCell) (float64, bool)

func metricSuccessRate(c ct.BatchCell) (float64, bool) {
	if !Applicable(c) {
		return 0, false
	}
	return c.SuccessRate, true
}

func metricP99Latency(c ct.BatchCell) (float64, bool) {
	if !Applicable(c) || c.DecisionTime.Len() == 0 {
		return 0, false
	}
	return c.DecisionTime.Percentile(99), true
}

func metricBandwidthMedian(c ct.BatchCell) (float64, bool) {
	if !Applicable(c) || c.ClusterBandwidth.Len() == 0 {
		return 0, false
	}
	return c.ClusterBandwidth.Median(), true
}

func writeTrendLineScript(sb *strings.Builder, canvasID string, sw ct.SweepResult,
	scenarios []ct.Scenario, protocols []string, colors map[string]string,
	xLabels []string, yLabel string, yMin, yMax float64, metric metricFn,
) {
	fmt.Fprintf(sb, "<script>\nnew Chart(document.getElementById('%s'), { type: 'line', data: { labels: [", canvasID)
	for i, lbl := range xLabels {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(sb, "'%s'", jsEscape(lbl))
	}
	sb.WriteString("], datasets: [")

	first := true
	for _, sc := range scenarios {
		for _, p := range protocols {
			// Gather data series for this (scenario, protocol) across all sweep points.
			values := make([]string, len(sw.Reports))
			anyValue := false
			for i, rep := range sw.Reports {
				cell, ok := findCell(rep.Cells, sc.Name, p)
				if !ok {
					values[i] = "null"
					continue
				}
				v, valid := metric(cell)
				if !valid {
					values[i] = "null"
					continue
				}
				values[i] = fmt.Sprintf("%.4f", v)
				anyValue = true
			}
			if !anyValue {
				continue // skip all-null datasets to reduce legend clutter
			}
			if !first {
				sb.WriteString(",")
			}
			first = false
			dash := ""
			if isDashedProtocol(p) {
				dash = ", borderDash: [6,4]"
			}
			fmt.Fprintf(sb,
				"{ label: '%s (%s)', borderColor: '%s', backgroundColor: '%s', pointStyle: '%s', spanGaps: true, fill: false, tension: 0.15%s, data: [",
				jsEscape(sc.DisplayTitle()), jsEscape(p),
				colors[sc.Name], colors[sc.Name], protocolPointStyle(p), dash)
			for i, v := range values {
				if i > 0 {
					sb.WriteString(",")
				}
				sb.WriteString(v)
			}
			sb.WriteString("] }")
		}
	}

	sb.WriteString("] }, options: { responsive: true, maintainAspectRatio: false, interaction: { mode: 'nearest', intersect: false }, scales: { x: { title: { display: true, text: ")
	fmt.Fprintf(sb, "'%s'", jsEscape(sw.Sweep.AxisLabel))
	sb.WriteString(" } }, y: {")
	if yMin >= 0 && yMax > yMin {
		fmt.Fprintf(sb, " min: %g, max: %g,", yMin, yMax)
	}
	fmt.Fprintf(sb, " title: { display: true, text: '%s' } } }, plugins: { legend: { position: 'right', labels: { boxWidth: 12, font: { size: 10 } } } } } });\n</script>\n", jsEscape(yLabel))
}

// ---- helpers ----------------------------------------------------------

func extractScenarioOrder(sweeps []ct.SweepResult) []ct.Scenario {
	if len(sweeps) == 0 || len(sweeps[0].Reports) == 0 {
		return nil
	}
	return sweeps[0].Reports[0].Config.Scenarios
}

func extractProtocolOrder(sweeps []ct.SweepResult) []string {
	if len(sweeps) == 0 || len(sweeps[0].Reports) == 0 {
		return nil
	}
	protos := sweeps[0].Reports[0].Config.Protocols
	names := make([]string, len(protos))
	for i, p := range protos {
		names[i] = p.Name()
	}
	return names
}

func findCell(cells []ct.BatchCell, scenario, protocol string) (ct.BatchCell, bool) {
	for _, c := range cells {
		if c.Scenario == scenario && c.Protocol == protocol {
			return c, true
		}
	}
	return ct.BatchCell{}, false
}

// buildScenarioPalette assigns one HSL color per scenario, spaced evenly
// around the hue circle. Stable: same scenario set in same order always
// produces the same palette.
func buildScenarioPalette(scenarios []ct.Scenario) map[string]string {
	palette := make(map[string]string, len(scenarios))
	n := len(scenarios)
	if n == 0 {
		return palette
	}
	for i, sc := range scenarios {
		hue := (i * 360) / n
		palette[sc.Name] = fmt.Sprintf("hsl(%d, 65%%, 50%%)", hue)
	}
	return palette
}

// protocolColor returns the headline color used for solid-fill chart
// elements (bars in detail panels). OBFT is a calm blue; QBFT a warm red;
// any other protocol gets a neutral gray.
func protocolColor(name string) string {
	switch name {
	case "OBFT":
		return "#0969da"
	case "QBFT":
		return "#cf222e"
	default:
		return "#888"
	}
}

func protocolColorAlpha(name string, alpha float64) string {
	r, g, b := protocolRGB(name)
	return fmt.Sprintf("rgba(%d,%d,%d,%.2f)", r, g, b, alpha)
}

func protocolRGB(name string) (int, int, int) {
	switch name {
	case "OBFT":
		return 9, 105, 218
	case "QBFT":
		return 207, 34, 46
	default:
		return 136, 136, 136
	}
}

// isDashedProtocol picks the line style for trend charts. OBFT solid,
// QBFT dashed, everything else dashed.
func isDashedProtocol(name string) bool {
	return name != "OBFT"
}

func protocolPointStyle(name string) string {
	switch name {
	case "OBFT":
		return "circle"
	case "QBFT":
		return "triangle"
	default:
		return "rect"
	}
}

// kindColor returns a per-message-kind color for stacked bandwidth bars.
// Distinct hues at fixed saturation/lightness so segments are visually
// separable in the stacked bar.
func kindColor(idx, total int) string {
	if total <= 0 {
		return "#888"
	}
	hue := (idx * 360) / total
	return fmt.Sprintf("hsl(%d, 55%%, 55%%)", hue)
}

func jsEscape(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `'`, `\'`)
	return s
}

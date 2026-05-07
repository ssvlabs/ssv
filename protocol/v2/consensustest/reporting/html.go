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

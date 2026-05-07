package consensustest

import (
	"fmt"
	"sort"
	"strings"
	"time"
)

// MatrixReport collects per-(protocol, scenario) Results and renders them
// as a compact table for diagnostic inspection.
type MatrixReport struct {
	protocols []string                     // order preserved
	scenarios []string                     // order preserved
	results   map[string]map[string]Result // [scenario][protocol]
}

func NewMatrixReport(protocols []string, scenarios []string) *MatrixReport {
	return &MatrixReport{
		protocols: protocols,
		scenarios: scenarios,
		results:   make(map[string]map[string]Result, len(scenarios)),
	}
}

func (m *MatrixReport) Record(r Result) {
	if m.results[r.ScenarioName] == nil {
		m.results[r.ScenarioName] = make(map[string]Result, len(m.protocols))
	}
	m.results[r.ScenarioName][r.ProtocolName] = r
}

// Render produces a compact ASCII table. Cells show ✓ <decision_time> L<round>
// on success, ✗ on miss, n/a on not-applicable, ! on mismatch (footnoted).
func (m *MatrixReport) Render() string {
	var b strings.Builder

	// Header.
	scenarioWidth := 25
	for _, s := range m.scenarios {
		if len(s) > scenarioWidth {
			scenarioWidth = len(s)
		}
	}
	const cellWidth = 16
	fmt.Fprintf(&b, "%-*s |", scenarioWidth, "Scenario")
	for _, p := range m.protocols {
		fmt.Fprintf(&b, " %-*s |", cellWidth-2, p)
	}
	b.WriteString("\n")
	fmt.Fprintf(&b, "%s+", strings.Repeat("-", scenarioWidth+1))
	for range m.protocols {
		fmt.Fprintf(&b, "%s+", strings.Repeat("-", cellWidth))
	}
	b.WriteString("\n")

	// Rows.
	mismatches := []string{}
	for _, sname := range m.scenarios {
		fmt.Fprintf(&b, "%-*s |", scenarioWidth, sname)
		for _, pname := range m.protocols {
			r, ok := m.results[sname][pname]
			cell := ""
			switch {
			case !ok:
				cell = "—"
			case r.Skipped:
				cell = "n/a"
			case !r.Match:
				cell = "! mismatch"
				mismatches = append(mismatches,
					fmt.Sprintf("  %s/%s: %s (got %s)", sname, pname, r.Why, renderCell(r)))
			case r.Outcome.Decided:
				cell = fmt.Sprintf("✓ %v L%d", r.Outcome.DecisionTime.Round(10*time.Millisecond), r.Outcome.DecidedRound)
			default:
				cell = "✗ miss"
			}
			fmt.Fprintf(&b, " %-*s |", cellWidth-2, cell)
		}
		b.WriteString("\n")
	}

	if len(mismatches) > 0 {
		b.WriteString("\nMismatches:\n")
		sort.Strings(mismatches)
		for _, m := range mismatches {
			b.WriteString(m + "\n")
		}
	}
	return b.String()
}

// AnyMismatch reports whether any cell did not match its declared expectation.
func (m *MatrixReport) AnyMismatch() bool {
	for _, scenarioMap := range m.results {
		for _, r := range scenarioMap {
			if !r.Match {
				return true
			}
		}
	}
	return false
}

func renderCell(r Result) string {
	if r.Skipped {
		return "n/a"
	}
	if r.Outcome.Decided {
		return fmt.Sprintf("✓ %v L%d", r.Outcome.DecisionTime, r.Outcome.DecidedRound)
	}
	return "✗ miss"
}

package consensustest_test

import (
	"strings"
	"testing"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestComparison_Matrix runs every scenario × protocol combination and
// asserts the outcome matches the scenario's declared per-protocol
// expectation. Universal safety invariants (no two V signatures, honest
// agreement, termination) are enforced by RunScenarioOnProtocol — any
// violation panics regardless of declared expectation.
//
// Output: a compact ASCII table of the comparison matrix, plus a final
// pass/fail signal based on whether any cell mismatched.
func TestComparison_Matrix(t *testing.T) {
	base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)

	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		qbftadapter.Protocol{},
	}
	protoNames := []string{}
	for _, p := range protocols {
		protoNames = append(protoNames, p.Name())
	}

	scenarioNames := []string{}
	for _, s := range ct.Catalog {
		scenarioNames = append(scenarioNames, s.Name)
	}

	matrix := ct.NewMatrixReport(protoNames, scenarioNames)
	for _, p := range protocols {
		for _, s := range ct.Catalog {
			r := ct.RunScenarioOnProtocol(t, p, s, base)
			matrix.Record(r)
		}
	}

	report := matrix.Render()
	t.Logf("\n%s", report)

	if matrix.AnyMismatch() {
		t.Fatalf("comparison matrix has mismatches:\n%s", report)
	}
}

// TestComparison_BTTSweep runs the matrix at multiple BTT operating points
// to surface where each protocol's envelope breaks. Logs a per-BTT table;
// safety invariants are enforced inside RunScenarioOnProtocol via panic, so
// any SingleV/HonestAgreement violation crashes the run. Per-cell match
// expectations are intentionally NOT asserted — at off-spec BTT values, some
// scenarios are expected to mismatch (that's the sweep's purpose).
func TestComparison_BTTSweep(t *testing.T) {
	bttValues := []time.Duration{
		100 * time.Millisecond, // ideal mesh
		200 * time.Millisecond, // operating point per OBFT.md §Application
		400 * time.Millisecond, // degraded mesh
	}

	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		qbftadapter.Protocol{},
	}

	for _, btt := range bttValues {
		btt := btt
		t.Run("BTT="+btt.String(), func(t *testing.T) {
			t.Parallel()
			base := ct.DefaultProposerDutyConfig(btt)

			var b strings.Builder
			b.WriteString("\nBTT=" + btt.String() + ":\n")
			b.WriteString("Scenario                    | OBFT          | QBFT          | Notes\n")
			b.WriteString("----------------------------+---------------+---------------+----------------\n")
			for _, s := range ct.Catalog {
				cells := []string{}
				for _, p := range protocols {
					r := ct.RunScenarioOnProtocol(t, p, s, base)
					cell := renderCell(r)
					cells = append(cells, cell)
				}
				b.WriteString(formatRow(s.Name, cells, s.Note))
			}
			t.Log(b.String())
		})
	}
}

func renderCell(r ct.Result) string {
	if r.Skipped {
		return "n/a"
	}
	if !r.Match {
		return "! mismatch"
	}
	if r.Outcome.Decided {
		return durationOnly(r.Outcome.DecisionTime) + " L" + intStr(r.Outcome.DecidedRound)
	}
	return "✗ miss"
}

func formatRow(name string, cells []string, note string) string {
	out := padRight(name, 28) + " |"
	for _, c := range cells {
		out += " " + padRight(c, 13) + " |"
	}
	if note != "" && len(note) > 60 {
		note = note[:60] + "..."
	}
	out += " " + note + "\n"
	return out
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}

func intStr(i int) string {
	if i < 0 {
		return "-1"
	}
	return string(rune('0' + i))
}

func durationOnly(d time.Duration) string {
	rounded := d.Round(10 * time.Millisecond)
	return rounded.String()
}

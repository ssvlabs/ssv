// Package reporting renders consensustest results as HTML / CSV / Markdown
// for visual inspection of stress-test runs. Reports are gated behind a
// Makefile target so default `go test` runs stay quiet.
//
// Reports cover the matrix of (scenario × protocol) cells plus per-cell
// metadata (decision time, decided round, bandwidth, evidence counts). HTML
// reports load Chart.js via CDN — single self-contained file, viewable in
// any modern browser.
//
// Usage from a test:
//
//	run := reporting.NewRun("Title", "Description")
//	for _, s := range scenarios {
//	    for _, p := range protocols {
//	        r := ct.RunScenarioOnProtocol(t, p, s, base)
//	        run.AddCell(s.Name, p.Name(), r)
//	    }
//	}
//	run.Finalize()
//	reporting.RenderHTML(run, "out.html")
//	reporting.RenderCSV(run, "out.csv")
//	reporting.RenderMarkdown(run, "out.md")
package reporting

import (
	"strconv"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Run captures the inputs/outputs of one matrix sweep test, ready for
// rendering.
type Run struct {
	Title       string
	Description string
	StartedAt   time.Time
	Duration    time.Duration

	// Cells maps (scenario, protocol) → Result. Iteration order should
	// follow Scenarios × ProtocolNames for stable rendering.
	Cells         map[CellKey]ct.Result
	Scenarios     []string // scenario names in display order
	ProtocolNames []string // protocol names in display order

	// SweepDimensions optionally records the parameter axes a sweep test
	// varied (e.g., {"K": [3,4], "BTT": [100ms, 200ms]}).
	SweepDimensions map[string][]string
}

// CellKey uniquely identifies one (scenario, protocol) pairing.
type CellKey struct {
	Scenario string
	Protocol string
}

// NewRun returns an empty Run with allocated maps.
func NewRun(title, description string) *Run {
	return &Run{
		Title:           title,
		Description:     description,
		StartedAt:       time.Now(),
		Cells:           make(map[CellKey]ct.Result),
		SweepDimensions: make(map[string][]string),
	}
}

// AddCell records one matrix cell. Auto-tracks Scenario / Protocol order
// if not already present.
func (r *Run) AddCell(scenario, protocol string, result ct.Result) {
	r.Cells[CellKey{Scenario: scenario, Protocol: protocol}] = result
	if !contains(r.Scenarios, scenario) {
		r.Scenarios = append(r.Scenarios, scenario)
	}
	if !contains(r.ProtocolNames, protocol) {
		r.ProtocolNames = append(r.ProtocolNames, protocol)
	}
}

// Finalize stamps the run's Duration. Call once after all AddCell calls.
// Scenarios and ProtocolNames are preserved in insertion order (which is
// AddCell call order — typically the canonical Catalog order).
func (r *Run) Finalize() {
	r.Duration = time.Since(r.StartedAt)
}

func contains(s []string, x string) bool {
	for _, v := range s {
		if v == x {
			return true
		}
	}
	return false
}

// cellSummary returns a concise human-readable summary of one cell:
//
//	"3.9s L1"  on success
//	"miss"     on miss
//	"n/a"      on skip
//	"!err"     on adapter error
func cellSummary(c ct.Result) string {
	if c.Skipped {
		return "n/a"
	}
	if !c.Match && c.Why != "" {
		return "!err"
	}
	if c.Outcome.Decided {
		return c.Outcome.DecisionTime.Round(10*time.Millisecond).String() +
			" L" + strconv.Itoa(c.Outcome.DecidedRound)
	}
	return "miss"
}

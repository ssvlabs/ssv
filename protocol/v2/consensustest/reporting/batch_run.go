package reporting

import (
	"sort"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// BatchRun is the render-layer struct for a multi-sim batch comparison.
// Parallel to Run (which holds single-sim Results); BatchRun holds
// distribution-aware BatchCells from a consensustest.BatchReport.
//
// Usage:
//
//	report := ct.RunBatch(t, batchCfg)
//	br := reporting.NewBatchRun("OBFT vs QBFT — canonical", "...", report)
//	reporting.RenderBatchHTML(br, "out.html")
//	reporting.RenderBatchCSV(br, "out.csv")
//	reporting.RenderBatchMarkdown(br, "out.md")
type BatchRun struct {
	Title       string
	Description string

	// Cells are the per-(scenario, protocol) aggregates, in stable
	// (Scenarios × ProtocolNames) iteration order.
	Cells []ct.BatchCell

	// Scenarios / ProtocolNames are the unique values present in Cells,
	// in display order (Scenarios in Catalog order; Protocols in their
	// BatchConfig.Protocols order).
	Scenarios     []string
	ProtocolNames []string

	// Iterations is the per-cell sample count (= BatchConfig.Iterations).
	// Same across all cells in one BatchRun. Renderers display this in
	// the report header for context.
	Iterations int

	// Wallclock is how long the full batch took to run.
	Wallclock time.Duration

	// GeneratedAt timestamps the report (UTC). Set on construction.
	GeneratedAt time.Time

	// SweepDimensions optionally describes the parameter axes a sweep
	// varied across multiple BatchRuns (e.g., {"BTT": ["200ms", "400ms"]}).
	// Single-BatchRun reports leave this empty.
	SweepDimensions map[string][]string
}

// NewBatchRun converts a consensustest.BatchReport into a BatchRun ready
// for rendering. Cells are sorted by (Scenario, Protocol) for stable
// display order; the input report's Cells slice is not mutated.
//
// Title and Description are operator-facing context shown in report
// headers (e.g., "OBFT vs QBFT — canonical operating point").
func NewBatchRun(title, description string, report ct.BatchReport) *BatchRun {
	cellsCp := make([]ct.BatchCell, len(report.Cells))
	copy(cellsCp, report.Cells)
	sort.Slice(cellsCp, func(i, j int) bool {
		if cellsCp[i].Scenario != cellsCp[j].Scenario {
			return cellsCp[i].Scenario < cellsCp[j].Scenario
		}
		return cellsCp[i].Protocol < cellsCp[j].Protocol
	})

	br := &BatchRun{
		Title:           title,
		Description:     description,
		Cells:           cellsCp,
		Iterations:      report.Config.Iterations,
		Wallclock:       report.Wallclock,
		GeneratedAt:     report.GeneratedAt,
		SweepDimensions: make(map[string][]string),
	}

	// Preserve scenario order from BatchConfig (which typically reflects
	// the catalog's Apply order); preserve protocol order from
	// BatchConfig.Protocols.
	seenScenario := make(map[string]bool)
	for _, s := range report.Config.Scenarios {
		if seenScenario[s.Name] {
			continue
		}
		seenScenario[s.Name] = true
		br.Scenarios = append(br.Scenarios, s.Name)
	}
	for _, p := range report.Config.Protocols {
		br.ProtocolNames = append(br.ProtocolNames, p.Name())
	}

	return br
}

// CellAt looks up the cell for (scenario, protocol). Returns the cell and
// true if found; zero-value cell and false otherwise. Linear scan — fine
// at the matrix sizes the framework targets (≤ 25 scenarios × 4
// protocols).
func (b *BatchRun) CellAt(scenario, protocol string) (ct.BatchCell, bool) {
	for _, c := range b.Cells {
		if c.Scenario == scenario && c.Protocol == protocol {
			return c, true
		}
	}
	return ct.BatchCell{}, false
}

// Applicable reports whether the cell ran at least one iteration. Cells
// where the scenario returned ErrNotApplicable for the protocol have
// Iterations=0 — renderers treat them as "n/a".
func Applicable(c ct.BatchCell) bool {
	return c.Iterations > 0
}

// SweepIndexEntry is one row of the top-level index page — a link to a
// per-(sweep, point) report.
type SweepIndexEntry struct {
	SweepName        string
	SweepDescription string
	AxisLabel        string
	PointLabel       string
	HTMLPath         string // relative or absolute, used verbatim in <a href>
	CSVPath          string
	MarkdownPath     string
}

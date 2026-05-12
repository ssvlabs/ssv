// Package reporting writes the consensustest comparison report data as a
// small JavaScript bootstrap file (`data.js`) consumed by the static UI
// in `stresstest-report/` (index.html + app.js + styles.css).
//
// Splitting data from rendering means iteration on the UI is just
// "edit app.js / styles.css → refresh browser" — no test rerun needed.
// Re-running the test (`make stresstest`) only regenerates
// data.js with fresh stats.
//
// The static UI files are checked into git at `stresstest-report/`;
// `data.js` is gitignored.
//
// Usage from a test:
//
//	sweepResults := []ct.SweepResult{ct.RunSweep(t, ct.DefaultSweeps(...)[0]), ...}
//	c := reporting.Comparison{
//	    Title: "OBFT vs QBFT", Description: "...",
//	    Sweeps: sweepResults,
//	    BaselineIterations: 100, UnstableIterations: 10,
//	    Wallclock: elapsed,
//	}
//	reporting.WriteReportData(c, "./stresstest-report")
package reporting

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
)

// Comparison bundles all data needed for one consolidated report.
// Each sweep with a single point renders as a detail section (summary
// matrix + four charts); each multi-point sweep renders as a trend
// section (three line charts with the swept axis on X). Selection is
// done in the JS app based on `len(points)`.
type Comparison struct {
	Title       string
	Description string
	Sweeps      []ct.SweepResult
	// BaselineIterations / UnstableIterations carry the per-group iteration
	// counts the test driver applied (Baseline-group scenarios at Baseline,
	// every other scenario at Unstable). Per-cell counts also live on
	// BatchCell.Iterations, which is what the UI reads for rendering.
	BaselineIterations int
	UnstableIterations int
	Wallclock          time.Duration
}

// Applicable reports whether the cell ran ≥ 1 iteration. n/a cells (the
// scenario doesn't translate to that protocol) carry Iterations=0.
func Applicable(c ct.BatchCell) bool { return c.Iterations > 0 }

// WriteReportData writes <dir>/data.js containing a `window.REPORT_DATA = {...}`
// assignment consumed by the static UI in <dir>. The companion files
// (index.html, app.js, styles.css) are checked into git at
// `stresstest-report/` and not touched by this function.
func WriteReportData(c Comparison, dir string) error {
	if len(c.Sweeps) == 0 {
		return fmt.Errorf("reporting: WriteReportData: no sweeps")
	}
	// Sweep names become DOM IDs in the UI; duplicates would collide.
	seen := make(map[string]bool, len(c.Sweeps))
	for _, sw := range c.Sweeps {
		if seen[sw.Sweep.Name] {
			return fmt.Errorf("reporting: WriteReportData: duplicate sweep name %q", sw.Sweep.Name)
		}
		seen[sw.Sweep.Name] = true
	}

	payload := buildPayload(c)
	body, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("reporting: marshal: %w", err)
	}
	out := append([]byte("window.REPORT_DATA = "), body...)
	out = append(out, ';', '\n')

	path := filepath.Join(dir, "data.js")
	if err := os.WriteFile(path, out, 0o644); err != nil {
		return fmt.Errorf("reporting: write %s: %w", path, err)
	}
	return nil
}

// ---- payload DTOs ----------------------------------------------------
//
// Field naming follows JS conventions (camelCase) so the UI consumes
// `window.REPORT_DATA.successRate` directly. Optional fields use
// omitempty so the JSON stays compact and "no data" is distinguishable
// from "value zero" in the UI (e.g., a cell with no successful sims
// omits decisionTime entirely; the UI treats missing as "no samples").

type reportPayload struct {
	Title       string `json:"title"`
	Description string `json:"description,omitempty"`
	// BaselineIterations / UnstableIterations carry the per-group counts the
	// driver applied. Per-cell iteration counts live on cellPayload.Iterations
	// (which is what the UI's CDF rendering keys off of).
	BaselineIterations int               `json:"baselineIterations,omitempty"`
	UnstableIterations int               `json:"unstableIterations,omitempty"`
	Wallclock          string            `json:"wallclock"`
	Scenarios          []scenarioPayload `json:"scenarios"`
	Protocols          []string          `json:"protocols"`
	Sweeps             []sweepPayload    `json:"sweeps"`
}

type scenarioPayload struct {
	Name        string `json:"name"`
	Title       string `json:"title"`
	Group       string `json:"group,omitempty"`
	Adversarial bool   `json:"adversarial,omitempty"`
	Note        string `json:"note,omitempty"`
}

type sweepPayload struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Params      []string       `json:"params,omitempty"`
	Description string         `json:"description,omitempty"`
	AxisLabel   string         `json:"axisLabel,omitempty"`
	Points      []pointPayload `json:"points"`
}

type pointPayload struct {
	Label string `json:"label"`
	// Fields exposes the numeric axis values for this point (K, BTT in ms,
	// Sigma, …) so the UI can look up points by exact value without
	// parsing Label. Mirrors SweepPoint.Fields verbatim.
	Fields map[string]float64 `json:"fields,omitempty"`
	Cells  []cellPayload      `json:"cells"`
}

type cellPayload struct {
	Scenario   string  `json:"scenario"`
	Protocol   string  `json:"protocol"`
	Iterations int     `json:"iterations"`
	SuccessRate float64 `json:"successRate"`
	// DecisionTimes is the sorted ms-integer decision-time sample for
	// every successful sim. Length = SuccessRate × Iterations. Emitted
	// so the UI can render a CDF directly instead of just summary
	// percentiles. omitted when no sim decided.
	DecisionTimes    []int               `json:"decisionTimes,omitempty"`
	DecisionTime     *percentilesPayload `json:"decisionTime,omitempty"`
	ClusterBandwidth *percentilesPayload `json:"clusterBandwidth,omitempty"`
	PerKindBandwidth map[string]float64  `json:"perKindBandwidth,omitempty"`
	MissReasons      map[string]int      `json:"missReasons,omitempty"`
}

type percentilesPayload struct {
	P50  float64 `json:"p50"`
	P90  float64 `json:"p90"`
	P99  float64 `json:"p99"`
	Mean float64 `json:"mean"`
}

func buildPayload(c Comparison) reportPayload {
	scenarios := extractScenarios(c.Sweeps)
	protocols := extractProtocols(c.Sweeps)

	pl := reportPayload{
		Title:              c.Title,
		Description:        c.Description,
		BaselineIterations: c.BaselineIterations,
		UnstableIterations: c.UnstableIterations,
		Wallclock:          c.Wallclock.String(),
		Protocols:          protocols,
	}
	for _, sc := range scenarios {
		pl.Scenarios = append(pl.Scenarios, scenarioPayload{
			Name: sc.Name, Title: sc.DisplayTitle(), Group: sc.Group,
			Adversarial: sc.IsAdversarial(),
			Note:        sc.Note,
		})
	}
	for _, sw := range c.Sweeps {
		swp := sweepPayload{
			Name:        sw.Sweep.Name,
			Title:       sw.Sweep.DisplayTitle(),
			Params:      sw.Sweep.Params,
			Description: sw.Sweep.Description,
			AxisLabel:   sw.Sweep.AxisLabel,
		}
		for i, rep := range sw.Reports {
			pt := pointPayload{
				Label:  sw.Sweep.Points[i].Label,
				Fields: sw.Sweep.Points[i].Fields,
			}
			for _, cell := range rep.Cells {
				pt.Cells = append(pt.Cells, buildCell(cell))
			}
			swp.Points = append(swp.Points, pt)
		}
		pl.Sweeps = append(pl.Sweeps, swp)
	}
	return pl
}

func buildCell(c ct.BatchCell) cellPayload {
	out := cellPayload{
		Scenario:    c.Scenario,
		Protocol:    c.Protocol,
		Iterations:  c.Iterations,
		SuccessRate: c.SuccessRate,
	}
	if c.DecisionTime.Len() > 0 {
		out.DecisionTime = &percentilesPayload{
			P50:  c.DecisionTime.Percentile(50),
			P90:  c.DecisionTime.Percentile(90),
			P99:  c.DecisionTime.Percentile(99),
			Mean: c.DecisionTime.Mean(),
		}
		// Sorted integer-ms samples for the UI CDF chart. One per successful
		// sim; absent values (missed sims) are implicit via Iterations -
		// len(DecisionTimes).
		out.DecisionTimes = make([]int, len(c.DecisionTime))
		for i, v := range c.DecisionTime {
			out.DecisionTimes[i] = int(v + 0.5)
		}
		sort.Ints(out.DecisionTimes)
	}
	if c.ClusterBandwidth.Len() > 0 {
		out.ClusterBandwidth = &percentilesPayload{
			P50:  c.ClusterBandwidth.Percentile(50),
			P90:  c.ClusterBandwidth.Percentile(90),
			P99:  c.ClusterBandwidth.Percentile(99),
			Mean: c.ClusterBandwidth.Mean(),
		}
	}
	if len(c.PerKindBandwidth) > 0 {
		// Emit median per kind. Sorted keys for deterministic output.
		kinds := make([]string, 0, len(c.PerKindBandwidth))
		for k := range c.PerKindBandwidth {
			kinds = append(kinds, k)
		}
		sort.Strings(kinds)
		out.PerKindBandwidth = make(map[string]float64, len(kinds))
		for _, k := range kinds {
			d := c.PerKindBandwidth[k]
			if d.Len() == 0 {
				continue
			}
			out.PerKindBandwidth[k] = d.Median()
		}
	}
	if len(c.MissReasons) > 0 {
		out.MissReasons = make(map[string]int, len(c.MissReasons))
		for k, v := range c.MissReasons {
			out.MissReasons[k] = v
		}
	}
	return out
}

func extractScenarios(sweeps []ct.SweepResult) []ct.Scenario {
	if len(sweeps) == 0 || len(sweeps[0].Reports) == 0 {
		return nil
	}
	return sweeps[0].Reports[0].Config.Scenarios
}

func extractProtocols(sweeps []ct.SweepResult) []string {
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

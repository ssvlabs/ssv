// Package reporting writes the consensustest comparison report data as a
// small JavaScript bootstrap file (`data.js`) consumed by the static UI
// in `consensustest-reports/` (index.html + app.js + styles.css).
//
// Splitting data from rendering means iteration on the UI is just
// "edit app.js / styles.css → refresh browser" — no test rerun needed.
// Re-running the test (`make consensustest-report`) only regenerates
// data.js with fresh stats.
//
// The static UI files are checked into git at `consensustest-reports/`;
// `data.js` is gitignored.
//
// Usage from a test:
//
//	sweepResults := []ct.SweepResult{ct.RunSweep(t, ct.DefaultSweeps(...)[0]), ...}
//	c := reporting.Comparison{
//	    Title: "OBFT vs QBFT", Description: "...",
//	    Sweeps: sweepResults, Iterations: 100,
//	    Wallclock: elapsed, GeneratedAt: time.Now(),
//	}
//	reporting.WriteReportData(c, "./consensustest-reports")
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
	Iterations  int
	Wallclock   time.Duration
	GeneratedAt time.Time
}

// Applicable reports whether the cell ran ≥ 1 iteration. n/a cells (the
// scenario doesn't translate to that protocol) carry Iterations=0.
func Applicable(c ct.BatchCell) bool { return c.Iterations > 0 }

// WriteReportData writes <dir>/data.js containing a `window.REPORT_DATA = {...}`
// assignment consumed by the static UI in <dir>. The companion files
// (index.html, app.js, styles.css) are checked into git at
// `consensustest-reports/` and not touched by this function.
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
	Title       string            `json:"title"`
	Description string            `json:"description,omitempty"`
	Iterations  int               `json:"iterations"`
	Wallclock   string            `json:"wallclock"`
	GeneratedAt string            `json:"generatedAt"`
	Scenarios   []scenarioPayload `json:"scenarios"`
	Protocols   []string          `json:"protocols"`
	Sweeps      []sweepPayload    `json:"sweeps"`
}

type scenarioPayload struct {
	Name  string `json:"name"`
	Title string `json:"title"`
	Group string `json:"group,omitempty"`
}

type sweepPayload struct {
	Name        string         `json:"name"`
	Title       string         `json:"title"`
	Description string         `json:"description,omitempty"`
	AxisLabel   string         `json:"axisLabel,omitempty"`
	Points      []pointPayload `json:"points"`
}

type pointPayload struct {
	Label string        `json:"label"`
	Cells []cellPayload `json:"cells"`
}

type cellPayload struct {
	Scenario         string              `json:"scenario"`
	Protocol         string              `json:"protocol"`
	Iterations       int                 `json:"iterations"`
	SuccessRate      float64             `json:"successRate"`
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
		Title:       c.Title,
		Description: c.Description,
		Iterations:  c.Iterations,
		Wallclock:   c.Wallclock.String(),
		GeneratedAt: c.GeneratedAt.Format("2006-01-02 15:04:05"),
		Protocols:   protocols,
	}
	for _, sc := range scenarios {
		pl.Scenarios = append(pl.Scenarios, scenarioPayload{
			Name: sc.Name, Title: sc.DisplayTitle(), Group: sc.Group,
		})
	}
	for _, sw := range c.Sweeps {
		swp := sweepPayload{
			Name:        sw.Sweep.Name,
			Title:       sw.Sweep.DisplayTitle(),
			Description: sw.Sweep.Description,
			AxisLabel:   sw.Sweep.AxisLabel,
		}
		for i, rep := range sw.Reports {
			pt := pointPayload{Label: sw.Sweep.Points[i].Label}
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

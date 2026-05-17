package reporting_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// smallScenarios picks a few scenarios for fast smoke tests. Mixes:
//   - Healthy (success both protocols)
//   - Equivocate_111 (miss OBFT, success QBFT)
//   - HV1SelectiveDelivery (success OBFT, n/a QBFT)
func smallScenarios(t *testing.T) []ct.Scenario {
	t.Helper()
	out := []ct.Scenario{}
	wanted := map[string]bool{
		"Healthy":              true,
		"Equivocate_111":       true,
		"HV1SelectiveDelivery": true,
	}
	for _, s := range ct.Catalog {
		if wanted[s.Name] {
			out = append(out, s)
		}
	}
	require.Len(t, out, 3)
	return out
}

func runOneSweep(t *testing.T, name, description, axis string, points []ct.SweepPoint) ct.SweepResult {
	t.Helper()
	return ct.RunSweep(t, ct.Sweep{
		Name: name, Description: description, AxisLabel: axis, Points: points,
	})
}

// parseDataJS reads <dir>/data.js, strips the JS wrapper, and parses the
// JSON payload into a map for assertions. data.js has the shape:
//
//	window.REPORT_DATA = { ... };
func parseDataJS(t *testing.T, dir string) map[string]any {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(dir, "data.js"))
	require.NoError(t, err)
	body := strings.TrimSpace(string(raw))
	body = strings.TrimPrefix(body, "window.REPORT_DATA = ")
	body = strings.TrimSuffix(body, ";")
	var out map[string]any
	require.NoError(t, json.Unmarshal([]byte(body), &out), "data.js JSON payload must parse")
	return out
}

// TestWriteReportData_PayloadShape — drives a small comparison through
// WriteReportData and verifies the JSON payload has the expected top-
// level keys + at least one cell with the expected sub-shape.
func TestWriteReportData_PayloadShape(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	canonical := runOneSweep(t, "canonical", "Reference: n=4 BTT=200ms", "",
		[]ct.SweepPoint{{
			Label: "n=4 BTT=200ms",
			Config: ct.BatchConfig{
				Iterations: 3, SeedStart: 1,
				Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios: scenarios, Protocols: protos,
			},
		}})

	dir := t.TempDir()
	require.NoError(t, reporting.WriteReportData(reporting.Comparison{
		Title:              "smoke comparison",
		Description:        "three scenarios × two protocols",
		Sweeps:             []ct.SweepResult{canonical},
		BaselineIterations: 3,
		UnstableIterations: 3,
		Wallclock:          123 * time.Millisecond,
	}, dir))

	payload := parseDataJS(t, dir)
	require.Equal(t, "smoke comparison", payload["title"])
	require.Equal(t, "three scenarios × two protocols", payload["description"])
	require.EqualValues(t, 3, payload["baselineIterations"])
	require.EqualValues(t, 3, payload["unstableIterations"])
	require.NotContains(t, payload, "iterations", "legacy top-level iterations field removed from payload")
	require.NotContains(t, payload, "generatedAt", "generatedAt field removed from payload")

	protocols := payload["protocols"].([]any)
	require.Equal(t, []any{"OBFT", "QBFT"}, protocols)

	scenarioList := payload["scenarios"].([]any)
	require.Len(t, scenarioList, 3, "3 scenarios in payload")
	// Scenarios carry Title + Group for the UI.
	first := scenarioList[0].(map[string]any)
	require.NotEmpty(t, first["title"], "scenario.title populated")
	require.NotEmpty(t, first["group"], "scenario.group populated")

	sweeps := payload["sweeps"].([]any)
	require.Len(t, sweeps, 1)
	canon := sweeps[0].(map[string]any)
	require.Equal(t, "canonical", canon["name"])
	require.NotEmpty(t, canon["title"], "sweep.title populated")
	points := canon["points"].([]any)
	require.Len(t, points, 1)
	cells := points[0].(map[string]any)["cells"].([]any)
	require.Equal(t, 6, len(cells), "3 scenarios × 2 protocols = 6 cells")
}

// TestWriteReportData_NACellOmitsDecisionTime — a cell with Iterations=0
// (scenario n/a for protocol) carries iterations=0 + successRate=0 and
// has no decisionTime/clusterBandwidth/perKindBandwidth keys, so the UI
// can detect n/a via `cell.iterations === 0`.
func TestWriteReportData_NACellOmitsDecisionTime(t *testing.T) {
	naScenario := []ct.Scenario{}
	for _, s := range ct.Catalog {
		// CertWithholding is OBFT-specific (cluster cert gossip phase)
		// and translates to ErrNotApplicable on the QBFT side — a stable
		// "n/a for QBFT" pick for this test.
		if s.Name == "CertWithholding" {
			naScenario = append(naScenario, s)
			break
		}
	}
	require.Len(t, naScenario, 1)

	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	canonical := runOneSweep(t, "canonical", "n=4", "",
		[]ct.SweepPoint{{Label: "n=4 BTT=200ms",
			Config: ct.BatchConfig{
				Iterations: 3, SeedStart: 1,
				Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios: naScenario, Protocols: protos,
			}}})

	dir := t.TempDir()
	require.NoError(t, reporting.WriteReportData(reporting.Comparison{
		Title: "na test", Sweeps: []ct.SweepResult{canonical}, BaselineIterations: 3, UnstableIterations: 3,
	}, dir))
	payload := parseDataJS(t, dir)
	cells := payload["sweeps"].([]any)[0].(map[string]any)["points"].([]any)[0].(map[string]any)["cells"].([]any)
	var qbft map[string]any
	for _, c := range cells {
		cm := c.(map[string]any)
		if cm["protocol"] == "QBFT" {
			qbft = cm
			break
		}
	}
	require.NotNil(t, qbft, "QBFT cell present")
	require.EqualValues(t, 0, qbft["iterations"], "iterations=0 means n/a")
	require.NotContains(t, qbft, "decisionTime", "no decisionTime key for n/a cell")
	require.NotContains(t, qbft, "clusterBandwidth", "no clusterBandwidth key for n/a cell")
}

// TestWriteReportData_EmptySweeps — rejects empty input rather than
// silently writing an empty data file.
func TestWriteReportData_EmptySweeps(t *testing.T) {
	err := reporting.WriteReportData(reporting.Comparison{
		Title: "empty", Sweeps: nil,
	}, t.TempDir())
	require.Error(t, err)
}

// TestWriteReportData_DuplicateSweepNames — sweep names become DOM IDs;
// duplicates collide silently and are rejected up front.
func TestWriteReportData_DuplicateSweepNames(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	one := runOneSweep(t, "same", "first", "", []ct.SweepPoint{{
		Label: "n=4", Config: ct.BatchConfig{
			Iterations: 1, SeedStart: 1,
			Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
			Scenarios: scenarios, Protocols: protos,
		}}})
	two := runOneSweep(t, "same", "second", "", []ct.SweepPoint{{
		Label: "n=4", Config: ct.BatchConfig{
			Iterations: 1, SeedStart: 2,
			Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
			Scenarios: scenarios, Protocols: protos,
		}}})

	err := reporting.WriteReportData(reporting.Comparison{
		Title: "dup", Sweeps: []ct.SweepResult{one, two}, BaselineIterations: 1, UnstableIterations: 1,
	}, t.TempDir())
	require.Error(t, err)
	require.Contains(t, err.Error(), `duplicate sweep name "same"`)
}

// TestWriteReportData_MultiPointSweep — verifies the payload has the
// expected `points` shape for a multi-point sweep (used for trend
// charts in the UI).
func TestWriteReportData_MultiPointSweep(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	btt := runOneSweep(t, "btt", "BTT sweep", "BTT",
		[]ct.SweepPoint{
			{Label: "BTT=100ms", Config: ct.BatchConfig{
				Iterations: 2, SeedStart: 1,
				Base:      ct.DefaultProposerDutyConfig(100 * time.Millisecond),
				Scenarios: scenarios, Protocols: protos,
			}},
			{Label: "BTT=200ms", Config: ct.BatchConfig{
				Iterations: 2, SeedStart: 2,
				Base:      ct.DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios: scenarios, Protocols: protos,
			}},
		})

	dir := t.TempDir()
	require.NoError(t, reporting.WriteReportData(reporting.Comparison{
		Title: "trend", Sweeps: []ct.SweepResult{btt}, BaselineIterations: 2, UnstableIterations: 2,
	}, dir))

	payload := parseDataJS(t, dir)
	sw := payload["sweeps"].([]any)[0].(map[string]any)
	require.Equal(t, "BTT", sw["axisLabel"])
	points := sw["points"].([]any)
	require.Len(t, points, 2)
	require.Equal(t, "BTT=100ms", points[0].(map[string]any)["label"])
	require.Equal(t, "BTT=200ms", points[1].(map[string]any)["label"])
}

// TestApplicable_ZeroIterationsIsFalse — sanity check the helper used
// by external callers to detect n/a cells (mirrored in the UI's check
// `cell.iterations === 0`).
func TestApplicable_ZeroIterationsIsFalse(t *testing.T) {
	require.False(t, reporting.Applicable(ct.BatchCell{Iterations: 0}))
	require.True(t, reporting.Applicable(ct.BatchCell{Iterations: 1}))
}

// TestDefaultSweeps_FieldsKeyStable enforces the contract documented on
// the reporting layer's fieldsKey helper: every Fields value emitted by
// DefaultSweeps must round-trip through `%g` byte-identically so the
// merge can dedup points by Fields-tuple across runs.
//
// Implemented as a black-box round-trip: format each emitted float with
// %g, parse it back, re-format with %g, and assert the two strings are
// byte-identical. Today every Fields value is a literal or an int-cast
// float so the check passes trivially; the value of the test is to
// catch a future sweep that ever computes a Fields value (e.g.
// `BTT/3`) without rounding to canonical form — which would silently
// split one logical point into two across runs.
func TestDefaultSweeps_FieldsKeyStable(t *testing.T) {
	scenarios := ct.ScenariosWithMode(ct.Catalog, ct.ModeStress)
	require.NotEmpty(t, scenarios)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	iters := ct.Iterations{Baseline: 1, Unstable: 1}
	sweeps := ct.DefaultSweeps(scenarios, protos, iters, 4, 2, ct.P2PProfileNames)
	require.NotEmpty(t, sweeps)

	for _, sw := range sweeps {
		for i, pt := range sw.Points {
			for key, val := range pt.Fields {
				formatted := strconv.FormatFloat(val, 'g', -1, 64)
				parsed, err := strconv.ParseFloat(formatted, 64)
				require.NoErrorf(t, err, "sweep=%q point=%d field=%q value=%v failed to parse back from %%g format",
					sw.Name, i, key, val)
				reformatted := strconv.FormatFloat(parsed, 'g', -1, 64)
				require.Equalf(t, formatted, reformatted,
					"sweep=%q point=%d field=%q value=%v: %%g format %q is not round-trip stable — would split into two logical points across merged runs",
					sw.Name, i, key, val, formatted)
			}
		}
	}
}

// TestWriteReportData_MergesAcrossRuns — WriteReportData composes points
// from multiple runs at different (n, K) operating points into one
// data.js instead of overwriting. Run 1 contributes (n=4, K=4); run 2
// contributes (n=4, K=2); run 3 re-runs (n=4, K=4) at a different
// iteration count and the freshest data replaces the original-K=4
// point — we pin that by varying `iters` between runs and reading
// back the iterations the K=4 point reports.
func TestWriteReportData_MergesAcrossRuns(t *testing.T) {
	scenarios := smallScenarios(t)
	protos := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	dir := t.TempDir()

	build := func(n, k, iters int) reporting.Comparison {
		base := ct.DefaultProposerDutyConfig(200 * time.Millisecond)
		base.N = n
		base.Operators = ct.MakeOperators(n)
		base.K = k
		return reporting.Comparison{
			Title:              "merge test",
			BaselineIterations: iters,
			UnstableIterations: iters,
			Sweeps: []ct.SweepResult{runOneSweep(t, "p2p_baseline", "", "", []ct.SweepPoint{{
				Label:  "n=" + strconv.Itoa(n) + " K=" + strconv.Itoa(k),
				Fields: map[string]float64{"N": float64(n), "K": float64(k), "BTT": 300, "Sigma": 0.5},
				Config: ct.BatchConfig{
					Iterations: iters, SeedStart: 1,
					Base:      base,
					Scenarios: scenarios, Protocols: protos,
				},
			}})},
		}
	}

	// pointByLabel finds a point by its Label field; returns nil if absent.
	pointByLabel := func(pts []any, want string) map[string]any {
		for _, p := range pts {
			m := p.(map[string]any)
			if m["label"] == want {
				return m
			}
		}
		return nil
	}
	// firstCellIters returns the cell iterations from a point (a stable
	// run-distinguishing marker: increasing iters across runs is reflected
	// here even though the cell's own scenario doesn't otherwise vary).
	firstCellIters := func(pt map[string]any) float64 {
		cells := pt["cells"].([]any)
		return cells[0].(map[string]any)["iterations"].(float64)
	}

	// Run 1: (n=4, K=4) at iters=1.
	require.NoError(t, reporting.WriteReportData(build(4, 4, 1), dir))
	pl := parseDataJS(t, dir)
	pts := pl["sweeps"].([]any)[0].(map[string]any)["points"].([]any)
	require.Len(t, pts, 1, "after run 1, one point")
	require.Equal(t, "n=4 K=4", pts[0].(map[string]any)["label"])
	require.Equal(t, float64(1), firstCellIters(pts[0].(map[string]any)),
		"run 1 K=4 should carry iters=1")

	// Run 2: (n=4, K=2) — appended, prior point preserved.
	require.NoError(t, reporting.WriteReportData(build(4, 2, 1), dir))
	pl = parseDataJS(t, dir)
	pts = pl["sweeps"].([]any)[0].(map[string]any)["points"].([]any)
	require.Len(t, pts, 2, "after run 2, two points (one per (n, K))")
	require.NotNil(t, pointByLabel(pts, "n=4 K=4"), "K=4 point preserved across run 2")
	require.NotNil(t, pointByLabel(pts, "n=4 K=2"), "K=2 point present after run 2")

	// Run 3: (n=4, K=4) at iters=3 — replaces the original K=4 slice.
	// The point count stays at 2, but the K=4 point now reports the
	// fresh iterations count from run 3 (the replace-not-append
	// semantic). A regression that accidentally appended instead of
	// replacing would either yield Len=3 or leave iters=1 on the K=4
	// point.
	require.NoError(t, reporting.WriteReportData(build(4, 4, 3), dir))
	pl = parseDataJS(t, dir)
	pts = pl["sweeps"].([]any)[0].(map[string]any)["points"].([]any)
	require.Len(t, pts, 2, "after re-run, point count still 2")
	k4 := pointByLabel(pts, "n=4 K=4")
	require.NotNil(t, k4, "K=4 point still present after run 3")
	require.Equal(t, float64(3), firstCellIters(k4),
		"run 3 K=4 should overwrite run 1's iters=1 with iters=3")
}


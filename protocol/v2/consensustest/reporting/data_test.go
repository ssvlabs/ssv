package reporting_test

import (
	"encoding/json"
	"os"
	"path/filepath"
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
		Title:       "smoke comparison",
		Description: "three scenarios × two protocols",
		Sweeps:      []ct.SweepResult{canonical},
		Iterations:  3,
		Wallclock:   123 * time.Millisecond,
		GeneratedAt: time.Date(2026, 5, 11, 12, 0, 0, 0, time.UTC),
	}, dir))

	payload := parseDataJS(t, dir)
	require.Equal(t, "smoke comparison", payload["title"])
	require.Equal(t, "three scenarios × two protocols", payload["description"])
	require.EqualValues(t, 3, payload["iterations"])
	require.Equal(t, "2026-05-11 12:00:00", payload["generatedAt"])

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
		if s.Name == "HV1SelectiveDelivery" {
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
		Title: "na test", Sweeps: []ct.SweepResult{canonical}, Iterations: 3,
		GeneratedAt: time.Now(),
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
		Title: "dup", Sweeps: []ct.SweepResult{one, two}, Iterations: 1,
		GeneratedAt: time.Now(),
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
		Title: "trend", Sweeps: []ct.SweepResult{btt}, Iterations: 2,
		GeneratedAt: time.Now(),
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

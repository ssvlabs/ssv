package reporting_test

import (
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// TestNewBatchRun_ConvertsReport — running a small batch, converting to
// BatchRun, and verifying the conversion preserves scenario/protocol
// ordering plus all cells.
func TestNewBatchRun_ConvertsReport(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" || s.Name == "PrimaryLeaderSilent" {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 2)

	report := ct.RunBatch(t, ct.BatchConfig{
		Iterations: 3,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	})

	br := reporting.NewBatchRun("Smoke", "BatchRun conversion smoke", report)
	require.Equal(t, "Smoke", br.Title)
	require.Equal(t, "BatchRun conversion smoke", br.Description)
	require.Equal(t, 3, br.Iterations)
	require.Len(t, br.Cells, 4, "2 scenarios × 2 protocols = 4 cells")
	require.Equal(t, []string{"Healthy", "PrimaryLeaderSilent"}, br.Scenarios)
	require.Equal(t, []string{"OBFT", "QBFT"}, br.ProtocolNames)
	require.NotZero(t, br.GeneratedAt)

	// CellAt lookup works for every (scenario, protocol) pair.
	for _, sn := range br.Scenarios {
		for _, pn := range br.ProtocolNames {
			cell, ok := br.CellAt(sn, pn)
			require.Truef(t, ok, "missing cell for %s/%s", sn, pn)
			require.Equal(t, sn, cell.Scenario)
			require.Equal(t, pn, cell.Protocol)
			require.True(t, reporting.Applicable(cell), "all cells in this test should be applicable")
		}
	}
}

// TestRenderBatchHTML_SmokeContent — small batch → BatchRun → RenderHTML.
// Verifies the rendered HTML contains all four charts and the summary
// matrix populated with expected content for hand-inspection.
func TestRenderBatchHTML_SmokeContent(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" || s.Name == "PrimaryLeaderSilent" || s.Name == "Equivocate_111" {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 3)

	report := ct.RunBatch(t, ct.BatchConfig{
		Iterations: 5,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	})
	br := reporting.NewBatchRun("Batch smoke", "Three-scenario × two-protocol smoke", report)

	path := t.TempDir() + "/batch.html"
	require.NoError(t, reporting.RenderBatchHTML(br, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)

	// Header.
	require.Contains(t, body, "<title>Batch smoke</title>", "HTML title")
	require.Contains(t, body, "Three-scenario × two-protocol smoke", "HTML description")
	require.Contains(t, body, "Iterations: 5", "HTML iteration count")

	// Summary matrix.
	require.Contains(t, body, "<h2>Summary matrix</h2>", "summary matrix section")
	require.Contains(t, body, ">Healthy<", "Healthy row in matrix")
	require.Contains(t, body, ">Equivocate_111<", "Equivocate_111 row")
	require.Contains(t, body, "P99 ", "P99 latency cells")

	// Charts.
	require.Contains(t, body, `<canvas id="successChart"`, "success chart")
	require.Contains(t, body, `<canvas id="latencyChart"`, "latency chart")
	require.Contains(t, body, `<canvas id="bandwidthChart"`, "bandwidth chart")
	require.Contains(t, body, `<canvas id="tradeoffChart"`, "tradeoff scatter")

	// Chart.js init scripts.
	require.Contains(t, body, "type: 'bar'", "bar chart config")
	require.Contains(t, body, "type: 'scatter'", "scatter chart config")
	require.Contains(t, body, "stacked: true", "bandwidth chart is stacked")

	// Legend labels.
	require.Contains(t, body, "OBFT P50", "OBFT P50 series")
	require.Contains(t, body, "OBFT P99", "OBFT P99 series")
	require.Contains(t, body, "QBFT P50", "QBFT P50 series")

	// Footer.
	require.Contains(t, body, "</html>", "HTML closes")
}

// TestRenderBatchCSV_SmokeContent — small batch → BatchRun → RenderCSV.
// Verifies header columns + per-cell rows + n/a handling.
func TestRenderBatchCSV_SmokeContent(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" || s.Name == "Equivocate_111" || s.Name == "HV1SelectiveDelivery" {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 3)

	report := ct.RunBatch(t, ct.BatchConfig{
		Iterations: 5,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	})
	br := reporting.NewBatchRun("CSV smoke", "", report)

	path := t.TempDir() + "/batch.csv"
	require.NoError(t, reporting.RenderBatchCSV(br, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)

	require.Contains(t, body, "Scenario,Protocol,Iterations,SuccessRate", "header")
	require.Contains(t, body, "DecisionTime_P50_ms,DecisionTime_P90_ms,DecisionTime_P99_ms", "percentile cols")
	require.Contains(t, body, "Bandwidth_P50_B,Bandwidth_P99_B,Bandwidth_Mean_B", "bandwidth cols")
	require.Contains(t, body, "Healthy,OBFT,5,1.0000", "Healthy/OBFT 100%% success row")
	require.Contains(t, body, "Equivocate_111,OBFT,5,0.0000", "Equivocate_111/OBFT 0%% success")
	// HV1SelectiveDelivery is n/a for QBFT — must emit a row with empty
	// distribution columns rather than skipping.
	require.Contains(t, body, "HV1SelectiveDelivery,QBFT,0,", "QBFT n/a cell still in CSV")
}

// TestRenderBatchMarkdown_SmokeContent — small batch → BatchRun →
// RenderMarkdown. Verifies three summary tables present.
func TestRenderBatchMarkdown_SmokeContent(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" || s.Name == "PrimaryLeaderSilent" {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 2)

	report := ct.RunBatch(t, ct.BatchConfig{
		Iterations: 3,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	})
	br := reporting.NewBatchRun("MD smoke", "Two-scenario markdown smoke", report)

	path := t.TempDir() + "/batch.md"
	require.NoError(t, reporting.RenderBatchMarkdown(br, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	body := string(data)

	require.Contains(t, body, "# MD smoke", "title")
	require.Contains(t, body, "Two-scenario markdown smoke", "description")
	require.Contains(t, body, "**Iterations:** 3 per cell", "iteration header")
	require.Contains(t, body, "## Success rate", "success-rate table")
	require.Contains(t, body, "## Decision-time P99", "P99 table")
	require.Contains(t, body, "## Bandwidth median", "bandwidth table")
	require.Contains(t, body, "| Scenario | OBFT | QBFT |", "matrix header")
	require.Contains(t, body, "| Healthy |", "Healthy row")
	require.Contains(t, body, "100% |", "100%% success cell")
}

// TestNewBatchRun_NotApplicableCell — a cell with Iterations=0 (scenario
// not applicable to protocol) is preserved in BatchRun and surfaces
// Applicable(cell)=false.
func TestNewBatchRun_NotApplicableCell(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "HV1SelectiveDelivery" {
			scenarios = append(scenarios, s)
			break
		}
	}
	require.Len(t, scenarios, 1)

	report := ct.RunBatch(t, ct.BatchConfig{
		Iterations: 3,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	})
	br := reporting.NewBatchRun("NA test", "", report)

	obftCell, _ := br.CellAt("HV1SelectiveDelivery", "OBFT")
	qbftCell, _ := br.CellAt("HV1SelectiveDelivery", "QBFT")
	require.True(t, reporting.Applicable(obftCell), "OBFT-applicable scenario")
	require.False(t, reporting.Applicable(qbftCell), "QBFT-not-applicable scenario")
}

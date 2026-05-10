package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestBatch_Smoke runs a small batch (5 iterations × 3 scenarios × 2
// protocols = 30 sims) at canonical config and verifies the BatchReport
// shape: every (scenario, protocol) cell present, success-rate populated,
// distributions non-empty for cells that should have decisions.
func TestBatch_Smoke(t *testing.T) {
	scenarios := []ct.Scenario{}
	pickedNames := map[string]bool{
		"Healthy":             true,
		"PrimaryLeaderSilent": true,
		"Equivocate_111":      true,
	}
	for _, s := range ct.Catalog {
		if pickedNames[s.Name] {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 3, "test setup: expected 3 picked scenarios")

	cfg := ct.BatchConfig{
		Iterations: 5,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	}

	report := ct.RunBatch(t, cfg)
	require.Len(t, report.Cells, 6, "3 scenarios × 2 protocols = 6 cells")
	require.Greater(t, report.Wallclock, time.Duration(0))

	for _, cell := range report.Cells {
		t.Logf("%s/%s: success=%.2f%% (n=%d) decisionP99=%v bandwidth_total=%v",
			cell.Protocol, cell.Scenario,
			cell.SuccessRate*100, cell.Iterations,
			time.Duration(cell.DecisionTime.Percentile(99))*time.Millisecond,
			cell.ClusterBandwidth.Median())

		require.Equal(t, 5, cell.Iterations, "every cell must run all iterations")
		require.GreaterOrEqual(t, cell.SuccessRate, 0.0)
		require.LessOrEqual(t, cell.SuccessRate, 1.0)
		require.Equal(t, 0, cell.SafetyViolations, "no safety violations expected")
		// ClusterBandwidth is recorded per-sim regardless of decide outcome.
		require.Equal(t, 5, cell.ClusterBandwidth.Len(),
			"ClusterBandwidth should have one sample per iter")
	}
}

// TestBatch_Determinism — same (Iterations, SeedStart, Base, Scenarios,
// Protocols) produces byte-identical BatchCell stats across runs. This is
// the load-bearing guarantee for the framework's reproducibility.
func TestBatch_Determinism(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" || s.Name == "PrimaryLeaderSilent" {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 2)

	cfg := ct.BatchConfig{
		Iterations: 3,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
		// Single-goroutine to make this assertion robust against scheduler
		// reordering effects (cell-level parallelism preserves per-cell
		// determinism but `report.Cells` order could vary in principle —
		// pinning Parallelism=1 keeps the assertion strict).
		Parallelism: 1,
	}

	r1 := ct.RunBatch(t, cfg)
	r2 := ct.RunBatch(t, cfg)

	require.Equal(t, len(r1.Cells), len(r2.Cells))
	for i := range r1.Cells {
		require.Equal(t, r1.Cells[i].Protocol, r2.Cells[i].Protocol,
			"cell %d protocol differs across runs", i)
		require.Equal(t, r1.Cells[i].Scenario, r2.Cells[i].Scenario)
		require.Equal(t, r1.Cells[i].SuccessRate, r2.Cells[i].SuccessRate,
			"cell %d %s/%s SuccessRate differs", i,
			r1.Cells[i].Protocol, r1.Cells[i].Scenario)
		require.Equal(t, []float64(r1.Cells[i].DecisionTime), []float64(r2.Cells[i].DecisionTime),
			"cell %d %s/%s DecisionTime samples differ",
			i, r1.Cells[i].Protocol, r1.Cells[i].Scenario)
		require.Equal(t, []float64(r1.Cells[i].ClusterBandwidth), []float64(r2.Cells[i].ClusterBandwidth))
	}
}

// TestBatch_NotApplicableSkips — scenarios marked OBFT-specific (e.g.,
// HV1SelectiveDelivery) return ErrNotApplicable on QBFT, producing an
// Iterations=0 cell. RunBatch must handle this without t.Fatal.
func TestBatch_NotApplicableSkips(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "HV1SelectiveDelivery" {
			scenarios = append(scenarios, s)
			break
		}
	}
	require.Len(t, scenarios, 1)

	cfg := ct.BatchConfig{
		Iterations: 3,
		SeedStart:  1,
		Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
		Scenarios:  scenarios,
		Protocols:  []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}},
	}
	report := ct.RunBatch(t, cfg)

	var obftCell, qbftCell ct.BatchCell
	for _, c := range report.Cells {
		switch c.Protocol {
		case "OBFT":
			obftCell = c
		case "QBFT":
			qbftCell = c
		}
	}
	require.Equal(t, 3, obftCell.Iterations, "OBFT-applicable scenario must run all iterations")
	require.Equal(t, 0, qbftCell.Iterations, "QBFT-not-applicable scenario must report Iterations=0")
}

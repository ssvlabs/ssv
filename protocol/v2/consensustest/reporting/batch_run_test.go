package reporting_test

import (
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

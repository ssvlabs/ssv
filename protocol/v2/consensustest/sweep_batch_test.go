package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
)

// TestRunSweep_Smoke — small sweep (2 points × 2 scenarios × 2 protocols
// × 3 iterations = 24 sims) verifies the DSL produces a SweepResult with
// one BatchReport per Point in the right order.
func TestRunSweep_Smoke(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" || s.Name == "PrimaryLeaderSilent" {
			scenarios = append(scenarios, s)
		}
	}
	require.Len(t, scenarios, 2)

	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}

	sweep := ct.Sweep{
		Name:        "smoke",
		Description: "two-point sweep over BTT",
		AxisLabel:   "BTT",
		Points: []ct.SweepPoint{
			{
				Label: "BTT=200ms",
				Config: ct.BatchConfig{
					Iterations: 3,
					Base:       ct.DefaultProposerDutyConfig(200 * time.Millisecond),
					Scenarios:  scenarios,
					Protocols:  protocols,
				},
			},
			{
				Label: "BTT=400ms",
				Config: ct.BatchConfig{
					Iterations: 3,
					Base:       ct.DefaultProposerDutyConfig(400 * time.Millisecond),
					Scenarios:  scenarios,
					Protocols:  protocols,
				},
			},
		},
	}

	result := ct.RunSweep(t, sweep)
	require.Equal(t, "smoke", result.Sweep.Name)
	require.Len(t, result.Reports, 2)
	for i, rep := range result.Reports {
		require.Equal(t, 4, len(rep.Cells), "point %d should have 2 scenarios × 2 protocols = 4 cells", i)
	}
}

// TestDefaultSweeps_NamesAndShape — DefaultSweeps returns the seven
// curated sweeps with the documented names and at least one point each.
// Doesn't actually RUN the sweeps (would take minutes); just verifies
// the metadata + point construction.
func TestDefaultSweeps_NamesAndShape(t *testing.T) {
	scenarios := []ct.Scenario{}
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" {
			scenarios = append(scenarios, s)
			break
		}
	}
	require.Len(t, scenarios, 1)

	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	iters := ct.Iterations{Baseline: 10, Unstable: 10}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iters, 4)

	require.Len(t, sweeps, 7, "seven curated sweeps per plan")

	expected := map[string]int{
		"p2p_ideal":             1, // single reference point at σ=0.1 (control)
		"p2p_normal":            1, // single reference point at σ=0.5 (production baseline)
		"p2p_increasing_BTT":    6, // BTT ∈ {100, 200, 400, 600, 800, 1000} ms
		"p2p_heavy_tail":        6, // Sigma ∈ {0.1, 0.3, 0.4, 0.5, 0.6, 0.7}
		"p2p_packet_loss":       5, // LossRate ∈ {0, 0.01, 0.05, 0.10, 0.20}
		"p2p_correlated_delays": 4, // BadLinkProb ∈ {0, 0.05, 0.10, 0.20}
		"p2p_node_slowness":     4, // slow op count ∈ {0, 1, 2, 3}
	}
	for _, sw := range sweeps {
		wantPoints, ok := expected[sw.Name]
		require.Truef(t, ok, "unexpected sweep name %q", sw.Name)
		require.Equalf(t, wantPoints, len(sw.Points), "sweep %s point count", sw.Name)
		require.NotEmpty(t, sw.Description, "sweep %s description", sw.Name)
		delete(expected, sw.Name)
	}
	require.Empty(t, expected, "missing sweep names: %v", expected)
}

// TestDefaultSweeps_EmptyInputs — DefaultSweeps returns nil on empty
// scenarios / protocols / non-positive iter counts (defensive).
func TestDefaultSweeps_EmptyInputs(t *testing.T) {
	protocols := []ct.Protocol{obftadapter.Protocol{}}
	scen := []ct.Scenario{ct.Catalog[0]}
	good := ct.Iterations{Baseline: 10, Unstable: 10}
	require.Nil(t, ct.DefaultSweeps(nil, protocols, good, 4))
	require.Nil(t, ct.DefaultSweeps(scen, nil, good, 4))
	require.Nil(t, ct.DefaultSweeps(scen, protocols, ct.Iterations{Baseline: 0, Unstable: 10}, 4))
	require.Nil(t, ct.DefaultSweeps(scen, protocols, ct.Iterations{Baseline: 10, Unstable: 0}, 4))
	require.Nil(t, ct.DefaultSweeps(scen, protocols, good, 0))
}

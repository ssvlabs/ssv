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

// TestDefaultSweeps_NamesAndShape — DefaultSweeps returns the six
// curated sweeps with the documented names and the expected point
// counts after the K × axis cross-product. Doesn't actually RUN the
// sweeps (would take minutes); just verifies the metadata + point
// construction.
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

	require.Len(t, sweeps, 6, "six curated sweeps per plan")

	// Phase 2 point counts: each non-baseline sweep crosses K ∈ {3, 4}
	// with its existing axis, so previous counts double.
	expected := map[string]int{
		"p2p_baseline":          2 * 5 * 4, // K × BTT × σ
		"p2p_increasing_BTT":    2 * 6,     // K × BTT
		"p2p_heavy_tail":        2 * 6,     // K × σ
		"p2p_packet_loss":       2 * 5,     // K × LossRate
		"p2p_correlated_delays": 2 * 4,     // K × BadLinkProb
		"p2p_node_slowness":     2 * 4,     // K × slow-op count
	}
	for _, sw := range sweeps {
		wantPoints, ok := expected[sw.Name]
		require.Truef(t, ok, "unexpected sweep name %q", sw.Name)
		require.Equalf(t, wantPoints, len(sw.Points), "sweep %s point count", sw.Name)
		require.NotEmpty(t, sw.Description, "sweep %s description", sw.Name)
		// Every point carries a Fields["K"] entry (3 or 4); the UI uses
		// this to filter trend charts by selected K.
		for _, pt := range sw.Points {
			require.NotNil(t, pt.Fields, "sweep %s point %q missing Fields", sw.Name, pt.Label)
			require.Containsf(t, pt.Fields, "K", "sweep %s point %q missing K field", sw.Name, pt.Label)
		}
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

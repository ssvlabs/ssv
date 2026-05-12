package consensustest_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
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

// TestDefaultSweeps_InvalidInputs — DefaultSweeps panics with a specific
// reason on each invalid-input class. Programmer-error inputs (empty
// scenarios / protocols, non-positive iter budgets, non-positive cluster
// size) surface at the call site instead of confusingly returning nil.
func TestDefaultSweeps_InvalidInputs(t *testing.T) {
	protocols := []ct.Protocol{obftadapter.Protocol{}}
	scen := []ct.Scenario{ct.Catalog[0]}
	good := ct.Iterations{Baseline: 10, Unstable: 10}
	require.PanicsWithValue(t, "consensustest: DefaultSweeps called with empty scenarios",
		func() { ct.DefaultSweeps(nil, protocols, good, 4) })
	require.PanicsWithValue(t, "consensustest: DefaultSweeps called with empty protocols",
		func() { ct.DefaultSweeps(scen, nil, good, 4) })
	require.PanicsWithValue(t, "consensustest: DefaultSweeps: Iterations.Baseline must be > 0 (got 0)",
		func() { ct.DefaultSweeps(scen, protocols, ct.Iterations{Baseline: 0, Unstable: 10}, 4) })
	require.PanicsWithValue(t, "consensustest: DefaultSweeps: Iterations.Unstable must be > 0 (got 0)",
		func() { ct.DefaultSweeps(scen, protocols, ct.Iterations{Baseline: 10, Unstable: 0}, 4) })
	require.PanicsWithValue(t, "consensustest: DefaultSweeps: cluster size n must be > 0 (got 0)",
		func() { ct.DefaultSweeps(scen, protocols, good, 0) })
}

// TestPhase2_AllSweepPoints_NoSetupErrors runs one sim of Healthy at
// every Phase-2 sweep point across all three protocols. Guards against
// regressions introduced by the K2 floor lift + K cross-product:
//
//  1. Every sweep point executes without panicking (RunBatch's
//     SafetyPanic fires on any safety-invariant violation, which would
//     fail this test).
//  2. At every p2p_baseline point, *at least one* protocol completes
//     without falling into the "config out of envelope" path. Per-
//     protocol envelope mismatches are valid data (e.g. 2abOBFT's
//     4·BTT phase-2 tax pushes its deepest layer below BFT-min at
//     BTT=500ms; the framework correctly renders that cell as 0%
//     red). What we don't want is a baseline point where *all*
//     protocols fail to set up — that would mean the conditions
//     chart has nothing to render for the user's selection.
//
// Other sweeps (p2p_increasing_BTT etc.) intentionally probe extreme
// operating points where envelope errors are valid data ("here's where
// the protocol stops working at this K"). The framework renders OOE
// cells as 0% red rather than failing — that's expected.
//
// Healthy is the natural smoke scenario: if even the all-honest happy
// path fails to set up at a given (K, BTT, σ), every other scenario
// at that point is bogus too.
func TestPhase2_AllSweepPoints_NoSetupErrors(t *testing.T) {
	var healthy ct.Scenario
	for _, s := range ct.Catalog {
		if s.Name == "Healthy" {
			healthy = s
			break
		}
	}
	require.NotEmpty(t, healthy.Name, "Healthy scenario must exist in Catalog")

	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		twoabadapter.Protocol{},
		qbftadapter.Protocol{},
	}
	iters := ct.Iterations{Baseline: 1, Unstable: 1}
	sweeps := ct.DefaultSweeps([]ct.Scenario{healthy}, protocols, iters, 4)
	require.Len(t, sweeps, 6)

	totalPoints := 0
	for _, sw := range sweeps {
		for ptIdx, pt := range sw.Points {
			cfg := pt.Config
			report := ct.RunBatch(t, cfg)
			require.NotEmptyf(t, report.Cells, "sweep %s pt %d (%q): no cells",
				sw.Name, ptIdx, pt.Label)
			// Envelope check applies only to p2p_baseline: at least one
			// protocol must complete without OOE so the conditions chart
			// has something to render.
			if sw.Name != "p2p_baseline" {
				totalPoints++
				continue
			}
			anyOK := false
			for _, cell := range report.Cells {
				if cell.Iterations == 0 {
					continue // n/a (scenario doesn't apply to this protocol)
				}
				count, ok := cell.MissReasons["config out of envelope"]
				if !ok || count != cell.Iterations {
					anyOK = true
					break
				}
			}
			require.Truef(t, anyOK,
				"sweep %s pt %q: ALL protocols out of envelope at K=%v BTT=%v σ=%v — the UI has nothing to render at this baseline point",
				sw.Name, pt.Label, pt.Fields["K"], pt.Fields["BTT"], pt.Fields["Sigma"])
			totalPoints++
		}
	}
	t.Logf("verified %d sweep points × Healthy at K ∈ {3, 4}", totalPoints)
}

package consensustest_test

import (
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestStress is the stress-tier entry point per the catalog-split plan.
// It runs DefaultSweeps over every catalog scenario opted into
// ModeStress (currently all 29 — see Phase 2) for the three registered
// protocols (OBFT / 2abOBFT / QBFT) and writes a `data.js` file
// consumed by the static UI in `stresstest-report/` (index.html + app.js
// + styles.css, all tracked in git). Refreshing index.html in a browser
// re-renders from the new data.js without rerunning this test.
//
// Only safety-invariant violations fail TestStress; per-scenario
// expectation mismatches are recorded in the report stats but do not
// fail the test. Per-scenario expectation enforcement lives in
// TestCorrectness (single deterministic operating point per scenario).
//
// Gated on the REPORT_DIR env var so default `go test` runs stay quiet.
// Iteration count tunable via ITERATIONS env (default 100).
//
// Usage:
//
//	make stresstest
//
// Or directly:
//
//	REPORT_DIR=./reports ITERATIONS=100 \
//	    go test -timeout 30m -run TestStress \
//	    ./protocol/v2/consensustest/
//
// Estimated runtime at CLUSTER_SIZE=4 and default 100 iterations:
//
//	p2p_ideal              ~8s    (29 scenarios × 3 protocols × 100 sims)
//	p2p_normal             ~8s    (single point at σ=0.5)
//	p2p_increasing_BTT     ~45s   (× 6 BTT values)
//	p2p_heavy_tail         ~45s   (× 6 sigmas)
//	p2p_packet_loss        ~38s   (× 5 loss rates)
//	p2p_correlated_delays  ~30s   (× 4 BadLinkProb values)
//	TOTAL                  ~3 min on a typical dev machine.
//
// At ITERATIONS=1000, scale linearly (~30 min). At ITERATIONS=10000
// (the Makefile default), expect ~90-100 min. Above that, consider
// parallelizing across sweeps (currently sequential — per-batch is
// already parallelized internally).
//
// See docs/CONSENSUSTEST-SPLIT-PLAN.md.
func TestStress(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make stresstest` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	clusterSize := 4
	if v := os.Getenv("CLUSTER_SIZE"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid CLUSTER_SIZE=%q", v)
		require.Greater(t, n, 0, "CLUSTER_SIZE must be > 0")
		clusterSize = n
	}
	iterations := 100
	if v := os.Getenv("ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid ITERATIONS=%q", v)
		require.Greater(t, n, 0, "ITERATIONS must be > 0")
		iterations = n
	}

	// Filter the catalog to ModeStress opt-ins. Currently all 29 (Phase 2
	// audit); the filter is defensive — future scenarios that opt out of
	// stress (e.g. correctness-only behavioral checks) will be excluded
	// from the report without a driver-side change.
	scenarios := ct.ScenariosWithMode(ct.Catalog, ct.ModeStress)
	require.NotEmpty(t, scenarios, "no catalog scenarios opted into ModeStress")
	// QBFT comes in two flavors: the research variant (default Protocol{},
	// RT = 3·PhaseBudget = 6·BTT — apples-to-apples with OBFT's 2·BTT-
	// per-phase budget convention) and the production SSV variant (RT =
	// QBFTRoundTimeout = 2s fixed, matching roundtimer/timer.go). Both
	// share bftStart=0 and PhaseBudget-based post-consensus margin.
	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		twoabadapter.Protocol{},
		qbftadapter.Protocol{},
		qbftadapter.Protocol{VariantName: "QBFT-SSV", UseFixedRT: true},
	}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iterations, clusterSize)
	require.Len(t, sweeps, 6)

	protocolNames := make([]string, len(protocols))
	for i, p := range protocols {
		protocolNames[i] = p.Name()
	}
	totalStart := time.Now()
	results := make([]ct.SweepResult, 0, len(sweeps))
	for _, sw := range sweeps {
		pointLabels := make([]string, len(sw.Points))
		for i, pt := range sw.Points {
			pointLabels[i] = pt.Label
		}
		t.Logf("--- sweep %s: %d sweep points [%s] × %d iterations × %d scenarios × %d protocols [%s]",
			sw.Name, len(sw.Points), strings.Join(pointLabels, ", "),
			iterations, len(scenarios), len(protocols), strings.Join(protocolNames, ", "))
		swStart := time.Now()
		results = append(results, ct.RunSweep(t, sw))
		t.Logf("    %s wallclock: %v", sw.Name, time.Since(swStart))
	}

	require.NoError(t, reporting.WriteReportData(reporting.Comparison{
		Title:       "consensustest comparison — OBFT vs 2abOBFT vs QBFT",
		Description: "Curated sweeps × OBFT/2abOBFT/QBFT × " + strconv.Itoa(iterations) + " iterations per cell.",
		Sweeps:      results,
		Iterations:  iterations,
		Wallclock:   time.Since(totalStart),
	}, dir))

	t.Logf("Report data written: %s/data.js", dir)
	t.Logf("Open: %s/index.html", dir)
	t.Logf("Total wallclock: %v", time.Since(totalStart))
}

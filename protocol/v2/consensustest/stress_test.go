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
)

// TestStress is the stress-tier entry point per the catalog-split plan.
// It runs DefaultSweeps over every catalog scenario opted into
// ModeStress (currently all 29 — see Phase 2) for both protocols and
// writes a `data.js` file consumed by the static UI in
// `stresstest-report/` (index.html + app.js + styles.css, all
// tracked in git). Refreshing index.html in a browser re-renders from
// the new data.js without rerunning this test.
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
// Estimated runtime at default 100 iterations:
//
//	canonical          ~5s    (29 scenarios × 2 protocols × 100 sims)
//	cluster_scaling    ~30s   (× 4 cluster sizes)
//	btt_degradation    ~20s   (× 4 BTT values)
//	heavy_tail         ~20s   (× 4 sigmas)
//	loss               ~20s   (× 4 loss rates)
//	TOTAL              ~95s   on a typical dev machine (jittered network).
//
// At ITERATIONS=1000, scale linearly (~12-15 min). Above that, consider
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
	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iterations)
	require.Len(t, sweeps, 5)

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
		Title:       "consensustest comparison — OBFT vs QBFT",
		Description: "Five curated sweeps × OBFT/QBFT × " + strconv.Itoa(iterations) + " iterations per cell.",
		Sweeps:      results,
		Iterations:  iterations,
		Wallclock:   time.Since(totalStart),
	}, dir))

	t.Logf("Report data written: %s/data.js", dir)
	t.Logf("Open: %s/index.html", dir)
	t.Logf("Total wallclock: %v", time.Since(totalStart))
}

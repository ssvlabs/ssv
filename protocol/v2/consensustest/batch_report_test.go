package consensustest_test

import (
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
)

// TestGenerateBatchReport runs DefaultSweeps over the full catalog with
// both protocols and writes a `data.js` file consumed by the static UI
// in `consensustest-reports/` (index.html + app.js + styles.css, all
// tracked in git). Refreshing index.html in a browser re-renders from
// the new data.js without rerunning this test.
//
// Gated on the REPORT_DIR env var so default `go test` runs stay quiet.
// Iteration count tunable via ITERATIONS env (default 100).
//
// Usage:
//
//	make consensustest-report
//
// Or directly:
//
//	REPORT_DIR=./reports ITERATIONS=100 \
//	    go test -timeout 30m -run TestGenerateBatchReport \
//	    ./protocol/v2/consensustest/
//
// Estimated runtime at default 100 iterations:
//
//	canonical          ~4s    (25 scenarios × 2 protocols × 100 sims)
//	cluster_scaling    ~25s   (× 4 cluster sizes)
//	btt_degradation    ~16s   (× 4 BTT values)
//	heavy_tail         ~16s   (× 4 sigmas)
//	loss               ~16s   (× 4 loss rates)
//	TOTAL              ~75s   on a typical dev machine.
//
// At ITERATIONS=1000, scale linearly (~12-15 min). Above that, consider
// parallelizing across sweeps (currently sequential — per-batch is
// already parallelized internally).
func TestGenerateBatchReport(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make consensustest-report` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	iterations := 100
	if v := os.Getenv("ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid ITERATIONS=%q", v)
		require.Greater(t, n, 0, "ITERATIONS must be > 0")
		iterations = n
	}

	scenarios := ct.Catalog
	protocols := []ct.Protocol{obftadapter.Protocol{}, qbftadapter.Protocol{}}
	sweeps := ct.DefaultSweeps(scenarios, protocols, iterations)
	require.Len(t, sweeps, 5)

	totalStart := time.Now()
	results := make([]ct.SweepResult, 0, len(sweeps))
	for _, sw := range sweeps {
		t.Logf("--- sweep %s (%d points × %d iterations × %d cells)",
			sw.Name, len(sw.Points), iterations, len(scenarios)*len(protocols))
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
		GeneratedAt: time.Now(),
	}, dir))

	t.Logf("Report data written: %s/data.js", dir)
	t.Logf("Open: %s/index.html", dir)
	t.Logf("Total wallclock: %v", time.Since(totalStart))
}

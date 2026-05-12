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
// Iteration count split into two env vars:
//
//   - ITERATIONS_BASELINE_OPERATIONS (default 100) — used for scenarios
//     with Group == "Baseline" (currently just "Healthy"). Higher count
//     keeps the healthy-path CDF tail well-sampled.
//   - ITERATIONS_UNSTABLE_OPERATIONS (default 10) — used for every
//     other scenario. Lower count, since rare-event behaviour reaches
//     non-zero success rates at far fewer samples.
//
// ITERATIONS (legacy single-knob) is honored as a backwards-compatible
// override: if set, it overrides BOTH budgets.
//
// Usage:
//
//	make stresstest
//
// Or directly:
//
//	REPORT_DIR=./reports ITERATIONS_BASELINE_OPERATIONS=100 \
//	    ITERATIONS_UNSTABLE_OPERATIONS=10 \
//	    go test -timeout 30m -run TestStress \
//	    ./protocol/v2/consensustest/
//
// At the default split (100 / 10), expected runtime at CLUSTER_SIZE=4
// is a few minutes on a typical dev machine — most cells are at the
// unstable budget, so wallclock is dominated by sweep count × sweep
// fan-out × Unstable budget, with Baseline contributing a tiny
// constant overhead.
//
// See docs/CONSENSUSTEST-SPLIT-PLAN.md.
func TestStress(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make stresstest` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// CLUSTER_SIZE_N / LAYERS_K semantics:
	//   - unset → "all": iterate every supported size (ClusterSizes) and
	//     every valid K per size (MinK(N)..N).
	//   - set to a single int: scope to that one value.
	// Reruns at different (n, K) merge into the same data.js via
	// WriteReportData's Fields-tuple match; running with both unset is
	// the "fill the whole matrix" mode.
	var clusterSizes []int
	if v := os.Getenv("CLUSTER_SIZE_N"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid CLUSTER_SIZE_N=%q", v)
		require.Greater(t, n, 0, "CLUSTER_SIZE_N must be > 0")
		clusterSizes = []int{n}
	} else {
		clusterSizes = append([]int{}, ct.ClusterSizes...)
	}

	// LAYERS_K env (single int) constrains to that K for every cluster
	// size. Empty LAYERS_K iterates the full MinK(N)..N range per size.
	// Per-(n, K) validation skips combinations where the K is out of the
	// valid range for that size (e.g. LAYERS_K=2 only fits n=4 where
	// MinK(4)=2; logged + skipped for larger sizes).
	var layersKEnv int
	useExplicitK := false
	if v := os.Getenv("LAYERS_K"); v != "" {
		k, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid LAYERS_K=%q", v)
		require.Greater(t, k, 0, "LAYERS_K must be > 0")
		layersKEnv = k
		useExplicitK = true
	}
	type runPair struct{ n, k int }
	var pairs []runPair
	for _, n := range clusterSizes {
		minK, maxK := ct.MinK(n), n
		if useExplicitK {
			if layersKEnv < minK || layersKEnv > maxK {
				t.Logf("LAYERS_K=%d skipped for CLUSTER_SIZE_N=%d (valid range %d..%d)",
					layersKEnv, n, minK, maxK)
				continue
			}
			pairs = append(pairs, runPair{n: n, k: layersKEnv})
		} else {
			for k := minK; k <= maxK; k++ {
				pairs = append(pairs, runPair{n: n, k: k})
			}
		}
	}
	require.NotEmpty(t, pairs, "no valid (n, K) combinations to run; check CLUSTER_SIZE_N / LAYERS_K env")

	// Defaults: 100 baseline / 10 unstable. ITERATIONS (legacy) overrides
	// both for callers that don't care about the split.
	iters := ct.Iterations{Baseline: 100, Unstable: 10}
	if v := os.Getenv("ITERATIONS_BASELINE_OPERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid ITERATIONS_BASELINE_OPERATIONS=%q", v)
		require.Greater(t, n, 0, "ITERATIONS_BASELINE_OPERATIONS must be > 0")
		iters.Baseline = n
	}
	if v := os.Getenv("ITERATIONS_UNSTABLE_OPERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid ITERATIONS_UNSTABLE_OPERATIONS=%q", v)
		require.Greater(t, n, 0, "ITERATIONS_UNSTABLE_OPERATIONS must be > 0")
		iters.Unstable = n
	}
	if v := os.Getenv("ITERATIONS"); v != "" {
		n, err := strconv.Atoi(v)
		require.NoErrorf(t, err, "invalid ITERATIONS=%q", v)
		require.Greater(t, n, 0, "ITERATIONS must be > 0")
		iters.Baseline = n
		iters.Unstable = n
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
	protocolNames := make([]string, len(protocols))
	for i, p := range protocols {
		protocolNames[i] = p.Name()
	}
	totalStart := time.Now()
	t.Logf("=== %d (n, K) operating points to run: %v", len(pairs), pairs)
	for pairIdx, pp := range pairs {
		sweeps := ct.DefaultSweeps(scenarios, protocols, iters, pp.n, pp.k)
		require.NotEmpty(t, sweeps, "DefaultSweeps returned no sweeps for (n=%d, K=%d)", pp.n, pp.k)
		pairStart := time.Now()
		t.Logf("--- [%d/%d] n=%d K=%d", pairIdx+1, len(pairs), pp.n, pp.k)
		results := make([]ct.SweepResult, 0, len(sweeps))
		for _, sw := range sweeps {
			pointLabels := make([]string, len(sw.Points))
			for i, pt := range sw.Points {
				pointLabels[i] = pt.Label
			}
			t.Logf("    sweep %s: %d sweep points [%s] × baseline=%d unstable=%d iterations × %d scenarios × %d protocols [%s]",
				sw.Name, len(sw.Points), strings.Join(pointLabels, ", "),
				iters.Baseline, iters.Unstable, len(scenarios), len(protocols), strings.Join(protocolNames, ", "))
			swStart := time.Now()
			results = append(results, ct.RunSweep(t, sw))
			t.Logf("        %s wallclock: %v", sw.Name, time.Since(swStart))
		}
		// Each (n, K) pair's data merges into data.js — WriteReportData
		// reads the existing file and combines by Fields-tuple, so
		// iterating multiple pairs in one process composes the same way
		// as multiple `make stresstest` invocations would.
		require.NoError(t, reporting.WriteReportData(reporting.Comparison{
			Title:              "consensustest comparison — OBFT vs 2abOBFT vs QBFT",
			Description:        "Curated sweeps × OBFT/2abOBFT/QBFT across diverse network conditions and cluster sizes.",
			Sweeps:             results,
			BaselineIterations: iters.Baseline,
			UnstableIterations: iters.Unstable,
			Wallclock:          time.Since(pairStart),
		}, dir))
		t.Logf("    n=%d K=%d wallclock: %v", pp.n, pp.k, time.Since(pairStart))
	}

	t.Logf("Report data written: %s/data.js", dir)
	t.Logf("Open: %s/index.html", dir)
	t.Logf("Total wallclock: %v", time.Since(totalStart))
}

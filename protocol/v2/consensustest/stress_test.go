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
//   - ITERATIONS_BASELINE_OPERATIONS — used for scenarios with Group ==
//     "Baseline" (currently just "Healthy"). Higher count keeps the
//     healthy-path CDF tail well-sampled. Test-internal fallback when
//     unset: 100. `make stresstest` sets this to 1000 (see Makefile).
//   - ITERATIONS_UNSTABLE_OPERATIONS — used for every other scenario.
//     Lower count, since rare-event behaviour reaches non-zero success
//     rates at far fewer samples. Test-internal fallback when unset:
//     10. `make stresstest` sets this to 100 (see Makefile).
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
// At the test-internal-default split (100 / 10), expected runtime at
// CLUSTER_SIZE_N=4 LAYERS_K=4 is a few minutes on a typical dev machine;
// at the `make stresstest` default (1000 / 100) it's roughly an order
// of magnitude longer. Most cells run at the Unstable budget, so
// wallclock is dominated by sweep count × sweep fan-out × Unstable
// budget, with Baseline contributing a smaller constant overhead.
//
// See docs/CONSENSUSTEST-SPLIT-PLAN.md.
func TestStress(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make stresstest` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// CLUSTER_SIZE_N / LAYERS_K semantics:
	//   - BOTH unset → curated quick default: {(n=4, K=2), (n=4, K=4)}.
	//     The two K values bracket the f=1/n=4 BFT-liveness floor
	//     (MinK(4)=2) and the SSV K=N convention (4), giving useful
	//     coverage in a single fast run.
	//   - Only CLUSTER_SIZE_N set: scope to that n, iterate every valid K
	//     for it (MinK(N)..N).
	//   - Only LAYERS_K set: iterate every supported n (ClusterSizes),
	//     keeping only (n, K) pairs where K is valid for that n.
	//   - Both set: a single (n, K) point.
	// Reruns at different (n, K) merge into the same data.js via
	// WriteReportData's Fields-tuple match.
	type runPair struct{ n, k int }
	var pairs []runPair

	clusterSizeEnv := os.Getenv("CLUSTER_SIZE_N")
	layersKEnvRaw := os.Getenv("LAYERS_K")

	if clusterSizeEnv == "" && layersKEnvRaw == "" {
		// Quick default — small, fast, representative. Iterating the
		// full ClusterSizes × MinK..N matrix at the make-stresstest
		// iteration budget runs for ~hours; this default fits the
		// "PR-time smoke" use case. Set CLUSTER_SIZE_N / LAYERS_K to
		// scope or expand from here.
		pairs = []runPair{{n: 4, k: 2}, {n: 4, k: 4}}
	} else {
		var clusterSizes []int
		if clusterSizeEnv != "" {
			n, err := strconv.Atoi(clusterSizeEnv)
			require.NoErrorf(t, err, "invalid CLUSTER_SIZE_N=%q", clusterSizeEnv)
			require.Greater(t, n, 0, "CLUSTER_SIZE_N must be > 0")
			clusterSizes = []int{n}
		} else {
			clusterSizes = append([]int{}, ct.ClusterSizes...)
		}

		var layersKEnv int
		useExplicitK := false
		if layersKEnvRaw != "" {
			k, err := strconv.Atoi(layersKEnvRaw)
			require.NoErrorf(t, err, "invalid LAYERS_K=%q", layersKEnvRaw)
			require.Greater(t, k, 0, "LAYERS_K must be > 0")
			layersKEnv = k
			useExplicitK = true
		}

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
	// Two flavor axes:
	//   - OBFT and 2abOBFT each ship in a canonical (multiplier=1) form
	//     plus "x2" and "x3" multiplier variants that scale bttEff
	//     internally. Every BTT-derived budget (Δ_2, B_k shallow layers,
	//     FetchAt fetch buffer, and 2ab's TAcceptMax / TVerdictMax
	//     horizons) scales linearly with the multiplier. T_commit lands
	//     earlier as a result (Δ_2 = 2·m·BTT for OBFT-m; Δ_2a + Δ_2b =
	//     4·m·BTT for 2abOBFT-m) at the cost of MEV freshness.
	//     Network propagation still happens at the sweep's actual BTT —
	//     the multiplier models operator-side pessimism only.
	//   - QBFT ships in the research variant (computed RT = 3·PhaseBudget
	//     = 6·bttEff) and the production SSV variant (fixed 2s RT).
	protocols := []ct.Protocol{
		obftadapter.Protocol{},
		obftadapter.Protocol{VariantName: "OBFTx2", BTTMultiplier: 2},
		obftadapter.Protocol{VariantName: "OBFTx3", BTTMultiplier: 3},
		twoabadapter.Protocol{},
		twoabadapter.Protocol{VariantName: "2abOBFTx2", BTTMultiplier: 2},
		twoabadapter.Protocol{VariantName: "2abOBFTx3", BTTMultiplier: 3},
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
		//
		// Wallclock is passed as time.Since(totalStart) — i.e. the
		// CUMULATIVE elapsed time across every pair run so far — so
		// after the final pair the field in data.js reflects the full
		// matrix-run duration. Passing per-pair time would leave data.js
		// showing only the last pair's time (the merge keeps next.Wallclock).
		require.NoError(t, reporting.WriteReportData(reporting.Comparison{
			Title:              "consensustest comparison — OBFT vs 2abOBFT vs QBFT",
			Description:        "Curated sweeps × OBFT/2abOBFT/QBFT across diverse network conditions and cluster sizes.",
			Sweeps:             results,
			BaselineIterations: iters.Baseline,
			UnstableIterations: iters.Unstable,
			Wallclock:          time.Since(totalStart),
		}, dir))
		t.Logf("    n=%d K=%d wallclock: %v (cumulative %v)", pp.n, pp.k, time.Since(pairStart), time.Since(totalStart))
	}

	t.Logf("Report data written: %s/data.js", dir)
	t.Logf("Open: %s/index.html", dir)
	t.Logf("Total wallclock: %v", time.Since(totalStart))
}

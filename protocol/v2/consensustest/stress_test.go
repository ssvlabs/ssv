package consensustest_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	ct "github.com/ssvlabs/ssv/protocol/v2/consensustest"
	obftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/obft"
	psigsadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/psigs"
	qbftadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/qbft"
	"github.com/ssvlabs/ssv/protocol/v2/consensustest/reporting"
	twoabadapter "github.com/ssvlabs/ssv/protocol/v2/consensustest/twoab"
)

// TestStress is the stress-tier entry point per the catalog-split plan.
// It runs DefaultSweeps over every catalog scenario opted into
// ModeStress for the registered protocol families (OBFT family incl.
// OBFT-RD0 / OBFTx2 / OBFTx3, 2abOBFT family incl. 2abOBFTx2 / x3,
// QBFT family incl. QBFT-SSV, and PSigs) and writes a `data.js` file
// consumed by the static UI in `stresstest-report/` (index.html + app.js
// + styles.css, all tracked in git). Refreshing index.html in a browser
// re-renders from the new data.js without rerunning this test.
//
// Only safety-invariant violations fail TestStress. Per-scenario
// expectations (ExpectSuccessFastest, ExpectSlotMiss, etc.) are NOT
// enforced here — the report records raw per-cell aggregates
// (SuccessRate, DecisionTime distribution, MissReasons), not pass/fail
// against the scenario's expectation. Reading the report means
// comparing the aggregate against the scenario's documented behavior
// manually; for automated expectation enforcement use TestCorrectness
// (single deterministic operating point per scenario, where Match is
// computed against the expectation).
//
// Gated on the REPORT_DIR env var so default `go test` runs stay quiet.
// Iteration count split into two env vars:
//
//   - ITERATIONS_BASELINE_OPERATIONS — used for scenarios with Group ==
//     "Baseline" (currently just "Healthy"). Higher count keeps the
//     healthy-path CDF tail well-sampled. Test-internal fallback when
//     unset: 100. `make stresstest` sets this to 10000 (see Makefile).
//   - ITERATIONS_UNSTABLE_OPERATIONS — used for every other scenario.
//     Lower count, since rare-event behaviour reaches non-zero success
//     rates at far fewer samples. Test-internal fallback when unset:
//     10. `make stresstest` sets this to 1 (single-sample probe — see
//     the Makefile target's docstring for the rationale and how to
//     override when adversarial CDFs matter).
//
// Operating-point env vars (defaults listed are the test-internal
// fallbacks for direct `go test` invocation; `make stresstest` sets
// most of these to a more conservative value — see the Makefile target):
//
//   - CLUSTER_SIZES_N — comma-separated cluster sizes ∈ {4, 7}. Default: 4.
//   - LAYERS_K — comma-separated K values ∈ {2, 3, 4}. Default: 2,4
//     (brackets the BFT-liveness floor + SSV's K=N convention at n=4).
//   - P2P_PROFILES — comma-separated calibrated mesh-hop profile names
//     for the p2p_baseline sweep's profile axis. Valid values: prod,
//     stage1, stage2, slow, heavy_tail, slow_heavy_tail. Default: all
//     six. See ct.P2PProfileNames / ct.P2PProfile.
//   - PROTOCOLS — comma-separated protocol names to include in the sweep
//     (e.g. "OBFT,QBFT,PSigs"). Default (unset / empty): all registered
//     protocols. Useful for partial regens — e.g. PROTOCOLS=PSigs runs
//     only the baseline reference. Names must exactly match Protocol.Name()
//     values from the `protocols` slice in this file.
//   - BTT_VALUES_MS — comma-separated BTT values (ms) shared by the
//     p2p_baseline and p2p_increasing_BTT sweeps. Default: 100, 200,
//     300, 400 per ct.DefaultBaselineBTTValues.
//   - BFT_STARTS — comma-separated BFT_start values (ms) for the
//     p2p_baseline sweep's BFT_start axis. Default: 0, 2000, 2400,
//     2800 per ct.DefaultBaselineBFTStarts. BFT_start > 0 points emit
//     OBFT-family cells only; pipeline-shift protocols (PSigs / QBFT)
//     are covered by the BFT_start=0 cell + UI pipeline-shift.
//
// Usage:
//
//	make stresstest
//
// Or directly (test-internal fallbacks — see Makefile for higher
// budgets that the `make stresstest` wrapper sets):
//
//	REPORT_DIR=./reports CLUSTER_SIZES_N=4 LAYERS_K=2 P2P_PROFILES=prod,stage1,stage2 \
//	    ITERATIONS_BASELINE_OPERATIONS=100 ITERATIONS_UNSTABLE_OPERATIONS=10 \
//	    go test -timeout 30m -run TestStress ./protocol/v2/consensustest/
//
// See docs/CONSENSUSTEST-SPLIT-PLAN.md.
func TestStress(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make stresstest` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// CLUSTER_SIZES_N — comma-separated cluster sizes, each ∈ {4, 7}
	// (the two SSV-relevant sizes: f=1 and f=2). Default: 4.
	const validClusterSizesDesc = "{4, 7}"
	validClusterSizes := map[int]bool{4: true, 7: true}
	clusterSizesRaw := os.Getenv("CLUSTER_SIZES_N")
	if clusterSizesRaw == "" {
		clusterSizesRaw = "4"
	}
	var clusterSizes []int
	for _, s := range strings.Split(clusterSizesRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		require.NoErrorf(t, err, "invalid CLUSTER_SIZES_N value %q", s)
		require.Truef(t, validClusterSizes[n], "CLUSTER_SIZES_N value %d not in %s", n, validClusterSizesDesc)
		clusterSizes = append(clusterSizes, n)
	}
	require.NotEmpty(t, clusterSizes, "CLUSTER_SIZES_N is empty after parsing")

	// LAYERS_K — comma-separated K values, each ∈ {2, 3, 4}. Default: 2,4
	// (brackets the BFT-liveness floor and SSV's K=N convention for n=4).
	// A K value is skipped for a given n when K < MinK(n) — below the
	// BFT-liveness floor for that cluster size. For example, K=2 is
	// skipped for n=7 (MinK(7)=3).
	const validLayersKDesc = "{2, 3, 4}"
	validLayersKSet := map[int]bool{2: true, 3: true, 4: true}
	layersKRaw := os.Getenv("LAYERS_K")
	if layersKRaw == "" {
		layersKRaw = "2,4"
	}
	var layersK []int
	for _, s := range strings.Split(layersKRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k, err := strconv.Atoi(s)
		require.NoErrorf(t, err, "invalid LAYERS_K value %q", s)
		require.Truef(t, validLayersKSet[k], "LAYERS_K value %d not in %s", k, validLayersKDesc)
		layersK = append(layersK, k)
	}
	require.NotEmpty(t, layersK, "LAYERS_K is empty after parsing")

	// P2P_PROFILES — comma-separated calibrated mesh-hop profile names
	// for the p2p_baseline sweep's profile axis. Default: all six
	// (prod, stage1, stage2, slow, heavy_tail, slow_heavy_tail) from
	// ct.P2PProfileNames. Each name becomes one point in the
	// BTT × profile × instability cross-product, with both cfg.Network
	// and cfg.Mesh.HopDelay sourced from the named profile.
	p2pProfilesRaw := os.Getenv("P2P_PROFILES")
	if p2pProfilesRaw == "" {
		p2pProfilesRaw = strings.Join(ct.P2PProfileNames, ",")
	}
	var profiles []string
	validProfileNames := make(map[string]bool, len(ct.P2PProfileNames))
	for _, name := range ct.P2PProfileNames {
		validProfileNames[name] = true
	}
	for _, s := range strings.Split(p2pProfilesRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		require.Truef(t, validProfileNames[s], "invalid P2P_PROFILES value %q; valid: %v", s, ct.P2PProfileNames)
		profiles = append(profiles, s)
	}
	require.NotEmpty(t, profiles, "P2P_PROFILES is empty after parsing")

	// BFT_STARTS — comma-separated BFT_start values (ms) for the
	// p2p_baseline sweep's BFT_start axis. Default: 0, 2000, 2400, 2800
	// per DefaultBaselineBFTStarts — covers BFT_start=0 (used by the UI
	// for picker values in [0, 1600]ms via the close-to-ground-truth
	// approximation) plus the {2000, 2400, 2800} values where the
	// OBFT-family broadcast schedule's L_0 clamp begins to bite (at
	// BTT=100ms, T_commit − B_0 ≈ 2700ms). BFT_start > 0 only runs the
	// OBFT-family protocols; pipeline-shift protocols (PSigs / QBFT)
	// are covered by the UI's pipeline-shift from the BFT_start=0 cell.
	// Sorted ascending after parse for stable axis ordering.
	bftStartsRaw := os.Getenv("BFT_STARTS")
	var bftStarts []time.Duration
	if bftStartsRaw == "" {
		bftStarts = ct.DefaultBaselineBFTStarts
	} else {
		for _, s := range strings.Split(bftStartsRaw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			ms, err := strconv.Atoi(s)
			require.NoErrorf(t, err, "invalid BFT_STARTS value %q (want comma-separated ms integers)", s)
			require.GreaterOrEqualf(t, ms, 0, "BFT_STARTS value %q must be >= 0", s)
			bftStarts = append(bftStarts, time.Duration(ms)*time.Millisecond)
		}
		require.NotEmpty(t, bftStarts, "BFT_STARTS is empty after parsing")
		slices.Sort(bftStarts)
	}

	// BTT_VALUES_MS — comma-separated BTT values (ms) shared by the
	// p2p_baseline and p2p_increasing_BTT sweeps. Default: 100, 200,
	// 300, 400 per DefaultBaselineBTTValues. Sorted ascending after
	// parse so p2p_increasing_BTT's axis is monotonic regardless of
	// user input order.
	bttValuesRaw := os.Getenv("BTT_VALUES_MS")
	var bttValues []time.Duration
	if bttValuesRaw == "" {
		bttValues = ct.DefaultBaselineBTTValues
	} else {
		for _, s := range strings.Split(bttValuesRaw, ",") {
			s = strings.TrimSpace(s)
			if s == "" {
				continue
			}
			ms, err := strconv.Atoi(s)
			require.NoErrorf(t, err, "invalid BTT_VALUES_MS value %q (want comma-separated ms integers)", s)
			require.Greaterf(t, ms, 0, "BTT_VALUES_MS value %q must be > 0", s)
			bttValues = append(bttValues, time.Duration(ms)*time.Millisecond)
		}
		require.NotEmpty(t, bttValues, "BTT_VALUES_MS is empty after parsing")
		slices.Sort(bttValues)
	}

	// Build (n, K) pairs: cross-product of clusterSizes × layersK,
	// filtering out pairs where K < MinK(n) (below BFT-liveness floor).
	type runPair struct{ n, k int }
	var pairs []runPair
	for _, n := range clusterSizes {
		for _, k := range layersK {
			minK := ct.MinK(n)
			if k < minK {
				t.Logf("LAYERS_K=%d skipped for CLUSTER_SIZES_N=%d (MinK(%d)=%d)", k, n, n, minK)
				continue
			}
			pairs = append(pairs, runPair{n: n, k: k})
		}
	}
	require.NotEmptyf(t, pairs, "no valid (n, K) pairs after filtering; check CLUSTER_SIZES_N=%s × LAYERS_K=%s against MinK constraints", clusterSizesRaw, layersKRaw)

	// Defaults: 100 baseline / 10 unstable.
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

	// Filter the catalog to ModeStress opt-ins. The filter is defensive —
	// future scenarios that opt out of stress (e.g. correctness-only
	// behavioral checks) will be excluded from the report without a
	// driver-side change. Logged below for visibility per run.
	scenarios := ct.ScenariosWithMode(ct.Catalog, ct.ModeStress)
	require.NotEmpty(t, scenarios, "no catalog scenarios opted into ModeStress")
	t.Logf("=== %d/%d catalog scenarios opted into ModeStress", len(scenarios), len(ct.Catalog))
	// Three flavor axes:
	//   - OBFT and 2abOBFT each ship in a canonical (multiplier=1) form
	//     plus "x2" and "x3" multiplier variants that scale bttEff
	//     internally. Every BTT-derived budget (Δ_2, primary B_0 = 2·BTT,
	//     FetchAt fetch buffer, and 2ab's TAcceptMax / TVerdictMax
	//     horizons) scales linearly with the multiplier; backup layers
	//     L_1..L_{K-1} have B_k = T_commit and don't scale with the
	//     multiplier. T_commit lands earlier as a result (Δ_2 = 1·m·BTT
	//     for OBFT-m at spec-aligned sizing; Δ_2a + Δ_2b = 3·m·BTT for
	//     2abOBFT-m, of which Δ_2a = 2·m·BTT is the structural minimum)
	//     at the cost of MEV freshness. Network propagation still
	//     happens at the sweep's actual BTT — the multiplier models
	//     operator-side pessimism only.
	//   - OBFT ships an additional OBFT-RD0 variant that forces
	//     RefloodDelay=0 in the broadcast budget (B_0 = 2·BTT instead
	//     of 2·BTT + 700ms). Same protocol, no schedule-level cushion
	//     for lazy-push recovery — the per-cell delta against bare
	//     OBFT quantifies the RefloodDelay cushion's value at each
	//     operating point. Listed first in the slice so the report
	//     renders OBFT-RD0 immediately above bare OBFT.
	//   - QBFT ships in the research variant (computed RT = 6·PhaseBudget
	//     = 6·bttEff at tightened per-emission PhaseBudget = 1·bttEff) and
	//     the production SSV variant (fixed 2s RT).
	protocols := []ct.Protocol{
		// OBFT-RD0 is the canonical OBFT with RefloodDelay forced to 0 —
		// models the "fully-meshed cluster, eager push reliable" assumption
		// (OBFT.md §Setting `RefloodDelay=0` path). Listed before bare OBFT
		// so the report shows the no-cushion variant immediately above the
		// with-cushion baseline, making the per-cell RefloodDelay-cost
		// delta read directly. Identical to OBFT on adversarial scenarios
		// (which already set cfg.RefloodDelay=0); the interesting cells
		// are Healthy on a degraded p2p_profile, where bare OBFT's 700ms
		// cushion in B_0 buys the lazy-push absorption that OBFT-RD0 lacks.
		obftadapter.Protocol{VariantName: "OBFT-RD0", NoRefloodDelay: true},
		obftadapter.Protocol{},
		obftadapter.Protocol{VariantName: "OBFTx2", BTTMultiplier: 2},
		obftadapter.Protocol{VariantName: "OBFTx3", BTTMultiplier: 3},
		twoabadapter.Protocol{},
		twoabadapter.Protocol{VariantName: "2abOBFTx2", BTTMultiplier: 2},
		twoabadapter.Protocol{VariantName: "2abOBFTx3", BTTMultiplier: 3},
		qbftadapter.Protocol{},
		qbftadapter.Protocol{VariantName: "QBFT-SSV", UseFixedRT: true},
		// PSigs is a baseline-cost reference: every honest op signs the
		// pre-agreed V at BFTStart and broadcasts; the cluster decides at
		// the qV-th partial-sig arrival. No consensus on V, no rounds,
		// no encrypted onion — most adversarial catalog scenarios return
		// ErrNotApplicable (rendered as n/a in the report). The cell row
		// gives the network-only cost of partial-sig collection, against
		// which OBFT/2abOBFT/QBFT's full consensus overhead is measured.
		psigsadapter.Protocol{},
	}

	// PROTOCOLS — comma-separated allowlist; empty → all. Useful for
	// partial regens (e.g. only PSigs, or only the canonical OBFT/QBFT
	// pair without the multiplier / fixed-RT variants). Each requested
	// name must exactly match a Protocol.Name() in the slice above;
	// typos fail loudly so a partial regen doesn't silently produce a
	// data.js missing the cells the caller expected.
	if raw := os.Getenv("PROTOCOLS"); raw != "" {
		requested := make(map[string]bool)
		for _, name := range strings.Split(raw, ",") {
			name = strings.TrimSpace(name)
			if name == "" {
				continue
			}
			requested[name] = true
		}
		available := make([]string, len(protocols))
		availableSet := make(map[string]bool, len(protocols))
		for i, p := range protocols {
			available[i] = p.Name()
			availableSet[p.Name()] = true
		}
		for name := range requested {
			require.Truef(t, availableSet[name],
				"invalid PROTOCOLS value %q; valid: %v", name, available)
		}
		filtered := make([]ct.Protocol, 0, len(requested))
		for _, p := range protocols {
			if requested[p.Name()] {
				filtered = append(filtered, p)
			}
		}
		protocols = filtered
		require.NotEmptyf(t, protocols, "PROTOCOLS=%q matched no protocols", raw)
	}

	protocolNames := make([]string, len(protocols))
	for i, p := range protocols {
		protocolNames[i] = p.Name()
	}
	totalStart := time.Now()
	t.Logf("=== %d (n, K) operating points to run: %v", len(pairs), pairs)
	for pairIdx, pp := range pairs {
		sweeps := ct.DefaultSweeps(scenarios, protocols, iters, pp.n, pp.k, profiles, bftStarts, bttValues)
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
			Title: "consensustest comparison — OBFT vs 2abOBFT vs QBFT",
			Description: "Curated sweeps × OBFT/2abOBFT/QBFT across diverse network conditions and cluster sizes. " +
				"DecisionTime semantic: \"ready to submit\" for all three protocols — for OBFT/2abOBFT this is " +
				"the earliest local σ-cert in hand; for QBFT it is the earliest receiver to accumulate 2f+1 " +
				"post-consensus partial sigs on the decided value (Phase C of the mesh-transport plan). " +
				"Healthy is the only scenario that runs through the libp2p-shaped mesh transport (per-cluster: " +
				"N protocol peers + N forward-only relay peers, each node at degree 3); every adversarial " +
				"scenario uses direct fanout to keep per-(from, to) byz primitives precise.",
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

	// Final-output smoke. Guards against a regression where the
	// writer silently produces a malformed / empty data.js (e.g. an
	// adapter change accidentally drops a protocol from the matrix,
	// or WriteReportData errors are swallowed somewhere upstream).
	// Cheap (just stats + a small JSON read); fires after a real run.
	dataJS := filepath.Join(dir, "data.js")
	st, err := os.Stat(dataJS)
	require.NoErrorf(t, err, "data.js should exist after WriteReportData runs")
	require.Greaterf(t, st.Size(), int64(1<<10),
		"data.js is suspiciously small (%d bytes); a writer bug may have produced an empty payload", st.Size())
	body, err := os.ReadFile(dataJS)
	require.NoError(t, err)
	const prefix = "window.REPORT_DATA = "
	trimmed := strings.TrimPrefix(string(body), prefix)
	trimmed = strings.TrimSuffix(strings.TrimRight(trimmed, "\n"), ";")
	var parsed map[string]any
	require.NoErrorf(t, json.Unmarshal([]byte(trimmed), &parsed),
		"data.js payload must be valid JSON")
	gotSweeps, _ := parsed["sweeps"].([]any)
	require.NotEmptyf(t, gotSweeps, "data.js should contain at least one sweep")
	gotProtocols, _ := parsed["protocols"].([]any)
	require.GreaterOrEqualf(t, len(gotProtocols), len(protocolNames),
		"data.js protocols (%d) should cover the requested set (%d): %v",
		len(gotProtocols), len(protocolNames), protocolNames)
}

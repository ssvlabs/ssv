package consensustest_test

import (
	"encoding/json"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"
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

// TestStress is the stress-tier entry point.
// It runs DefaultSweeps over every catalog scenario opted into
// ModeStress for the registered protocol families (each consensus family —
// OBFT / 2abOBFT / QBFT — ships the X-0 / X-300 / X-500 / X-700 cushion
// ladder, plus QBFT-SSV and PSigs) and writes a `data.js` file
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
//     (e.g. "OBFT-700,QBFT-700,PSigs"). Test-level default (unset / empty):
//     all registered protocols. `make stresstest` overrides this with a
//     curated default that OMITS PSigs — pass `PROTOCOLS=` to that target
//     for the full set. Useful for partial regens — e.g. PROTOCOLS=PSigs
//     runs only the baseline reference. Names must exactly match
//     Protocol.Name() values from the `protocols` slice in this file.
//   - BTT_VALUES_MS — comma-separated BTT values (ms) shared by the
//     p2p_baseline and p2p_increasing_BTT sweeps. Default: 100, 200,
//     300, 400 per ct.DefaultBaselineBTTValues.
//
// BFT_start is not a sweep axis — the sim always runs at BFT_start=0 and
// the report UI derives later BFT_start values post-hoc.
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
func TestStress(t *testing.T) {
	dir := os.Getenv("REPORT_DIR")
	if dir == "" {
		t.Skip("REPORT_DIR not set; skipping report generation. Run via `make stresstest` to populate.")
	}
	require.NoError(t, os.MkdirAll(dir, 0o755))

	// CLUSTER_SIZES_N — comma-separated cluster sizes, each ∈ {4, 7}
	// (the two SSV-relevant sizes: f=1 and f=2). Default: 4. Duplicates
	// are deduped + sorted ascending so e.g. `CLUSTER_SIZES_N=7,4,4` runs
	// the (n=4, n=7) pair exactly once each, in canonical order.
	const validClusterSizesDesc = "{4, 7}"
	validClusterSizes := map[int]bool{4: true, 7: true}
	clusterSizesRaw := os.Getenv("CLUSTER_SIZES_N")
	if clusterSizesRaw == "" {
		clusterSizesRaw = "4"
	}
	seenClusterSizes := make(map[int]bool)
	var clusterSizes []int
	for _, s := range strings.Split(clusterSizesRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		n, err := strconv.Atoi(s)
		require.NoErrorf(t, err, "invalid CLUSTER_SIZES_N value %q", s)
		require.Truef(t, validClusterSizes[n], "CLUSTER_SIZES_N value %d not in %s", n, validClusterSizesDesc)
		if seenClusterSizes[n] {
			continue
		}
		seenClusterSizes[n] = true
		clusterSizes = append(clusterSizes, n)
	}
	require.NotEmpty(t, clusterSizes, "CLUSTER_SIZES_N is empty after parsing")
	slices.Sort(clusterSizes)

	// LAYERS_K — comma-separated K values, each ∈ {2, 3, 4}. Default: 2,4
	// (brackets the BFT-liveness floor and SSV's K=N convention for n=4).
	// A K value is skipped for a given n when K < MinK(n) — below the
	// BFT-liveness floor for that cluster size. For example, K=2 is
	// skipped for n=7 (MinK(7)=3). Duplicates are deduped + sorted
	// ascending for canonical run order.
	const validLayersKDesc = "{2, 3, 4}"
	validLayersKSet := map[int]bool{2: true, 3: true, 4: true}
	layersKRaw := os.Getenv("LAYERS_K")
	if layersKRaw == "" {
		layersKRaw = "2,4"
	}
	seenLayersK := make(map[int]bool)
	var layersK []int
	for _, s := range strings.Split(layersKRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		k, err := strconv.Atoi(s)
		require.NoErrorf(t, err, "invalid LAYERS_K value %q", s)
		require.Truef(t, validLayersKSet[k], "LAYERS_K value %d not in %s", k, validLayersKDesc)
		if seenLayersK[k] {
			continue
		}
		seenLayersK[k] = true
		layersK = append(layersK, k)
	}
	require.NotEmpty(t, layersK, "LAYERS_K is empty after parsing")
	slices.Sort(layersK)

	// P2P_PROFILES — comma-separated calibrated mesh-hop profile names
	// for the p2p_baseline sweep's profile axis. Default: all six
	// (prod, stage1, stage2, slow, heavy_tail, slow_heavy_tail) from
	// ct.P2PProfileNames. Each name becomes one point in the
	// BTT × profile × instability cross-product, with both cfg.Network
	// and cfg.Mesh.HopDelay sourced from the named profile. Duplicates
	// are dropped, preserving first-occurrence order.
	p2pProfilesRaw := os.Getenv("P2P_PROFILES")
	if p2pProfilesRaw == "" {
		p2pProfilesRaw = strings.Join(ct.P2PProfileNames, ",")
	}
	var profiles []string
	validProfileNames := make(map[string]bool, len(ct.P2PProfileNames))
	for _, name := range ct.P2PProfileNames {
		validProfileNames[name] = true
	}
	seenProfiles := make(map[string]bool)
	for _, s := range strings.Split(p2pProfilesRaw, ",") {
		s = strings.TrimSpace(s)
		if s == "" {
			continue
		}
		require.Truef(t, validProfileNames[s], "invalid P2P_PROFILES value %q; valid: %v", s, ct.P2PProfileNames)
		if seenProfiles[s] {
			continue
		}
		seenProfiles[s] = true
		profiles = append(profiles, s)
	}
	require.NotEmpty(t, profiles, "P2P_PROFILES is empty after parsing")

	// BTT_VALUES_MS — comma-separated BTT values (ms) shared by the
	// p2p_baseline and p2p_increasing_BTT sweeps. Default: 100, 200,
	// 300, 400 per DefaultBaselineBTTValues. Sorted ascending and
	// de-duplicated after parse so p2p_increasing_BTT's axis is monotonic
	// regardless of user input order.
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
		// Drop duplicates (e.g. BTT_VALUES_MS=100,100,200) so the sweeps
		// don't emit redundant identical points; Compact needs the sort above.
		bttValues = slices.Compact(bttValues)
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
	// Variant families in the matrix:
	//   - Cushion ladder per consensus family — OBFT / 2abOBFT / QBFT each
	//     ship rungs X-0 / X-300 / X-500 / X-700, naming the mesh-tail cushion
	//     in ms: SafetyBuffer folded into OBFT's primary B_0; SafetyBuffer for
	//     2abOBFT; the per-round RT cushion for QBFT. Each rung is a FIXED,
	//     scenario-independent cushion (so the names are accurate on every
	//     scenario). See the per-family comments below for the exact mapping
	//     and caveats (notably the uneven QBFT ladder and the 2abOBFT
	//     max(SB,BTT) crossover).
	//   - QBFT-SSV (production fixed 2s RT) and PSigs (partial-sigs baseline)
	//     sit outside the cushion ladders.
	protocols := []ct.Protocol{
		// Within each family, variants are listed least-safe → most-safe (least
		// mesh-tail cushion → most), so the report reads floor → production.
		// OBFT cushion ladder — SafetyBuffer = 0 / 300 / 500 / 700ms (the
		// reflood absorption folded into the primary's B_0 = 2·BTT +
		// SafetyBuffer). Each rung is a FIXED, scenario-independent cushion
		// (SafetyBufferOverride), so the names are accurate on every scenario —
		// including adversarial, where the generic bare OBFT would instead
		// collapse to cfg.SafetyBuffer=0. B_0 is linear in SafetyBuffer, so all
		// four rungs stay distinct at every BTT (unlike the 2abOBFT ladder,
		// whose lower rungs collapse via the max(SB,BTT) crossover).
		obftadapter.Protocol{VariantName: "OBFT-0", SafetyBufferOverride: durPtr(0), BaselineOnly: true},
		obftadapter.Protocol{VariantName: "OBFT-300", SafetyBufferOverride: durPtr(300 * time.Millisecond), BaselineOnly: true},
		obftadapter.Protocol{VariantName: "OBFT-500", SafetyBufferOverride: durPtr(500 * time.Millisecond), BaselineOnly: true},
		obftadapter.Protocol{VariantName: "OBFT-700", SafetyBufferOverride: durPtr(700 * time.Millisecond)},
		// 2abOBFT cushion ladder — SafetyBuffer = 0 / 300 / 500 / 700ms (the
		// post-TPhase2a σ-pool fill window). Fixed per rung (SafetyBufferOverride).
		// Note: the resolve window is max(1·BTT + SafetyBuffer, 2·BTT), so a
		// rung's SafetyBuffer only bites ABOVE the 1·BTT crossover — in the BTT
		// sweep the lower rungs collapse onto each other (e.g. at BTT=300 the
		// 0/300 rungs coincide; at BTT=500, 0/300/500 coincide), distinct only
		// at smaller BTT. Absolute ms, not BTT multiples — intentional. See
		// docs/2abOBFT.md §Timing parameters (SafetyBuffer crossover).
		twoabadapter.Protocol{VariantName: "2abOBFT-0", SafetyBufferOverride: durPtr(0), BaselineOnly: true},
		twoabadapter.Protocol{VariantName: "2abOBFT-300", SafetyBufferOverride: durPtr(300 * time.Millisecond), BaselineOnly: true},
		twoabadapter.Protocol{VariantName: "2abOBFT-500", SafetyBufferOverride: durPtr(500 * time.Millisecond), BaselineOnly: true},
		twoabadapter.Protocol{VariantName: "2abOBFT-700", SafetyBufferOverride: durPtr(700 * time.Millisecond)},
		// QBFT cushion ladder — round-timer cushion = 0 / 300 / 500 / 700ms,
		// plus the fixed-2s production variant. NB uneven: QBFT-0 (the
		// no-reflood floor) is 3·BTT — it drops a structural 1·BTT that the
		// cushioned rungs carry — while QBFT-{300,500,700} are 4·BTT+cushion.
		baselineOnly(qbftadapter.QBFT0{}),   // "QBFT-0" (Baseline-only)
		baselineOnly(qbftadapter.QBFT300{}), // "QBFT-300" (Baseline-only)
		baselineOnly(qbftadapter.QBFT500{}), // "QBFT-500" (Baseline-only)
		qbftadapter.QBFT700{},               // "QBFT-700"
		qbftadapter.QBFTSSV{},
		// PSigs is a baseline-cost reference: every honest op signs the
		// pre-agreed V at slot start and broadcasts; the cluster decides at
		// the qV-th partial-sig arrival. No consensus on V, no rounds,
		// no encrypted onion — most adversarial catalog scenarios return
		// ErrNotApplicable (rendered as n/a in the report). The cell row
		// gives the network-only cost of partial-sig collection, against
		// which OBFT/2abOBFT/QBFT's full consensus overhead is measured.
		psigsadapter.Protocol{},
	}

	// PROTOCOLS — comma-separated allowlist; empty → all. Useful for
	// partial regens (e.g. only PSigs, or only the canonical X-700 rungs
	// without the rest of the cushion ladder). Each requested
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
	// K-independent protocols (QBFT family, PSigs) don't consume K — it's a
	// layer count only OBFT/2abOBFT use — so running them at more than one K
	// for the same cluster size is wasted work. Within this invocation run
	// them at only the smallest K requested per n; the report resolves them
	// from that slice regardless of the K picker (stresstest-report/app.js
	// findPipelineShiftBaselineCell), and higher-K pairs then run just the
	// K-dependent families. The smallest requested K is always simulated, so
	// no data is lost — a standalone higher-K run, or a PROTOCOLS set with no
	// K-dependent members, still produces them (the guard in the loop below).
	minKForN := make(map[int]int)
	for _, pp := range pairs {
		if mk, ok := minKForN[pp.n]; !ok || pp.k < mk {
			minKForN[pp.n] = pp.k
		}
	}
	kDependentProtocols := make([]ct.Protocol, 0, len(protocols))
	for _, p := range protocols {
		if !ct.IsPipelineShiftProtocol(p.Name()) {
			kDependentProtocols = append(kDependentProtocols, p)
		}
	}

	// Build every pair's sweeps up front so the TOTAL sim count is known
	// before the first sim runs — that's what lets the progress bar show a
	// run-wide ETA instead of per-batch guesses. DefaultSweeps only constructs
	// config (no sims), so this pre-pass is cheap.
	type pairWork struct {
		pair          runPair
		sweeps        []ct.Sweep
		protocolNames []string // per-pair set (higher-K pairs drop K-independent protocols)
	}
	work := make([]pairWork, 0, len(pairs))
	totalByProtocol := make(map[string]int64)
	var grandTotal int64
	for _, pp := range pairs {
		// Above the smallest K for this n, drop the K-independent protocols —
		// but only when a K-dependent protocol remains, so a pipeline-shift-only
		// PROTOCOLS set still runs at every K rather than yielding an empty sweep.
		pairProtocols := protocols
		if pp.k > minKForN[pp.n] && len(kDependentProtocols) > 0 {
			pairProtocols = kDependentProtocols
		}
		sweeps := ct.DefaultSweeps(scenarios, pairProtocols, iters, pp.n, pp.k, profiles, bttValues)
		require.NotEmpty(t, sweeps, "DefaultSweeps returned no sweeps for (n=%d, K=%d)", pp.n, pp.k)
		for si := range sweeps {
			for i := range sweeps[si].Points {
				for name, n := range sweeps[si].Points[i].Config.TotalItersByProtocol() {
					totalByProtocol[name] += n
					grandTotal += n
				}
			}
		}
		pairProtocolNames := make([]string, len(pairProtocols))
		for i, p := range pairProtocols {
			pairProtocolNames[i] = p.Name()
		}
		work = append(work, pairWork{pair: pp, sweeps: sweeps, protocolNames: pairProtocolNames})
	}

	// cancel is closed by the interrupt handler below; wired into every batch so
	// RunBatch/RunSweep stop launching new work and return what they've
	// completed (see graceful-interrupt handling at the run loop).
	cancel := make(chan struct{})

	// Wire one shared progress tracker (one bar per protocol) and the cancel
	// channel into every batch. The tracker renders live to the terminal;
	// observability only — neither affects data.js. protocolNames gives the
	// bars a stable display order.
	progress := ct.NewProgressTracker(protocolNames, totalByProtocol)
	for wi := range work {
		for si := range work[wi].sweeps {
			for i := range work[wi].sweeps[si].Points {
				work[wi].sweeps[si].Points[i].Config.Progress = progress
				work[wi].sweeps[si].Points[i].Config.Cancel = cancel
			}
		}
	}
	// Render to the controlling terminal directly: `go test` captures the test
	// binary's stdout/stderr and buffers it until the test returns, so a bar
	// written to os.Stderr wouldn't appear live during the (long) run. /dev/tty
	// bypasses that capture and is a real TTY, so the bar redraws in place.
	// Falls back to stderr when there's no controlling terminal (CI / piped),
	// where the periodic-line mode is the right behavior anyway.
	progressOut := os.Stderr
	if tty, err := os.OpenFile("/dev/tty", os.O_WRONLY, 0); err == nil {
		defer tty.Close()
		progressOut = tty
	}

	// Start the live renderer before the interrupt watcher so the watcher can
	// capture stopProgress and stop the renderer cleanly — clearing the
	// in-place block and parking the cursor below it — BEFORE printing its
	// notice. Printing while the renderer is still doing cursor-relative
	// redraws would corrupt the display. stopProgress is idempotent (sync.Once),
	// so the watcher's call and this deferred call don't conflict.
	stopProgress := progress.StartRenderer(progressOut)
	defer stopProgress()

	// Graceful interrupt: on the first SIGINT/SIGTERM, close `cancel` so the run
	// stops launching work and the loop below saves what's completed so far. A
	// second signal force-quits. `done` unblocks the watcher when the test ends
	// normally so it doesn't leak.
	sigCh := make(chan os.Signal, 2)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-sigCh:
			close(cancel)
			stopProgress() // clear the live block before printing the notice
			fmt.Fprintf(progressOut, "\ninterrupt — stopping after the in-flight batch and saving partial data (Ctrl-C again to force quit)\n")
			select {
			case <-sigCh:
				os.Exit(130)
			case <-done:
			}
		case <-done:
		}
	}()

	totalStart := time.Now()
	t.Logf("=== %d (n, K) operating points to run: %v (%s simulations total)",
		len(pairs), pairs, ct.CommaInt(grandTotal))
	for pairIdx, w := range work {
		pp := w.pair
		sweeps := w.sweeps
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
				iters.Baseline, iters.Unstable, len(scenarios), len(w.protocolNames), strings.Join(w.protocolNames, ", "))
			swStart := time.Now()
			results = append(results, ct.RunSweep(t, sw))
			t.Logf("        %s wallclock: %v", sw.Name, time.Since(swStart))
			if ct.IsCancelled(cancel) {
				break // interrupted: stop launching sweeps; partial results saved below
			}
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
				"post-consensus partial sigs on the decided value. " +
				"Healthy is the only scenario that runs through the libp2p-shaped mesh transport (per-cluster: " +
				"N protocol peers + N forward-only relay peers, each node at degree 3); every adversarial " +
				"scenario uses direct fanout to keep per-(from, to) byz primitives precise.",
			Sweeps:             results,
			BaselineIterations: iters.Baseline,
			UnstableIterations: iters.Unstable,
			Wallclock:          time.Since(totalStart),
		}, dir))
		t.Logf("    n=%d K=%d wallclock: %v (cumulative %v)", pp.n, pp.k, time.Since(pairStart), time.Since(totalStart))
		if ct.IsCancelled(cancel) {
			// Interrupted: the partial results for this pair were just written
			// above (merged into data.js). Exit gracefully — skip the remaining
			// pairs and the full-run smoke check below.
			t.Logf("interrupted: saved partial data through n=%d K=%d to %s/data.js", pp.n, pp.k, dir)
			return
		}
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

// durPtr returns a pointer to a time.Duration — used to populate optional
// duration override fields like Protocol.SafetyBufferOverride.
func durPtr(d time.Duration) *time.Duration { return &d }

// baselineOnlyVariant wraps a Protocol to mark it Baseline-group-only: it
// runs on Healthy but RunBatch renders it n/a on adversarial scenarios (see
// isBaselineOnly in batch.go). Used for the QBFT cushion rungs, whose
// adapters are fixed structs that can't carry a per-instance flag — the
// obft/twoab adapters set their Protocol.BaselineOnly field directly instead.
type baselineOnlyVariant struct{ ct.Protocol }

func (baselineOnlyVariant) IsBaselineOnly() bool { return true }

func baselineOnly(p ct.Protocol) ct.Protocol { return baselineOnlyVariant{p} }

package consensustest

import (
	"fmt"
	"testing"
	"time"
)

// SweepPoint is one axis-point of a Sweep — a labeled BatchConfig that
// will produce one BatchReport when the Sweep runs. Label is shown in
// reports as the axis-tick text (e.g., "n=7", "BTT=400ms",
// "profile=prod").
//
// Fields carries the numeric axis values (N, K, BTT, p2p_profile, …)
// for the point in a machine-readable form. Picker-driven UIs (the
// stresstest-report's N / K / BTT / p2p_profile / p2p_instability /
// BFT_start pickers in particular) consume Fields to drive lookups
// by exact value, without parsing the human-readable Label. Keys are
// the per-axis names — typically capitalized for the cluster-shape
// axes ("N", "K", "Instability", "BFT_start", "Loss", "BadLinkProb",
// "SlowOps") and lowercase for the picker-labeled axes ("BTT" (ms),
// "p2p_profile" (index into ct.P2PProfileNames)) — whichever the
// point varies. Case is load-bearing: the JS lookup matches exactly.
type SweepPoint struct {
	Label  string
	Config BatchConfig
	Fields map[string]float64
}

// Sweep is an ordered series of SweepPoints sharing a theme. Built once,
// run via RunSweep, rendered as a single report with multiple sections —
// one per Point — or as a line/series chart with AxisLabel on the X-axis.
type Sweep struct {
	Name        string
	Title       string   // human-readable section heading; falls back to Name when empty
	Params      []string // optional fixed-config tokens rendered as badges next to the title (e.g. "n=4", "BTT=300ms"); used by single-point sweeps where every config knob is constant
	Description string
	AxisLabel   string // e.g. "Cluster size n", "BTT (ms)", "LogNormal sigma"
	Points      []SweepPoint
}

// DisplayTitle returns the sweep's Title if set, otherwise Name. Used by
// report renderers for human-readable section headings.
func (s Sweep) DisplayTitle() string {
	if s.Title != "" {
		return s.Title
	}
	return s.Name
}

// SweepResult bundles a Sweep with the per-Point BatchReports it produced.
type SweepResult struct {
	Sweep   Sweep
	Reports []BatchReport // same order as Sweep.Points
}

// RunSweep drives every SweepPoint sequentially and returns the per-point
// BatchReports. Sequential (not parallel across points) because each point
// itself parallelizes per-cell via RunBatch — running points in parallel
// would over-subscribe goroutines without speedup.
func RunSweep(t *testing.T, s Sweep) SweepResult {
	t.Helper()
	res := SweepResult{Sweep: s, Reports: make([]BatchReport, len(s.Points))}
	for i, pt := range s.Points {
		res.Reports[i] = RunBatch(t, pt.Config)
	}
	return res
}

// Iterations carries the per-scenario-group iteration counts the stress
// driver applies. Baseline-group scenarios (currently just "Healthy")
// run at the larger Baseline budget; everything else (adversarial /
// rare-event groups) runs at the smaller Unstable budget. Splitting the
// budget keeps the high-confidence "is the happy path healthy?"
// signal sharp without paying the same cost on dozens of low-success-
// rate scenarios where 10 samples is enough to surface non-zero
// behaviour.
//
// The struct is converted to BatchConfig.{Iterations, IterationsByGroup}
// inside each sweep builder via asBatchIterations.
type Iterations struct {
	Baseline int // applied to scenarios with Group == "Baseline"
	Unstable int // applied to every other scenario (default fallback)
}

// asBatchIterations expands `i` into the (fallback, group-overrides)
// fields BatchConfig exposes. Centralizing the conversion keeps each
// sweep builder identical and ensures all sweeps split the budget the
// same way.
func (i Iterations) asBatchIterations() (int, map[string]int) {
	return i.Unstable, map[string]int{"Baseline": i.Baseline}
}

// DefaultBaselineBTTValues — the default BTT axis shared by the
// p2p_baseline and p2p_increasing_BTT sweeps. Driver-overridable via
// BTT_VALUES_MS env var. K and N are passed per-run (one (n, k) combo
// per `make stresstest`); the profile axis is controlled by the
// `profiles` parameter to DefaultSweeps (driven by P2P_PROFILES in the
// stress driver, default = all six entries of P2PProfileNames).
var DefaultBaselineBTTValues = []time.Duration{
	100 * time.Millisecond,
	200 * time.Millisecond,
	300 * time.Millisecond,
	400 * time.Millisecond,
}

// DefaultBaselineBFTStarts — the BFT_start axis the p2p_baseline sweep
// adds for OBFT-family protocols. UI picker values that fall under
// each cell's per-variant approximation boundary (computed in JS via
// obftFamilyApproxBoundaryMs — for bare OBFT at BTT=100ms / Healthy
// RefloodDelay=700ms, the boundary is `T_commit − B_0 ≈ 2800ms`)
// reuse the BFT_start=0 cell as a close-to-ground-truth approximation;
// picker values above the per-cell boundary require a matching
// pre-computed cell or render n/a. Driver-overridable via BFT_STARTS
// env var. PSigs and QBFT are skipped at BFT_start > 0 (the UI's
// pipeline-shift covers them from the BFT_start=0 cell).
var DefaultBaselineBFTStarts = []time.Duration{
	0,
	2000 * time.Millisecond,
	2400 * time.Millisecond,
	2800 * time.Millisecond,
}

// DefaultSweeps returns the curated set of comparison sweeps the stress
// driver runs at a single (n, k) operating point. Mesh-mode Healthy
// uses calibrated LogNormalMixture profiles fitted to production /
// staging SSV gossipsub telemetry; direct-mode uses the same profile
// at n=4 (per-hop ≈ cluster-wide for SSV cluster sizes that fit inside
// a single gossipsub mesh). BTT-axis and synthetic-degradation sweeps
// retain their LogNormal anchors so the parameter axis stays meaningful.
//
//  1. p2p_baseline — BTT × profile × instability × BFT_start cross-product.
//     BTT from the `bttValues` parameter (default {100, 200, 300, 400} ms
//     via BTT_VALUES_MS env var), profile from the `profiles` parameter
//     (default {prod, stage1, stage2, slow, heavy_tail, slow_heavy_tail}
//     via P2P_PROFILES env var), instability ∈ {none, low, moderate,
//     high, extreme}, BFT_start from the `bftStarts` parameter (default
//     {0, 2000, 2400, 2800} ms via BFT_STARTS env var). Level=0 emits
//     the full catalog; Level>0 emits ONLY Baseline-group scenarios.
//     BFT_start > 0 emits only OBFT-family protocols (PSigs / QBFT use
//     UI pipeline-shift from BFT_start=0 cells instead of a per-BFT_start
//     sim).
//  2. p2p_increasing_BTT — same BTT axis as p2p_baseline (via
//     `bttValues`); per-point LogNormal{Median: BTT/2, σ: 0.5}
//     (synthetic BTT-scaling exploration).
//  3. p2p_packet_loss — LossRate ∈ {0, 0.01, 0.05, 0.10, 0.20} at
//     fixed BTT=300ms, BurstFactor=5, σ=0.5.
//  4. p2p_correlated_delays — BadLinkProb ∈ {0, 0.05, 0.10, 0.20};
//     BadLinkMultiplier=3, BurstMessages=20, inner LogNormal σ=0.5.
//  5. p2p_node_slowness — slow op count ∈ {0, 1, 2, 3}; ExtraDelay=3·Network.SlowOpAnchor,
//     PersistP=0.8, inner LogNormal σ=0.5.
//  6. p2p_instability — 5 levels (none/low/moderate/high/extreme);
//     fixed BTT=300ms σ=0.5; Healthy-only "production p2p" curve.
//
// (Note: a prior p2p_heavy_tail sweep — synthetic LogNormal-sigma axis
// at fixed BTT — was removed once the empirical profiles landed. The
// "heavy_tail" profile in p2p_baseline now covers that exploration on
// calibrated data; the synthetic axis added no information beyond what
// p2p_heavy_tail's profile point now expresses.)
//
// All sweeps run at the same (n, k), share the Iterations split, and
// run over the same Scenarios / Protocols matrix; only the per-point
// Network / SimConfig (and the per-sweep axis) differ. To compare
// across cluster sizes or layer counts, re-run the driver with
// different CLUSTER_SIZES_N / LAYERS_K values; WriteReportData merges
// the new (n, k) slice into the existing data.js instead of overwriting.
//
// Every emitted SweepPoint.Fields carries N and K explicitly so points
// are uniquely identified by Fields-tuple across runs — the merge in
// reporting.WriteReportData uses that tuple to dedup / append.
//
// Panics with a specific reason on invalid input (empty scenarios /
// protocols / profiles / bftStarts / bttValues, unknown profile names,
// non-positive iteration budgets, non-positive n or k). These are
// programmer errors — the test driver should always pass valid inputs;
// a panic surfaces the bug at the failure site rather than collapsing
// to a confusing "expected N sweeps got 0" downstream.
func DefaultSweeps(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int, profiles []string, bftStarts, bttValues []time.Duration) []Sweep {
	switch {
	case len(scenarios) == 0:
		panic("consensustest: DefaultSweeps called with empty scenarios")
	case len(protocols) == 0:
		panic("consensustest: DefaultSweeps called with empty protocols")
	case len(profiles) == 0:
		panic("consensustest: DefaultSweeps called with empty profiles")
	case len(bftStarts) == 0:
		panic("consensustest: DefaultSweeps called with empty bftStarts")
	case len(bttValues) == 0:
		panic("consensustest: DefaultSweeps called with empty bttValues")
	case iters.Baseline <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: Iterations.Baseline must be > 0 (got %d)", iters.Baseline))
	case iters.Unstable <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: Iterations.Unstable must be > 0 (got %d)", iters.Unstable))
	case n <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: cluster size n must be > 0 (got %d)", n))
	case k <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: layer count k must be > 0 (got %d)", k))
	}
	// Validate profile names eagerly — a typo in P2P_PROFILES should
	// fail at sweep construction, not silently when a sim panics
	// inside P2PProfile.
	for _, name := range profiles {
		_ = P2PProfileIndex(name)
	}
	for _, bs := range bftStarts {
		if bs < 0 {
			panic(fmt.Sprintf("consensustest: DefaultSweeps: BFT_start %v must be >= 0", bs))
		}
	}
	for _, btt := range bttValues {
		if btt <= 0 {
			panic(fmt.Sprintf("consensustest: DefaultSweeps: BTT %v must be > 0", btt))
		}
	}
	return []Sweep{
		p2pBaselineSweep(scenarios, protocols, iters, n, k, profiles, bftStarts, bttValues),
		p2pIncreasingBTTSweep(scenarios, protocols, iters, n, k, bttValues),
		p2pPacketLossSweep(scenarios, protocols, iters, n, k),
		p2pCorrelatedDelaysSweep(scenarios, protocols, iters, n, k),
		p2pNodeSlownessSweep(scenarios, protocols, iters, n, k),
		p2pInstabilitySweep(scenarios, protocols, iters, n, k),
	}
}

// withClusterSize overrides N + Operators on the supplied SimConfig.
// Other defaults (K, schedules) are derived inside Validate when the
// runner consumes the config, so we don't need to touch them here.
func withClusterSize(cfg SimConfig, n int) SimConfig {
	cfg.N = n
	cfg.Operators = MakeOperators(n)
	return cfg
}

// productionLogNormal returns the production-shaped LogNormal delay
// model (σ=0.5) used as the baseline by every BTT-anchored stress
// sweep (p2p_increasing_BTT, packet-loss / correlated-delay /
// node-slowness wraps). p2p_baseline uses calibrated empirical
// profiles instead (see P2PProfile). Median = BTT/2 mirrors the spec's
// typical-mesh P99 propagation per OBFT.md §Setting (BTT = P99_propagation
// + clock skew δ).
func productionLogNormal(btt time.Duration) LogNormalDelay {
	return LogNormalDelay{Median: btt / 2, Sigma: 0.5}
}

// p2pBaselineSweep enumerates the BTT × profile × instability × BFT_start
// cross-product at (n, k) using the empirically calibrated profile from
// P2PProfile(name). The UI's conditions section picks one point at a
// time via the N/K/BTT/σ/instability/BFT_start pickers; the heatmap
// cell colors derive from whichever point is selected.
//
// Instability variants (Level > 0) emit ONLY Baseline-group scenarios
// (Healthy) — non-Baseline scenarios are instability-invariant by
// construction (the wrap is a no-op for them, and the same seed
// produces the same outcome), so duplicating their runs at every
// level would just waste compute. The UI's per-row cell lookup falls
// back to the Level=0 point for non-Baseline scenarios when the
// picker is elsewhere.
//
// BFT_start > 0 variants emit only OBFT-family cells. PSigs and QBFT
// have wholesale pipeline-shift semantics — one BFT_start=0 sim with a
// post-hoc UI shift is equivalent to running at any BFT_start, so
// duplicating their runs adds cost without information. The UI's
// shiftedCell consults Fields.BFT_start for OBFT-family cells and
// pipeline-shifts the BFT_start=0 cell for the others.
func p2pBaselineSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int, profiles []string, bftStarts, bttValues []time.Duration) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	baselineOnly := filterBaselineScenarios(scenarios)
	// Pre-compute the OBFT-family-only subset once; the per-BFT_start
	// loop below selects which protocol set to emit per iteration.
	// Pipeline-shift protocols (PSigs / QBFT family) only run at
	// BFT_start=0; the UI shifts their decision times post-hoc to
	// model later BFT_start. OBFT-family protocols (slot-anchored
	// broadcast schedules) need a real per-BFT_start simulation and
	// run at every value in bftStarts.
	obftFamily := make([]Protocol, 0, len(protocols))
	for _, p := range protocols {
		if !IsPipelineShiftProtocol(p) {
			obftFamily = append(obftFamily, p)
		}
	}
	// faulty_nodes axis (0..f) crossed with every other axis (full outer
	// product). 0 reproduces the pre-crash cells; >0 crashes that many
	// operators on the Baseline group only (Healthy), drawn per-seed in
	// Validate. Like instability, faulty_nodes>0 emits only Baseline-group
	// scenarios — non-Baseline rows are crash-invariant here and the UI
	// falls back to their faulty_nodes=0 cell.
	faultyNodes := FaultyNodesRange(n)
	pts := make([]SweepPoint, 0, len(bttValues)*len(profiles)*len(InstabilityLevels)*len(bftStarts)*len(faultyNodes))
	for _, bftStart := range bftStarts {
		// Per-BFT_start protocol set: full slice at BFT_start=0;
		// obftFamily-only at BFT_start>0 (skip empty obftFamily to
		// avoid emitting cell-less points).
		pointProtocols := protocols
		if bftStart > 0 {
			pointProtocols = obftFamily
			if len(pointProtocols) == 0 {
				continue
			}
		}
		for _, btt := range bttValues {
			for _, profile := range profiles {
				profileIdx := P2PProfileIndex(profile)
				for _, level := range InstabilityLevels {
					for _, fn := range faultyNodes {
						base := withClusterSize(DefaultProposerDutyConfig(btt), n)
						base.K = k
						base.BFTStart = bftStart
						// Calibrated profile drives BOTH direct and mesh paths.
						// At n=4 the cluster typically fits inside one
						// gossipsub mesh, so per-hop ≈ cluster-wide for real
						// prod; using the same profile for cfg.Network and
						// cfg.Mesh.HopDelay keeps the report's direct and
						// mesh columns anchored to the same empirical data
						// instead of one being synthetic. Fresh per-point
						// instances so stateful wrappers (loss / correlated /
						// markov-slow added by Apply or instability wraps)
						// compose on independent state per sim.
						base.Network = P2PProfile(profile)
						base.Mesh.HopDelay = P2PProfile(profile)
						// At level=0 AND fn=0 we run the full catalog (the
						// wraps are no-ops for non-Baseline anyway, but this
						// is what the heatmap reads for the bulk of
						// scenarios). When either degradation axis is active
						// only the Baseline group (Healthy) reruns under the
						// wrap(s).
						pointScenarios := scenarios
						if level.Level > 0 || fn > 0 {
							pointScenarios = baselineOnly
						}
						pts = append(pts, SweepPoint{
							Label: fmt.Sprintf("n=%d K=%d BTT=%dms profile=%s instab=%s faulty=%d BFT_start=%dms",
								n, k, btt.Milliseconds(), profile, level.Name, fn, bftStart.Milliseconds()),
							Fields: map[string]float64{
								"N":           float64(n),
								"K":           float64(k),
								"BTT":         float64(btt.Milliseconds()),
								"p2p_profile": float64(profileIdx),
								"Instability": float64(level.Level),
								"FaultyNodes": float64(fn),
								"BFT_start":   float64(bftStart.Milliseconds()),
							},
							Config: BatchConfig{
								Iterations:        fallback,
								IterationsByGroup: byGroup,
								Base:              base,
								Scenarios:         wrapAllForFaultyNodes(wrapAllForInstability(pointScenarios, level), fn),
								Protocols:         pointProtocols,
							},
						})
					}
				}
			}
		}
	}
	return Sweep{
		Name:        "p2p_baseline",
		Title:       "Baseline conditions",
		Description: "Calibrated empirical baseline across (n, K, BTT, profile, instability, BFT_start). Profile selects a per-hop latency mixture fitted to real SSV gossipsub telemetry: `prod` / `stage1` / `stage2` are mainnet + staging clusters; `slow`, `heavy_tail`, `slow_heavy_tail` are derived from prod (latency ×80 / outlier frequency ×24 / both). Per-hop ≈ cluster-wide at n=4 (cluster ops typically share a gossipsub mesh), so cfg.Network and cfg.Mesh.HopDelay both use the selected profile; larger-n cells are an extrapolation. Important: under empirical profiles, the BTT axis is a PROTOCOL-BUDGET axis, not a network-speed axis — the network model is the profile (≈ 1-10 ms in prod), while BTT (driver-overridable via BTT_VALUES_MS) drives the protocol's internal timing budgets. Framework-side budgets (spec-aligned, post-tightening): OBFT Δ_2 = 1·BTT (spec recommendation, reflood absorbed by B_0); OBFT primary B_0 = 2·BTT + scenario-set RefloodDelay (Healthy opts into 700ms, matching SSV's libp2p heartbeat; adversarial scenarios keep the default 0); QBFT computed-RT phase budget = 1·BTT (RT = 6×phaseBudget = 6·BTT); 2abOBFT Δ_2a = 2·BTT structural minimum, Δ_2b = 1·BTT spec-aligned (Phase-2 total 3·BTT). The instability axis applies only to Baseline-group scenarios (Healthy); non-Baseline rows show their level=none stats regardless of picker. Each `make stresstest` run contributes one (n, K) slice; reruns compose into the same data.js.",
		AxisLabel:   "", // multi-axis; UI picks one point at a time.
		Points:      pts,
	}
}

func p2pIncreasingBTTSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int, bttValues []time.Duration) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	pts := make([]SweepPoint, 0, len(bttValues))
	for _, btt := range bttValues {
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		base.K = k
		// Median scales with BTT (per-point production tail shape preserved):
		// only the configured BTT budget varies along the axis.
		base.Network = productionLogNormal(btt)
		// Co-set Mesh.HopDelay so mesh-mode Healthy responds to the
		// BTT axis: per-hop median scales identically to cluster-wide
		// median (BTT/3 vs BTT/2), preserving the calibration anchor
		// across the axis.
		base.Mesh.HopDelay = LogNormalDelay{Median: btt / 3, Sigma: 0.5}
		pts = append(pts, SweepPoint{
			Label: fmt.Sprintf("n=%d K=%d BTT=%s", n, k, btt),
			Fields: map[string]float64{
				"N":   float64(n),
				"K":   float64(k),
				"BTT": float64(btt.Milliseconds()),
			},
			Config: BatchConfig{
				Iterations:        fallback,
				IterationsByGroup: byGroup,
				Base:              base,
				Scenarios:         scenarios,
				Protocols:         protocols,
			},
		})
	}
	return Sweep{
		Name:        "p2p_increasing_BTT",
		Title:       "Increasing BTT",
		Params:      []string{"LogNormal σ=0.5"},
		Description: "BTT-degradation envelope under production-shaped tail (σ=0.5). One (n, K) slice per `make stresstest` run; the chart filters by the currently-selected (n, K).",
		AxisLabel:   "BTT",
		Points:      pts,
	}
}

func p2pPacketLossSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	rates := []float64{0, 0.01, 0.05, 0.10, 0.20}
	pts := make([]SweepPoint, 0, len(rates))
	for _, rate := range rates {
		rate := rate
		// Each point gets its OWN scenario list with the loss model
		// injected via Apply — fresh LossyNetwork per sim is required
		// (the Markov state is stateful per-instance; sharing across
		// sims would cross-contaminate).
		scenariosWithLoss := make([]Scenario, len(scenarios))
		for i, s := range scenarios {
			scenariosWithLoss[i] = CloneScenarioWith(s, func(cfg *SimConfig) {
				if rate <= 0 {
					return
				}
				// Compose: wrap whatever Network the inner scenario
				// configured (e.g. PerReceiverDelay for MeshFlakiness)
				// so loss adds ON TOP of the inner model. cfg.Network
				// may be nil if the inner scenario didn't set it; use
				// ConstantDelay{D: BTT} as the equivalent of Validate's
				// default.
				base := cfg.Network
				if base == nil {
					base = ConstantDelay{D: cfg.BTT}
				}
				cfg.Network = NewLossyNetwork(base, rate, 5)
				// Mirror the loss wrap onto Mesh.HopDelay so mesh-mode
				// Healthy responds to the loss axis. Keying is per
				// mesh-endpoint (cluster + relay synthetic IDs), so the
				// fresh LossyNetwork tracks state per mesh edge.
				meshInner := cfg.Mesh.HopDelay
				if meshInner == nil {
					meshInner = LogNormalDelay{Median: cfg.BTT / 3, Sigma: 0.3}
				}
				cfg.Mesh.HopDelay = NewLossyNetwork(meshInner, rate, 5)
			})
		}
		btt := 300 * time.Millisecond
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		base.K = k
		// Production-shaped baseline; loss adds on top.
		base.Network = productionLogNormal(btt)
		pts = append(pts, SweepPoint{
			Label: fmt.Sprintf("n=%d K=%d loss=%.2f", n, k, rate),
			Fields: map[string]float64{
				"N":    float64(n),
				"K":    float64(k),
				"Loss": rate,
			},
			Config: BatchConfig{
				Iterations:        fallback,
				IterationsByGroup: byGroup,
				Base:              base,
				Scenarios:         scenariosWithLoss,
				Protocols:         protocols,
			},
		})
	}
	return Sweep{
		Name:  "p2p_packet_loss",
		Title: "Stochastic loss",
		Params: []string{
			"LossyNetwork",
			"BurstFactor=5",
			"direct: LogNormal{Median: BTT/2, σ: 0.5}",
			"mesh per-hop: LogNormal{Median: BTT/3, σ: 0.3}",
		},
		Description: "Each scenario gets a fresh LossyNetwork instance per sim to preserve determinism. Direct-path inner delay is production-shaped (σ=0.5); mesh per-hop inner delay uses the framework's calibration (σ=0.3) so the convolution over ~2 mesh hops matches direct's cluster-wide envelope. One (n, K) slice per run; the chart filters by the currently-selected (n, K).",
		AxisLabel:   "Loss rate",
		Points:      pts,
	}
}

func p2pCorrelatedDelaysSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	// BadLinkProb axis spans the mainnet-calibrated 5–20% range cited in
	// CorrelatedLinkDelay's docstring (network.go §CALIBRATE), with 0 as
	// the no-correlation control point. Other params held at calibrated
	// mid-range: BadLinkMultiplier=3 (bad link delivers in 3× baseline
	// delay), BurstMessages=20 (~mid of the 10–50 dwell-time range).
	probs := []float64{0, 0.05, 0.10, 0.20}
	pts := make([]SweepPoint, 0, len(probs))
	for _, prob := range probs {
		prob := prob
		// Per-sim CorrelatedLinkDelay (stateful per-pair Markov chains —
		// must construct fresh per sim, just like LossyNetwork).
		//
		// COMPOSITION: wrap whatever Network the inner scenario configured
		// (e.g. PerReceiverDelay for MeshFlakiness, AsymmetricPropagation_*)
		// so correlated-link slowness composes ON TOP of the scenario's
		// own per-receiver model. cfg.Network may be nil if the inner
		// scenario didn't set it; use ConstantDelay{D: BTT} as the
		// equivalent of Validate's default. NOTE that for scenarios with
		// hand-tuned per-receiver delays (MeshFlakiness's exactly-2·BTT
		// flaky receivers), the extra correlated-slowness wrap pushes
		// those receivers off the scenario's calibrated boundary — the
		// resulting cell measures the COMPOUND effect, not the isolated
		// scenario, which is the intended interpretation for this sweep.
		scenariosWithCorr := make([]Scenario, len(scenarios))
		for i, s := range scenarios {
			scenariosWithCorr[i] = CloneScenarioWith(s, func(cfg *SimConfig) {
				if prob <= 0 {
					return
				}
				base := cfg.Network
				if base == nil {
					base = ConstantDelay{D: cfg.BTT}
				}
				cfg.Network = NewCorrelatedLinkDelay(base, prob, 3.0, 20)
				// Mirror onto Mesh.HopDelay so mesh-mode Healthy
				// responds to the BadLinkProb axis; per-edge state
				// keys on mesh-endpoint OperatorIDs (cluster + relay
				// synthetic), so distinct mesh edges track distinct
				// chains.
				meshInner := cfg.Mesh.HopDelay
				if meshInner == nil {
					meshInner = LogNormalDelay{Median: cfg.BTT / 3, Sigma: 0.3}
				}
				cfg.Mesh.HopDelay = NewCorrelatedLinkDelay(meshInner, prob, 3.0, 20)
			})
		}
		btt := 300 * time.Millisecond
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		base.K = k
		base.Network = productionLogNormal(btt)
		pts = append(pts, SweepPoint{
			Label: fmt.Sprintf("n=%d K=%d badProb=%.2f", n, k, prob),
			Fields: map[string]float64{
				"N":           float64(n),
				"K":           float64(k),
				"BadLinkProb": prob,
			},
			Config: BatchConfig{
				Iterations:        fallback,
				IterationsByGroup: byGroup,
				Base:              base,
				Scenarios:         scenariosWithCorr,
				Protocols:         protocols,
			},
		})
	}
	return Sweep{
		Name:  "p2p_correlated_delays",
		Title: "Correlated link delays",
		Params: []string{
			"CorrelatedLinkDelay",
			"mult=3.0",
			"burst=20",
			"direct: LogNormal{Median: BTT/2, σ: 0.5}",
			"mesh per-hop: LogNormal{Median: BTT/3, σ: 0.3}",
		},
		Description: "Per-pair sustained-slow links over a production-shaped baseline. Direct-path inner delay is σ=0.5; mesh per-hop inner delay uses the framework's calibration (σ=0.3). One (n, K) slice per run; the chart filters by the currently-selected (n, K).",
		AxisLabel:   "BadLinkProb",
		Points:      pts,
	}
}

// p2pNodeSlownessSweep varies the number of operators flagged as
// "markov-slow" — each flagged op's link returns ExtraDelay (=
// 3·Network.SlowOpAnchor) for the first message it touches (in either
// direction), and for each subsequent touched message independently
// with probability PersistP (= 0.8). With the sweep's productionLogNormal
// direct baseline (Median=BTT/2 → anchor=BTT), the direct-path tax at
// BTT=300ms is 900ms — matching the old fixed `3·BTT` magnitude. The
// mesh-path fallback (LogNormal{Median: BTT/3}) has anchor=2·BTT/3, so
// the mesh tax is 2·BTT (600ms at BTT=300ms), reflecting the smaller
// per-hop magnitude that model encodes. Models correlated peer-link
// degradation: real-world latency / congestion / GC pauses persist for
// stretches, not toggle per-packet.
//
// Axis = slow-op count ∈ {0, 1, 2, 3}, with k slow ops at op2..op{k+1}
// (leader op1 stays fast). k=0 is the no-degradation baseline; k=1
// crosses into "f slow" territory; k=2 hits the "f+1 slow" boundary at
// n=4; k=3 stresses past the boundary. Wraps a production-shaped
// LogNormal baseline so non-slow ops still see the spec's typical-mesh
// variance.
func p2pNodeSlownessSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	const persistP = 0.8
	counts := []int{0, 1, 2, 3}
	pts := make([]SweepPoint, 0, len(counts))
	for _, slowCount := range counts {
		slowCount := slowCount
		// Each point gets its OWN scenario list with a fresh
		// MarkovianSlownessDelay constructed per Apply call — the
		// per-op state map must NOT be shared across iterations.
		//
		// COMPOSITION: wrap whatever Network the inner scenario configured
		// (e.g. PerReceiverDelay for MeshFlakiness, AsymmetricPropagation_*)
		// so markov slowness composes ON TOP of the scenario's per-receiver
		// model. cfg.Network may be nil if the inner scenario didn't set
		// it; use ConstantDelay{D: BTT} as the equivalent of Validate's
		// default. NOTE that for scenarios with hand-tuned per-receiver
		// delays, the extra slowness wrap distorts the scenario's
		// calibrated boundary — the resulting cell measures the COMPOUND
		// effect (scenario + slowness), not the isolated scenario, which
		// is the intended interpretation for this sweep.
		scenariosWithSlowness := make([]Scenario, len(scenarios))
		for i, s := range scenarios {
			scenariosWithSlowness[i] = CloneScenarioWith(s, func(cfg *SimConfig) {
				if slowCount <= 0 {
					return
				}
				slowOps := make([]OperatorID, 0, slowCount)
				for j := 0; j < slowCount; j++ {
					slowOps = append(slowOps, OperatorID(j+2))
				}
				base := cfg.Network
				if base == nil {
					base = ConstantDelay{D: cfg.BTT}
				}
				// ExtraDelay anchored to the network's SlowOpAnchor (per-
				// impl; see NetworkModel interface comment). At this
				// sweep's hardcoded BTT=300ms with productionLogNormal(btt)
				// baseline (Median=BTT/2=150ms, anchor=2×Median=300ms),
				// 3×anchor = 900ms — matches the old `3 × cfg.BTT`
				// magnitude. Empirical-profile callers (if added later)
				// would see the per-profile anchor.
				cfg.Network = NewMarkovianSlowness(base, slowOps, slowOpExtraDelay(base, 3), persistP)
				// Mirror onto Mesh.HopDelay so mesh-mode Healthy
				// responds to the slow-op axis. SlowOps are cluster
				// OperatorIDs (op2..op{k+1}), which match the cluster
				// endpoint IDs returned by MeshTopology.EndpointFor —
				// so the markov chain keys on cluster-op participation
				// in mesh edges just as it does in the direct path.
				meshInner := cfg.Mesh.HopDelay
				if meshInner == nil {
					meshInner = LogNormalDelay{Median: cfg.BTT / 3, Sigma: 0.3}
				}
				cfg.Mesh.HopDelay = NewMarkovianSlowness(meshInner, slowOps, slowOpExtraDelay(meshInner, 3), persistP)
			})
		}
		btt := 300 * time.Millisecond
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		base.K = k
		base.Network = productionLogNormal(btt)
		pts = append(pts, SweepPoint{
			Label: fmt.Sprintf("n=%d K=%d slowOps=%d", n, k, slowCount),
			Fields: map[string]float64{
				"N":       float64(n),
				"K":       float64(k),
				"SlowOps": float64(slowCount),
			},
			Config: BatchConfig{
				Iterations:        fallback,
				IterationsByGroup: byGroup,
				Base:              base,
				Scenarios:         scenariosWithSlowness,
				Protocols:         protocols,
			},
		})
	}
	return Sweep{
		Name:  "p2p_node_slowness",
		Title: "Correlated node slowness",
		Params: []string{
			"MarkovianSlownessDelay",
			"ExtraDelay=3·Network.SlowOpAnchor",
			"PersistP=0.8",
			"direct: LogNormal{Median: BTT/2, σ: 0.5}",
			"mesh per-hop: LogNormal{Median: BTT/3, σ: 0.3}",
		},
		Description: "Per-op Markov slowness over a production-shaped baseline (two-state chain, P(stay)=0.8 in both states). Direct-path inner delay is σ=0.5; mesh per-hop inner delay uses the framework's calibration (σ=0.3). One (n, K) slice per run; chart filters by selected (n, K).",
		AxisLabel:   "Slow op count",
		Points:      pts,
	}
}

// p2pInstabilitySweep is the dedicated "Healthy under production p2p"
// chart: 5 points along the instability axis (none / low / moderate /
// high / extreme) at fixed (BTT=300ms, σ=0.5), with the Healthy
// scenario wrapped by MarkovianSlowness + LossyNetwork per the level.
// Renders as the rightmost collapsible chart so the user can see how
// the all-honest healthy path degrades as the simulated mesh gets
// worse. Non-Baseline scenarios are skipped entirely — the same
// instability picker on p2p_baseline already drives the heatmap's
// Healthy row across the same axis.
func p2pInstabilitySweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	const btt = 300 * time.Millisecond
	const sigma = 0.5
	baselineOnly := filterBaselineScenarios(scenarios)
	pts := make([]SweepPoint, 0, len(InstabilityLevels))
	for _, level := range InstabilityLevels {
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		base.K = k
		base.Network = LogNormalDelay{Median: btt / 2, Sigma: sigma}
		// Co-set Mesh.HopDelay; the instability wrap in
		// WrapBaselineForInstability layers MarkovianSlowness +
		// LossyNetwork on top of cfg.Mesh.HopDelay too, so mesh-mode
		// Healthy responds to the instability axis.
		base.Mesh.HopDelay = LogNormalDelay{Median: btt / 3, Sigma: sigma}
		pts = append(pts, SweepPoint{
			Label: fmt.Sprintf("n=%d K=%d %s", n, k, level.Name),
			Fields: map[string]float64{
				"N":           float64(n),
				"K":           float64(k),
				"Instability": float64(level.Level),
			},
			Config: BatchConfig{
				Iterations:        fallback,
				IterationsByGroup: byGroup,
				Base:              base,
				Scenarios:         wrapAllForInstability(baselineOnly, level),
				Protocols:         protocols,
			},
		})
	}
	return Sweep{
		Name:        "p2p_instability",
		Title:       "P2P instability (Healthy only)",
		Params:      []string{"BTT=300ms", "LogNormal σ=0.5", "MarkovianSlowness + LossyNetwork on top"},
		Description: "Healthy-path degradation across 5 instability levels (none → low → moderate → high → extreme). Each level layers MarkovianSlowness + LossyNetwork on the production-shaped LogNormal baseline; see InstabilityLevels in instability.go for the per-level params. Same picker in the Conditions section drives Healthy on the main heatmap; this chart visualizes the full curve side-by-side.",
		AxisLabel:   "Instability level",
		Points:      pts,
	}
}

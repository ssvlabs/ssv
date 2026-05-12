package consensustest

import (
	"fmt"
	"testing"
	"time"
)

// SweepPoint is one axis-point of a Sweep — a labeled BatchConfig that
// will produce one BatchReport when the Sweep runs. Label is shown in
// reports as the axis-tick text (e.g., "n=7", "BTT=400ms", "Sigma=0.5").
//
// Fields carries the numeric axis values (K, BTT, Sigma, …) for the
// point in a machine-readable form. Picker-driven UIs (the
// stresstest-report's K/BTT/σ pickers in particular) consume Fields to
// drive lookups by exact value, without parsing the human-readable
// Label. Keys are uppercase per-axis names: "K", "BTT" (ms), "Sigma",
// "Loss", "BadLinkProb", "SlowOps" — whichever the point varies.
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
	Title       string // human-readable section heading; falls back to Name when empty
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

// baselineBTTValues / baselineSigmaValues — the BTT × σ axes that
// p2p_baseline sweeps. K and N are now passed in per-run (one (n, k)
// combo per `make stresstest`); the UI composes data from multiple runs
// and the K / N pickers select the slice.
var (
	baselineBTTValues = []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
	}
	baselineSigmaValues = []float64{0.1, 0.3, 0.5, 0.7, 0.9}
)

// DefaultSweeps returns the curated set of comparison sweeps the stress
// driver runs at a single (n, k) operating point. Every sweep models
// per-message propagation with LogNormalDelay — the production-shaped
// distribution — anchored at Median = BTT/2 (the spec's typical-mesh
// P99 propagation per OBFT.md §Setting). Pure JitteredDelay is no
// longer used.
//
//  1. p2p_baseline — BTT × σ × instability cross-product (5 × 5 × 5 =
//     125 points per run). BTT ∈ {100, 200, 300, 400, 500} ms, σ ∈
//     {0.1, 0.3, 0.5, 0.7, 0.9}, instability ∈ {none, low, moderate,
//     high, extreme}. Level=0 emits the full catalog; Level>0 emits
//     ONLY Baseline-group scenarios (non-Baseline rows are
//     instability-invariant — see p2pBaselineSweep).
//  2. p2p_increasing_BTT — BTT ∈ {100, 200, 400, 600, 800, 1000} ms;
//     per-point LogNormal{Median: BTT/2, σ: 0.5}.
//  3. p2p_heavy_tail — σ ∈ {0.1, 0.3, 0.4, 0.5, 0.6, 0.7, 0.9} (7
//     points); BTT=300ms, Median=BTT/2.
//  4. p2p_packet_loss — LossRate ∈ {0, 0.01, 0.05, 0.10, 0.20} at
//     fixed BTT=300ms, BurstFactor=5, σ=0.5.
//  5. p2p_correlated_delays — BadLinkProb ∈ {0, 0.05, 0.10, 0.20};
//     BadLinkMultiplier=3, BurstMessages=20, inner LogNormal σ=0.5.
//  6. p2p_node_slowness — slow op count ∈ {0, 1, 2, 3}; ExtraDelay=3·BTT,
//     PersistP=0.8, inner LogNormal σ=0.5.
//  7. p2p_instability — 5 levels (none/low/moderate/high/extreme);
//     fixed BTT=300ms σ=0.5; Healthy-only "production p2p" curve.
//
// All sweeps run at the same (n, k), share the Iterations split, and
// run over the same Scenarios / Protocols matrix; only the per-point
// Network / SimConfig (and the per-sweep axis) differ. To compare
// across cluster sizes or layer counts, re-run the driver with
// different CLUSTER_SIZE_N / LAYERS_K values; WriteReportData merges
// the new (n, k) slice into the existing data.js instead of overwriting.
//
// Every emitted SweepPoint.Fields carries N and K explicitly so points
// are uniquely identified by Fields-tuple across runs — the merge in
// reporting.WriteReportData uses that tuple to dedup / append.
//
// Panics with a specific reason on invalid input (empty scenarios /
// protocols, non-positive iteration budgets, non-positive n or k).
// These are programmer errors — the test driver should always pass
// valid inputs; a panic surfaces the bug at the failure site rather
// than collapsing to a confusing "expected N sweeps got 0" downstream.
func DefaultSweeps(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) []Sweep {
	switch {
	case len(scenarios) == 0:
		panic("consensustest: DefaultSweeps called with empty scenarios")
	case len(protocols) == 0:
		panic("consensustest: DefaultSweeps called with empty protocols")
	case iters.Baseline <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: Iterations.Baseline must be > 0 (got %d)", iters.Baseline))
	case iters.Unstable <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: Iterations.Unstable must be > 0 (got %d)", iters.Unstable))
	case n <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: cluster size n must be > 0 (got %d)", n))
	case k <= 0:
		panic(fmt.Sprintf("consensustest: DefaultSweeps: layer count k must be > 0 (got %d)", k))
	}
	return []Sweep{
		p2pBaselineSweep(scenarios, protocols, iters, n, k),
		p2pIncreasingBTTSweep(scenarios, protocols, iters, n, k),
		p2pHeavyTailSweep(scenarios, protocols, iters, n, k),
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
// model (σ=0.5) used as the baseline by every stress sweep except
// p2p_baseline and p2p_heavy_tail (which pick their own σ along the
// axis). Median = BTT/2 mirrors the spec's typical-mesh P99
// propagation per OBFT.md §Setting (BTT = P99_propagation + clock
// skew δ).
func productionLogNormal(btt time.Duration) LogNormalDelay {
	return LogNormalDelay{Median: btt / 2, Sigma: 0.5}
}

// p2pBaselineSweep enumerates the BTT × σ × instability cross-product
// at (n, k) using the production-shaped LogNormal delay model
// {Median: BTT/2, σ: σ}. The UI's conditions section picks one point
// at a time via the N/K/BTT/σ/instability pickers; the heatmap cell
// colors derive from whichever point is selected.
//
// Instability variants (Level > 0) emit ONLY Baseline-group scenarios
// (Healthy) — non-Baseline scenarios are instability-invariant by
// construction (the wrap is a no-op for them, and the same seed
// produces the same outcome), so duplicating their runs at every
// level would just waste compute. The UI's per-row cell lookup falls
// back to the Level=0 point for non-Baseline scenarios when the
// picker is elsewhere.
func p2pBaselineSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	baselineOnly := filterBaselineScenarios(scenarios)
	pts := make([]SweepPoint, 0, len(baselineBTTValues)*len(baselineSigmaValues)*len(InstabilityLevels))
	for _, btt := range baselineBTTValues {
		for _, sigma := range baselineSigmaValues {
			for _, level := range InstabilityLevels {
				base := withClusterSize(DefaultProposerDutyConfig(btt), n)
				base.K = k
				base.Network = LogNormalDelay{Median: btt / 2, Sigma: sigma}
				// At level=0 we run the full catalog (no wrap is a no-op
				// for non-Baseline anyway, but emitting them here is what
				// the heatmap reads for the bulk of scenarios). At
				// level>0 only the Baseline group reruns under the wrap.
				pointScenarios := scenarios
				if level.Level > 0 {
					pointScenarios = baselineOnly
				}
				pts = append(pts, SweepPoint{
					Label: fmt.Sprintf("n=%d K=%d BTT=%dms σ=%.1f instab=%s",
						n, k, btt.Milliseconds(), sigma, level.Name),
					Fields: map[string]float64{
						"N":           float64(n),
						"K":           float64(k),
						"BTT":         float64(btt.Milliseconds()),
						"Sigma":       sigma,
						"Instability": float64(level.Level),
					},
					Config: BatchConfig{
						Iterations:        fallback,
						IterationsByGroup: byGroup,
						Base:              base,
						Scenarios:         wrapAllForInstability(pointScenarios, level),
						Protocols:         protocols,
					},
				})
			}
		}
	}
	return Sweep{
		Name:        "p2p_baseline",
		Title:       "Baseline conditions",
		Description: "Production-shaped LogNormal baseline across (n, K, BTT, σ, instability). The conditions section's pickers select the operating point; heatmap cell colors track the same selection. The instability axis applies only to Baseline-group scenarios (Healthy); non-Baseline rows show their level=none stats regardless of picker. Each `make stresstest` run contributes one (n, K) slice; reruns compose into the same data.js.",
		AxisLabel:   "", // multi-axis; UI picks one point at a time.
		Points:      pts,
	}
}

func p2pIncreasingBTTSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	btts := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		600 * time.Millisecond,
		800 * time.Millisecond,
		1000 * time.Millisecond,
	}
	pts := make([]SweepPoint, 0, len(btts))
	for _, btt := range btts {
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		base.K = k
		// Median scales with BTT (per-point production tail shape preserved):
		// only the configured BTT budget varies along the axis.
		base.Network = productionLogNormal(btt)
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

func p2pHeavyTailSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n, k int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	sigmas := []float64{0.1, 0.3, 0.4, 0.5, 0.6, 0.7, 0.9}
	pts := make([]SweepPoint, 0, len(sigmas))
	for _, sigma := range sigmas {
		base := withClusterSize(DefaultProposerDutyConfig(300*time.Millisecond), n)
		base.K = k
		// Heavy-tail propagation: log-normal centered at BTT/2 (= typical
		// P50 propagation per spec §Setting), with Sigma controlling tail
		// fatness. P99/P50 ratio = exp(Sigma · 2.326): 1.27× / 2.01× /
		// 2.54× / 3.20× / 4.03× / 5.09× at the six sample points.
		base.Network = LogNormalDelay{Median: base.BTT / 2, Sigma: sigma}
		pts = append(pts, SweepPoint{
			Label: fmt.Sprintf("n=%d K=%d Sigma=%.2f", n, k, sigma),
			Fields: map[string]float64{
				"N":     float64(n),
				"K":     float64(k),
				"Sigma": sigma,
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
		Name:        "p2p_heavy_tail",
		Title:       "Heavy-tail propagation",
		Params:      []string{"LogNormalDelay", "BTT=300ms", "Median=BTT/2"},
		Description: "Surfaces P99/P50-ratio effects on OBFT's hard B_k cutoff vs QBFT's round-change tolerance. One (n, K) slice per run; the chart filters by the currently-selected (n, K).",
		AxisLabel:   "LogNormal sigma",
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
			inner := s
			scenariosWithLoss[i] = Scenario{
				Name:  s.Name,
				Title: s.Title,
				Group: s.Group,
				Modes: s.Modes,
				Apply: func(cfg *SimConfig) {
					if inner.Apply != nil {
						inner.Apply(cfg)
					}
					if rate > 0 {
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
					}
				},
				Expect: s.Expect,
				Note:   s.Note,
			}
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
		Name:        "p2p_packet_loss",
		Title:       "Stochastic loss",
		Params:      []string{"LossyNetwork", "LogNormal σ=0.5", "BurstFactor=5"},
		Description: "Each scenario gets a fresh LossyNetwork instance per sim to preserve determinism. Inner delay is production-shaped (σ=0.5). One (n, K) slice per run; the chart filters by the currently-selected (n, K).",
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
		scenariosWithCorr := make([]Scenario, len(scenarios))
		for i, s := range scenarios {
			inner := s
			scenariosWithCorr[i] = Scenario{
				Name:  s.Name,
				Title: s.Title,
				Group: s.Group,
				Modes: s.Modes,
				Apply: func(cfg *SimConfig) {
					if inner.Apply != nil {
						inner.Apply(cfg)
					}
					if prob > 0 {
						base := cfg.Network
						if base == nil {
							base = ConstantDelay{D: cfg.BTT}
						}
						cfg.Network = NewCorrelatedLinkDelay(base, prob, 3.0, 20)
					}
				},
				Expect: s.Expect,
				Note:   s.Note,
			}
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
		Name:        "p2p_correlated_delays",
		Title:       "Correlated link delays",
		Params:      []string{"CorrelatedLinkDelay", "LogNormal σ=0.5", "mult=3.0", "burst=20"},
		Description: "Per-pair sustained-slow links over a production-shaped baseline. One (n, K) slice per run; the chart filters by the currently-selected (n, K).",
		AxisLabel:   "BadLinkProb",
		Points:      pts,
	}
}

// p2pNodeSlownessSweep varies the number of operators flagged as
// "markov-slow" — each flagged op's link returns ExtraDelay (= 3·BTT)
// for the first message it touches (in either direction), and for each
// subsequent touched message independently with probability PersistP
// (= 0.8). Models correlated peer-link degradation: real-world latency /
// congestion / GC pauses persist for stretches, not toggle per-packet.
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
		scenariosWithSlowness := make([]Scenario, len(scenarios))
		for i, s := range scenarios {
			inner := s
			scenariosWithSlowness[i] = Scenario{
				Name:  s.Name,
				Title: s.Title,
				Group: s.Group,
				Modes: s.Modes,
				Apply: func(cfg *SimConfig) {
					if inner.Apply != nil {
						inner.Apply(cfg)
					}
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
					cfg.Network = NewMarkovianSlowness(base, slowOps, 3*cfg.BTT, persistP)
				},
				Expect: s.Expect,
				Note:   s.Note,
			}
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
		Name:        "p2p_node_slowness",
		Title:       "Correlated node slowness",
		Params:      []string{"MarkovianSlownessDelay", "LogNormal σ=0.5", "ExtraDelay=3·BTT", "PersistP=0.8"},
		Description: "Per-op Markov slowness over a production-shaped baseline (two-state chain, P(stay)=0.8 in both states). One (n, K) slice per run; chart filters by selected (n, K).",
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

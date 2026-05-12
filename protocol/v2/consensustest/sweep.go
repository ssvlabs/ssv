package consensustest

import (
	"fmt"
	"strconv"
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

// baselineKValues, baselineBTTValues, baselineSigmaValues — the axes
// that p2p_baseline sweeps as a K × BTT × σ cross-product (2 × 5 × 4 =
// 40 points). Other sweeps cross with baselineKValues only and vary
// their own axis at the production-baseline (BTT=300ms, σ=0.5).
var (
	baselineKValues   = []int{3, 4}
	baselineBTTValues = []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		300 * time.Millisecond,
		400 * time.Millisecond,
		500 * time.Millisecond,
	}
	baselineSigmaValues = []float64{0.1, 0.3, 0.5, 0.7}
)

// DefaultSweeps returns the curated set of comparison sweeps the stress
// driver runs. Every sweep models per-message propagation with
// LogNormalDelay — the production-shaped distribution — anchored at
// Median = BTT/2 (the spec's typical-mesh P99 propagation per
// OBFT.md §Setting). Pure JitteredDelay is no longer used.
//
//  1. p2p_baseline — K × BTT × σ cross-product (2 × 5 × 4 = 40 points).
//     K ∈ {3, 4}, BTT ∈ {100..500} ms, σ ∈ {0.1, 0.3, 0.5, 0.7}.
//     Subsumes the prior p2p_ideal (σ=0.1) and p2p_normal (σ=0.5)
//     single-point sweeps; UI pickers select the operating point.
//  2. p2p_increasing_BTT — BTT ∈ {100, 200, 400, 600, 800, 1000} ms
//     crossed with K ∈ {3, 4}; per-point LogNormal{Median: BTT/2,
//     σ: 0.5} so the relative tail shape stays constant.
//  3. p2p_heavy_tail — σ ∈ {0.1, 0.3, 0.4, 0.5, 0.6, 0.7} crossed with
//     K ∈ {3, 4}; BTT=300ms, Median=BTT/2.
//  4. p2p_packet_loss — LossRate ∈ {0, 0.01, 0.05, 0.10, 0.20} crossed
//     with K ∈ {3, 4} at fixed BTT=300ms, BurstFactor=5, σ=0.5.
//  5. p2p_correlated_delays — BadLinkProb ∈ {0, 0.05, 0.10, 0.20}
//     crossed with K ∈ {3, 4}; BadLinkMultiplier=3, BurstMessages=20,
//     inner LogNormal σ=0.5.
//  6. p2p_node_slowness — slow op count ∈ {0, 1, 2, 3} crossed with K
//     ∈ {3, 4}; ExtraDelay=3·BTT, PersistP=0.8, inner LogNormal σ=0.5.
//
// All sweeps run at the same cluster size n, share the Iterations
// split, and run over the same Scenarios / Protocols matrix; only the
// per-point Network / SimConfig (and K) differ. To compare across
// cluster sizes, re-run the driver with different CLUSTER_SIZE values
// (each run produces its own data.js).
//
// Returns nil if Scenarios or Protocols is empty (defensive — caller
// driver test should always pass non-empty lists).
func DefaultSweeps(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) []Sweep {
	if len(scenarios) == 0 || len(protocols) == 0 || iters.Baseline <= 0 || iters.Unstable <= 0 || n <= 0 {
		return nil
	}
	return []Sweep{
		p2pBaselineSweep(scenarios, protocols, iters, n),
		p2pIncreasingBTTSweep(scenarios, protocols, iters, n),
		p2pHeavyTailSweep(scenarios, protocols, iters, n),
		p2pPacketLossSweep(scenarios, protocols, iters, n),
		p2pCorrelatedDelaysSweep(scenarios, protocols, iters, n),
		p2pNodeSlownessSweep(scenarios, protocols, iters, n),
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

// p2pBaselineSweep enumerates the K × BTT × σ cross-product (40 points
// at the current axis sizes) using the production-shaped LogNormal
// delay model {Median: BTT/2, σ: σ}. The UI's conditions section picks
// one point at a time via the K/BTT/σ pickers; the heatmap cell colors
// derive from whichever point is selected. Replaces the older
// single-point p2p_ideal / p2p_normal pair.
func p2pBaselineSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	pts := make([]SweepPoint, 0, len(baselineKValues)*len(baselineBTTValues)*len(baselineSigmaValues))
	for _, k := range baselineKValues {
		for _, btt := range baselineBTTValues {
			for _, sigma := range baselineSigmaValues {
				base := withClusterSize(DefaultProposerDutyConfig(btt), n)
				base.K = k
				base.Network = LogNormalDelay{Median: btt / 2, Sigma: sigma}
				pts = append(pts, SweepPoint{
					Label: fmt.Sprintf("K=%d BTT=%dms σ=%.1f", k, btt.Milliseconds(), sigma),
					Fields: map[string]float64{
						"K":     float64(k),
						"BTT":   float64(btt.Milliseconds()),
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
		}
	}
	return Sweep{
		Name:        "p2p_baseline",
		Title:       "Baseline conditions",
		Params:      []string{"n=" + strconv.Itoa(n)},
		Description: "Production-shaped LogNormal baseline across (K, BTT, σ) cross-product. The conditions section's pickers select the operating point; heatmap cell colors track the same selection.",
		AxisLabel:   "", // multi-axis; UI picks one point at a time.
		Points:      pts,
	}
}

func p2pIncreasingBTTSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	btts := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		600 * time.Millisecond,
		800 * time.Millisecond,
		1000 * time.Millisecond,
	}
	pts := make([]SweepPoint, 0, len(baselineKValues)*len(btts))
	for _, k := range baselineKValues {
		for _, btt := range btts {
			base := withClusterSize(DefaultProposerDutyConfig(btt), n)
			base.K = k
			// Median scales with BTT (per-point production tail shape preserved):
			// only the configured BTT budget varies along the axis.
			base.Network = productionLogNormal(btt)
			pts = append(pts, SweepPoint{
				Label: fmt.Sprintf("K=%d BTT=%s", k, btt),
				Fields: map[string]float64{
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
	}
	return Sweep{
		Name:        "p2p_increasing_BTT",
		Title:       "Increasing BTT",
		Params:      []string{"n=" + strconv.Itoa(n), "LogNormal σ=0.5"},
		Description: "BTT-degradation envelope under production-shaped tail (σ=0.5). Points cross K ∈ {3, 4}; the chart filters by the currently-selected K.",
		AxisLabel:   "BTT",
		Points:      pts,
	}
}

func p2pHeavyTailSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	sigmas := []float64{0.1, 0.3, 0.4, 0.5, 0.6, 0.7}
	pts := make([]SweepPoint, 0, len(baselineKValues)*len(sigmas))
	for _, k := range baselineKValues {
		for _, sigma := range sigmas {
			base := withClusterSize(DefaultProposerDutyConfig(300*time.Millisecond), n)
			base.K = k
			// Heavy-tail propagation: log-normal centered at BTT/2 (= typical
			// P50 propagation per spec §Setting), with Sigma controlling tail
			// fatness. P99/P50 ratio = exp(Sigma · 2.326): 1.27× / 2.01× /
			// 2.54× / 3.20× / 4.03× / 5.09× at the six sample points.
			base.Network = LogNormalDelay{Median: base.BTT / 2, Sigma: sigma}
			pts = append(pts, SweepPoint{
				Label: fmt.Sprintf("K=%d Sigma=%.2f", k, sigma),
				Fields: map[string]float64{
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
	}
	return Sweep{
		Name:        "p2p_heavy_tail",
		Title:       "Heavy-tail propagation",
		Params:      []string{"LogNormalDelay", "n=" + strconv.Itoa(n), "BTT=300ms", "Median=BTT/2"},
		Description: "Surfaces P99/P50-ratio effects on OBFT's hard B_k cutoff vs QBFT's round-change tolerance. Points cross K ∈ {3, 4}; the chart filters by the currently-selected K.",
		AxisLabel:   "LogNormal sigma",
		Points:      pts,
	}
}

func p2pPacketLossSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	rates := []float64{0, 0.01, 0.05, 0.10, 0.20}
	pts := make([]SweepPoint, 0, len(baselineKValues)*len(rates))
	for _, k := range baselineKValues {
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
				Label: fmt.Sprintf("K=%d loss=%.2f", k, rate),
				Fields: map[string]float64{
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
	}
	return Sweep{
		Name:        "p2p_packet_loss",
		Title:       "Stochastic loss",
		Params:      []string{"LossyNetwork", "LogNormal σ=0.5", "n=" + strconv.Itoa(n), "BurstFactor=5"},
		Description: "Each scenario gets a fresh LossyNetwork instance per sim to preserve determinism. Inner delay is production-shaped (σ=0.5). Points cross K ∈ {3, 4}; the chart filters by the currently-selected K.",
		AxisLabel:   "Loss rate",
		Points:      pts,
	}
}

func p2pCorrelatedDelaysSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	// BadLinkProb axis spans the mainnet-calibrated 5–20% range cited in
	// CorrelatedLinkDelay's docstring (network.go §CALIBRATE), with 0 as
	// the no-correlation control point. Other params held at calibrated
	// mid-range: BadLinkMultiplier=3 (bad link delivers in 3× baseline
	// delay), BurstMessages=20 (~mid of the 10–50 dwell-time range).
	probs := []float64{0, 0.05, 0.10, 0.20}
	pts := make([]SweepPoint, 0, len(baselineKValues)*len(probs))
	for _, k := range baselineKValues {
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
				Label: fmt.Sprintf("K=%d badProb=%.2f", k, prob),
				Fields: map[string]float64{
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
	}
	return Sweep{
		Name:        "p2p_correlated_delays",
		Title:       "Correlated link delays",
		Params:      []string{"CorrelatedLinkDelay", "LogNormal σ=0.5", "n=" + strconv.Itoa(n), "mult=3.0", "burst=20"},
		Description: "Per-pair sustained-slow links over a production-shaped baseline. Points cross K ∈ {3, 4}; the chart filters by the currently-selected K.",
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
func p2pNodeSlownessSweep(scenarios []Scenario, protocols []Protocol, iters Iterations, n int) Sweep {
	fallback, byGroup := iters.asBatchIterations()
	const persistP = 0.8
	counts := []int{0, 1, 2, 3}
	pts := make([]SweepPoint, 0, len(baselineKValues)*len(counts))
	for _, kLayers := range baselineKValues {
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
			base.K = kLayers
			base.Network = productionLogNormal(btt)
			pts = append(pts, SweepPoint{
				Label: fmt.Sprintf("K=%d slowOps=%d", kLayers, slowCount),
				Fields: map[string]float64{
					"K":       float64(kLayers),
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
	}
	return Sweep{
		Name:        "p2p_node_slowness",
		Title:       "Correlated node slowness",
		Params:      []string{"MarkovianSlownessDelay", "LogNormal σ=0.5", "n=" + strconv.Itoa(n), "ExtraDelay=3·BTT", "PersistP=0.8"},
		Description: "Per-op Markov slowness over a production-shaped baseline: 1st message touching a slow op is 100% slow, each subsequent has 80% chance of being slow. Models correlated peer-link degradation. Points cross K ∈ {3, 4}; chart filters by selected K.",
		AxisLabel:   "Slow op count",
		Points:      pts,
	}
}

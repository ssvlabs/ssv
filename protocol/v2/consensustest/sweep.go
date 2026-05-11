package consensustest

import (
	"strconv"
	"testing"
	"time"
)

// SweepPoint is one axis-point of a Sweep — a labeled BatchConfig that
// will produce one BatchReport when the Sweep runs. Label is shown in
// reports as the axis-tick text (e.g., "n=7", "BTT=400ms", "Sigma=0.5").
type SweepPoint struct {
	Label  string
	Config BatchConfig
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

// DefaultSweeps returns the curated set of comparison sweeps the stress
// driver runs. Every sweep models per-message propagation with
// LogNormalDelay — the production-shaped distribution — anchored at
// Median = BTT/2 (the spec's typical-mesh P99 propagation per
// OBFT.md §Setting). Pure JitteredDelay is no longer used.
//
//  1. p2p_ideal — single point at BTT=300ms with LogNormal σ=0.1
//     (effectively constant; low-noise control baseline).
//  2. p2p_normal — single point at BTT=300ms with LogNormal σ=0.5
//     (production-shaped baseline; P99/P50 ≈ 3.2× — matches the
//     mainnet floor cited in network.go's LogNormalDelay docstring).
//  3. p2p_increasing_BTT — varies BTT ∈ {100, 200, 400, 600, 800, 1000} ms
//     at fixed n; per-point LogNormal{Median: BTT/2, σ: 0.5} so the
//     relative tail shape stays constant and only the configured BTT
//     budget varies. Probes the protocol's BTT-sizing envelope.
//  4. p2p_heavy_tail — varies LogNormal σ ∈ {0.1, 0.3, 0.4, 0.5, 0.6,
//     0.7} at fixed BTT=300ms, Median=BTT/2. Surfaces P99/P50-ratio
//     effects on OBFT's hard B_k cutoff vs QBFT's round-change
//     tolerance.
//  5. p2p_packet_loss — varies LossyNetwork LossRate ∈ {0, 0.01, 0.05,
//     0.10, 0.20} at fixed n, BurstFactor=5. Inner is LogNormal
//     {Median: BTT/2, σ: 0.5} so loss is measured against the same
//     production baseline as p2p_normal.
//  6. p2p_correlated_delays — varies CorrelatedLinkDelay BadLinkProb ∈
//     {0, 0.05, 0.10, 0.20} at fixed BadLinkMultiplier=3, BurstMessages
//     =20, inner LogNormal{Median: BTT/2, σ: 0.5}. Probes per-pair
//     sustained-slow link behaviour that pure iid LogNormal misses.
//
// All sweeps run at the same cluster size n, share Iterations, and run
// over the same Scenarios / Protocols matrix; only the per-point
// Network / SimConfig differs. To compare across cluster sizes, re-run
// the driver with different CLUSTER_SIZE values (each run produces its
// own data.js).
//
// Returns nil if Scenarios or Protocols is empty (defensive — caller
// driver test should always pass non-empty lists).
func DefaultSweeps(scenarios []Scenario, protocols []Protocol, iterations int, n int) []Sweep {
	if len(scenarios) == 0 || len(protocols) == 0 || iterations <= 0 || n <= 0 {
		return nil
	}
	return []Sweep{
		p2pIdealSweep(scenarios, protocols, iterations, n),
		p2pNormalSweep(scenarios, protocols, iterations, n),
		p2pIncreasingBTTSweep(scenarios, protocols, iterations, n),
		p2pHeavyTailSweep(scenarios, protocols, iterations, n),
		p2pPacketLossSweep(scenarios, protocols, iterations, n),
		p2pCorrelatedDelaysSweep(scenarios, protocols, iterations, n),
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
// model used as the baseline for every stress sweep except p2p_ideal
// and p2p_heavy_tail (which pick their own σ along the axis).
// Median = BTT/2 mirrors the spec's typical-mesh P99 propagation per
// OBFT.md §Setting (BTT = P99_propagation + clock skew δ).
func productionLogNormal(btt time.Duration) LogNormalDelay {
	return LogNormalDelay{Median: btt / 2, Sigma: 0.5}
}

func p2pIdealSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	btt := 300 * time.Millisecond
	base := withClusterSize(DefaultProposerDutyConfig(btt), n)
	// σ=0.1 makes the LogNormal effectively constant — keeps this baseline
	// as a low-noise control so deviations in other sweeps are attributable
	// to their axis variable, not to baseline jitter.
	base.Network = LogNormalDelay{Median: btt / 2, Sigma: 0.1}
	nStr := strconv.Itoa(n)
	return Sweep{
		Name:        "p2p_ideal",
		Title:       "Ideal conditions",
		Params:      []string{"n=" + nStr, "BTT=300ms", "LogNormal σ=0.1"},
		Description: "Low-noise control baseline (LogNormal σ=0.1 ≈ constant). Every other sweep is read relative to this.",
		AxisLabel:   "",
		Points: []SweepPoint{
			{
				Label: "n=" + nStr + " BTT=300ms σ=0.1",
				Config: BatchConfig{
					Iterations: iterations,
					Base:       base,
					Scenarios:  scenarios,
					Protocols:  protocols,
				},
			},
		},
	}
}

func p2pNormalSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	btt := 300 * time.Millisecond
	base := withClusterSize(DefaultProposerDutyConfig(btt), n)
	base.Network = productionLogNormal(btt)
	nStr := strconv.Itoa(n)
	return Sweep{
		Name:        "p2p_normal",
		Title:       "Normal conditions",
		Params:      []string{"n=" + nStr, "BTT=300ms", "LogNormal σ=0.5"},
		Description: "Production-shaped baseline (LogNormal σ=0.5; P99/P50 ≈ 3.2× — mainnet floor). Heatmap colors derive from this sweep.",
		AxisLabel:   "",
		Points: []SweepPoint{
			{
				Label: "n=" + nStr + " BTT=300ms σ=0.5",
				Config: BatchConfig{
					Iterations: iterations,
					Base:       base,
					Scenarios:  scenarios,
					Protocols:  protocols,
				},
			},
		},
	}
}

func p2pIncreasingBTTSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
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
		// Median scales with BTT (per-point production tail shape preserved):
		// only the configured BTT budget varies along the axis.
		base.Network = productionLogNormal(btt)
		pts = append(pts, SweepPoint{
			Label: "BTT=" + btt.String(),
			Config: BatchConfig{
				Iterations: iterations,
				Base:       base,
				Scenarios:  scenarios,
				Protocols:  protocols,
			},
		})
	}
	return Sweep{
		Name:        "p2p_increasing_BTT",
		Title:       "Increasing BTT",
		Params:      []string{"n=" + strconv.Itoa(n), "LogNormal σ=0.5"},
		Description: "BTT-degradation envelope under production-shaped tail (σ=0.5). Reveals where each protocol's window math falls apart.",
		AxisLabel:   "BTT",
		Points:      pts,
	}
}

func p2pHeavyTailSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	sigmas := []float64{0.1, 0.3, 0.4, 0.5, 0.6, 0.7}
	pts := make([]SweepPoint, 0, len(sigmas))
	for _, sigma := range sigmas {
		base := withClusterSize(DefaultProposerDutyConfig(300*time.Millisecond), n)
		// Heavy-tail propagation: log-normal centered at BTT/2 (= typical
		// P50 propagation per spec §Setting), with Sigma controlling tail
		// fatness. P99/P50 ratio = exp(Sigma · 2.326): 1.27× / 2.01× /
		// 2.54× / 3.20× / 4.03× / 5.09× at the six sample points.
		base.Network = LogNormalDelay{Median: base.BTT / 2, Sigma: sigma}
		pts = append(pts, SweepPoint{
			Label: "Sigma=" + strconv.FormatFloat(sigma, 'f', 2, 64),
			Config: BatchConfig{
				Iterations: iterations,
				Base:       base,
				Scenarios:  scenarios,
				Protocols:  protocols,
			},
		})
	}
	return Sweep{
		Name:        "p2p_heavy_tail",
		Title:       "Heavy-tail propagation",
		Params:      []string{"LogNormalDelay", "n=" + strconv.Itoa(n), "BTT=300ms", "Median=BTT/2"},
		Description: "Surfaces P99/P50-ratio effects on OBFT's hard B_k cutoff vs QBFT's round-change tolerance.",
		AxisLabel:   "LogNormal sigma",
		Points:      pts,
	}
}

func p2pPacketLossSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
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
		// Production-shaped baseline (matches p2p_normal); loss adds on top.
		base.Network = productionLogNormal(btt)
		pts = append(pts, SweepPoint{
			Label: "loss=" + strconv.FormatFloat(rate, 'f', 2, 64),
			Config: BatchConfig{
				Iterations: iterations,
				Base:       base,
				Scenarios:  scenariosWithLoss,
				Protocols:  protocols,
			},
		})
	}
	return Sweep{
		Name:        "p2p_packet_loss",
		Title:       "Stochastic loss",
		Params:      []string{"LossyNetwork", "LogNormal σ=0.5", "n=" + strconv.Itoa(n), "BurstFactor=5"},
		Description: "Each scenario gets a fresh LossyNetwork instance per sim to preserve determinism. Inner delay is production-shaped (σ=0.5).",
		AxisLabel:   "Loss rate",
		Points:      pts,
	}
}

func p2pCorrelatedDelaysSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
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
		base.Network = productionLogNormal(btt)
		pts = append(pts, SweepPoint{
			Label: "badProb=" + strconv.FormatFloat(prob, 'f', 2, 64),
			Config: BatchConfig{
				Iterations: iterations,
				Base:       base,
				Scenarios:  scenariosWithCorr,
				Protocols:  protocols,
			},
		})
	}
	return Sweep{
		Name:        "p2p_correlated_delays",
		Title:       "Correlated link delays",
		Params:      []string{"CorrelatedLinkDelay", "LogNormal σ=0.5", "n=" + strconv.Itoa(n), "mult=3.0", "burst=20"},
		Description: "Per-pair sustained-slow links over a production-shaped baseline. Captures the correlation iid LogNormal misses.",
		AxisLabel:   "BadLinkProb",
		Points:      pts,
	}
}

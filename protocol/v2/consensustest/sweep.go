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
	Params      []string // optional fixed-config tokens rendered as badges next to the title (e.g. "n=4", "BTT=200ms"); used by single-point sweeps where every config knob is constant
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
// driver runs:
//
//  1. Canonical — single point at the spec's reference config
//     (BTT=200ms, JitteredDelay) at cluster size n. The baseline.
//  2. Network degradation — varies BTT ∈ {100ms, 200ms, 400ms, 600ms}
//     at fixed n under JitteredDelay (per-BTT scaled jitter). Reveals
//     envelope-fit curves.
//  3. Heavy-tail propagation — varies LogNormalDelay Sigma ∈
//     {0.1, 0.3, 0.5, 0.7} at fixed n, Median=BTT/2. Surfaces
//     P99/P50-ratio effects on OBFT's hard B_k cutoff. (Delay
//     distribution itself is the variable being studied, so this is
//     the one sweep that does not layer JitteredDelay underneath.)
//  4. Loss — varies LossyNetwork LossRate ∈ {0, 0.01, 0.05, 0.10}
//     at fixed n, BurstFactor=5. Inner is JitteredDelay so loss is
//     measured against the same realistic baseline as the other sweeps.
//
// All sweeps run at the same cluster size n, share Iterations, and run
// over the same Scenarios / Protocols matrix; only the per-point
// Network / SimConfig differs. To compare across cluster sizes, re-run
// the driver with different CLUSTER_SIZE values (each run produces its
// own data.js).
//
// Stress-tier network choice: JitteredDelay with ±25% jitter around BTT
// is the universal stress baseline for every sweep except heavy-tail
// (which varies the delay distribution by design). Loss layers
// LossyNetwork on top of the jittered baseline; the propagation
// scenarios that wrap PerReceiverDelay preserve whatever inner model
// their sweep set.
//
// Returns nil if Scenarios or Protocols is empty (defensive — caller
// driver test should always pass non-empty lists).
func DefaultSweeps(scenarios []Scenario, protocols []Protocol, iterations int, n int) []Sweep {
	if len(scenarios) == 0 || len(protocols) == 0 || iterations <= 0 || n <= 0 {
		return nil
	}
	return []Sweep{
		canonicalSweep(scenarios, protocols, iterations, n),
		bttDegradationSweep(scenarios, protocols, iterations, n),
		heavyTailSweep(scenarios, protocols, iterations, n),
		lossSweep(scenarios, protocols, iterations, n),
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

func canonicalSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	btt := 200 * time.Millisecond
	base := withClusterSize(DefaultProposerDutyConfig(btt), n)
	base.Network = JitteredDelay{D: btt, Jitter: btt / 4}
	nStr := strconv.Itoa(n)
	return Sweep{
		Name:        "canonical",
		Title:       "Normal operations",
		Params:      []string{"n=" + nStr, "BTT=200ms"},
		Description: "The spec's canonical config — every other sweep's baseline.",
		AxisLabel:   "",
		Points: []SweepPoint{
			{
				Label: "n=" + nStr + " BTT=200ms",
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

func bttDegradationSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	btts := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		600 * time.Millisecond,
	}
	pts := make([]SweepPoint, 0, len(btts))
	for _, btt := range btts {
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		// Jitter scales with BTT so the relative variance stays at ±25%.
		base.Network = JitteredDelay{D: btt, Jitter: btt / 4}
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
		Name:        "btt_degradation",
		Title:       "Network-degradation curves (BTT)",
		Params:      []string{"n=" + strconv.Itoa(n)},
		Description: "Reveals envelope-fit at each protocol's tolerance ceiling.",
		AxisLabel:   "BTT",
		Points:      pts,
	}
}

func heavyTailSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	sigmas := []float64{0.1, 0.3, 0.5, 0.7}
	pts := make([]SweepPoint, 0, len(sigmas))
	for _, sigma := range sigmas {
		base := withClusterSize(DefaultProposerDutyConfig(200*time.Millisecond), n)
		// Heavy-tail propagation: log-normal centered at BTT/2 (= typical
		// P50 propagation per spec §Setting), with Sigma controlling tail
		// fatness. P99/P50 ratio = exp(Sigma · 2.326): 1.27× / 2.01× /
		// 3.20× / 5.09× at the four sample points.
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
		Name:        "heavy_tail",
		Title:       "Heavy-tail propagation",
		Params:      []string{"LogNormalDelay", "n=" + strconv.Itoa(n), "Median=BTT/2"},
		Description: "Surfaces P99/P50-ratio effects on OBFT's hard B_k cutoff vs QBFT's round-change tolerance.",
		AxisLabel:   "LogNormal sigma",
		Points:      pts,
	}
}

func lossSweep(scenarios []Scenario, protocols []Protocol, iterations int, n int) Sweep {
	rates := []float64{0, 0.01, 0.05, 0.10}
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
		btt := 200 * time.Millisecond
		base := withClusterSize(DefaultProposerDutyConfig(btt), n)
		// Stress-tier jitter under the loss model — matches the other
		// non-network-varying sweeps. Loss adds on top.
		base.Network = JitteredDelay{D: btt, Jitter: btt / 4}
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
		Name:        "loss",
		Title:       "Stochastic loss",
		Params:      []string{"LossyNetwork", "JitteredDelay", "n=" + strconv.Itoa(n), "BurstFactor=5"},
		Description: "Each scenario gets a fresh LossyNetwork instance per sim to preserve determinism.",
		AxisLabel:   "Loss rate",
		Points:      pts,
	}
}

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

// DefaultSweeps returns the curated set of comparison sweeps from
// docs/CONSENSUSTEST-BATCH-PLAN.md / Phase D2:
//
//  1. Canonical — single point at the spec's reference config
//     (n=4, BTT=200ms, K=4, JitteredDelay). The baseline.
//  2. Cluster scaling — varies n ∈ {4, 7, 10, 13} at JitteredDelay.
//     Shows per-protocol scaling behavior across SSV-supported cluster
//     sizes.
//  3. Network degradation — varies BTT ∈ {100ms, 200ms, 400ms, 600ms}
//     at fixed n=4 under JitteredDelay (per-BTT scaled jitter). Reveals
//     envelope-fit curves.
//  4. Heavy-tail propagation — varies LogNormalDelay Sigma ∈
//     {0.1, 0.3, 0.5, 0.7} at fixed n=4, Median=BTT/2. Surfaces
//     P99/P50-ratio effects on OBFT's hard B_k cutoff. (Network varies
//     by design; ignores the stress-tier jitter default.)
//  5. Loss — varies LossyNetwork LossRate ∈ {0, 0.01, 0.05, 0.10}
//     at fixed n=4, BurstFactor=5. Inner model is ConstantDelay to
//     isolate the loss effect from jitter.
//
// All sweeps share Iterations and the input Scenarios / Protocols
// matrix; only the per-point Network / SimConfig differs.
//
// Stress-tier network choice: per Phase 5 of the catalog-split plan,
// sweeps that don't already vary network shape (canonical / cluster
// scaling / BTT degradation) use JitteredDelay with ±25% jitter around
// BTT to model real-world propagation variance. Heavy-tail and loss
// sweeps each set their own network model and are unaffected.
//
// Returns nil if Scenarios or Protocols is empty (defensive — caller
// driver test should always pass non-empty lists).
func DefaultSweeps(scenarios []Scenario, protocols []Protocol, iterations int) []Sweep {
	if len(scenarios) == 0 || len(protocols) == 0 || iterations <= 0 {
		return nil
	}
	return []Sweep{
		canonicalSweep(scenarios, protocols, iterations),
		clusterScalingSweep(scenarios, protocols, iterations),
		bttDegradationSweep(scenarios, protocols, iterations),
		heavyTailSweep(scenarios, protocols, iterations),
		lossSweep(scenarios, protocols, iterations),
	}
}

func canonicalSweep(scenarios []Scenario, protocols []Protocol, iterations int) Sweep {
	btt := 200 * time.Millisecond
	base := DefaultProposerDutyConfig(btt)
	base.Network = JitteredDelay{D: btt, Jitter: btt / 4}
	return Sweep{
		Name:        "canonical",
		Title:       "Canonical operating point",
		Description: "Reference operating point: n=4, BTT=200ms, K=4, JitteredDelay. The spec's canonical config — every other sweep's baseline.",
		AxisLabel:   "",
		Points: []SweepPoint{
			{
				Label: "n=4 BTT=200ms",
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

func clusterScalingSweep(scenarios []Scenario, protocols []Protocol, iterations int) Sweep {
	pts := make([]SweepPoint, 0, len(ClusterSizes))
	btt := 200 * time.Millisecond
	for _, n := range ClusterSizes {
		base := SimConfig{
			N:            n,
			Operators:    MakeOperators(n),
			SlotDuration: 12 * time.Second,
			RelayCutoff:  4 * time.Second,
			BTT:          btt,
			Network:      JitteredDelay{D: btt, Jitter: btt / 4},
		}
		pts = append(pts, SweepPoint{
			Label: "n=" + strconv.Itoa(n),
			Config: BatchConfig{
				Iterations: iterations,
				Base:       base,
				Scenarios:  scenarios,
				Protocols:  protocols,
			},
		})
	}
	return Sweep{
		Name:        "cluster_scaling",
		Title:       "Cluster-size scaling",
		Description: "Cluster-size scaling: n ∈ {4, 7, 10, 13} at fixed BTT=200ms. Shows per-protocol scaling behavior across SSV-supported cluster sizes.",
		AxisLabel:   "Cluster size n",
		Points:      pts,
	}
}

func bttDegradationSweep(scenarios []Scenario, protocols []Protocol, iterations int) Sweep {
	btts := []time.Duration{
		100 * time.Millisecond,
		200 * time.Millisecond,
		400 * time.Millisecond,
		600 * time.Millisecond,
	}
	pts := make([]SweepPoint, 0, len(btts))
	for _, btt := range btts {
		base := DefaultProposerDutyConfig(btt)
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
		Description: "Network-degradation curves: BTT ∈ {100, 200, 400, 600}ms at fixed n=4. Reveals envelope-fit at each protocol's tolerance ceiling.",
		AxisLabel:   "BTT",
		Points:      pts,
	}
}

func heavyTailSweep(scenarios []Scenario, protocols []Protocol, iterations int) Sweep {
	sigmas := []float64{0.1, 0.3, 0.5, 0.7}
	pts := make([]SweepPoint, 0, len(sigmas))
	for _, sigma := range sigmas {
		base := DefaultProposerDutyConfig(200 * time.Millisecond)
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
		Description: "Heavy-tail propagation: LogNormalDelay Sigma ∈ {0.1, 0.3, 0.5, 0.7} at fixed n=4, Median=BTT/2. Surfaces P99/P50-ratio effects on OBFT's hard B_k cutoff vs QBFT's round-change tolerance.",
		AxisLabel:   "LogNormal sigma",
		Points:      pts,
	}
}

func lossSweep(scenarios []Scenario, protocols []Protocol, iterations int) Sweep {
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
		pts = append(pts, SweepPoint{
			Label: "loss=" + strconv.FormatFloat(rate, 'f', 2, 64),
			Config: BatchConfig{
				Iterations: iterations,
				Base:       DefaultProposerDutyConfig(200 * time.Millisecond),
				Scenarios:  scenariosWithLoss,
				Protocols:  protocols,
			},
		})
	}
	return Sweep{
		Name:        "loss",
		Title:       "Stochastic loss",
		Description: "Stochastic loss: LossyNetwork LossRate ∈ {0, 0.01, 0.05, 0.10}, BurstFactor=5, at fixed n=4. Each scenario gets a fresh LossyNetwork instance via Apply to preserve per-sim determinism.",
		AxisLabel:   "Loss rate",
		Points:      pts,
	}
}

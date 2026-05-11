package consensustest

import (
	"errors"
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// BatchConfig parameterizes a multi-sim batch run. Each (scenario, protocol)
// cell runs Iterations times with seeds [SeedStart, SeedStart+Iterations);
// per-cell goroutines parallelize across cells (sims within a cell run
// sequentially, since they share the same RNG-derived state across seeds).
//
// Determinism: byte-identical BatchReport stats are guaranteed for the same
// (Iterations, SeedStart, Base, Scenarios, Protocols) tuple. Parallelism
// reorders WHEN sims execute but not WHICH outputs they produce — each
// sim is wholly determined by its (cfg, seed) input.
type BatchConfig struct {
	// Iterations is the number of sims per (scenario, protocol) cell. Must
	// be > 0. Default in the driver test is 100; 1000+ surfaces rare-event
	// distributions but takes proportionally longer.
	Iterations int

	// SeedStart is the per-cell first seed; iter k uses Seed = SeedStart+k.
	// Different SeedStart values produce a different (but still
	// deterministic) sample set — useful for cross-validation.
	SeedStart int64

	// Base is the template SimConfig. Each iteration sets Seed and applies
	// the scenario's Apply. Network / Host / Byz on Base are starting
	// points; scenarios that mutate them via Apply override per-cell.
	Base SimConfig

	// Scenarios is the list of scenarios to run. Each is applied to a copy
	// of Base; the original Base is not mutated.
	Scenarios []Scenario

	// Protocols is the list of protocols to compare. Each cell is one
	// (scenario, protocol) pair; the matrix dimension is
	// len(Scenarios) * len(Protocols).
	Protocols []Protocol

	// Parallelism caps the number of goroutines running cells in parallel.
	// 0 → GOMAXPROCS. A single goroutine processes its assigned cell's
	// Iterations sims sequentially, so per-cell determinism is preserved.
	Parallelism int
}

// validate sanity-checks the config and fills defaults.
func (c *BatchConfig) validate() error {
	if c.Iterations <= 0 {
		return errors.New("consensustest: BatchConfig.Iterations must be > 0")
	}
	if len(c.Scenarios) == 0 {
		return errors.New("consensustest: BatchConfig.Scenarios must be non-empty")
	}
	if len(c.Protocols) == 0 {
		return errors.New("consensustest: BatchConfig.Protocols must be non-empty")
	}
	if c.Parallelism <= 0 {
		c.Parallelism = runtime.GOMAXPROCS(0)
	}
	// SeedStart=0 is valid (default zero value); same for Base (validated
	// per-sim inside Protocol.Run via SimConfig.Validate).
	return nil
}

// RunBatch drives all (scenario, protocol) cells Iterations times each and
// aggregates results. Cells run in parallel up to BatchConfig.Parallelism;
// sims within a cell run sequentially.
//
// Safety: RunBatch panics on any SafetyReport.IsViolation() in any sim,
// per the existing RunScenarioOnProtocol contract. A safety violation in
// batch mode is a hard test failure regardless of declared scenario
// expectation.
//
// Scenarios that return ErrNotApplicable for a given protocol contribute
// a cell with zero Iterations (Iterations field reflects ATTEMPTED count;
// Distributions are empty). Renderers should treat such cells as "n/a".
//
// Determinism: re-running with the same (Iterations, SeedStart, Base,
// Scenarios, Protocols) produces identical Cells stats. Wallclock varies
// across runs.
func RunBatch(t *testing.T, cfg BatchConfig) BatchReport {
	t.Helper()
	if err := cfg.validate(); err != nil {
		t.Fatalf("consensustest: RunBatch: %v", err)
	}

	type job struct {
		scenarioIdx int
		protocolIdx int
	}

	cellCount := len(cfg.Scenarios) * len(cfg.Protocols)
	results := make([]BatchCell, cellCount)
	jobs := make(chan job, cellCount)

	var wg sync.WaitGroup
	for w := 0; w < cfg.Parallelism; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				idx := j.scenarioIdx*len(cfg.Protocols) + j.protocolIdx
				results[idx] = runCell(t, cfg, cfg.Scenarios[j.scenarioIdx], cfg.Protocols[j.protocolIdx])
			}
		}()
	}

	start := time.Now()
	for si := range cfg.Scenarios {
		for pi := range cfg.Protocols {
			jobs <- job{scenarioIdx: si, protocolIdx: pi}
		}
	}
	close(jobs)
	wg.Wait()
	wallclock := time.Since(start)

	return BatchReport{
		Config:      cfg,
		Cells:       results,
		Wallclock:   wallclock,
		GeneratedAt: time.Now().UTC(),
	}
}

// runCell runs Iterations sims for one (scenario, protocol) pair and
// aggregates them into a BatchCell.
func runCell(t *testing.T, cfg BatchConfig, scenario Scenario, protocol Protocol) BatchCell {
	t.Helper()
	cell := BatchCell{
		Protocol:         protocol.Name(),
		Scenario:         scenario.Name,
		Iterations:       cfg.Iterations,
		DecisionTime:     make(Distribution, 0, cfg.Iterations),
		ClusterBandwidth: make(Distribution, 0, cfg.Iterations),
		PerKindBandwidth: make(map[string]Distribution),
		EvidenceCounts:   make(map[string]Distribution),
		MissReasons:      make(map[string]int),
	}

	successCount := 0
	for iter := 0; iter < cfg.Iterations; iter++ {
		simCfg := cfg.Base
		simCfg.Seed = cfg.SeedStart + int64(iter)
		if scenario.Apply != nil {
			scenario.Apply(&simCfg)
		}

		out, err := protocol.Run(simCfg)
		if err != nil {
			if errors.Is(err, ErrNotApplicable) {
				// Scenario not applicable to this protocol — return an
				// empty cell with Iterations=0 to signal "n/a" downstream.
				return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}
			}
			// Other Run errors — typically config validation (e.g. at
			// BTT=600ms, OBFT's deepest layer's broadcast deadline goes
			// negative). Log once per cell at iter=0 and surface as an
			// Iterations=0 cell so renderers show "n/a" rather than the
			// whole batch aborting. The user gets all OTHER cells'
			// comparison data even when one operating point is out of
			// envelope for one protocol.
			if iter == 0 {
				t.Logf("RunBatch: %s/%s out of envelope: %v (cell marked n/a)",
					protocol.Name(), scenario.Name, err)
			}
			return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}
		}

		// Universal safety invariants — panic on violation, matching the
		// existing RunScenarioOnProtocol contract. A safety violation in
		// batch mode is a hard failure regardless of scenario expectation;
		// SafetyPanic terminates the test before any further sims run.
		report := ComputeSafetyReport(out)
		if report.IsViolation() {
			SafetyPanic(report, scenario.Name, protocol.Name(), ExpectSuccessOrMiss, out)
		}

		if out.Decided {
			successCount++
			cell.DecisionTime = append(cell.DecisionTime, float64(out.DecisionTime.Milliseconds()))
		} else {
			cell.MissReasons[classifyMiss(out)]++
		}

		cell.ClusterBandwidth = append(cell.ClusterBandwidth, float64(out.Bandwidth.TotalBytes))
		for kind, bytes := range out.Bandwidth.PerKindBytes {
			cell.PerKindBandwidth[kind] = append(cell.PerKindBandwidth[kind], float64(bytes))
		}

		// EvidenceCounts: one sample per sim per rule, value = sum of fires
		// across all operators in this sim.
		perSimEvidence := make(map[string]int)
		for _, oo := range out.PerOp {
			for rule, count := range oo.EvidenceByRule {
				perSimEvidence[rule] += count
			}
		}
		for rule, count := range perSimEvidence {
			cell.EvidenceCounts[rule] = append(cell.EvidenceCounts[rule], float64(count))
		}
	}

	cell.SuccessRate = float64(successCount) / float64(cfg.Iterations)
	return cell
}

// classifyMiss returns a coarse string label for why an Outcome reported
// !Decided. Used as the MissReasons map key. Free-form — renderers treat
// it as an attribute, not an enum.
//
// Today's classifier checks the per-op Err strings against a few known
// patterns. Future work can refine (e.g., distinguish "no σ-quorum +
// no NR-quorum at L_0" from "no quorum at any layer") by inspecting
// PerOp evidence or the Trace.
func classifyMiss(o Outcome) string {
	// Aggregate per-op Err strings. If they cluster on a single class,
	// label that; else "mixed".
	classes := make(map[string]int)
	for _, oo := range o.PerOp {
		if oo.Decided {
			continue
		}
		if oo.Err == "" {
			classes["no_decision_no_error"]++
		} else {
			classes[shortenErr(oo.Err)]++
		}
	}
	if len(classes) == 0 {
		return "unknown"
	}
	if len(classes) == 1 {
		for k := range classes {
			return k
		}
	}
	// Multiple classes — return the most common one with a "+mixed" suffix.
	top, topCount := "", 0
	for k, c := range classes {
		if c > topCount {
			top = k
			topCount = c
		}
	}
	return top + "+mixed"
}

// shortenErr maps a verbose per-op error string to a coarse label.
// Specific to the production OBFT / QBFT error messages this framework
// observes; defaults to the first 32 chars if unrecognized.
func shortenErr(err string) string {
	lower := strings.ToLower(err)
	known := []struct {
		needle string
		label  string
	}{
		{"no quorum", "no_quorum"},
		{"noquorum", "no_quorum"},
		{"timed out", "timeout"},
		{"timeout", "timeout"},
		{"context", "context_cancel"},
	}
	for _, k := range known {
		if strings.Contains(lower, k.needle) {
			return k.label
		}
	}
	if len(err) > 32 {
		return err[:32]
	}
	return err
}

// SortedCellKeys returns the (Scenario, Protocol) pairs in stable order for
// rendering. Convenience for renderers that want deterministic table /
// chart orderings.
func (r BatchReport) SortedCellKeys() []string {
	type key struct {
		scenario string
		protocol string
	}
	seen := make(map[key]bool, len(r.Cells))
	keys := make([]key, 0, len(r.Cells))
	for _, c := range r.Cells {
		k := key{scenario: c.Scenario, protocol: c.Protocol}
		if seen[k] {
			continue
		}
		seen[k] = true
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool {
		if keys[i].scenario != keys[j].scenario {
			return keys[i].scenario < keys[j].scenario
		}
		return keys[i].protocol < keys[j].protocol
	})
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = fmt.Sprintf("%s/%s", k.scenario, k.protocol)
	}
	return out
}

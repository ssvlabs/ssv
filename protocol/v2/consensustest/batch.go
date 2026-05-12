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
// cell runs `IterationsFor(scenario)` times with seeds
// [SeedStart, SeedStart+iter); per-cell goroutines parallelize across cells
// (sims within a cell run sequentially, since they share the same RNG-
// derived state across seeds).
//
// Determinism: byte-identical BatchReport stats are guaranteed for the same
// (Iterations, IterationsByGroup, SeedStart, Base, Scenarios, Protocols)
// tuple. Parallelism reorders WHEN sims execute but not WHICH outputs they
// produce — each sim is wholly determined by its (cfg, seed) input.
type BatchConfig struct {
	// Iterations is the default per-cell iteration count, used for any
	// scenario whose Group is not covered by IterationsByGroup. Must be
	// > 0. Typical drivers set it to the "unstable" budget (e.g. 10)
	// so the iteration cost stays bounded on the long tail of
	// adversarial scenarios.
	Iterations int

	// IterationsByGroup, if set, overrides the per-cell iteration count
	// for scenarios whose Group is a key here. The intended split is
	// "Baseline" (high iteration count — Healthy / normal-operations
	// flavor) vs everything else (low count — rare-event behaviour
	// where 10 sims is enough to surface a non-zero success rate).
	// Map values must be > 0; non-positive entries are ignored.
	IterationsByGroup map[string]int

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
	// iter-count sims sequentially, so per-cell determinism is preserved.
	Parallelism int
}

// IterationsFor returns the per-cell iteration count for scenario `sc`.
// If IterationsByGroup has a positive entry for sc.Group, that wins;
// otherwise Iterations is the fallback.
func (c BatchConfig) IterationsFor(sc Scenario) int {
	if c.IterationsByGroup != nil {
		if v, ok := c.IterationsByGroup[sc.Group]; ok && v > 0 {
			return v
		}
	}
	return c.Iterations
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

	cellCount := len(cfg.Scenarios) * len(cfg.Protocols)
	scenarioOf := func(cellIdx int) Scenario { return cfg.Scenarios[cellIdx/len(cfg.Protocols)] }
	protocolOf := func(cellIdx int) Protocol { return cfg.Protocols[cellIdx%len(cfg.Protocols)] }

	// Per-cell iteration count — looked up via IterationsFor(scenario) so
	// "Baseline"-group scenarios can run with a different budget than
	// adversarial / unstable ones.
	cellIters := make([]int, cellCount)
	totalIters := 0
	for ci := 0; ci < cellCount; ci++ {
		cellIters[ci] = cfg.IterationsFor(scenarioOf(ci))
		totalIters += cellIters[ci]
	}

	// Single iteration-level work queue: each job is one (cellIdx, iter)
	// sim run. Replaces the prior cell-level pool — by flattening the unit
	// of work to a single sim, end-of-batch stragglers don't idle cores
	// (the last cell's iterations spread across all workers instead of
	// monopolizing a single worker). Net wall-time gain is largest for
	// sweeps with high per-cell timing variance (p2p_heavy_tail, p2p_packet_loss).
	type job struct {
		cellIdx int
		iter    int
	}

	// results[cellIdx][iter] — each slot is written by exactly one
	// goroutine (the worker that pulls that job), so no per-slot
	// synchronization is needed. Reduce happens single-threaded after
	// wg.Wait().
	results := make([][]iterOutcome, cellCount)
	for ci := range results {
		results[ci] = make([]iterOutcome, cellIters[ci])
	}

	jobs := make(chan job, totalIters)
	for ci := 0; ci < cellCount; ci++ {
		for iter := 0; iter < cellIters[ci]; iter++ {
			jobs <- job{cellIdx: ci, iter: iter}
		}
	}
	close(jobs)

	start := time.Now()
	var wg sync.WaitGroup
	for w := 0; w < cfg.Parallelism; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				simCfg := cfg.Base
				simCfg.Seed = cfg.SeedStart + int64(j.iter)
				if apply := scenarioOf(j.cellIdx).Apply; apply != nil {
					apply(&simCfg)
				}
				out, err := protocolOf(j.cellIdx).Run(simCfg)
				results[j.cellIdx][j.iter] = iterOutcome{out: out, err: err}
			}
		}()
	}
	wg.Wait()
	wallclock := time.Since(start)

	// Reduce per-iter results into per-cell BatchCells single-threaded.
	cells := make([]BatchCell, cellCount)
	for ci := range cells {
		cells[ci] = reduceCellResults(t, cellIters[ci], scenarioOf(ci), protocolOf(ci), results[ci])
	}

	return BatchReport{
		Config:      cfg,
		Cells:       cells,
		Wallclock:   wallclock,
		GeneratedAt: time.Now().UTC(),
	}
}

// iterOutcome captures the per-iteration Outcome + error from one sim
// run inside RunBatch. Workers write directly to results[cellIdx][iter];
// reduceCellResults consumes the per-cell slice single-threaded.
type iterOutcome struct {
	out Outcome
	err error
}

// reduceCellResults aggregates one cell's per-iter outcomes into a
// BatchCell. Mirrors the per-sim accumulation that runCell used to do
// inline, lifted out so the parallel iteration loop above can stay
// straightforward and the reduction stays single-threaded (no per-cell
// mutex on the distributions / maps).
//
// cellIter is the iteration count BatchConfig.IterationsFor returned for
// this cell's scenario — already used to size results[ci] in RunBatch.
//
// Two error classes get distinct cell treatments:
//   - ErrNotApplicable (scenario semantically doesn't translate to this
//     protocol — e.g. OBFT-only byz patterns on QBFT): Iterations=0,
//     renderers show "n/a".
//   - ErrConfigOutOfEnvelope (the scenario applies but the SimConfig
//     derives an infeasible schedule — e.g. BTT=600ms collapses OBFT's
//     deepest broadcast budget to negative): Iterations=cellIter,
//     SuccessRate=0, renderers show a red 0%. This is a protocol failure
//     mode at this operating point, not an inapplicability.
//   - Any other Run error: still treated as n/a (with a t.Logf — these are
//     bugs to investigate).
//
// The check inspects the first iter only since these errors are config-level
// and uniform across iters at a given (protocol, config) pair.
func reduceCellResults(t *testing.T, cellIter int, scenario Scenario, protocol Protocol, iters []iterOutcome) BatchCell {
	t.Helper()
	if len(iters) > 0 {
		if first := iters[0]; first.err != nil {
			if errors.Is(first.err, ErrNotApplicable) {
				return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}
			}
			if errors.Is(first.err, ErrConfigOutOfEnvelope) {
				// Protocol can't operate at this operating point — return a
				// full-iter-count cell with no successful decisions so the
				// UI renders it as a hard 0% (red) rather than n/a.
				return BatchCell{
					Protocol:    protocol.Name(),
					Scenario:    scenario.Name,
					Iterations:  cellIter,
					SuccessRate: 0,
					MissReasons: map[string]int{"config out of envelope": cellIter},
				}
			}
			// Unexpected non-applicability / envelope error — bug to chase.
			t.Logf("RunBatch: %s/%s unexpected error: %v (cell marked n/a)",
				protocol.Name(), scenario.Name, first.err)
			return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}
		}
	}

	cell := BatchCell{
		Protocol:         protocol.Name(),
		Scenario:         scenario.Name,
		Iterations:       cellIter,
		DecisionTime:     make(Distribution, 0, cellIter),
		ClusterBandwidth: make(Distribution, 0, cellIter),
		PerKindBandwidth: make(map[string]Distribution),
		EvidenceCounts:   make(map[string]Distribution),
		MissReasons:      make(map[string]int),
	}

	successCount := 0
	for _, r := range iters {
		if r.err != nil {
			// Defense in depth — the pre-check above caught uniform errors;
			// a per-iter err here would indicate non-uniform behavior that
			// the rest of the framework doesn't anticipate. Mark n/a to be
			// safe.
			return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}
		}

		// Universal safety invariants — panic on violation, matching the
		// existing RunScenarioOnProtocol contract. SafetyPanic terminates
		// the test before any further reduce work runs.
		report := ComputeSafetyReport(r.out)
		if report.IsViolation() {
			SafetyPanic(report, scenario.Name, protocol.Name(), ExpectSuccessOrMiss, r.out)
		}

		if r.out.Decided {
			successCount++
			cell.DecisionTime = append(cell.DecisionTime, float64(r.out.DecisionTime.Milliseconds()))
		} else {
			cell.MissReasons[classifyMiss(r.out)]++
		}

		cell.ClusterBandwidth = append(cell.ClusterBandwidth, float64(r.out.Bandwidth.TotalBytes))
		for kind, bytes := range r.out.Bandwidth.PerKindBytes {
			cell.PerKindBandwidth[kind] = append(cell.PerKindBandwidth[kind], float64(bytes))
		}

		// EvidenceCounts: one sample per sim per rule, value = sum of fires
		// across all operators in this sim.
		perSimEvidence := make(map[string]int)
		for _, oo := range r.out.PerOp {
			for rule, count := range oo.EvidenceByRule {
				perSimEvidence[rule] += count
			}
		}
		for rule, count := range perSimEvidence {
			cell.EvidenceCounts[rule] = append(cell.EvidenceCounts[rule], float64(count))
		}
	}

	cell.SuccessRate = float64(successCount) / float64(cellIter)
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

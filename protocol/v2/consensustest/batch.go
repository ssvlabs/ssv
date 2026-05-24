package consensustest

import (
	"errors"
	"fmt"
	"runtime"
	"runtime/debug"
	"sort"
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

	// Progress, when non-nil, is incremented once per completed sim so a
	// driver can render run-wide progress / ETA. Observability only — it never
	// affects outcomes or the report, so it is excluded from the determinism
	// contract above. Nil for callers that don't want progress (unit tests).
	Progress *ProgressTracker
}

// cellIterCounts returns the per-cell iteration count, indexed the same way as
// RunBatch's cell grid (cellIdx = scenario*nProtocols + protocol). A cell is 0
// when it won't run: baseline-only protocol variants on non-Baseline scenarios
// (rendered n/a). Single source of truth shared by RunBatch and TotalIters so
// the progress total can't drift from the work actually executed.
func (c BatchConfig) cellIterCounts() []int {
	nProto := len(c.Protocols)
	counts := make([]int, len(c.Scenarios)*nProto)
	for ci := range counts {
		sc := c.Scenarios[ci/nProto]
		p := c.Protocols[ci%nProto]
		if isBaselineOnly(p) && !IsBaselineGroup(sc) {
			continue
		}
		counts[ci] = c.IterationsFor(sc)
	}
	return counts
}

// TotalIters is the number of sims this batch will run (sum of cellIterCounts).
// Drivers sum it across every batch to seed a ProgressTracker before any sim
// runs.
func (c BatchConfig) TotalIters() int64 {
	var total int64
	for _, n := range c.cellIterCounts() {
		total += int64(n)
	}
	return total
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

// isBaselineOnly reports whether a protocol variant runs only on
// Baseline-group (Healthy) scenarios — the cushion-sensitivity rungs
// (X-0 / X-300 / X-500) declare this via an optional IsBaselineOnly()
// method, so RunBatch renders them n/a on adversarial scenarios, where only
// the canonical X-700 rung per family runs. A protocol that doesn't
// implement the interface is treated as not baseline-only.
func isBaselineOnly(p Protocol) bool {
	bo, ok := p.(interface{ IsBaselineOnly() bool })
	return ok && bo.IsBaselineOnly()
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
	// adversarial / unstable ones. Baseline-only variants (the cushion-
	// sensitivity rungs X-0/X-300/X-500) get 0 on adversarial scenarios (n/a;
	// only the canonical X-700 rung runs there). Shared with TotalIters via
	// cellIterCounts so progress accounting matches the work executed.
	cellIters := cfg.cellIterCounts()
	totalIters := 0
	for _, n := range cellIters {
		totalIters += n
	}

	// Single iteration-level work queue: each job is one (cellIdx, iter)
	// sim run. Replaces the prior cell-level pool — by flattening the unit
	// of work to a single sim, end-of-batch stragglers don't idle cores
	// (the last cell's iterations spread across all workers instead of
	// monopolizing a single worker). Net wall-time gain is largest for
	// sweeps with high per-cell timing variance (p2p_packet_loss,
	// p2p_correlated_delays, instability-wrapped Healthy).
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
				results[j.cellIdx][j.iter] = runOneSim(cfg, scenarioOf(j.cellIdx), protocolOf(j.cellIdx), j.iter)
				cfg.Progress.Add(1) // nil-safe; observability only
			}
		}()
	}
	wg.Wait()
	wallclock := time.Since(start)

	// Reduce per-iter results into per-cell BatchCells single-threaded.
	cells := make([]BatchCell, cellCount)
	for ci := range cells {
		// Baseline-only variant on an adversarial scenario — not run; emit
		// an n/a cell (Iterations:0), same shape as ErrNotApplicable cells.
		if isBaselineOnly(protocolOf(ci)) && !IsBaselineGroup(scenarioOf(ci)) {
			cells[ci] = BatchCell{Protocol: protocolOf(ci).Name(), Scenario: scenarioOf(ci).Name, Iterations: 0}
			continue
		}
		cells[ci] = reduceCellResults(t, cellIters[ci], scenarioOf(ci), protocolOf(ci), results[ci])
	}

	return BatchReport{
		Config:      cfg,
		Cells:       cells,
		Wallclock:   wallclock,
		GeneratedAt: time.Now().UTC(),
	}
}

// iterOutcome captures the per-iteration Outcome + error + seed from
// one sim run inside RunBatch. Workers write directly to
// results[cellIdx][iter]; reduceCellResults consumes the per-cell slice
// single-threaded. seed is the SimConfig.Seed used for this iter — kept
// alongside the outcome so SafetyPanic can attach it for repro.
type iterOutcome struct {
	out  Outcome
	err  error
	seed int64
}

// errSimPanic is the sentinel returned by runOneSim's recover wrapper.
// Treated as a per-iter miss by aggregateCellIters (recorded under the
// "Simulation panicked..." MissReasons bucket) rather than collapsing
// the whole cell to n/a — a single panicking seed in a 1000-iter
// baseline cell shouldn't hide the other 999 sims' data.
var errSimPanic = fmt.Errorf("consensustest: sim panic")

// runOneSim runs one iteration on its own goroutine and returns an
// iterOutcome. A panic from inside the protocol (adapter or production
// code under it) is recovered and returned as errSimPanic — wrapped
// with the seed + scenario / protocol identity + a stack snapshot — so
// a single buggy sim doesn't take down the whole stress run. The cell
// then surfaces the failure as a per-iter miss (see
// aggregateCellIters), preserving every other iter's data. Safety
// panics (SafetyPanic) intentionally do NOT happen here: safety is
// checked in the single-threaded reduce phase, where panic propagation
// is well-defined.
//
// The panic-wrap error string includes the seed so the operator can
// reproduce the panic by re-running with that exact seed (sims are
// deterministic given (cfg, seed)). The stack snapshot is attached so
// the root cause is loggable without re-running.
func runOneSim(cfg BatchConfig, sc Scenario, p Protocol, iter int) (oc iterOutcome) {
	seed := cfg.SeedStart + int64(iter)
	defer func() {
		if r := recover(); r != nil {
			oc = iterOutcome{
				err: fmt.Errorf("%w: %s/%s iter %d seed %d: %v\n%s",
					errSimPanic, p.Name(), sc.Name, iter, seed, r, debug.Stack()),
				seed: seed,
			}
		}
	}()
	simCfg := cfg.Base
	simCfg.Seed = seed
	// Propagate scenario-level Delivery before Apply runs. Apply may
	// still override (no scenario does today, but the field is on
	// SimConfig so the option exists). Zero value (DeliveryDirect) is
	// the back-compat path — preserves every adversarial scenario's
	// per-(from, to) primitives.
	simCfg.Delivery = sc.Delivery
	// Sweep-level applicability check. Scenarios that aren't meaningful
	// at this (n, K, ...) operating point return false here and the cell
	// is recorded with ErrNotApplicable, mirroring the per-protocol
	// translateByz dispatch. Runs before Apply so Applies sees the
	// sweep-supplied cfg without scenario-internal mutations.
	if sc.Applies != nil && !sc.Applies(simCfg) {
		return iterOutcome{err: ErrNotApplicable, seed: seed}
	}
	if sc.Apply != nil {
		sc.Apply(&simCfg)
	}
	out, err := p.Run(simCfg)
	return iterOutcome{out: out, err: err, seed: seed}
}

// reduceCellResults aggregates one cell's per-iter outcomes into a
// BatchCell. Single-threaded by construction (no per-cell mutex on the
// distributions / maps); the parallel sims write to results[cellIdx][iter]
// independently in RunBatch and this function reduces post-Wait.
//
// cellIter is the iteration count BatchConfig.IterationsFor returned for
// this cell's scenario — already used to size results[ci] in RunBatch.
//
// Two error classes get distinct cell treatments via classifyCellErrors:
//   - ErrNotApplicable (scenario semantically doesn't translate to this
//     protocol — e.g. OBFT-only byz patterns on QBFT): Iterations=0,
//     renderers show "n/a".
//   - ErrConfigOutOfEnvelope (the scenario applies but the SimConfig
//     derives an infeasible schedule — e.g. BTT=600ms collapses OBFT's
//     deepest broadcast budget to negative): Iterations=cellIter,
//     SuccessRate=0, renderers show a red 0%. This is a protocol failure
//     mode at this operating point, not an inapplicability.
//   - Any other Run error: treated as n/a (with a t.Logf — these are
//     bugs to investigate).
//
// classifyCellErrors scans every iter so a non-uniform error (e.g. one
// iter hits ErrConfigOutOfEnvelope while iter 0 succeeded) still surfaces
// — config-level errors should be uniform across iters, but the scan
// catches sim-internal bugs that breach that invariant.
func reduceCellResults(t *testing.T, cellIter int, scenario Scenario, protocol Protocol, iters []iterOutcome) BatchCell {
	t.Helper()
	if cell, classified := classifyCellErrors(t, cellIter, scenario, protocol, iters); classified {
		return cell
	}
	return aggregateCellIters(t, cellIter, scenario, protocol, iters)
}

// classifyCellErrors scans every iter's err once and emits a special-case
// BatchCell if the cell hit a uniform config-level error (ErrNotApplicable
// or ErrConfigOutOfEnvelope). errSimPanic is NOT a special case here —
// per-iter sim panics flow into aggregateCellIters and are recorded as
// per-iter misses under the "Simulation panicked..." MissReasons bucket,
// preserving every other iter's data instead of collapsing the cell to
// n/a. Other unexpected errors log and return an n/a cell. Returns
// (cell, true) when a special-case applies; (zero, false) when the
// caller should aggregate.
//
// Priority on mixed errors (an adapter bug, but defensive):
//
//  1. hasOtherErr  — unknown error class → n/a + log
//  2. NotApplicable — "scenario doesn't translate" trumps all timing
//     verdicts; cell renders as n/a (0 iters)
//  3. OutOfEnvelope — "schedule collapsed" → red 0% with miss reason
//
// NotApplicable beats OutOfEnvelope because the former is a per-cfg
// semantic dispatch decision (translateByz) and the latter is a
// derived-schedule verdict. If even one iter said NotApplicable, the
// scenario didn't apply at all — surfacing the noisy OOE classification
// from the buggy iters would mislead the reader.
func classifyCellErrors(t *testing.T, cellIter int, scenario Scenario, protocol Protocol, iters []iterOutcome) (BatchCell, bool) {
	t.Helper()
	hasNotApplicable, hasOutOfEnvelope, hasOtherErr := false, false, false
	notApplicableCount, outOfEnvelopeCount := 0, 0
	var firstOther error
	for _, r := range iters {
		if r.err == nil {
			continue
		}
		switch {
		case errors.Is(r.err, ErrNotApplicable):
			hasNotApplicable = true
			notApplicableCount++
		case errors.Is(r.err, ErrConfigOutOfEnvelope):
			hasOutOfEnvelope = true
			outOfEnvelopeCount++
		case errors.Is(r.err, errSimPanic):
			// per-iter; aggregate normally and record as a miss
		default:
			if !hasOtherErr {
				firstOther = r.err
			}
			hasOtherErr = true
		}
	}
	switch {
	case hasOtherErr:
		t.Logf("RunBatch: %s/%s unexpected error (cell marked n/a): %v",
			protocol.Name(), scenario.Name, firstOther)
		return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}, true
	case hasNotApplicable:
		// NotApplicable is a semantic "this scenario doesn't translate
		// to this protocol" — adapters set it from per-cfg dispatch
		// (e.g. translateByz), so it should be uniform across every
		// iter of a cell. It takes priority over OutOfEnvelope because
		// "the scenario doesn't apply" is a stronger statement than
		// "the schedule collapsed" — if even one iter returned
		// NotApplicable, the scenario didn't apply at all and the
		// remaining iters' OOE classifications are spurious noise
		// from an adapter bug. Render as n/a (0 iters) rather than
		// red 0%.
		//
		// Uniformity expected (per-cfg semantic dispatch); a partial
		// hit is an adapter bug — surface it so we don't silently mask
		// it under the n/a render.
		if notApplicableCount > 0 && notApplicableCount < cellIter {
			t.Logf("RunBatch: %s/%s ErrNotApplicable was non-uniform across iters (%d/%d) — adapter bug suspected; cell rendered as n/a",
				protocol.Name(), scenario.Name, notApplicableCount, cellIter)
		}
		return BatchCell{Protocol: protocol.Name(), Scenario: scenario.Name, Iterations: 0}, true
	case hasOutOfEnvelope:
		// Protocol can't operate at this operating point — return a
		// full-iter-count cell with no successful decisions so the UI
		// renders it as a hard 0% (red) rather than n/a.
		//
		// OOE is derived deterministically from the SimConfig
		// (cfg.Validate, T_commit ≤ 0, etc.) and does NOT vary with
		// seed — a partial hit therefore indicates either an adapter
		// bug or a sim-internal nondeterminism. Log it so the
		// non-uniform case doesn't silently consume any otherwise-
		// successful iters' samples (the cell still collapses to red
		// 0% per the spec invariant — config-level errors trump per-
		// iter outcomes).
		if outOfEnvelopeCount > 0 && outOfEnvelopeCount < cellIter {
			t.Logf("RunBatch: %s/%s ErrConfigOutOfEnvelope was non-uniform across iters (%d/%d) — adapter bug suspected; cell rendered as red 0%% and any successful iters' samples discarded",
				protocol.Name(), scenario.Name, outOfEnvelopeCount, cellIter)
		}
		return BatchCell{
			Protocol:    protocol.Name(),
			Scenario:    scenario.Name,
			Iterations:  cellIter,
			SuccessRate: 0,
			MissReasons: map[string]int{"Protocol cannot operate at this configuration": cellIter},
		}, true
	}
	return BatchCell{}, false
}

// aggregateCellIters reduces a cell's successful iters into a BatchCell.
// Caller must have already routed config-level errors through
// classifyCellErrors. Panics on any SafetyReport violation, matching the
// RunScenarioOnProtocol contract — safety panics are intentional and
// terminate the test before any further reduce work runs.
//
// Per-iter errSimPanic results contribute to the "Simulation panicked..." MissReasons bucket
// (rather than collapsing the whole cell to n/a) so a single panicking
// seed doesn't hide the other surviving iters' samples.
func aggregateCellIters(t *testing.T, cellIter int, scenario Scenario, protocol Protocol, iters []iterOutcome) BatchCell {
	t.Helper()
	cell := BatchCell{
		Protocol:              protocol.Name(),
		Scenario:              scenario.Name,
		Iterations:            cellIter,
		DecisionTime:          make(Distribution, 0, cellIter),
		DecidingBroadcastTime: make(Distribution, 0, cellIter),
		ClusterBandwidth:      make(Distribution, 0, cellIter),
		PerKindBandwidth:      make(map[string]Distribution),
		MissReasons:           make(map[string]int),
		DecidedRounds:         make(map[int]int),
	}
	successCount := 0
	loggedPanic := false
	for i, r := range iters {
		if errors.Is(r.err, errSimPanic) {
			// Surface the first panic's full stack at the cell level (the
			// stack lives in r.err per runOneSim) so the failing seed can
			// be reproduced from the log; subsequent panics in the same
			// cell increment the counter without re-spamming the log.
			cell.MissReasons["Simulation panicked (framework bug, not a protocol failure)"]++
			if !loggedPanic {
				t.Logf("RunBatch: %s", r.err)
				loggedPanic = true
			}
			// Record a 0-byte ClusterBandwidth sample for the panicked iter
			// so cell.ClusterBandwidth.Len() == cellIter, matching the
			// PerKindBandwidth series after padToIters. Without this,
			// cluster-total medians sit over (cellIter - panicCount)
			// samples while per-kind medians sit over cellIter (with
			// phantom zeros), and the two disagree.
			cell.ClusterBandwidth = append(cell.ClusterBandwidth, 0)
			continue
		}
		// The BFT_start-independence threshold is iter-invariant (a pure
		// function of cfg + protocol, set even on miss iters), so capture
		// it from the first non-panic iter that carries it. nil for
		// pipeline-shift protocols / out-of-envelope cells.
		if cell.BFTStartIndependence == nil && r.out.BFTStartIndependenceThreshold != nil {
			cell.BFTStartIndependence = r.out.BFTStartIndependenceThreshold
		}
		report := ComputeSafetyReport(r.out)
		if report.IsViolation() {
			SafetyPanic(report, scenario.Name, protocol.Name(), ExpectSuccessOrMiss, r.seed, r.out)
		}
		if r.out.Decided {
			successCount++
			cell.DecisionTime = append(cell.DecisionTime, float64(r.out.DecisionTime.Milliseconds()))
			cell.DecidingBroadcastTime = append(cell.DecidingBroadcastTime, float64(r.out.DecidingBroadcastTime.Milliseconds()))
			cell.DecidedRounds[r.out.DecidedRound]++
		} else {
			cell.MissReasons[classifyMiss(r.out)]++
		}
		cell.ClusterBandwidth = append(cell.ClusterBandwidth, float64(r.out.Bandwidth.TotalBytes))
		for kind, bytes := range r.out.Bandwidth.PerKindBytes {
			ensureLen(cell.PerKindBandwidth, kind, i)
			cell.PerKindBandwidth[kind] = append(cell.PerKindBandwidth[kind], float64(bytes))
		}
	}
	// Right-pad every kind slice with zeros so its length matches
	// cell.Iterations. Without this, kinds that only fire on some iters
	// publish a Median computed over the subset where they fired, biasing
	// reported per-kind percentiles upward and silently disagreeing with
	// cell.ClusterBandwidth.Len() / cell.Iterations.
	padToIters(cell.PerKindBandwidth, len(iters))
	cell.SuccessRate = float64(successCount) / float64(cellIter)
	return cell
}

// ensureLen extends m[key] with trailing zeros until its length is
// `iterIdx`, so the caller's next append lands at position `iterIdx`.
// (Mechanically it's `append` — appends zeros to the right end — but
// the EFFECT from the caller's perspective is to back-fill the
// "missing" iter slots before iterIdx with zeros, so the resulting
// series is aligned with the iter axis.) Used by aggregateCellIters
// to handle kinds that didn't appear in r.out.Bandwidth.PerKindBytes
// on prior iterations.
func ensureLen(m map[string]Distribution, key string, iterIdx int) {
	d := m[key]
	for len(d) < iterIdx {
		d = append(d, 0)
	}
	m[key] = d
}

// padToIters extends every map value to length `iters` by right-padding
// with zeros. Mirrors ensureLen for kinds whose final iteration was a
// no-op.
func padToIters(m map[string]Distribution, iters int) {
	for key, d := range m {
		for len(d) < iters {
			d = append(d, 0)
		}
		m[key] = d
	}
}

// classifyMiss returns the MissReasons map key for an Outcome that
// reported !Decided. Every active adapter sets Outcome.MissReason on
// !Decided (see each protocol's classify*Miss helper), so this is
// effectively a pass-through. The empty-MissReason branch is a safety
// net for a hypothetical adapter that forgets the contract; in normal
// operation it never fires.
func classifyMiss(o Outcome) string {
	if o.MissReason == "" {
		return "Unknown failure (adapter did not set MissReason)"
	}
	return o.MissReason
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

package metrics

import (
	"context"
	"sync"

	"go.opentelemetry.io/otel/metric"
)

// Background: PromQL `increase()` and `rate()` compute the delta between two samples in a
// counter's time series. If a counter is "sparse" (rarely incremented — e.g. error events
// or other rare conditions), it may have no baseline sample for some windows: the series
// starts from the first non-zero increment, and Prometheus has nothing to subtract from.
// After a process restart, this gets worse — the in-process counter starts at 0 and only
// emits a sample on the first increment, which looks to Prometheus like 0→N with no
// previous baseline, so `increase()` returns 0 instead of N.
//
// The fix is to emit `Add(ctx, 0)` once at startup (after the MeterProvider is installed)
// for each sparse counter, guaranteeing a baseline sample is present.
//
// This file provides a tiny registry so each package's observability.go can declare which
// of its counters are sparse, and a single `EmitBaselines` call at startup writes zeros
// for all of them.
//
// High-volume counters (per-message, per-attestation, per-slot, etc.) do not need this
// treatment — they always have recent samples for Prometheus to work from.

var (
	sparseCountersMu sync.Mutex
	sparseCounters   []metric.Int64Counter

	labeledBaselinesMu sync.Mutex
	labeledBaselineFns []func(context.Context)
)

// RegisterSparseCounter declares a counter as sparse (rarely incremented). Returns the
// passed counter unchanged so it can wrap an instrument declaration inline. The counter
// is recorded for baseline emission via EmitBaselines.
//
// Typical use, inside a package-level var block in an observability.go file:
//
//	myErrorCounter = metrics.RegisterSparseCounter(
//	    metrics.New(meter.Int64Counter(observability.InstrumentName(ns, "my_errors"), ...)))
//
// Call EmitBaselines exactly once at startup, after observability.Initialize has set up
// the MeterProvider.
//
// Note: this only baselines the unlabeled time series. Counters that emit with per-call
// attributes still produce one un-baselined series per attribute set on first labeled
// increment. For counters whose attribute combinations are bounded and known at startup
// (e.g. labeled by a fixed set of configured beacon addresses or validator roles), use
// RegisterLabeledBaseline to pre-emit baselines for each attribute combination.
func RegisterSparseCounter(c metric.Int64Counter) metric.Int64Counter {
	sparseCountersMu.Lock()
	defer sparseCountersMu.Unlock()
	sparseCounters = append(sparseCounters, c)
	return c
}

// RegisterLabeledBaseline lets a package contribute a custom baseline-emission function
// that knows the specific attribute combinations it will use at runtime. Useful when a
// counter is labeled by a bounded, runtime-known set of values (e.g. the addresses of
// configured beacon clients, the small set of validator roles).
//
// The registered function is invoked from EmitBaselines, after the OTel MeterProvider is
// installed. The function should iterate its known attribute combinations and emit
// Add(ctx, 0, WithAttributes(...)) for each, so PromQL increase()/rate() return correct
// values for per-label queries even after process restart.
func RegisterLabeledBaseline(fn func(context.Context)) {
	labeledBaselinesMu.Lock()
	defer labeledBaselinesMu.Unlock()
	labeledBaselineFns = append(labeledBaselineFns, fn)
}

// EmitBaselines emits Add(ctx, 0) for every counter registered via RegisterSparseCounter
// (the unlabeled series), then invokes every function registered via
// RegisterLabeledBaseline (which handle per-attribute-set baselines). Call once at startup
// after the OTel MeterProvider is installed (i.e. after observability.Initialize completes).
func EmitBaselines(ctx context.Context) {
	sparseCountersMu.Lock()
	for _, c := range sparseCounters {
		c.Add(ctx, 0)
	}
	sparseCountersMu.Unlock()

	// Copy the slice under lock, then invoke without the lock — defensive in case any
	// registered function indirectly triggers another registration.
	labeledBaselinesMu.Lock()
	fns := make([]func(context.Context), len(labeledBaselineFns))
	copy(fns, labeledBaselineFns)
	labeledBaselinesMu.Unlock()
	for _, fn := range fns {
		fn(ctx)
	}
}

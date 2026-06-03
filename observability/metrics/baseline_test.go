package metrics

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// snapshotRegistries captures the current global registry state and restores it on test
// cleanup. RegisterSparseCounter and RegisterLabeledBaseline mutate process-global slices
// (see baseline.go), so tests must isolate themselves from each other.
func snapshotRegistries(t *testing.T) {
	t.Helper()
	sparseCountersMu.Lock()
	savedSparse := append([]metric.Int64Counter(nil), sparseCounters...)
	sparseCounters = nil
	sparseCountersMu.Unlock()

	labeledBaselinesMu.Lock()
	savedLabeled := make([]func(context.Context), len(labeledBaselineFns))
	copy(savedLabeled, labeledBaselineFns)
	labeledBaselineFns = nil
	labeledBaselinesMu.Unlock()

	t.Cleanup(func() {
		sparseCountersMu.Lock()
		sparseCounters = savedSparse
		sparseCountersMu.Unlock()
		labeledBaselinesMu.Lock()
		labeledBaselineFns = savedLabeled
		labeledBaselinesMu.Unlock()
	})
}

// newTestMeter builds a MeterProvider with a manual reader for in-test metric collection.
func newTestMeter(t *testing.T) (metric.Meter, *sdkmetric.ManualReader) {
	t.Helper()
	reader := sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return provider.Meter("baseline_test"), reader
}

// findCounterByName returns the metricdata.Sum[int64] for the named instrument from a
// collected ResourceMetrics, or fails the test.
func findCounterByName(t *testing.T, rm metricdata.ResourceMetrics, name string) metricdata.Sum[int64] {
	t.Helper()
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				sum, ok := m.Data.(metricdata.Sum[int64])
				require.True(t, ok, "metric %q is not a Sum[int64]", name)
				return sum
			}
		}
	}
	t.Fatalf("counter %q not present in collected metrics", name)
	return metricdata.Sum[int64]{}
}

func TestRegisterSparseCounter_ReturnsSameCounter(t *testing.T) {
	snapshotRegistries(t)

	meter, _ := newTestMeter(t)
	c, err := meter.Int64Counter("test.sparse_pass_through")
	require.NoError(t, err)

	out := RegisterSparseCounter(c)
	assert.Same(t, c, out, "RegisterSparseCounter must return the same counter passed in")

	sparseCountersMu.Lock()
	defer sparseCountersMu.Unlock()
	require.Len(t, sparseCounters, 1)
	assert.Same(t, c, sparseCounters[0])
}

func TestEmitBaselines_EmitsZeroSampleForSparseCounter(t *testing.T) {
	snapshotRegistries(t)

	meter, reader := newTestMeter(t)
	c, err := meter.Int64Counter("test.sparse_emits_zero")
	require.NoError(t, err)
	RegisterSparseCounter(c)

	// Before EmitBaselines the counter has never been touched — no data point should
	// exist for the unlabeled series.
	var beforeRM metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &beforeRM))
	for _, sm := range beforeRM.ScopeMetrics {
		for _, m := range sm.Metrics {
			require.NotEqual(t, "test.sparse_emits_zero", m.Name,
				"counter must not appear in collection before EmitBaselines")
		}
	}

	EmitBaselines(t.Context())

	var afterRM metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &afterRM))
	sum := findCounterByName(t, afterRM, "test.sparse_emits_zero")
	require.Len(t, sum.DataPoints, 1, "expected exactly one unlabeled baseline sample")
	assert.Equal(t, int64(0), sum.DataPoints[0].Value, "baseline sample must be zero")
	assert.Equal(t, 0, sum.DataPoints[0].Attributes.Len(), "baseline sample must have no attributes")
}

func TestEmitBaselines_InvokesRegisteredLabeledFunctions(t *testing.T) {
	snapshotRegistries(t)

	meter, reader := newTestMeter(t)
	c, err := meter.Int64Counter("test.labeled_emits_zero")
	require.NoError(t, err)

	beaconAddrs := []string{"http://bn-a:5052", "http://bn-b:5052"}
	RegisterLabeledBaseline(func(ctx context.Context) {
		for _, addr := range beaconAddrs {
			c.Add(ctx, 0, metric.WithAttributes(attribute.String("ssv.beacon.client", addr)))
		}
	})

	EmitBaselines(t.Context())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	sum := findCounterByName(t, rm, "test.labeled_emits_zero")
	require.Len(t, sum.DataPoints, len(beaconAddrs),
		"expected one labeled baseline sample per configured beacon address")

	gotAddrs := make(map[string]int64)
	for _, dp := range sum.DataPoints {
		v, ok := dp.Attributes.Value("ssv.beacon.client")
		require.True(t, ok, "labeled baseline sample is missing ssv.beacon.client attribute")
		gotAddrs[v.AsString()] = dp.Value
	}
	for _, addr := range beaconAddrs {
		val, ok := gotAddrs[addr]
		assert.True(t, ok, "no baseline sample for beacon %q", addr)
		assert.Equal(t, int64(0), val, "baseline sample for %q must be zero", addr)
	}
}

// TestEmitBaselines_LabeledFunctionCanRegisterAnother locks the contract described by the
// "Copy the slice under lock, then invoke without the lock" comment in EmitBaselines.
// Without the defensive copy this would deadlock on labeledBaselinesMu.
func TestEmitBaselines_LabeledFunctionCanRegisterAnother(t *testing.T) {
	snapshotRegistries(t)

	var outerRan, nestedRegistered bool
	RegisterLabeledBaseline(func(ctx context.Context) {
		outerRan = true
		// Re-entrant registration: would deadlock if EmitBaselines held the lock while
		// invoking registered fns. The nested fn must NOT run during this EmitBaselines
		// call (it was registered after the slice snapshot was taken) — only the next.
		RegisterLabeledBaseline(func(context.Context) {
			nestedRegistered = true
		})
	})

	EmitBaselines(t.Context())
	assert.True(t, outerRan, "outer labeled baseline fn must run")
	assert.False(t, nestedRegistered, "nested fn must not run during the same EmitBaselines")

	// Second call should pick up the nested fn.
	EmitBaselines(t.Context())
	assert.True(t, nestedRegistered, "nested labeled baseline fn must run on the next EmitBaselines")
}

// TestEmitBaselines_RepeatedCallsAddZeroEachTime documents that EmitBaselines is safe to
// call more than once (each invocation just emits another Add(0), which does not change
// the counter value). See the docstring caveat about preferring once-at-startup.
func TestEmitBaselines_RepeatedCallsAddZeroEachTime(t *testing.T) {
	snapshotRegistries(t)

	meter, reader := newTestMeter(t)
	c, err := meter.Int64Counter("test.sparse_repeated_calls")
	require.NoError(t, err)
	RegisterSparseCounter(c)

	EmitBaselines(t.Context())
	EmitBaselines(t.Context())
	EmitBaselines(t.Context())

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))
	sum := findCounterByName(t, rm, "test.sparse_repeated_calls")
	require.Len(t, sum.DataPoints, 1, "all baseline emissions should fold into one unlabeled series")
	assert.Equal(t, int64(0), sum.DataPoints[0].Value, "value must remain zero across repeated EmitBaselines calls")
}

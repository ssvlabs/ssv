// Package metricstest reads OpenTelemetry metrics in tests.
package metricstest

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// SetGlobalReader makes a manual reader's meter provider the global one and returns the reader. Instruments a
// package creates at init come from otel.Meter's delegating proxy, so they forward to it. Call it from TestMain,
// before the tests run.
func SetGlobalReader() *sdkmetric.ManualReader {
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	return reader
}

// CounterValue returns the value of the Int64 counter name, summed over the data points that carry every
// attribute in attrs. Counters are cumulative, so tests compare the value before and after the action they
// exercise.
func CounterValue(t *testing.T, reader *sdkmetric.ManualReader, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()

	var total int64
	for _, m := range collect(t, reader, name) {
		sum, ok := m.Data.(metricdata.Sum[int64])
		require.True(t, ok, "expected Sum[int64], got %T", m.Data)
		for _, dp := range sum.DataPoints {
			if hasAttributes(dp.Attributes, attrs) {
				total += dp.Value
			}
		}
	}
	return total
}

// GaugeValue returns the value of the Int64 gauge name at the data point that carries every attribute in attrs,
// and whether there is one.
func GaugeValue(t *testing.T, reader *sdkmetric.ManualReader, name string, attrs ...attribute.KeyValue) (int64, bool) {
	t.Helper()

	for _, m := range collect(t, reader, name) {
		gauge, ok := m.Data.(metricdata.Gauge[int64])
		require.True(t, ok, "expected Gauge[int64], got %T", m.Data)
		for _, dp := range gauge.DataPoints {
			if hasAttributes(dp.Attributes, attrs) {
				return dp.Value, true
			}
		}
	}
	return 0, false
}

func collect(t *testing.T, reader *sdkmetric.ManualReader, name string) []metricdata.Metrics {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(t.Context(), &rm))

	var out []metricdata.Metrics
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				out = append(out, m)
			}
		}
	}
	return out
}

func hasAttributes(set attribute.Set, attrs []attribute.KeyValue) bool {
	for _, kv := range attrs {
		v, ok := set.Value(kv.Key)
		if !ok || v.String() != kv.Value.String() {
			return false
		}
	}
	return true
}

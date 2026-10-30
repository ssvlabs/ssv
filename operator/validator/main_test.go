package validator

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// pkgTestMetricReader exposes the package-global counters created at init time to tests that
// assert on them. Because otel.Meter() returns a delegating proxy, calling otel.SetMeterProvider
// before tests run is enough to make the package-globals forward to this reader.
var pkgTestMetricReader *sdkmetric.ManualReader

func TestMain(m *testing.M) {
	pkgTestMetricReader = sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(pkgTestMetricReader))
	otel.SetMeterProvider(provider)
	os.Exit(m.Run())
}

// counterValue returns the current value of the package-global Int64 counter `name`, summed over
// the data points that carry every attribute in attrs. Counters are cumulative, so tests compare
// the value before and after the action they exercise.
func counterValue(t *testing.T, name string, attrs ...attribute.KeyValue) int64 {
	t.Helper()

	var rm metricdata.ResourceMetrics
	require.NoError(t, pkgTestMetricReader.Collect(t.Context(), &rm))

	var total int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			sum, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok, "expected Sum[int64], got %T", m.Data)
			for _, dp := range sum.DataPoints {
				if hasAttributes(dp.Attributes, attrs) {
					total += dp.Value
				}
			}
		}
	}
	return total
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

package queue

import (
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// pkgTestMetricReader exposes the package-global counters created at init time
// (e.g. droppedMessagesMetric) for tests that need to assert on them. Because
// otel.Meter() returns a delegating proxy, calling otel.SetMeterProvider before
// tests run is enough to make the package-globals forward to this reader.
var pkgTestMetricReader *sdkmetric.ManualReader

func TestMain(m *testing.M) {
	pkgTestMetricReader = sdkmetric.NewManualReader()
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(pkgTestMetricReader))
	otel.SetMeterProvider(provider)
	os.Exit(m.Run())
}

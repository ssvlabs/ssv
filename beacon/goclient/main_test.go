package goclient

import (
	"os"
	"testing"

	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
)

// pkgTestMetricReader exposes the package-global counters created at init time (e.g.
// attestationDataRefetchSkippedCounter) to tests that assert on them. otel.Meter() returns a delegating
// proxy, so setting the provider before the tests run makes the package globals forward to this reader.
var pkgTestMetricReader *sdkmetric.ManualReader

func TestMain(m *testing.M) {
	pkgTestMetricReader = sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(pkgTestMetricReader)))
	os.Exit(m.Run())
}

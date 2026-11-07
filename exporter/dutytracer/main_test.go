package dutytracer

import (
	"os"
	"testing"

	sdkmetric "go.opentelemetry.io/otel/sdk/metric"

	"github.com/ssvlabs/ssv/observability/metrics/metricstest"
)

// pkgTestMetricReader reads the metrics the package creates at init time.
var pkgTestMetricReader *sdkmetric.ManualReader

func TestMain(m *testing.M) {
	pkgTestMetricReader = metricstest.SetGlobalReader()
	os.Exit(m.Run())
}

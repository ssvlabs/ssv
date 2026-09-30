package metrics

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/prometheus/common/expfmt"
	"github.com/prometheus/common/model"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"
)

// The Prometheus exporter is versioned separately from otel/sdk/metric; an sdk bump
// that breaks the exporter would still serve /metrics with 200 but drop measurements.
// Record through the production provider and assert the value shows up in the scrape.
func TestInitializeProvider_MeasurementReachesScrape(t *testing.T) {
	provider, shutdown, err := InitializeProvider(t.Context(), resource.Empty(), true)
	require.NoError(t, err)
	t.Cleanup(func() { _ = shutdown(context.Background()) })

	counter, err := provider.Meter("ssv.test").Int64Counter("ssv.test.scrape")
	require.NoError(t, err)
	counter.Add(t.Context(), 3)

	addr, _, err := NewHandler(zap.NewNop(), nil, false, stubHealthChecker{}).Start(t.Context(), http.NewServeMux(), "127.0.0.1:0")
	require.NoError(t, err)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get("http://" + addr + "/metrics")
	require.NoError(t, err)
	t.Cleanup(func() { _ = resp.Body.Close() })
	require.Equal(t, http.StatusOK, resp.StatusCode)

	parser := expfmt.NewTextParser(model.UTF8Validation)
	families, err := parser.TextToMetricFamilies(resp.Body)
	require.NoError(t, err)

	family, ok := families["ssv_test_scrape_total"]
	require.True(t, ok, "recorded counter missing from /metrics scrape")
	require.Len(t, family.GetMetric(), 1)
	require.Equal(t, float64(3), family.GetMetric()[0].GetCounter().GetValue())
}

package observability

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
)

const (
	testApp     = "ssv-test"
	testVersion = "1.0.0"
)

// TestInitialize verifies that Initialize returns a valid shutdown function.
func TestInitialize(t *testing.T) {
	t.Parallel()

	shutdown, err := Initialize(testApp, testVersion)

	require.NoError(t, err)
	require.NotNil(t, shutdown)

	require.NoError(t, shutdown(context.Background()))
}

// TestInitializeWithMetrics verifies that Initialize with metrics option sets up the meter provider.
func TestInitializeWithMetrics(t *testing.T) {
	t.Parallel()

	originalProvider := otel.GetMeterProvider()
	defer otel.SetMeterProvider(originalProvider)

	shutdown, err := Initialize(testApp, testVersion, WithMetrics())

	require.NoError(t, err)
	require.NotNil(t, shutdown)
	require.NotEqual(t, originalProvider, otel.GetMeterProvider())

	require.NoError(t, shutdown(context.Background()))
}

// TestInitializeShutdown verifies the returned shutdown function returns nil.
func TestInitializeShutdown(t *testing.T) {
	t.Parallel()

	shutdown, err := Initialize(testApp, testVersion)

	require.NoError(t, err)

	err = shutdown(context.Background())

	require.NoError(t, err)
}

// TestInitializeResourceError verifies error handling when resource creation fails.
func TestInitializeResourceError(t *testing.T) {
	t.Parallel()

	mockDeps := Dependencies{
		ResourceMerge: func(res *resource.Resource, other *resource.Resource) (*resource.Resource, error) {
			return nil, errors.New("resource merge error")
		},
		PrometheusNew: prometheus.New,
	}

	_, err := Initialize(testApp, testVersion, WithDependencies(mockDeps))
	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to instantiate observability resources")
}

// TestInitializePrometheusError verifies error handling when Prometheus exporter creation fails.
func TestInitializePrometheusError(t *testing.T) {
	t.Parallel()

	mockDeps := Dependencies{
		ResourceMerge: resource.Merge,
		PrometheusNew: func(opts ...prometheus.Option) (*prometheus.Exporter, error) {
			return nil, errors.New("prometheus error")
		},
	}

	_, err := Initialize(testApp, testVersion, WithMetrics(), WithDependencies(mockDeps))

	require.Error(t, err)
	require.Contains(t, err.Error(), "failed to instantiate metric Prometheus exporter")
}

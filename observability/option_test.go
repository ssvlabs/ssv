package observability

import (
	"go.opentelemetry.io/otel/exporters/prometheus"
	"go.opentelemetry.io/otel/sdk/resource"
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithMetrics verifies that WithMetrics properly sets metrics configuration.
func TestWithMetrics(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name            string
		initialEnabled  bool
		expectedEnabled bool
	}{
		{
			name:            "enables metrics when disabled",
			initialEnabled:  false,
			expectedEnabled: true,
		},
		{
			name:            "keeps metrics enabled when already enabled",
			initialEnabled:  true,
			expectedEnabled: true,
		},
	}

	for _, scenario := range testCases {
		t.Run(scenario.name, func(t *testing.T) {
			t.Parallel()

			cfg := &Config{metricsEnabled: scenario.initialEnabled}
			deps := &Dependencies{}
			opt := WithMetrics()
			opt(cfg, deps)

			require.True(t, cfg.metricsEnabled)
			require.Equal(t, scenario.expectedEnabled, cfg.metricsEnabled)
		})
	}
}

// TestWithDependencies verifies that WithDependencies properly sets custom dependencies.
func TestWithDependencies(t *testing.T) {
	t.Parallel()

	var resourceMergeCalled bool
	var prometheusNewCalled bool

	mockResourceMerge := func(res *resource.Resource, other *resource.Resource) (*resource.Resource, error) {
		resourceMergeCalled = true
		return nil, nil
	}

	mockPrometheusNew := func(opts ...prometheus.Option) (*prometheus.Exporter, error) {
		prometheusNewCalled = true
		return nil, nil
	}

	customDeps := Dependencies{
		ResourceMerge: mockResourceMerge,
		PrometheusNew: mockPrometheusNew,
	}

	cfg := &Config{}
	deps := &Dependencies{}

	opt := WithDependencies(customDeps)
	opt(cfg, deps)

	_, _ = deps.ResourceMerge(nil, nil)

	require.True(t, resourceMergeCalled)

	_, _ = deps.PrometheusNew()

	require.True(t, prometheusNewCalled)
}

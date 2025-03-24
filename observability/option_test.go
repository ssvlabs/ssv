package observability

import (
	"testing"

	"github.com/stretchr/testify/require"
)

// TestWithMetrics verifies that WithMetrics sets metricsEnabled to true.
func TestWithMetrics(t *testing.T) {
	t.Parallel()

	cfg := &Config{metricsEnabled: false}
	deps := &Dependencies{}
	opt := WithMetrics()
	opt(cfg, deps)

	require.True(t, cfg.metricsEnabled)
}

// TestWithMetricsAlreadyEnabled verifies that WithMetrics keeps metrics enabled.
func TestWithMetricsAlreadyEnabled(t *testing.T) {
	t.Parallel()

	cfg := &Config{metricsEnabled: true}
	deps := &Dependencies{}
	opt := WithMetrics()
	opt(cfg, deps)

	require.True(t, cfg.metricsEnabled)
}

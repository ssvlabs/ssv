package metricsreporter

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func Test_registerMetric(t *testing.T) {
	const totalMetrics = 67
	require.Equal(t, totalMetrics, len(allMetrics))
}

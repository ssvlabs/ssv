package observability

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
)

// TestNewMetric verifies NewMetric returns the metric when no error occurs.
func TestNewMetric(t *testing.T) {
	t.Parallel()

	metricValue := "test-metric"
	var err error = nil

	result := NewMetric(metricValue, err)

	require.Equal(t, metricValue, result)
}

// TestNewMetricError verifies NewMetric returns the metric even when an error occurs.
func TestNewMetricError(t *testing.T) {
	t.Parallel()

	metricValue := 123
	err := errors.New("test error")

	result := NewMetric(metricValue, err)

	require.Equal(t, metricValue, result)
}

// TestRecordUint64Value verifies RecordUint64Value correctly handles values within int64 range.
func TestRecordUint64Value(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		value         uint64
		shouldRecord  bool
		expectedValue int64
	}{
		{
			name:          "value within int64 range",
			value:         100,
			shouldRecord:  true,
			expectedValue: 100,
		},
		{
			name:          "value exceeds int64 range",
			value:         math.MaxInt64 + 1,
			shouldRecord:  false,
			expectedValue: 0,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var recordedValue int64
			recordF := func(ctx context.Context, value int64, options ...metric.RecordOption) {
				recordedValue = value
			}

			RecordUint64Value(context.TODO(), tc.value, recordF)

			if tc.shouldRecord {
				require.Equal(t, tc.value, uint64(recordedValue))
			} else {
				require.Equal(t, tc.expectedValue, recordedValue)
			}
		})
	}
}

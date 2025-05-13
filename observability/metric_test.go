package observability

import (
	"context"
	"errors"
	"math"
	"testing"

	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/metric"
)

// TestNewMetric verifies NewMetric always returns the metric regardless of error.
func TestNewMetric(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name  string
		value interface{}
		err   error
	}{
		{
			name:  "no error",
			value: "test-metric",
			err:   nil,
		},
		{
			name:  "with error",
			value: 123,
			err:   errors.New("test error"),
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			result := NewMetric(tc.value, tc.err)

			require.Equal(t, tc.value, result)
		})
	}
}

// TestRecordUint64Value_RangeHandling verifies RecordUint64Value correctly handles values within int64 range.
func TestRecordUint64Value_RangeHandling(t *testing.T) {
	t.Parallel()

	testCases := []struct {
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

	for _, tc := range testCases {
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

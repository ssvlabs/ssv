package observability

import (
	"context"
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

func Test_GivenValueThatDoesNotExceedMaxInt64_WhenRecordUint64Value_ThenRecords(t *testing.T) {
	var recordedValue int64
	recordF := func(ctx context.Context, value int64, options ...metric.RecordOption) {
		recordedValue = value
	}

	var val uint64 = 100
	RecordUint64Value(t.Context(), val, recordF)

	assert.Equal(t, val, uint64(recordedValue))
}

func Test_GivenValueThatExceedsMaxInt64_WhenRecordUint64Value_ThenDoesNotRecord(t *testing.T) {
	initLogger(zap.NewNop())

	var recordedValue int64
	recordF := func(ctx context.Context, value int64, options ...metric.RecordOption) {
		recordedValue = value
	}

	var val uint64 = math.MaxInt64 + 1
	RecordUint64Value(t.Context(), val, recordF)

	assert.Equal(t, int64(0), recordedValue)
}

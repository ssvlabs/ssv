package metrics

import (
	"context"
	"math"

	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"
)

var (
	logger                  *zap.Logger
	SecondsHistogramBuckets = []float64{0, 0.001, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
)

func InitLogger(l *zap.Logger) {
	logger = l
}

func New[T any](metric T, err error) T {
	if err != nil {
		logger.Error("failed to instantiate metric", zap.Error(err))
	}
	return metric
}

func RecordUint64Value(ctx context.Context,
	value uint64,
	recordF func(ctx context.Context, value int64, options ...metric.RecordOption),
	options ...metric.RecordOption) {
	if value > math.MaxInt64 {
		logger.Error("value exceeds int64 range, metric not recorded")
		return
	}

	recordF(ctx, int64(value), options...)
}

package observability

import "go.uber.org/zap"

var (
	SecondsHistogramBuckets = []float64{0, 0.005, 0.01, 0.025, 0.05, 0.075, 0.1, 0.25, 0.5, 0.75, 1, 2.5, 5, 7.5, 10}
)

func GetMetric[T any](metricName string, create func(metricName string) (T, error)) T {
	logger := zap.L()
	metric, err := create(metricName)
	if err != nil {
		logger.Error("failed to instantiate metric",
			zap.String("metric_name", metricName),
			zap.Error(err))
	}
	return metric
}

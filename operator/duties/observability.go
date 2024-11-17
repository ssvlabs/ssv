package duties

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/operator/duties"
	observabilityComponentNamespace = "ssv.operator.duty_scheduler"
)

var (
	meter = otel.Meter(observabilityComponentName)

	slotDelayHistogram = observability.GetMetric(
		fmt.Sprintf("%s.slot_ticker_delay.duration", observabilityComponentNamespace),
		func(metricName string) (metric.Float64Histogram, error) {
			return meter.Float64Histogram(
				metricName,
				metric.WithUnit("s"),
				metric.WithDescription("delay of the slot ticker in seconds"),
				metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...))
		},
	)

	dutiesExecutedCounter = observability.GetMetric(
		fmt.Sprintf("%s.executions", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{duty}"),
				metric.WithDescription("total number of duties executed by scheduler"))
		},
	)

	committeeDutiesExecutedCounter = observability.GetMetric(
		fmt.Sprintf("%s.committee_executions", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{committee_duty}"),
				metric.WithDescription("total number of committee duties executed by scheduler"))
		},
	)
)

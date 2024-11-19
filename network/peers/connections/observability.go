package connections

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/network/peers/connections"
	observabilityComponentNamespace = "ssv.p2p.connection"
)

var (
	meter = otel.Meter(observabilityComponentName)

	connectedCounter = observability.GetMetric(
		fmt.Sprintf("%s.connected", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{connection}"),
				metric.WithDescription("total number of connected peers"))
		},
	)

	disconnectedCounter = observability.GetMetric(
		fmt.Sprintf("%s.disconnected", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{connection}"),
				metric.WithDescription("total number of disconnected peers"))
		},
	)

	filteredCounter = observability.GetMetric(
		fmt.Sprintf("%s.filtered", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{connection}"),
				metric.WithDescription("total number of filtered connections"))
		},
	)
)

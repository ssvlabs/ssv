package connections

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName = "github.com/ssvlabs/ssv/network/peers/connections"
	observabilityNamespace     = "ssv.p2p.connection"
)

var (
	meter = otel.Meter(observabilityComponentName)

	connectedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("connected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of connected peers")))

	disconnectedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("disconnected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of disconnected peers")))

	filteredCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("filtered"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of filtered connections")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

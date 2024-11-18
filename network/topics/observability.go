package topics

import (
	"fmt"
	"strconv"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/network/topics"
	observabilityComponentNamespace = "ssv.p2p.message"
)

var (
	meter = otel.Meter(observabilityComponentName)

	pubSubInboundCounter = observability.GetMetric(
		fmt.Sprintf("%s.in", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{message}"),
				metric.WithDescription("total number of inbound messages"))
		},
	)

	pubSubOutboundCounter = observability.GetMetric(
		fmt.Sprintf("%s.out", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{message}"),
				metric.WithDescription("total number of outbound(broadcasted) messages"))
		},
	)
)

func messageTypeAttribute(value uint64) attribute.KeyValue {
	eventNameAttrName := fmt.Sprintf("%s.type", observabilityComponentNamespace)
	return attribute.String(eventNameAttrName, strconv.FormatUint(value, 10))
}

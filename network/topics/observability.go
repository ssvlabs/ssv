package topics

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/topics"
	observabilityNamespace = "ssv.p2p.messages"
)

var (
	meter = otel.Meter(observabilityName)

	inboundMessageCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("in"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of inbound messages")))

	outboundMessageCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("out"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of outbound(broadcasted) messages")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func messageTypeAttribute(value uint64) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.p2p.message.type",
		Value: observability.Uint64AttributeValue(value),
	}
}

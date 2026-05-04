package topics

import (
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/topics"
	observabilityNamespace = "ssv.p2p.messages"
)

var (
	meter = otel.Meter(observabilityName)

	inboundMessageCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "in"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of inbound messages")))

	outboundMessageCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "out"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of outbound(broadcasted) messages")))

	msgIDHandlerBufferFallbackCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "msg_id_buffer_fallback"),
			metric.WithUnit("{event}"),
			metric.WithDescription("total number of msg_id add operations processed synchronously because the async buffer was full")))
)

func messageTopicAttribute(value string) attribute.KeyValue {
	return attribute.String("ssv.p2p.message.topic", value)
}

func messageTypeAttribute(value uint64) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.p2p.message.type",
		Value: observability.Uint64AttributeValue(value),
	}
}

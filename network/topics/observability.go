package topics

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/topics"
	observabilityNamespace = "ssv.p2p.messages"

	pubsubObservabilityNamespace = "ssv.p2p.pubsub.messages"
	pubsubTopicAttributeKey      = "ssv.p2p.pubsub.topic"
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

	pubsubMessagesReceivedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(pubsubObservabilityNamespace, "received"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of messages delivered to the pubsub topic validator, before SSV validation runs (compare with ssv_p2p_messages_in_total for the post-validation rate)")))

	msgIDHandlerBufferFallbackCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "msg_id_buffer_fallback"),
			metric.WithUnit("{event}"),
			metric.WithDescription("total number of msg_id add operations processed synchronously because the async buffer was full")))
)

func pubsubTopicAttribute(value string) attribute.KeyValue {
	return attribute.String(pubsubTopicAttributeKey, value)
}

func messageTopicAttribute(value string) attribute.KeyValue {
	return attribute.String("ssv.p2p.message.topic", value)
}

func messageTypeAttribute(value uint64) attribute.KeyValue {
	return attribute.KeyValue{
		Key:   "ssv.p2p.message.type",
		Value: observability.Uint64AttributeValue(value),
	}
}

// recordPubsubMessageReceived is called from the topic validator wrapper before the inner SSV
// validator runs, so the counter increments for every message libp2p hands to the validator
// regardless of validation outcome (accept/ignore/reject/timeout).
func recordPubsubMessageReceived(ctx context.Context, topic string) {
	pubsubMessagesReceivedCounter.Add(ctx, 1, metric.WithAttributes(pubsubTopicAttribute(topic)))
}

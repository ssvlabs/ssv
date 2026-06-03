package topics

import (
	"context"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/topics"
	observabilityNamespace = "ssv.p2p.messages"

	pubsubObservabilityNamespace       = "ssv.p2p.pubsub.messages"
	pubsubTopicAttributeKey            = "ssv.p2p.pubsub.topic"
	pubsubValidationResultAttributeKey = "ssv.p2p.pubsub.validation.result"
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
			metric.WithDescription("total number of messages received by the pubsub topic validator")))

	pubsubMessagesValidatedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(pubsubObservabilityNamespace, "validated"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of messages completed by the pubsub topic validator by result")))

	pubsubMessageValidationDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(pubsubObservabilityNamespace, "validation_duration"),
			metric.WithUnit("s"),
			metric.WithDescription("pubsub topic validator duration by result"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	pubsubMessageHandlerErrorsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(pubsubObservabilityNamespace, "handler_errors"),
			metric.WithUnit("{error}"),
			metric.WithDescription("total number of messages accepted by pubsub validation but failed by the topic message handler")))

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

func recordPubsubMessageReceived(ctx context.Context, topic string) {
	pubsubMessagesReceivedCounter.Add(ctx, 1, metric.WithAttributes(pubsubTopicAttribute(topic)))
}

func pubsubValidationResultAttribute(value string) attribute.KeyValue {
	return attribute.String(pubsubValidationResultAttributeKey, value)
}

func recordPubsubMessageValidated(ctx context.Context, topic string, result string, dur time.Duration) {
	attrs := metric.WithAttributes(pubsubTopicAttribute(topic), pubsubValidationResultAttribute(result))
	pubsubMessagesValidatedCounter.Add(ctx, 1, attrs)
	pubsubMessageValidationDurationHistogram.Record(ctx, dur.Seconds(), attrs)
}

func recordPubsubMessageHandlerError(ctx context.Context, topic string) {
	pubsubMessageHandlerErrorsCounter.Add(ctx, 1, metric.WithAttributes(pubsubTopicAttribute(topic)))
}

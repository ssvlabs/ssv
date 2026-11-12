package dutytracer

import (
	"context"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/exporter/dutytracer"
	observabilityNamespace = "ssv.dutytracer"
)

// trace kinds reported via tracerLostTracesCounter.
const (
	traceKindCommittee = "committee"
	traceKindValidator = "validator"
)

var (
	meter = otel.Meter(observabilityName)

	tracerInFlightMessageCounter = metrics.New(
		meter.Int64Counter(
			metricName("messages.received"),
			metric.WithDescription("total number of messages received tracer intercepted")))

	tracerFailedMessageCounter = metrics.New(
		meter.Int64Counter(
			metricName("messages.failed"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of messages the tracer failed to record")))

	tracerDroppedPendingSignaturesCounter = metrics.New(
		meter.Int64Counter(
			metricName("pending_signatures.dropped"),
			metric.WithUnit("{signature}"),
			metric.WithDescription("total number of post-consensus signatures dropped because their signing root was never classified")))

	tracerLostTracesCounter = metrics.New(
		meter.Int64Counter(
			metricName("traces.lost"),
			metric.WithUnit("{trace}"),
			metric.WithDescription("total number of evicted traces that could not be saved to disk")))

	tracerInFlightMessageHist = metrics.New(
		meter.Float64Histogram(
			metricName("messages.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("message processing duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))

	tracerDBDurationHistogram = metrics.New(
		meter.Float64Histogram(
			metricName("db.duration"),
			metric.WithUnit("s"),
			metric.WithDescription("db interaction duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

// messageAttributes labels a message counter by SSV message type and runner role.
func messageAttributes(msg *queue.SSVMessage) metric.MeasurementOption {
	return metric.WithAttributes(
		observability.ValidatorMsgTypeAttribute(msg.MsgType),
		observability.RunnerRoleAttribute(msg.MsgID.GetRoleType()),
	)
}

func recordFailedMessage(ctx context.Context, msg *queue.SSVMessage) {
	tracerFailedMessageCounter.Add(ctx, 1, messageAttributes(msg))
}

func recordDroppedPendingSignatures(count int, role spectypes.RunnerRole) {
	tracerDroppedPendingSignaturesCounter.Add(context.Background(), int64(count),
		metric.WithAttributes(observability.RunnerRoleAttribute(role)))
}

func recordLostTraces(count int, kind string) {
	tracerLostTracesCounter.Add(context.Background(), int64(count),
		metric.WithAttributes(attribute.String("ssv.dutytracer.trace.kind", kind)))
}

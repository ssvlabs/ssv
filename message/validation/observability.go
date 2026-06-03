package validation

import (
	"context"
	"time"

	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/message/validation"
	observabilityNamespace = "ssv.p2p.message.validations"
)

var (
	meter = otel.Meter(observabilityName)

	messageValidationsAcceptedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "accepted"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages successfully validated and accepted")))

	// TODO(audit): classification uncertain — depends on validation failure rate which
	// in turn depends on network health and version skew. Re-evaluate against production
	// data. Mis-classifying as sparse is harmless for PromQL but should reflect intent.
	messageValidationsIgnoredCounter = metrics.RegisterSparseCounter(metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "ignored"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages that failed validation and were ignored"))))

	// TODO(audit): see note on messageValidationsIgnoredCounter — same caveat applies.
	messageValidationsRejectedCounter = metrics.RegisterSparseCounter(metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "rejected"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages that failed validation and were rejected"))))

	messageValidationDurationHistogram = metrics.New(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "duration"),
			metric.WithUnit("s"),
			metric.WithDescription("message validation duration"),
			metric.WithExplicitBucketBoundaries(metrics.SecondsHistogramBuckets...)))
)

func reasonAttribute(reason string) attribute.KeyValue {
	return attribute.String("ssv.p2p.message.validation.discard_reason", reason)
}

func recordAcceptedMessage(ctx context.Context, role types.RunnerRole) {
	messageValidationsAcceptedCounter.Add(ctx, 1, metric.WithAttributes(observability.RunnerRoleAttribute(role)))
}

func recordRejectedMessage(ctx context.Context, role types.RunnerRole, reason string) {
	messageValidationsRejectedCounter.Add(ctx, 1, metric.WithAttributes(reasonAttribute(reason), observability.RunnerRoleAttribute(role)))
}

func recordIgnoredMessage(ctx context.Context, role types.RunnerRole, reason string) {
	messageValidationsIgnoredCounter.Add(ctx, 1, metric.WithAttributes(reasonAttribute(reason), observability.RunnerRoleAttribute(role)))
}

func recordMessageDuration(ctx context.Context, role types.RunnerRole, dur time.Duration) {
	messageValidationDurationHistogram.Record(ctx, dur.Seconds(), metric.WithAttributes(observability.RunnerRoleAttribute(role)))
}

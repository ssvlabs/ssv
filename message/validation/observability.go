package validation

import (
	"context"

	"github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/message/validation"
	observabilityNamespace = "ssv.p2p.message.validations"
)

var (
	meter = otel.Meter(observabilityName)

	messageValidationsCounter = observability.NewMetric(
		meter.Int64Counter(
			observabilityNamespace,
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages validated")))

	messageValidationsAcceptedCounter = observability.NewMetric(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "accepted"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages successfully validated and accepted")))

	messageValidationsIgnoredCounter = observability.NewMetric(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "ignored"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages that failed validation and were ignored")))

	messageValidationsRejectedCounter = observability.NewMetric(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "rejected"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages that failed validation and were rejected")))

	messageValidationDurationHistogram = observability.NewMetric(
		meter.Float64Histogram(
			observability.InstrumentName(observabilityNamespace, "duration"),
			metric.WithUnit("s"),
			metric.WithDescription("message validation duration"),
			metric.WithExplicitBucketBoundaries(observability.SecondsHistogramBuckets...)))
)

func reasonAttribute(reason string) attribute.KeyValue {
	return attribute.String("ssv.p2p.message.validation.discard_reason", reason)
}

func recordMessage(ctx context.Context) {
	messageValidationsCounter.Add(ctx, 1)
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

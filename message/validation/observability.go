package validation

import (
	"context"

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

	messageValidationsIgnoredCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "ignored"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages that failed validation and were ignored")))

	messageValidationsRejectedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "rejected"),
			metric.WithUnit("{message_validation}"),
			metric.WithDescription("total number of messages that failed validation and were rejected")))

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

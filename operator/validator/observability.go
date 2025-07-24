package validator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/validator"
	observabilityNamespace = "ssv.validator"
)

var (
	tracer = otel.Tracer(observabilityName)
	meter  = otel.Meter(observabilityName)

	validatorStatusGauge = observability.NewMetric(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "validators.per_status"),
			metric.WithDescription("total number of validators by status")))

	validatorsRemovedCounter = observability.NewMetric(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "validators.removed"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("total number of validator errors")))

	validatorErrorsCounter = observability.NewMetric(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "errors"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("total number of validator errors")))
)

func validatorStatusAttribute(value registrystorage.ValidatorStatus) attribute.KeyValue {
	attrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(attrName, string(value))
}

func recordValidatorStatus(ctx context.Context, count uint32, status registrystorage.ValidatorStatus) {
	validatorStatusGauge.Record(ctx, int64(count),
		metric.WithAttributes(validatorStatusAttribute(status)),
	)
}

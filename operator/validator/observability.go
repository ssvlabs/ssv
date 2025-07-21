package validator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/validator"
	observabilityNamespace = "ssv.validator"
)

type validatorStatus string

const (
	statusParticipating validatorStatus = "participating"
	statusNotFound      validatorStatus = "not_found"
	statusActive        validatorStatus = "active"
	statusSlashed       validatorStatus = "slashed"
	statusExiting       validatorStatus = "exiting"
	statusNotActivated  validatorStatus = "not_activated"
	statusPending       validatorStatus = "pending"
	statusNoIndex       validatorStatus = "no_index"
	statusUnknown       validatorStatus = "unknown"
)

var (
	tracer = otel.Tracer(observabilityName)
	meter  = otel.Meter(observabilityName)

	validatorStatusGauge = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "validators.per_status"),
			metric.WithDescription("total number of validators by status")))

	validatorsRemovedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "validators.removed"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("total number of validator errors")))

	validatorErrorsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "errors"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("total number of validator errors")))
)

func validatorStatusAttribute(value validatorStatus) attribute.KeyValue {
	attrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(attrName, string(value))
}

func recordValidatorStatus(ctx context.Context, count uint32, status validatorStatus) {
	validatorStatusGauge.Record(ctx, int64(count),
		metric.WithAttributes(validatorStatusAttribute(status)),
	)
}

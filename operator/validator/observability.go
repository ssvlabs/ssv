package validator

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
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
	meter = otel.Meter(observabilityName)

	validatorStatusGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("validators.per_status"),
			metric.WithDescription("total number of validators by status")))

	validatorsRemovedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("validators.removed"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("total number of validator errors")))

	validatorErrorsCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("errors"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("total number of validator errors")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func validatorStatusAttribute(value validatorStatus) attribute.KeyValue {
	attrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(attrName, string(value))
}

func recordValidatorStatus(ctx context.Context, count uint32, status validatorStatus) {
	validatorStatusGauge.Record(ctx, int64(count),
		metric.WithAttributes(validatorStatusAttribute(status)),
	)
}

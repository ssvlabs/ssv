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
	statusError        validatorStatus = "error"
	statusNotFound     validatorStatus = "not_found"
	statusReady        validatorStatus = "ready"
	statusSlashed      validatorStatus = "slashed"
	statusExiting      validatorStatus = "exiting"
	statusNotActivated validatorStatus = "not_activated"
	statusPending      validatorStatus = "pending"
	statusNoIndex      validatorStatus = "no_index"
	statusRemoved      validatorStatus = "removed"
	statusUnknown      validatorStatus = "unknown"
)

var (
	meter = otel.Meter(observabilityName)

	validatorStatusCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("validators.per_status"),
			metric.WithDescription("total number of validators by status")))

	validatorsCountGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("validators.active"),
			metric.WithDescription("number of active validators")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func validatorStatusAttribute(value validatorStatus) attribute.KeyValue {
	attrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(attrName, string(value))
}

func recordValidatorStatus(ctx context.Context, status validatorStatus) {
	validatorStatusCounter.Add(ctx, 1,
		metric.WithAttributes(validatorStatusAttribute(status)),
	)
}

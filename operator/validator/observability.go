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

	validatorStatusGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("status"),
			metric.WithDescription("validator status")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func validatorStatusAttribute(value validatorStatus) attribute.KeyValue {
	attrName := fmt.Sprintf("%s.status", observabilityNamespace)
	return attribute.String(attrName, string(value))
}

func recordValidatorStatus(ctx context.Context, status validatorStatus) {
	resetExecutionClientStatusGauge(ctx)

	validatorStatusGauge.Record(ctx, 1,
		metric.WithAttributes(validatorStatusAttribute(status)),
	)
}

func resetExecutionClientStatusGauge(ctx context.Context) {
	for _, status := range []validatorStatus{
		statusError,
		statusNotFound,
		statusReady,
		statusSlashed,
		statusExiting,
		statusNotActivated,
		statusPending,
		statusNoIndex,
		statusRemoved,
		statusUnknown} {
		validatorStatusGauge.Record(ctx, 0,
			metric.WithAttributes(validatorStatusAttribute(status)),
		)
	}
}

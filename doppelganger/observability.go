package doppelganger

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/doppelganger"
	observabilityNamespace = "ssv.doppelganger"
)

var (
	meter = otel.Meter(observabilityName)

	validatorsStateGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("validators.state"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("Tracks the current number of validators in Doppelganger state, categorized by safety"),
		),
	)
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func unsafeAttribute(isUnsafe bool) attribute.KeyValue {
	const attrName = observabilityNamespace + ".validators.unsafe"
	return attribute.Bool(attrName, isUnsafe)
}

package doppelganger

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/doppelganger"
	observabilityNamespace = "ssv.doppelganger"
)

var (
	meter = otel.Meter(observabilityName)

	validatorsStateCounter = observability.NewMetric(
		meter.Int64UpDownCounter(
			metricName("validators.state.count"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("Tracks the total number of validators in Doppelganger state, categorized by safety"),
		),
	)
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func unsafeAttribute(isUnsafe bool) attribute.KeyValue {
	const attrName = observabilityNamespace + ".unsafe"
	return attribute.Bool(attrName, isUnsafe)
}

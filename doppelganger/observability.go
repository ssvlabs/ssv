package doppelganger

import (
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
			observability.InstrumentName(observabilityNamespace, "validators.state"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("Tracks the current number of validators in Doppelganger state, categorized by safety"),
		),
	)
)

func unsafeAttribute(isUnsafe bool) attribute.KeyValue {
	const attrName = observabilityNamespace + ".validators.unsafe"
	return attribute.Bool(attrName, isUnsafe)
}

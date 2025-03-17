package doppelganger

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/operator/doppelganger"
	observabilityNamespace = "ssv.doppelganger"
)

var (
	meter = otel.Meter(observabilityName)

	// Tracks the total number of validators currently in the Doppelganger state map
	validatorsStateCounter = observability.NewMetric(
		meter.Int64UpDownCounter(
			metricName("validators.state_count"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("Total number of validators currently tracked in the Doppelganger state"),
		),
	)

	// Tracks the number of unsafe validators currently under Doppelganger protection
	unsafeValidatorsCounter = observability.NewMetric(
		meter.Int64UpDownCounter(
			metricName("protection.unsafe_validators"),
			metric.WithUnit("{validator}"),
			metric.WithDescription("Number of validators currently under Doppelganger protection"),
		),
	)
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

package queue

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
	"github.com/ssvlabs/ssv/observability/utils"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	observabilityNamespace = "ssv.queue"
)

var (
	meter = otel.Meter(observabilityName)

	// ValidatorQueuesInboxSizeMetric keeps track of message queue size(s) across all validator-related message queues.
	// This metric is meant to be shared across many queues differentiating between them via an added "id" attribute.
	ValidatorQueuesInboxSizeMetric = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "validator_queues_inbox_size"),
			metric.WithUnit("{size}"),
			metric.WithDescription("the latest observed validator queue(s) size")),
	)

	// CommitteeQueuesInboxSizeMetric keeps track of message queue size(s) across all committee-related message queues.
	// This metric is meant to be shared across many queues differentiating between them via an added "id" attribute.
	CommitteeQueuesInboxSizeMetric = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "committee_queues_inbox_size"),
			metric.WithUnit("{size}"),
			metric.WithDescription("the latest observed committee queue(s) size")),
	)
)

// ValidatorMetricID returns a queue identifier to differentiate validator-related queues (in metrics).
// Runner-role is the only parameter we differentiate by.
func ValidatorMetricID(runnerRole spectypes.RunnerRole) string {
	return utils.FormatRunnerRole(runnerRole)
}

// CommitteeMetricID returns a queue identifier to differentiate committee-related queues (in metrics).
// We are splitting all committee-related queues into 32 buckets, this allows us to observe the properties
// of the last ~32 queues (this corresponds to ~1 epoch).
func CommitteeMetricID(slot phase0.Slot) string {
	slotInEpoch := slot % 32
	return fmt.Sprintf("%d", slotInEpoch)
}

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

	// InboxSizeMetric keeps track of message queue size(s) across all validator/committee related message queues.
	// This metric is meant to be shared across many queues differentiating between them via an added "queue_type" and
	// "queue_id" attributes.
	InboxSizeMetric = metrics.New(
		meter.Int64Gauge(
			observability.InstrumentName(observabilityNamespace, "inbox_size"),
			metric.WithUnit("{size}"),
			metric.WithDescription("the latest observed inbox size (for some queue)"),
		),
	)

	droppedMessagesMetric = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "messages.dropped"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of dropped queue messages by queue and reason"),
		),
	)

	// purgedMessagesMetric counts stale-purged messages, kept separate from droppedMessagesMetric so
	// that counter stays a pure fault signal: purges happen routinely at duty starts, so folding them
	// into messages.dropped would make any alert summing it fire continuously on a healthy node.
	purgedMessagesMetric = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "messages.purged"),
			metric.WithUnit("{message}"),
			metric.WithDescription("total number of purged (stale) queue messages by queue and reason"),
		),
	)
)

const (
	ValidatorQueueMetricType           = "validator"
	CommitteeQueueMetricType           = "committee"
	AggregatorCommitteeQueueMetricType = "aggregator_committee"

	// DropReasonBufferFull marks a message dropped because the queue was full — a fault/overload
	// signal, counted under messages.dropped.
	DropReasonBufferFull = "buffer_full"
	// PurgeReasonStale marks a queued message purged because its slot fell below the runner's floor.
	// Counted under messages.purged (not messages.dropped), see purgedMessagesMetric.
	PurgeReasonStale = "stale"
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

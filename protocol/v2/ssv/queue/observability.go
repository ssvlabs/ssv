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

func ValidatorMetricID(runnerRole spectypes.RunnerRole) string {
	return utils.FormatRunnerRole(runnerRole)
}

func CommitteeMetricID(slot phase0.Slot) string {
	slotInEpoch := slot % 32
	return fmt.Sprintf("%d", slotInEpoch)
}

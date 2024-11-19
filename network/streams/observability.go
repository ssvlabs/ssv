package streams

import (
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/network/streams"
	observabilityComponentNamespace = "ssv.p2p.stream"
)

var (
	meter = otel.Meter(observabilityComponentName)

	requestsSentCounter = observability.GetMetric(
		fmt.Sprintf("%s.requests.sent", observabilityComponentNamespace), //add attribute: incoming, outgoing
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{request}"),
				metric.WithDescription("total number of stream requests sent"))
		},
	)

	requestsReceivedCounter = observability.GetMetric(
		fmt.Sprintf("%s.requests.received", observabilityComponentNamespace), //add attribute: incoming, outgoing
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{request}"),
				metric.WithDescription("total number of stream requests received"))
		},
	)

	responsesSentCounter = observability.GetMetric(
		fmt.Sprintf("%s.responses.sent", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{response}"),
				metric.WithDescription("total number of stream responses sent(as response to a peer request)"))
		},
	)

	responsesReceivedCounter = observability.GetMetric(
		fmt.Sprintf("%s.responses.received", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Counter, error) {
			return meter.Int64Counter(
				metricName,
				metric.WithUnit("{response}"),
				metric.WithDescription("total number of stream responses received(as response to initiated by us request)"))
		},
	)
)

func protocolIDAttribute(id protocol.ID) attribute.KeyValue {
	const attrName = "ssv.p2p.protocol.id"
	return attribute.String(attrName, string(id))
}

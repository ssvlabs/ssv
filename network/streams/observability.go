package streams

import (
	"fmt"

	"github.com/libp2p/go-libp2p/core/protocol"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/streams"
	observabilityNamespace = "ssv.p2p.stream"
)

var (
	meter = otel.Meter(observabilityName)

	requestsSentCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("requests.sent"),
			metric.WithUnit("{request}"),
			metric.WithDescription("total number of stream requests sent")))

	requestsReceivedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("requests.received"),
			metric.WithUnit("{request}"),
			metric.WithDescription("total number of stream requests received")))

	responsesSentCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("responses.sent"),
			metric.WithUnit("{response}"),
			metric.WithDescription("total number of stream responses sent(as response to a peer request)")))

	responsesReceivedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("responses.received"),
			metric.WithUnit("{response}"),
			metric.WithDescription("total number of stream responses received(as response to initiated by us request)")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func protocolIDAttribute(id protocol.ID) attribute.KeyValue {
	const attrName = "ssv.p2p.protocol.id"
	return attribute.String(attrName, string(id))
}

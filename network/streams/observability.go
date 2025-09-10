package streams

import (
	"github.com/libp2p/go-libp2p/core/protocol"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/streams"
	observabilityNamespace = "ssv.p2p.stream"
)

var (
	meter = otel.Meter(observabilityName)

	requestsSentCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "requests.sent"),
			metric.WithUnit("{request}"),
			metric.WithDescription("total number of stream requests sent")))

	requestsReceivedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "requests.received"),
			metric.WithUnit("{request}"),
			metric.WithDescription("total number of stream requests received")))

	responsesSentCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "responses.sent"),
			metric.WithUnit("{response}"),
			metric.WithDescription("total number of stream responses sent(as response to a peer request)")))

	responsesReceivedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "responses.received"),
			metric.WithUnit("{response}"),
			metric.WithDescription("total number of stream responses received(as response to initiated by us request)")))
)

func protocolIDAttribute(id protocol.ID) attribute.KeyValue {
	const attrName = "ssv.p2p.protocol.id"
	return attribute.String(attrName, string(id))
}

package streams

import (
	"context"

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

	streamOperationAttribute   = "ssv.p2p.stream.operation"
	streamErrorReasonAttribute = "ssv.p2p.stream.error.reason"
)

const (
	streamOperationDial          = "dial"
	streamOperationWriteRequest  = "write_request"
	streamOperationCloseWrite    = "close_write"
	streamOperationReadResponse  = "read_response"
	streamOperationReadRequest   = "read_request"
	streamOperationWriteResponse = "write_response"

	streamErrorReasonError            = "error"
	streamErrorReasonOversizedPayload = "oversized_payload"
	streamErrorReasonReset            = "reset"
	streamErrorReasonTimeout          = "timeout"
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

	oversizedPayloadsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "payloads.oversized"),
			metric.WithUnit("{payload}"),
			metric.WithDescription("total number of oversized stream payloads rejected")))

	streamErrorsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "errors"),
			metric.WithUnit("{error}"),
			metric.WithDescription("total number of p2p stream errors by protocol, operation, and reason")))
)

func protocolIDAttribute(id protocol.ID) attribute.KeyValue {
	const attrName = "ssv.p2p.protocol.id"
	return attribute.String(attrName, string(id))
}

func streamDirectionAttribute(direction string) attribute.KeyValue {
	const attrName = "ssv.p2p.stream.direction"
	return attribute.String(attrName, direction)
}

func recordStreamError(ctx context.Context, id protocol.ID, operation string, reason string) {
	streamErrorsCounter.Add(ctx, 1, metric.WithAttributes(
		protocolIDAttribute(id),
		attribute.String(streamOperationAttribute, operation),
		attribute.String(streamErrorReasonAttribute, reason),
	))
}

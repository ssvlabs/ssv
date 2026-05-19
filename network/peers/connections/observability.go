package connections

import (
	"context"

	"github.com/libp2p/go-libp2p/core/network"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
	"github.com/ssvlabs/ssv/observability/metrics"
)

const (
	observabilityComponentName = "github.com/ssvlabs/ssv/network/peers/connections"
	observabilityNamespace     = "ssv.p2p.connections"
)

var (
	meter = otel.Meter(observabilityComponentName)

	connectedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "connected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of connected peers")))

	disconnectedCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "disconnected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of disconnected peers")))

	filteredCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "filtered"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of filtered connections")))

	connectionGaterDecisionsCounter = metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "gater_decisions"),
			metric.WithUnit("{decision}"),
			metric.WithDescription("total number of connection gater decisions by phase, decision, reason, direction, and highlighted peer status")))
)

func recordConnected(ctx context.Context, direction network.Direction) {
	connectedCounter.Add(ctx, 1,
		metric.WithAttributes(observability.NetworkDirectionAttribute(direction)))
}

func recordDisconnected(ctx context.Context, direction network.Direction) {
	disconnectedCounter.Add(ctx, 1,
		metric.WithAttributes(observability.NetworkDirectionAttribute(direction)))
}

func recordFiltered(ctx context.Context, direction network.Direction) {
	filteredCounter.Add(ctx, 1,
		metric.WithAttributes(observability.NetworkDirectionAttribute(direction)))
}

func recordConnectionGaterDecision(
	ctx context.Context,
	phase string,
	decision string,
	reason string,
	direction network.Direction,
	highlighted bool,
) {
	connectionGaterDecisionsCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("ssv.p2p.connection.gater.phase", phase),
		attribute.String("ssv.p2p.connection.gater.decision", decision),
		attribute.String("ssv.p2p.connection.gater.reason", reason),
		observability.NetworkDirectionAttribute(direction),
		attribute.Bool("ssv.p2p.connection.gater.highlighted_peer", highlighted),
	))
}

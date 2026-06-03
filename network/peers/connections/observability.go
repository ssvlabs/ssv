package connections

import (
	"context"

	"github.com/libp2p/go-libp2p/core/network"
	"go.opentelemetry.io/otel"
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

	// TODO(audit): peer-connection lifecycle events on a node with dozens of peers may
	// not be sparse in practice (peer churn, discovery surges). Re-evaluate classification
	// against production rates. Mis-classifying as sparse is harmless (Add(0) on a dense
	// counter is a no-op for PromQL), but the intent should be accurate.
	connectedCounter = metrics.RegisterSparseCounter(metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "connected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of connected peers"))))

	// TODO(audit): see note on connectedCounter — same caveat applies.
	disconnectedCounter = metrics.RegisterSparseCounter(metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "disconnected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of disconnected peers"))))

	// TODO(audit): see note on connectedCounter — same caveat applies.
	filteredCounter = metrics.RegisterSparseCounter(metrics.New(
		meter.Int64Counter(
			observability.InstrumentName(observabilityNamespace, "filtered"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of filtered connections"))))
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

package connections

import (
	"context"
	"fmt"

	"github.com/libp2p/go-libp2p/core/network"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"

	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName = "github.com/ssvlabs/ssv/network/peers/connections"
	observabilityNamespace     = "ssv.p2p.connections"
)

var (
	meter = otel.Meter(observabilityComponentName)

	connectedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("connected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of connected peers")))

	disconnectedCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("disconnected"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of disconnected peers")))

	filteredCounter = observability.NewMetric(
		meter.Int64Counter(
			metricName("filtered"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("total number of filtered connections")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

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

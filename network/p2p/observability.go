package p2pv1

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityName      = "github.com/ssvlabs/ssv/network/p2p"
	observabilityNamespace = "ssv.p2p"
)

var (
	meter = otel.Meter(observabilityName)

	peersConnectedGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("peers.connected"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("number of connected peers")))

	connectionsGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("connections.active"),
			metric.WithUnit("{connection}"),
			metric.WithDescription("number of active connections")))

	peersPerTopicGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("peers.per_topic"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("number of connected peers per topic")))

	peerIdentityGauge = observability.NewMetric(
		meter.Int64Gauge(
			metricName("peers.per_version"),
			metric.WithUnit("{peer}"),
			metric.WithDescription("number of connected peers per node version")))
)

func metricName(name string) string {
	return fmt.Sprintf("%s.%s", observabilityNamespace, name)
}

func recordPeerCount(ctx context.Context, host host.Host) func() {
	return func() {
		peers := host.Network().Peers()
		var (
			numOfOutbound,
			numOfInbound int64
		)
		for _, peer := range peers {
			conns := host.Network().ConnsToPeer(peer)
			for _, conn := range conns {
				direction := conn.Stat().Direction
				if direction == network.DirInbound {
					numOfInbound++
				} else if direction == network.DirOutbound {
					numOfOutbound++
				}
			}
		}
		connectionsGauge.Record(ctx, numOfInbound, metric.WithAttributes(
			observability.NetworkDirectionAttribute(network.DirInbound),
		))
		connectionsGauge.Record(ctx, numOfOutbound, metric.WithAttributes(
			observability.NetworkDirectionAttribute(network.DirOutbound),
		))
		peersConnectedGauge.Record(ctx, int64(len(peers)))
	}
}

func recordPeerCountPerTopic(ctx context.Context, ctrl topics.Controller) func() {
	return func() {
		for _, topicName := range ctrl.Topics() {
			peers, err := ctrl.Peers(topicName)
			if err != nil {
				return
			}
			peersPerTopicGauge.Record(ctx, int64(len(peers)), metric.WithAttributes(attribute.String("ssv.p2p.topic.name", topicName)))
		}
	}
}

func recordPeerIdentities(ctx context.Context, host host.Host, index peers.Index) func() {
	return func() {
		peersByVersion := make(map[string]int64)
		for _, pid := range host.Network().Peers() {
			nodeVersion := "unknown"
			ni := index.NodeInfo(pid)
			if ni != nil {
				if ni.Metadata != nil {
					nodeVersion = ni.Metadata.NodeVersion
				}
			}
			peersByVersion[nodeVersion]++
		}
		for version, peerCount := range peersByVersion {
			peerIdentityGauge.Record(ctx, peerCount, metric.WithAttributes(attribute.String("ssv.node.version", version)))
		}
	}
}

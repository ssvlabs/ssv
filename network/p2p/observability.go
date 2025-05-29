package p2pv1

import (
	"context"
	"fmt"
	"sort"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
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

func recordPeerCount(ctx context.Context, logger *zap.Logger, h host.Host) func() {
	return func() {
		numOfInbound, numOfOutbound := connectionStats(h)
		numTotal := numOfInbound + numOfOutbound

		logger.Debug(
			"connected peers status",
			zap.Int("peers_inbound", numOfInbound),
			zap.Int("peers_outbound", numOfOutbound),
			zap.Int("peers_total", numTotal),
		)

		connectionsGauge.Record(ctx, int64(numOfInbound), metric.WithAttributes(
			observability.NetworkDirectionAttribute(network.DirInbound),
		))
		connectionsGauge.Record(ctx, int64(numOfOutbound), metric.WithAttributes(
			observability.NetworkDirectionAttribute(network.DirOutbound),
		))
		peersConnectedGauge.Record(ctx, int64(numTotal))
	}
}

func recordPeerCountPerTopic(ctx context.Context, logger *zap.Logger, ctrl topics.Controller, loggingFrequency int) func() {
	var iterations int
	return func() {
		var (
			shouldLog        = iterations%loggingFrequency == 0
			subnetPeerCounts []int
			deadSubnets,
			unhealthySubnets int
		)
		for _, topicName := range ctrl.Topics() {
			peers, err := ctrl.Peers(topicName)
			if err != nil {
				return
			}

			peersPerTopicGauge.Record(ctx, int64(len(peers)), metric.WithAttributes(attribute.String("ssv.p2p.topic.name", topicName)))

			if shouldLog {
				subnetPeerCounts = append(subnetPeerCounts, len(peers))
				if len(peers) == 0 {
					deadSubnets++
				} else if len(peers) <= 2 {
					unhealthySubnets++
				}
				logger.Debug("topic peers status", fields.Topic(topicName), fields.Count(len(peers)), zap.Any("peers", peers))
			}
		}

		// Calculate min, median, max
		if shouldLog {
			sort.Ints(subnetPeerCounts)
			var min, median, max int
			if len(subnetPeerCounts) > 0 {
				min = subnetPeerCounts[0]
				median = subnetPeerCounts[len(subnetPeerCounts)/2]
				max = subnetPeerCounts[len(subnetPeerCounts)-1]
			}
			logger.Debug(
				"topic peers distribution",
				zap.Int("subnets_subscribed_total", len(ctrl.Topics())),
				zap.Int("min", min),
				zap.Int("median", median),
				zap.Int("max", max),
				zap.Int("dead_subnets", deadSubnets),
				zap.Int("unhealthy_subnets", unhealthySubnets),
			)
		}

		iterations++
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

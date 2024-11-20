package p2pv1

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/ssvlabs/ssv/network/peers"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/observability"
)

const (
	observabilityComponentName      = "github.com/ssvlabs/ssv/network/p2p"
	observabilityComponentNamespace = "ssv.p2p.peer"
)

var (
	meter = otel.Meter(observabilityComponentName)

	peersConnectedGauge = observability.GetMetric(
		fmt.Sprintf("%s.connected", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{peer}"),
				metric.WithDescription("number of connected peers"))
		},
	)

	peersByTopicCounter = observability.GetMetric(
		fmt.Sprintf("%s.by_topic", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{peer}"),
				metric.WithDescription("number of connected peers per topic"))
		},
	)

	peerIdentityGauge = observability.GetMetric(
		fmt.Sprintf("%s.identity", observabilityComponentNamespace),
		func(metricName string) (metric.Int64Gauge, error) {
			return meter.Int64Gauge(
				metricName,
				metric.WithUnit("{peer}"),
				metric.WithDescription("describes identities of connected peers"))
		},
	)
)

func recordPeerCount(ctx context.Context, peers []peer.ID) func() {
	return func() {
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
			peersByTopicCounter.Record(ctx, int64(len(peers)), metric.WithAttributes(attribute.String("ssv.p2p.topic.name", topicName)))
		}
	}
}

func recordPeerIdentities(ctx context.Context, peers []peer.ID, index peers.Index) func() {
	return func() {
		for _, pid := range peers {
			nodeVersion := "unknown"
			ni := index.NodeInfo(pid)
			if ni != nil {
				if ni.Metadata != nil {
					nodeVersion = ni.Metadata.NodeVersion
				}
			}
			peerIdentityGauge.Record(ctx, 1, metric.WithAttributes(attribute.String("ssv.node.version", nodeVersion)))
		}
	}
}

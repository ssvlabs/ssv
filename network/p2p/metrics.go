package p2pv1

import (
	"sort"
	"strconv"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network/peers/connections"
	"github.com/ssvlabs/ssv/network/topics"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/utils/format"
)

type Metrics interface {
	connections.Metrics
	topics.Metrics
}

var (
	// MetricsAllConnectedPeers counts all connected peers
	MetricsAllConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv_p2p_all_connected_peers",
		Help: "Count connected peers",
	})
	// MetricsConnectedPeers counts connected peers for a topic
	MetricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv_p2p_connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	// MetricsPeersIdentity tracks peers identity
	MetricsPeersIdentity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peers_identity",
		Help: "Peers identity",
	}, []string{"pubKey", "operatorID", "v", "pid", "type"})
	metricsRouterIncoming = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:router:in",
		Help: "Counts incoming messages",
	}, []string{"mt"})
)

func init() {
	logger := zap.L()
	if err := prometheus.Register(MetricsAllConnectedPeers); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(MetricsPeersIdentity); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(MetricsConnectedPeers); err != nil {
		logger.Debug("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsRouterIncoming); err != nil {
		logger.Debug("could not register prometheus collector")
	}
}

var unknown = "unknown"

func (n *p2pNetwork) reportAllPeers(logger *zap.Logger) func() {
	return func() {
		pids := n.host.Network().Peers()
		logger.Debug("connected peers status", fields.Count(len(pids)))
		MetricsAllConnectedPeers.Set(float64(len(pids)))
	}
}

func (n *p2pNetwork) reportPeerIdentities(logger *zap.Logger) func() {
	return func() {
		pids := n.host.Network().Peers()
		for _, pid := range pids {
			n.reportPeerIdentity(logger, pid)
		}
	}
}

func (n *p2pNetwork) reportTopics(logger *zap.Logger) func() {
	return func() {
		topics := n.topicsCtrl.Topics()
		logger.Debug("connected topics", fields.Count(len(topics)))

		subnetPeerCounts := []int{}
		deadSubnets := 0
		unhealthySubnets := 0
		for _, name := range topics {
			count := n.reportTopicPeers(logger, name)
			subnetPeerCounts = append(subnetPeerCounts, count)

			if count == 0 {
				deadSubnets++
			} else if count <= 2 {
				unhealthySubnets++
			}
		}

		// Calculate min, median, max
		sort.Ints(subnetPeerCounts)
		min := subnetPeerCounts[0]
		median := subnetPeerCounts[len(subnetPeerCounts)/2]
		max := subnetPeerCounts[len(subnetPeerCounts)-1]

		logger.Debug("topic peers distribution",
			zap.Int("min", min),
			zap.Int("median", median),
			zap.Int("max", max),
			zap.Int("dead_subnets", deadSubnets),
			zap.Int("unhealthy_subnets", unhealthySubnets),
		)
	}
}

func (n *p2pNetwork) reportTopicPeers(logger *zap.Logger, name string) (peerCount int) {
	peers, err := n.topicsCtrl.Peers(name)
	if err != nil {
		logger.Warn("could not get topic peers", fields.Topic(name), zap.Error(err))
		return 0
	}
	logger.Debug("topic peers status", fields.Topic(name), fields.Count(len(peers)), zap.Any("peers", peers))
	MetricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
	return len(peers)
}

func (n *p2pNetwork) reportPeerIdentity(logger *zap.Logger, pid peer.ID) {
	opPKHash, opID, nodeVersion, nodeType := unknown, unknown, unknown, unknown
	ni := n.idx.NodeInfo(pid)
	if ni != nil {
		if ni.Metadata != nil {
			nodeVersion = ni.Metadata.NodeVersion
		}
		nodeType = "operator"
		if len(opPKHash) == 0 && nodeVersion != unknown {
			nodeType = "exporter"
		}
	}

	if pubKey, ok := n.operatorPKHashToPKCache.Get(opPKHash); ok {
		operatorData, found, opDataErr := n.nodeStorage.GetOperatorDataByPubKey(nil, pubKey)
		if opDataErr == nil && found {
			opID = strconv.FormatUint(operatorData.ID, 10)
		}
	} else {
		operators, err := n.nodeStorage.ListOperators(nil, 0, 0)
		if err != nil {
			logger.Warn("failed to get all operators for reporting", zap.Error(err))
		}

		for _, operator := range operators {
			pubKeyHash := format.OperatorID(operator.PublicKey)
			n.operatorPKHashToPKCache.Set(pubKeyHash, operator.PublicKey)
			if pubKeyHash == opPKHash {
				opID = strconv.FormatUint(operator.ID, 10)
			}
		}
	}

	state := n.idx.State(pid)
	logger.Debug("peer identity",
		fields.PeerID(pid),
		zap.String("node_version", nodeVersion),
		zap.String("operator_id", opID),
		zap.String("state", state.String()),
	)
	MetricsPeersIdentity.WithLabelValues(opPKHash, opID, nodeVersion, pid.String(), nodeType).Set(1)
}

//
// func reportLastMsg(pid string) {
//	MetricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
//}
//
// func timestamp() int64 {
//	return time.Now().UnixNano() / int64(time.Millisecond)
//}

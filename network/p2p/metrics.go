package p2pv1

import (
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)

var (
	// MetricsAllConnectedPeers counts all connected peers
	MetricsAllConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:all_connected_peers",
		Help: "Count connected peers",
	})
	// MetricsConnectedPeers counts connected peers for a topic
	MetricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	// MetricsPeersIdentity tracks peers identity
	MetricsPeersIdentity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peers_identity",
		Help: "Peers identity",
	}, []string{"pubKey", "v", "pid", "type"})
	metricsRouterIncoming = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:router:in",
		Help: "Counts incoming messages",
	}, []string{"identifier", "mt"})
)

func init() {
	if err := prometheus.Register(MetricsAllConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(MetricsPeersIdentity); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(MetricsConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsRouterIncoming); err != nil {
		log.Println("could not register prometheus collector")
	}
}

var unknown = "unknown"

func (n *p2pNetwork) reportAllPeers() {
	pids := n.host.Network().Peers()
	var ids []string
	for _, pid := range pids {
		ids = append(ids, pid.String())
		n.reportPeerIdentity(pid)
	}
	n.logger.Debug("connected peers status",
		zap.Int("count", len(ids)))
	MetricsAllConnectedPeers.Set(float64(len(ids)))
}

func (n *p2pNetwork) reportTopics() {
	topics := n.topicsCtrl.Topics()
	nTopics := len(topics)
	n.logger.Debug("connected topics",
		zap.Int("count", nTopics))
	for _, name := range topics {
		n.reportTopicPeers(name)
	}
}

func (n *p2pNetwork) reportTopicPeers(name string) {
	peers, err := n.topicsCtrl.Peers(name)
	if err != nil {
		n.logger.Warn("could not get topic peers", zap.String("topic", name), zap.Error(err))
		return
	}
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Int("count", len(peers)),
		zap.Any("peers", peers))
	MetricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

func (n *p2pNetwork) reportPeerIdentity(pid peer.ID) {
	oid, forkv, nodeVersion, nodeType := unknown, unknown, unknown, unknown
	ni, err := n.idx.GetNodeInfo(pid)
	if err == nil && ni != nil {
		oid = unknown
		nodeVersion = unknown
		forkv = ni.ForkVersion.String()
		if ni.Metadata != nil {
			oid = ni.Metadata.OperatorID
			nodeVersion = ni.Metadata.NodeVersion
		}
		nodeType = "operator"
		if len(oid) == 0 && nodeVersion != unknown {
			nodeType = "exporter"
		}
	}
	nodeState := n.idx.State(pid)
	n.logger.Debug("peer identity",
		zap.String("peer", pid.String()),
		zap.String("forkv", forkv),
		zap.String("nodeVersion", nodeVersion),
		zap.String("oid", oid),
		zap.String("nodeType", nodeType),
		zap.String("nodeState", nodeState.String()))
	MetricsPeersIdentity.WithLabelValues(oid, nodeVersion,
		pid.String(), nodeType).Set(1)
}

//
//func reportLastMsg(pid string) {
//	MetricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
//}
//
//func timestamp() int64 {
//	return time.Now().UnixNano() / int64(time.Millisecond)
//}

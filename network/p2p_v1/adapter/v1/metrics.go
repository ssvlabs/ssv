package v1

import (
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)

var (
	metricsAllConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:all_connected_peers:v1:adapter",
		Help: "Count connected peers",
	})
	metricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:connected_peers:v1:adapter",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	metricsTopicsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:topics:count:v1:adapter",
		Help: "Count connected peers for a validator",
	})
	metricsPeersIdentity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peers_identity:v1:adapter",
		Help: "Peers identity",
	}, []string{"pubKey", "v", "pid", "type"})
	metricsPeerLastMsg = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peer_last_msg:v1:adapter",
		Help: "Timestamps of last messages",
	}, []string{"pid"})
)

func init() {
	if err := prometheus.Register(metricsAllConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsTopicsCount); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPeersIdentity); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsPeerLastMsg); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func (n *netV0Adapter) reportAllPeers() {
	pids := n.host.Network().Peers()
	var ids []string
	for _, pid := range pids {
		ids = append(ids, pid.String())
		n.reportPeerIdentity(pid)
	}
	n.logger.Debug("connected peers status",
		zap.Int("count", len(ids)))
	metricsAllConnectedPeers.Set(float64(len(ids)))
}

func (n *netV0Adapter) reportTopics() {
	topics := n.topicsCtrl.Topics()
	nTopics := len(topics)
	n.logger.Debug("connected topics",
		zap.Int("count", nTopics))
	metricsTopicsCount.Set(float64(nTopics))
	for _, name := range topics {
		n.reportTopicPeers(name)
	}
}

func (n *netV0Adapter) reportTopicPeers(name string) {
	peers, err := n.topicsCtrl.Peers(name)
	if err != nil {
		n.logger.Warn("could not get topic peers", zap.String("topic", name), zap.Error(err))
		return
	}
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Int("count", len(peers)),
		zap.Any("peers", peers))
	metricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

func (n *netV0Adapter) reportPeerIdentity(pid peer.ID) {
	identity, err := n.idx.Identity(pid)
	if err != nil {
		//n.trace("WARNING: could not report peer", zap.String("peer", pid.String()), zap.Error(err))
		return
	}
	n.logger.Debug("peer identity", zap.String("peer", pid.String()),
		zap.String("oid", identity.OperatorID))

	metricsPeersIdentity.WithLabelValues(identity.OperatorID, identity.NodeVersion(),
		pid.String(), identity.NodeType()).Set(1)
}

//func reportLastMsg(pid string) {
//	metricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
//}
//
//func timestamp() int64 {
//	return time.Now().UnixNano() / int64(time.Millisecond)
//}

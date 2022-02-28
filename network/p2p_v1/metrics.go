package p2pv1

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)

var (
	metricsAllConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:all_connected_peers",
		Help: "Count connected peers",
	})
	metricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	metricsTopicsCount = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:topics:count",
		Help: "Count connected peers for a validator",
	})
	metricsPeersIdentity = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peers_identity",
		Help: "Peers identity",
	}, []string{"pubKey", "v", "pid", "type"})
	metricsPeerLastMsg = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:peer_last_msg",
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

func reportAllPeers(n *p2pNetwork) {
	pids := n.host.Network().Peers()
	var ids []string
	for _, pid := range pids {
		ids = append(ids, pid.String())
		//reportPeerIdentity(n, pid)
	}
	n.logger.Debug("connected peers status",
		zap.Int("count", len(ids)))
	metricsAllConnectedPeers.Set(float64(len(ids)))
}

func reportTopics(n *p2pNetwork) {
	topics := n.topicsCtrl.Topics()
	nTopics := len(topics)
	n.logger.Debug("connected topics",
		zap.Int("count", nTopics))
	metricsTopicsCount.Set(float64(nTopics))
	for _, name := range topics {
		reportTopicPeers(n, name)
	}
}

func reportTopicPeers(n *p2pNetwork, name string) {
	peers, err := n.topicsCtrl.Peers(name)
	if err != nil {
		n.logger.Warn("could not get topic peers", zap.String("topic", name), zap.Error(err))
		return
	}
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Int("count", len(peers)),
		zap.Any("peers", peers))
	metricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

//func reportPeerIdentity(n *p2pNetwork, pid peer.ID) {
//	if ua, err := n.peersIndex.getUserAgent(pid); err != nil {
//		n.trace("WARNING: could not report peer", zap.String("peer", pid.String()), zap.Error(err))
//	} else {
//		n.trace("peer identity", zap.String("peer", pid.String()), zap.String("ua", string(ua)))
//		metricsPeersIdentity.WithLabelValues(ua.OperatorID(), ua.NodeVersion(), pid.String(), ua.NodeType()).Set(1)
//	}
//}
//
//func reportLastMsg(pid string) {
//	metricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
//}
//
//func timestamp() int64 {
//	return time.Now().UnixNano() / int64(time.Millisecond)
//}

package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
	"strings"
	"time"
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

func reportAllConnections(n *p2pNetwork) {
	conns := n.host.Network().Conns()
	var ids []string
	for _, conn := range conns {
		pid := conn.RemotePeer().String()
		ids = append(ids, pid)
		reportPeerIdentity(n, pid)
	}
	var peersActiveDisv5 []peer.ID
	if n.peers != nil {
		peersActiveDisv5 = n.peers.Active()
	}
	n.logger.Debug("connected peers status",
		zap.Int("count", len(ids)),
		zap.Any("ids", ids),
		zap.Any("peersActiveDisv5", peersActiveDisv5))
	metricsAllConnectedPeers.Set(float64(len(ids)))
}

func reportTopicPeers(n *p2pNetwork, name string, topic *pubsub.Topic) {
	peers := n.allPeersOfTopic(topic)
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Int("count", len(peers)),
		zap.Any("peers", peers))
	metricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

func reportPeerIdentity(n *p2pNetwork, pid string) {
	ua := n.peersIndex.GetPeerData(pid, UserAgentKey)
	n.logger.Debug("peer identity", zap.String("peer", pid), zap.String("ua", ua))
	uaParts := strings.Split(ua, ":")
	if len(uaParts) > 2 {
		if len(uaParts) > 3 { // In order to support backwards compatibility. where older version have only 2 fields
			metricsPeersIdentity.WithLabelValues(uaParts[3], uaParts[1], pid, uaParts[2]).Set(1)
			return
		}
		metricsPeersIdentity.WithLabelValues(uaParts[2], uaParts[1], pid).Set(1)
	}
}

func reportLastMsg(pid string) {
	metricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
}

func timestamp() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

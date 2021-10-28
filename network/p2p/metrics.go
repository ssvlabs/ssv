package p2p

import (
	libp2pnetwork "github.com/libp2p/go-libp2p-core/network"
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
	}, []string{"pubKey", "v", "pid"})
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
		var addr string
		ma := conn.RemoteMultiaddr()
		if ma != nil {
			addr = ma.String()
		}
		reportPeerIdentity(n, pid, addr, n.host.Network().Connectedness(conn.RemotePeer()))
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

func reportPeerIdentity(n *p2pNetwork, pid, addr string, connectedness libp2pnetwork.Connectedness) {
	ua := n.peersIndex.GetPeerData(pid, UserAgentKey)
	n.logger.Debug("peer identity", zap.String("peer", pid), zap.String("ua", ua),
		zap.String("addr", addr), zap.String("connectedness", connectedness.String()))
	uaParts := strings.Split(ua, ":")
	if len(uaParts) > 2 {
		metricsPeersIdentity.WithLabelValues(uaParts[2], uaParts[1], pid).Set(float64(connectedness))
	}
}

func reportLastMsg(pid string) {
	metricsPeerLastMsg.WithLabelValues(pid).Set(float64(timestamp()))
}

func timestamp() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}

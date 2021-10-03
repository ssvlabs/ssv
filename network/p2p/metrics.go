package p2p

import (
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)

var (
	metricsAllConnectedPeers = promauto.NewGauge(prometheus.GaugeOpts{
		Name: "ssv:network:all_connected_peers",
		Help: "Count connected peers for a validator",
	})
	metricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	metricsNetMsgsInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:net_messages_inbound",
		Help: "Count incoming network messages",
	}, []string{"pubKey", "type", "signer"})
	metricsIBFTDecidedMsgsOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:ibft_decided_messages_outbound",
		Help: "Count IBFT decided messages outbound",
	}, []string{"pubKey", "seq"})
)

func init() {
	if err := prometheus.Register(metricsAllConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsNetMsgsInbound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsIBFTDecidedMsgsOutbound); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportConnectionsCount(n *p2pNetwork) {
	conns := n.host.Network().Conns()
	var connsIDs []string
	for _, conn := range conns {
		connsIDs = append(connsIDs, conn.RemotePeer().String())
	}
	peersActiveDisv5 := n.peers.Active()
	n.logger.Debug("connected peers status",
		zap.Int("count", len(conns)),
		zap.Any("connsIDs", connsIDs),
		zap.Any("peersActiveDisv5", peersActiveDisv5))
	metricsAllConnectedPeers.Set(float64(len(conns)))
}

func reportTopicPeers(n *p2pNetwork, name string, topic *pubsub.Topic) {
	peers := n.allPeersOfTopic(topic)
	n.logger.Debug("topic peers status", zap.String("topic", name), zap.Any("peers", peers))
	metricsConnectedPeers.WithLabelValues(name).Set(float64(len(peers)))
}

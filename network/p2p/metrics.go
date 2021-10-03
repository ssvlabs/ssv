package p2p

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
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

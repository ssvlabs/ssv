package p2p

import (
	"github.com/bloxapp/ssv/network"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"log"
	"strconv"
)

var (
	metricsConnectedPeers = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Name: "ssv:network:connected_peers",
		Help: "Count connected peers for a validator",
	}, []string{"pubKey"})
	metricsNetMsgsInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:net_messages_inbound",
		Help: "Count incoming network messages",
	}, []string{"pubKey", "type", "signer"})
	metricsIBFTMsgsInbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:ibft_messages_inbound",
		Help: "Count incoming network messages",
	}, []string{"pubKey", "signer", "seq", "round"})
	metricsIBFTMsgsOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:ibft_messages_outbound",
		Help: "Count IBFT messages outbound",
	}, []string{"pubKey", "type", "seq", "round"})
	metricsIBFTDecidedMsgsOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:ibft_decided_messages_outbound",
		Help: "Count IBFT decided messages outbound",
	}, []string{"pubKey", "seq"})
)

func init() {
	if err := prometheus.Register(metricsConnectedPeers); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsNetMsgsInbound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsIBFTMsgsOutbound); err != nil {
		log.Println("could not register prometheus collector")
	}
	if err := prometheus.Register(metricsIBFTDecidedMsgsOutbound); err != nil {
		log.Println("could not register prometheus collector")
	}
}

func reportIncomingSignedMessage(cm *network.Message, topic string) {
	if cm.SignedMessage != nil && len(cm.SignedMessage.SignerIds) > 0 {
		for _, nodeID := range cm.SignedMessage.SignerIds {
			metricsNetMsgsInbound.WithLabelValues(unwrapTopicName(topic), cm.Type.String(),
				strconv.FormatUint(nodeID, 10)).Inc()
			if cm.Type == network.NetworkMsg_IBFTType && cm.SignedMessage.Message != nil {
				seq := strconv.FormatUint(cm.SignedMessage.Message.SeqNumber, 10)
				round := strconv.FormatUint(cm.SignedMessage.Message.Round, 10)
				metricsIBFTMsgsInbound.WithLabelValues(unwrapTopicName(topic),
					strconv.FormatUint(nodeID, 10), seq, round).Inc()
			}
		}
	}
}

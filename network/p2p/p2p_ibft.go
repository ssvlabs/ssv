package p2p

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"go.uber.org/zap"
	"log"
)


var (
	metricsIBFTMsgsOutbound = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "ssv:network:ibft_messages_outbound",
		Help: "Count IBFT messages outbound",
	}, []string{"topic"})
)

func init() {
	if err := prometheus.Register(metricsIBFTMsgsOutbound); err != nil {
		log.Println("could not register prometheus collector")
	}
}

// Broadcast propagates a signed message to all peers
func (n *p2pNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	msgBytes, err := json.Marshal(network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	topic, err := n.getTopic(topicName)
	if err != nil {
		return errors.Wrap(err, "failed to get topic")
	}

	n.logger.Debug("broadcasting ibft msg", zap.String("lambda", string(msg.Message.Lambda)), zap.Any("topic", topic), zap.Any("peers", topic.ListPeers()))
	metricsIBFTMsgsOutbound.WithLabelValues(topic.String()).Inc()

	return topic.Publish(n.ctx, msgBytes)
}

// ReceivedMsgChan return a channel with messages
func (n *p2pNetwork) ReceivedMsgChan() <-chan *proto.SignedMessage {
	ls := listener{
		msgCh: make(chan *proto.SignedMessage, MsgChanSize),
	}

	n.listenersLock.Lock()
	n.listeners = append(n.listeners, ls)
	n.listenersLock.Unlock()

	return ls.msgCh
}

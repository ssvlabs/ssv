package p2p

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Broadcast propagates a signed message to all peers
func (n *p2pNetwork) Broadcast(topicName []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
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

	n.logger.Debug("broadcasting ibft msg", zap.String("lambda", string(msg.Message.Lambda)),
		zap.Any("topic", topic), zap.Any("peers", topic.ListPeers()))

	return topic.Publish(n.ctx, msgBytes)
}

// ReceivedMsgChan return a channel with messages
func (n *p2pNetwork) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	ls := &listener{
		msgCh:   make(chan *proto.SignedMessage, MsgChanSize),
		msgType: network.NetworkMsg_IBFTType,
	}

	return ls.msgCh, n.listenersContainer.register(ls)
}

package p2p

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Broadcast propagates a signed message to all peers
func (n *p2pNetwork) Broadcast(msg *proto.SignedMessage) error {
	msgBytes, err := json.Marshal(network.Message{
		Lambda:        msg.Message.Lambda,
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	topic, err := n.getTopic(msg.Message.GetValidatorPk())
	if err != nil {
		return errors.Wrap(err, "failed to get topic")
	}

	n.logger.Debug("Broadcasting to topic", zap.Any("topic", topic), zap.Any("peers", topic.ListPeers()))
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

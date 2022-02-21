package p2p

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// Broadcast propagates a signed message to all peers
func (n *p2pNetwork) Broadcast(pk []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_IBFTType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	topicID := n.fork.ValidatorTopicID(pk)
	name := getTopicName(topicID)

	peers, err := n.topicManager.Peers(name)
	if err != nil {
		return errors.Wrap(err, "could not check peers")
	}
	n.logger.Debug("Broadcasting ibft message", zap.Uint64("seqNum", msg.Message.SeqNumber),
		zap.String("lambda", string(msg.Message.Lambda)), zap.String("topic", topicID),
		zap.Any("peers", peers))

	return n.topicManager.Broadcast(name, msgBytes, time.Second*5)
}

// ReceivedMsgChan return a channel with messages
func (n *p2pNetwork) ReceivedMsgChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_IBFTType)

	return ls.MsgChan(), n.listeners.Register(ls)
}

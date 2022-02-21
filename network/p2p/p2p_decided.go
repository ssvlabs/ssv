package p2p

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/network/commons/listeners"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// BroadcastDecided broadcasts a decided instance with collected signatures
func (n *p2pNetwork) BroadcastDecided(pk []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_DecidedType,
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
	n.logger.Debug("Broadcasting decided message", zap.Uint64("seqNum", msg.Message.SeqNumber),
		zap.String("lambda", string(msg.Message.Lambda)), zap.String("topic", topicID),
		zap.Any("peers", peers))

	if n.useMainTopic {
		go func() {
			err := n.topicManager.Broadcast(mainTopicName, msgBytes[:], time.Second*3)
			if err != nil {
				n.logger.Error("failed to publish on main topic")
			}
		}()
	}

	return n.topicManager.Broadcast(name, msgBytes, time.Second*5)
}

// ReceivedDecidedChan returns the channel for decided messages
func (n *p2pNetwork) ReceivedDecidedChan() (<-chan *proto.SignedMessage, func()) {
	ls := listeners.NewListener(network.NetworkMsg_DecidedType)

	return ls.DecidedChan(), n.listeners.Register(ls)
}

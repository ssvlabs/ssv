package p2p

import (
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// BroadcastSignature broadcasts the given signature for the given lambda
func (n *p2pNetwork) BroadcastSignature(topicName []byte, msg *proto.SignedMessage) error {
	msgBytes, err := n.fork.EncodeNetworkMsg(&network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_SignatureType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	topic, err := n.getTopic(topicName)
	if err != nil {
		return errors.Wrap(err, "failed to get topic")
	}

	n.logger.Debug("Broadcasting signature message", zap.String("lambda", string(msg.Message.Lambda)), zap.Any("topic", topic), zap.Any("peers", topic.ListPeers()))
	return topic.Publish(n.ctx, msgBytes)
}

// ReceivedSignatureChan returns the channel with signatures
func (n *p2pNetwork) ReceivedSignatureChan() (<-chan *proto.SignedMessage, func()) {
	ls := listener{
		sigCh: make(chan *proto.SignedMessage, MsgChanSize),
	}

	//n.listenersLock.Lock()
	//n.listeners = append(n.listeners, ls)
	//n.listenersLock.Unlock()

	return ls.sigCh, n.registerListener(ls)
}

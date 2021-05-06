package p2p

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
)

// BroadcastDecided broadcasts a decided instance with collected signatures
func (n *p2pNetwork) BroadcastDecided(msg *proto.SignedMessage) error {
	msgBytes, err := json.Marshal(network.Message{
		Lambda:        msg.Message.Lambda,
		SignedMessage: msg,
		Type:          network.NetworkMsg_DecidedType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}

	return n.cfg.Topic.Publish(n.ctx, msgBytes)
}

// ReceivedDecidedChan returns the channel for decided messages
func (n *p2pNetwork) ReceivedDecidedChan() <-chan *proto.SignedMessage {
	ls := listener{
		decidedCh: make(chan *proto.SignedMessage, MsgChanSize),
	}

	n.listenersLock.Lock()
	n.listeners = append(n.listeners, ls)
	n.listenersLock.Unlock()

	return ls.sigCh
}

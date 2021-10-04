package p2p

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	"github.com/pkg/errors"
)

// BroadcastMainChannel broadcasts the given msg on main channel
func (n *p2pNetwork) BroadcastMainChannel(msg *proto.SignedMessage) error {
	msgBytes, err := json.Marshal(network.Message{
		SignedMessage: msg,
		Type:          network.NetworkMsg_DecidedType,
	})
	if err != nil {
		return errors.Wrap(err, "failed to marshal message")
	}
	topic, err := n.getMainTopic()
	if err != nil {
		return errors.Wrap(err, "failed to get main topic")
	}
	if err := topic.Publish(n.ctx, msgBytes); err != nil {
		return errors.Wrap(err, "failed to publish on main topic")
	}
	return nil
}

// SubscribeToMainTopic subscribes to main topic
func (n *p2pNetwork) SubscribeToMainTopic() error {
	topic, err := n.getMainTopic()
	if err != nil {
		return err
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return errors.Wrap(err, "failed to subscribe on Topic")
	}
	go n.listen(sub)

	return nil
}

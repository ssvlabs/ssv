package p2p

import (
	"encoding/json"
	"github.com/bloxapp/ssv/ibft/proto"
	"github.com/bloxapp/ssv/network"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
)

// BroadcastMainTopic broadcasts the given msg on main channel
func (n *p2pNetwork) BroadcastMainTopic(msg *proto.SignedMessage) error {
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
	go n.listen(n.ctx, sub)

	return nil
}

// getTopic return topic by validator public key
func (n *p2pNetwork) getMainTopic() (*pubsub.Topic, error) {
	n.psTopicsLock.RLock()
	defer n.psTopicsLock.RUnlock()

	name := "main"
	if _, ok := n.cfg.Topics[name]; !ok {
		topic, err := n.pubsub.Join(getTopicName(name))
		if err != nil {
			return nil, errors.Wrap(err, "failed to join main topic")
		}
		n.cfg.Topics[name] = topic
	}
	return n.cfg.Topics[name], nil
}

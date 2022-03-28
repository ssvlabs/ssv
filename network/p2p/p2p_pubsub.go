package p2pv1

import (
	"github.com/bloxapp/ssv/network"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/protocol/v1/core"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// UseMessageRouter registers a message router to handle incoming messages
func (n *p2pNetwork) UseMessageRouter(router network.MessageRouter) {
	n.msgRouter = router
}

// Broadcast publishes the message to all peers in subnet
func (n *p2pNetwork) Broadcast(message core.SSVMessage) error {
	raw, err := n.fork.EncodeNetworkMsg(&message)
	if err != nil {
		return errors.Wrap(err, "could not decode message")
	}
	vpk := message.GetID().GetValidatorPK()
	topic := n.fork.ValidatorTopicID(vpk)
	if topic == forksv1.UnknownSubnet {
		return errors.New("unknown topic")
	}
	if err := n.topicsCtrl.Broadcast(topic, raw, n.cfg.RequestTimeout); err != nil {
		//return errors.Wrap(err, "could not broadcast message")
		return err
	}
	return nil
}

// Subscribe subscribes to validator subnet
func (n *p2pNetwork) Subscribe(pk core.ValidatorPK) error {
	topic := n.fork.ValidatorTopicID(pk)
	if topic == forksv1.UnknownSubnet {
		return errors.New("unknown topic")
	}
	return n.topicsCtrl.Subscribe(topic)
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(pk core.ValidatorPK) error {
	topic := n.fork.ValidatorTopicID(pk)
	if topic == forksv1.UnknownSubnet {
		return errors.New("unknown topic")
	}
	return n.topicsCtrl.Unsubscribe(topic)
}

// handleIncomingMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handlePubsubMessages(topic string, msg *pubsub.Message) error {
	if n.msgRouter == nil {
		n.logger.Warn("msg router is not configured")
		return nil
	}
	if msg == nil {
		n.logger.Warn("got nil message", zap.String("topic", topic))
		return nil
	}
	parsed, err := n.fork.(*forksv1.ForkV1).DecodeNetworkMsgV1(msg.GetData())
	if err != nil {
		n.logger.Warn("could not decode message", zap.String("topic", topic), zap.Error(err))
		// TODO: handle..
		return nil
	}
	if parsed == nil {
		// TODO: handle..
		return nil
	}
	n.msgRouter.Route(*parsed)
	return nil
}

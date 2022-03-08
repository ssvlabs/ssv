package p2pv1

import (
	"context"
	"github.com/bloxapp/ssv/network"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/network/p2p_v1/topics"
	"github.com/bloxapp/ssv/protocol"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"time"
)

// UseMessageRouter registers a message router to handle incoming messages
func (n *p2pNetwork) UseMessageRouter(router network.MessageRouter) {
	n.msgRouter = router
}

// Broadcast publishes the message to all peers in subnet
func (n *p2pNetwork) Broadcast(message protocol.SSVMessage) error {
	raw, err := message.Encode()
	if err != nil {
		return errors.Wrap(err, "could not decode message")
	}
	vpk := message.GetID().GetValidatorPK()
	topic := n.cfg.Fork.ValidatorTopicID(vpk)
	if topic == forksv1.UnknownSubnet {
		return errors.New("unknown topic")
	}
	if err := n.topicsCtrl.Broadcast(topic, raw, time.Second*5); err != nil { // TODO: extract interval to variable
		//return errors.Wrap(err, "could not broadcast message")
		return err
	}
	return nil
}

// Subscribe subscribes to validator subnet
func (n *p2pNetwork) Subscribe(pk protocol.ValidatorPK) error {
	topic := n.cfg.Fork.ValidatorTopicID(pk)
	if topic == forksv1.UnknownSubnet {
		return errors.New("unknown topic")
	}
	ctx, cancel := context.WithTimeout(n.ctx, time.Second*10)
	defer cancel()
	logger := n.logger.With(zap.String("topic", topic))
	for ctx.Err() == nil {
		cn, err := n.topicsCtrl.Subscribe(topic)
		if err != nil {
			if err == topics.ErrInProcess {
				logger.Debug("topic in process")
				time.Sleep(time.Second)
				continue
			}
			return err
		}
		if cn == nil { // already registered
			logger.Debug("already registered on topic")
			return nil
		}
		go func(cn <-chan *pubsub.Message) {
			ctx, cancel := context.WithCancel(n.ctx)
			defer cancel()
			logger.Debug("start listening to topic")
			n.handleIncomingMessages(ctx, cn)
			logger.Debug("finished listening to topic")
			if err := n.Unsubscribe(pk); err != nil {
				logger.Warn("could not unsubscribe from topic")
				return
			}
			logger.Debug("unsubscribed from topic")
		}(cn)
		break
	}
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(pk protocol.ValidatorPK) error {
	topic := n.cfg.Fork.ValidatorTopicID(pk)
	if topic == forksv1.UnknownSubnet {
		return errors.New("unknown topic")
	}
	return n.topicsCtrl.Unsubscribe(topic)
}

// handleIncomingMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handleIncomingMessages(ctx context.Context, in <-chan *pubsub.Message) {
listenLoop:
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-in:
			parsed := protocol.SSVMessage{}
			if err := parsed.Decode(msg.Data); err != nil {
				n.logger.Warn("could not decode message", zap.Error(err))
				// TODO: handle..
				continue listenLoop
			}
			n.msgRouter.Route(parsed)
		}
	}
}

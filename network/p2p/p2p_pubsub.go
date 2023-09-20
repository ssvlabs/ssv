package p2pv1

import (
	"encoding/hex"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/commons"
	"github.com/bloxapp/ssv/network/records"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
)

type validatorStatus int

const (
	validatorStatusInactive    validatorStatus = 0
	validatorStatusSubscribing validatorStatus = 1
	validatorStatusSubscribed  validatorStatus = 2
)

// UseMessageRouter registers a message router to handle incoming messages
func (n *p2pNetwork) UseMessageRouter(router network.MessageRouter) {
	n.msgRouter = router
}

// Peers registers a message router to handle incoming messages
func (n *p2pNetwork) Peers(pk spectypes.ValidatorPK) ([]peer.ID, error) {
	all := make([]peer.ID, 0)
	topics := commons.ValidatorTopicID(pk)
	for _, topic := range topics {
		peers, err := n.topicsCtrl.Peers(topic)
		if err != nil {
			return nil, err
		}
		all = append(all, peers...)
	}
	return all, nil
}

// Broadcast publishes the message to all peers in subnet
func (n *p2pNetwork) Broadcast(msg *spectypes.SSVMessage) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	raw, err := commons.EncodeNetworkMsg(msg)
	if err != nil {
		return errors.Wrap(err, "could not decode msg")
	}

	vpk := msg.GetID().GetPubKey()
	topics := commons.ValidatorTopicID(vpk)

	for _, topic := range topics {
		if err := n.topicsCtrl.Broadcast(topic, raw, n.cfg.RequestTimeout); err != nil {
			n.interfaceLogger.Debug("could not broadcast msg", fields.PubKey(vpk), zap.Error(err))
			return errors.Wrap(err, "could not broadcast msg")
		}
	}
	return nil
}

func (n *p2pNetwork) SubscribeAll(logger *zap.Logger) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	n.subnets, _ = records.Subnets{}.FromString(records.AllSubnets)
	for subnet := 0; subnet < commons.Subnets(); subnet++ {
		err := n.topicsCtrl.Subscribe(logger, commons.SubnetTopicID(subnet))
		if err != nil {
			return err
		}
	}
	return nil
}

// Subscribe subscribes to validator subnet
func (n *p2pNetwork) Subscribe(pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	pkHex := hex.EncodeToString(pk)
	status, found := n.activeValidators.GetOrInsert(pkHex, validatorStatusSubscribing)
	if found && status != validatorStatusInactive {
		return nil
	}
	err := n.subscribe(n.interfaceLogger, pk)
	if err != nil {
		return err
	}
	n.activeValidators.Set(pkHex, validatorStatusSubscribed)
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(logger *zap.Logger, pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	pkHex := hex.EncodeToString(pk)
	if status, _ := n.activeValidators.Get(pkHex); status != validatorStatusSubscribed {
		return nil
	}
	topics := commons.ValidatorTopicID(pk)
	for _, topic := range topics {
		if err := n.topicsCtrl.Unsubscribe(logger, topic, false); err != nil {
			return err
		}
	}
	n.activeValidators.Del(pkHex)
	return nil
}

// subscribe to validator topics, as defined in the fork
func (n *p2pNetwork) subscribe(logger *zap.Logger, pk spectypes.ValidatorPK) error {
	topics := commons.ValidatorTopicID(pk)
	for _, topic := range topics {
		if err := n.topicsCtrl.Subscribe(logger, topic); err != nil {
			// return errors.Wrap(err, "could not broadcast message")
			return err
		}
	}
	return nil
}

// handleIncomingMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handlePubsubMessages(logger *zap.Logger) func(topic string, msg *pubsub.Message) error {
	return func(topic string, msg *pubsub.Message) error {
		if n.msgRouter == nil {
			logger.Debug("msg router is not configured")
			return nil
		}
		if msg == nil {
			return nil
		}

		var ssvMsg *spectypes.SSVMessage
		if msg.ValidatorData != nil {
			m, ok := msg.ValidatorData.(spectypes.SSVMessage)
			if ok {
				ssvMsg = &m
			}
		}
		if ssvMsg == nil {
			return errors.New("message was not decoded")
		}

		p2pID := ssvMsg.GetID().String()

		//	logger.With(
		// 		zap.String("pubKey", hex.EncodeToString(ssvMsg.MsgID.GetPubKey())),
		// 		zap.String("role", ssvMsg.MsgID.GetRoleType().String()),
		// 	).Debug("handlePubsubMessages")

		metricsRouterIncoming.WithLabelValues(p2pID, message.MsgTypeToString(ssvMsg.MsgType)).Inc()
		n.msgRouter.Route(logger, *ssvMsg)
		return nil
	}
}

// subscribeToSubnets subscribes to all the node's subnets
func (n *p2pNetwork) subscribeToSubnets(logger *zap.Logger) error {
	if len(n.subnets) == 0 {
		return nil
	}
	logger.Debug("subscribing to subnets", fields.Subnets(n.subnets))
	for i, val := range n.subnets {
		if val > 0 {
			subnet := fmt.Sprintf("%d", i)
			if err := n.topicsCtrl.Subscribe(logger, subnet); err != nil {
				logger.Warn("could not subscribe to subnet",
					zap.String("subnet", subnet), zap.Error(err))
				// TODO: handle error
			}
		}
	}
	return nil
}

package p2pv1

import (
	"encoding/hex"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/logging/fields"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/network"
	"github.com/bloxapp/ssv/protocol/v2/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
)

const (
	validatorStateInactive    int32 = 0
	validatorStateSubscribing int32 = 1
	validatorStateSubscribed  int32 = 2
)

// UseMessageRouter registers a message router to handle incoming messages
func (n *p2pNetwork) UseMessageRouter(router network.MessageRouter) {
	n.msgRouter = router
}

// Peers registers a message router to handle incoming messages
func (n *p2pNetwork) Peers(pk spectypes.ValidatorPK) ([]peer.ID, error) {
	all := make([]peer.ID, 0)
	topics := n.fork.ValidatorTopicID(pk)
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

	raw, err := n.fork.EncodeNetworkMsg(msg)
	if err != nil {
		return errors.Wrap(err, "could not decode msg")
	}

	vpk := msg.GetID().GetPubKey()
	topics := n.fork.ValidatorTopicID(vpk)

	for _, topic := range topics {
		if err := n.topicsCtrl.Broadcast(topic, raw, n.cfg.RequestTimeout); err != nil {
			n.interfaceLogger.Debug("could not broadcast msg", fields.PubKey(vpk), zap.Error(err))
			return errors.Wrap(err, "could not broadcast msg")
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
	if !n.setValidatorStateSubscribing(pkHex) {
		return nil
	}
	err := n.subscribe(n.interfaceLogger, pk)
	if err != nil {
		return err
	}
	n.setValidatorStateSubscribed(pkHex)
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(logger *zap.Logger, pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	pkHex := hex.EncodeToString(pk)
	if !n.canUnsubscribe(pkHex) {
		return nil
	}
	topics := n.fork.ValidatorTopicID(pk)
	for _, topic := range topics {
		if err := n.topicsCtrl.Unsubscribe(logger, topic, false); err != nil {
			return err
		}
	}
	n.clearValidatorState(pkHex)
	return nil
}

// subscribe to validator topics, as defined in the fork
func (n *p2pNetwork) subscribe(logger *zap.Logger, pk spectypes.ValidatorPK) error {
	topics := n.fork.ValidatorTopicID(pk)
	for _, topic := range topics {
		if err := n.topicsCtrl.Subscribe(logger, topic); err != nil {
			// return errors.Wrap(err, "could not broadcast message")
			return err
		}
	}
	return nil
}

// setValidatorStateSubscribing swaps the validator state to validatorStateSubscribing
// if the current state is not subscribed or subscribing
func (n *p2pNetwork) setValidatorStateSubscribing(pkHex string) bool {
	n.activeValidatorsLock.Lock()
	defer n.activeValidatorsLock.Unlock()
	currentState := n.activeValidators[pkHex]
	switch currentState {
	case validatorStateSubscribed, validatorStateSubscribing:
		return false
	default:
		n.activeValidators[pkHex] = validatorStateSubscribing
	}
	return true
}

// setValidatorStateSubscribed swaps the validator state to validatorStateSubscribed
// if currentState is subscribing
func (n *p2pNetwork) setValidatorStateSubscribed(pkHex string) bool {
	n.activeValidatorsLock.Lock()
	defer n.activeValidatorsLock.Unlock()
	currentState, ok := n.activeValidators[pkHex]
	if !ok {
		return false
	}
	switch currentState {
	case validatorStateInactive, validatorStateSubscribed:
		return false
	default:
		n.activeValidators[pkHex] = validatorStateSubscribed
	}
	return true
}

// canUnsubscribe checks we should unsubscribe from the given validator
func (n *p2pNetwork) canUnsubscribe(pkHex string) bool {
	n.activeValidatorsLock.Lock()
	defer n.activeValidatorsLock.Unlock()

	currentState, ok := n.activeValidators[pkHex]
	if !ok {
		return false
	}
	return currentState != validatorStateInactive
}

// clearValidatorState clears validator state
func (n *p2pNetwork) clearValidatorState(pkHex string) {
	n.activeValidatorsLock.Lock()
	defer n.activeValidatorsLock.Unlock()

	delete(n.activeValidators, pkHex)
}

var pubKeysToLog = map[string]struct{}{
	"a23efab72ad458f7edb626476afab9c6dae2f59d3136c4be76df621927c549a32cda2aa22188266b203dd9fe33b50468": {},
	"b3b9acf0d47d27b07f398fff103d0db6f0c688cc6257c5201f826bf9cec6a055b99418527fb12b1d1fde627a371e297a": {},
	"973550aa35e7c22453ab4065965c56e57c84794270b629158dcd81201f530442f2cbbef1f04d7c177f5fff1a66bcde9d": {},
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

		if _, ok := pubKeysToLog[hex.EncodeToString(ssvMsg.MsgID.GetPubKey())]; ok {
			logger = logger.With(
				zap.String("pubKey", hex.EncodeToString(ssvMsg.MsgID.GetPubKey())),
				zap.String("role", ssvMsg.MsgID.GetRoleType().String()),
			)
			logger.Debug("handlePubsubMessages")
		}

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

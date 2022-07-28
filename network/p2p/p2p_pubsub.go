package p2pv1

import (
	"encoding/hex"
	"fmt"
	"github.com/bloxapp/ssv/network"
	forksv1 "github.com/bloxapp/ssv/network/forks/v1"
	"github.com/bloxapp/ssv/protocol/v1/message"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
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
func (n *p2pNetwork) Peers(pk message.ValidatorPK) ([]peer.ID, error) {
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
func (n *p2pNetwork) Broadcast(msg message.SSVMessage) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	raw, err := n.fork.EncodeNetworkMsg(&msg)
	if err != nil {
		return errors.Wrap(err, "could not decode msg")
	}
	vpk := msg.GetIdentifier().GetValidatorPK()
	topics := n.fork.ValidatorTopicID(vpk)
	// for decided message, send on decided channel first
	logger := n.logger.With(zap.String("pk", hex.EncodeToString(vpk)))
	if msg.MsgType == message.SSVDecidedMsgType {
		if decidedTopic := n.fork.DecidedTopic(); len(decidedTopic) > 0 {
			topics = append([]string{decidedTopic}, topics...)
		}
	}
	sm := message.SignedMessage{}
	if err := sm.Decode(msg.Data); err == nil && sm.Message != nil {
		logger = logger.With(zap.Int64("height", int64(sm.Message.Height)),
			zap.String("consensusMsgType", sm.Message.MsgType.String()),
			zap.Any("signers", sm.GetSigners()))
	}
	for _, topic := range topics {
		if topic == forksv1.UnknownSubnet {
			return errors.New("unknown topic")
		}
		logger.Debug("trying to broadcast message", zap.String("topic", topic), zap.Any("msg", msg))
		if err := n.topicsCtrl.Broadcast(topic, raw, n.cfg.RequestTimeout); err != nil {
			return errors.Wrap(err, "could not broadcast msg")
		}
	}
	return nil
}

// Subscribe subscribes to validator subnet
func (n *p2pNetwork) Subscribe(pk message.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	pkHex := hex.EncodeToString(pk)
	if !n.setValidatorStateSubscribing(pkHex) {
		return nil
	}
	err := n.subscribe(pk)
	if err != nil {
		return err
	}
	n.setValidatorStateSubscribed(pkHex)
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(pk message.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	pkHex := hex.EncodeToString(pk)
	if !n.canUnsubscribe(pkHex) {
		return nil
	}
	topics := n.fork.ValidatorTopicID(pk)
	for _, topic := range topics {
		if topic == forksv1.UnknownSubnet {
			return errors.New("unknown topic")
		}
		if err := n.topicsCtrl.Unsubscribe(topic, false); err != nil {
			return err
		}
	}
	n.clearValidatorState(pkHex)
	return nil
}

// subscribe subscribes to validator topics, as defined in the fork
func (n *p2pNetwork) subscribe(pk message.ValidatorPK) error {
	topics := n.fork.ValidatorTopicID(pk)
	for _, topic := range topics {
		if topic == forksv1.UnknownSubnet {
			return errors.New("unknown topic")
		}
		if err := n.topicsCtrl.Subscribe(topic); err != nil {
			//return errors.Wrap(err, "could not broadcast message")
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

// handleIncomingMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handlePubsubMessages(topic string, msg *pubsub.Message) error {
	logger := n.logger.With(zap.String("topic", topic))
	if n.msgRouter == nil {
		n.logger.Warn("msg router is not configured")
		return nil
	}
	if msg == nil {
		return nil
	}
	ssvMsg, err := n.fork.DecodeNetworkMsg(msg.GetData())
	if err != nil {
		logger.Warn("could not decode message", zap.Error(err))
		// TODO: handle..
		return nil
	}
	if ssvMsg == nil {
		return nil
	}
	//logger = withIncomingMsgFields(logger, msg, ssvMsg)
	//logger.Debug("incoming pubsub message", zap.String("topic", topic),
	//	zap.String("msgType", ssvMsg.MsgType.String()))
	metricsRouterIncoming.WithLabelValues(ssvMsg.ID.String(), ssvMsg.MsgType.String()).Inc()
	n.msgRouter.Route(*ssvMsg)
	return nil
}

//// withIncomingMsgFields adds fields to the given logger
//func withIncomingMsgFields(logger *zap.Logger, msg *pubsub.Message, ssvMsg *message.SSVMessage) *zap.Logger {
//	logger = logger.With(zap.String("identifier", ssvMsg.ID.String()))
//	if ssvMsg.MsgType == message.SSVDecidedMsgType || ssvMsg.MsgType == message.SSVConsensusMsgType {
//		logger = logger.With(zap.String("receivedFrom", msg.GetFrom().String()))
//		from, err := peer.IDFromBytes(msg.Message.GetFrom())
//		if err == nil {
//			logger = logger.With(zap.String("msgFrom", from.String()))
//		}
//		var sm message.SignedMessage
//		err = sm.Decode(ssvMsg.Data)
//		if err == nil && sm.Message != nil {
//			logger = logger.With(zap.Int64("height", int64(sm.Message.Height)),
//				zap.String("consensusMsgType", sm.Message.MsgType.String()),
//				zap.Any("signers", sm.GetSigners()))
//		}
//	}
//	return logger
//}

// subscribeToSubnets subscribes to all the node's subnets
func (n *p2pNetwork) subscribeToSubnets() error {
	if len(n.subnets) == 0 {
		return nil
	}
	n.logger.Debug("subscribing to subnets", zap.ByteString("subnets", n.subnets))
	for i, val := range n.subnets {
		if val > 0 {
			subnet := fmt.Sprintf("%d", i)
			if err := n.topicsCtrl.Subscribe(subnet); err != nil {
				n.logger.Warn("could not subscribe to subnet",
					zap.String("subnet", subnet), zap.Error(err))
				// TODO: handle error
			}
		}
	}
	return nil
}

package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	"github.com/ssvlabs/ssv/protocol/v2/message"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/records"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
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
	topics, err := n.broadcastTopics(pk)
	if err != nil {
		return nil, fmt.Errorf("could not get validator topics: %w", err)
	}
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
func (n *p2pNetwork) Broadcast(msgID spectypes.MessageID, msg *spectypes.SignedSSVMessage) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	if !n.operatorDataStore.OperatorIDReady() {
		return fmt.Errorf("operator ID is not ready")
	}

	// TODO: (genesis) old encoding
	// encodedMsg, err := commons.EncodeNetworkMsg(msg)
	// if err != nil {
	// 	return errors.Wrap(err, "could not decode msg")
	// }
	// signature, err := n.operatorSigner.Sign(encodedMsg)
	// if err != nil {
	// 	return err
	// }
	// encodedMsg = commons.EncodeSignedSSVMessage(encodedMsg, n.operatorDataStore.GetOperatorID(), signature)

	encodedMsg, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("could not encode signed ssv message: %w", err)
	}

	topics, err := n.broadcastTopics(spectypes.ValidatorPK(msg.SSVMessage.MsgID.GetDutyExecutorID()))
	if err != nil {
		return fmt.Errorf("could not get validator topics: %w", err)
	}

	for _, topic := range topics {
		if err := n.topicsCtrl.Broadcast(topic, encodedMsg, n.cfg.RequestTimeout); err != nil {
			n.interfaceLogger.Debug("could not broadcast msg", fields.Topic(topic), zap.Error(err))
			return fmt.Errorf("could not broadcast msg: %w", err)
		}
	}
	return nil
}

func (n *p2pNetwork) SubscribeAll(logger *zap.Logger) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	n.fixedSubnets, _ = records.Subnets{}.FromString(records.AllSubnets)
	for subnet := 0; subnet < commons.Subnets(); subnet++ {
		err := n.topicsCtrl.Subscribe(logger, commons.SubnetTopicID(subnet))
		if err != nil {
			return err
		}
	}
	return nil
}

// SubscribeRandoms subscribes to random subnets. This method isn't thread-safe.
func (n *p2pNetwork) SubscribeRandoms(logger *zap.Logger, numSubnets int) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if numSubnets > commons.Subnets() {
		numSubnets = commons.Subnets()
	}

	// Subscribe to random subnets.
	// #nosec G404
	randomSubnets := rand.New(rand.NewSource(time.Now().UnixNano())).Perm(commons.Subnets())
	randomSubnets = randomSubnets[:numSubnets]
	for _, subnet := range randomSubnets {
		err := n.topicsCtrl.Subscribe(logger, commons.SubnetTopicID(subnet))
		if err != nil {
			return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
		}
	}

	// Update the subnets slice.
	subnets := make([]byte, commons.Subnets())
	copy(subnets, n.fixedSubnets)
	for _, subnet := range randomSubnets {
		subnets[subnet] = byte(1)
	}
	n.fixedSubnets = subnets

	return nil
}

// Subscribe subscribes to validator subnet
func (n *p2pNetwork) Subscribe(pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	share := n.validatorStore.Validator(pk[:])
	if share == nil {
		return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(pk[:]))
	}

	err := n.subscribeCommittee(share.CommitteeID())
	if err != nil {
		return fmt.Errorf("could not subscribe to committee: %w", err)
	}
	if !n.cfg.Network.PastAlanFork() {
		return n.subscribeValidator(pk)
	}
	return nil
}

// subscribeCommittee handles the subscription logic for committee subnets
func (n *p2pNetwork) subscribeCommittee(cid spectypes.CommitteeID) error {
	status, found := n.activeCommittees.GetOrInsert(string(cid[:]), validatorStatusSubscribing)
	if found && status != validatorStatusInactive {
		return nil
	}

	for _, topic := range commons.CommitteeTopicID(cid) {
		if err := n.topicsCtrl.Subscribe(n.interfaceLogger, topic); err != nil {
			return fmt.Errorf("could not subscribe to topic %s: %w", topic, err)
		}
	}
	n.activeCommittees.Set(string(cid[:]), validatorStatusSubscribed)
	return nil
}

// subscribeValidator handles the subscription logic for validator subnets
func (n *p2pNetwork) subscribeValidator(pk spectypes.ValidatorPK) error {
	pkHex := hex.EncodeToString(pk[:])
	status, found := n.activeValidators.GetOrInsert(pkHex, validatorStatusSubscribing)
	if found && status != validatorStatusInactive {
		return nil
	}
	for _, topic := range commons.ValidatorTopicID(pk[:]) {
		if err := n.topicsCtrl.Subscribe(n.interfaceLogger, topic); err != nil {
			return fmt.Errorf("could not subscribe to topic %s: %w", topic, err)
		}
	}
	n.activeValidators.Set(pkHex, validatorStatusSubscribed)
	return nil
}

func (n *p2pNetwork) unsubscribeSubnet(logger *zap.Logger, subnet uint) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if subnet >= uint(commons.Subnets()) {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	if err := n.topicsCtrl.Unsubscribe(logger, commons.SubnetTopicID(int(subnet)), false); err != nil {
		return fmt.Errorf("could not unsubscribe from subnet %d: %w", subnet, err)
	}
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(logger *zap.Logger, pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	if !n.cfg.Network.PastAlanFork() {
		pkHex := hex.EncodeToString(pk[:])
		if status, _ := n.activeValidators.Get(pkHex); status != validatorStatusSubscribed {
			return nil
		}
		n.activeValidators.Del(pkHex)
	}

	cmtid := n.nodeStorage.ValidatorStore().Validator(pk[:]).CommitteeID()
	topics := commons.CommitteeTopicID(cmtid)
	for _, topic := range topics {
		if err := n.topicsCtrl.Unsubscribe(logger, topic, false); err != nil {
			return err
		}
	}
	n.activeCommittees.Del(string(cmtid[:]))
	return nil
}

// handleIncomingMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handlePubsubMessages(logger *zap.Logger) func(ctx context.Context, topic string, msg *pubsub.Message) error {
	return func(ctx context.Context, topic string, msg *pubsub.Message) error {
		if n.msgRouter == nil {
			logger.Debug("msg router is not configured")
			return nil
		}
		if msg == nil {
			return nil
		}

		var decodedMsg *queue.DecodedSSVMessage
		if msg.ValidatorData != nil {
			m, ok := msg.ValidatorData.(*queue.DecodedSSVMessage)
			if ok {
				decodedMsg = m
			}
		}
		if decodedMsg == nil {
			return errors.New("message was not decoded")
		}

		metricsRouterIncoming.WithLabelValues(message.MsgTypeToString(decodedMsg.MsgType)).Inc()

		n.msgRouter.Route(ctx, decodedMsg)

		return nil
	}
}

// subscribeToSubnets subscribes to all the node's subnets
func (n *p2pNetwork) subscribeToSubnets(logger *zap.Logger) error {
	if len(n.fixedSubnets) == 0 {
		return nil
	}
	logger.Debug("subscribing to fixed subnets", fields.Subnets(n.fixedSubnets))
	for i, val := range n.fixedSubnets {
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

func (n *p2pNetwork) broadcastTopics(pk spectypes.ValidatorPK) ([]string, error) {
	pkBytes := pk[:]
	if n.cfg.Network.PastAlanFork() {
		share := n.nodeStorage.ValidatorStore().Validator(pkBytes)
		if share == nil {
			return nil, fmt.Errorf("could not find share for validator %s", hex.EncodeToString(pkBytes))
		}
		cid := share.CommitteeID()
		return commons.CommitteeTopicID(cid), nil
	}
	return commons.ValidatorTopicID(pkBytes), nil
}

func (n *p2pNetwork) subscriptionTopics(pk spectypes.ValidatorPK) ([]string, error) {
	pkBytes := pk[:]
	var topics []string

	if !n.cfg.Network.PastAlanFork() {
		topics = commons.ValidatorTopicID(pkBytes)
	}

	share := n.nodeStorage.ValidatorStore().Validator(pkBytes)
	if share == nil {
		return nil, fmt.Errorf("could not find share for validator %s", hex.EncodeToString(pkBytes))
	}
	cid := share.CommitteeID()
	topics = append(topics, commons.CommitteeTopicID(cid)...)
	return topics, nil
}

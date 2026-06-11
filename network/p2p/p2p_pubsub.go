package p2pv1

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/observability/log/fields"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

// committeeSubscriptionStatus reflects the state of committee subscription (whether we are subscribed to the
// corresponding p2p topic).
type committeeSubscriptionStatus int

const (
	committeeSubscriptionStatusInactive    committeeSubscriptionStatus = 0
	committeeSubscriptionStatusSubscribing committeeSubscriptionStatus = 1
	committeeSubscriptionStatusSubscribed  committeeSubscriptionStatus = 2
)

// UseMessageRouter registers a message router to handle incoming messages
func (n *p2pNetwork) UseMessageRouter(router network.MessageRouter) {
	n.msgRouter = router
}

// Broadcast publishes the message to all peers in subnet
func (n *p2pNetwork) Broadcast(msgID spectypes.MessageID, msg *spectypes.SignedSSVMessage) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	if !n.operatorDataStore.OperatorIDReady() {
		return fmt.Errorf("operator ID is not ready")
	}

	encodedMsg, err := msg.Encode()
	if err != nil {
		return fmt.Errorf("could not encode signed ssv message: %w", err)
	}

	role := msg.SSVMessage.MsgID.GetRoleType()

	var topicNames []string
	if role == spectypes.RoleCommittee {
		topicNames = commons.CommitteeTopicID(spectypes.CommitteeID(msg.SSVMessage.MsgID.GetDutyExecutorID()[16:]))
	} else {
		val, exists := n.nodeStorage.ValidatorStore().Validator(msg.SSVMessage.MsgID.GetDutyExecutorID())
		if !exists {
			return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
		}
		topicNames = commons.CommitteeTopicID(val.CommitteeID())
	}

	for _, topic := range topicNames {
		if err := n.topicsCtrl.Broadcast(topic, encodedMsg, n.cfg.RequestTimeout); err != nil {
			n.logger.Debug("could not broadcast msg", fields.Topic(topic), zap.Error(err))
			return fmt.Errorf("could not broadcast msg: %w", err)
		}

		// topicPeers surfaces a sparse/empty mesh — a prime suspect when a one-shot message fails to reach peers.
		topicPeers, _ := n.topicsCtrl.Peers(topic)
		n.logger.Debug("📤 broadcast message to topic",
			fields.MessageID(msg.SSVMessage.MsgID),
			zap.String("gossip_msg_id", hex.EncodeToString([]byte(topics.MsgID(encodedMsg)))),
			fields.Topic(topic),
			zap.Int("topic_peers", len(topicPeers)),
		)
	}
	return nil
}

func (n *p2pNetwork) SubscribeAll() error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	n.persistentSubnets = commons.AllSubnets
	for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
		err := n.topicsCtrl.Subscribe(commons.SubnetTopicID(subnet))
		if err != nil {
			return err
		}
	}
	return nil
}

// SubscribeRandoms subscribes to random subnets. This method isn't thread-safe.
func (n *p2pNetwork) SubscribeRandoms(numSubnets int) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if numSubnets <= 0 {
		return nil
	}
	if numSubnets > commons.SubnetsCount {
		numSubnets = commons.SubnetsCount
	}

	currentSubnets := n.currentSubnetsSnapshot()
	availableSubnetsCount := commons.SubnetsCount - currentSubnets.ActiveCount()
	if numSubnets > availableSubnetsCount {
		return fmt.Errorf("not enough available subnets: requested %d, available %d", numSubnets, availableSubnetsCount)
	}

	availableSubnets := make([]uint64, 0, availableSubnetsCount)
	for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
		if !currentSubnets.IsSet(subnet) {
			availableSubnets = append(availableSubnets, subnet)
		}
	}

	rng := rand.New(rand.NewSource(time.Now().UnixNano())) // #nosec G404
	rng.Shuffle(len(availableSubnets), func(i, j int) {
		availableSubnets[i], availableSubnets[j] = availableSubnets[j], availableSubnets[i]
	})

	randomSubnets := availableSubnets[:numSubnets]

	for _, subnet := range randomSubnets {
		err := n.topicsCtrl.Subscribe(commons.SubnetTopicID(subnet))
		if err != nil {
			return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
		}
	}

	for _, subnet := range randomSubnets {
		n.persistentSubnets.Set(subnet)
	}

	return nil
}

// SubscribedSubnets returns the subnets the node is subscribed to, consisting of the fixed subnets
// and the active committees/validators.
func (n *p2pNetwork) SubscribedSubnets() commons.Subnets {
	// Compute the new subnets according to the active committees/validators.
	updatedSubnets := n.persistentSubnets

	n.subscribedCommittees.Range(func(cid string, status committeeSubscriptionStatus) bool {
		subnet := commons.CommitteeSubnet(spectypes.CommitteeID([]byte(cid)))
		updatedSubnets.Set(subnet)
		return true
	})

	return updatedSubnets
}

// Subscribe subscribes to validator subnet
func (n *p2pNetwork) Subscribe(pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	share, exists := n.nodeStorage.ValidatorStore().Validator(pk[:])
	if !exists {
		return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(pk[:]))
	}

	err := n.subscribeCommittee(share.CommitteeID())
	if err != nil {
		return fmt.Errorf("could not subscribe to committee: %w", err)
	}

	return nil
}

// subscribeCommittee subscribes us to the topic that corresponds to cid committee, also
// ensuring we only subscribe once (when the committee is "newly activated").
func (n *p2pNetwork) subscribeCommittee(cid spectypes.CommitteeID) error {
	status, found := n.subscribedCommittees.GetOrSet(string(cid[:]), committeeSubscriptionStatusSubscribing)
	if found && status != committeeSubscriptionStatusInactive {
		return nil
	}

	n.logger.Debug("subscribing to a topic corresponding to a newly activated committee", fields.CommitteeID(cid))

	for _, topic := range commons.CommitteeTopicID(cid) {
		if err := n.topicsCtrl.Subscribe(topic); err != nil {
			return fmt.Errorf("could not subscribe to topic %s: %w", topic, err)
		}
	}

	n.subscribedCommittees.Set(string(cid[:]), committeeSubscriptionStatusSubscribed)

	return nil
}

func (n *p2pNetwork) unsubscribeSubnet(subnet uint64) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if subnet >= commons.SubnetsCount {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	if err := n.topicsCtrl.Unsubscribe(commons.SubnetTopicID(subnet), false); err != nil {
		return fmt.Errorf("could not unsubscribe from subnet %d: %w", subnet, err)
	}
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(pk spectypes.ValidatorPK) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}

	share, exists := n.nodeStorage.ValidatorStore().Validator(pk[:])
	if !exists {
		return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(pk[:]))
	}

	cmtid := share.CommitteeID()
	topics := commons.CommitteeTopicID(cmtid)
	for _, topic := range topics {
		if err := n.topicsCtrl.Unsubscribe(topic, false); err != nil {
			return err
		}
	}
	n.subscribedCommittees.Delete(string(cmtid[:]))
	return nil
}

// handlePubsubMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handlePubsubMessages() func(ctx context.Context, topic string, msg *pubsub.Message) error {
	return func(ctx context.Context, topic string, msg *pubsub.Message) error {
		if n.msgRouter == nil {
			n.logger.Debug("msg router is not configured")
			return nil
		}
		if msg == nil {
			return nil
		}

		var decodedMsg network.DecodedSSVMessage
		switch m := msg.ValidatorData.(type) {
		case *queue.SSVMessage:
			decodedMsg = m
		case nil:
			return errors.New("message was not decoded")
		default:
			return fmt.Errorf("unknown decoded message type: %T", m)
		}

		n.msgRouter.Route(ctx, decodedMsg)

		return nil
	}
}

// subscribeToFixedSubnets subscribes to all the node's persistent subnets.
func (n *p2pNetwork) subscribeToFixedSubnets() {
	if !n.persistentSubnets.HasActive() {
		return
	}

	n.logger.Debug("subscribing to fixed subnets", zap.String("persistent_subnets", n.persistentSubnets.StringHumanReadable()))

	for _, subnet := range n.persistentSubnets.SubnetList() {
		if err := n.topicsCtrl.Subscribe(strconv.FormatUint(subnet, 10)); err != nil {
			n.logger.Error("could not subscribe to subnet", zap.Uint64("subnet", subnet), zap.Error(err))
		}
	}
}

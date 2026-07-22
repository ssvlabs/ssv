package p2pv1

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"

	specqbft "github.com/ssvlabs/ssv-spec/qbft"
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/topics"
	"github.com/ssvlabs/ssv/observability/log/fields"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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
	msgSlot, err := broadcastMessageSlot(msg)
	if err != nil {
		return fmt.Errorf("could not resolve message slot: %w", err)
	}

	return n.BroadcastAtSlot(msg, msgSlot)
}

// broadcastMessageSlot resolves the slot of a message that's about to be broadcast, decoding only
// the message body type actually sent over the wire (consensus or partial-signature), rather than
// building a full queue.DecodedSSVMessage wrapper that Broadcast has no other use for.
func broadcastMessageSlot(msg *spectypes.SignedSSVMessage) (phase0.Slot, error) {
	switch msg.SSVMessage.MsgType {
	case spectypes.SSVConsensusMsgType:
		qbftMsg := &specqbft.Message{}
		if err := qbftMsg.Decode(msg.SSVMessage.Data); err != nil {
			return 0, fmt.Errorf("failed to decode qbft.Message: %w", err)
		}
		return phase0.Slot(qbftMsg.Height), nil
	case spectypes.SSVPartialSignatureMsgType:
		psMsg := &spectypes.PartialSignatureMessages{}
		if err := psMsg.Decode(msg.SSVMessage.Data); err != nil {
			return 0, fmt.Errorf("failed to decode PartialSignatureMessages: %w", err)
		}
		return psMsg.Slot, nil
	default:
		return 0, queue.ErrUnknownMessageType
	}
}

// BroadcastAtSlot behaves like Broadcast but takes the message's slot explicitly, so it can
// choose between the Alan and Boole topics depending on whether the slot is post-Boole-fork.
func (n *p2pNetwork) BroadcastAtSlot(msg *spectypes.SignedSSVMessage, slot phase0.Slot) error {
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

	var topic string
	role := msg.SSVMessage.MsgID.GetRoleType()
	if role == spectypes.RoleCommittee || role == spectypes.RoleAggregatorCommittee {
		committeeID := spectypes.CommitteeID(msg.SSVMessage.MsgID.GetDutyExecutorID()[16:])
		if n.cfg.NetworkConfig.BooleForkAtSlot(slot) {
			val, exists := n.nodeStorage.ValidatorStore().Committee(committeeID)
			if !exists {
				return fmt.Errorf("could not find share for committee %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
			}
			topic = commons.BooleTopic(n.cfg.NetworkConfig.SSV.Name, val.BooleSubnet)
		} else {
			topic = commons.GetTopicFullName(commons.SubnetTopicID(commons.AlanCommitteeSubnet(committeeID)))
		}
	} else {
		val, exists := n.nodeStorage.ValidatorStore().Validator(msg.SSVMessage.MsgID.GetDutyExecutorID())
		if !exists {
			return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
		}
		if n.cfg.NetworkConfig.BooleForkAtSlot(slot) {
			topic = commons.BooleTopic(n.cfg.NetworkConfig.SSV.Name, val.BooleCommitteeSubnet())
		} else {
			topic = commons.GetTopicFullName(commons.SubnetTopicID(val.AlanCommitteeSubnet()))
		}
	}

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
	return nil
}

func (n *p2pNetwork) SubscribeAll() error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	n.persistentSubnets = commons.AllSubnets
	for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
		if err := n.subscribeSubnetForCurrentEpoch(subnet); err != nil {
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
		if err := n.subscribeSubnetForCurrentEpoch(subnet); err != nil {
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
	alanSubnets, booleSubnets := n.subscribedSubnetsForCurrentEpoch()
	return unionSubnets(alanSubnets, booleSubnets)
}

// subscribedSubnetsForCurrentEpoch computes the desired Alan and Boole subnet sets for the
// current slot, based on the node's persistent subnets and active committee subscriptions.
// TODO: Remove Alan subnets after the Boole fork transition logic is dropped.
func (n *p2pNetwork) subscribedSubnetsForCurrentEpoch() (commons.Subnets, commons.Subnets) {
	currentSlot := n.cfg.NetworkConfig.EstimatedCurrentSlot()
	alanSubnets := commons.ZeroSubnets
	booleSubnets := commons.ZeroSubnets

	// Evaluate the time-derived fork state once so the persistent and committee sides
	// can't observe a mixed Alan/Boole snapshot if it flips mid-iteration.
	inTransitionWindow := n.cfg.NetworkConfig.InBooleTransitionWindow(currentSlot)
	postBooleFork := n.cfg.NetworkConfig.BooleForkAtSlot(currentSlot)

	switch {
	case inTransitionWindow:
		alanSubnets = n.persistentSubnets
		booleSubnets = n.persistentSubnets
	case postBooleFork:
		booleSubnets = n.persistentSubnets
	default:
		alanSubnets = n.persistentSubnets
	}

	n.subscribedCommittees.Range(func(encodedCommittee string, statusAndSubnet statusWithSubnet) bool {
		switch {
		case inTransitionWindow:
			alanSubnets.Set(statusAndSubnet.alanSubnet)
			booleSubnets.Set(statusAndSubnet.booleSubnet)
		case postBooleFork:
			booleSubnets.Set(statusAndSubnet.booleSubnet)
		default:
			alanSubnets.Set(statusAndSubnet.alanSubnet)
		}
		return true
	})

	return alanSubnets, booleSubnets
}

func unionSubnets(left, right commons.Subnets) commons.Subnets {
	union := left
	for _, subnet := range right.SubnetList() {
		union.Set(subnet)
	}
	return union
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

	if err := n.subscribeCommittee(share); err != nil {
		return fmt.Errorf("could not subscribe to committee: %w", err)
	}

	return nil
}

// subscribeCommittee subscribes us to the topic(s) that correspond to the share's committee, also
// ensuring we only subscribe once (when the committee is "newly activated").
func (n *p2pNetwork) subscribeCommittee(share *ssvtypes.SSVShare) error {
	cid := share.CommitteeID()

	statusToSet := statusWithSubnet{
		status:      committeeSubscriptionStatusSubscribing,
		booleSubnet: share.BooleCommitteeSubnet(),
		alanSubnet:  share.AlanCommitteeSubnet(),
	}
	currentStatus, found := n.subscribedCommittees.GetOrSet(string(cid[:]), statusToSet)
	if found && currentStatus.status != committeeSubscriptionStatusInactive {
		return nil
	}

	topicSet := n.committeeTopicSetForCurrentEpoch(share)

	n.logger.Debug("subscribing to a topic corresponding to a newly activated committee", fields.CommitteeID(cid))

	for topic := range topicSet {
		if err := n.topicsCtrl.Subscribe(topic); err != nil {
			return fmt.Errorf("could not subscribe to topic %s: %w", topic, err)
		}
	}

	statusToSet.status = committeeSubscriptionStatusSubscribed
	n.subscribedCommittees.Set(string(cid[:]), statusToSet)

	return nil
}

// subscribeSubnetForCurrentEpoch subscribes to the given subnet's topic(s) for the current
// slot's fork state: Alan-only pre-fork, both Alan and Boole during the transition window,
// Boole-only after.
func (n *p2pNetwork) subscribeSubnetForCurrentEpoch(subnet uint64) error {
	currentSlot := n.cfg.NetworkConfig.EstimatedCurrentSlot()
	switch {
	case n.cfg.NetworkConfig.InBooleTransitionWindow(currentSlot):
		if err := n.subscribeSubnet(subnet, false); err != nil {
			return err
		}
		return n.subscribeSubnet(subnet, true)
	case n.cfg.NetworkConfig.BooleForkAtSlot(currentSlot):
		return n.subscribeSubnet(subnet, true)
	default:
		return n.subscribeSubnet(subnet, false)
	}
}

func (n *p2pNetwork) subscribeSubnet(subnet uint64, useBoole bool) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if subnet >= commons.SubnetsCount {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	topic := commons.GetTopicFullName(commons.SubnetTopicID(subnet))
	if useBoole {
		topic = commons.BooleTopic(n.cfg.NetworkConfig.SSV.Name, subnet)
	}
	if err := n.topicsCtrl.Subscribe(topic); err != nil {
		return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
	}
	return nil
}

func (n *p2pNetwork) unsubscribeSubnet(subnet uint64, useBoole bool) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if subnet >= commons.SubnetsCount {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	topic := commons.GetTopicFullName(commons.SubnetTopicID(subnet))
	if useBoole {
		topic = commons.BooleTopic(n.cfg.NetworkConfig.SSV.Name, subnet)
	}
	if err := n.topicsCtrl.Unsubscribe(topic, false); err != nil {
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

	for topic := range n.committeeTopicSetForCurrentEpoch(share) {
		if err := n.topicsCtrl.Unsubscribe(topic, false); err != nil {
			return err
		}
	}

	cid := share.CommitteeID()
	n.subscribedCommittees.Delete(string(cid[:]))
	return nil
}

// committeeTopicSetForCurrentEpoch returns the set of topics (Alan and/or Boole) that the
// given share's committee should be subscribed to for the current slot's fork state.
func (n *p2pNetwork) committeeTopicSetForCurrentEpoch(share *ssvtypes.SSVShare) map[string]struct{} {
	currentSlot := n.cfg.NetworkConfig.EstimatedCurrentSlot()
	alanTopic := commons.GetTopicFullName(commons.SubnetTopicID(share.AlanCommitteeSubnet()))
	booleTopic := commons.BooleTopic(n.cfg.NetworkConfig.SSV.Name, share.BooleCommitteeSubnet())
	topicSet := make(map[string]struct{})

	switch {
	case n.cfg.NetworkConfig.InBooleTransitionWindow(currentSlot):
		topicSet[alanTopic] = struct{}{}
		topicSet[booleTopic] = struct{}{}
	case n.cfg.NetworkConfig.BooleForkAtSlot(currentSlot):
		topicSet[booleTopic] = struct{}{}
	default:
		topicSet[alanTopic] = struct{}{}
	}

	return topicSet
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
		if err := n.subscribeSubnetForCurrentEpoch(subnet); err != nil {
			n.logger.Error("could not subscribe to subnet", zap.Uint64("subnet", subnet), zap.Error(err))
		}
	}
}

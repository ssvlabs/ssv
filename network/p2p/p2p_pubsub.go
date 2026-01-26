package p2pv1

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"math/rand"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
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
		if n.cfg.NetworkConfig.BooleFork() {
			val, exists := n.nodeStorage.ValidatorStore().Committee(committeeID)
			if !exists {
				return fmt.Errorf("could not find share for committee %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
			}
			topic = val.BooleSubnet.BooleTopic(n.cfg.NetworkConfig.Beacon.Name)
		} else {
			topic = commons.AlanCommitteeSubnet(committeeID).AlanTopic()
		}
	} else {
		val, exists := n.nodeStorage.ValidatorStore().Validator(msg.SSVMessage.MsgID.GetDutyExecutorID())
		if !exists {
			return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
		}
		if n.cfg.NetworkConfig.BooleFork() {
			topic = val.BooleCommitteeSubnet().BooleTopic(n.cfg.NetworkConfig.Beacon.Name)
		} else {
			topic = val.AlanCommitteeSubnet().AlanTopic()
		}
	}

	if err := n.topicsCtrl.Broadcast(topic, encodedMsg, n.cfg.RequestTimeout); err != nil {
		n.logger.Debug("could not broadcast msg", fields.Topic(topic), zap.Error(err))
		return fmt.Errorf("could not broadcast msg: %w", err)
	}
	return nil
}

func (n *p2pNetwork) SubscribeAll() error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	n.persistentSubnets = commons.AllSubnets
	for subnet := commons.Subnet(0); subnet < commons.Subnet(commons.SubnetsCount); subnet++ {
		if err := n.subscribeSubnetForCurrentEpoch(subnet); err != nil {
			return err
		}
	}
	return nil
}

// SubscribeRandoms subscribes to random subnets. This method isn't thread-safe.
// #nosec G115 -- Perm slice is [0, n)
func (n *p2pNetwork) SubscribeRandoms(numSubnets int) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if numSubnets > commons.SubnetsCount {
		numSubnets = commons.SubnetsCount
	}

	var randomSubnets []int
	for {
		// pick random subnets
		randomSubnets = rand.New(rand.NewSource(time.Now().UnixNano())).Perm(commons.SubnetsCount) // #nosec G404
		randomSubnets = randomSubnets[:numSubnets]
		// check if any of subnets we've generated in this random set is already being used by us
		randSubnetAlreadyInUse := false
		for _, subnet := range randomSubnets {
			subnetID := commons.Subnet(subnet)
			if n.currentSubnets.IsSet(subnetID) {
				randSubnetAlreadyInUse = true
				break
			}
		}
		if !randSubnetAlreadyInUse {
			// found a set of random subnets that we aren't yet using
			break
		}
	}

	for _, subnet := range randomSubnets {
		if err := n.subscribeSubnetForCurrentEpoch(commons.Subnet(subnet)); err != nil {
			return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
		}
	}

	for _, subnet := range randomSubnets {
		n.persistentSubnets.Set(commons.Subnet(subnet))
	}

	return nil
}

// SubscribedSubnets returns the subnets the node is subscribed to, consisting of the fixed subnets
// and the active committees/validators.
func (n *p2pNetwork) SubscribedSubnets() commons.Subnets {
	alanSubnets, booleSubnets := n.subscribedSubnetsForCurrentEpoch()
	return unionSubnets(alanSubnets, booleSubnets)
}

// TODO: Remove Alan subnets after the Boole fork transition logic is dropped.
func (n *p2pNetwork) subscribedSubnetsForCurrentEpoch() (commons.Subnets, commons.Subnets) {
	currentSlot := n.cfg.NetworkConfig.EstimatedCurrentSlot()
	alanSubnets := commons.ZeroSubnets
	booleSubnets := commons.ZeroSubnets

	switch {
	case n.cfg.NetworkConfig.InBooleTransitionWindow(currentSlot):
		alanSubnets = n.persistentSubnets
		booleSubnets = n.persistentSubnets
	case n.cfg.NetworkConfig.BooleForkAtSlot(currentSlot):
		booleSubnets = n.persistentSubnets
	default:
		alanSubnets = n.persistentSubnets
	}

	n.subscribedCommittees.Range(func(encodedCommittee string, statusAndSubnet statusWithSubnet) bool {
		switch {
		case n.cfg.NetworkConfig.InBooleTransitionWindow(currentSlot):
			alanSubnets.Set(statusAndSubnet.alanSubnet)
			booleSubnets.Set(statusAndSubnet.booleSubnet)
		case n.cfg.NetworkConfig.BooleForkAtSlot(currentSlot):
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

// subscribeCommittee subscribes us to the topic that corresponds to cid committee, also
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

func (n *p2pNetwork) subscribeSubnetForCurrentEpoch(subnet commons.Subnet) error {
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

func (n *p2pNetwork) subscribeSubnet(subnet commons.Subnet, useBoole bool) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if uint64(subnet) >= commons.SubnetsCount {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	topic := subnet.AlanTopic()
	if useBoole {
		topic = subnet.BooleTopic(n.cfg.NetworkConfig.Beacon.Name)
	}
	if err := n.topicsCtrl.Subscribe(topic); err != nil {
		return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
	}
	return nil
}

func (n *p2pNetwork) unsubscribeSubnet(subnet commons.Subnet, useBoole bool) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if uint64(subnet) >= commons.SubnetsCount {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	topic := subnet.AlanTopic()
	if useBoole {
		topic = subnet.BooleTopic(n.cfg.NetworkConfig.Beacon.Name)
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

func (n *p2pNetwork) committeeTopicSetForCurrentEpoch(share *ssvtypes.SSVShare) map[string]struct{} {
	currentSlot := n.cfg.NetworkConfig.EstimatedCurrentSlot()
	alanTopic := share.AlanCommitteeSubnet().AlanTopic()
	booleTopic := share.BooleCommitteeSubnet().BooleTopic(n.cfg.NetworkConfig.Beacon.Name)
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
			n.logger.Error("could not subscribe to subnet", fields.Subnet(subnet), zap.Error(err))
		}
	}
}

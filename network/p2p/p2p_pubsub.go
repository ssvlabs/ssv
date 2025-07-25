package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"strconv"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	p2pprotocol "github.com/ssvlabs/ssv/protocol/v2/p2p"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
	ssvtypes "github.com/ssvlabs/ssv/protocol/v2/types"
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

	var topics []string

	if msg.SSVMessage.MsgID.GetRoleType() == spectypes.RoleCommittee {
		// Unlike the logic in p2p, where we subscribe the post-fork subnets before fork to be ready at the fork,
		// we don't expect post-fork messages to be sent before the fork.
		if n.cfg.NetworkConfig.NetworkTopologyFork() {
			val, exists := n.nodeStorage.ValidatorStore().Committee(spectypes.CommitteeID(msg.SSVMessage.MsgID.GetDutyExecutorID()[16:]))
			if !exists {
				return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
			}
			topics = []string{commons.SubnetTopicID(val.Subnet)}
		} else {
			topics = commons.CommitteeTopicIDAlan(spectypes.CommitteeID(msg.SSVMessage.MsgID.GetDutyExecutorID()[16:]))
		}
	} else {
		val, exists := n.nodeStorage.ValidatorStore().Validator(msg.SSVMessage.MsgID.GetDutyExecutorID())
		if !exists {
			return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
		}
		if n.cfg.NetworkConfig.NetworkTopologyFork() {
			topics = val.CommitteeTopicID()
		} else {
			topics = val.CommitteeTopicIDAlan()
		}
	}

	for _, topic := range topics {
		if err := n.topicsCtrl.Broadcast(topic, encodedMsg, n.cfg.RequestTimeout); err != nil {
			n.logger.Debug("could not broadcast msg", fields.Topic(topic), zap.Error(err))
			return fmt.Errorf("could not broadcast msg: %w", err)
		}
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
			if n.currentSubnets.IsSet(uint64(subnet)) {
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
		err := n.topicsCtrl.Subscribe(commons.SubnetTopicID(uint64(subnet)))
		if err != nil {
			return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
		}
	}

	for _, subnet := range randomSubnets {
		n.persistentSubnets.Set(uint64(subnet))
	}

	return nil
}

// SubscribedSubnets returns the subnets the node is subscribed to, consisting of the fixed subnets and the active committees/validators.
func (n *p2pNetwork) SubscribedSubnets() commons.Subnets {
	// Compute the new subnets according to the active committees/validators.
	updatedSubnets := n.persistentSubnets

	n.activeCommittees.Range(func(encodedCommittee string, statusAndSubnet validatorStatusAndSubnet) bool {
		updatedSubnets.Set(statusAndSubnet.subnet)
		// We use both pre-fork and post-fork subnets before fork to make sure we have everything ready when fork happens.
		// Afterwards, we just need the post-fork algorithm.
		if !n.cfg.NetworkConfig.NetworkTopologyFork() {
			updatedSubnets.Set(statusAndSubnet.subnetAlan)
		}
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

	if err := n.subscribeCommittee(share); err != nil {
		return fmt.Errorf("could not subscribe to committee (post-fork): %w", err)
	}

	return nil
}

func (n *p2pNetwork) subscribeCommittee(share *ssvtypes.SSVShare) error {
	cid := share.CommitteeID()
	committee := share.OperatorIDs()

	n.logger.Debug("subscribing to committee", fields.CommitteeID(cid), fields.OperatorIDs(committee))

	statusToSet := validatorStatusAndSubnet{
		status:     validatorStatusSubscribing,
		subnet:     share.CommitteeSubnet(),
		subnetAlan: share.CommitteeSubnetAlan(),
	}
	currentStatus, found := n.activeCommittees.GetOrSet(string(cid[:]), statusToSet)
	if found && currentStatus.status != validatorStatusInactive {
		return nil
	}

	topicSet := make(map[string]struct{})
	for _, topic := range share.CommitteeTopicID() {
		topicSet[topic] = struct{}{}
	}

	// We use both pre-fork and post-fork subnets before fork to make sure we have everything ready when fork happens.
	// Afterwards, we just need the post-fork algorithm.
	if !n.cfg.NetworkConfig.NetworkTopologyFork() {
		for _, topic := range share.CommitteeTopicIDAlan() {
			topicSet[topic] = struct{}{}
		}
	}

	for topic := range topicSet {
		if err := n.topicsCtrl.Subscribe(topic); err != nil {
			return fmt.Errorf("could not subscribe to topic %s: %w", topic, err)
		}
	}

	statusToSet.status = validatorStatusSubscribed
	n.activeCommittees.Set(string(cid[:]), statusToSet)

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

	topicSet := make(map[string]struct{})
	topics := share.CommitteeTopicID()
	for _, topic := range topics {
		topicSet[topic] = struct{}{}
	}

	if !n.cfg.NetworkConfig.NetworkTopologyFork() {
		topics = share.CommitteeTopicIDAlan()
		for _, topic := range topics {
			topicSet[topic] = struct{}{}
		}
	}

	for topic := range topicSet {
		if err := n.topicsCtrl.Unsubscribe(topic, false); err != nil {
			return err
		}
	}

	cid := share.CommitteeID()
	n.activeCommittees.Delete(string(cid[:]))
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

// subscribeToFixedSubnets subscribes to all the node's subnets
func (n *p2pNetwork) subscribeToFixedSubnets() error {
	if !n.persistentSubnets.HasActive() {
		return nil
	}

	n.logger.Debug("subscribing to fixed subnets", fields.Subnets(n.persistentSubnets))

	subnetList := n.persistentSubnets.SubnetList()
	for _, subnet := range subnetList {
		if err := n.topicsCtrl.Subscribe(strconv.FormatUint(subnet, 10)); err != nil {
			n.logger.Warn("could not subscribe to subnet",
				zap.Uint64("subnet", subnet), zap.Error(err))
			// TODO: handle error
		}
	}
	return nil
}

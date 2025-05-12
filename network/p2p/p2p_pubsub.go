package p2pv1

import (
	"context"
	"encoding/hex"
	"fmt"
	"math/rand"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/network"
	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/network/discovery"
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
		topics = commons.CommitteeTopicID(spectypes.CommitteeID(msg.SSVMessage.MsgID.GetDutyExecutorID()[16:]))
	} else {
		val, exists := n.nodeStorage.ValidatorStore().Validator(msg.SSVMessage.MsgID.GetDutyExecutorID())
		if !exists {
			return fmt.Errorf("could not find share for validator %s", hex.EncodeToString(msg.SSVMessage.MsgID.GetDutyExecutorID()))
		}
		topics = commons.CommitteeTopicID(val.CommitteeID())
	}

	for _, topic := range topics {
		if err := n.topicsCtrl.Broadcast(topic, encodedMsg, n.cfg.RequestTimeout); err != nil {
			n.logger.Debug("could not broadcast msg", fields.Topic(topic), zap.Error(err))
			return fmt.Errorf("could not broadcast msg: %w", err)
		}
	}
	return nil
}

func (n *p2pNetwork) SubscribeAll(logger *zap.Logger) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	n.fixedSubnets, _ = commons.FromString(commons.AllSubnets)
	for subnet := uint64(0); subnet < commons.SubnetsCount; subnet++ {
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
			if n.activeSubnets[subnet] == 1 {
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
		// #nosec G115
		err := n.topicsCtrl.Subscribe(logger, commons.SubnetTopicID(uint64(subnet))) // Perm slice is [0, n)
		if err != nil {
			return fmt.Errorf("could not subscribe to subnet %d: %w", subnet, err)
		}
	}

	// Update the subnets slice.
	subnets := make([]byte, commons.SubnetsCount)
	copy(subnets, n.fixedSubnets)
	for _, subnet := range randomSubnets {
		subnets[subnet] = byte(1)
	}
	n.fixedSubnets = subnets

	return nil
}

// SubscribedSubnets returns the subnets the node is subscribed to, consisting of the fixed subnets and the active committees/validators.
func (n *p2pNetwork) SubscribedSubnets() []byte {
	// Compute the new subnets according to the active committees/validators.
	updatedSubnets := make([]byte, commons.SubnetsCount)
	copy(updatedSubnets, n.fixedSubnets)

	n.activeCommittees.Range(func(cid string, status validatorStatus) bool {
		subnet := commons.CommitteeSubnet(spectypes.CommitteeID([]byte(cid)))
		updatedSubnets[subnet] = byte(1)
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

// subscribeCommittee handles the subscription logic for committee subnets
func (n *p2pNetwork) subscribeCommittee(cid spectypes.CommitteeID) error {
	n.logger.Debug("subscribing to committee", fields.CommitteeID(cid))
	status, found := n.activeCommittees.GetOrSet(string(cid[:]), validatorStatusSubscribing)
	if found && status != validatorStatusInactive {
		return nil
	}

	for _, topic := range commons.CommitteeTopicID(cid) {
		if err := n.topicsCtrl.Subscribe(n.logger, topic); err != nil {
			return fmt.Errorf("could not subscribe to topic %s: %w", topic, err)
		}
	}
	n.activeCommittees.Set(string(cid[:]), validatorStatusSubscribed)
	return nil
}

func (n *p2pNetwork) unsubscribeSubnet(logger *zap.Logger, subnet uint64) error {
	if !n.isReady() {
		return p2pprotocol.ErrNetworkIsNotReady
	}
	if subnet >= commons.SubnetsCount {
		return fmt.Errorf("invalid subnet %d", subnet)
	}
	if err := n.topicsCtrl.Unsubscribe(logger, commons.SubnetTopicID(subnet), false); err != nil {
		return fmt.Errorf("could not unsubscribe from subnet %d: %w", subnet, err)
	}
	return nil
}

// Unsubscribe unsubscribes from the validator subnet
func (n *p2pNetwork) Unsubscribe(logger *zap.Logger, pk spectypes.ValidatorPK) error {
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
		if err := n.topicsCtrl.Unsubscribe(logger, topic, false); err != nil {
			return err
		}
	}
	n.activeCommittees.Delete(string(cmtid[:]))
	return nil
}

// handlePubsubMessages reads messages from the given channel and calls the router, note that this function blocks.
func (n *p2pNetwork) handlePubsubMessages(logger *zap.Logger) func(ctx context.Context, topic string, msg *pubsub.Message) error {
	return func(ctx context.Context, topic string, msg *pubsub.Message) error {
		if n.msgRouter == nil {
			logger.Debug("msg router is not configured")
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
func (n *p2pNetwork) subscribeToFixedSubnets(logger *zap.Logger) error {
	if !discovery.HasActiveSubnets(n.fixedSubnets) {
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

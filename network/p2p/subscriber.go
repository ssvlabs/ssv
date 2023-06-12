package p2pv1

import (
	"encoding/hex"
	"errors"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/topics"
	"go.uber.org/zap"
)

// subscriber keeps track of active validators and their corresponding subnets
// and automatically subscribes/unsubscribes from subnets accordingly.
type subscriber struct {
	topicsCtrl topics.Controller
	fork       forks.Fork

	validators                map[string]struct{}
	subscriptions             []int
	addSubnets, removeSubnets []int
	changed                   chan struct{}
	mu                        sync.Mutex
}

// newSubscriber creates a new subscriber.
//
// `constantSubnets` is a list of subnets that should always remain subscribed to,
// regardless of they have active validators or not.
func newSubscriber(topicsCtrl topics.Controller, fork forks.Fork, constantSubnets []byte) *subscriber {
	s := &subscriber{
		topicsCtrl:    topicsCtrl,
		fork:          fork,
		validators:    make(map[string]struct{}),
		subscriptions: make([]int, fork.Subnets()),
		changed:       make(chan struct{}, 1),
	}
	for subnet, active := range constantSubnets {
		if active == 1 {
			s.subscriptions[subnet] = 1
			s.addSubnets = append(s.addSubnets, subnet)
		}
	}
	return s
}

func (s *subscriber) AddValidator(pk spectypes.ValidatorPK) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pkHex := hex.EncodeToString(pk)
	if _, ok := s.validators[pkHex]; ok {
		// Already exists.
		return
	}
	s.validators[pkHex] = struct{}{}

	// Increment subscriptions.
	subnet := s.fork.ValidatorSubnet(pkHex)
	s.subscriptions[subnet]++
	if s.subscriptions[subnet] == 1 {
		s.addSubnets = append(s.addSubnets, subnet)
	}
}

func (s *subscriber) RemoveValidator(pk spectypes.ValidatorPK) {
	s.mu.Lock()
	defer s.mu.Unlock()

	pkHex := hex.EncodeToString(pk)
	if _, ok := s.validators[pkHex]; !ok {
		// Doesn't exist.
		return
	}
	delete(s.validators, pkHex)

	// Decrement subscriptions.
	subnet := s.fork.ValidatorSubnet(pkHex)
	s.subscriptions[subnet]--
	if s.subscriptions[subnet] == 0 {
		s.removeSubnets = append(s.removeSubnets, subnet)
	}
}

func (s *subscriber) Subnets() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnets := make([]byte, s.fork.Subnets())
	for subnet, validators := range s.subscriptions {
		if validators > 0 {
			subnets[subnet] = 1
		}
	}
	return subnets
}

// Update subscribes/unsubscribes from subnets based on the current state of
// the validator set.
//
// Update always keeps the subnets specified in `constantSubnets` active.
func (s *subscriber) Update(logger *zap.Logger) (addedSubnets []int, removedSubnets []int, err error) {
	addedSubnets, removedSubnets = s.changes()

	// Subscribe to new subnets.
	for subnet := range addedSubnets {
		subscribeErr := s.topicsCtrl.Subscribe(logger, s.fork.SubnetTopicID(subnet))
		err = errors.Join(err, subscribeErr)
	}

	// Unsubscribe from inactive subnets.
	for _, subnet := range removedSubnets {
		unsubscribeErr := s.topicsCtrl.Unsubscribe(logger, s.fork.SubnetTopicID(subnet), false)
		err = errors.Join(err, unsubscribeErr)
	}
	return
}

// changes returns the list of subnets that should be subscribed to and
// unsubscribed from.
func (s *subscriber) changes() (addSubnets []int, removeSubnets []int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	addSubnets, removeSubnets = s.addSubnets, s.removeSubnets
	s.addSubnets, s.removeSubnets = nil, nil
	return
}

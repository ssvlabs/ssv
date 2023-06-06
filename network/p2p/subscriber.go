package p2pv1

import (
	"encoding/hex"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/topics"
	"go.uber.org/multierr"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

// subscriber keeps track of active validators and their corresponding subnets
// and automatically subscribes/unsubscribes from subnets accordingly.
type subscriber struct {
	topicsCtrl      topics.Controller
	fork            forks.Fork
	constantSubnets []byte

	validators       map[string]struct{}
	subscriptions    map[int]int
	newSubscriptions map[int]struct{}
	mu               sync.Mutex
}

// newSubscriber creates a new subscriber.
//
// `constantSubnets` is a list of subnets that should always remain subscribed to,
// regardless of they have active validators or not.
func newSubscriber(topicsCtrl topics.Controller, fork forks.Fork, constantSubnets []byte) *subscriber {
	return &subscriber{
		topicsCtrl:       topicsCtrl,
		fork:             fork,
		constantSubnets:  constantSubnets,
		validators:       make(map[string]struct{}),
		subscriptions:    make(map[int]int),
		newSubscriptions: make(map[int]struct{}),
	}
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
	if _, ok := s.subscriptions[subnet]; ok {
		s.subscriptions[subnet]++
	} else {
		s.subscriptions[subnet] = 1
		s.newSubscriptions[subnet] = struct{}{}
	}
}

func (s *subscriber) RemoveValidator(logger *zap.Logger, pk spectypes.ValidatorPK) {
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
	if _, ok := s.subscriptions[subnet]; ok {
		s.subscriptions[subnet]--
	}
}

func (s *subscriber) Subnets() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnets := make([]byte, s.fork.Subnets())
	for subnet := range s.subscriptions {
		subnets[subnet] = 1
	}
	return subnets
}

// Update subscribes/unsubscribes from subnets based on the current state of
// the validator set.
//
// Update always keeps the subnets specified in `constantSubnets` active.
func (s *subscriber) Update(logger *zap.Logger) (newSubnets []int, inactiveSubnets []int, err error) {
	func() {
		s.mu.Lock()
		defer s.mu.Unlock()

		// Get new subnets.
		newSubnets = maps.Keys(s.newSubscriptions)
		s.newSubscriptions = make(map[int]struct{})

		// Compute inactive subnets.
		inactiveSubnets = make([]int, 0, len(s.subscriptions))
		for subnet, validators := range s.subscriptions {
			if validators == 0 && s.constantSubnets[subnet] != 1 {
				inactiveSubnets = append(inactiveSubnets, subnet)
				delete(s.subscriptions, subnet)
			}
		}
	}()

	// Subscribe to new subnets.
	for subnet := range s.newSubscriptions {
		subscribeErr := s.topicsCtrl.Subscribe(logger, s.fork.SubnetTopicID(subnet))
		err = multierr.Append(err, subscribeErr)
	}

	// Unsubscribe from inactive subnets.
	for _, subnet := range inactiveSubnets {
		unsubscribeErr := s.topicsCtrl.Unsubscribe(logger, s.fork.SubnetTopicID(subnet), false)
		err = multierr.Append(err, unsubscribeErr)
	}

	return
}

package p2pv1

import (
	"encoding/hex"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/bloxapp/ssv/network/topics"
	"go.uber.org/zap"
	"golang.org/x/exp/maps"
)

type subscriber struct {
	topicsCtrl      topics.Controller
	fork            forks.Fork
	constantSubnets []byte

	validators       map[string]struct{}
	subscriptions    map[int]int
	newSubscriptions map[int]struct{}
	mu               sync.Mutex
}

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

func (s *subscriber) addValidator(pk spectypes.ValidatorPK) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pkHex := hex.EncodeToString(pk)
	if _, ok := s.validators[pkHex]; ok {
		// Already exists.
		return nil
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

	return nil
}

func (s *subscriber) removeValidator(logger *zap.Logger, pk spectypes.ValidatorPK) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	pkHex := hex.EncodeToString(pk)
	if _, ok := s.validators[pkHex]; !ok {
		// Doesn't exist.
		return nil
	}
	delete(s.validators, pkHex)

	// Decrement subscriptions.
	subnet := s.fork.ValidatorSubnet(pkHex)
	if _, ok := s.subscriptions[subnet]; ok {
		s.subscriptions[subnet]--
	}

	return nil
}

func (s *subscriber) subnets() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()

	subnets := make([]byte, s.fork.Subnets())
	for subnet := range s.subscriptions {
		subnets[subnet] = 1
	}
	return subnets
}

func (s *subscriber) update(logger *zap.Logger) (newSubnets []int, err error) {
	s.mu.Lock()
	newSubnets = maps.Keys(s.newSubscriptions)
	inactiveSubnets := make([]int, 0, len(s.subscriptions))
	for subnet, validators := range s.subscriptions {
		if validators == 0 && s.constantSubnets[subnet] != 1 {
			inactiveSubnets = append(inactiveSubnets, subnet)
			delete(s.subscriptions, subnet)
		}
	}
	s.mu.Unlock()

	// Subscribe to new subnets.
	for subnet := range s.newSubscriptions {
		if err = s.topicsCtrl.Subscribe(logger, s.fork.SubnetTopicID(subnet)); err != nil {
			return
		}
	}

	// Unsubscribe from inactive subnets.
	for _, subnet := range inactiveSubnets {
		if err = s.topicsCtrl.Unsubscribe(logger, s.fork.SubnetTopicID(subnet), false); err != nil {
			return
		}
	}

	s.newSubscriptions = make(map[int]struct{})

	return
}

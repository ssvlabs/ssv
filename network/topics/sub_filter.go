package topics

import (
	"github.com/bloxapp/ssv/logging/fields"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/cornelk/hashmap"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.uber.org/zap"
)

// SubFilter is a wrapper on top of pubsub.SubscriptionFilter,
type SubFilter interface {
	// SubscriptionFilter allows controlling what topics the node will subscribe to
	// otherwise it might subscribe to irrelevant topics that were suggested by other peers
	pubsub.SubscriptionFilter
	// Whitelist
}

type subFilter struct {
	fork      forks.Fork
	whitelist *dynamicWhitelist
	subsLimit int
}

func newSubFilter(logger *zap.Logger, fork forks.Fork, subsLimit int) SubFilter {
	return &subFilter{
		fork:      fork,
		whitelist: newWhitelist(),
		subsLimit: subsLimit,
	}
}

// CanSubscribe returns true if the topic is of interest and we can subscribe to it
func (sf *subFilter) CanSubscribe(topic string) bool {
	if sf.fork.GetTopicBaseName(topic) == topic {
		// not of the same fork
		return false
	}
	return sf.Whitelisted(topic)
}

// FilterIncomingSubscriptions is invoked for all RPCs containing subscription notifications.
// It should filter only the subscriptions of interest and my return an error if (for instance)
// there are too many subscriptions.
func (sf *subFilter) FilterIncomingSubscriptions(pi peer.ID, subs []*ps_pb.RPC_SubOpts) (res []*ps_pb.RPC_SubOpts, err error) {
	defer func() {
		zap.L().Debug(
			"FilterIncomingSubscriptions",
			fields.PeerID(pi),
			zap.Int("before_subscriptions_len", len(subs)),
			zap.Int("after_subscriptions_len", len(res)),
			zap.Any("before_subscriptions", subs),
			zap.Any("after_subscriptions", res),
			zap.Error(err),
		)
	}()

	if len(subs) > subscriptionRequestLimit {
		err = pubsub.ErrTooManySubscriptions
		return
	}

	res = pubsub.FilterSubscriptions(subs, sf.CanSubscribe)
	return res, nil
}

// Register adds the given topic to the whitelist
func (sf *subFilter) Register(topic string) {
	sf.whitelist.Register(topic)
}

// Deregister removes the given topic from the whitelist
func (sf *subFilter) Deregister(topic string) {
	sf.whitelist.Deregister(topic)
}

// Whitelisted implements Whitelist
func (sf *subFilter) Whitelisted(topic string) bool {
	return sf.whitelist.Whitelisted(topic)
}

// Whitelist is an interface to maintain dynamic whitelists
type Whitelist interface {
	// Register adds the given name to the whitelist
	Register(name string)
	// Deregister removes the given name from the whitelist
	Deregister(name string)
	// Whitelisted checks if the given name was whitelisted
	Whitelisted(name string) bool
}

// dynamicWhitelist helps to maintain a filter based on some whitelist
type dynamicWhitelist struct {
	whitelist *hashmap.Map[string, struct{}]
}

// newWhitelist creates a new whitelist
func newWhitelist() *dynamicWhitelist {
	return &dynamicWhitelist{
		whitelist: hashmap.New[string, struct{}](),
	}
}

// Register adds the given topic to the whitelist
func (wl *dynamicWhitelist) Register(name string) {
	wl.whitelist.Set(name, struct{}{})
}

// Deregister removes the given topic from the whitelist
func (wl *dynamicWhitelist) Deregister(name string) {
	wl.whitelist.Del(name)
}

// Whitelisted checks if the given name was whitelisted
func (wl *dynamicWhitelist) Whitelisted(name string) bool {
	_, ok := wl.whitelist.Get(name)
	return ok
}

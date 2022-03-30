package topics

import (
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"sync"
)

// SubFilter is a wrapper on top of pubsub.SubscriptionFilter,
// it has a register function that enables to add topics to the whitelist
type SubFilter interface {
	// SubscriptionFilter allows controlling what topics the node will subscribe to
	// otherwise it might subscribe to irrelevant topics that were suggested by other peers
	pubsub.SubscriptionFilter
	// Register adds the given topic to the whitelist
	Register(topic string)
	// Deregister removes the given topic from the whitelist
	Deregister(topic string)
}

type subFilter struct {
	whitelist *sync.Map
	subsLimit int
}

func newSubFilter(subsLimit int) SubFilter {
	return &subFilter{
		whitelist: &sync.Map{},
		subsLimit: subsLimit,
	}
}

// Register adds the given topic to the whitelist
func (sf *subFilter) Register(topic string) {
	sf.whitelist.Store(topic, true)
}

// Deregister removes the given topic from the whitelist
func (sf *subFilter) Deregister(topic string) {
	sf.whitelist.Delete(topic)
}

// CanSubscribe returns true if the topic is of interest and we can subscribe to it
func (sf *subFilter) CanSubscribe(topic string) bool {
	if _, ok := sf.whitelist.Load(topic); ok {
		return true
	}
	return false
}

// FilterIncomingSubscriptions is invoked for all RPCs containing subscription notifications.
// It should filter only the subscriptions of interest and my return an error if (for instance)
// there are too many subscriptions.
func (sf *subFilter) FilterIncomingSubscriptions(pi peer.ID, subs []*ps_pb.RPC_SubOpts) ([]*ps_pb.RPC_SubOpts, error) {
	if len(subs) > subscriptionRequestLimit {
		return nil, pubsub.ErrTooManySubscriptions
	}

	return pubsub.FilterSubscriptions(subs, sf.CanSubscribe), nil
}

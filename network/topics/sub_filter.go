package topics

import (
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"go.uber.org/zap"
)

// SubFilter is a wrapper on top of pubsub.SubscriptionFilter,
// TODO: enable black/whitelisting
type SubFilter interface {
	// SubscriptionFilter allows controlling what topics the node will subscribe to
	// otherwise it might subscribe to irrelevant topics that were suggested by other peers
	pubsub.SubscriptionFilter
	//Whitelist
}

type subFilter struct {
	logger    *zap.Logger
	fork      forks.Fork
	subsLimit int
}

func newSubFilter(logger *zap.Logger, fork forks.Fork, subsLimit int) SubFilter {
	return &subFilter{
		logger: logger,
		fork:   fork,
		//whitelist: NewWhitelist(),
		subsLimit: subsLimit,
	}
}

// CanSubscribe returns true if the topic is of interest and we can subscribe to it
func (sf *subFilter) CanSubscribe(topic string) bool {
	//if sf.Whitelisted(topic) {
	//	return true
	//}
	// checks that we know this topic
	return sf.fork.GetTopicBaseName(topic) != topic
}

// FilterIncomingSubscriptions is invoked for all RPCs containing subscription notifications.
// It should filter only the subscriptions of interest and my return an error if (for instance)
// there are too many subscriptions.
func (sf *subFilter) FilterIncomingSubscriptions(pi peer.ID, subs []*ps_pb.RPC_SubOpts) ([]*ps_pb.RPC_SubOpts, error) {
	if len(subs) > subscriptionRequestLimit {
		return nil, pubsub.ErrTooManySubscriptions
	}

	res := pubsub.FilterSubscriptions(subs, sf.CanSubscribe)

	if len(res) == 0 {
		sf.logger.Debug("no relevant subscriptions", zap.Any("res", res))
	}

	return res, nil
}

//
//// Register adds the given topic to the whitelist
//func (sf *subFilter) Register(topic string) {
//	sf.whitelist.Register(topic)
//}
//
//// Deregister removes the given topic from the whitelist
//func (sf *subFilter) Deregister(topic string) {
//	sf.whitelist.Deregister(topic)
//}
//
//// Whitelisted implements Whitelist
//func (sf *subFilter) Whitelisted(topic string) bool {
//	return sf.whitelist.Whitelisted(topic)
//}

//// Whitelist is an interface to maintain dynamic whitelists
//type Whitelist interface {
//	// Register adds the given name to the whitelist
//	Register(name string)
//	// Deregister removes the given name from the whitelist
//	Deregister(name string)
//	// Whitelisted checks if the given name was whitelisted
//	Whitelisted(name string) bool
//}
//
//// dynamicWhitelist helps to maintain a filter based on some whitelist
//type dynamicWhitelist struct {
//	whitelist *sync.Map
//}
//
//// NewWhitelist creates a new whitelist
//func NewWhitelist() *dynamicWhitelist {
//	return &dynamicWhitelist{
//		whitelist: &sync.Map{},
//	}
//}
//
//// Register adds the given topic to the whitelist
//func (wl *dynamicWhitelist) Register(name string) {
//	wl.whitelist.Store(name, true)
//}
//
//// Deregister removes the given topic from the whitelist
//func (wl *dynamicWhitelist) Deregister(name string) {
//	wl.whitelist.Delete(name)
//}
//
//// Whitelisted checks if the given name was whitelisted
//func (wl *dynamicWhitelist) Whitelisted(name string) bool {
//	_, ok := wl.whitelist.Load(name)
//	if ok {
//		return true
//	}
//	return false
//}

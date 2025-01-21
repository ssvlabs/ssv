package topics

import (
	"sync"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
)

// Increased from the default (32) to reduce message drops caused by slow subscribers.
const subscriptionBufferSize = 256

type onTopicJoined func(ps *pubsub.PubSub, topic *pubsub.Topic)

type topicsContainer struct {
	ps *pubsub.PubSub

	onTopicJoined onTopicJoined

	subLock *sync.RWMutex
	lock    *sync.RWMutex
	topics  map[string]*pubsub.Topic
	subs    map[string]*pubsub.Subscription
}

func newTopicsContainer(ps *pubsub.PubSub, onTopicJoined onTopicJoined) *topicsContainer {
	return &topicsContainer{
		ps:            ps,
		onTopicJoined: onTopicJoined,
		subLock:       &sync.RWMutex{},
		lock:          &sync.RWMutex{},
		topics:        make(map[string]*pubsub.Topic),
		subs:          make(map[string]*pubsub.Subscription),
	}
}

func (tc *topicsContainer) Get(name string) *pubsub.Topic {
	topic, _ := tc.Join(name)
	return topic
}

func (tc *topicsContainer) Leave(name string) error {
	tc.lock.Lock()
	topic, ok := tc.topics[name]
	if ok {
		delete(tc.topics, name)
	}
	tc.lock.Unlock()
	if topic == nil {
		return nil
	}

	return topic.Close()
}

func (tc *topicsContainer) Join(name string, opts ...pubsub.TopicOpt) (*pubsub.Topic, error) {
	tc.lock.Lock()
	defer tc.lock.Unlock()

	topic, ok := tc.topics[name]
	if !ok {
		t, err := tc.ps.Join(name, opts...)
		if err != nil {
			return nil, err
		}
		tc.onTopicJoined(tc.ps, t)
		topic = t
		tc.topics[name] = t
	}

	return topic, nil
}

func (tc *topicsContainer) Unsubscribe(name string) bool {
	tc.subLock.Lock()
	defer tc.subLock.Unlock()

	sub, ok := tc.subs[name]
	if !ok {
		return false
	}
	delete(tc.subs, name)
	sub.Cancel()
	return true
}

func (tc *topicsContainer) Subscribe(name string, opts ...pubsub.SubOpt) (*pubsub.Subscription, error) {
	tc.subLock.Lock()
	defer tc.subLock.Unlock()

	_, ok := tc.subs[name]
	if ok {
		return nil, nil
	}

	topic, err := tc.Join(name)
	if err != nil {
		return nil, err
	}

	opts = append(opts, pubsub.WithBufferSize(subscriptionBufferSize))

	s, err := topic.Subscribe(opts...)
	if err != nil {
		return nil, err
	}
	tc.subs[name] = s

	return s, nil
}

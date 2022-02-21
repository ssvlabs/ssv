package pubsub

import (
	"context"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
	"sync/atomic"
)

const (
	topicStateClosed      = uint32(0)
	topicStateJoining     = uint32(1)
	topicStateJoined      = uint32(2)
	topicStateSubscribing = uint32(3)
	topicStateSubscribed  = uint32(4)
)

// psTopic is an internal wrapper for pubsub.Topic
// it has a reference for the current active subscription,
// and in addition a state to manage concurrent access to the topic
type psTopic struct {
	logger *zap.Logger

	state     uint32
	topic     *pubsub.Topic
	sub       *pubsub.Subscription
	cancelSub context.CancelFunc
}

// newTopic creates a new instance of psTopic
func newTopic(logger *zap.Logger) *psTopic {
	return &psTopic{
		logger: logger,
		state:  topicStateClosed,
	}
}

// getState returns current state of the topic
func (ta *psTopic) getState() uint32 {
	return atomic.LoadUint32(&ta.state)
}

// setState updates state and returns previous state
func (ta *psTopic) setState(newState uint32) uint32 {
	return atomic.SwapUint32(&ta.state, newState)
}

// join updates state to joined
func (ta *psTopic) join(topic *pubsub.Topic) {
	if atomic.CompareAndSwapUint32(&ta.state, topicStateJoining, topicStateJoined) {
		ta.topic = topic
	}
}

// joining set state to topicStateJoining
func (ta *psTopic) joining() {
	ta.topic = nil
	ta.sub = nil
	ta.cancelSub = nil
	ta.setState(topicStateJoining)
}

// subscribing set state to topicStateSubscribing
func (ta *psTopic) subscribing() {
	ta.sub = nil
	ta.cancelSub = nil
	ta.setState(topicStateSubscribing)
}

// subscribe updates state to subscribed
func (ta *psTopic) subscribe(sub *pubsub.Subscription, cancel context.CancelFunc) {
	if atomic.CompareAndSwapUint32(&ta.state, topicStateSubscribing, topicStateSubscribed) {
		ta.sub = sub
		ta.cancelSub = cancel
	}
}

// close set state to topicStateClosed
func (ta *psTopic) close() {
	if ta.setState(topicStateClosed) != topicStateClosed {
		if ta.cancelSub != nil {
			ta.cancelSub()
		}
		if ta.sub != nil {
			ta.sub.Cancel()
		}
		if ta.topic != nil {
			name := ta.topic.String()
			if err := ta.topic.Close(); err != nil {
				ta.logger.Warn("could not close topic", zap.String("name", name), zap.Error(err))
			}
			ta.topic = nil
		}
	}
}

// canPublish returns whether we can publish on the topic
func (ta *psTopic) canPublish() bool {
	return ta.getState() >= topicStateJoined
}

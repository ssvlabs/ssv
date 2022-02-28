package topics

import (
	"context"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"go.uber.org/zap"
	"sync/atomic"
)

const (
	topicStateError       = int32(-1)
	topicStateClosed      = int32(0)
	topicStateJoining     = int32(1)
	topicStateJoined      = int32(2)
	topicStateSubscribing = int32(3)
	topicStateSubscribed  = int32(4)
)

// topicState is an internal wrapper on top of pubsub.Topic
// it has a reference for the current active subscription,
// and in addition a state to manage concurrent access to the topic
type topicState struct {
	logger *zap.Logger

	state     int32
	topic     *pubsub.Topic
	sub       *pubsub.Subscription
	cancelSub context.CancelFunc
}

// newTopicWrapper creates a new instance of topicState
func newTopicWrapper(logger *zap.Logger) *topicState {
	return &topicState{
		logger: logger,
		state:  topicStateClosed,
	}
}

// getState returns current state of the topic
func (ts *topicState) getState() int32 {
	return atomic.LoadInt32(&ts.state)
}

// setState updates state and returns previous state
func (ts *topicState) setState(newState int32) int32 {
	return atomic.SwapInt32(&ts.state, newState)
}

// setStateCond updates state if the given previous state was set
func (ts *topicState) setStateCond(prevState int32, newState int32) bool {
	return atomic.CompareAndSwapInt32(&ts.state, prevState, newState)
}

// join updates state to joined
func (ts *topicState) join(topic *pubsub.Topic) {
	if ts.setStateCond(topicStateJoining, topicStateJoined) {
		ts.topic = topic
	}
}

// joining set state to topicStateJoining
func (ts *topicState) joining() {
	ts.topic = nil
	ts.sub = nil
	ts.cancelSub = nil
	ts.setState(topicStateJoining)
}

// subscribing set state to topicStateSubscribing
func (ts *topicState) subscribing() {
	ts.sub = nil
	ts.cancelSub = nil
	ts.setState(topicStateSubscribing)
}

// subscribe updates state to subscribed
func (ts *topicState) subscribe(sub *pubsub.Subscription, cancel context.CancelFunc) {
	if ts.setStateCond(topicStateSubscribing, topicStateSubscribed) {
		ts.sub = sub
		ts.cancelSub = cancel
	}
}

// close set state to topicStateClosed
func (ts *topicState) close() {
	if ts.setState(topicStateClosed) != topicStateClosed {
		if ts.cancelSub != nil {
			ts.cancelSub()
		}
		if ts.sub != nil {
			ts.sub.Cancel()
		}
		if ts.topic != nil {
			name := ts.topic.String()
			if err := ts.topic.Close(); err != nil {
				ts.logger.Warn("could not close topic", zap.String("name", name), zap.Error(err))
				// TODO: handle or return the error
				// this might be "cannot close topic: outstanding event handlers or subscriptions"
				// in case the topic was oversubscribed, marking as error
				ts.setState(topicStateError)
				return
			}
			ts.topic = nil
		}
	}
}

// canPublish returns whether we can publish on the topic
func (ts *topicState) canPublish() bool {
	return ts.getState() >= topicStateJoined
}

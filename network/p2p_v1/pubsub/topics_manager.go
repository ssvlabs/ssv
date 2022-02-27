package pubsub

import (
	"context"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	bufSize = 32
)

var (
	// ErrInProcess happens when you try to subscribe multiple times concurrently
	// exported so the caller can decide how to act upon it
	ErrInProcess = errors.New("in process")
	// ErrCouldNotJoin is exported so the caller can track reasons of failures when calling Subscribe
	ErrCouldNotJoin = errors.New("could not join topic")
	// ErrCouldNotSubscribe is exported so the caller can track reasons of failures when calling Subscribe
	ErrCouldNotSubscribe = errors.New("could not subscribe topic")
	// ErrTopicNotReady happens when trying to access a topic which is not ready yet
	ErrTopicNotReady = errors.New("topic is not ready")

	errTopicAlreadyExists = errors.New("topic already exists")
)

// TopicsManager is an interface for managing pubsub topics
type TopicsManager interface {
	// Subscribe subscribes to the given topic
	Subscribe(topicName string) (<-chan *pubsub.Message, error)
	// Unsubscribe unsubscribes from the given topic
	Unsubscribe(topicName string) error
	// Peers returns the peers subscribed to the given topic
	Peers(topicName string) ([]peer.ID, error)
	// Topics lists all the available topics
	Topics() []string
	// Broadcast publishes the message on the given topic
	Broadcast(topicName string, data []byte, timeout time.Duration) error
}

// topicsManager implements
type topicsManager struct {
	ctx    context.Context
	logger *zap.Logger
	ps     *pubsub.PubSub
	// scoreParams is a function that helps to set scoring params on topics
	scoreParams func(string) *pubsub.TopicScoreParams
	// topics holds all the available topics
	topics *sync.Map
}

// NewTopicManager creates an instance of TopicsManager
func NewTopicManager(ctx context.Context, logger *zap.Logger, pubSub *pubsub.PubSub, scoreParams func(string) *pubsub.TopicScoreParams) TopicsManager {
	return &topicsManager{
		ctx:         ctx,
		logger:      logger,
		ps:          pubSub,
		scoreParams: scoreParams,
		topics:      &sync.Map{},
	}
}

// Peers returns the peers subscribed to the given topic
func (tm *topicsManager) Peers(topicName string) ([]peer.ID, error) {
	topic := tm.getTopicState(topicName)
	if topic == nil {
		return nil, nil
	}
	if topic.getState() >= topicStateJoined {
		return topic.topic.ListPeers(), nil
	}
	return nil, ErrTopicNotReady
}

// Subscribe subscribes to the given topic
func (tm *topicsManager) Subscribe(topicName string) (<-chan *pubsub.Message, error) {
	return tm.subscribe(topicName)
}

// Topics lists all the available topics
func (tm *topicsManager) Topics() []string {
	return tm.ps.GetTopics()
}

// Unsubscribe unsubscribes from the given topic
func (tm *topicsManager) Unsubscribe(topicName string) error {
	topic := tm.getTopicState(topicName)
	if topic == nil {
		return nil
	}
	tm.topics.Delete(topicName)
	topic.close()
	return nil
}

// Broadcast publishes the message on the given topic
func (tm *topicsManager) Broadcast(topicName string, data []byte, timeout time.Duration) error {
	//topic := tm.fork.ValidatorTopicID(pk)
	topic, err := tm.joinTopic(topicName)
	if err != nil {
		return err
	}
	if !topic.canPublish() {
		return errors.New("can't publish message as topic is not ready")
	}
	//data, err := tm.fork.EncodeNetworkMsg(msg)
	//if err != nil {
	//	return errors.Wrap(err, "could not encode message")
	//}
	ctx, done := context.WithTimeout(tm.ctx, timeout)
	defer done()

	return topic.topic.Publish(ctx, data)
}

// getTopicState returns the topic wrapper if exist
func (tm *topicsManager) getTopicState(name string) *topicState {
	t, ok := tm.topics.Load(name)
	if !ok {
		return nil
	}
	return t.(*topicState)
}

// joinTopic joins the given topic and returns the wrapper
func (tm *topicsManager) joinTopic(name string) (*topicState, error) {
	state := tm.getTopicState(name)
	if state == nil {
		state = newTopicWrapper(tm.logger)
		tm.topics.Store(name, state)
	}
	switch state.getState() {
	case topicStateJoining:
		return state, ErrInProcess
	case topicStateClosed:
		state.joining()
		t, err := tm.ps.Join(name)
		if err != nil {
			tm.logger.Warn("could not join topic", zap.String("name", name), zap.Error(err))
			state.close()
			return nil, ErrCouldNotJoin
		}
		if tm.scoreParams != nil {
			err = t.SetScoreParams(tm.scoreParams(name))
			if err != nil {
				state.close()
				return nil, errors.Wrap(err, "could not set score params")
			}
		}
		state.join(t)
		tm.topics.Store(name, state)
	default:
	}
	return state, nil
}

// subscribe to the given topic and returns a channel to read the messages from
func (tm *topicsManager) subscribe(name string) (<-chan *pubsub.Message, error) {
	state, err := tm.joinTopic(name)
	if err != nil {
		return nil, err
	}
	switch state.getState() {
	case topicStateSubscribing:
		return nil, ErrInProcess
	case topicStateJoined:
		state.subscribing()
		sub, err := state.topic.Subscribe()
		if err == pubsub.ErrTopicClosed {
			// rejoin a topic in case it was closed, and try to subscribe again
			state.close()
			state, err = tm.joinTopic(name)
			if err != nil {
				return nil, err
			}
			sub, err = state.topic.Subscribe()
			if err != nil {
				tm.logger.Warn("could not subscribe to topic", zap.String("topic", name), zap.Error(err))
			}
		}
		if sub == nil {
			state.close()
			return nil, ErrCouldNotSubscribe
		}
		in := tm.listen(state, sub)
		return in, nil
	default:
	}
	return nil, nil
}

// listen handles incoming messages from the topic
// it buffers results to make sure we keep up with incoming messages rate
// in case subscription returns error, the topic is closed
func (tm *topicsManager) listen(state *topicState, sub *pubsub.Subscription) chan *pubsub.Message {
	ctx, cancel := context.WithCancel(tm.ctx)
	in := make(chan *pubsub.Message, bufSize)
	go func() {
		state.subscribe(sub, cancel)
		tm.topics.Store(state.topic.String(), state)
		defer func() {
			state.close()
			close(in)
		}()
		logger := tm.logger.With(zap.String("topic", sub.Topic()))
		logger.Info("start listening to topic")
		for ctx.Err() == nil {
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					logger.Debug("stop listening to topic: context is done")
					return
				}
				logger.Warn("stop listening to topic: could not read message from subscription", zap.Error(err))
				return
			}
			if msg == nil {
				logger.Warn("got empty message from subscription")
				continue
			}
			in <- msg
		}
	}()
	return in
}

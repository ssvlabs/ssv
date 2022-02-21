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

// TopicManager is an interface for managing pubsub topics
type TopicManager interface {
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

// topicManager implements
type topicManager struct {
	ctx    context.Context
	logger *zap.Logger
	ps     *pubsub.PubSub
	// scoreParams is a function that helps to set scoring params on topics
	scoreParams func(string) *pubsub.TopicScoreParams
	// topics holds all the available topics
	topics *sync.Map
}

// NewTopicManager creates an instance of TopicManager
func NewTopicManager(ctx context.Context, logger *zap.Logger, pubSub *pubsub.PubSub, scoreParams func(string) *pubsub.TopicScoreParams) TopicManager {
	return &topicManager{
		ctx:         ctx,
		logger:      logger,
		ps:          pubSub,
		scoreParams: scoreParams,
		topics:      &sync.Map{},
	}
}

// Peers returns the peers subscribed to the given topic
func (tm *topicManager) Peers(topicName string) ([]peer.ID, error) {
	topic := tm.getTopic(topicName)
	if topic == nil {
		return nil, nil
	}
	if topic.getState() >= topicStateJoined {
		return topic.topic.ListPeers(), nil
	}
	return nil, ErrTopicNotReady
}

// Subscribe subscribes to the given topic
func (tm *topicManager) Subscribe(topicName string) (<-chan *pubsub.Message, error) {
	return tm.subscribe(topicName)
}

// Topics lists all the available topics
func (tm *topicManager) Topics() []string {
	return tm.ps.GetTopics()
}

// Unsubscribe unsubscribes from the given topic
func (tm *topicManager) Unsubscribe(topicName string) error {
	topic := tm.getTopic(topicName)
	if topic == nil {
		return nil
	}
	tm.topics.Delete(topicName)
	topic.close()
	return nil
}

// Broadcast publishes the message on the given topic
func (tm *topicManager) Broadcast(topicName string, data []byte, timeout time.Duration) error {
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

// getTopic returns the topic wrapper if exist
func (tm *topicManager) getTopic(name string) *psTopic {
	t, ok := tm.topics.Load(name)
	if !ok {
		return nil
	}
	return t.(*psTopic)
}

// joinTopic joins the given topic and returns the wrapper
func (tm *topicManager) joinTopic(name string) (*psTopic, error) {
	topic := tm.getTopic(name)
	if topic == nil {
		topic = newTopic(tm.logger)
		tm.topics.Store(name, topic)
	}
	switch topic.getState() {
	case topicStateJoining:
		return topic, ErrInProcess
	case topicStateClosed:
		topic.joining()
		t, err := tm.ps.Join(name)
		if err != nil {
			tm.logger.Warn("could not join topic", zap.String("name", name), zap.Error(err))
			topic.close()
			return nil, ErrCouldNotJoin
		}
		if tm.scoreParams != nil {
			err = t.SetScoreParams(tm.scoreParams(name))
			if err != nil {
				topic.close()
				return nil, errors.Wrap(err, "could not set score params")
			}
		}
		topic.join(t)
		tm.topics.Store(name, topic)
	default:
	}
	return topic, nil
}

// subscribe to the given topic and returns a channel to read the messages from
func (tm *topicManager) subscribe(name string) (<-chan *pubsub.Message, error) {
	adapter, err := tm.joinTopic(name)
	if err != nil {
		return nil, err
	}
	switch adapter.getState() {
	case topicStateSubscribing:
		return nil, ErrInProcess
	case topicStateJoined:
		adapter.subscribing()
		sub, err := adapter.topic.Subscribe()
		if err == pubsub.ErrTopicClosed {
			// rejoin a topic in case it was closed, and try to subscribe again
			adapter.close()
			adapter, err = tm.joinTopic(name)
			if err != nil {
				return nil, err
			}
			sub, err = adapter.topic.Subscribe()
			if err != nil {
				tm.logger.Warn("could not subscribe to topic", zap.String("topic", name), zap.Error(err))
			}
		}
		if sub == nil {
			adapter.close()
			return nil, ErrCouldNotSubscribe
		}
		in, cancel := tm.listen(sub)
		adapter.subscribe(sub, cancel)
		tm.topics.Store(name, adapter)
		return in, nil
	default:
	}
	return nil, nil
}

// listen handles incoming messages from the topic
func (tm *topicManager) listen(sub *pubsub.Subscription) (chan *pubsub.Message, context.CancelFunc) {
	ctx, cancel := context.WithCancel(tm.ctx)
	in := make(chan *pubsub.Message, bufSize)
	go func() {
		logger := tm.logger.With(zap.String("topic", sub.Topic()))
		defer close(in)
		defer sub.Cancel()
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
	return in, cancel
}

package topics

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

// Controller is an interface for managing pubsub topics
// it encapsulates all the functionality to facilitate subnets on top of
// pubsub (gossipsub v1.1)
type Controller interface {
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

// topicsCtrl implements Controller
type topicsCtrl struct {
	ctx    context.Context
	logger *zap.Logger
	ps     *pubsub.PubSub
	// scoreParams is a function that helps to set scoring params on topics
	scoreParams func(string) *pubsub.TopicScoreParams
	// topics holds all the available topics
	topics *sync.Map
}

// NewTopicsController creates an instance of Controller
func NewTopicsController(ctx context.Context, logger *zap.Logger, pubSub *pubsub.PubSub, scoreParams func(string) *pubsub.TopicScoreParams) Controller {
	return &topicsCtrl{
		ctx:         ctx,
		logger:      logger,
		ps:          pubSub,
		scoreParams: scoreParams,
		topics:      &sync.Map{},
	}
}

// Peers returns the peers subscribed to the given topic
func (ctrl *topicsCtrl) Peers(topicName string) ([]peer.ID, error) {
	topic := ctrl.getTopicState(topicName)
	if topic == nil {
		return nil, nil
	}
	if topic.getState() >= topicStateJoined {
		return topic.topic.ListPeers(), nil
	}
	return nil, ErrTopicNotReady
}

// Subscribe subscribes to the given topic
func (ctrl *topicsCtrl) Subscribe(topicName string) (<-chan *pubsub.Message, error) {
	return ctrl.subscribe(topicName)
}

// Topics lists all the available topics
func (ctrl *topicsCtrl) Topics() []string {
	return ctrl.ps.GetTopics()
}

// Unsubscribe unsubscribes from the given topic
func (ctrl *topicsCtrl) Unsubscribe(topicName string) error {
	topic := ctrl.getTopicState(topicName)
	if topic == nil {
		return nil
	}
	ctrl.topics.Delete(topicName)
	topic.close()
	return nil
}

// Broadcast publishes the message on the given topic
func (ctrl *topicsCtrl) Broadcast(topicName string, data []byte, timeout time.Duration) error {
	//topic := ctrl.fork.ValidatorTopicID(pk)
	topic, err := ctrl.joinTopic(topicName)
	if err != nil {
		return err
	}
	if !topic.canPublish() {
		return errors.New("can't publish message as topic is not ready")
	}
	//data, err := ctrl.fork.EncodeNetworkMsg(msg)
	//if err != nil {
	//	return errors.Wrap(err, "could not encode message")
	//}
	ctx, done := context.WithTimeout(ctrl.ctx, timeout)
	defer done()

	return topic.topic.Publish(ctx, data)
}

// getTopicState returns the topic wrapper if exist
func (ctrl *topicsCtrl) getTopicState(name string) *topicState {
	t, ok := ctrl.topics.Load(name)
	if !ok {
		return nil
	}
	return t.(*topicState)
}

// joinTopic joins the given topic and returns the wrapper
func (ctrl *topicsCtrl) joinTopic(name string) (*topicState, error) {
	state := ctrl.getTopicState(name)
	if state == nil {
		state = newTopicWrapper(ctrl.logger)
		ctrl.topics.Store(name, state)
	}
	switch state.getState() {
	case topicStateJoining:
		return state, ErrInProcess
	case topicStateClosed:
		state.joining()
		t, err := ctrl.ps.Join(name)
		if err != nil {
			ctrl.logger.Warn("could not join topic", zap.String("name", name), zap.Error(err))
			state.close()
			return nil, ErrCouldNotJoin
		}
		if ctrl.scoreParams != nil {
			err = t.SetScoreParams(ctrl.scoreParams(name))
			if err != nil {
				state.close()
				return nil, errors.Wrap(err, "could not set score params")
			}
		}
		state.join(t)
		ctrl.topics.Store(name, state)
	default:
	}
	return state, nil
}

// subscribe to the given topic and returns a channel to read the messages from
func (ctrl *topicsCtrl) subscribe(name string) (<-chan *pubsub.Message, error) {
	state, err := ctrl.joinTopic(name)
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
			state, err = ctrl.joinTopic(name)
			if err != nil {
				return nil, err
			}
			sub, err = state.topic.Subscribe()
			if err != nil {
				ctrl.logger.Warn("could not subscribe to topic", zap.String("topic", name), zap.Error(err))
			}
		}
		if sub == nil {
			state.close()
			return nil, ErrCouldNotSubscribe
		}
		in := ctrl.listen(state, sub)
		return in, nil
	default:
	}
	return nil, nil
}

// listen handles incoming messages from the topic
// it buffers results to make sure we keep up with incoming messages rate
// in case subscription returns error, the topic is closed
func (ctrl *topicsCtrl) listen(state *topicState, sub *pubsub.Subscription) chan *pubsub.Message {
	ctx, cancel := context.WithCancel(ctrl.ctx)
	in := make(chan *pubsub.Message, bufSize)
	go func() {
		state.subscribe(sub, cancel)
		ctrl.topics.Store(state.topic.String(), state)
		defer func() {
			state.close()
			close(in)
		}()
		logger := ctrl.logger.With(zap.String("topic", sub.Topic()))
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

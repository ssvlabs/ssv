package topics

import (
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"sync"
	"time"
)

const (
	bufSize = 32
	// subscriptionRequestLimit sets an upper bound for the number of topic we are allowed to subscribe to
	subscriptionRequestLimit = 2128
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
type Controller interface {
	WithPubsub(ps *pubsub.PubSub)
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
	// SubscriptionFilter allows controlling what topics the node will subscribe to
	// otherwise it might subscribe to irrelevant topics that were suggested by other peers
	pubsub.SubscriptionFilter
}

// topicsCtrl implements Controller
type topicsCtrl struct {
	ctx    context.Context
	logger *zap.Logger
	ps     *pubsub.PubSub
	// scoreParams is a function that helps to set scoring params on topics
	scoreParams func(string) *pubsub.TopicScoreParams
	// topics holds all the available topics
	topics           *sync.Map
	fork             forks.Fork
	msgValidatorFunc MsgValidatorFunc
}

// NewTopicsController creates an instance of Controller
func NewTopicsController(ctx context.Context, logger *zap.Logger, fork forks.Fork, msgValidatorFunc MsgValidatorFunc,
	pubSub *pubsub.PubSub, scoreParams func(string) *pubsub.TopicScoreParams) Controller {
	return &topicsCtrl{
		ctx:              ctx,
		logger:           logger,
		ps:               pubSub,
		scoreParams:      scoreParams,
		topics:           &sync.Map{},
		fork:             fork,
		msgValidatorFunc: msgValidatorFunc,
	}
}

// WithPubsub allows injecting a pubsub router
func (ctrl *topicsCtrl) WithPubsub(ps *pubsub.PubSub) {
	ctrl.ps = ps
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

// CanSubscribe returns true if the topic is of interest and we can subscribe to it
func (ctrl *topicsCtrl) CanSubscribe(topic string) bool {
	if _, ok := ctrl.topics.Load(topic); ok {
		return true
	}
	return false
}

// FilterIncomingSubscriptions is invoked for all RPCs containing subscription notifications.
// It should filter only the subscriptions of interest and my return an error if (for instance)
// there are too many subscriptions.
func (ctrl *topicsCtrl) FilterIncomingSubscriptions(pi peer.ID, subs []*ps_pb.RPC_SubOpts) ([]*ps_pb.RPC_SubOpts, error) {
	if len(subs) > subscriptionRequestLimit {
		return nil, pubsub.ErrTooManySubscriptions
	}

	return pubsub.FilterSubscriptions(subs, ctrl.CanSubscribe), nil
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
		//msgVal := ctrl.msgValidatorFactory.New(ctrl.logger.With(zap.String("topic", name)), ctrl.fork)
		if ctrl.msgValidatorFunc != nil {
			err = ctrl.ps.RegisterTopicValidator(name, ctrl.msgValidatorFunc,
				pubsub.WithValidatorConcurrency(512)) // TODO: find the best value for concurrency
			// TODO: check pubsub.WithValidatorInline() and pubsub.WithValidatorTimeout()
			if err != nil {
				state.close()
				return nil, errors.Wrap(err, "could not register topic validator")
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

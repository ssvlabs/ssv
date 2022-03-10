package topics

import (
	"context"
	"github.com/bloxapp/ssv/network/forks"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	ps_pb "github.com/libp2p/go-libp2p-pubsub/pb"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"sync"
	"time"
)

const (
	bufSize = 32
	// subscriptionRequestLimit sets an upper bound for the number of topic we are allowed to subscribe to
	subscriptionRequestLimit = 2128
)

var (
	// ErrLocked happens when you try to access a locked topic
	ErrLocked = errors.New("locked")
	// ErrInProcess happens when you try to subscribe multiple times concurrently
	// exported so the caller can decide how to act upon it
	ErrInProcess = errors.New("in process")
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

	fork                forks.Fork
	msgValidatorFactory func(string) MsgValidatorFunc

	sfTopics      *singleflight.Group
	activeTopics  *sync.Map
	allowedTopics *sync.Map

	sfSubs     *singleflight.Group
	activeSubs *sync.Map
}

// NewTopicsController creates an instance of Controller
func NewTopicsController(ctx context.Context, logger *zap.Logger, fork forks.Fork,
	msgValidatorFactory func(string) MsgValidatorFunc, pubSub *pubsub.PubSub,
	scoreParams func(string) *pubsub.TopicScoreParams) Controller {
	ctrl := &topicsCtrl{
		ctx:                 ctx,
		logger:              logger,
		ps:                  pubSub,
		scoreParams:         scoreParams,
		fork:                fork,
		msgValidatorFactory: msgValidatorFactory,

		sfTopics:      &singleflight.Group{},
		activeTopics:  &sync.Map{},
		allowedTopics: &sync.Map{},
		sfSubs:        &singleflight.Group{},
		activeSubs:    &sync.Map{},
	}

	return ctrl
}

func (ctrl *topicsCtrl) JoinTopic(name string) (*pubsub.Topic, error) {
	cn := ctrl.sfTopics.DoChan(name, func() (interface{}, error) {
		ctrl.logger.Debug("joining topic", zap.String("topic", name))
		ctrl.allowedTopics.Store(name, true)
		topic, err := ctrl.joinTopic(name)
		if err == nil {
			ctrl.logger.Debug("joined topic", zap.String("topic", name))
		}
		return topic, err
	})
	res := <-cn
	if res.Err != nil {
		return nil, res.Err
	}
	topic, ok := res.Val.(*pubsub.Topic)
	if !ok {
		return nil, errors.New("could not cast topic")
	}
	return topic, nil
}

func (ctrl *topicsCtrl) Subscribe(name string) (<-chan *pubsub.Message, error) {
	cn := ctrl.sfSubs.DoChan(name, func() (interface{}, error) {
		in, err := ctrl.subscribe(name)
		if err == nil {
			ctrl.logger.Debug("subscribed topic", zap.String("topic", name))
		}
		return in, err
	})
	res := <-cn
	if res.Err != nil {
		return nil, res.Err
	}
	in, ok := res.Val.(chan *pubsub.Message)
	if !ok {
		ctrl.logger.Debug("val", zap.Any("val", res.Val))
		return nil, errors.New("could not cast subscription channel")
	}
	return in, nil
}

// Broadcast publishes the message on the given topic
func (ctrl *topicsCtrl) Broadcast(topicName string, data []byte, timeout time.Duration) error {
	topic, err := ctrl.JoinTopic(topicName)
	if err != nil {
		return err
	}
	if topic == nil {
		return ErrTopicNotReady
	}

	ctx, done := context.WithTimeout(ctrl.ctx, timeout)
	defer done()

	return topic.Publish(ctx, data)
}

// WithPubsub allows injecting a pubsub router
func (ctrl *topicsCtrl) WithPubsub(ps *pubsub.PubSub) {
	ctrl.ps = ps
}

// Peers returns the peers subscribed to the given topic
func (ctrl *topicsCtrl) Peers(topicName string) ([]peer.ID, error) {
	topic := ctrl.getTopic(topicName)
	if topic == nil {
		return nil, ErrTopicNotReady
	}
	return topic.ListPeers(), nil
}

// Topics lists all the available topics
func (ctrl *topicsCtrl) Topics() []string {
	return ctrl.ps.GetTopics()
}

// Unsubscribe unsubscribes from the given topic
func (ctrl *topicsCtrl) Unsubscribe(topicName string) error {
	topic := ctrl.getTopic(topicName)
	if topic == nil {
		return nil
	}
	ctrl.closeTopic(topicName)
	return nil
}

// CanSubscribe returns true if the topic is of interest and we can subscribe to it
func (ctrl *topicsCtrl) CanSubscribe(topic string) bool {
	if _, ok := ctrl.allowedTopics.Load(topic); ok {
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

// getTopic returns the topic if exist
func (ctrl *topicsCtrl) getTopic(name string) *pubsub.Topic {
	t, ok := ctrl.activeTopics.Load(name)
	if !ok {
		return nil
	}
	return t.(*pubsub.Topic)
}

// getTopic returns the subscription if exit
func (ctrl *topicsCtrl) getSub(name string) *pubsub.Subscription {
	s, ok := ctrl.activeSubs.Load(name)
	if !ok {
		return nil
	}
	return s.(*pubsub.Subscription)
}

func (ctrl *topicsCtrl) subscribe(name string) (chan *pubsub.Message, error) {
	sub := ctrl.getSub(name)
	if sub == nil {
		topic, err := ctrl.JoinTopic(name)
		if err != nil {
			return nil, err
		}
		if topic == nil {
			return nil, ErrTopicNotReady
		}
		sub, err = topic.Subscribe()
		if err != nil {
			return nil, err
		}
		ctrl.activeSubs.Store(name, sub)
		return ctrl.listen(sub), nil
	}
	return nil, nil
}

func (ctrl *topicsCtrl) joinTopic(name string) (*pubsub.Topic, error) {
	topic := ctrl.getTopic(name)
	if topic != nil {
		return topic, nil
	}
	// joining topic
	topic, err := ctrl.ps.Join(name)
	if err != nil {
		return nil, err
	}
	if ctrl.scoreParams != nil {
		err = topic.SetScoreParams(ctrl.scoreParams(name))
		if err != nil {
			ctrl.logger.Warn("could not set topic score params", zap.String("topic", name), zap.Error(err))
		}
	}
	ctrl.setupTopicValidator(name)
	ctrl.activeTopics.Store(name, topic)
	return topic, nil
}

func (ctrl *topicsCtrl) setupTopicValidator(name string) {
	if ctrl.msgValidatorFactory != nil {
		ctrl.logger.Debug("setup topic validator", zap.String("topic", name))
		err := ctrl.ps.RegisterTopicValidator(name, ctrl.msgValidatorFactory(name),
			pubsub.WithValidatorConcurrency(512)) // TODO: find the best value for concurrency
		// TODO: check pubsub.WithValidatorInline() and pubsub.WithValidatorTimeout()
		if err != nil {
			ctrl.logger.Warn("could not register topic validator", zap.Error(err))
		}
	}
}

func (ctrl *topicsCtrl) closeTopic(name string) {
	sub := ctrl.getSub(name)
	sub.Cancel()
	ctrl.activeSubs.Delete(name)
	ctrl.activeTopics.Delete(name)
	if ctrl.msgValidatorFactory != nil {
		err := ctrl.ps.UnregisterTopicValidator(name)
		if err != nil {
			ctrl.logger.Warn("could not unregister msg validator", zap.String("topic", name), zap.Error(err))
		}
	}
}

// listen handles incoming messages from the topic
// it buffers results to make sure we keep up with incoming messages rate
// in case subscription returns error, the topic is closed
func (ctrl *topicsCtrl) listen(sub *pubsub.Subscription) chan *pubsub.Message {
	in := make(chan *pubsub.Message, bufSize)
	go func() {
		ctx, cancel := context.WithCancel(ctrl.ctx)
		defer cancel()
		logger := ctrl.logger.With(zap.String("topic", sub.Topic()))
		defer func() {
			sub.Cancel()
			close(in)
		}()
		logger.Info("start listening to topic")
		for ctx.Err() == nil {
			msg, err := sub.Next(ctx)
			if err != nil {
				if ctx.Err() != nil {
					logger.Debug("stop listening to topic: context is done", zap.Error(err))
				} else {
					logger.Warn("stop listening to topic: could not read message from subscription", zap.Error(err))
				}
				return
			}
			if msg == nil || msg.Data == nil {
				logger.Warn("got empty message from subscription")
				continue
			}
			in <- msg
		}
		logger.Info("boo")
	}()
	return in
}

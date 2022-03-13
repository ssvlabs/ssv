package topics

import (
	"context"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"golang.org/x/sync/singleflight"
	"sync"
	"time"
)

const (
	bufSize = 32
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
	// Subscribe subscribes to the given topic
	Subscribe(name string) error
	// Unsubscribe unsubscribes from the given topic
	Unsubscribe(topicName string) error
	// Peers returns the peers subscribed to the given topic
	Peers(topicName string) ([]peer.ID, error)
	// Topics lists all the available topics
	Topics() []string
	// Broadcast publishes the message on the given topic
	Broadcast(topicName string, data []byte, timeout time.Duration) error
}

// PubsubMessageHandler handles incoming messages
type PubsubMessageHandler func(string, *pubsub.Message) error

// topicsCtrl implements Controller
type topicsCtrl struct {
	ctx    context.Context
	logger *zap.Logger
	ps     *pubsub.PubSub
	// scoreParams is a function that helps to set scoring params on topics
	scoreParams func(string) *pubsub.TopicScoreParams

	msgHandler          PubsubMessageHandler
	msgValidatorFactory func(string) MsgValidatorFunc
	subFilter           SubFilter

	sfTopics     *singleflight.Group
	activeTopics *sync.Map

	sfSubs     *singleflight.Group
	activeSubs *sync.Map
}

// NewTopicsController creates an instance of Controller
func NewTopicsController(ctx context.Context, logger *zap.Logger, msgHandler PubsubMessageHandler,
	msgValidatorFactory func(string) MsgValidatorFunc, subFilter SubFilter, pubSub *pubsub.PubSub,
	scoreParams func(string) *pubsub.TopicScoreParams) Controller {
	ctrl := &topicsCtrl{
		ctx:                 ctx,
		logger:              logger,
		ps:                  pubSub,
		scoreParams:         scoreParams,
		msgHandler:          msgHandler,
		msgValidatorFactory: msgValidatorFactory,

		sfTopics:     &singleflight.Group{},
		activeTopics: &sync.Map{},
		subFilter:    subFilter,
		sfSubs:       &singleflight.Group{},
		activeSubs:   &sync.Map{},
	}

	return ctrl
}

// JoinTopic joins the given topic, it uses singleflight to streamline join requests
func (ctrl *topicsCtrl) JoinTopic(name string) (*pubsub.Topic, error) {
	cn := ctrl.sfTopics.DoChan(name, func() (interface{}, error) {
		ctrl.subFilter.Register(name)
		return ctrl.joinTopic(name)
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

// Subscribe subscribes to the given topic, it can handle multiple concurrent calls.
// it will create a single goroutine and channel for every topic
func (ctrl *topicsCtrl) Subscribe(name string) error {
	cn := ctrl.sfSubs.DoChan(name, func() (interface{}, error) {
		err := ctrl.subscribe(name)
		return nil, err
	})
	res := <-cn
	return res.Err
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
// TODO: handle errors, currently they are just printed to log inside closeTopic()
func (ctrl *topicsCtrl) Unsubscribe(topicName string) error {
	topic := ctrl.getTopic(topicName)
	if topic == nil {
		return nil
	}
	<-ctrl.sfTopics.DoChan(topicName, func() (interface{}, error) {
		ctrl.closeTopic(topicName)
		return nil, nil
	})
	return nil
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

// subscribe keeps a live listener on the given topic.
// it spawns a goroutine for listening to topic messages,
// which will try to resubscribe in case of some failure while reading messages.
// if resubscribe fails we exit.
func (ctrl *topicsCtrl) subscribe(name string) error {
	sub := ctrl.getSub(name)
	if sub == nil {
		sub, err := ctrl.subscribeUnsafe(name)
		if err != nil {
			return err
		}
		go func() {
			for {
				if _, ok := ctrl.activeTopics.Load(name); !ok {
					return
				}
				err := ctrl.listen(sub)
				if err == nil {
					// context done or subscription was cancelled -> exit
					return
				}
				// otherwise, trying to resubscribe
				if sub, err = ctrl.subscribeUnsafe(name); err != nil {
					return
				}
			}
		}()
	}
	return nil
}

// subscribeUnsafe is subscribing to the given topic, and makes sure that we joined it.
// NOTE: it is an internal method that should be called from within subscribe, to avoid concurrency issue
func (ctrl *topicsCtrl) subscribeUnsafe(name string) (*pubsub.Subscription, error) {
	topic, err := ctrl.JoinTopic(name)
	if err != nil {
		return nil, err
	}
	if topic == nil {
		return nil, ErrTopicNotReady
	}
	sub, err := topic.Subscribe()
	if err != nil {
		return nil, err
	}
	ctrl.logger.Debug("subscribed topic", zap.String("topic", name))
	ctrl.activeSubs.Store(name, sub)
	return sub, nil
}

func (ctrl *topicsCtrl) joinTopic(name string) (*pubsub.Topic, error) {
	topic := ctrl.getTopic(name)
	if topic != nil {
		return topic, nil
	}
	ctrl.logger.Debug("joining topic", zap.String("topic", name))
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
	ctrl.activeTopics.Store(name, topic)
	ctrl.setupTopicValidator(name)
	ctrl.logger.Debug("joined topic", zap.String("topic", name))
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

// closeTopic cancels subscription, closes the topic, and clears it from all the maps
func (ctrl *topicsCtrl) closeTopic(name string) {
	if sub := ctrl.getSub(name); sub != nil {
		ctrl.activeSubs.Delete(name)
		sub.Cancel()
	}
	if topic := ctrl.getTopic(name); topic != nil {
		ctrl.activeTopics.Delete(name)
		if err := topic.Close(); err != nil {
			ctrl.logger.Warn("could not close topic", zap.String("topic", name), zap.Error(err))
		}
		if ctrl.msgValidatorFactory != nil {
			err := ctrl.ps.UnregisterTopicValidator(name)
			if err != nil {
				ctrl.logger.Warn("could not unregister msg validator", zap.String("topic", name), zap.Error(err))
			}
		}
	}
	ctrl.subFilter.Deregister(name)
}

// listen handles incoming messages from the topic
// it buffers results to make sure we keep up with incoming messages rate
// in case subscription returns error, the topic is closed
func (ctrl *topicsCtrl) listen(sub *pubsub.Subscription) error {
	in := make(chan *pubsub.Message, bufSize)
	ctx, cancel := context.WithCancel(ctrl.ctx)
	defer cancel()
	topicName := sub.Topic()
	logger := ctrl.logger.With(zap.String("topic", topicName))
	defer func() {
		ctrl.activeSubs.Delete(topicName)
		sub.Cancel()
		close(in)
		// TODO: unsubscribe?
	}()
	go func() {
		for msg := range in {
			if err := ctrl.msgHandler(topicName, msg); err != nil {
				logger.Debug("could not handle msg", zap.Error(err))
			}
		}
	}()
	logger.Info("start listening to topic")
	for ctx.Err() == nil {
		msg, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Debug("stop listening to topic: context is done", zap.Error(err))
				return nil
			} else if err.Error() == "subscription cancelled" {
				logger.Debug("stop listening to topic: subscription cancelled", zap.Error(err))
				return nil
			}
			logger.Warn("could not read message from subscription", zap.Error(err))
			return err
		}
		if msg == nil || msg.Data == nil {
			logger.Warn("got empty message from subscription")
			continue
		}
		in <- msg
	}
	return nil
}

package topics

import (
	"context"
	"fmt"
	"github.com/libp2p/go-libp2p-core/peer"
	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/pkg/errors"
	"go.uber.org/zap"
	"strings"
	"sync"
	"time"
)

const (
	topicPrefix = "bloxstaking.ssv"
)

var (
	// ErrTopicNotReady happens when trying to access a topic which is not ready yet
	ErrTopicNotReady = errors.New("topic is not ready")
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
	// scoreParamsFactory is a function that helps to set scoring params on topics
	scoreParamsFactory  func(string) *pubsub.TopicScoreParams
	msgValidatorFactory func(string) MsgValidatorFunc
	msgHandler          PubsubMessageHandler
	subFilter           SubFilter

	containers map[string]*topicContainer
	topicsLock *sync.RWMutex
}

// NewTopicsController creates an instance of Controller
func NewTopicsController(ctx context.Context, logger *zap.Logger, msgHandler PubsubMessageHandler,
	msgValidatorFactory func(string) MsgValidatorFunc, subFilter SubFilter, pubSub *pubsub.PubSub,
	scoreParams func(string) *pubsub.TopicScoreParams) Controller {
	ctrl := &topicsCtrl{
		ctx:                 ctx,
		logger:              logger,
		ps:                  pubSub,
		scoreParamsFactory:  scoreParams,
		msgValidatorFactory: msgValidatorFactory,
		msgHandler:          msgHandler,

		topicsLock: &sync.RWMutex{},
		containers: make(map[string]*topicContainer),

		subFilter: subFilter,
	}

	return ctrl
}

// Peers returns the peers subscribed to the given topic
func (ctrl *topicsCtrl) Peers(topicName string) ([]peer.ID, error) {
	topicName = getTopicName(topicName)
	cont := ctrl.getTopicContainer(topicName)
	if cont == nil || cont.topic == nil {
		return nil, ErrTopicNotReady
	}
	peers := cont.topic.ListPeers()
	return peers, nil
}

// Topics lists all the available topics
func (ctrl *topicsCtrl) Topics() []string {
	topics := ctrl.ps.GetTopics()
	for i, tp := range topics {
		topics[i] = getTopicBaseName(tp)
	}
	return topics
}

// Subscribe subscribes to the given topic, it can handle multiple concurrent calls.
// it will create a single goroutine and channel for every topic
func (ctrl *topicsCtrl) Subscribe(name string) error {
	name = getTopicName(name)
	tc, err := ctrl.joinTopic(name)
	if err == nil && tc != nil {
		tc.incSubCount()
	}
	return err
}

// Broadcast publishes the message on the given topic
func (ctrl *topicsCtrl) Broadcast(name string, data []byte, timeout time.Duration) error {
	name = getTopicName(name)

	tc, err := ctrl.joinTopic(name)
	if err != nil {
		return err
	}

	ctx, done := context.WithTimeout(ctrl.ctx, timeout)
	defer done()

	ctrl.logger.Debug("broadcasting message on topic", zap.String("topic", name))

	return tc.Publish(ctx, data)
}

// Unsubscribe unsubscribes from the given topic, only if there are no other subscribers of the given topic
func (ctrl *topicsCtrl) Unsubscribe(name string) error {
	ctrl.topicsLock.Lock()
	defer ctrl.topicsLock.Unlock()

	tc := ctrl.getTopicContainerUnsafe(name)
	if tc == nil {
		return nil
	}
	if subCount := tc.decSubCount(); subCount > 0 {
		ctrl.logger.Debug("there are still active subscriptions for this topic",
			zap.String("topic", name), zap.Int32("subCount", subCount))
		ctrl.setTopicContainerUnsafe(name, tc)
		return nil
	}
	ctrl.logger.Debug("unsubscribing topic", zap.String("topic", name))
	delete(ctrl.containers, name)

	if err := tc.Close(); err != nil {
		return err
	}
	if ctrl.msgValidatorFactory != nil {
		err := ctrl.ps.UnregisterTopicValidator(name)
		if err != nil {
			ctrl.logger.Warn("could not unregister msg validator", zap.String("topic", name), zap.Error(err))
		}
	}
	ctrl.subFilter.Deregister(name)
	return nil
}

func (ctrl *topicsCtrl) getTopicContainer(name string) *topicContainer {
	ctrl.topicsLock.RLock()
	defer ctrl.topicsLock.RUnlock()

	return ctrl.getTopicContainerUnsafe(name)
}

func (ctrl *topicsCtrl) getTopicContainerUnsafe(name string) *topicContainer {
	tc, ok := ctrl.containers[name]
	if !ok {
		return nil
	}
	return tc
}

func (ctrl *topicsCtrl) setTopicContainerUnsafe(name string, tc *topicContainer) {
	ctrl.containers[name] = tc
}

// joinTopic joins and subscribes the given topic
func (ctrl *topicsCtrl) joinTopic(name string) (*topicContainer, error) {
	ctrl.topicsLock.Lock()
	// get or create the container
	tc := ctrl.getTopicContainerUnsafe(name)
	if tc == nil {
		tc = newTopicContainer()
		ctrl.setTopicContainerUnsafe(name, tc)
		// initial setup for the topic, should happen only once
		ctrl.subFilter.Register(name)
		if err := ctrl.setupTopicValidator(name); err != nil {
			// TODO: close topic?
			//return err
			ctrl.logger.Warn("failed to setup topic", zap.String("topic", name), zap.Error(err))
		}
	}
	// lock topic and release main lock
	ctrl.topicsLock.Unlock()
	tc.locker.Lock()
	defer tc.locker.Unlock()

	if tc.topic != nil { // already joined topic
		return tc, nil
	}

	err := ctrl.joinTopicUnsafe(tc, name)
	if err != nil {
		return nil, err
	}

	go ctrl.start(name, tc)

	return tc, nil
}

// start will listen to *pubsub.Subscription,
// if some error happened we try to leave and rejoin the topic
// the loop stops once a topic is unsubscribed and therefore not listed
func (ctrl *topicsCtrl) start(name string, tc *topicContainer) {
	for {
		err := ctrl.listen(tc.sub)
		// rejoin in case failed for some reason
		if err != nil {
			ctrl.logger.Warn("failed listening to topic", zap.String("topic", name), zap.Error(err))
			time.Sleep(time.Second)
			err = ctrl.rejoinTopic(name)
			if err == nil {
				continue
			}
			ctrl.logger.Warn("could not rejoin topic", zap.String("topic", name), zap.Error(err))
		}
		return
	}
}

// listen handles incoming messages from the topic
func (ctrl *topicsCtrl) listen(sub *pubsub.Subscription) error {
	ctx, cancel := context.WithCancel(ctrl.ctx)
	defer cancel()
	topicName := sub.Topic()
	logger := ctrl.logger.With(zap.String("topic", topicName))
	logger.Info("start listening to topic")
	for ctx.Err() == nil {
		msg, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Debug("stop listening to topic: context is done")
				return nil
			} else if err.Error() == "subscription cancelled" {
				logger.Debug("stop listening to topic: subscription cancelled")
				return nil
			}
			logger.Warn("could not read message from subscription", zap.Error(err))
			// TODO: handle instead of return?
			return err
		}
		if msg == nil || msg.Data == nil {
			logger.Warn("got empty message from subscription")
			continue
		}
		metricsPubsubInbound.WithLabelValues(getTopicBaseName(topicName)).Inc()
		if err := ctrl.msgHandler(topicName, msg); err != nil {
			logger.Debug("could not handle msg", zap.Error(err))
		}
	}
	return nil
}

// setupTopicValidator registers the topic validator
func (ctrl *topicsCtrl) setupTopicValidator(name string) error {
	if ctrl.msgValidatorFactory != nil {
		ctrl.logger.Debug("setup topic validator", zap.String("topic", name))
		err := ctrl.ps.RegisterTopicValidator(name, ctrl.msgValidatorFactory(name),
			pubsub.WithValidatorConcurrency(512)) // TODO: find the best value for concurrency
		// TODO: check pubsub.WithValidatorInline() and pubsub.WithValidatorTimeout()
		if err != nil {
			//ctrl.logger.Warn("could not register topic validator", zap.Error(err))
			return errors.Wrap(err, "could not register topic validator")
		}
	}
	return nil
}

func (ctrl *topicsCtrl) rejoinTopic(name string) error {
	tc := ctrl.getTopicContainer(name)
	if tc.topic != nil {
		tc.locker.Lock()
		defer tc.locker.Unlock()
		tc.sub.Cancel()
		if err := tc.topic.Close(); err != nil {
			ctrl.logger.Warn("failed to close topic", zap.String("topic", name), zap.Error(err))
		}
		if err := ctrl.joinTopicUnsafe(tc, name); err != nil {
			ctrl.logger.Warn("could not join topic", zap.String("topic", name), zap.Error(err))
			return err
		}
	}
	return nil
}

func (ctrl *topicsCtrl) joinTopicUnsafe(tc *topicContainer, name string) error {
	ctrl.logger.Debug("joining topic", zap.String("topic", name))
	topic, err := ctrl.ps.Join(name)
	if err != nil {
		return err
	}
	tc.topic = topic
	if ctrl.scoreParamsFactory != nil {
		if p := ctrl.scoreParamsFactory(name); p != nil {
			if err := topic.SetScoreParams(p); err != nil {
				//ctrl.logger.Warn("could not set topic score params", zap.String("topic", name), zap.Error(err))
				return errors.Wrap(err, "could not set topic score params")
			}
		}
	}

	sub, err := topic.Subscribe()
	if err != nil {
		ctrl.logger.Warn("could not subscribe to topic", zap.String("topic", name), zap.Error(err))
		if errClose := tc.Close(); errClose != nil {
			ctrl.logger.Warn("could not close topic", zap.String("topic", name), zap.Error(errClose))
		}
		return err
	}
	tc.sub = sub

	return nil
}

// getTopicName returns the topic full name, including prefix
// TODO: consider moving this to network fork
func getTopicName(baseName string) string {
	return fmt.Sprintf("%s.%s", topicPrefix, baseName)
}

// getTopicBaseName return the base topic name of the topic, w/o ssv prefix
// TODO: consider moving this to network fork
func getTopicBaseName(topicName string) string {
	return strings.Replace(topicName, fmt.Sprintf("%s.", topicPrefix), "", 1)
}

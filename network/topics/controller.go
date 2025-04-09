package topics

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	pubsub "github.com/libp2p/go-libp2p-pubsub"
	"github.com/libp2p/go-libp2p/core/peer"
	"go.opentelemetry.io/otel/metric"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/network/commons"
	"github.com/ssvlabs/ssv/protocol/v2/ssv/queue"
)

var (
	// ErrTopicNotReady happens when trying to access a topic which is not ready yet
	ErrTopicNotReady = errors.New("topic is not ready")
)

// Controller is an interface for managing pubsub topics
type Controller interface {
	// Subscribe subscribes to the given topic
	Subscribe(logger *zap.Logger, name string) error
	// Unsubscribe unsubscribes from the given topic
	Unsubscribe(logger *zap.Logger, topicName string, hard bool) error
	// Peers returns a list of peers we are connected to in the given topic, if topicName
	// param is an empty string it returns a list of all peers we are connected to.
	Peers(topicName string) ([]peer.ID, error)
	// Topics lists all topics this node is subscribed to
	Topics() []string
	// Broadcast publishes the message on the given topic
	Broadcast(topicName string, data []byte, timeout time.Duration) error
	// UpdateScoreParams refreshes the score params for every subscribed topic
	UpdateScoreParams(logger *zap.Logger) error

	io.Closer
}

// PubsubMessageHandler handles incoming messages
type PubsubMessageHandler func(context.Context, string, *pubsub.Message) error

type messageValidator interface {
	ValidatorForTopic(topic string) func(ctx context.Context, p peer.ID, pmsg *pubsub.Message) pubsub.ValidationResult
}

// topicsCtrl implements Controller
type topicsCtrl struct {
	ctx    context.Context
	logger *zap.Logger // struct logger to implement i.Closer
	ps     *pubsub.PubSub
	// scoreParamsFactory is a function that helps to set scoring params on topics
	scoreParamsFactory func(string) *pubsub.TopicScoreParams
	msgValidator       messageValidator
	msgHandler         PubsubMessageHandler
	subFilter          SubFilter

	container *topicsContainer
}

// NewTopicsController creates an instance of Controller
func NewTopicsController(
	ctx context.Context,
	logger *zap.Logger,
	msgHandler PubsubMessageHandler,
	msgValidator messageValidator,
	subFilter SubFilter,
	pubSub *pubsub.PubSub,
	scoreParams func(string) *pubsub.TopicScoreParams,
) Controller {
	ctrl := &topicsCtrl{
		ctx:                ctx,
		logger:             logger,
		ps:                 pubSub,
		scoreParamsFactory: scoreParams,
		msgValidator:       msgValidator,
		msgHandler:         msgHandler,

		subFilter: subFilter,
	}

	ctrl.container = newTopicsContainer(pubSub, ctrl.onNewTopic(logger))

	return ctrl
}

func (ctrl *topicsCtrl) onNewTopic(logger *zap.Logger) onTopicJoined {
	return func(ps *pubsub.PubSub, topic *pubsub.Topic) {
		// initial setup for the topic, should happen only once
		name := topic.String()
		if err := ctrl.setupTopicValidator(topic.String()); err != nil {
			// TODO: close topic?
			// return err
			logger.Warn("could not setup topic", zap.String("topic", name), zap.Error(err))
		}
		if ctrl.scoreParamsFactory != nil {
			if p := ctrl.scoreParamsFactory(name); p != nil {
				logger.Debug("using scoring params for topic", zap.String("topic", name), zap.Any("params", p))
				if err := topic.SetScoreParams(p); err != nil {
					// logger.Warn("could not set topic score params", zap.String("topic", name), zap.Error(err))
					logger.Warn("could not set topic score params", zap.String("topic", name), zap.Error(err))
				}
			}
		}
	}
}

func (ctrl *topicsCtrl) UpdateScoreParams(logger *zap.Logger) error {
	if ctrl.scoreParamsFactory == nil {
		return fmt.Errorf("scoreParamsFactory is not set")
	}
	var errs error
	topics := ctrl.ps.GetTopics()
	for _, topicName := range topics {
		topic := ctrl.container.Get(topicName)
		if topic == nil {
			errs = errors.Join(errs, fmt.Errorf("topic %s is not ready; ", topicName))
			continue
		}
		p := ctrl.scoreParamsFactory(topicName)
		if p == nil {
			errs = errors.Join(errs, fmt.Errorf("score params for topic %s is nil; ", topicName))
			continue
		}
		if err := topic.SetScoreParams(p); err != nil {
			errs = errors.Join(errs, fmt.Errorf("could not set score params for topic %s: %w; ", topicName, err))
			continue
		}
	}
	return errs
}

// Close implements io.Closer
func (ctrl *topicsCtrl) Close() error {
	topics := ctrl.ps.GetTopics()
	for _, tp := range topics {
		_ = ctrl.Unsubscribe(ctrl.logger, commons.GetTopicBaseName(tp), true)
		_ = ctrl.container.Leave(tp)
	}
	return nil
}

// Peers returns a list of peers we are connected to in the given topic.
func (ctrl *topicsCtrl) Peers(name string) ([]peer.ID, error) {
	if name == "" {
		return ctrl.ps.ListPeers(""), nil
	}
	name = commons.GetTopicFullName(name)
	topic := ctrl.container.Get(name)
	if topic == nil {
		return nil, nil
	}
	return topic.ListPeers(), nil
}

// Topics lists all topics this node is subscribed to
func (ctrl *topicsCtrl) Topics() []string {
	topics := ctrl.ps.GetTopics()
	for i, tp := range topics {
		topics[i] = commons.GetTopicBaseName(tp)
	}
	return topics
}

// Subscribe subscribes to the given topic, it can handle multiple concurrent calls.
// it will create a single goroutine and channel for every topic
func (ctrl *topicsCtrl) Subscribe(logger *zap.Logger, name string) error {
	name = commons.GetTopicFullName(name)
	ctrl.subFilter.(Whitelist).Register(name)
	sub, err := ctrl.container.Subscribe(name)
	defer logger.Debug("subscribing to topic", zap.String("topic", name), zap.Bool("already_subscribed", sub == nil), zap.Error(err))
	if err != nil {
		return err
	}
	if sub == nil { // already subscribed
		return nil
	}
	go ctrl.start(logger, name, sub)

	return nil
}

// Broadcast publishes the message on the given topic
func (ctrl *topicsCtrl) Broadcast(name string, data []byte, timeout time.Duration) error {
	name = commons.GetTopicFullName(name)

	topic, err := ctrl.container.Join(name)
	if err != nil {
		return err
	}

	go func() {
		ctx, done := context.WithTimeout(ctrl.ctx, timeout)
		defer done()

		err := topic.Publish(ctx, data)
		if err == nil {
			outboundMessageCounter.Add(ctrl.ctx, 1)
		}
	}()

	return err
}

// Unsubscribe unsubscribes from the given topic, only if there are no other subscribers of the given topic
// if hard is true, we will unsubscribe the topic even if there are more subscribers.
func (ctrl *topicsCtrl) Unsubscribe(logger *zap.Logger, name string, hard bool) error {
	name = commons.GetTopicFullName(name)

	if !ctrl.container.Unsubscribe(name) {
		return fmt.Errorf("failed to unsubscribe from topic %s: not subscribed", name)
	}

	if ctrl.msgValidator != nil {
		err := ctrl.ps.UnregisterTopicValidator(name)
		if err != nil {
			logger.Debug("could not unregister msg validator", zap.String("topic", name), zap.Error(err))
		}
	}
	ctrl.subFilter.(Whitelist).Deregister(name)

	return nil
}

// start will listen to *pubsub.Subscription,
// if some error happened we try to leave and rejoin the topic
// the loop stops once a topic is unsubscribed and therefore not listed
func (ctrl *topicsCtrl) start(logger *zap.Logger, name string, sub *pubsub.Subscription) {
	for ctrl.ctx.Err() == nil {
		err := ctrl.listen(logger, sub)
		if err == nil {
			return
		}
		// rejoin in case failed
		logger.Debug("could not listen to topic", zap.String("topic", name), zap.Error(err))
		ctrl.container.Unsubscribe(name)
		_ = ctrl.container.Leave(name)
		sub, err = ctrl.container.Subscribe(name)
		if err == nil {
			continue
		}
		logger.Debug("could not rejoin topic", zap.String("topic", name), zap.Error(err))
	}
}

// listen handles incoming messages from the topic
func (ctrl *topicsCtrl) listen(logger *zap.Logger, sub *pubsub.Subscription) error {
	ctx, cancel := context.WithCancel(ctrl.ctx)
	defer cancel()

	topicName := sub.Topic()

	logger = logger.With(zap.String("topic", topicName))
	logger.Debug("start listening to topic")
	for ctx.Err() == nil {
		msg, err := sub.Next(ctx)
		if err != nil {
			if ctx.Err() != nil {
				logger.Debug("stop listening to topic: context is done")
				return nil
			} else if errors.Is(err, pubsub.ErrSubscriptionCancelled) || errors.Is(err, pubsub.ErrTopicClosed) {
				logger.Debug("stop listening to topic", zap.Error(err))
				return nil
			}
			logger.Warn("could not read message from subscription", zap.Error(err))
			continue
		}
		if msg == nil || msg.Data == nil {
			logger.Warn("got empty message from subscription")
			continue
		}

		switch m := msg.ValidatorData.(type) {
		case *queue.SSVMessage:
			inboundMessageCounter.Add(ctrl.ctx, 1,
				metric.WithAttributes(messageTypeAttribute(uint64(m.MsgType))))
		default:
			logger.Warn("unknown message type", zap.Any("message", m))
		}

		if err := ctrl.msgHandler(ctx, topicName, msg); err != nil {
			logger.Debug("could not handle msg", zap.Error(err))
		}
	}
	return nil
}

// setupTopicValidator registers the topic validator
func (ctrl *topicsCtrl) setupTopicValidator(name string) error {
	if ctrl.msgValidator != nil {
		// first try to unregister in case there is already a msg validator for that topic (e.g. fork scenario)
		err := ctrl.ps.UnregisterTopicValidator(name)
		if err != nil {
			ctrl.logger.Debug("failed to unregister topic validator", zap.String("topic", name), zap.Error(err))
		}

		var opts []pubsub.ValidatorOpt
		// Optional: set a timeout for message validation
		// opts = append(opts, pubsub.WithValidatorTimeout(time.Second))

		err = ctrl.ps.RegisterTopicValidator(name, ctrl.msgValidator.ValidatorForTopic(name), opts...)
		if err != nil {
			return fmt.Errorf("could not register topic validator: %w", err)
		}
	}
	return nil
}

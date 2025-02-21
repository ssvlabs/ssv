package goclient

import (
	"context"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type Event interface {
}

type Subscriber[T Event] struct {
	Identifier string
	Channel    chan T
}

func NewSubscriber[T Event](identifier string) *Subscriber[T] {
	return &Subscriber[T]{
		Identifier: identifier,
		Channel:    make(chan T, 32),
	}
}

func (gc *GoClient) SubscribeToHeadEvents(ctx context.Context, subscriberIdentifier string) (<-chan *apiv1.HeadEvent, error) {
	gc.subscribersLock.Lock()
	defer gc.subscribersLock.Unlock()
	logger := gc.log.With(zap.String("subscriber_identifier", subscriberIdentifier))

	logger.Info("Adding 'head' event subscriber")

	if len(gc.headEventSubscribers) == 0 {
		logger.Info("Launching event listener")
		if err := gc.startEventListener(ctx); err != nil {
			const errMsg = "Failed to start event listener"
			logger.Error(errMsg, zap.Error(err))
			return nil, errors.Wrap(err, errMsg)
		}
	}

	headEventSubscriber := NewSubscriber[*apiv1.HeadEvent](subscriberIdentifier)
	gc.headEventSubscribers = append(gc.headEventSubscribers, headEventSubscriber)

	logger.
		With(zap.Int("head_event_subscribers_len", len(gc.headEventSubscribers))).
		Info("Subscribed to head events")

	return headEventSubscriber.Channel, nil
}

func (gc *GoClient) startEventListener(ctx context.Context) error {
	var strTopics []string
	for _, topic := range gc.supportedTopics {
		strTopics = append(strTopics, string(topic))
	}

	if err := gc.multiClient.Events(ctx, strTopics, gc.eventHandler); err != nil {
		gc.log.Error(clResponseErrMsg, zap.String("api", "Events"), zap.Error(err))
		return err
	}

	return nil
}

func (gc *GoClient) eventHandler(e *apiv1.Event) {
	switch EventTopic(e.Topic) {
	case HeadEventTopic:
		if e.Data == nil {
			gc.log.
				With(zap.String("topic", e.Topic)).
				Error("event data is nil")
			return
		}
		headEventData := e.Data.(*apiv1.HeadEvent)
		for _, sub := range gc.headEventSubscribers {
			select {
			case sub.Channel <- headEventData:
			default:
				gc.log.
					With(zap.String("topic", e.Topic)).
					With(zap.String("subscriber_identifier", sub.Identifier)).
					Error("subscriber channel full, dropping the message")
			}
		}
	default:
		gc.log.
			With(zap.String("topic", e.Topic)).
			Error("unsupported event topic")
	}
}

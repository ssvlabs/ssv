package goclient

import (
	"context"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

type event interface {
	*apiv1.HeadEvent
}

type subscriber[T event] struct {
	Identifier string
	Channel    chan T
}

func (gc *GoClient) SubscribeToHeadEvents(ctx context.Context, subscriberIdentifier string) (<-chan *apiv1.HeadEvent, error) {
	gc.subscribersLock.Lock()
	defer gc.subscribersLock.Unlock()
	logger := gc.log.With(zap.String("subscriber_identifier", subscriberIdentifier))

	logger.Info("adding 'head' event subscriber")

	if len(gc.headEventSubscribers) == 0 {
		logger.Info("launching event listener")
		if err := gc.startEventListener(ctx); err != nil {
			const errMsg = "failed to start event listener"
			logger.Error(errMsg, zap.Error(err))
			return nil, errors.Wrap(err, errMsg)
		}
	}

	headEventSubscriber := subscriber[*apiv1.HeadEvent]{
		Identifier: subscriberIdentifier,
		Channel:    make(chan *apiv1.HeadEvent, 32),
	}
	gc.headEventSubscribers = append(gc.headEventSubscribers, headEventSubscriber)

	logger.
		With(zap.Int("head_event_subscribers_len", len(gc.headEventSubscribers))).
		Info("subscribed to head events")

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

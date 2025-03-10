package goclient

import (
	"context"
	"fmt"
	"slices"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
)

type event interface {
	*apiv1.HeadEvent
}

type subscriber[T event] struct {
	Identifier string
	Channel    chan<- T
}

func (gc *GoClient) SubscribeToHeadEvents(ctx context.Context, subscriberIdentifier string, ch chan<- *apiv1.HeadEvent) error {
	logger := gc.log.With(zap.String("subscriber_identifier", subscriberIdentifier))

	if !slices.Contains(gc.supportedTopics, EventTopicHead) {
		logger.Warn("the list of supported topics did not contain 'HeadEventTopic', cannot add new subscriber")
		return fmt.Errorf("the list of supported topics did not contain 'HeadEventTopic', cannot add new subscriber")
	}

	logger.Info("adding 'head' event subscriber")

	headEventSubscriber := subscriber[*apiv1.HeadEvent]{
		Identifier: subscriberIdentifier,
		Channel:    ch,
	}

	gc.subscribersLock.Lock()
	defer gc.subscribersLock.Unlock()

	gc.headEventSubscribers = append(gc.headEventSubscribers, headEventSubscriber)

	logger.
		With(zap.Int("head_event_subscribers_len", len(gc.headEventSubscribers))).
		Info("subscribed to head events")

	return nil
}

func (gc *GoClient) startEventListener(ctx context.Context) error {
	if len(gc.supportedTopics) == 0 {
		gc.log.Warn("the list of supported topics was empty, won't launch event listener")
		return nil
	}

	var strTopics []string
	for _, topic := range gc.supportedTopics {
		strTopics = append(strTopics, string(topic))
	}

	for _, client := range gc.clients {
		if err := client.Events(ctx, strTopics, gc.eventHandler); err != nil {
			gc.log.Error(clResponseErrMsg, zap.String("api", "Events"), zap.Error(err))
			return err
		}
	}

	gc.log.
		With(zap.Int("clients_len", len(gc.clients))).
		Debug("subscribed to events")

	return nil
}

func (gc *GoClient) eventHandler(e *apiv1.Event) {
	if e == nil {
		gc.log.Warn("event was nil, skipping")
		return
	}

	switch EventTopic(e.Topic) {
	case EventTopicHead:
		logger := gc.log.
			With(zap.String("topic", e.Topic))
		logger.Debug("event received")

		if e.Data == nil {
			logger.Warn("event data is nil")
			return
		}
		headEventData, ok := e.Data.(*apiv1.HeadEvent)
		if !ok {
			logger.Warn("could not type assert")
			return
		}

		gc.lastProcessedHeadEventSlotLock.Lock()
		if headEventData.Slot <= gc.lastProcessedHeadEventSlot {
			logger.
				With(zap.Uint64("event_slot", uint64(headEventData.Slot))).
				With(zap.Uint64("last_processed_slot", uint64(gc.lastProcessedHeadEventSlot))).
				Debug("event slot is lower or equal than last processed slot")
			gc.lastProcessedHeadEventSlotLock.Unlock()
			return
		}

		gc.lastProcessedHeadEventSlot = headEventData.Slot
		gc.lastProcessedHeadEventSlotLock.Unlock()

		cacheItem := gc.blockRootToSlotCache.Set(headEventData.Block, headEventData.Slot, ttlcache.NoTTL)
		logger.
			With(zap.Int64("cache_item_version", cacheItem.Version())).
			With(fields.Slot(headEventData.Slot)).
			With(fields.BlockRoot(headEventData.Block)).
			Info("block root to slot cache updated")

		gc.subscribersLock.RLock()
		defer gc.subscribersLock.RUnlock()

		for _, sub := range gc.headEventSubscribers {
			logger = logger.With(zap.String("subscriber_identifier", sub.Identifier))

			select {
			case sub.Channel <- headEventData:
				logger.Info("event broadcasted")
			default:
				logger.Warn("subscriber channel full, dropping the message")
			}
		}
	default:
		gc.log.
			With(zap.String("topic", e.Topic)).
			Warn("unsupported event topic")
	}
}

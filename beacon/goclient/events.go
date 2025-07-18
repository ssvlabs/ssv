package goclient

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
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

	logger := gc.log.With(
		zap.Int("clients_len", len(gc.clients)),
		zap.String("topics", strings.Join(strTopics, ", ")),
		zap.Bool("is_multi_client_listener", !gc.withWeightedAttestationData),
	)

	/*
		When weighted attestation data is disabled, the method responsible for fetching attestation data
		will use a multi-client instance. It is essential that both event listening and attestation data fetching
		communicate with the same Beacon Node to ensure data consistency.

		Different clients may report varying Block Roots for the same slot as part of the event or attestation data object.
		To mitigate discrepancies, both the Event Listener and the Attestation Data Fetcher should use the same multi-client setup,
		maximizing the chances of interacting with the same Beacon Node and maintaining consistency.

		When weighted attestation data is enabled, fetching attestation data will result in selecting the data
		from the fastest Beacon Node with the highest score. In this case, the Event Listener should broadcast
		the first event received for the slot(and ignore other events for the same slot), as it will most likely
		originate from the same Beacon Node that provided the attestation data.
	*/
	logger.Info("subscribing to events")

	opts := &api.EventsOpts{
		Topics:  strTopics,
		Handler: gc.eventHandler,
	}

	if gc.withWeightedAttestationData {
		for _, client := range gc.clients {
			if err := client.Events(ctx, opts); err != nil {
				logger.Error(clResponseErrMsg, zap.String("api", "Events"), zap.Error(err))
				return err
			}
		}
	} else {
		if err := gc.multiClient.Events(ctx, opts); err != nil {
			logger.Error(clResponseErrMsg, zap.String("api", "Events"), zap.Error(err))
			return err
		}
	}

	logger.Debug("subscribed to events")

	return nil
}

func (gc *GoClient) eventHandler(e *apiv1.Event) {
	if e == nil {
		gc.log.Warn("event was nil, skipping")
		return
	}

	logger := gc.log.With(zap.String("topic", e.Topic))
	logger.Debug("event received")

	if e.Data == nil {
		logger.Warn("event data is nil")
		return
	}

	switch EventTopic(e.Topic) {
	case EventTopicHead:
		eventData, ok := e.Data.(*apiv1.HeadEvent)
		if !ok {
			logger.Warn("could not type assert")
			return
		}

		gc.lastProcessedEventSlotLock.Lock()
		if eventData.Slot <= gc.lastProcessedEventSlot {
			logger.
				With(zap.Uint64("event_slot", uint64(eventData.Slot))).
				With(zap.Uint64("last_processed_slot", uint64(gc.lastProcessedEventSlot))).
				Debug("event slot is lower or equal than last processed slot")
			gc.lastProcessedEventSlotLock.Unlock()
			return
		}

		gc.lastProcessedEventSlot = eventData.Slot
		gc.lastProcessedEventSlotLock.Unlock()

		gc.subscribersLock.RLock()
		defer gc.subscribersLock.RUnlock()
		for _, sub := range gc.headEventSubscribers {
			logger = logger.With(zap.String("subscriber_identifier", sub.Identifier))

			select {
			case sub.Channel <- eventData:
				logger.Info("event broadcasted")
			default:
				logger.Warn("subscriber channel full, dropping the message")
			}
		}
	case EventTopicBlock:
		eventData, ok := e.Data.(*apiv1.BlockEvent)
		if !ok {
			logger.Warn("could not type assert")
			return
		}

		noTTLOpt := ttlcache.WithTTL[phase0.Root, phase0.Slot](ttlcache.NoTTL)
		_, exists := gc.blockRootToSlotCache.GetOrSet(eventData.Block, eventData.Slot, noTTLOpt)
		if !exists {
			logger.
				With(fields.Slot(eventData.Slot)).
				With(fields.BlockRoot(eventData.Block)).
				Info("block root to slot cache updated")
		}
	default:
		gc.log.
			With(zap.String("topic", e.Topic)).
			Warn("unsupported event topic")
	}
}

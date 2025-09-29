package goclient

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log/fields"
)

type eventTopic string

const (
	eventTopicHead  eventTopic = "head"
	eventTopicBlock eventTopic = "block"
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

	if !slices.Contains(gc.supportedTopics, eventTopicHead) {
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

	strTopics := make([]string, 0, len(gc.supportedTopics))
	for _, topic := range gc.supportedTopics {
		strTopics = append(strTopics, string(topic))
	}

	logger := gc.log.With(
		zap.String("topics", strings.Join(strTopics, ", ")),
		zap.Bool("with_weighted_attestation_data", gc.withWeightedAttestationData),
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

	eventHandler := gc.newEventHandler()
	opts := &api.EventsOpts{
		Topics:  strTopics,
		Handler: eventHandler,
	}

	// By default, we subscribe to CL events via multiClient that only propagates events from a single client
	// (the 1st active client). If we have weighted attestations enabled, we need to process events from all
	// clients GoClient is configured with to get the most up-to-date data to score attestations correctly
	// (and if some clients are unavailable at the moment - it's not a big deal, `go-eth2-client` automatically
	// periodically resubscribes to Events).
	// There is no need to issue these calls asynchronously since they spawn their own go-routines and return fast.
	subscribeToEvents := func() error {
		reqStart := time.Now()
		err := gc.multiClient.Events(ctx, opts)
		recordRequest(ctx, logger, "Events", http.MethodGet, gc.multiClient.Address(), true, time.Since(reqStart), err)
		if err != nil {
			return errMultiClient(fmt.Errorf("request events: %w", err), "Events")
		}
		return nil
	}
	if gc.withWeightedAttestationData {
		subscribeToEvents = func() error {
			var errs error
			success := false

			for _, client := range gc.clients {
				reqStart := time.Now()
				err := client.Events(ctx, opts)
				recordRequest(ctx, logger, "Events", client.Address(), http.MethodGet, false, time.Since(reqStart), err)
				if err != nil {
					errs = errors.Join(errs, errSingleClient(fmt.Errorf("request events: %w", err), client.Address(), "Events"))
					continue
				}
				success = true
			}

			if !success {
				return fmt.Errorf("couldn't subscribe to `Events` with at least 1 client: %w", errs)
			}

			return nil
		}
	}

	if err := subscribeToEvents(); err != nil {
		return err
	}

	logger.Debug("subscribed to events")

	return nil
}

// newEventHandler creates a handler to process CL events.
// IMPORTANT: this func is called concurrently by different CL client implementations, hence the handling it
// performs must be thread-safe and idempotent (it can receive the same event multiple times).
func (gc *GoClient) newEventHandler() func(*apiv1.Event) {
	var lastProcessedEventSlotLock sync.Mutex
	var lastProcessedEventSlot phase0.Slot

	return func(e *apiv1.Event) {
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

		switch eventTopic(e.Topic) {
		case eventTopicHead:
			eventData, ok := e.Data.(*apiv1.HeadEvent)
			if !ok {
				logger.Warn("could not type assert")
				return
			}

			lastProcessedEventSlotLock.Lock()
			if eventData.Slot <= lastProcessedEventSlot {
				logger.
					With(zap.Uint64("event_slot", uint64(eventData.Slot))).
					With(zap.Uint64("last_processed_slot", uint64(lastProcessedEventSlot))).
					Debug("event slot is lower or equal than last processed slot")
				lastProcessedEventSlotLock.Unlock()
				return
			}

			lastProcessedEventSlot = eventData.Slot
			lastProcessedEventSlotLock.Unlock()

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
			return
		case eventTopicBlock:
			eventData, ok := e.Data.(*apiv1.BlockEvent)
			if !ok {
				logger.Warn("could not type assert")
				return
			}

			// Note, a block root "commits" (hashes over it) to a specific slot number - hence we can't accidentally
			// update gc.blockRootToSlotCache here overwriting the freshest slot value with a stale one regardless
			// of what order the events are processed in.
			noTTLOpt := ttlcache.WithTTL[phase0.Root, phase0.Slot](ttlcache.NoTTL)
			_, exists := gc.blockRootToSlotCache.GetOrSet(eventData.Block, eventData.Slot, noTTLOpt)
			if !exists {
				logger.
					With(fields.Slot(eventData.Slot)).
					With(fields.BlockRoot(eventData.Block)).
					Info("block root to slot cache updated")
			}
			return
		default:
			gc.log.
				With(zap.String("topic", e.Topic)).
				Warn("unsupported event topic")
			return
		}
	}
}

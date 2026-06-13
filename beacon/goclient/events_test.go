package goclient

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/jellydator/ttlcache/v3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
)

func TestSubscribeToHeadEvents(t *testing.T) {
	t.Run("Should launch event listener when go client is instantiated", func(t *testing.T) {
		eventsEndpointSubscribedCh := make(chan any)
		var subscribedTopics []string
		server := mocks.NewServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
			if strings.Contains(r.URL.Path, "/eth/v1/events") {
				queryValues := r.URL.Query()
				require.True(t, queryValues.Has("topics"))

				topics := queryValues["topics"]
				subscribedTopics = append(subscribedTopics, topics...)
				eventsEndpointSubscribedCh <- struct{}{}
			}
			return resp, nil
		})
		defer server.Close()

		client := eventsTestClient(t, server.URL)

		assert.NotNil(t, client)

		for {
			select {
			case <-eventsEndpointSubscribedCh:
				assert.Len(t, subscribedTopics, 2)
				assert.Contains(t, subscribedTopics, "block")
				assert.Contains(t, subscribedTopics, "head")
				return
			case <-time.After(time.Second * 5):
				t.Fatalf("timed out waiting for events endpoint to be subscribed")
			}
		}
	})

	t.Run("Should create subscriber", func(t *testing.T) {
		server := mocks.NewServer(nil)
		client := eventsTestClient(t, server.URL)
		defer server.Close()

		err := client.SubscribeToHeadEvents(t.Context(), "test_caller", make(chan<- *apiv1.HeadEvent))

		assert.NoError(t, err)
		assert.Len(t, client.headEventSubscribers, 1)
		sub := client.headEventSubscribers[0]
		assert.Equal(t, "test_caller", sub.Identifier)
		assert.NotNil(t, sub.Channel)
	})

	t.Run("Should not create subscriber and return error when supported topics does not contain HeadEventTopic", func(t *testing.T) {
		server := mocks.NewServer(nil)
		client := eventsTestClient(t, server.URL)
		client.supportedTopics = []eventTopic{}
		defer server.Close()

		err := client.SubscribeToHeadEvents(t.Context(), "test_caller", make(chan<- *apiv1.HeadEvent))

		assert.Error(t, err)
		assert.Equal(t, "the list of supported topics did not contain 'HeadEventTopic', cannot add new subscriber", err.Error())
		assert.Empty(t, client.headEventSubscribers)
	})
}

func TestNewEventHandler(t *testing.T) {
	t.Run("Deduplicates concurrent head events by slot", func(t *testing.T) {
		eventsCh := make(chan *apiv1.HeadEvent, 16)
		client := &GoClient{
			log:       zap.NewNop(),
			headCache: ttlcache.New[phase0.Slot, phase0.Root](),
			headEventSubscribers: []subscriber[*apiv1.HeadEvent]{
				{Identifier: "test_subscriber", Channel: eventsCh},
			},
		}
		handler := client.newEventHandler()

		initialRoot := phase0.Root{0x0A}
		nextRoot := phase0.Root{0x0B}

		handler(&apiv1.Event{
			Topic: string(eventTopicHead),
			Data: &apiv1.HeadEvent{
				Slot:  10,
				Block: initialRoot,
			},
		})

		require.Len(t, eventsCh, 1)
		initialEvent := <-eventsCh
		require.NotNil(t, initialEvent)
		require.Equal(t, phase0.Slot(10), initialEvent.Slot)

		start := make(chan struct{})
		var wg sync.WaitGroup
		for i := 0; i < 32; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				<-start

				slot := phase0.Slot(10)
				block := initialRoot
				if i%2 == 0 {
					slot = 11
					block = nextRoot
				}

				handler(&apiv1.Event{
					Topic: string(eventTopicHead),
					Data: &apiv1.HeadEvent{
						Slot:  slot,
						Block: block,
					},
				})
			}(i)
		}
		close(start)
		wg.Wait()

		require.Len(t, eventsCh, 1)
		nextEvent := <-eventsCh
		require.NotNil(t, nextEvent)
		assert.Equal(t, phase0.Slot(11), nextEvent.Slot)
		assert.Equal(t, nextRoot, nextEvent.Block)

		headAtSlot10 := client.headCache.Get(10)
		require.NotNil(t, headAtSlot10)
		assert.Equal(t, initialRoot, headAtSlot10.Value())

		headAtSlot11 := client.headCache.Get(11)
		require.NotNil(t, headAtSlot11)
		assert.Equal(t, nextRoot, headAtSlot11.Value())
	})

	t.Run("Drops lower advancing slot after a higher slot is processed", func(t *testing.T) {
		eventsCh := make(chan *apiv1.HeadEvent, 16)
		client := &GoClient{
			log:       zap.NewNop(),
			headCache: ttlcache.New[phase0.Slot, phase0.Root](),
			headEventSubscribers: []subscriber[*apiv1.HeadEvent]{
				{Identifier: "test_subscriber", Channel: eventsCh},
			},
		}
		handler := client.newEventHandler()

		initialRoot := phase0.Root{0x0A}
		midRoot := phase0.Root{0x0B}
		latestRoot := phase0.Root{0x0C}

		handler(&apiv1.Event{
			Topic: string(eventTopicHead),
			Data: &apiv1.HeadEvent{
				Slot:  10,
				Block: initialRoot,
			},
		})

		require.Len(t, eventsCh, 1)
		<-eventsCh

		handler(&apiv1.Event{
			Topic: string(eventTopicHead),
			Data: &apiv1.HeadEvent{
				Slot:  12,
				Block: latestRoot,
			},
		})
		handler(&apiv1.Event{
			Topic: string(eventTopicHead),
			Data: &apiv1.HeadEvent{
				Slot:  11,
				Block: midRoot,
			},
		})

		require.Len(t, eventsCh, 1)
		latestEvent := <-eventsCh
		require.NotNil(t, latestEvent)
		assert.Equal(t, phase0.Slot(12), latestEvent.Slot)
		assert.Equal(t, latestRoot, latestEvent.Block)

		headAtSlot10 := client.headCache.Get(10)
		require.NotNil(t, headAtSlot10)
		assert.Equal(t, initialRoot, headAtSlot10.Value())

		headAtSlot11 := client.headCache.Get(11)
		assert.Nil(t, headAtSlot11)

		headAtSlot12 := client.headCache.Get(12)
		require.NotNil(t, headAtSlot12)
		assert.Equal(t, latestRoot, headAtSlot12.Value())
	})

	t.Run("Drops head event broadcast when subscriber channel is full", func(t *testing.T) {
		eventsCh := make(chan *apiv1.HeadEvent, 1)
		client := &GoClient{
			log:       zap.NewNop(),
			headCache: ttlcache.New[phase0.Slot, phase0.Root](),
			headEventSubscribers: []subscriber[*apiv1.HeadEvent]{
				{Identifier: "test_subscriber", Channel: eventsCh},
			},
		}
		handler := client.newEventHandler()

		firstRoot := phase0.Root{0x01}
		secondRoot := phase0.Root{0x02}

		handler(&apiv1.Event{
			Topic: string(eventTopicHead),
			Data: &apiv1.HeadEvent{
				Slot:  1,
				Block: firstRoot,
			},
		})
		handler(&apiv1.Event{
			Topic: string(eventTopicHead),
			Data: &apiv1.HeadEvent{
				Slot:  2,
				Block: secondRoot,
			},
		})

		require.Len(t, eventsCh, 1)
		firstEvent := <-eventsCh
		require.NotNil(t, firstEvent)
		assert.Equal(t, phase0.Slot(1), firstEvent.Slot)
		assert.Equal(t, firstRoot, firstEvent.Block)

		droppedEvent := client.headCache.Get(2)
		require.NotNil(t, droppedEvent)
		assert.Equal(t, secondRoot, droppedEvent.Value())
	})
}

func eventsTestClient(t *testing.T, serverURL string) *GoClient {
	server, err := New(t.Context(), zap.NewNop(), Options{
		BeaconNodeAddr: serverURL,
	})
	require.NoError(t, err)

	return server
}

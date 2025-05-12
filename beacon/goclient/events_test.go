package goclient

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

func TestSubscribeToHeadEvents(t *testing.T) {
	t.Run("Should launch event listener when go client is instantiated", func(t *testing.T) {
		eventsEndpointSubscribedCh := make(chan any)
		var subscribedTopics []string
		server := tests.MockServer(func(r *http.Request, resp json.RawMessage) (json.RawMessage, error) {
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
		server := tests.MockServer(nil)
		client := eventsTestClient(t, server.URL)
		defer server.Close()

		err := client.SubscribeToHeadEvents(context.Background(), "test_caller", make(chan<- *apiv1.HeadEvent))

		assert.NoError(t, err)
		assert.Len(t, client.headEventSubscribers, 1)
		sub := client.headEventSubscribers[0]
		assert.Equal(t, "test_caller", sub.Identifier)
		assert.NotNil(t, sub.Channel)
	})

	t.Run("Should not create subscriber and return error when supported topics does not contain HeadEventTopic", func(t *testing.T) {
		server := tests.MockServer(nil)
		client := eventsTestClient(t, server.URL)
		client.supportedTopics = []EventTopic{}
		defer server.Close()

		err := client.SubscribeToHeadEvents(context.Background(), "test_caller", make(chan<- *apiv1.HeadEvent))

		assert.Error(t, err)
		assert.Equal(t, "the list of supported topics did not contain 'HeadEventTopic', cannot add new subscriber", err.Error())
		assert.Empty(t, client.headEventSubscribers)
	})
}

func eventsTestClient(t *testing.T, serverURL string) *GoClient {
	server, err := New(zap.NewNop(), Options{
		BeaconNodeAddr: serverURL,
		Context:        context.Background(),
		Network:        beacon.NewNetwork(types.MainNetwork),
	},
		tests.MockSlotTickerProvider)

	require.NoError(t, err)
	return server
}

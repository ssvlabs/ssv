package goclient

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestSubscribeToHeadEvents(t *testing.T) {
	t.Run("Should create subscriber and launch event listener when go client is instantiated", func(t *testing.T) {
		eventsEndpointSubscribedCh := make(chan any)

		server := tests.MockServer(t, func(_ http.ResponseWriter, r *http.Request) error {
			if strings.Contains(r.URL.Path, "/eth/v1/events") {
				eventsEndpointSubscribedCh <- struct{}{}
			}
			return nil
		})
		defer server.Close()

		client := eventsTestClient(t, server.URL)

		assert.NotNil(t, client)
		assert.Len(t, client.headEventSubscribers, 1)
		sub := client.headEventSubscribers[0]
		assert.Equal(t, "go_client", sub.Identifier)
		assert.NotNil(t, sub.Channel)
		assert.IsType(t, make(chan *apiv1.HeadEvent, 32), sub.Channel)

		for {
			select {
			case <-eventsEndpointSubscribedCh:
				return
			case <-time.After(time.Second * 5):
				t.Fatalf("timed out waiting for events endpoint to be subscribed")
			}
		}
	})

	t.Run("Should create subscriber", func(t *testing.T) {
		server := tests.MockServer(t, func(_ http.ResponseWriter, r *http.Request) error { return nil })
		client := eventsTestClient(t, server.URL)
		defer server.Close()

		ch, err := client.SubscribeToHeadEvents(context.Background(), "test_caller")

		assert.NoError(t, err)
		assert.Len(t, client.headEventSubscribers, 2)
		sub := client.headEventSubscribers[1]
		assert.Equal(t, "test_caller", sub.Identifier)
		assert.NotNil(t, sub.Channel)
		assert.IsType(t, make(chan *apiv1.HeadEvent, 32), sub.Channel)
		assert.IsType(t, make(<-chan *apiv1.HeadEvent, 32), ch)
	})
}

func eventsTestClient(t *testing.T, serverURL string) *GoClient {
	server, err := New(zap.NewNop(), beacon.Options{
		BeaconNodeAddr: serverURL,
		Context:        context.Background(),
	},
		tests.MockDataStore{},
		tests.MockSlotTickerProvider)

	require.NoError(t, err)
	return server
}

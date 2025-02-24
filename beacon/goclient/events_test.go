package goclient

import (
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestHeadEvents(t *testing.T) {
	t.Run("Should listen to events when go client is instantiated", func(t *testing.T) {
		eventsEndpointSubscribedCh := make(chan any)

		server := tests.MockServer(t, func(w http.ResponseWriter, r *http.Request) error {
			if strings.Contains(r.URL.Path, "/eth/v1/events") {
				w.Header().Set("Content-Type", "application/json")
				eventsEndpointSubscribedCh <- struct{}{}
			}
			return nil
		})
		client := eventsTestClient(t, server.URL)
		assert.NotNil(t, client)

		for {
			select {
			case <-eventsEndpointSubscribedCh:
				return
			case <-time.After(time.Second * 5):
				assert.Fail(t, "timed out waiting for events endpoint to be subscribed")
			}
		}
	})
}

func eventsTestClient(t *testing.T, serverURL string) *GoClient {
	server, err := New(zap.NewNop(), beacon.Options{
		BeaconNodeAddr: serverURL,
		Context:        t.Context(),
	},
		tests.MockDataStore{},
		tests.MockSlotTickerProvider)

	require.NoError(t, err)
	return server
}

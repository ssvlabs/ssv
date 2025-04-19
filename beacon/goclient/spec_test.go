package goclient

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

func TestSpec(t *testing.T) {
	ctx := context.Background()

	t.Run("success", func(t *testing.T) {
		mockServer := tests.MockServer(nil)
		defer mockServer.Close()

		client, err := New(
			zap.NewNop(),
			Options{
				Context:        ctx,
				Network:        beacon.NewNetwork(types.MainNetwork),
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
			tests.MockSlotTickerProvider,
		)
		require.NoError(t, err)

		spec, err := client.Spec(ctx)
		require.NoError(t, err)
		require.NotNil(t, spec)

		require.Equal(t, "mainnet", spec["CONFIG_NAME"])
	})
}

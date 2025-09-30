package goclient

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/beacon/goclient/mocks"
	"github.com/ssvlabs/ssv/networkconfig"
)

func Test_specForClient(t *testing.T) {
	ctx := t.Context()

	t.Run("success", func(t *testing.T) {
		mockServer := mocks.NewServer(nil)
		defer mockServer.Close()

		client, err := New(
			ctx,
			zap.NewNop(),
			Options{
				BeaconConfig:   networkconfig.TestNetwork.Beacon,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
		)
		require.NoError(t, err)

		spec, err := client.specForClient(ctx, client.multiClient)
		require.NoError(t, err)
		require.NotNil(t, spec)

		require.Equal(t, "mainnet", spec["CONFIG_NAME"])
	})
}

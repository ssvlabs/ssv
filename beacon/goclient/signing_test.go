package goclient

import (
	"encoding/hex"
	"testing"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/beacon/goclient/tests"
	"github.com/ssvlabs/ssv/networkconfig"
)

func Test_computeVoluntaryExitDomain(t *testing.T) {
	ctx := t.Context()

	t.Run("success", func(t *testing.T) {
		mockServer := tests.MockServer(nil)
		defer mockServer.Close()

		client, err := New(
			ctx,
			zap.NewNop(),
			Options{
				BeaconConfig:   networkconfig.TestNetwork.BeaconConfig,
				BeaconNodeAddr: mockServer.URL,
				CommonTimeout:  100 * time.Millisecond,
				LongTimeout:    500 * time.Millisecond,
			},
		)
		require.NoError(t, err)

		domain, err := client.computeVoluntaryExitDomain()
		require.NoError(t, err)
		require.NotNil(t, domain)

		currentForkVersion, err := hex.DecodeString("03000000")
		require.NoError(t, err)
		require.Len(t, currentForkVersion, 4)

		genesisValidatorsRoot, err := hex.DecodeString("4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95")
		require.NoError(t, err)
		require.Len(t, genesisValidatorsRoot, 32)

		forkData := &phase0.ForkData{
			CurrentVersion:        [4]byte(currentForkVersion),
			GenesisValidatorsRoot: [32]byte(genesisValidatorsRoot),
		}

		root, err := forkData.HashTreeRoot()
		require.NoError(t, err)

		require.EqualValues(t, append(spectypes.DomainVoluntaryExit[:], root[:]...), domain)
	})
}

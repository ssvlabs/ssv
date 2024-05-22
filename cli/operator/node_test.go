package operator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
)

func Test_verifyConfig(t *testing.T) {
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))

	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	testNetworkName := networkconfig.TestNetwork.Name

	t.Run("no config in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
		}
		verifyConfig(logger, nodeStorage, c.NetworkName, c.UsingLocalEvents)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has same config in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		verifyConfig(logger, nodeStorage, testNetworkName, true)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has different network name and events type in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName + "1",
			UsingLocalEvents: false,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't change network from \"testnet1\" to \"testnet\" in an existing database, it must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, true) },
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has different network name in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName + "1",
			UsingLocalEvents: true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't change network from \"testnet1\" to \"testnet\" in an existing database, it must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, true) },
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has real events in DB but runs with local events", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: false,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't switch on localevents, database must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, true) },
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has local events in DB but runs with real events", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't switch off localevents, database must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, false) },
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})
}

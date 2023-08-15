package operator

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

func Test_ensureNoConfigBreakingChanges(t *testing.T) {
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
		ensureNoConfigBreakingChanges(logger, nodeStorage, c.NetworkName, c.UsingLocalEvents)

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

		ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true)

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
			fmt.Sprintf("stored config mismatch: node was already run with a different network name, the database needs to be cleaned to switch the network, current %q, stored %q", testNetworkName, testNetworkName+"1"),
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true) },
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
			fmt.Sprintf("stored config mismatch: node was already run with a different network name, the database needs to be cleaned to switch the network, current %q, stored %q", testNetworkName, testNetworkName+"1"),
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true) },
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
			"stored config mismatch: node was already run with real events, the database needs to be cleaned to use local events",
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true) },
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
			"stored config mismatch: node was already run with local events, the database needs to be cleaned to use real events",
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, false) },
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})
}

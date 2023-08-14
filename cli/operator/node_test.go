package operator

import (
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/bloxapp/ssv/networkconfig"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

func Test_ensureNoConfigBreakingChanges(t *testing.T) {
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))

	db, err := ssvstorage.GetStorageFactory(logger, basedb.Options{
		Type: "badger-memory",
		Path: "",
	})
	require.NoError(t, err)

	nodeStorage, err := operatorstorage.NewNodeStorage(logger, db)
	require.NoError(t, err)

	testNetworkName := networkconfig.TestNetwork.Name

	t.Run("no config in DB", func(t *testing.T) {
		ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true)

		configName, found, err := nodeStorage.GetNetworkConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testNetworkName, configName)

		le, found, err := nodeStorage.GetLocalEventsConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, true, le)

		require.NoError(t, nodeStorage.DeleteNetworkConfig(nil))
		require.NoError(t, nodeStorage.DeleteLocalEventsConfig(nil))
	})

	t.Run("has same config in DB", func(t *testing.T) {
		require.NoError(t, nodeStorage.SaveNetworkConfig(nil, testNetworkName))
		require.NoError(t, nodeStorage.SaveLocalEventsConfig(nil, true))

		ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true)

		configName, found, err := nodeStorage.GetNetworkConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testNetworkName, configName)

		le, found, err := nodeStorage.GetLocalEventsConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, true, le)

		require.NoError(t, nodeStorage.DeleteNetworkConfig(nil))
		require.NoError(t, nodeStorage.DeleteLocalEventsConfig(nil))
	})

	t.Run("has different network name and events type in DB", func(t *testing.T) {
		require.NoError(t, nodeStorage.SaveNetworkConfig(nil, testNetworkName+"1"))
		require.NoError(t, nodeStorage.SaveLocalEventsConfig(nil, false))

		require.PanicsWithValue(t,
			"node was already run with a different network name, the database needs to be cleaned to switch the network",
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true) },
		)

		configName, found, err := nodeStorage.GetNetworkConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testNetworkName+"1", configName)

		le, found, err := nodeStorage.GetLocalEventsConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, false, le)

		require.NoError(t, nodeStorage.DeleteNetworkConfig(nil))
		require.NoError(t, nodeStorage.DeleteLocalEventsConfig(nil))
	})

	t.Run("has different network name in DB", func(t *testing.T) {
		require.NoError(t, nodeStorage.SaveNetworkConfig(nil, testNetworkName+"1"))
		require.NoError(t, nodeStorage.SaveLocalEventsConfig(nil, true))

		require.PanicsWithValue(t,
			"node was already run with a different network name, the database needs to be cleaned to switch the network",
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true) },
		)

		configName, found, err := nodeStorage.GetNetworkConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testNetworkName+"1", configName)

		le, found, err := nodeStorage.GetLocalEventsConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, true, le)

		require.NoError(t, nodeStorage.DeleteNetworkConfig(nil))
		require.NoError(t, nodeStorage.DeleteLocalEventsConfig(nil))
	})

	t.Run("has real events in DB but runs with local events", func(t *testing.T) {
		require.NoError(t, nodeStorage.SaveNetworkConfig(nil, testNetworkName))
		require.NoError(t, nodeStorage.SaveLocalEventsConfig(nil, false))

		require.PanicsWithValue(t,
			"node was already run with real events, the database needs to be cleaned to use local events",
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, true) },
		)

		configName, found, err := nodeStorage.GetNetworkConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testNetworkName, configName)

		le, found, err := nodeStorage.GetLocalEventsConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, false, le)

		require.NoError(t, nodeStorage.DeleteNetworkConfig(nil))
		require.NoError(t, nodeStorage.DeleteLocalEventsConfig(nil))
	})

	t.Run("has local events in DB but runs with real events", func(t *testing.T) {
		require.NoError(t, nodeStorage.SaveNetworkConfig(nil, testNetworkName))
		require.NoError(t, nodeStorage.SaveLocalEventsConfig(nil, true))

		require.PanicsWithValue(t,
			"node was already run with local events, the database needs to be cleaned to use real events",
			func() { ensureNoConfigBreakingChanges(logger, nodeStorage, testNetworkName, false) },
		)

		configName, found, err := nodeStorage.GetNetworkConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, testNetworkName, configName)

		le, found, err := nodeStorage.GetLocalEventsConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, true, le)

		require.NoError(t, nodeStorage.DeleteNetworkConfig(nil))
		require.NoError(t, nodeStorage.DeleteLocalEventsConfig(nil))
	})
}

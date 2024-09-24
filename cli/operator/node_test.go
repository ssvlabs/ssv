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
	testingVersion := "v0.0.0-test"

	t.Run("no config in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			Version:          testingVersion,
		}
		verifyConfig(logger, nodeStorage, c.NetworkName, c.UsingLocalEvents, c.Version)

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
			Version:          testingVersion,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		verifyConfig(logger, nodeStorage, testNetworkName, true, testingVersion)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has different version in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			Version:          "v0.0.0-old", // Different version in the DB
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		// Expect that the version is updated when it differs from the one in DB
		newVersion := "v0.0.0-new"
		verifyConfig(logger, nodeStorage, testNetworkName, true, newVersion)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, newVersion, storedConfig.Version) // Ensure version was updated

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has invalid version in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			Version:          "invalid-version", // Invalid version format
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		validVersion := "v0.0.0-test"
		require.Panics(t, func() {
			verifyConfig(logger, nodeStorage, testNetworkName, true, validVersion)
		})

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("version downgrade detected (failure)", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			Version:          "v2.0.0", // Stored version has a higher major version
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		// Attempt to run with a lower major version (e.g., v1.0.0)
		require.PanicsWithValue(t,
			"incompatible config change: downgrade detected. Current version v1.0.0 (major: v1) is lower than stored version v2.0.0 (major: v2)",
			func() {
				verifyConfig(logger, nodeStorage, testNetworkName, true, "v1.0.0")
			},
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("version upgrade detected (success)", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			Version:          "v1.0.0", // Stored version has a lower major version
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		// Attempt to run with a higher major version (e.g., v2.0.0)
		verifyConfig(logger, nodeStorage, testNetworkName, true, "v2.0.0")

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, "v2.0.0", storedConfig.Version) // Ensure the config was updated with the new version

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has different network name and events type in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName + "1",
			UsingLocalEvents: false,
			Version:          testingVersion,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't change network from \"testnet1\" to \"testnet\" in an existing database, it must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, true, testingVersion) },
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
			Version:          testingVersion,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't change network from \"testnet1\" to \"testnet\" in an existing database, it must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, true, testingVersion) },
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
			Version:          testingVersion,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't switch on localevents, database must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, true, testingVersion) },
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
			Version:          testingVersion,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))

		require.PanicsWithValue(t,
			"incompatible config change: can't switch off localevents, database must be removed first",
			func() { verifyConfig(logger, nodeStorage, testNetworkName, false, testingVersion) },
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})
}

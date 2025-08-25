package operator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func Test_verifyConfig(t *testing.T) {
	logger := zap.New(zapcore.NewNopCore(), zap.WithFatalHook(zapcore.WriteThenPanic))

	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	netCfg := networkconfig.TestNetwork
	nodeStorage, err := operatorstorage.NewNodeStorage(netCfg.Beacon, logger, db)
	require.NoError(t, err)

	testNetworkName := netCfg.StorageName()

	t.Run("no config in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			UsingSSVSigner:   true,
		}
		require.NoError(t, validateConfig(nodeStorage, c.NetworkName, c.UsingLocalEvents, c.UsingSSVSigner))

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
			UsingSSVSigner:   true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.NoError(t, validateConfig(nodeStorage, c.NetworkName, c.UsingLocalEvents, c.UsingSSVSigner))

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has different network name, events type, and ssv signer in DB", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName + "1",
			UsingLocalEvents: false,
			UsingSSVSigner:   false,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.ErrorContains(t,
			validateConfig(nodeStorage, testNetworkName, true, true),
			"incompatible config change: network mismatch. Stored network testnet:alan1 does not match current network testnet:alan. The database must be removed or reinitialized",
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
			UsingSSVSigner:   true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.ErrorContains(t,
			validateConfig(nodeStorage, testNetworkName, c.UsingLocalEvents, c.UsingSSVSigner),
			"incompatible config change: network mismatch. Stored network testnet:alan1 does not match current network testnet:alan. The database must be removed or reinitialized",
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
			UsingSSVSigner:   true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.ErrorContains(t,
			validateConfig(nodeStorage, c.NetworkName, true, true),
			"incompatible config change: enabling local events is not allowed. The database must be removed or reinitialized",
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
			UsingSSVSigner:   true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.ErrorContains(t,
			validateConfig(nodeStorage, c.NetworkName, false, true),
			"incompatible config change: disabling local events is not allowed. The database must be removed or reinitialized",
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has local signer in DB but runs with remote signer", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			UsingSSVSigner:   true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.ErrorContains(t,
			validateConfig(nodeStorage, c.NetworkName, true, false),
			"incompatible config change: disabling ssv-signer is not allowed. The database must be removed or reinitialized",
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("has remote signer in DB but runs with local signer", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			UsingSSVSigner:   false,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.ErrorContains(t,
			validateConfig(nodeStorage, c.NetworkName, true, true),
			"incompatible config change: enabling ssv-signer is not allowed. The database must be removed or reinitialized",
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})
}

func Test_validateProposerDelayConfig(t *testing.T) {
	t.Run("safe delay - no error", func(t *testing.T) {
		// Test with safe delays
		testCases := []time.Duration{
			0 * time.Millisecond,    // Default value
			100 * time.Millisecond,  // Small value
			300 * time.Millisecond,  // Recommended starting value
			1000 * time.Millisecond, // Exactly at the limit
		}

		for _, delay := range testCases {
			t.Run(delay.String(), func(t *testing.T) {
				// Save original config
				originalCfg := cfg
				defer func() { cfg = originalCfg }()

				// Setup test config
				cfg.ProposerDelay = delay
				cfg.AllowDangerousProposerDelay = false

				// Create logger with observer
				core, recorded := observer.New(zapcore.WarnLevel)
				logger := zap.New(core)

				// Should not return error
				err := validateProposerDelayConfig(logger)
				require.NoError(t, err)

				// Should not have any warning logs
				logs := recorded.All()
				require.Len(t, logs, 0)
			})
		}
	})

	t.Run("dangerous delay without flag - should error", func(t *testing.T) {
		testCases := []time.Duration{
			1001 * time.Millisecond, // Just over the limit
			2000 * time.Millisecond, // 2 seconds
			5000 * time.Millisecond, // 5 seconds
		}

		for _, delay := range testCases {
			t.Run(delay.String(), func(t *testing.T) {
				// Save original config
				originalCfg := cfg
				defer func() { cfg = originalCfg }()

				// Setup test config
				cfg.ProposerDelay = delay
				cfg.AllowDangerousProposerDelay = false

				// Create logger with observer to capture logs
				core, recorded := observer.New(zapcore.WarnLevel)
				logger := zap.New(core)

				// Should return error
				err := validateProposerDelayConfig(logger)
				require.Error(t, err)
				require.Contains(t, err.Error(), "ProposerDelay value")
				require.Contains(t, err.Error(), "exceeds maximum safe delay")
				require.Contains(t, err.Error(), "AllowDangerousProposerDelay")
				require.Contains(t, err.Error(), "ALLOW_DANGEROUS_PROPOSER_DELAY")

				// Should not have logged a warning
				logs := recorded.All()
				require.Len(t, logs, 0)
			})
		}
	})

	t.Run("dangerous delay with flag - should warn but pass", func(t *testing.T) {
		testCases := []time.Duration{
			1001 * time.Millisecond, // Just over the limit
			2000 * time.Millisecond, // 2 seconds
			5000 * time.Millisecond, // 5 seconds
		}

		for _, delay := range testCases {
			t.Run(delay.String(), func(t *testing.T) {
				// Save original config
				originalCfg := cfg
				defer func() { cfg = originalCfg }()

				// Setup test config
				cfg.ProposerDelay = delay
				cfg.AllowDangerousProposerDelay = true

				// Create logger with observer to capture logs
				core, recorded := observer.New(zapcore.WarnLevel)
				logger := zap.New(core)

				// Should not return error
				err := validateProposerDelayConfig(logger)
				require.NoError(t, err)

				// Should have logged a warning
				logs := recorded.All()
				require.Len(t, logs, 1)
				require.Equal(t, zapcore.WarnLevel, logs[0].Level)
				require.Contains(t, logs[0].Message, "Using dangerous ProposerDelay value")
				require.Contains(t, logs[0].Message, "may cause missed block proposals")

				// Check log fields
				fields := logs[0].ContextMap()
				require.Contains(t, fields, "proposer_delay")
				require.Contains(t, fields, "max_safe_proposer_delay")
				require.Equal(t, delay, fields["proposer_delay"])
				require.Equal(t, 1000*time.Millisecond, fields["max_safe_proposer_delay"])
			})
		}
	})
}

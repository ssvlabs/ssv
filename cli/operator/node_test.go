package operator

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/ssvsigner"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func newTestSSVSignerClient(t *testing.T, register func(mux *http.ServeMux)) *ssvsigner.Client {
	mux := http.NewServeMux()
	if register != nil {
		register(mux)
	}

	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)

	return ssvsigner.NewClient(server.URL, ssvsigner.WithLogger(zap.NewNop()))
}

func Test_warnIfSSVAPIAddressUnset(t *testing.T) {
	t.Parallel()

	t.Run("warns when address is empty", func(t *testing.T) {
		core, recorded := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)

		warnIfSSVAPIAddressUnset(logger, "", 16000)

		logs := recorded.All()
		require.Len(t, logs, 1)
		require.Equal(t, zapcore.WarnLevel, logs[0].Level)
		require.Equal(t, "SSV API address not configured; listening on all interfaces", logs[0].Message)
		require.EqualValues(t, 16000, logs[0].ContextMap()["port"])
		require.Equal(t, "SSVAPIAddress", logs[0].ContextMap()["config_key"])
		require.Equal(t, "127.0.0.1", logs[0].ContextMap()["recommended_address"])
	})

	t.Run("does not warn when address is set", func(t *testing.T) {
		core, recorded := observer.New(zapcore.WarnLevel)
		logger := zap.New(core)

		warnIfSSVAPIAddressUnset(logger, "127.0.0.1", 16000)

		require.Len(t, recorded.All(), 0)
	})
}

func Test_ssvAPIListenAddress(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name    string
		address string
		port    int
		want    string
	}{
		{
			name: "empty address listens on all interfaces",
			port: 16000,
			want: ":16000",
		},
		{
			name:    "ipv4 loopback",
			address: "127.0.0.1",
			port:    16000,
			want:    "127.0.0.1:16000",
		},
		{
			name:    "ipv6 loopback",
			address: "::1",
			port:    16000,
			want:    "[::1]:16000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := net.JoinHostPort(tc.address, strconv.Itoa(tc.port))
			require.Equal(t, tc.want, got)
		})
	}
}

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

func Test_probeRemoteNetworkKeyProtector(t *testing.T) {
	t.Run("uses remote signer encrypt and decrypt endpoints when supported", func(t *testing.T) {
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathOperatorEncrypt, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_, err = w.Write(append([]byte("encrypted:"), payload...))
				require.NoError(t, err)
			})
			mux.HandleFunc(ssvsigner.PathOperatorDecrypt, func(w http.ResponseWriter, r *http.Request) {
				require.Equal(t, http.MethodPost, r.Method)
				payload, err := io.ReadAll(r.Body)
				require.NoError(t, err)
				_, err = w.Write(bytes.TrimPrefix(payload, []byte("encrypted:")))
				require.NoError(t, err)
			})
		})

		err := probeRemoteNetworkKeyProtector(context.Background(), client)
		require.NoError(t, err)
	})

	t.Run("returns unsupported when remote signer does not support remote data protection", func(t *testing.T) {
		client := newTestSSVSignerClient(t, nil)

		err := probeRemoteNetworkKeyProtector(context.Background(), client)
		require.ErrorIs(t, err, ssvsigner.ErrOperatorDataProtectionUnsupported)
	})

	t.Run("fails instead of downgrading on transient remote signer fetch error", func(t *testing.T) {
		client := newTestSSVSignerClient(t, func(mux *http.ServeMux) {
			mux.HandleFunc(ssvsigner.PathOperatorEncrypt, func(w http.ResponseWriter, r *http.Request) {
				http.Error(w, "temporary upstream failure", http.StatusInternalServerError)
			})
		})

		err := probeRemoteNetworkKeyProtector(context.Background(), client)
		require.ErrorContains(t, err, "probe remote data protector encrypt")
		require.ErrorContains(t, err, "unexpected status: 500")
	})
}

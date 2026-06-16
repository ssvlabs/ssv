package operator

import (
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/exporter"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

const (
	testSignerEndpoint = "http://signer:9000"
	testOperatorKey    = "super-secret-operator-key"
)

func Test_config_load(t *testing.T) {
	var c config
	require.NoError(t, c.load("", "")) // both paths unset -> no-op

	require.ErrorContains(t, c.load("/nonexistent/config.yaml", ""),
		"could not read config needed for logger initialization")
	require.ErrorContains(t, c.load("", "/nonexistent/share.yaml"),
		"could not read share config needed for logger initialization")
}

// Test_resolveAndValidate_proposerDelay covers the proposer-delay advisory warning emitted by
// resolveAndValidate (validation itself is covered by Test_validateProposerDelay). A minimal
// operator signing source is set so resolveSigning passes and these cases isolate proposer-delay.
func Test_resolveAndValidate_proposerDelay(t *testing.T) {
	t.Run("dangerous delay with flag - warns with duration fields", func(t *testing.T) {
		for _, delay := range []time.Duration{1001 * time.Millisecond, 2000 * time.Millisecond, 5000 * time.Millisecond} {
			t.Run(delay.String(), func(t *testing.T) {
				core, recorded := observer.New(zapcore.WarnLevel)
				c := config{}
				c.OperatorPrivateKey = testOperatorKey
				c.ProposerDelay = delay
				c.AllowDangerousProposerDelay = true

				_, err := c.resolveAndValidate(zap.New(core))
				require.NoError(t, err)

				logs := recorded.All()
				require.Len(t, logs, 1)
				require.Equal(t, zapcore.WarnLevel, logs[0].Level)
				require.Contains(t, logs[0].Message, "Using dangerous ProposerDelay value")
				require.Contains(t, logs[0].Message, "may cause missed block proposals")

				fields := logs[0].ContextMap()
				require.Equal(t, delay, fields["proposer_delay"])
				require.Equal(t, 1000*time.Millisecond, fields["max_safe_proposer_delay"])
			})
		}
	})

	t.Run("safe delay - no warning", func(t *testing.T) {
		for _, delay := range []time.Duration{0, 300 * time.Millisecond, 1000 * time.Millisecond} {
			t.Run(delay.String(), func(t *testing.T) {
				core, recorded := observer.New(zapcore.WarnLevel)
				c := config{}
				c.OperatorPrivateKey = testOperatorKey
				c.ProposerDelay = delay

				_, err := c.resolveAndValidate(zap.New(core))
				require.NoError(t, err)
				require.Len(t, recorded.All(), 0)
			})
		}
	})

	t.Run("dangerous delay without flag - error, no warning", func(t *testing.T) {
		core, recorded := observer.New(zapcore.WarnLevel)
		c := config{}
		c.OperatorPrivateKey = testOperatorKey
		c.ProposerDelay = 2000 * time.Millisecond

		_, err := c.resolveAndValidate(zap.New(core))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum safe delay")
		require.Len(t, recorded.All(), 0)
	})
}

// Test_resolveAndValidate_signingErrorContext verifies resolveAndValidate enriches a signing
// error with the configured-source context, without exposing the private key value.
func Test_resolveAndValidate_signingErrorContext(t *testing.T) {
	c := config{}
	c.SSVSigner.Endpoint = testSignerEndpoint
	c.OperatorPrivateKey = testOperatorKey

	_, err := c.resolveAndValidate(zap.NewNop())
	require.Error(t, err)
	require.Contains(t, err.Error(), "cannot enable both remote signing")

	// start_node.go logs runNode()'s error via startupErrorLogFields — the signing-source context
	// must be preserved as queryable structured fields, without exposing the key value.
	core, recorded := observer.New(zapcore.ErrorLevel)
	zap.New(core).Error("could not start node", startupErrorLogFields(err)...)
	require.Len(t, recorded.All(), 1)

	m := recorded.All()[0].ContextMap()
	require.Equal(t, testSignerEndpoint, m["ssv_signer_endpoint"])
	require.Contains(t, m, "private_key_file")
	require.Contains(t, m, "password_file")
	require.EqualValues(t, len(testOperatorKey), m["operator_private_key_len"])
	for _, v := range m {
		require.NotEqual(t, testOperatorKey, v) // the private key value itself is never logged
	}
}

// Test_resolveAndValidate_mode verifies the operating mode is resolved into the result and that
// an invalid EXPORTER_MODE fails validation up front.
func Test_resolveAndValidate_mode(t *testing.T) {
	t.Run("non-exporter -> modeOperator", func(t *testing.T) {
		c := config{}
		c.OperatorPrivateKey = testOperatorKey
		res, err := c.resolveAndValidate(zap.NewNop())
		require.NoError(t, err)
		require.Equal(t, modeOperator, res.mode)
	})

	t.Run("exporter -> modeExporter", func(t *testing.T) {
		c := config{}
		c.ExporterOptions.Enabled = true
		res, err := c.resolveAndValidate(zap.NewNop())
		require.NoError(t, err)
		require.Equal(t, modeExporter, res.mode)
	})
}

func Test_validateProposerDelay(t *testing.T) {
	tests := []struct {
		name           string
		delay          time.Duration
		allowDangerous bool
		wantErr        bool
	}{
		{name: "zero -> ok", delay: 0, allowDangerous: false, wantErr: false},
		{name: "100ms -> ok", delay: 100 * time.Millisecond, allowDangerous: false, wantErr: false},
		{name: "300ms (recommended start) -> ok", delay: 300 * time.Millisecond, allowDangerous: false, wantErr: false},
		{name: "at limit 1000ms -> ok", delay: 1000 * time.Millisecond, allowDangerous: false, wantErr: false},
		{name: "1001ms without flag -> error", delay: 1001 * time.Millisecond, allowDangerous: false, wantErr: true},
		{name: "2000ms without flag -> error", delay: 2000 * time.Millisecond, allowDangerous: false, wantErr: true},
		{name: "5000ms without flag -> error", delay: 5000 * time.Millisecond, allowDangerous: false, wantErr: true},
		{name: "1001ms with flag -> ok", delay: 1001 * time.Millisecond, allowDangerous: true, wantErr: false},
		{name: "5000ms with flag -> ok", delay: 5000 * time.Millisecond, allowDangerous: true, wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateProposerDelay(tt.delay, tt.allowDangerous)
			if tt.wantErr {
				require.Error(t, err)
				require.Contains(t, err.Error(), "ProposerDelay value")
				require.Contains(t, err.Error(), "exceeds maximum safe delay")
				require.Contains(t, err.Error(), "AllowDangerousProposerDelay")
				require.Contains(t, err.Error(), "ALLOW_DANGEROUS_PROPOSER_DELAY")
				return
			}
			require.NoError(t, err)
		})
	}
}

func Test_validateConfig(t *testing.T) {
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
		require.NoError(t, validateConfig(nodeStorage, c.NetworkName, c.UsingLocalEvents, c.UsingSSVSigner, false))

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
		require.NoError(t, validateConfig(nodeStorage, c.NetworkName, c.UsingLocalEvents, c.UsingSSVSigner, false))

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
			validateConfig(nodeStorage, testNetworkName, true, true, false),
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
			validateConfig(nodeStorage, testNetworkName, c.UsingLocalEvents, c.UsingSSVSigner, false),
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
			validateConfig(nodeStorage, c.NetworkName, true, true, false),
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
			validateConfig(nodeStorage, c.NetworkName, false, true, false),
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
			validateConfig(nodeStorage, c.NetworkName, true, false, false),
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
			validateConfig(nodeStorage, c.NetworkName, true, true, false),
			"incompatible config change: enabling ssv-signer is not allowed. The database must be removed or reinitialized",
		)

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})

	t.Run("exporter ignores stored signer mode", func(t *testing.T) {
		c := &operatorstorage.ConfigLock{
			NetworkName:      testNetworkName,
			UsingLocalEvents: true,
			UsingSSVSigner:   true,
		}
		require.NoError(t, nodeStorage.SaveConfig(nil, c))
		require.NoError(t, validateConfig(nodeStorage, c.NetworkName, true, false, true))

		storedConfig, found, err := nodeStorage.GetConfig(nil)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, c, storedConfig)

		require.NoError(t, nodeStorage.DeleteConfig(nil))
	})
}

// Test_resolveSigning covers signing-method resolution + mutual-exclusivity validation.
func Test_resolveSigning(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config)
		wantErr string // substring; "" = no error
		wantSSV bool
		wantKS  bool
		wantPK  bool
	}{
		{name: "nothing set -> error", mutate: func(c *config) {}, wantErr: "no operator signing configured"},
		{name: "ssv-signer only", mutate: func(c *config) { c.SSVSigner.Endpoint = testSignerEndpoint }, wantSSV: true},
		{name: "keystore (both files)", mutate: func(c *config) {
			c.KeyStore.PrivateKeyFile = "pk"
			c.KeyStore.PasswordFile = "pw"
		}, wantKS: true},
		{name: "operator private key", mutate: func(c *config) { c.OperatorPrivateKey = testOperatorKey }, wantPK: true},
		{name: "keystore missing password -> error", mutate: func(c *config) { c.KeyStore.PrivateKeyFile = "pk" },
			wantErr: "both keystore and password files"},
		{name: "keystore missing key -> error", mutate: func(c *config) { c.KeyStore.PasswordFile = "pw" },
			wantErr: "both keystore and password files"},
		{name: "ssv-signer + private key -> error", mutate: func(c *config) {
			c.SSVSigner.Endpoint = testSignerEndpoint
			c.OperatorPrivateKey = testOperatorKey
		}, wantErr: "cannot enable both remote signing"},
		{name: "ssv-signer + keystore -> error", mutate: func(c *config) {
			c.SSVSigner.Endpoint = testSignerEndpoint
			c.KeyStore.PrivateKeyFile = "pk"
			c.KeyStore.PasswordFile = "pw"
		}, wantErr: "cannot enable both remote signing"},
		{name: "keystore + private key -> error", mutate: func(c *config) {
			c.KeyStore.PrivateKeyFile = "pk"
			c.KeyStore.PasswordFile = "pw"
			c.OperatorPrivateKey = testOperatorKey
		}, wantErr: "cannot enable both OperatorPrivateKey and PrivateKeyFile"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := config{}
			tt.mutate(&c)

			res, err := c.resolveSigning()
			if tt.wantErr != "" {
				require.Error(t, err)
				require.Contains(t, err.Error(), tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tt.wantSSV, res.usingSSVSigner)
			require.Equal(t, tt.wantKS, res.usingKeystore)
			require.Equal(t, tt.wantPK, res.usingPrivKey)
		})
	}
}

// Test_resolveMode covers operating-mode resolution: an enabled exporter resolves to modeExporter
// (full duty tracing), everything else to modeOperator.
func Test_resolveMode(t *testing.T) {
	require.Equal(t, modeOperator, resolveMode(exporter.Options{Enabled: false}))
	require.Equal(t, modeExporter, resolveMode(exporter.Options{Enabled: true}))
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

// Test_startupErrorLogFields verifies the consolidated startup fatal preserves the structured
// fields carried by a startupError (e.g. the ssv-signer endpoint), and degrades to just the
// error for a plain error.
func Test_startupErrorLogFields(t *testing.T) {
	t.Run("startupError fields are attached", func(t *testing.T) {
		err := startupError{
			err:    fmt.Errorf("ssv-signer unavailable"),
			fields: []zap.Field{zap.String("ssv_signer_endpoint", testSignerEndpoint)},
		}
		core, recorded := observer.New(zapcore.ErrorLevel)
		zap.New(core).Error("could not start node", startupErrorLogFields(err)...)

		m := recorded.All()[0].ContextMap()
		require.Equal(t, testSignerEndpoint, m["ssv_signer_endpoint"])
		require.Contains(t, m, "error")
	})

	t.Run("plain error -> no extra fields", func(t *testing.T) {
		core, recorded := observer.New(zapcore.ErrorLevel)
		zap.New(core).Error("could not start node", startupErrorLogFields(fmt.Errorf("boom"))...)

		m := recorded.All()[0].ContextMap()
		require.Contains(t, m, "error")
		require.NotContains(t, m, "ssv_signer_endpoint")
	})
}

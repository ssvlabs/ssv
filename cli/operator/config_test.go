package operator

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"go.uber.org/zap/zaptest/observer"
)

func TestValidateProposerDelay(t *testing.T) {
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

// Test_resolveAndValidate_proposerDelay covers the proposer-delay advisory warning emitted by
// resolveAndValidate (validation itself is covered by TestValidateProposerDelay). Signing is
// left unset + non-exporter so resolveSigning is a no-op for these cases.
func Test_resolveAndValidate_proposerDelay(t *testing.T) {
	t.Run("dangerous delay with flag - warns with duration fields", func(t *testing.T) {
		for _, delay := range []time.Duration{1001 * time.Millisecond, 2000 * time.Millisecond, 5000 * time.Millisecond} {
			t.Run(delay.String(), func(t *testing.T) {
				core, recorded := observer.New(zapcore.WarnLevel)
				c := config{}
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
		c.ProposerDelay = 2000 * time.Millisecond

		_, err := c.resolveAndValidate(zap.New(core))
		require.Error(t, err)
		require.Contains(t, err.Error(), "exceeds maximum safe delay")
		require.Len(t, recorded.All(), 0)
	})
}

// Test_resolveSigning covers signing-method resolution + mutual-exclusivity validation. On
// stage these conflicts were logger.Fatal calls (untested); they are now returned errors.
func Test_resolveSigning(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config)
		wantErr string // substring; "" = no error
		wantSSV bool
		wantKS  bool
		wantPK  bool
	}{
		{name: "nothing set -> no flags", mutate: func(c *config) {}},
		{name: "ssv-signer only", mutate: func(c *config) { c.SSVSigner.Endpoint = "http://signer:9000" }, wantSSV: true},
		{name: "keystore (both files)", mutate: func(c *config) {
			c.KeyStore.PrivateKeyFile = "pk"
			c.KeyStore.PasswordFile = "pw"
		}, wantKS: true},
		{name: "operator private key", mutate: func(c *config) { c.OperatorPrivateKey = "key" }, wantPK: true},
		{name: "keystore missing password -> error", mutate: func(c *config) { c.KeyStore.PrivateKeyFile = "pk" },
			wantErr: "both keystore and password files"},
		{name: "keystore missing key -> error", mutate: func(c *config) { c.KeyStore.PasswordFile = "pw" },
			wantErr: "both keystore and password files"},
		{name: "ssv-signer + private key -> error", mutate: func(c *config) {
			c.SSVSigner.Endpoint = "http://signer:9000"
			c.OperatorPrivateKey = "key"
		}, wantErr: "cannot enable both remote signing"},
		{name: "keystore + private key -> error", mutate: func(c *config) {
			c.KeyStore.PrivateKeyFile = "pk"
			c.KeyStore.PasswordFile = "pw"
			c.OperatorPrivateKey = "key"
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

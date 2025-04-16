package ssvsigner

import (
	"crypto/tls"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestClientConfig_HasTLSConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		config   ClientTLSConfig
		expected bool
	}{
		{
			name:     "Empty config",
			config:   ClientTLSConfig{},
			expected: false,
		},
		{
			name: "With client cert only",
			config: ClientTLSConfig{
				ClientCertFile: "cert.pem",
			},
			expected: true,
		},
		{
			name: "With client key only",
			config: ClientTLSConfig{
				ClientKeyFile: "key.pem",
			},
			expected: true,
		},
		{
			name: "With CA cert only",
			config: ClientTLSConfig{
				ClientCACertFile: "ca.pem",
			},
			expected: true,
		},
		{
			name: "With insecure skip verify only",
			config: ClientTLSConfig{
				ClientInsecureSkipVerify: true,
			},
			expected: true,
		},
		{
			name: "With all options",
			config: ClientTLSConfig{
				ClientCertFile:           "cert.pem",
				ClientKeyFile:            "key.pem",
				ClientCACertFile:         "ca.pem",
				ClientInsecureSkipVerify: true,
			},
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := tt.config.HasTLSConfig()
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestCreateTLSConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		clientCert         []byte
		clientKey          []byte
		caCert             []byte
		insecureSkipVerify bool
		wantErr            bool
		checkFunc          func(*testing.T, *tls.Config)
	}{
		{
			name:    "Empty config",
			wantErr: false,
			checkFunc: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(tls.VersionTLS13), cfg.MinVersion)
				assert.Empty(t, cfg.Certificates)
				assert.Nil(t, cfg.RootCAs)
				assert.False(t, cfg.InsecureSkipVerify)
			},
		},
		{
			name:               "With insecure skip verify",
			insecureSkipVerify: true,
			wantErr:            false,
			checkFunc: func(t *testing.T, cfg *tls.Config) {
				assert.True(t, cfg.InsecureSkipVerify)
			},
		},
		{
			name:       "With invalid client cert",
			clientCert: []byte("invalid cert"),
			clientKey:  []byte("some key"),
			wantErr:    true,
		},
		{
			name:    "With invalid CA cert",
			caCert:  []byte("invalid CA cert"),
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := createTLSConfig(tt.clientCert, tt.clientKey, tt.caCert, tt.insecureSkipVerify)

			if tt.wantErr {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tt.checkFunc != nil {
				tt.checkFunc(t, cfg)
			}
		})
	}
}

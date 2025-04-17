package tls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/tls/testingutils"
)

func TestCreateClientConfig(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "tls-client-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	caCert, _, clientCert, clientKey := testingutils.GenerateCertificates(t, "localhost")

	caCertFile := filepath.Join(tempDir, "ca.crt")
	clientCertFile := filepath.Join(tempDir, "client.crt")
	clientKeyFile := filepath.Join(tempDir, "client.key")

	require.NoError(t, os.WriteFile(caCertFile, caCert, 0600))
	require.NoError(t, os.WriteFile(clientCertFile, clientCert, 0600))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKey, 0600))

	time.Sleep(10 * time.Millisecond)

	testCases := []struct {
		name              string
		config            ClientConfig
		expectError       bool
		validateTLSConfig func(*testing.T, *tls.Config)
	}{
		{
			name: "valid client config with all certificates",
			config: ClientConfig{
				ClientCertFile:           clientCertFile,
				ClientKeyFile:            clientKeyFile,
				ClientCACertFile:         caCertFile,
				ClientInsecureSkipVerify: false,
			},
			expectError: false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name: "valid client config without ca cert",
			config: ClientConfig{
				ClientCertFile:           clientCertFile,
				ClientKeyFile:            clientKeyFile,
				ClientCACertFile:         "",
				ClientInsecureSkipVerify: false,
			},
			expectError: false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.Nil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name: "valid client config with insecure skip verify",
			config: ClientConfig{
				ClientCertFile:           clientCertFile,
				ClientKeyFile:            clientKeyFile,
				ClientCACertFile:         caCertFile,
				ClientInsecureSkipVerify: true,
			},
			expectError: false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.True(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name: "invalid client config with cert but no key",
			config: ClientConfig{
				ClientCertFile:           clientCertFile,
				ClientKeyFile:            "",
				ClientCACertFile:         caCertFile,
				ClientInsecureSkipVerify: false,
			},
			expectError:       true,
			validateTLSConfig: nil,
		},
		{
			name: "invalid client config with key but no cert",
			config: ClientConfig{
				ClientCertFile:           "",
				ClientKeyFile:            clientKeyFile,
				ClientCACertFile:         caCertFile,
				ClientInsecureSkipVerify: false,
			},
			expectError:       true,
			validateTLSConfig: nil,
		},
		{
			name: "invalid ca cert file",
			config: ClientConfig{
				ClientCertFile:           clientCertFile,
				ClientKeyFile:            clientKeyFile,
				ClientCACertFile:         filepath.Join(tempDir, "nonexistent.crt"),
				ClientInsecureSkipVerify: false,
			},
			expectError:       true,
			validateTLSConfig: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tlsConfig, err := CreateClientConfig(tc.config)

			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, tlsConfig)

			if tc.validateTLSConfig != nil {
				tc.validateTLSConfig(t, tlsConfig)
			}
		})
	}
}

func TestClientConfig_HasConfig(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name     string
		config   ClientConfig
		expected bool
	}{
		{
			name:     "empty config",
			config:   ClientConfig{},
			expected: false,
		},
		{
			name: "only cert file",
			config: ClientConfig{
				ClientCertFile: "cert.pem",
			},
			expected: true,
		},
		{
			name: "only key file",
			config: ClientConfig{
				ClientKeyFile: "key.pem",
			},
			expected: true,
		},
		{
			name: "only ca cert file",
			config: ClientConfig{
				ClientCACertFile: "ca.pem",
			},
			expected: true,
		},
		{
			name: "only insecure skip verify",
			config: ClientConfig{
				ClientInsecureSkipVerify: true,
			},
			expected: true,
		},
		{
			name: "all fields set",
			config: ClientConfig{
				ClientCertFile:           "cert.pem",
				ClientKeyFile:            "key.pem",
				ClientCACertFile:         "ca.pem",
				ClientInsecureSkipVerify: true,
			},
			expected: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, tc.expected, tc.config.HasConfig())
		})
	}
}

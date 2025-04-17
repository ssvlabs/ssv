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

func TestCreateServerConfig(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "tls-server-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	caCert, _, serverCert, serverKey := testingutils.GenerateCertificates(t, "localhost")

	caCertFile := filepath.Join(tempDir, "ca.crt")
	serverCertFile := filepath.Join(tempDir, "server.crt")
	serverKeyFile := filepath.Join(tempDir, "server.key")

	require.NoError(t, os.WriteFile(caCertFile, caCert, 0600))
	require.NoError(t, os.WriteFile(serverCertFile, serverCert, 0600))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKey, 0600))

	time.Sleep(10 * time.Millisecond)

	testCases := []struct {
		name              string
		config            ServerConfig
		expectError       bool
		validateTLSConfig func(*testing.T, *tls.Config)
	}{
		{
			name: "valid server config with all certificates",
			config: ServerConfig{
				ServerCertFile:           serverCertFile,
				ServerKeyFile:            serverKeyFile,
				ServerCACertFile:         caCertFile,
				ServerInsecureSkipVerify: false,
			},
			expectError: false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.ClientCAs)
				assert.Equal(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name: "valid server config without ca cert",
			config: ServerConfig{
				ServerCertFile:           serverCertFile,
				ServerKeyFile:            serverKeyFile,
				ServerCACertFile:         "",
				ServerInsecureSkipVerify: false,
			},
			expectError: false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.Nil(t, config.ClientCAs)
				assert.NotEqual(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name: "valid server config with insecure skip verify",
			config: ServerConfig{
				ServerCertFile:           serverCertFile,
				ServerKeyFile:            serverKeyFile,
				ServerCACertFile:         caCertFile,
				ServerInsecureSkipVerify: true,
			},
			expectError: false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.True(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.ClientCAs)
				assert.Equal(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name: "invalid server config with cert but no key",
			config: ServerConfig{
				ServerCertFile:           serverCertFile,
				ServerKeyFile:            "",
				ServerCACertFile:         caCertFile,
				ServerInsecureSkipVerify: false,
			},
			expectError:       true,
			validateTLSConfig: nil,
		},
		{
			name: "invalid server config with key but no cert",
			config: ServerConfig{
				ServerCertFile:           "",
				ServerKeyFile:            serverKeyFile,
				ServerCACertFile:         caCertFile,
				ServerInsecureSkipVerify: false,
			},
			expectError:       true,
			validateTLSConfig: nil,
		},
		{
			name: "invalid ca cert file",
			config: ServerConfig{
				ServerCertFile:           serverCertFile,
				ServerKeyFile:            serverKeyFile,
				ServerCACertFile:         filepath.Join(tempDir, "nonexistent.crt"),
				ServerInsecureSkipVerify: false,
			},
			expectError:       true,
			validateTLSConfig: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tlsConfig, err := CreateServerConfig(tc.config)

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

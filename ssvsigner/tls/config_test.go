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

func TestCreateConfigFromFiles(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "tls-config-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	caCert, _, serverCert, serverKey := testingutils.GenerateCertificates(t, "localhost")

	caCertFile := filepath.Join(tempDir, "ca.crt")
	serverCertFile := filepath.Join(tempDir, "server.crt")
	serverKeyFile := filepath.Join(tempDir, "server.key")
	nonExistentFile := filepath.Join(tempDir, "nonexistent.crt")

	require.NoError(t, os.WriteFile(caCertFile, caCert, 0600))
	require.NoError(t, os.WriteFile(serverCertFile, serverCert, 0600))
	require.NoError(t, os.WriteFile(serverKeyFile, serverKey, 0600))

	time.Sleep(10 * time.Millisecond)

	testCases := []struct {
		name               string
		configType         ConfigType
		certFile           string
		keyFile            string
		caFile             string
		insecureSkipVerify bool
		expectError        bool
		validateTLSConfig  func(*testing.T, *tls.Config)
	}{
		{
			name:               "valid client config with all certificates",
			configType:         ClientConfigType,
			certFile:           serverCertFile,
			keyFile:            serverKeyFile,
			caFile:             caCertFile,
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid server config with all certificates",
			configType:         ServerConfigType,
			certFile:           serverCertFile,
			keyFile:            serverKeyFile,
			caFile:             caCertFile,
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.ClientCAs)
				assert.Equal(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid client config without ca cert",
			configType:         ClientConfigType,
			certFile:           serverCertFile,
			keyFile:            serverKeyFile,
			caFile:             "",
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.Nil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid server config without ca cert",
			configType:         ServerConfigType,
			certFile:           serverCertFile,
			keyFile:            serverKeyFile,
			caFile:             "",
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.Nil(t, config.ClientCAs)
				assert.NotEqual(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid client config with insecure skip verify",
			configType:         ClientConfigType,
			certFile:           serverCertFile,
			keyFile:            serverKeyFile,
			caFile:             caCertFile,
			insecureSkipVerify: true,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.True(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "invalid config with cert but no key",
			configType:         ClientConfigType,
			certFile:           serverCertFile,
			keyFile:            "",
			caFile:             caCertFile,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid config with key but no cert",
			configType:         ClientConfigType,
			certFile:           "",
			keyFile:            serverKeyFile,
			caFile:             caCertFile,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid ca cert file",
			configType:         ClientConfigType,
			certFile:           serverCertFile,
			keyFile:            serverKeyFile,
			caFile:             nonExistentFile,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid cert file",
			configType:         ClientConfigType,
			certFile:           nonExistentFile,
			keyFile:            serverKeyFile,
			caFile:             caCertFile,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid key file",
			configType:         ClientConfigType,
			certFile:           serverCertFile,
			keyFile:            nonExistentFile,
			caFile:             caCertFile,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			tlsConfig, err := CreateConfigFromFiles(
				tc.configType,
				tc.certFile,
				tc.keyFile,
				tc.caFile,
				tc.insecureSkipVerify,
			)

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

func TestCreateConfig(t *testing.T) {
	t.Parallel()

	caCert, _, serverCert, serverKey := testingutils.GenerateCertificates(t, "localhost")

	testCases := []struct {
		name               string
		configType         ConfigType
		cert               []byte
		key                []byte
		caCert             []byte
		insecureSkipVerify bool
		expectError        bool
		validateTLSConfig  func(*testing.T, *tls.Config)
	}{
		{
			name:               "valid client config with all certificates",
			configType:         ClientConfigType,
			cert:               serverCert,
			key:                serverKey,
			caCert:             caCert,
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid server config with all certificates",
			configType:         ServerConfigType,
			cert:               serverCert,
			key:                serverKey,
			caCert:             caCert,
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.ClientCAs)
				assert.Equal(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid client config without ca cert",
			configType:         ClientConfigType,
			cert:               serverCert,
			key:                serverKey,
			caCert:             nil,
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.Nil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid server config without ca cert",
			configType:         ServerConfigType,
			cert:               serverCert,
			key:                serverKey,
			caCert:             nil,
			insecureSkipVerify: false,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.False(t, config.InsecureSkipVerify)
				assert.Nil(t, config.ClientCAs)
				assert.NotEqual(t, tls.RequireAndVerifyClientCert, config.ClientAuth)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "valid client config with insecure skip verify",
			configType:         ClientConfigType,
			cert:               serverCert,
			key:                serverKey,
			caCert:             caCert,
			insecureSkipVerify: true,
			expectError:        false,
			validateTLSConfig: func(t *testing.T, config *tls.Config) {
				assert.Equal(t, uint16(0x304), config.MinVersion)
				assert.True(t, config.InsecureSkipVerify)
				assert.NotNil(t, config.RootCAs)
				assert.Len(t, config.Certificates, 1)
			},
		},
		{
			name:               "invalid config with cert but no key",
			configType:         ClientConfigType,
			cert:               serverCert,
			key:                nil,
			caCert:             caCert,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid config with key but no cert",
			configType:         ClientConfigType,
			cert:               nil,
			key:                serverKey,
			caCert:             caCert,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid cert data",
			configType:         ClientConfigType,
			cert:               []byte("invalid cert data"),
			key:                serverKey,
			caCert:             caCert,
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
		{
			name:               "invalid ca cert data",
			configType:         ClientConfigType,
			cert:               serverCert,
			key:                serverKey,
			caCert:             []byte("invalid CA cert data"),
			insecureSkipVerify: false,
			expectError:        true,
			validateTLSConfig:  nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tlsConfig, err := CreateConfig(
				tc.configType,
				tc.cert,
				tc.key,
				tc.caCert,
				tc.insecureSkipVerify,
			)

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

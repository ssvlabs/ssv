package tls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/tls/testingutils"
)

func TestLoadTLSConfig(t *testing.T) {
	caCert, _, serverCert, serverKey := testingutils.GenerateCertificates(t, "localhost")

	tempDir, err := os.MkdirTemp("", "tls-test-*")
	require.NoError(t, err, "Failed to create temp directory")
	defer os.RemoveAll(tempDir)

	certPath := filepath.Join(tempDir, "cert.pem")
	keyPath := filepath.Join(tempDir, "key.pem")
	caPath := filepath.Join(tempDir, "ca.pem")

	require.NoError(t, os.WriteFile(certPath, serverCert, 0600), "Failed to write server cert")
	require.NoError(t, os.WriteFile(keyPath, serverKey, 0600), "Failed to write server key")
	require.NoError(t, os.WriteFile(caPath, caCert, 0600), "Failed to write CA cert")

	invalidCertPath := filepath.Join(tempDir, "invalid-cert.pem")
	invalidKeyPath := filepath.Join(tempDir, "invalid-key.pem")
	invalidCAPath := filepath.Join(tempDir, "invalid-ca.pem")

	require.NoError(t, os.WriteFile(invalidCertPath, []byte("invalid cert"), 0600), "Failed to write invalid cert")
	require.NoError(t, os.WriteFile(invalidKeyPath, []byte("invalid key"), 0600), "Failed to write invalid key")
	require.NoError(t, os.WriteFile(invalidCAPath, []byte("invalid CA"), 0600), "Failed to write invalid CA")

	testCases := []struct {
		name              string
		certPath          string
		keyPath           string
		caPath            string
		requireClientCert bool
		expectError       bool
		checkConfig       func(*testing.T, *tls.Config)
	}{
		{
			name:        "empty paths",
			certPath:    "",
			keyPath:     "",
			caPath:      "",
			expectError: false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(minimumTLSVersion), cfg.MinVersion)
				assert.Empty(t, cfg.Certificates)
				assert.Nil(t, cfg.RootCAs)
				assert.Nil(t, cfg.ClientCAs)
			},
		},
		{
			name:        "valid cert and key",
			certPath:    certPath,
			keyPath:     keyPath,
			caPath:      "",
			expectError: false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(minimumTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
				assert.Nil(t, cfg.RootCAs)
				assert.Nil(t, cfg.ClientCAs)
			},
		},
		{
			name:        "valid cert, key, and CA without client auth",
			certPath:    certPath,
			keyPath:     keyPath,
			caPath:      caPath,
			expectError: false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(minimumTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
				assert.NotNil(t, cfg.RootCAs)
				assert.Nil(t, cfg.ClientCAs)
			},
		},
		{
			name:              "valid cert, key, and CA with client auth",
			certPath:          certPath,
			keyPath:           keyPath,
			caPath:            caPath,
			requireClientCert: true,
			expectError:       false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(minimumTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
				assert.Nil(t, cfg.RootCAs)
				assert.NotNil(t, cfg.ClientCAs)
				assert.Equal(t, tls.RequireAndVerifyClientCert, cfg.ClientAuth)
			},
		},
		{
			name:        "invalid cert path",
			certPath:    "/nonexistent/cert.pem",
			keyPath:     keyPath,
			caPath:      "",
			expectError: true,
		},
		{
			name:        "invalid key path",
			certPath:    certPath,
			keyPath:     "/nonexistent/key.pem",
			caPath:      "",
			expectError: true,
		},
		{
			name:        "invalid CA path",
			certPath:    certPath,
			keyPath:     keyPath,
			caPath:      "/nonexistent/ca.pem",
			expectError: true,
		},
		{
			name:        "invalid cert format",
			certPath:    invalidCertPath,
			keyPath:     keyPath,
			caPath:      "",
			expectError: true,
		},
		{
			name:        "invalid key format",
			certPath:    certPath,
			keyPath:     invalidKeyPath,
			caPath:      "",
			expectError: true,
		},
		{
			name:        "invalid CA format",
			certPath:    certPath,
			keyPath:     keyPath,
			caPath:      invalidCAPath,
			expectError: true,
		},
		{
			name:        "cert without key",
			certPath:    certPath,
			keyPath:     "",
			caPath:      "",
			expectError: false,
		},
		{
			name:        "key without cert",
			certPath:    "",
			keyPath:     keyPath,
			caPath:      "",
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cfg, err := LoadTLSConfig(tc.certPath, tc.keyPath, tc.caPath, tc.requireClientCert)

			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)
			require.NotNil(t, cfg)

			if tc.checkConfig != nil {
				tc.checkConfig(t, cfg)
			}
		})
	}
}

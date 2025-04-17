package tls

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/ssvsigner/tls/testingutils"
)

func TestLoadCertificatesFromFiles(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "tls-utils-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	caCert, _, clientCert, clientKey := testingutils.GenerateCertificates(t, "localhost")

	caCertFile := filepath.Join(tempDir, "ca.crt")
	clientCertFile := filepath.Join(tempDir, "client.crt")
	clientKeyFile := filepath.Join(tempDir, "client.key")
	nonExistentFile := filepath.Join(tempDir, "nonexistent.crt")

	require.NoError(t, os.WriteFile(caCertFile, caCert, 0600))
	require.NoError(t, os.WriteFile(clientCertFile, clientCert, 0600))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKey, 0600))

	time.Sleep(10 * time.Millisecond)

	testCases := []struct {
		name          string
		certFile      string
		keyFile       string
		caCertFile    string
		expectError   bool
		validateCerts func(*testing.T, []byte, []byte, []byte)
	}{
		{
			name:        "valid certificate files",
			certFile:    clientCertFile,
			keyFile:     clientKeyFile,
			caCertFile:  caCertFile,
			expectError: false,
			validateCerts: func(t *testing.T, cert, key, loadedCaCert []byte) {
				assert.NotNil(t, cert)
				assert.NotNil(t, key)
				assert.NotNil(t, caCert)
				assert.Equal(t, clientCert, cert)
				assert.Equal(t, clientKey, key)
				assert.Equal(t, caCert, loadedCaCert)
			},
		},
		{
			name:        "valid certificate files without ca",
			certFile:    clientCertFile,
			keyFile:     clientKeyFile,
			caCertFile:  "",
			expectError: false,
			validateCerts: func(t *testing.T, cert, key, caCert []byte) {
				assert.NotNil(t, cert)
				assert.NotNil(t, key)
				assert.Nil(t, caCert)
				assert.Equal(t, clientCert, cert)
				assert.Equal(t, clientKey, key)
			},
		},
		{
			name:        "only ca certificate",
			certFile:    "",
			keyFile:     "",
			caCertFile:  caCertFile,
			expectError: false,
			validateCerts: func(t *testing.T, cert, key, loadedCaCert []byte) {
				assert.Nil(t, cert)
				assert.Nil(t, key)
				assert.NotNil(t, loadedCaCert)
				assert.Equal(t, caCert, loadedCaCert)
			},
		},
		{
			name:          "certificate without key",
			certFile:      clientCertFile,
			keyFile:       "",
			caCertFile:    caCertFile,
			expectError:   true,
			validateCerts: nil,
		},
		{
			name:          "key without certificate",
			certFile:      "",
			keyFile:       clientKeyFile,
			caCertFile:    caCertFile,
			expectError:   true,
			validateCerts: nil,
		},
		{
			name:          "non-existent certificate file",
			certFile:      nonExistentFile,
			keyFile:       clientKeyFile,
			caCertFile:    caCertFile,
			expectError:   true,
			validateCerts: nil,
		},
		{
			name:          "non-existent key file",
			certFile:      clientCertFile,
			keyFile:       nonExistentFile,
			caCertFile:    caCertFile,
			expectError:   true,
			validateCerts: nil,
		},
		{
			name:          "non-existent ca certificate file",
			certFile:      clientCertFile,
			keyFile:       clientKeyFile,
			caCertFile:    nonExistentFile,
			expectError:   true,
			validateCerts: nil,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cert, key, caCert, err := LoadCertificatesFromFiles(tc.certFile, tc.keyFile, tc.caCertFile)

			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err)

			if tc.validateCerts != nil {
				tc.validateCerts(t, cert, key, caCert)
			}
		})
	}
}

func TestLoadCertificateAndKey(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "tls-utils-loadcert-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	_, _, clientCert, clientKey := testingutils.GenerateCertificates(t, "localhost")

	clientCertFile := filepath.Join(tempDir, "client.crt")
	clientKeyFile := filepath.Join(tempDir, "client.key")
	invalidCertFile := filepath.Join(tempDir, "invalid.crt")
	nonExistentFile := filepath.Join(tempDir, "nonexistent.crt")

	require.NoError(t, os.WriteFile(clientCertFile, clientCert, 0600))
	require.NoError(t, os.WriteFile(clientKeyFile, clientKey, 0600))
	require.NoError(t, os.WriteFile(invalidCertFile, []byte("invalid certificate data"), 0600))

	time.Sleep(10 * time.Millisecond)

	testLoadCertificateAndKey := func(certFile, keyFile string) error {
		_, err := loadCertificateAndKey(certFile, keyFile)
		return err
	}

	testCases := []struct {
		name        string
		certFile    string
		keyFile     string
		expectError bool
	}{
		{
			name:        "valid certificate and key",
			certFile:    clientCertFile,
			keyFile:     clientKeyFile,
			expectError: false,
		},
		{
			name:        "non-existent certificate file",
			certFile:    nonExistentFile,
			keyFile:     clientKeyFile,
			expectError: true,
		},
		{
			name:        "non-existent key file",
			certFile:    clientCertFile,
			keyFile:     nonExistentFile,
			expectError: true,
		},
		{
			name:        "invalid certificate data",
			certFile:    invalidCertFile,
			keyFile:     clientKeyFile,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			err := testLoadCertificateAndKey(tc.certFile, tc.keyFile)

			if tc.expectError {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

func TestLoadCertFile(t *testing.T) {
	t.Parallel()

	tempDir, err := os.MkdirTemp("", "tls-utils-loadcertfile-test-*")
	require.NoError(t, err)
	defer os.RemoveAll(tempDir)

	caCert, _, _, _ := testingutils.GenerateCertificates(t, "localhost")

	caCertFile := filepath.Join(tempDir, "ca.crt")
	emptyFile := filepath.Join(tempDir, "empty.crt")
	nonExistentFile := filepath.Join(tempDir, "nonexistent.crt")

	require.NoError(t, os.WriteFile(caCertFile, caCert, 0600))
	require.NoError(t, os.WriteFile(emptyFile, []byte{}, 0600))

	time.Sleep(10 * time.Millisecond)

	testCases := []struct {
		name        string
		filePath    string
		expectError bool
		expectData  []byte
	}{
		{
			name:        "valid certificate file",
			filePath:    caCertFile,
			expectError: false,
			expectData:  caCert,
		},
		{
			name:        "empty path",
			filePath:    "",
			expectError: false,
			expectData:  nil,
		},
		{
			name:        "non-existent file",
			filePath:    nonExistentFile,
			expectError: true,
			expectData:  nil,
		},
		{
			name:        "empty file",
			filePath:    emptyFile,
			expectError: false,
			expectData:  []byte{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			data, err := loadCertFile(tc.filePath)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)

			if tc.filePath == "" {
				assert.Nil(t, data)
			} else {
				assert.NotNil(t, data)
				assert.Equal(t, tc.expectData, data)
			}
		})
	}
}

package tls

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestConfigValidateServerTLS tests the Config.ValidateServerTLS method.
func TestConfigValidateServerTLS(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	serverKeystoreFile := filepath.Join(tempDir, "server-keystore.p12")
	passwordFile := filepath.Join(tempDir, "server-password.txt")

	testCases := []struct {
		name          string
		config        Config
		expectError   bool
		errorContains string
	}{
		{
			name: "valid config with keystore and password file",
			config: Config{
				ServerKeystoreFile:         serverKeystoreFile,
				ServerKeystorePasswordFile: passwordFile,
			},
			expectError: false,
		},
		{
			name: "missing password file",
			config: Config{
				ServerKeystoreFile: serverKeystoreFile,
			},
			expectError:   true,
			errorContains: "server keystore password file is required",
		},
		{
			name:   "no tls configuration",
			config: Config{
				// Empty config - no TLS
			},
			expectError: false,
		},
		{
			name: "only known clients file",
			config: Config{
				ServerKnownClientsFile: filepath.Join(tempDir, "known-clients.txt"),
			},
			expectError:   true,
			errorContains: "server keystore file is required when specifying known clients file",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.config.ValidateServerTLS()

			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestConfigValidateClientTLS tests the Config.ValidateClientTLS method.
func TestConfigValidateClientTLS(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	clientKeystoreFile := filepath.Join(tempDir, "client-keystore.p12")
	passwordFile := filepath.Join(tempDir, "client-password.txt")
	knownServersFile := filepath.Join(tempDir, "known-servers.txt")

	testCases := []struct {
		name          string
		config        Config
		expectError   bool
		errorContains string
	}{
		{
			name: "valid config with keystore and password file",
			config: Config{
				ClientKeystoreFile:         clientKeystoreFile,
				ClientKeystorePasswordFile: passwordFile,
			},
			expectError: false,
		},
		{
			name: "missing password file",
			config: Config{
				ClientKeystoreFile: clientKeystoreFile,
			},
			expectError:   true,
			errorContains: "client keystore password file is required",
		},
		{
			name: "only known servers",
			config: Config{
				ClientKnownServersFile: knownServersFile,
			},
			expectError: false,
		},
		{
			name:        "no tls configuration",
			config:      Config{},
			expectError: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			err := tc.config.ValidateClientTLS()

			if tc.expectError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), tc.errorContains)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestLoadClientConfig tests the LoadClientConfig function.
func TestLoadClientConfig(t *testing.T) {
	t.Parallel()

	// Create a valid certificate for testing (with non-empty Certificate array)
	validCert := tls.Certificate{
		Certificate: [][]byte{[]byte("test certificate data")},
		PrivateKey:  nil,
	}

	testCases := []struct {
		name                string
		certificate         tls.Certificate
		trustedFingerprints map[string]string
		expectError         bool
		checkConfig         func(*testing.T, *tls.Config)
	}{
		{
			name:        "empty config",
			expectError: false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(MinTLSVersion), cfg.MinVersion)
				assert.Empty(t, cfg.Certificates)
				assert.False(t, cfg.InsecureSkipVerify)
			},
		},
		{
			name:        "with cert",
			certificate: validCert,
			expectError: false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(MinTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
			},
		},
		{
			name:                "with fingerprints",
			trustedFingerprints: map[string]string{"localhost": "0011223344556677889900aabbccddeeff"},
			expectError:         false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(MinTLSVersion), cfg.MinVersion)
				assert.Empty(t, cfg.Certificates)
				assert.NotNil(t, cfg.VerifyConnection)
			},
		},
		{
			name:                "with cert and fingerprints",
			certificate:         validCert,
			trustedFingerprints: map[string]string{"localhost": "0011223344556677889900aabbccddeeff"},
			expectError:         false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(MinTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
				assert.NotNil(t, cfg.VerifyConnection)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := LoadClientConfig(tc.certificate, tc.trustedFingerprints)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			require.NotNil(t, cfg)

			if tc.checkConfig != nil {
				tc.checkConfig(t, cfg)
			}
		})
	}
}

// TestLoadServerConfig tests the LoadServerConfig function.
func TestLoadServerConfig(t *testing.T) {
	t.Parallel()

	// Create a valid certificate for testing (with non-empty Certificate array)
	validCert := tls.Certificate{
		Certificate: [][]byte{[]byte("test certificate data")},
		PrivateKey:  nil,
	}

	testCases := []struct {
		name                string
		certificate         tls.Certificate
		trustedFingerprints map[string]string
		expectError         bool
		checkConfig         func(*testing.T, *tls.Config)
	}{
		{
			name:        "no certificate",
			certificate: tls.Certificate{},
			expectError: true,
		},
		{
			name:        "with cert only",
			certificate: validCert,
			expectError: false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(MinTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
				assert.Equal(t, tls.NoClientCert, cfg.ClientAuth)
			},
		},
		{
			name:                "with cert and fingerprints",
			certificate:         validCert,
			trustedFingerprints: map[string]string{"client": "0011223344556677889900aabbccddeeff"},
			expectError:         false,
			checkConfig: func(t *testing.T, cfg *tls.Config) {
				assert.Equal(t, uint16(MinTLSVersion), cfg.MinVersion)
				assert.Len(t, cfg.Certificates, 1)
				assert.Equal(t, tls.RequireAnyClientCert, cfg.ClientAuth)
				assert.NotNil(t, cfg.VerifyPeerCertificate)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cfg, err := LoadServerConfig(tc.certificate, tc.trustedFingerprints)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			assert.NoError(t, err)
			require.NotNil(t, cfg)

			if tc.checkConfig != nil {
				tc.checkConfig(t, cfg)
			}
		})
	}
}

// TestLoadKeystoreCertificate tests the loadKeystoreCertificate function.
func TestLoadKeystoreCertificate(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create a dummy file with some invalid data
	invalidKeystoreFile := filepath.Join(tempDir, "invalid-keystore.p12")
	err := os.WriteFile(invalidKeystoreFile, []byte("not a valid PKCS12 file"), 0o600)
	require.NoError(t, err, "Failed to write invalid keystore file")

	testCases := []struct {
		name          string
		keystoreFile  string
		password      string
		expectedError string
	}{
		{
			name:          "non-existent keystore file",
			keystoreFile:  filepath.Join(tempDir, "non-existent.p12"),
			password:      "testpassword",
			expectedError: "read keystore file",
		},
		{
			name:          "invalid keystore file format",
			keystoreFile:  invalidKeystoreFile,
			password:      "testpassword",
			expectedError: "decode PKCS12 keystore",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			_, err := loadKeystoreCertificate(tc.keystoreFile, tc.password)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.expectedError)
		})
	}
}

// TestLoadFingerprintsFile tests the loadFingerprintsFile function.
func TestLoadFingerprintsFile(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()

	// Create known clients file
	clientsContent := `
		# Comment line
		client1 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF
		client2 001122334455667788
		
		# Another comment
		client3 00:11:22:33:44:55:66:77
		`
	clientsFile := filepath.Join(tempDir, "known-clients.txt")
	require.NoError(t, os.WriteFile(clientsFile, []byte(clientsContent), 0o600))

	// Create known servers file
	serversContent := `
		# Comment line
		server1:443 00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF
		localhost:8443 001122334455667788
		
		# Another comment
		example.com:8080 00:11:22:33:44:55:66:77
		`
	serversFile := filepath.Join(tempDir, "known-servers.txt")
	require.NoError(t, os.WriteFile(serversFile, []byte(serversContent), 0o600))

	// Create invalid format file
	invalidContent := `
		client1 missing second part
		invalid format
		`
	invalidFile := filepath.Join(tempDir, "invalid.txt")
	require.NoError(t, os.WriteFile(invalidFile, []byte(invalidContent), 0o600))

	testCases := []struct {
		name         string
		filePath     string
		expectedKeys []string
		expectError  bool
	}{
		{
			name:         "load client fingerprints",
			filePath:     clientsFile,
			expectedKeys: []string{"client1", "client2", "client3"},
			expectError:  false,
		},
		{
			name:         "load server fingerprints",
			filePath:     serversFile,
			expectedKeys: []string{"server1:443", "localhost:8443", "example.com:8080"},
			expectError:  false,
		},
		{
			name:        "non-existent file",
			filePath:    "/nonexistent/file.txt",
			expectError: true,
		},
		{
			name:        "invalid format",
			filePath:    invalidFile,
			expectError: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			fingerprints, err := loadFingerprintsFile(tc.filePath)

			if tc.expectError {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			for _, key := range tc.expectedKeys {
				assert.Contains(t, fingerprints, key)
			}
			assert.Len(t, fingerprints, len(tc.expectedKeys))
		})
	}
}

// TestFingerprintFormatting tests the fingerprint formatting and parsing functions.
func TestFingerprintFormatting(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		input     string
		formatted string
		parsed    string
	}{
		{
			name:      "standard hex string",
			input:     "0011223344556677889900aabbccddeeff",
			formatted: "00:11:22:33:44:55:66:77:88:99:00:aa:bb:cc:dd:ee:ff",
			parsed:    "0011223344556677889900aabbccddeeff",
		},
		{
			name:      "hex with colons and uppercase",
			input:     "00:11:22:33:44:55:66:77:88:99:00:AA:BB:CC:DD:EE:FF",
			formatted: "00:11:22:33:44:55:66:77:88:99:00:AA:BB:CC:DD:EE:FF",
			parsed:    "0011223344556677889900aabbccddeeff",
		},
		{
			name:      "odd length string",
			input:     "0011223344556677889900aabbccddeef",
			formatted: "00:11:22:33:44:55:66:77:88:99:00:aa:bb:cc:dd:ee:f",
			parsed:    "0011223344556677889900aabbccddeef",
		},
		{
			name:      "short fingerprint",
			input:     "00:11:22:33:44:55",
			formatted: "00:11:22:33:44:55",
			parsed:    "001122334455",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			formatted := formatFingerprintWithColons(tc.input)
			assert.Equal(t, tc.formatted, formatted)

			parsed := parseFingerprint(tc.input)
			assert.Equal(t, tc.parsed, parsed)
		})
	}
}

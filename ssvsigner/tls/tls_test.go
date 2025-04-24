package tls

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestClientTLSConfigNoConfig verifies that LoadClientTLSConfig with empty configuration
// returns a minimal valid TLS config with proper TLS version and no certificates.
func TestClientTLSConfigNoConfig(t *testing.T) {
	t.Parallel()

	config := Config{}
	tlsConfig, err := config.LoadClientTLSConfig()

	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	assert.Equal(t, uint16(MinTLSVersion), tlsConfig.MinVersion)
	assert.Empty(t, tlsConfig.Certificates)
	assert.Nil(t, tlsConfig.VerifyConnection)
}

// TestClientTLSConfigWithServerCert verifies that LoadClientTLSConfig with a valid server certificate
// returns a config with custom certificate verification but no client certificate.
func TestClientTLSConfigWithServerCert(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	serverCertFile := filepath.Join(tempDir, "server-cert.pem")

	validCertContent := `-----BEGIN CERTIFICATE-----
MIIEDzCCAvegAwIBAgIUc0I1DeE9V66jUs9TI03ccCO9WMswDQYJKoZIhvcNAQEL
BQAwgZYxCzAJBgNVBAYTAlVTMQswCQYDVQQIDAJDQTEWMBQGA1UEBwwNU2FuIEZy
YW5jaXNjbzEUMBIGA1UECgwLU1NWIE5ldHdvcmsxFzAVBgNVBAsMDlNTViBWYWxp
ZGF0b3JzMRgwFgYDVQQDDA93ZWIzc2lnbmVyLmhvc3QxGTAXBgkqhkiG9w0BCQEW
CnNzdkBzc3YuaW8wHhcNMjUwNDI0MTAwMTI3WhcNMjYwNDI0MTAwMTI3WjCBljEL
MAkGA1UEBhMCVVMxCzAJBgNVBAgMAkNBMRYwFAYDVQQHDA1TYW4gRnJhbmNpc2Nv
MRQwEgYDVQQKDAtTU1YgTmV0d29yazEXMBUGA1UECwwOU1NWIFZhbGlkYXRvcnMx
GDAWBgNVBAMMD3dlYjNzaWduZXIuaG9zdDEZMBcGCSqGSIb3DQEJARYKc3N2QHNz
di5pbzCCASIwDQYJKoZIhvcNAQEBBQADggEPADCCAQoCggEBALSV1oKO1yHG04zF
iVIqkFmFYeCZajMDJFRIfxKIEJRdZ5Y4x+zO3dT2QHO0Ms3ovB01xtFUxUL/zDZr
yr5AWwfI51qAtQMjSlKrav7r14nhm4qp429e0sl0aVDf1/ikKqMOfNCzm14hpLkK
66+9dXORH+JtxxBn6BEdtBm2ivxhof0OJON/HwFIcsKSbo2qZVZHBZbaiwG+JlKL
bczYsb+Sr6umUj/ipFrwkWKlhiJyGmDr7mKcHQOHrNX1SocHAdiznhQpX20RiMA1
znHHIyuH7GzYuSsaC5rMpzTkFw4xbxSahHs34ufbCIxvFzfPOrubSWujbyy9lRGk
KpVxk4sCAwEAAaNTMFEwHQYDVR0OBBYEFIuwOlknq1nrK+geCkhIcM975us+MB8G
A1UdIwQYMBaAFIuwOlknq1nrK+geCkhIcM975us+MA8GA1UdEwEB/wQFMAMBAf8w
DQYJKoZIhvcNAQELBQADggEBAFSMnSP4RS/uyrG0eLlX0CeTbBHonpbyhSH/Dqnp
7FrHXcP1qKwK34J0BX1PfGFzZTIJBJPGXJiPHLQ1DH5+yVPxkmZosmUn938fWZFc
0s3Q7dnHKQ4XcNkRJYKbclQlhN/IeY8XSXLI0kv3ZJd3IKiL4Z9dgez8U80Th9C8
1Y+In8iUYlsx6pAgk9XLxpe1FJmCky3jf8PUmVtYX7NL16mGQkOsUeLwD1Hz0tQV
gE0B/97mnhuZ+evth+gPO/YugqfSl5qQ4tegnTLsMThjQNg1I4O27YbtegCUXvRc
eJdWyRsqIr/hvJETvWiFlf9gS05iFCXIlj1DrHYZ1WSh6pg=
-----END CERTIFICATE-----`
	err := os.WriteFile(serverCertFile, []byte(validCertContent), 0o600)
	require.NoError(t, err)

	config := Config{
		ClientServerCertFile: serverCertFile,
	}

	tlsConfig, err := config.LoadClientTLSConfig()

	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	assert.Equal(t, uint16(MinTLSVersion), tlsConfig.MinVersion)
	assert.Empty(t, tlsConfig.Certificates)
	assert.NotNil(t, tlsConfig.VerifyConnection)
	assert.True(t, tlsConfig.InsecureSkipVerify)
}

// TestClientTLSConfigWithInvalidServerCert verifies that LoadClientTLSConfig fails
// with appropriate error when an invalid PEM certificate is provided.
func TestClientTLSConfigWithInvalidServerCert(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	invalidCertFile := filepath.Join(tempDir, "invalid-cert.pem")
	err := os.WriteFile(invalidCertFile, []byte("This is not a valid certificate"), 0o600)
	require.NoError(t, err)

	config := Config{
		ClientServerCertFile: invalidCertFile,
	}

	_, err = config.LoadClientTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to decode PEM block")
}

// TestClientTLSConfigWithNonExistentServerCert verifies that LoadClientTLSConfig fails
// with appropriate error when the certificate file doesn't exist.
func TestClientTLSConfigWithNonExistentServerCert(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	nonExistentFile := filepath.Join(tempDir, "nonexistent.pem")

	config := Config{
		ClientServerCertFile: nonExistentFile,
	}

	_, err := config.LoadClientTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read certificate file")
}

// TestClientTLSConfigWithKeystoreNoPassword verifies that LoadClientTLSConfig validates
// that a password file is required when a keystore file is provided.
func TestClientTLSConfigWithKeystoreNoPassword(t *testing.T) {
	t.Parallel()

	config := Config{
		ClientKeystoreFile: "keystore.p12",
	}

	_, err := config.LoadClientTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "client keystore password file is required")
}

// TestServerTLSConfigNoConfig verifies that LoadServerTLSConfig with empty configuration
// returns a minimal valid TLS config with proper TLS version and no certificates.
func TestServerTLSConfigNoConfig(t *testing.T) {
	t.Parallel()

	config := Config{}
	tlsConfig, err := config.LoadServerTLSConfig()

	require.NoError(t, err)
	require.NotNil(t, tlsConfig)
	assert.Equal(t, uint16(MinTLSVersion), tlsConfig.MinVersion)
	assert.Empty(t, tlsConfig.Certificates)
}

// TestServerTLSConfigWithKeystoreNoPassword verifies that LoadServerTLSConfig validates
// that a password file is required when a keystore file is provided.
func TestServerTLSConfigWithKeystoreNoPassword(t *testing.T) {
	t.Parallel()

	config := Config{
		ServerKeystoreFile: "server.p12",
	}

	_, err := config.LoadServerTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server keystore password file is required")
}

// TestServerTLSConfigWithKnownClientsOnly verifies that LoadServerTLSConfig validates
// that a keystore file is required when a known clients file is provided.
func TestServerTLSConfigWithKnownClientsOnly(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	knownClientsFile := filepath.Join(tempDir, "known-clients.txt")
	err := os.WriteFile(knownClientsFile, []byte("client1 1234567890abcdef"), 0o600)
	require.NoError(t, err)

	config := Config{
		ServerKnownClientsFile: knownClientsFile,
	}

	_, err = config.LoadServerTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "server keystore file is required")
}

// TestServerTLSConfigWithKeystoreAndPassword verifies that LoadServerTLSConfig properly
// attempts to read the keystore file when a valid configuration with keystore path and password is provided.
func TestServerTLSConfigWithKeystoreAndPassword(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	keystoreFile := filepath.Join(tempDir, "server.p12")
	passwordFile := filepath.Join(tempDir, "password.txt")

	err := os.WriteFile(passwordFile, []byte("testpassword"), 0o600)
	require.NoError(t, err)

	config := Config{
		ServerKeystoreFile:         keystoreFile,
		ServerKeystorePasswordFile: passwordFile,
	}

	_, err = config.LoadServerTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read keystore file")
}

// TestServerTLSConfigWithCompleteConfig verifies that LoadServerTLSConfig properly processes
// a complete configuration with keystore, password, and known clients, attempting to read
// the keystore file.
func TestServerTLSConfigWithCompleteConfig(t *testing.T) {
	t.Parallel()

	tempDir := t.TempDir()
	keystoreFile := filepath.Join(tempDir, "server.p12")
	passwordFile := filepath.Join(tempDir, "password.txt")
	knownClientsFile := filepath.Join(tempDir, "known-clients.txt")

	err := os.WriteFile(knownClientsFile, []byte("client1 1234567890abcdef"), 0o600)
	require.NoError(t, err)

	err = os.WriteFile(passwordFile, []byte("testpassword"), 0o600)
	require.NoError(t, err)

	config := Config{
		ServerKeystoreFile:         keystoreFile,
		ServerKeystorePasswordFile: passwordFile,
		ServerKnownClientsFile:     knownClientsFile,
	}

	_, err = config.LoadServerTLSConfig()

	require.Error(t, err)
	assert.Contains(t, err.Error(), "read keystore file")
}

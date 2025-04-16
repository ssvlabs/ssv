package main

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsatesting"
)

var logger, _ = zap.NewDevelopment()

func TestRun_InvalidWeb3SignerEndpoint(t *testing.T) {
	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "invalid-url",
		PrivateKey:         base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
	}

	err := run(logger, cli)
	require.ErrorContains(t, err, "invalid WEB3SIGNER_ENDPOINT format")
}

func TestRun_MissingPrivateKey(t *testing.T) {
	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "http://example.com",
		PrivateKey:         "",
		PrivateKeyFile:     "",
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error for missing private key")
	require.ErrorContains(t, err, "neither private key nor keystore provided", "Error message should indicate missing keys")
}

func TestRun_InvalidPrivateKeyFormat(t *testing.T) {
	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "http://example.com",
		PrivateKey:         "invalid-key-format",
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error for invalid private key format")
	require.Contains(t, err.Error(), "failed to parse private key", "Error message should mention key parsing failure")
}

func TestRun_FailedKeystoreLoad(t *testing.T) {
	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "http://example.com",
		PrivateKeyFile:     "/nonexistent/path",
		PasswordFile:       "/nonexistent/password",
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error for failed keystore load")
	require.Contains(t, err.Error(), "failed to load operator key from file", "Error message should indicate keystore loading failure")
}

func TestRun_FailedServerStart(t *testing.T) {
	cli := CLI{
		ListenAddr:         ":999999",
		Web3SignerEndpoint: "http://example.com",
		PrivateKey:         base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error when the server fails to start")
	require.Contains(t, err.Error(), "invalid port", "Error message should indicate server startup failure")
}

func TestClientTLSConfig(t *testing.T) {
	t.Run("with InsecureSkipVerify", func(t *testing.T) {
		cli := CLI{
			InsecureSkipVerify: true,
		}

		config, err := createClientTLSConfig(logger, cli)
		require.NoError(t, err)
		require.NotNil(t, config)
		require.True(t, config.InsecureSkipVerify)
	})

	t.Run("with no certificates", func(t *testing.T) {
		cli := CLI{
			InsecureSkipVerify: false,
		}

		config, err := createClientTLSConfig(logger, cli)
		require.NoError(t, err)
		require.Nil(t, config)
	})
}

func TestServerTLSConfig(t *testing.T) {
	t.Run("with InsecureSkipVerify", func(t *testing.T) {
		// Create temporary certificate and key files for testing
		// This is a simplified test that just ensures the InsecureSkipVerify option
		// is properly set when a valid certificate configuration is provided
		tempDir := t.TempDir()

		certFile := tempDir + "/cert.pem"
		keyFile := tempDir + "/key.pem"

		err := createTestCertificates(certFile, keyFile)
		require.NoError(t, err, "Failed to create test certificates")

		cli := CLI{
			ServerCertFile:     certFile,
			ServerKeyFile:      keyFile,
			InsecureSkipVerify: true,
		}

		config, err := createServerTLSConfig(logger, cli)
		require.NoError(t, err)
		require.NotNil(t, config)
		require.True(t, config.InsecureSkipVerify)
	})

	t.Run("without InsecureSkipVerify", func(t *testing.T) {
		// Create temporary certificate and key files for testing
		tempDir := t.TempDir()

		certFile := tempDir + "/cert.pem"
		keyFile := tempDir + "/key.pem"

		err := createTestCertificates(certFile, keyFile)
		require.NoError(t, err, "Failed to create test certificates")

		cli := CLI{
			ServerCertFile:     certFile,
			ServerKeyFile:      keyFile,
			InsecureSkipVerify: false,
		}

		config, err := createServerTLSConfig(logger, cli)
		require.NoError(t, err)
		require.NotNil(t, config)
		require.False(t, config.InsecureSkipVerify)
	})

	t.Run("with no certificates", func(t *testing.T) {
		cli := CLI{
			InsecureSkipVerify: true,
		}

		config, err := createServerTLSConfig(logger, cli)
		require.NoError(t, err)
		require.Nil(t, config)
	})
}

// createTestCertificates creates a self-signed certificate and key for testing.
func createTestCertificates(certFile, keyFile string) error {
	// Sample test certificate and key (not for real use)
	certPEM := []byte(`-----BEGIN CERTIFICATE-----
MIIBhTCCASugAwIBAgIQIRi6zePL6mKjOipn+dNuaTAKBggqhkjOPQQDAjASMRAw
DgYDVQQKEwdBY21lIENvMB4XDTE3MTAyMDE5NDMwNloXDTE4MTAyMDE5NDMwNlow
EjEQMA4GA1UEChMHQWNtZSBDbzBZMBMGByqGSM49AgEGCCqGSM49AwEHA0IABD0d
7VNhbWvZLWPuj/RtHFjvtJBEwOkhbN/BnnE8rnZR8+sbwnc/KhCk3FhnpHZnQz7B
5aETbbIgmuvewdjvSBSjYzBhMA4GA1UdDwEB/wQEAwICpDATBgNVHSUEDDAKBggr
BgEFBQcDATAPBgNVHRMBAf8EBTADAQH/MCkGA1UdEQQiMCCCDmxvY2FsaG9zdDo1
NDUzgg4xMjcuMC4wLjE6NTQ1MzAKBggqhkjOPQQDAgNIADBFAiEA2zpJEPQyz6/l
Wf86aX6PepsntZv2GYlA5UpabfT2EZICICpJ5h/iI+i341gBmLiAFQOyTDT+/wQc
6MF9+Yw1Yy0t
-----END CERTIFICATE-----`)

	keyPEM := []byte(`-----BEGIN EC PRIVATE KEY-----
MHcCAQEEIIrYSSNQFaA2Hwf1duRSxKtLYX5CB04fSeQ6tF1aY/PuoAoGCCqGSM49
AwEHoUQDQgAEPR3tU2Fta9ktY+6P9G0cWO+0kETA6SFs38GecTyudlHz6xvCdz8q
EKTcWGekdmdDPsHloRNtsiCa697B2O9IFA==
-----END EC PRIVATE KEY-----`)

	if err := os.WriteFile(certFile, certPEM, 0644); err != nil {
		return err
	}

	return os.WriteFile(keyFile, keyPEM, 0600)
}

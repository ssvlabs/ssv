package main

import (
	"encoding/base64"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsatesting"
)

func TestRun_InvalidWeb3SignerEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		wantErr  string
	}{
		{
			name:     "invalid URL format",
			endpoint: "invalid-url",
			wantErr:  "invalid WEB3SIGNER_ENDPOINT: invalid url format",
		},
		{
			name:     "missing scheme",
			endpoint: "192.168.1.1:9000",
			wantErr:  "invalid WEB3SIGNER_ENDPOINT: invalid url format",
		},
		{
			name:     "invalid scheme",
			endpoint: "ftp://example.com",
			wantErr:  "invalid WEB3SIGNER_ENDPOINT: invalid url scheme \"ftp\": only http/https allowed",
		},
		{
			name:     "missing hostname",
			endpoint: "http://",
			wantErr:  "invalid WEB3SIGNER_ENDPOINT: missing hostname in url",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			logger, _ := zap.NewDevelopment()

			cli := CLI{
				ListenAddr:         ":8080",
				Web3SignerEndpoint: tt.endpoint,
				PrivateKey:         base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
			}

			err := run(logger, cli)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestRun_MissingPrivateKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "https://ssvlabs.io/",
		PrivateKey:         "",
		PrivateKeyFile:     "",
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error for missing private key")
	require.ErrorContains(t, err, "neither private key nor keystore provided", "Error message should indicate missing keys")
}

func TestRun_InvalidPrivateKeyFormat(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "https://ssvlabs.io/",
		PrivateKey:         "invalid-key-format",
		AllowInsecureHTTP:  true,
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error for invalid private key format")
	require.Contains(t, err.Error(), "failed to parse private key", "Error message should mention key parsing failure")
}

func TestRun_FailedKeystoreLoad(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "https://ssvlabs.io/",
		PrivateKeyFile:     "/nonexistent/path",
		PasswordFile:       "/nonexistent/password",
		AllowInsecureHTTP:  true,
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error for failed keystore load")
	require.Contains(t, err.Error(), "failed to load operator key from file", "Error message should indicate keystore loading failure")
}

func TestRun_FailedServerStart(t *testing.T) {
	logger, _ := zap.NewDevelopment()

	cli := CLI{
		ListenAddr:         ":999999",
		Web3SignerEndpoint: "https://ssvlabs.io/",
		PrivateKey:         base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
		AllowInsecureHTTP:  true,
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error when the server fails to start")
	require.Contains(t, err.Error(), "invalid port", "Error message should indicate server startup failure")
}

func TestCLIConfiguration(t *testing.T) {
	t.Run("Configure via flags", func(t *testing.T) {
		cli := &CLI{}
		parser, err := kong.New(cli)
		require.NoError(t, err)

		args := []string{"--listen-addr", ":9090", "--web3signer-endpoint", "https://web3signer.example.com"}
		_, err = parser.Parse(args)
		require.NoError(t, err)

		assert.Equal(t, ":9090", cli.ListenAddr)
		assert.Equal(t, "https://web3signer.example.com", cli.Web3SignerEndpoint)
	})

	t.Run("Configure via environment variables", func(t *testing.T) {
		t.Setenv("LISTEN_ADDR", ":8888")
		t.Setenv("WEB3SIGNER_ENDPOINT", "http://localhost:9000")

		cli := &CLI{}
		parser, err := kong.New(cli)
		require.NoError(t, err)

		_, err = parser.Parse([]string{})
		require.NoError(t, err)

		assert.Equal(t, ":8888", cli.ListenAddr)
		assert.Equal(t, "http://localhost:9000", cli.Web3SignerEndpoint)
	})
}

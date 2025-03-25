package main

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	rsatesting "github.com/ssvlabs/ssv/ssvsigner/rsaencryption/testingspace"
)

func TestRun_InvalidWeb3SignerEndpoint(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	cli := CLI{
		ListenAddr:         ":8080",
		Web3SignerEndpoint: "invalid-url",
		PrivateKey:         base64.StdEncoding.EncodeToString([]byte(rsatesting.SkPem)),
	}

	err := run(logger, cli)
	require.ErrorContains(t, err, "invalid WEB3SIGNER_ENDPOINT format")
}

func TestRun_MissingPrivateKey(t *testing.T) {
	logger, _ := zap.NewDevelopment()
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
	logger, _ := zap.NewDevelopment()
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
	logger, _ := zap.NewDevelopment()
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
	logger, _ := zap.NewDevelopment()
	cli := CLI{
		ListenAddr:         ":999999",
		Web3SignerEndpoint: "http://example.com",
		PrivateKey:         base64.StdEncoding.EncodeToString([]byte(rsatesting.SkPem)),
	}

	err := run(logger, cli)
	require.Error(t, err, "Expected an error when the server fails to start")
	require.Contains(t, err.Error(), "invalid port", "Error message should indicate server startup failure")
}

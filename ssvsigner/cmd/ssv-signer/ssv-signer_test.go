package main

import (
	"encoding/base64"
	"os"
	"reflect"
	"strings"
	"testing"

	"github.com/alecthomas/kong"
	"github.com/stretchr/testify/assert"
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

// TestParseEmbeddedFlags tests the parsing of embedded structures with prefixes.
func TestParseEmbeddedFlags(t *testing.T) {
	t.Parallel()

	originalArgs := os.Args
	defer func() { os.Args = originalArgs }()

	testCases := []struct {
		name     string
		args     []string
		validate func(t *testing.T, cli *CLI)
	}{
		{
			name: "Client TLS flags",
			args: []string{
				"ssv-signer",
				"--web3signer-endpoint", "http://example.com",
				"--private-key", base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
				"--client-cert-file=/path/to/client.crt",
				"--client-key-file=/path/to/client.key",
				"--client-ca-cert-file=/path/to/ca.crt",
				"--client-insecure-skip-verify",
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, "/path/to/client.crt", cli.Client.ClientCertFile)
				assert.Equal(t, "/path/to/client.key", cli.Client.ClientKeyFile)
				assert.Equal(t, "/path/to/ca.crt", cli.Client.ClientCACertFile)
				assert.True(t, cli.Client.ClientInsecureSkipVerify)
			},
		},
		{
			name: "Server TLS flags",
			args: []string{
				"ssv-signer",
				"--web3signer-endpoint", "http://example.com",
				"--private-key", base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
				"--server-cert-file", "/path/to/server.crt",
				"--server-key-file", "/path/to/server.key",
				"--server-ca-cert-file", "/path/to/ca.crt",
				"--server-insecure-skip-verify",
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, "/path/to/server.crt", cli.Server.ServerCertFile)
				assert.Equal(t, "/path/to/server.key", cli.Server.ServerKeyFile)
				assert.Equal(t, "/path/to/ca.crt", cli.Server.ServerCACertFile)
				assert.True(t, cli.Server.ServerInsecureSkipVerify)
			},
		},
		{
			name: "Mixed client and server flags",
			args: []string{
				"ssv-signer",
				"--listen-addr", ":9090",
				"--web3signer-endpoint", "https://web3signer.example.com",
				"--private-key", base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
				"--client-cert-file", "/path/to/client.crt",
				"--client-key-file", "/path/to/client.key",
				"--server-cert-file", "/path/to/server.crt",
				"--server-key-file", "/path/to/server.key",
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, ":9090", cli.ListenAddr)
				assert.Equal(t, "https://web3signer.example.com", cli.Web3SignerEndpoint)
				assert.Equal(t, "/path/to/client.crt", cli.Client.ClientCertFile)
				assert.Equal(t, "/path/to/client.key", cli.Client.ClientKeyFile)
				assert.Equal(t, "/path/to/server.crt", cli.Server.ServerCertFile)
				assert.Equal(t, "/path/to/server.key", cli.Server.ServerKeyFile)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			os.Args = tc.args

			cli := &CLI{}

			parser, err := kong.New(cli)
			require.NoError(t, err)

			ctx, err := parser.Parse(tc.args[1:])
			require.NoError(t, err)
			require.NotNil(t, ctx)

			tc.validate(t, cli)
		})
	}
}

// TestEnvironmentVariables tests parsing embedded structure values from environment variables.
func TestEnvironmentVariables(t *testing.T) {
	t.Parallel()

	originalEnv := os.Environ()
	defer func() {
		os.Clearenv()
		for _, env := range originalEnv {
			kv := os.Expand(env, os.Getenv)
			key, value, _ := strings.Cut(kv, "=")
			os.Setenv(key, value)
		}
	}()

	testCases := []struct {
		name     string
		envVars  map[string]string
		validate func(t *testing.T, cli *CLI)
	}{
		{
			name: "Client TLS environment variables",
			envVars: map[string]string{
				"WEB3SIGNER_ENDPOINT":         "http://example.com",
				"PRIVATE_KEY":                 base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
				"CLIENT_CERT_FILE":            "/path/to/client.crt",
				"CLIENT_KEY_FILE":             "/path/to/client.key",
				"CLIENT_CA_CERT_FILE":         "/path/to/ca.crt",
				"CLIENT_INSECURE_SKIP_VERIFY": "true",
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, "http://example.com", cli.Web3SignerEndpoint)
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)), cli.PrivateKey)
				assert.Equal(t, "/path/to/client.crt", cli.Client.ClientCertFile)
				assert.Equal(t, "/path/to/client.key", cli.Client.ClientKeyFile)
				assert.Equal(t, "/path/to/ca.crt", cli.Client.ClientCACertFile)
				assert.True(t, cli.Client.ClientInsecureSkipVerify)
			},
		},
		{
			name: "Server TLS environment variables",
			envVars: map[string]string{
				"WEB3SIGNER_ENDPOINT":         "http://example.com",
				"PRIVATE_KEY":                 base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)),
				"SERVER_CERT_FILE":            "/path/to/server.crt",
				"SERVER_KEY_FILE":             "/path/to/server.key",
				"SERVER_CA_CERT_FILE":         "/path/to/ca.crt",
				"SERVER_INSECURE_SKIP_VERIFY": "true",
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, "http://example.com", cli.Web3SignerEndpoint)
				assert.Equal(t, base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM)), cli.PrivateKey)
				assert.Equal(t, "/path/to/server.crt", cli.Server.ServerCertFile)
				assert.Equal(t, "/path/to/server.key", cli.Server.ServerKeyFile)
				assert.Equal(t, "/path/to/ca.crt", cli.Server.ServerCACertFile)
				assert.True(t, cli.Server.ServerInsecureSkipVerify)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			os.Clearenv()
			for k, v := range tc.envVars {
				t.Logf("setting env var %s=%s", k, v)
				os.Setenv(k, v)
			}

			cli := &CLI{}

			parser, err := kong.New(cli)
			require.NoError(t, err)

			ctx, err := parser.Parse([]string{})
			require.NoError(t, err)
			require.NotNil(t, ctx)

			tc.validate(t, cli)
		})
	}
}

// TestFlagEnvVarMapping tests that the expected environment variable names are used in the struct tags.
func TestFlagEnvVarMapping(t *testing.T) {
	t.Parallel()

	cliType := reflect.TypeOf(CLI{})

	expectedEnvVars := map[string]string{
		"ListenAddr":         "LISTEN_ADDR",
		"Web3SignerEndpoint": "WEB3SIGNER_ENDPOINT",
		"PrivateKey":         "PRIVATE_KEY",
		"PrivateKeyFile":     "PRIVATE_KEY_FILE",
		"PasswordFile":       "PASSWORD_FILE",
	}

	for fieldName, expectedEnvVar := range expectedEnvVars {
		field, found := cliType.FieldByName(fieldName)
		require.True(t, found)

		envTag := field.Tag.Get("env")
		require.Equal(t, expectedEnvVar, envTag)
	}

	clientField, found := cliType.FieldByName("Client")
	require.True(t, found)

	embedTag := clientField.Tag.Get("embed")
	require.Equal(t, "", embedTag)

	serverField, found := cliType.FieldByName("Server")
	require.True(t, found)

	embedTag = serverField.Tag.Get("embed")
	require.Equal(t, "", embedTag)
}

// TestInitializeWithFlags tests that the CLI struct fields can be properly initialized with direct assignment.
func TestInitializeWithFlags(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		configure func(cli *CLI)
		validate  func(t *testing.T, cli *CLI)
	}{
		{
			name: "Client TLS config",
			configure: func(cli *CLI) {
				cli.Web3SignerEndpoint = "http://example.com"
				cli.PrivateKey = base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM))
				cli.Client.ClientCertFile = "/path/to/client.crt"
				cli.Client.ClientKeyFile = "/path/to/client.key"
				cli.Client.ClientCACertFile = "/path/to/ca.crt"
				cli.Client.ClientInsecureSkipVerify = true
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, "http://example.com", cli.Web3SignerEndpoint)
				assert.Equal(t, "/path/to/client.crt", cli.Client.ClientCertFile)
				assert.Equal(t, "/path/to/client.key", cli.Client.ClientKeyFile)
				assert.Equal(t, "/path/to/ca.crt", cli.Client.ClientCACertFile)
				assert.True(t, cli.Client.ClientInsecureSkipVerify)
			},
		},
		{
			name: "Server TLS config",
			configure: func(cli *CLI) {
				cli.Web3SignerEndpoint = "http://example.com"
				cli.PrivateKey = base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM))
				cli.Server.ServerCertFile = "/path/to/server.crt"
				cli.Server.ServerKeyFile = "/path/to/server.key"
				cli.Server.ServerCACertFile = "/path/to/ca.crt"
				cli.Server.ServerInsecureSkipVerify = true
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, "http://example.com", cli.Web3SignerEndpoint)
				assert.Equal(t, "/path/to/server.crt", cli.Server.ServerCertFile)
				assert.Equal(t, "/path/to/server.key", cli.Server.ServerKeyFile)
				assert.Equal(t, "/path/to/ca.crt", cli.Server.ServerCACertFile)
				assert.True(t, cli.Server.ServerInsecureSkipVerify)
			},
		},
		{
			name: "Mixed client and server config",
			configure: func(cli *CLI) {
				cli.ListenAddr = ":9090"
				cli.Web3SignerEndpoint = "https://web3signer.example.com"
				cli.PrivateKey = base64.StdEncoding.EncodeToString([]byte(rsatesting.PrivKeyPEM))
				cli.Client.ClientCertFile = "/path/to/client.crt"
				cli.Client.ClientKeyFile = "/path/to/client.key"
				cli.Server.ServerCertFile = "/path/to/server.crt"
				cli.Server.ServerKeyFile = "/path/to/server.key"
			},
			validate: func(t *testing.T, cli *CLI) {
				assert.Equal(t, ":9090", cli.ListenAddr)
				assert.Equal(t, "https://web3signer.example.com", cli.Web3SignerEndpoint)
				assert.Equal(t, "/path/to/client.crt", cli.Client.ClientCertFile)
				assert.Equal(t, "/path/to/client.key", cli.Client.ClientKeyFile)
				assert.Equal(t, "/path/to/server.crt", cli.Server.ServerCertFile)
				assert.Equal(t, "/path/to/server.key", cli.Server.ServerKeyFile)
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli := &CLI{}

			tc.configure(cli)
			tc.validate(t, cli)
		})
	}
}

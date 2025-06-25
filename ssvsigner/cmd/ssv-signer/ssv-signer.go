package main

import (
	"fmt"
	"os"
	"time"

	"github.com/alecthomas/kong"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/cmd/internal/logger"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/cmd/internal/validation"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/tls"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	ListenAddr         string        `env:"LISTEN_ADDR" default:":8080" required:"" help:"The address and port to listen on (e.g. :8080)"` // TODO: finalize port
	Web3SignerEndpoint string        `env:"WEB3SIGNER_ENDPOINT" required:"" help:"URL of the web3signer service" name:"web3signer-endpoint"`
	PrivateKey         string        `env:"PRIVATE_KEY" xor:"keys" required:"" help:"Base64‑encoded PEM blob (RSA PRIVATE KEY) for operator; exclusive with PRIVATE_KEY_FILE"`
	PrivateKeyFile     string        `env:"PRIVATE_KEY_FILE" xor:"keys" and:"files" help:"Path to an encrypted keystore JSON file (v4 format) containing an RSA private key; exclusive with PRIVATE_KEY"`
	PasswordFile       string        `env:"PASSWORD_FILE" and:"files" help:"Path to file containing the password used to decrypt the keystore JSON file"`
	LogLevel           string        `env:"LOG_LEVEL" default:"info" enum:"debug,info,warn,error" help:"Set log level (debug, info, warn, error)"`
	LogFormat          string        `env:"LOG_FORMAT" default:"console" enum:"console,json" help:"Set log format (console, json)"`
	RequestTimeout     time.Duration `env:"REQUEST_TIMEOUT" default:"10s" help:"Timeout for outgoing HTTP requests (e.g. 500ms, 10s)"`

	// AllowInsecureHTTP allows ssv-signer to work without using TLS. Note that it allows "partial" TLS as well such as only server or only client.
	AllowInsecureHTTP bool `env:"ALLOW_INSECURE_HTTP" name:"allow-insecure-http" default:"false" help:"Allow insecure HTTP requests. Do not use in production"`
	// AllowInsecureNetworks allows ssv-signer to work in insecure networks, which are blocked by default.
	AllowInsecureNetworks bool `env:"ALLOW_INSECURE_NETWORKS" name:"allow-insecure-networks" default:"false" help:"Allow insecure HTTP networks. Do not use in production"`

	// Server TLS configuration (for incoming connections to SSV Signer)
	KeystoreFile         string `env:"KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for server TLS connections"`
	KeystorePasswordFile string `env:"KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for server keystore file"`
	KnownClientsFile     string `env:"KNOWN_CLIENTS_FILE" env-description:"Path to known clients file for authenticating clients"`

	// Client TLS configuration (for connecting to Web3Signer)
	Web3SignerKeystoreFile         string `env:"WEB3SIGNER_KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for TLS connection to Web3Signer"`
	Web3SignerKeystorePasswordFile string `env:"WEB3SIGNER_KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for client keystore file"`
	Web3SignerServerCertFile       string `env:"WEB3SIGNER_SERVER_CERT_FILE" env-description:"Path to trusted server certificate file for authenticating Web3Signer"`
}

func main() {
	var cli CLI

	kong.Must(&cli,
		kong.Name("ssv-signer"),
		kong.UsageOnError(),
	)

	log, err := logger.SetupLogger(cli.LogLevel, cli.LogFormat)
	if err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "setup logger: %v\n", err)
		os.Exit(1)
	}

	defer func() { _ = log.Sync() }()

	if err := run(log, cli); err != nil {
		log.Fatal("application failed", zap.Error(err))
	}
}

func run(logger *zap.Logger, cli CLI) error {
	logger.Debug("starting ssv-signer",
		zap.String("listen_addr", cli.ListenAddr),
		zap.String("web3signer_endpoint", cli.Web3SignerEndpoint),
		zap.Bool("got_private_key", cli.PrivateKey != ""),
		zap.String("log_level", cli.LogLevel),
		zap.String("log_format", cli.LogFormat),
		zap.Duration("request_timeout", cli.RequestTimeout),
		zap.Bool("server_tls_enabled", cli.KeystoreFile != ""),
		zap.Bool("client_tls_enabled", cli.Web3SignerKeystoreFile != ""),
		zap.Bool("allow_insecure_http", cli.AllowInsecureHTTP),
		zap.Bool("allow_insecure_networks", cli.AllowInsecureNetworks),
	)

	if cli.AllowInsecureHTTP {
		logger.Warn("ssv-signer started with an insecure mode that allows HTTP and doesn't enforce HTTPS, " +
			"do not use this option in production")
	}

	if err := validateConfig(cli); err != nil {
		return err
	}

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init bls: %w", err)
	}

	operatorPrivateKey, err := loadOperatorKey(cli.PrivateKey, cli.PrivateKeyFile, cli.PasswordFile)
	if err != nil {
		return err
	}

	tlsConfig := tls.Config{
		ServerKeystoreFile:         cli.KeystoreFile,
		ServerKeystorePasswordFile: cli.KeystorePasswordFile,
		ServerKnownClientsFile:     cli.KnownClientsFile,

		ClientKeystoreFile:         cli.Web3SignerKeystoreFile,
		ClientKeystorePasswordFile: cli.Web3SignerKeystorePasswordFile,
		ClientServerCertFile:       cli.Web3SignerServerCertFile,
	}

	web3SignerClient, err := setupWeb3SignerClient(cli.Web3SignerEndpoint, cli.RequestTimeout, tlsConfig)
	if err != nil {
		return err
	}

	return startServer(logger, cli.ListenAddr, operatorPrivateKey, web3SignerClient, tlsConfig)
}

func validateConfig(cli CLI) error {
	if cli.PrivateKey == "" && cli.PrivateKeyFile == "" {
		return fmt.Errorf("neither private key nor keystore provided")
	}

	if err := validation.ValidateWeb3SignerEndpoint(cli.Web3SignerEndpoint, cli.AllowInsecureNetworks); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT: %w", err)
	}

	if cli.AllowInsecureHTTP {
		allFiles := []string{
			cli.KeystoreFile,
			cli.KeystorePasswordFile,
			cli.KnownClientsFile,
			cli.Web3SignerKeystoreFile,
			cli.Web3SignerKeystorePasswordFile,
			cli.Web3SignerServerCertFile,
		}

		filesFound := 0
		for _, file := range allFiles {
			if file != "" {
				filesFound++
			}
		}

		if filesFound == len(allFiles) {
			return fmt.Errorf("insecure mode is enabled, but all TLS options are provided, please disable the insecure mode")
		}
	} else {
		if cli.KeystoreFile == "" {
			return fmt.Errorf("server TLS keystore file is required")
		}
		if cli.KeystorePasswordFile == "" {
			return fmt.Errorf("server TLS keystore password file is required")
		}
		if cli.KnownClientsFile == "" {
			return fmt.Errorf("known clients file is required for client authentication")
		}

		if cli.Web3SignerKeystoreFile == "" {
			return fmt.Errorf("web3signer TLS keystore file is required")
		}
		if cli.Web3SignerKeystorePasswordFile == "" {
			return fmt.Errorf("web3signer TLS keystore password file is required")
		}
		if cli.Web3SignerServerCertFile == "" {
			return fmt.Errorf("web3signer server cert file is required")
		}
	}

	return nil
}

func loadOperatorKey(privateKeyStr, privateKeyFile, passwordFile string) (keys.OperatorPrivateKey, error) {
	if privateKeyStr != "" {
		pk, err := keys.PrivateKeyFromString(privateKeyStr)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		return pk, nil
	}

	pk, err := keystore.LoadOperatorKeystore(privateKeyFile, passwordFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load operator key from file: %w", err)
	}
	return pk, nil
}

func setupWeb3SignerClient(endpoint string, timeout time.Duration, tlsConfig tls.Config) (*web3signer.Web3Signer, error) {
	if tlsConfig.ClientKeystoreFile != "" || tlsConfig.ClientServerCertFile != "" {
		config, err := tlsConfig.LoadClientTLSConfig()
		if err != nil {
			return nil, fmt.Errorf("load client TLS config: %w", err)
		}

		// Create client with TLS
		return web3signer.New(
			endpoint,
			web3signer.WithRequestTimeout(timeout),
			web3signer.WithTLS(config),
		), nil
	}

	// Create client without TLS
	return web3signer.New(
		endpoint,
		web3signer.WithRequestTimeout(timeout),
	), nil
}

func startServer(logger *zap.Logger, listenAddr string, operatorKey keys.OperatorPrivateKey, web3SignerClient *web3signer.Web3Signer, tlsConfig tls.Config) error {
	logger.Info("starting ssv-signer server",
		zap.String("addr", listenAddr),
		zap.Bool("tls_enabled", tlsConfig.ServerKeystoreFile != ""),
	)

	var opts []ssvsigner.Option
	if tlsConfig.ServerKeystoreFile != "" {
		config, err := tlsConfig.LoadServerTLSConfig()
		if err != nil {
			return fmt.Errorf("load server TLS config: %w", err)
		}

		opts = append(opts, ssvsigner.WithTLS(config))
	}

	srv := ssvsigner.NewServer(logger, operatorKey, web3SignerClient, opts...)
	return srv.ListenAndServe(listenAddr)
}

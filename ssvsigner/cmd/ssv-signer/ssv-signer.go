package main

import (
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/alecthomas/kong"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner"
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

	// Server TLS configuration (for incoming connections to SSV Signer)
	KeystoreFile         string `env:"KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for server TLS connections"`
	KeystorePasswordFile string `env:"KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for server keystore file"`
	KnownClientsFile     string `env:"KNOWN_CLIENTS_FILE" env-description:"Path to known clients file for authenticating clients"`

	// Client TLS configuration (for connecting to Web3Signer)
	Web3SignerKeystoreFile         string `env:"WEB3SIGNER_KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for TLS connection to Web3Signer"`
	Web3SignerKeystorePasswordFile string `env:"WEB3SIGNER_KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for client keystore file"`
	Web3SignerKnownServersFile     string `env:"WEB3SIGNER_KNOWN_SERVERS_FILE" env-description:"Path to known servers file for authenticating Web3Signer"`
}

func main() {
	cli := CLI{}
	_ = kong.Parse(&cli)

	logger, err := setupLogger(cli.LogLevel, cli.LogFormat)
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Println("failed to sync logger: ", err)
		}
	}()

	if err := run(logger, cli); err != nil {
		logger.Fatal("application failed", zap.Error(err))
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
	)

	tlsConfig := tls.Config{
		ServerKeystoreFile:         cli.KeystoreFile,
		ServerKeystorePasswordFile: cli.KeystorePasswordFile,
		ServerKnownClientsFile:     cli.KnownClientsFile,

		ClientKeystoreFile:         cli.Web3SignerKeystoreFile,
		ClientKeystorePasswordFile: cli.Web3SignerKeystorePasswordFile,
		ClientKnownServersFile:     cli.Web3SignerKnownServersFile,
	}

	if err := validateConfig(cli, tlsConfig); err != nil {
		return err
	}

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init bls: %w", err)
	}

	operatorPrivateKey, err := loadOperatorKey(cli.PrivateKey, cli.PrivateKeyFile, cli.PasswordFile)
	if err != nil {
		return err
	}

	web3SignerClient, err := setupWeb3SignerClient(cli.Web3SignerEndpoint, cli.RequestTimeout, tlsConfig)
	if err != nil {
		return err
	}

	return startServer(logger, cli.ListenAddr, operatorPrivateKey, web3SignerClient, tlsConfig)
}

func setupLogger(logLevel, logFormat string) (*zap.Logger, error) {
	cfg := zap.NewProductionConfig()
	if logFormat == "console" {
		cfg.Encoding = "console"
		cfg.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	} else {
		cfg.Encoding = "json"
	}

	level := zap.NewAtomicLevel()
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		return nil, fmt.Errorf("parse log level: %w", err)
	}
	cfg.Level = level

	return cfg.Build()
}

func validateConfig(cli CLI, tlsConfig tls.Config) error {
	// Validate private key configuration
	if cli.PrivateKey == "" && cli.PrivateKeyFile == "" {
		return fmt.Errorf("neither private key nor keystore provided")
	}

	// Validate Web3Signer endpoint
	if _, err := url.ParseRequestURI(cli.Web3SignerEndpoint); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT format: %w", err)
	}

	// Validate TLS configurations
	if err := tlsConfig.ValidateServerTLS(); err != nil {
		return fmt.Errorf("invalid server TLS config: %w", err)
	}

	if err := tlsConfig.ValidateClientTLS(); err != nil {
		return fmt.Errorf("invalid client TLS config: %w", err)
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
	if tlsConfig.ClientKeystoreFile != "" || tlsConfig.ClientKnownServersFile != "" {
		certificate, fingerprints, err := tlsConfig.LoadClientTLS()
		if err != nil {
			return nil, fmt.Errorf("load client TLS config: %w", err)
		}

		// Create client with TLS
		return web3signer.New(
			endpoint,
			web3signer.WithRequestTimeout(timeout),
			web3signer.WithTLS(certificate, fingerprints),
		)
	}

	// Create client without TLS
	return web3signer.New(
		endpoint,
		web3signer.WithRequestTimeout(timeout),
	)
}

func startServer(logger *zap.Logger, listenAddr string, operatorKey keys.OperatorPrivateKey, web3SignerClient *web3signer.Web3Signer, tlsConfig tls.Config) error {
	logger.Info("starting ssv-signer server",
		zap.String("addr", listenAddr),
		zap.Bool("tls_enabled", tlsConfig.ServerKeystoreFile != ""),
	)

	srv := ssvsigner.NewServer(logger, operatorKey, web3SignerClient)

	// Configure server TLS if needed
	if tlsConfig.ServerKeystoreFile != "" {
		// Load server TLS configuration
		certificate, fingerprints, err := tlsConfig.LoadServerTLS()
		if err != nil {
			return fmt.Errorf("load server TLS config: %w", err)
		}

		// Set TLS configuration
		if err := srv.SetTLS(certificate, fingerprints); err != nil {
			return fmt.Errorf("set server TLS config: %w", err)
		}
	}

	return srv.ListenAndServe(listenAddr)
}

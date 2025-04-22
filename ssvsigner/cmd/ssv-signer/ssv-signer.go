package main

import (
	"fmt"
	"log"
	"net/url"

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
	ListenAddr         string `env:"LISTEN_ADDR" default:":8080" required:""` // TODO: finalize port
	Web3SignerEndpoint string `env:"WEB3SIGNER_ENDPOINT" required:"" name:"web3signer-endpoint"`
	PrivateKey         string `env:"PRIVATE_KEY" xor:"keys" required:""`
	PrivateKeyFile     string `env:"PRIVATE_KEY_FILE" xor:"keys" and:"files"`
	PasswordFile       string `env:"PASSWORD_FILE" and:"files"`

	// Server TLS configuration (for incoming connections to SSV Signer)
	ServerKeystoreFile         string `env:"SERVER_KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for server TLS connections"`
	ServerKeystorePasswordFile string `env:"SERVER_KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for server keystore file"`
	ServerKnownClientsFile     string `env:"SERVER_KNOWN_CLIENTS_FILE" env-description:"Path to known clients file for authenticating clients"`

	// Client TLS configuration (for connecting to Web3Signer)
	ClientKeystoreFile         string `env:"CLIENT_KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for TLS connection to Web3Signer"`
	ClientKeystorePasswordFile string `env:"CLIENT_KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for client keystore file"`
	ClientKnownServersFile     string `env:"CLIENT_KNOWN_SERVERS_FILE" env-description:"Path to known servers file for authenticating Web3Signer"`
}

func main() {
	cli := CLI{}
	_ = kong.Parse(&cli)

	logger, err := zap.NewDevelopment()
	if err != nil {
		log.Fatal(err)
	}
	defer func() {
		if err := logger.Sync(); err != nil {
			log.Println("failed to sync logger: ", err)
		}
	}()

	if err := run(logger, cli); err != nil {
		logger.Fatal("Application failed", zap.Error(err))
	}
}

func run(logger *zap.Logger, cli CLI) error {
	logger.Debug("Starting ssv-signer",
		zap.String("listen_addr", cli.ListenAddr),
		zap.String("web3signer_endpoint", cli.Web3SignerEndpoint),
		zap.Bool("got_private_key", cli.PrivateKey != ""),
		zap.Bool("server_tls_enabled", cli.ServerKeystoreFile != ""),
		zap.Bool("client_tls_enabled", cli.ClientKeystoreFile != ""),
	)

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init BLS: %w", err)
	}

	// Create TLS config from CLI parameters
	tlsConfig := tls.Config{
		// Server TLS config
		ServerKeystoreFile:         cli.ServerKeystoreFile,
		ServerKeystorePasswordFile: cli.ServerKeystorePasswordFile,
		ServerKnownClientsFile:     cli.ServerKnownClientsFile,

		// Client TLS config
		ClientKeystoreFile:         cli.ClientKeystoreFile,
		ClientKeystorePasswordFile: cli.ClientKeystorePasswordFile,
		ClientKnownServersFile:     cli.ClientKnownServersFile,
	}

	if err := validateConfig(cli, tlsConfig); err != nil {
		return err
	}

	operatorPrivateKey, err := loadOperatorPrivateKey(cli)
	if err != nil {
		return err
	}

	// Configure Web3Signer client with TLS options
	var web3SignerClient *web3signer.Web3Signer
	if cli.ClientKeystoreFile != "" || cli.ClientKnownServersFile != "" {
		// Load client TLS configuration
		certificate, fingerprints, err := tlsConfig.LoadClientTLS()
		if err != nil {
			return fmt.Errorf("load client TLS config: %w", err)
		}

		web3SignerClient, err = web3signer.New(
			cli.Web3SignerEndpoint,
			web3signer.WithTLS(certificate, fingerprints),
		)
	} else {
		web3SignerClient, err = web3signer.New(cli.Web3SignerEndpoint)
	}

	if err != nil {
		return fmt.Errorf("init web3signer: %w", err)
	}

	srv := ssvsigner.NewServer(logger, operatorPrivateKey, web3SignerClient)

	// Configure server TLS
	if cli.ServerKeystoreFile != "" {
		// Load server TLS configuration
		certificate, fingerprints, err := tlsConfig.LoadServerTLS()
		if err != nil {
			return fmt.Errorf("load server TLS config: %w", err)
		}

		// Set TLS configuration
		if err := srv.SetTLS(certificate, fingerprints); err != nil {
			return fmt.Errorf("server TLS: %w", err)
		}
	}

	logger.Info("Starting ssv-signer server",
		zap.String("addr", cli.ListenAddr),
		zap.Bool("tls_enabled", cli.ServerKeystoreFile != ""),
	)

	return srv.ListenAndServe(cli.ListenAddr)
}

// validateConfig validates the CLI configuration.
func validateConfig(cli CLI, tlsConfig tls.Config) error {
	// PrivateKeyFile and PasswordFile use the same 'and' group,
	// so setting them as 'required' wouldn't allow to start with PrivateKey.
	if cli.PrivateKey == "" && cli.PrivateKeyFile == "" {
		return fmt.Errorf("neither private key nor keystore provided")
	}

	if _, err := url.ParseRequestURI(cli.Web3SignerEndpoint); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT format: %w", err)
	}

	// Validate TLS configurations
	if err := tlsConfig.ValidateServerTLS(); err != nil {
		return fmt.Errorf("server TLS config: %w", err)
	}

	if err := tlsConfig.ValidateClientTLS(); err != nil {
		return fmt.Errorf("client TLS config: %w", err)
	}

	return nil
}

// loadOperatorPrivateKey loads the operator private key from CLI parameters.
func loadOperatorPrivateKey(cli CLI) (keys.OperatorPrivateKey, error) {
	if cli.PrivateKey != "" {
		pk, err := keys.PrivateKeyFromString(cli.PrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to parse private key: %w", err)
		}
		return pk, nil
	}

	pk, err := keystore.LoadOperatorKeystore(cli.PrivateKeyFile, cli.PasswordFile)
	if err != nil {
		return nil, fmt.Errorf("failed to load operator key from file: %w", err)
	}
	return pk, nil
}

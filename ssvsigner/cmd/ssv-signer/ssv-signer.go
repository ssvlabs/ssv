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
	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	ListenAddr         string `env:"LISTEN_ADDR" default:":8080" required:""` // TODO: finalize port
	Web3SignerEndpoint string `env:"WEB3SIGNER_ENDPOINT" required:"" name:"web3signer-endpoint"`
	PrivateKey         string `env:"PRIVATE_KEY" xor:"keys" required:""`
	PrivateKeyFile     string `env:"PRIVATE_KEY_FILE" xor:"keys" and:"files"`
	PasswordFile       string `env:"PASSWORD_FILE" and:"files"`

	// TLS configuration
	Client ssvsignertls.ClientConfig `embed:""`
	Server ssvsignertls.ServerConfig `embed:""`
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
		zap.Bool("server_tls_enabled", cli.Server.ServerCertFile != "" && cli.Server.ServerKeyFile != ""),
		zap.Bool("client_tls_enabled", cli.Client.ClientCertFile != "" && cli.Client.ClientKeyFile != ""),
	)

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init BLS: %w", err)
	}

	if err := validateConfig(cli); err != nil {
		return err
	}

	operatorPrivateKey, err := loadOperatorPrivateKey(cli)
	if err != nil {
		return err
	}

	// TLS configurations
	clientTLSConfig, err := ssvsignertls.CreateClientConfig(cli.Client)
	if err != nil {
		return fmt.Errorf("failed to create client TLS configuration: %w", err)
	}

	serverTLSConfig, err := ssvsignertls.CreateServerConfig(cli.Server)
	if err != nil {
		return fmt.Errorf("failed to create server TLS configuration: %w", err)
	}

	web3SignerClient := web3signer.New(cli.Web3SignerEndpoint, web3signer.WithTLSConfig(clientTLSConfig))

	var serverOptions []ssvsigner.ServerOption
	if serverTLSConfig != nil {
		serverOptions = append(serverOptions, ssvsigner.WithServerTLSConfig(serverTLSConfig))
	}

	srv := ssvsigner.NewServer(logger, operatorPrivateKey, web3SignerClient, serverOptions...)

	logger.Info("Starting ssv-signer server",
		zap.String("addr", cli.ListenAddr),
		zap.Bool("tls_enabled", serverTLSConfig != nil),
	)

	return srv.ListenAndServe(cli.ListenAddr)
}

// validateConfig validates the CLI configuration.
func validateConfig(cli CLI) error {
	// PrivateKeyFile and PasswordFile use the same 'and' group,
	// so setting them as 'required' wouldn't allow to start with PrivateKey.
	if cli.PrivateKey == "" && cli.PrivateKeyFile == "" {
		return fmt.Errorf("neither private key nor keystore provided")
	}

	if _, err := url.ParseRequestURI(cli.Web3SignerEndpoint); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT format: %w", err)
	}

	_, _, _, err := ssvsignertls.LoadCertificatesFromFiles(cli.Client.ClientCertFile, cli.Client.ClientKeyFile, cli.Client.ClientCACertFile)
	if err != nil {
		return fmt.Errorf("invalid client TLS configuration: %w", err)
	}

	_, _, _, err = ssvsignertls.LoadCertificatesFromFiles(cli.Server.ServerCertFile, cli.Server.ServerKeyFile, cli.Server.ServerCACertFile)
	if err != nil {
		return fmt.Errorf("invalid server TLS configuration: %w", err)
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

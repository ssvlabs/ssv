package main

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"log"
	"net/url"
	"os"

	"github.com/alecthomas/kong"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	ListenAddr         string `env:"LISTEN_ADDR" default:":8080" required:""` // TODO: finalize port
	Web3SignerEndpoint string `env:"WEB3SIGNER_ENDPOINT" required:"" name:"web3signer-endpoint"`
	PrivateKey         string `env:"PRIVATE_KEY" xor:"keys" required:""`
	PrivateKeyFile     string `env:"PRIVATE_KEY_FILE" xor:"keys" and:"files"`
	PasswordFile       string `env:"PASSWORD_FILE" and:"files"`

	// TLS configuration
	Client ssvsigner.ClientTLSConfig `embed:""`
	Server ssvsigner.ServerTLSConfig `embed:""`
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
	clientTLSConfig, err := createClientTLSConfig(logger, cli)
	if err != nil {
		return fmt.Errorf("failed to create client TLS configuration: %w", err)
	}

	serverTLSConfig, err := createServerTLSConfig(logger, cli)
	if err != nil {
		return fmt.Errorf("failed to create server TLS configuration: %w", err)
	}

	web3SignerClient := web3signer.New(cli.Web3SignerEndpoint, web3signer.WithTLSConfig(clientTLSConfig))

	var serverOptions []ssvsigner.ServerOption
	if serverTLSConfig != nil {
		serverOptions = append(serverOptions, ssvsigner.WithTLSConfig(serverTLSConfig))
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

	// Validate client TLS configuration
	if cli.Client.ClientCertFile != "" && cli.Client.ClientKeyFile == "" {
		return fmt.Errorf("client certificate provided without key")
	}
	if cli.Client.ClientKeyFile != "" && cli.Client.ClientCertFile == "" {
		return fmt.Errorf("client key provided without certificate")
	}

	// Validate server TLS configuration
	if cli.Server.ServerCertFile != "" && cli.Server.ServerKeyFile == "" {
		return fmt.Errorf("server certificate provided without key")
	}
	if cli.Server.ServerKeyFile != "" && cli.Server.ServerCertFile == "" {
		return fmt.Errorf("server key provided without certificate")
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

// loadCertFile loads a certificate file if the path is provided.
func loadCertFile(path string) ([]byte, error) {
	if path == "" {
		return nil, nil
	}

	// #nosec G304
	cert, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("could not read certificate file %s: %w", path, err)
	}

	return cert, nil
}

// createClientTLSConfig creates TLS configuration for client connections.
func createClientTLSConfig(logger *zap.Logger, cli CLI) (*tls.Config, error) {
	if cli.Client.ClientInsecureSkipVerify {
		logger.Warn("client TLS configured with ServerInsecureSkipVerify=true (not recommended for production)")
		return &tls.Config{
			InsecureSkipVerify: true,
		}, nil
	}

	if cli.Client.ClientCertFile == "" || cli.Client.ClientKeyFile == "" {
		return nil, nil
	}

	clientCert, err := loadCertFile(cli.Client.ClientCertFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read client certificate: %w", err)
	}

	clientKey, err := loadCertFile(cli.Client.ClientKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read client key: %w", err)
	}

	cert, err := tls.X509KeyPair(clientCert, clientKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load client certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates: []tls.Certificate{cert},
	}

	// add CA certificate if available
	if cli.Server.ServerCACertFile != "" {
		caCert, err := loadCertFile(cli.Server.ServerCACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA certificate to pool")
		}

		tlsConfig.RootCAs = caCertPool
	}

	logger.Info("client TLS configured successfully")
	return tlsConfig, nil
}

// createServerTLSConfig creates TLS configuration for the server.
func createServerTLSConfig(logger *zap.Logger, cli CLI) (*tls.Config, error) {
	if cli.Server.ServerCertFile == "" || cli.Server.ServerKeyFile == "" {
		return nil, nil
	}

	serverCert, err := loadCertFile(cli.Server.ServerCertFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read server certificate: %w", err)
	}

	serverKey, err := loadCertFile(cli.Server.ServerKeyFile)
	if err != nil {
		return nil, fmt.Errorf("failed to read server key: %w", err)
	}

	cert, err := tls.X509KeyPair(serverCert, serverKey)
	if err != nil {
		return nil, fmt.Errorf("failed to load server certificate: %w", err)
	}

	tlsConfig := &tls.Config{
		Certificates:       []tls.Certificate{cert},
		InsecureSkipVerify: cli.Client.ClientInsecureSkipVerify,
	}

	if cli.Client.ClientInsecureSkipVerify {
		logger.Warn("server TLS configured with ServerInsecureSkipVerify=true (not recommended for production)")
	}

	// add CA certificate if available for client verification
	if cli.Server.ServerCACertFile != "" {
		caCert, err := loadCertFile(cli.Server.ServerCACertFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA certificate: %w", err)
		}

		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to append CA certificate to pool")
		}

		tlsConfig.ClientCAs = caCertPool
		tlsConfig.ClientAuth = tls.RequireAndVerifyClientCert
	}

	logger.Info("server TLS configured successfully")
	return tlsConfig, nil
}

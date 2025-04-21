package main

import (
	"errors"
	"fmt"
	"log"
	"net/url"

	"github.com/alecthomas/kong"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/ssvsigner/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	ListenAddr         string `env:"LISTEN_ADDR" default:":8080" required:""` // TODO: finalize port
	Web3SignerEndpoint string `env:"WEB3SIGNER_ENDPOINT" required:""`
	PrivateKey         string `env:"PRIVATE_KEY" xor:"keys" required:""`
	PrivateKeyFile     string `env:"PRIVATE_KEY_FILE" xor:"keys" and:"files"`
	PasswordFile       string `env:"PASSWORD_FILE" and:"files"`
	LogLevel           string `env:"LOG_LEVEL" default:"info" enum:"debug,info,warn,error" help:"Set log level (debug, info, warn, error)"`
	LogFormat          string `env:"LOG_FORMAT" default:"console" enum:"console,json" help:"Set log format (console, json)"`
}

func main() {
	cli := CLI{}
	err := kong.Parse(&cli)
	if err != nil {
		log.Fatal(err)
	}
	
	cfg := zap.NewProductionConfig()
	if cli.LogFormat == "console" {
		cfg.Encoding = "console"
		cfg.EncoderConfig = zap.NewDevelopmentEncoderConfig()
	} else {
		cfg.Encoding = "json"
	}

	level := zap.NewAtomicLevel()
	if err := level.UnmarshalText([]byte(cli.LogLevel)); err != nil {
		log.Fatalf("failed to parse log level: %v", err)
	}
	cfg.Level = level

	logger, err := cfg.Build()
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
		zap.String("private_key_file", cli.PrivateKeyFile),
		zap.String("password_file", cli.PasswordFile),
		zap.Bool("got_private_key", cli.PrivateKey != ""),
		zap.String("log_level", cli.LogLevel),
		zap.String("log_format", cli.LogFormat),
	)

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init BLS: %w", err)
	}

	// PrivateKeyFile and PasswordFile use the same 'and' group,
	// so setting them as 'required' wouldn't allow to start with PrivateKey.
	if cli.PrivateKey == "" && cli.PrivateKeyFile == "" {
		return errors.New("neither private key nor keystore provided")
	}

	if _, err := url.ParseRequestURI(cli.Web3SignerEndpoint); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT format: %w", err)
	}

	var operatorPrivateKey keys.OperatorPrivateKey
	if cli.PrivateKey != "" {
		pk, err := keys.PrivateKeyFromString(cli.PrivateKey)
		if err != nil {
			return fmt.Errorf("failed to parse private key: %w", err)
		}
		operatorPrivateKey = pk
	} else {
		pk, err := keystore.LoadOperatorKeystore(cli.PrivateKeyFile, cli.PasswordFile)
		if err != nil {
			return fmt.Errorf("failed to load operator key from file: %w", err)
		}
		operatorPrivateKey = pk
	}

	web3SignerClient := web3signer.New(cli.Web3SignerEndpoint)

	logger.Info("Starting ssv-signer server", zap.String("addr", cli.ListenAddr))

	srv := ssvsigner.NewServer(logger, operatorPrivateKey, web3SignerClient)
	if err := fasthttp.ListenAndServe(cli.ListenAddr, srv.Handler()); err != nil {
		return fmt.Errorf("listen on %v: %w", cli.ListenAddr, err)
	}

	return nil
}

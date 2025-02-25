package main

import (
	"log"

	"github.com/alecthomas/kong"
	"github.com/valyala/fasthttp"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/operator/keys"
	"github.com/ssvlabs/ssv/operator/keystore"
	"github.com/ssvlabs/ssv/ssvsigner/ssvsignerserver"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	ListenAddr              string `env:"LISTEN_ADDR" default:":8080"` // TODO: finalize port
	Web3SignerEndpoint      string `env:"WEB3SIGNER_ENDPOINT"`
	PrivateKey              string `env:"PRIVATE_KEY"`
	PrivateKeyFile          string `env:"PRIVATE_KEY_FILE"`
	PasswordFile            string `env:"PASSWORD_FILE"`
	ShareKeystorePassphrase string `env:"SHARE_KEYSTORE_PASSPHRASE" default:"password"` // TODO: finalize default password
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
			log.Println("failed to sync logger", zap.Error(err))
		}
	}()

	logger.Debug("Starting ssv-signer",
		zap.String("listen_addr", cli.ListenAddr),
		zap.String("web3signer_endpoint", cli.Web3SignerEndpoint),
		zap.String("private_key_file", cli.PrivateKeyFile),
		zap.String("password_file", cli.PasswordFile),
		zap.Bool("got_private_key", cli.PrivateKey != ""),
		zap.Bool("got_share_keystore_passphrase", cli.ShareKeystorePassphrase != ""),
	)

	if cli.PrivateKey == "" && cli.PrivateKeyFile == "" {
		logger.Fatal("either private key or private key file must be set, found none")
	}

	if cli.PrivateKey != "" && cli.PrivateKeyFile != "" {
		logger.Fatal("either private key or private key file must be set, found both")
	}

	if cli.ShareKeystorePassphrase == "" {
		logger.Fatal("share keystore passphrase must not be empty")
	}

	var operatorPrivateKey keys.OperatorPrivateKey
	if cli.PrivateKey != "" {
		operatorPrivateKey, err = keys.PrivateKeyFromString(cli.PrivateKey)
		if err != nil {
			logger.Fatal("failed to parse private key", zap.Error(err))
		}
	} else {
		operatorPrivateKey, err = keystore.LoadOperatorKeystore(cli.PrivateKeyFile, cli.PasswordFile)
		if err != nil {
			logger.Fatal("failed to load operator key from file", zap.Error(err))
		}
	}

	web3SignerClient, err := web3signer.New(logger, cli.Web3SignerEndpoint)
	if err != nil {
		logger.Fatal("create web3signer client", zap.Error(err))
	}

	logger.Info("Starting ssv-signer server", zap.String("addr", cli.ListenAddr))

	srv := ssvsignerserver.New(logger, operatorPrivateKey, web3SignerClient, cli.ShareKeystorePassphrase)
	if err := fasthttp.ListenAndServe(cli.ListenAddr, srv.Handler()); err != nil {
		logger.Fatal("failed to start server", zap.Error(err))
	}
}

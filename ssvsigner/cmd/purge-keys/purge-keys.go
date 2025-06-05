// purge-keys purges keys from the remote web3signer instance.
//
// Sometimes ssv-node needs to run with a clean DB for testing,
// and running it with the same web3signer instance will cause duplicated keys on event syncing.
package main

import (
	"context"
	"fmt"
	"log"
	"net/url"
	"time"

	"github.com/alecthomas/kong"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	ssvsignertls "github.com/ssvlabs/ssv/ssvsigner/tls"
	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	Web3SignerEndpoint string `env:"WEB3SIGNER_ENDPOINT" required:""`
	BatchSize          int    `env:"BATCH_SIZE" default:"20"` // reduce if getting context deadline exceeded; increase if it's fast

	// Client TLS configuration (for connecting to Web3Signer)
	Web3SignerKeystoreFile         string `env:"WEB3SIGNER_KEYSTORE_FILE" env-description:"Path to PKCS12 keystore file for TLS connection to Web3Signer"`
	Web3SignerKeystorePasswordFile string `env:"WEB3SIGNER_KEYSTORE_PASSWORD_FILE" env-description:"Path to file containing the password for client keystore file"`
	Web3SignerServerCertFile       string `env:"WEB3SIGNER_SERVER_CERT_FILE" env-description:"Path to trusted server certificate file for authenticating Web3Signer"`
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
	logger.Debug("running",
		zap.String("web3signer_endpoint", cli.Web3SignerEndpoint),
		zap.Int("batch_size", cli.BatchSize),
		zap.Bool("client_tls_enabled", cli.Web3SignerKeystoreFile != "" || cli.Web3SignerServerCertFile != ""),
	)

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init BLS: %w", err)
	}

	if _, err := url.ParseRequestURI(cli.Web3SignerEndpoint); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT format: %w", err)
	}

	ctx := context.Background()

	tlsConfig := ssvsignertls.Config{
		ClientKeystoreFile:         cli.Web3SignerKeystoreFile,
		ClientKeystorePasswordFile: cli.Web3SignerKeystorePasswordFile,
		ClientServerCertFile:       cli.Web3SignerServerCertFile,
	}

	var options []web3signer.Option

	if cli.Web3SignerKeystoreFile != "" || cli.Web3SignerServerCertFile != "" {
		config, err := tlsConfig.LoadClientTLSConfig()
		if err != nil {
			return fmt.Errorf("load client TLS config: %w", err)
		}

		options = append(options, web3signer.WithTLS(config))
	}

	web3SignerClient := web3signer.New(cli.Web3SignerEndpoint, options...)

	fetchStart := time.Now()
	logger.Info("fetching key list")
	keys, err := web3SignerClient.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}

	logger.Info("fetched key list", fields.Count(len(keys)), fields.Took(time.Since(fetchStart)))

	if len(keys) == 0 {
		logger.Warn("no keys found, exiting")
		return nil
	}

	deletingStart := time.Now()
	logger.Info("deleting keys in batches", fields.Count(len(keys)))

	statusCount := map[web3signer.Status]int{}

	for i := 0; i < len(keys); i += cli.BatchSize {
		batchStart := time.Now()

		end := i + cli.BatchSize
		if end > len(keys) {
			end = len(keys)
		}
		batch := keys[i:end]

		logger.Info("processing batch",
			zap.Int("batch_index", i/cli.BatchSize),
			zap.Int("batch_size", len(batch)))

		req := web3signer.DeleteKeystoreRequest{
			Pubkeys: batch,
		}
		resp, err := web3SignerClient.DeleteKeystore(ctx, req)
		if err != nil {
			logger.Error("failed to delete keystore batch",
				zap.Int("batch_index", i/cli.BatchSize),
				fields.Took(time.Since(batchStart)),
				zap.Error(err))
			continue
		}

		for _, data := range resp.Data {
			statusCount[data.Status]++
		}

		logger.Info("batch processed",
			zap.Int("batch_index", i/cli.BatchSize),
			zap.Int("batch_size", len(batch)),
			fields.Took(time.Since(batchStart)))
	}

	logger.Info("all batches completed",
		zap.Any("status_count", statusCount),
		fields.Took(time.Since(deletingStart)))

	return nil
}

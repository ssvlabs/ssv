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

	"github.com/alecthomas/kong"
	"github.com/herumi/bls-eth-go-binary/bls"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

type CLI struct {
	Web3SignerEndpoint string `env:"WEB3SIGNER_ENDPOINT" required:""`
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
	logger.Debug("purging keys",
		zap.String("web3signer_endpoint", cli.Web3SignerEndpoint),
	)

	if err := bls.Init(bls.BLS12_381); err != nil {
		return fmt.Errorf("init BLS: %w", err)
	}

	if _, err := url.ParseRequestURI(cli.Web3SignerEndpoint); err != nil {
		return fmt.Errorf("invalid WEB3SIGNER_ENDPOINT format: %w", err)
	}

	ctx := context.Background()

	web3SignerClient := web3signer.New(cli.Web3SignerEndpoint)
	keys, err := web3SignerClient.ListKeys(ctx)
	if err != nil {
		return fmt.Errorf("list keys: %w", err)
	}

	req := web3signer.DeleteKeystoreRequest{
		Pubkeys: keys,
	}

	statusCount := map[web3signer.Status]int{}

	resp, err := web3SignerClient.DeleteKeystore(ctx, req)
	if err != nil {
		return fmt.Errorf("delete keystore: %w", err)
	}

	for _, data := range resp.Data {
		statusCount[data.Status]++
	}

	logger.Info("purged keys", zap.Any("status_count", statusCount))

	return nil
}

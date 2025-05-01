package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	client "github.com/attestantio/go-eth2-client"
	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"go.uber.org/zap"
)

func (gc *GoClient) Genesis(ctx context.Context) (*apiv1.Genesis, error) {
	return genesisForClient(ctx, gc.log, gc.multiClient)
}

// It's used in both Genesis and singleClientHooks, so we need some common implementation to avoid code repetition.
func genesisForClient(ctx context.Context, log *zap.Logger, provider client.Service) (*apiv1.Genesis, error) {
	start := time.Now()
	// Genesis result is cached in the client and updated once in a while.
	// So calling this method often shouldn't worsen the performance.
	genesisResp, err := provider.(client.GenesisProvider).Genesis(ctx, &api.GenesisOpts{})
	recordRequestDuration(ctx, "Genesis", provider.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		log.Error(clResponseErrMsg,
			zap.String("api", "Genesis"),
			zap.Error(err),
		)
		return nil, err
	}
	if genesisResp.Data == nil {
		log.Error(clNilResponseDataErrMsg,
			zap.String("api", "Genesis"),
		)
		return nil, fmt.Errorf("genesis response data is nil")
	}

	return genesisResp.Data, err
}

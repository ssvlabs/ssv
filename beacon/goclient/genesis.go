package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"go.uber.org/zap"
)

func (gc *GoClient) Genesis(ctx context.Context) (*apiv1.Genesis, error) {
	start := time.Now()
	genesisResp, err := gc.multiClient.Genesis(ctx, &api.GenesisOpts{})
	recordRequestDuration(gc.ctx, "Genesis", gc.multiClient.Address(), http.MethodGet, time.Since(start), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Genesis"),
			zap.Error(err),
		)
		return nil, err
	}
	if genesisResp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "Genesis"),
		)
		return nil, fmt.Errorf("genesis response data is nil")
	}

	return genesisResp.Data, err
}

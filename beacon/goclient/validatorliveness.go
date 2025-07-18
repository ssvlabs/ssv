package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

func (gc *GoClient) ValidatorLiveness(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*apiv1.ValidatorLiveness, error) {
	start := time.Now()
	resp, err := gc.multiClient.ValidatorLiveness(ctx, &api.ValidatorLivenessOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequestDuration(ctx, "ValidatorLiveness", gc.multiClient.Address(), http.MethodPost, time.Since(start), err)

	logger := gc.log.With(zap.String("api", "ValidatorLiveness"))

	if err != nil {
		logger.Error(clResponseErrMsg, zap.Error(err))
		return nil, err
	}

	if resp == nil {
		logger.Error(clNilResponseErrMsg)
		return nil, fmt.Errorf("validator liveness response is nil")
	}

	if resp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg)
		return nil, fmt.Errorf("validator liveness data is nil")
	}

	return resp.Data, nil
}

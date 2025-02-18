package goclient

import (
	"context"
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

func (gc *GoClient) ValidatorLiveness(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*apiv1.ValidatorLiveness, error) {
	resp, err := gc.multiClient.ValidatorLiveness(ctx, &api.ValidatorLivenessOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "ValidatorLiveness"),
			zap.Error(err),
		)
		return nil, err
	}

	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "ValidatorLiveness"),
		)
		return nil, fmt.Errorf("validator liveness response is nil")
	}

	if resp.Data == nil {
		gc.log.Error(clNilResponseDataErrMsg,
			zap.String("api", "ValidatorLiveness"),
		)
		return nil, fmt.Errorf("validator liveness data is nil")
	}

	return resp.Data, nil
}

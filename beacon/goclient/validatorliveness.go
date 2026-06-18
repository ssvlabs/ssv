package goclient

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

func (gc *GoClient) ValidatorLiveness(ctx context.Context, epoch phase0.Epoch, validatorIndices []phase0.ValidatorIndex) ([]*apiv1.ValidatorLiveness, error) {
	start := time.Now()
	resp, err := gc.multiClient.ValidatorLiveness(ctx, &api.ValidatorLivenessOpts{
		Epoch:   epoch,
		Indices: validatorIndices,
	})
	recordRequest(ctx, gc.log, "ValidatorLiveness", gc.multiClient, http.MethodPost, true, time.Since(start), err)
	if err != nil {
		return nil, errMultiClient(fmt.Errorf("fetch validator liveness: %w", err), "ValidatorLiveness")
	}
	if err := checkSliceResponse(resp, "validator liveness"); err != nil {
		return nil, errMultiClient(err, "ValidatorLiveness")
	}

	return resp.Data, nil
}

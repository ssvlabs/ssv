package goclient

import (
	"fmt"
	"net/http"
	"time"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"go.uber.org/zap"
)

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *GoClient) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
	reqStart := time.Now()
	resp, err := gc.multiClient.Validators(gc.ctx, &api.ValidatorsOpts{
		State:   "head", // TODO maybe need to get the chainId (head) as var
		PubKeys: validatorPubKeys,
		Common:  api.CommonOpts{Timeout: gc.longTimeout},
	})
	recordRequestDuration(gc.ctx, "Validators", gc.client.Address(), http.MethodPost, time.Since(reqStart), err)
	if err != nil {
		gc.log.Error(clResponseErrMsg,
			zap.String("api", "Validators"),
			zap.Error(err),
		)
		return nil, fmt.Errorf("failed to obtain validators: %w", err)
	}
	if resp == nil {
		gc.log.Error(clNilResponseErrMsg,
			zap.String("api", "Validators"),
		)
		return nil, fmt.Errorf("validators response is nil")
	}

	return resp.Data, nil
}

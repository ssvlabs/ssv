package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *goClient) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
	resp, err := gc.client.Validators(gc.ctx, &api.ValidatorsOpts{
		State:              "head", // TODO maybe need to get the chainId (head) as var
		PubKeys:            validatorPubKeys,
		WithoutBeaconState: true,
		Common:             api.CommonOpts{Timeout: gc.longTimeout},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain validators: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("validators response is nil")
	}

	return resp.Data, nil
}

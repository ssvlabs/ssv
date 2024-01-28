package goclient

import (
	"fmt"

	"github.com/attestantio/go-eth2-client/api"
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *goClient) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
	// Fetch the validators with maxTimeout, since this method might call the heavy-duty BeaconState for large validator sets.
	resp, err := gc.client.Validators(gc.ctx, &api.ValidatorsOpts{
		State:   "head", // TODO maybe need to get the chainId (head) as var
		PubKeys: validatorPubKeys,
		Common:  api.CommonOpts{Timeout: gc.maxTimeout},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to obtain validators: %w", err)
	}
	if resp == nil {
		return nil, fmt.Errorf("validators response is nil")
	}

	return resp.Data, nil
}

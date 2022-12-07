package goclient

import (
	eth2client "github.com/attestantio/go-eth2-client"
	api "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/pkg/errors"
)

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *goClient) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*api.Validator, error) {
	if provider, isProvider := gc.client.(eth2client.ValidatorsProvider); isProvider {
		validatorsMap, err := provider.ValidatorsByPubKey(gc.ctx, "head", validatorPubKeys) // TODO maybe need to get the chainId (head) as var
		if err != nil {
			return nil, err
		}
		return validatorsMap, nil
	}
	return nil, errors.New("client does not support ValidatorsProvider")
}

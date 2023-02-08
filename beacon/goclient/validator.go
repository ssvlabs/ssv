package goclient

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// GetValidatorData returns metadata (balance, index, status, more) for each pubkey from the node
func (gc *goClient) GetValidatorData(validatorPubKeys []phase0.BLSPubKey) (map[phase0.ValidatorIndex]*eth2apiv1.Validator, error) {
	return gc.client.ValidatorsByPubKey(gc.ctx, "head", validatorPubKeys) // TODO maybe need to get the chainId (head) as var
}

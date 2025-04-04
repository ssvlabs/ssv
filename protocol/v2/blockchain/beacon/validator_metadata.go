package beacon

import (
	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// ValidatorMetadata represents validator metadata from Ethereum beacon node
type ValidatorMetadata struct {
	Balance         phase0.Gwei              `json:"balance"`
	Status          eth2apiv1.ValidatorState `json:"status"`
	Index           phase0.ValidatorIndex    `json:"index"`
	ActivationEpoch phase0.Epoch             `json:"activation_epoch"`
}

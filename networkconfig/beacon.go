package networkconfig

import (
	"github.com/attestantio/go-eth2-client/spec/phase0"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

type BeaconConfig struct {
	Beacon       beacon.BeaconNetwork
	GenesisEpoch phase0.Epoch
}

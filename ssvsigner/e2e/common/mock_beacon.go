package common

import (
	"fmt"

	eth2apiv1 "github.com/attestantio/go-eth2-client/api/v1"
	"github.com/attestantio/go-eth2-client/spec/phase0"
	"github.com/ethereum/go-ethereum/common/hexutil"
)

var MainnetGenesisValidatorsRoot = phase0.Root(hexutil.MustDecode("0x4b363db94e286120d76eb905340fdd4e54bfe9f06bf33ff6cf5ad27f511bfe95"))

var MainnetForks = []*phase0.Fork{
	// Phase0
	{
		Epoch:           phase0.Epoch(0),
		PreviousVersion: phase0.Version{0x00, 0x00, 0x00, 0x00}, // GENESIS_FORK_VERSION: 0x00000000
		CurrentVersion:  phase0.Version{0x00, 0x00, 0x00, 0x00},
	},
	// Altair @ epoch 74240
	{
		Epoch:           phase0.Epoch(74240),
		PreviousVersion: phase0.Version{0x00, 0x00, 0x00, 0x00},
		CurrentVersion:  phase0.Version{0x01, 0x00, 0x00, 0x00}, // ALTAIR_FORK_VERSION: 0x01000000
	},
	// Bellatrix @ epoch 144896
	{
		Epoch:           phase0.Epoch(144896),
		PreviousVersion: phase0.Version{0x01, 0x00, 0x00, 0x00},
		CurrentVersion:  phase0.Version{0x02, 0x00, 0x00, 0x00}, // BELLATRIX_FORK_VERSION: 0x02000000
	},
	// Capella @ epoch 194048
	{
		Epoch:           phase0.Epoch(194048),
		PreviousVersion: phase0.Version{0x02, 0x00, 0x00, 0x00},
		CurrentVersion:  phase0.Version{0x03, 0x00, 0x00, 0x00}, // CAPELLA_FORK_VERSION: 0x03000000
	},
	// Deneb @ epoch 269568
	{
		Epoch:           phase0.Epoch(269568),
		PreviousVersion: phase0.Version{0x03, 0x00, 0x00, 0x00},
		CurrentVersion:  phase0.Version{0x04, 0x00, 0x00, 0x00}, // DENEB_FORK_VERSION: 0x04000000
	},
	// Electra @ epoch 364032
	{
		Epoch:           phase0.Epoch(364032),
		PreviousVersion: phase0.Version{0x04, 0x00, 0x00, 0x00},
		CurrentVersion:  phase0.Version{0x05, 0x00, 0x00, 0x00}, // ELECTRA_FORK_VERSION: 0x05000000
	},
}

func ForkAtEpoch(epoch phase0.Epoch) (*phase0.Fork, error) {
	var forkAtEpoch *phase0.Fork
	for _, fork := range MainnetForks {
		if fork.Epoch <= epoch && (forkAtEpoch == nil || fork.Epoch > forkAtEpoch.Epoch) {
			forkAtEpoch = fork
		}
	}

	if forkAtEpoch == nil {
		return nil, fmt.Errorf("could not find fork at epoch %d", epoch)
	}

	return forkAtEpoch, nil
}

func Genesis() (*eth2apiv1.Genesis, error) {
	return &eth2apiv1.Genesis{
		GenesisValidatorsRoot: MainnetGenesisValidatorsRoot,
	}, nil
}

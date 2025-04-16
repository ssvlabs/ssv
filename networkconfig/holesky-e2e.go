package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var HoleskyE2E = NetworkConfig{
	Name: "holesky-e2e",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.HoleskyNetwork),
		GenesisEpoch:  1,
		SlotDuration:  spectypes.HoleskyNetwork.SlotDurationSec(),
		SlotsPerEpoch: phase0.Slot(spectypes.HoleskyNetwork.SlotsPerEpoch()),
		ForkVersion:   spectypes.HoleskyNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.HoleskyNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           spectypes.DomainType{0x0, 0x0, 0xee, 0x1},
		RegistryContractAddr: "0x58410bef803ecd7e63b23664c586a6db72daf59c",
		RegistrySyncOffset:   big.NewInt(405579),
		Bootnodes:            []string{},
	},
}

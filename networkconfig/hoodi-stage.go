package networkconfig

import (
	"math/big"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var HoodiStage = NetworkConfig{
	Name: "hoodi-stage",
	BeaconConfig: BeaconConfig{
		BeaconName:    string(spectypes.HoleskyNetwork),
		GenesisEpoch:  1,
		SlotDuration:  spectypes.HoodiNetwork.SlotDurationSec(),
		SlotsPerEpoch: phase0.Slot(spectypes.HoodiNetwork.SlotsPerEpoch()),
		ForkVersion:   spectypes.HoodiNetwork.ForkVersion(),
		GenesisTime:   time.Unix(int64(spectypes.HoodiNetwork.MinGenesisTime()), 0), // #nosec G115 -- time should not exceed int64
	},
	SSVConfig: SSVConfig{
		DomainType:           [4]byte{0x00, 0x00, 0x31, 0x14},
		RegistrySyncOffset:   new(big.Int).SetInt64(1004),
		RegistryContractAddr: "0x0aaace4e8affc47c6834171c88d342a4abd8f105",
		DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
		Bootnodes: []string{
			// SSV Labs
			"enr:-Ja4QJZcaYfS0GpX-5xREVBa26a-E-QHMFek-EndsJdgM6loIM7pfbJwPDCNK1VzPkUhMjwcTTuNASiHU6X-sjsrxFmGAZWjNu06gmlkgnY0gmlwhErcGnyJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
		},
	},
}

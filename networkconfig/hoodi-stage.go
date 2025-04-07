package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var HoodiStage = NetworkConfig{
	Name:                 "hoodi-stage",
	Beacon:               beacon.NewNetwork(spectypes.HoodiNetwork),
	DomainType:           [4]byte{0x00, 0x00, 0x31, 0x14},
	GenesisEpoch:         1,
	RegistrySyncOffset:   new(big.Int).SetInt64(1004),
	RegistryContractAddr: "0x0aaace4e8affc47c6834171c88d342a4abd8f105",
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QMME0XoEMoywhjbxvJ_IqFEF184IOQMdweMpZHymLRP2b3gm-XzFgSUuCw4HeZcPV_z6coRINusvqwVEJGxlxaiGAZWjNu0pgmlkgnY0gmlwhAorfUKJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}

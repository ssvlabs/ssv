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
	RegistryContractAddr: "0x868C2789045d7ffC144635Da7D8cC0a974C58f89",
	DiscoveryProtocolID:  [6]byte{'s', 's', 'v', 'd', 'v', '5'},
	Bootnodes: []string{
		// SSV Labs
		"enr:-Ja4QJZcaYfS0GpX-5xREVBa26a-E-QHMFek-EndsJdgM6loIM7pfbJwPDCNK1VzPkUhMjwcTTuNASiHU6X-sjsrxFmGAZWjNu06gmlkgnY0gmlwhErcGnyJc2VjcDI1NmsxoQP_bBE-ZYvaXKBR3dRYMN5K_lZP-q-YsBzDZEtxH_4T_YNzc3YBg3RjcIITioN1ZHCCD6I",
	},
}

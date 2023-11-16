package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var TestNetwork = NetworkConfig{
	Name:                 "testnet",
	Beacon:               beacon.NewNetwork(spectypes.PraterNetwork),
	Domain:               spectypes.V3Testnet,
	GenesisEpoch:         152834,
	RegistrySyncOffset:   new(big.Int).SetInt64(9015219),
	RegistryContractAddr: "0x4B133c68A084B8A88f72eDCd7944B69c8D545f03",
	Bootnodes: []string{
		"enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g;enr:-Li4QBoH15fXLV78y1_nmD5sODveptALORh568iWLS_eju3SUvF2ZfGE2j-nERKU1zb2g5KlS8L70SRLdRUJ-pHH-fmGAYgvh9oGh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO_tV3JP75ZUZPjhOgc2VqEu_FQEMeHc4AyOz6Lz33M2IN0Y3CCE4mDdWRwgg-h",
	},
	PermissionlessActivationEpoch: 123456789,
}

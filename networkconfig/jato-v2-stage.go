package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var JatoV2Stage = NetworkConfig{
	Name:                 "jato-v2-stage",
	Beacon:               beacon.NewNetwork(spectypes.PraterNetwork),
	domainType:           [4]byte{0x00, 0x00, 0x30, 0x12},
	GenesisEpoch:         152834,
	RegistrySyncOffset:   new(big.Int).SetInt64(9249887),
	RegistryContractAddr: "0xd6b633304Db2DD59ce93753FA55076DA367e5b2c",
	Bootnodes: []string{
		"enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g",
		"enr:-Li4QBoH15fXLV78y1_nmD5sODveptALORh568iWLS_eju3SUvF2ZfGE2j-nERKU1zb2g5KlS8L70SRLdRUJ-pHH-fmGAYgvh9oGh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO_tV3JP75ZUZPjhOgc2VqEu_FQEMeHc4AyOz6Lz33M2IN0Y3CCE4mDdWRwgg-h",
	},
}

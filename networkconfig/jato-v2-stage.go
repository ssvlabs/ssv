package networkconfig

import (
	"math/big"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var JatoV2Stage = spectypes.BeaconNetwork{
	Name: "jato-v2-stage",
	SSV: spectypes.SSVParams{
		Domain:               [4]byte{0x00, 0x00, 0x30, 0x12},
		ForkVersion:          [4]byte{0x00, 0x00, 0x10, 0x20},
		GenesisEpoch:         152834,
		ETH1SyncOffset:       new(big.Int).SetInt64(9015219),
		RegistryContractAddr: "0x4B133c68A084B8A88f72eDCd7944B69c8D545f03",
		Bootnodes: []string{
			"enr:-Li4QO86ZMZr_INMW_WQBsP2jS56yjrHnZXxAUOKJz4_qFPKD1Cr3rghQD2FtXPk2_VPnJUi8BBiMngOGVXC0wTYpJGGAYgqnGSNh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQKNW0Mf-xTXcevRSkZOvoN0Q0T9OkTjGZQyQeOl3bYU3YN0Y3CCE4iDdWRwgg-g;enr:-Li4QBoH15fXLV78y1_nmD5sODveptALORh568iWLS_eju3SUvF2ZfGE2j-nERKU1zb2g5KlS8L70SRLdRUJ-pHH-fmGAYgvh9oGh2F0dG5ldHOIAAAAAAAAAACEZXRoMpDkvpOTAAAQIP__________gmlkgnY0gmlwhArqAsGJc2VjcDI1NmsxoQO_tV3JP75ZUZPjhOgc2VqEu_FQEMeHc4AyOz6Lz33M2IN0Y3CCE4mDdWRwgg-h",
		},
	},
	ETH: spectypes.ETHParams{
		NetworkName:      string(core.PraterNetwork),
		MinGenesisTime:   1616508000,
		SlotDuration:     12 * time.Second,
		SlotsPerEpoch:    32,
		CapellaForkEpoch: 162304, // Goerli taken from https://github.com/ethereum/execution-specs/blob/37a8f892341eb000e56e962a051a87e05a2e4443/network-upgrades/mainnet-upgrades/shanghai.md?plain=1#L18
	},
}

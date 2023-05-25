package networkconfig

import (
	"math/big"
	"time"

	"github.com/bloxapp/eth2-key-manager/core"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var JatoV2 = spectypes.BeaconNetwork{
	Name: "jato-v2",
	SSV: spectypes.SSVParams{
		DefaultSyncOffset: new(big.Int).SetInt64(9015219),
		ForkVersion:       [4]byte{0x00, 0x00, 0x10, 0x20},
		Domain:            [4]byte{0x00, 0x00, 0x30, 0x12},
		Bootnodes: []string{ // TODO: check what nodes are needed here
			// env-default from BOOTNODES config
			// "enr:-Li4QO2k62g1tiwitaoFVMT8zN-sSNPp8cg8Kv-5lg6_6VLjVZREhxVMSmerOTptlKbBaO2iszi7rvKBYzbGf38HpcSGAYLoed50h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdWuKJc2VjcDI1NmsxoQITQ1OchoBl5XW9RfBembdN9Er1qNEOIc5ohrQ0rT9B-YN0Y3CCE4iDdWRwgg-g;enr:-Li4QAxqhjjQN2zMAAEtOF5wlcr2SFnPKINvvlwMXztJhClrfRYLrqNy2a_dMUwDPKcvM7bebq3uptRoGSV0LpYEJuyGAYRZG5n5h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQLbXMJi_Pq3imTq11EwH8MbxmXlHYvH2Drz_rsqP1rNyoN0Y3CCE4iDdWRwgg-g",
			// from config.go: STAGE
			// "enr:-LK4QHVq6HEA2KVnAw593SRMqUOvMGlkP8Jb-qHn4yPLHx--cStvWc38Or2xLcWgDPynVxXPT9NWIEXRzrBUsLmcFkUBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhDbUHcyJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			// from config.go: PROD - first public bootnode
			// from config.go: internal ip
			//"enr:-LK4QPbCB0Mw_8ji7D02OwXmqSRZe9wTmitle_cQnECIl-5GBPH9PH__eUpdeiI_t122inm62uTgO9CptbGNLKNId7gBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhArsBGGJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			// from config.go: external IP
			"enr:-LK4QMmL9hLJ1csDN4rQoSjlJGE2SvsXOETfcLH8uAVrxlHaELF0u3NeKCTY2eO_X1zy5eEKcHruyaAsGNiyyG4QWUQBh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO8KQz5L1UEXzEr-CXFFq1th0eG6gopbdul2OQVMuxfMoN0Y3CCE4iDdWRwgg-g",
			// from config.go: PROD - second public bootnode
			// "enr:-Li4QAxqhjjQN2zMAAEtOF5wlcr2SFnPKINvvlwMXztJhClrfRYLrqNy2a_dMUwDPKcvM7bebq3uptRoGSV0LpYEJuyGAYRZG5n5h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQLbXMJi_Pq3imTq11EwH8MbxmXlHYvH2Drz_rsqP1rNyoN0Y3CCE4iDdWRwgg-g",
		},
		DepositContractAddress: "0xff50ed3d0ec03ac01d4c79aad74928bff48a7b2b",
		GenesisValidatorsRoot:  "043db0d9a83813551ee2f33450d23797757d430911a9320530ad8a0eabc43efb",
		GenesisEpoch:           152834, // TODO: 152834? 156113? another value?
	},
	ETH: spectypes.ETHParams{
		NetworkName:      string(core.PraterNetwork),
		MinGenesisTime:   1616508000,
		SlotDuration:     12 * time.Second,
		SlotsPerEpoch:    32,
		CapellaForkEpoch: 162304, // Goerli taken from https://github.com/ethereum/execution-specs/blob/37a8f892341eb000e56e962a051a87e05a2e4443/network-upgrades/mainnet-upgrades/shanghai.md?plain=1#L18
	},
}

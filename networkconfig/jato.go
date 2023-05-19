package networkconfig

import (
	"math/big"
	"time"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var Jato = spectypes.BeaconNetwork{
	Name:              "prater", // TODO: rename to Jato, consider creating a field for "prater" as key manager needs it
	DefaultSyncOffset: new(big.Int).SetInt64(8661727),
	ForkVersion:       [4]byte{0x00, 0x00, 0x10, 0x20},
	MinGenesisTime:    1616508000,
	SlotDuration:      12 * time.Second,
	SlotsPerEpoch:     32,
	CapellaForkEpoch:  162304, // Goerli taken from https://github.com/ethereum/execution-specs/blob/37a8f892341eb000e56e962a051a87e05a2e4443/network-upgrades/mainnet-upgrades/shanghai.md?plain=1#L18
	//Domain:            spectypes.V3Testnet,
	Domain: [4]byte{0x00, 0x00, 0x30, 0x11}, // TODO: find out why this is was config instead of spectypes.V3Testnet
	BootNodes: []string{ // TODO: use
		"enr:-Li4QO2k62g1tiwitaoFVMT8zN-sSNPp8cg8Kv-5lg6_6VLjVZREhxVMSmerOTptlKbBaO2iszi7rvKBYzbGf38HpcSGAYLoed50h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdWuKJc2VjcDI1NmsxoQITQ1OchoBl5XW9RfBembdN9Er1qNEOIc5ohrQ0rT9B-YN0Y3CCE4iDdWRwgg-g;enr:-Li4QAxqhjjQN2zMAAEtOF5wlcr2SFnPKINvvlwMXztJhClrfRYLrqNy2a_dMUwDPKcvM7bebq3uptRoGSV0LpYEJuyGAYRZG5n5h2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhBLb3g2Jc2VjcDI1NmsxoQLbXMJi_Pq3imTq11EwH8MbxmXlHYvH2Drz_rsqP1rNyoN0Y3CCE4iDdWRwgg-g",
	},
	DepositContractAddress: "0xff50ed3d0ec03ac01d4c79aad74928bff48a7b2b",
	GenesisValidatorsRoot:  "043db0d9a83813551ee2f33450d23797757d430911a9320530ad8a0eabc43efb",
	GenesisEpoch:           152834, // TODO: 152834? 156113? another value?
}

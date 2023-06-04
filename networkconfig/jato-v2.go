package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var JatoV2 = NetworkConfig{
	Name:                 "jato-v2",
	Beacon:               beacon.NewNetwork(spectypes.PraterNetwork),
	Domain:               spectypes.V3Testnet,
	GenesisEpoch:         205852,
	ETH1SyncOffset:       new(big.Int).SetInt64(9120510),
	RegistryContractAddr: "0x9d3F908cB3b132379A97b0E0f8171F0B42756E28",
	Bootnodes: []string{
		"enr:-Li4QMwFYXASubP23bEmzbm5bGEtTZdbYqSQz-K12Hl4NZZlcJi2zE8oZ5LErjnvQ3qlyl2_flDIcIVOhX5mb9UPpv-GAYh8cjRLh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO0oKeSpS0TfgDof9xIGhUTLgnjEg91kVKA5fufSt_CQIN0Y3CCE4mDdWRwgg-h",
	},
}

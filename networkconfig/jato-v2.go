package networkconfig

import (
	"math/big"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
)

var JatoV2 = NetworkConfig{
	Name:         "jato-v2",
	Beacon:       beacon.NewNetwork(spectypes.PraterNetwork),
	Domain:       spectypes.V3Testnet,
	GenesisEpoch: 205852,

	// TODO: should be adjusted to the actual offset once we have a contract
	ETH1SyncOffset: new(big.Int).SetInt64(9015219),
	// TODO: should be adjusted to the actual contract address once we have a contract
	RegistryContractAddr: "0x01",
	Bootnodes: []string{
		"enr:-Li4QMwFYXASubP23bEmzbm5bGEtTZdbYqSQz-K12Hl4NZZlcJi2zE8oZ5LErjnvQ3qlyl2_flDIcIVOhX5mb9UPpv-GAYh8cjRLh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQO0oKeSpS0TfgDof9xIGhUTLgnjEg91kVKA5fufSt_CQIN0Y3CCE4mDdWRwgg-h",
	},
}

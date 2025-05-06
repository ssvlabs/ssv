package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/protocol/v2/blockchain/beacon"
)

var LocalTestnet = NetworkConfig{
	Name:                 "local-testnet",
	Beacon:               beacon.NewLocalTestNetwork(spectypes.LocalTestnetNetwork),
	DomainType:           spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x2},
	GenesisEpoch:         1,
	RegistryContractAddr: "0xBFfF570853d97636b78ebf262af953308924D3D8",
	Bootnodes: []string{
		"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",
	},
	RegistrySyncOffset: new(big.Int).SetInt64(1),
}

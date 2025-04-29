package networkconfig

import (
	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const LocalTestnetName = "local-testnet"

var LocalTestnetSSV = SSVConfig{
	DomainType:           spectypes.DomainType{0x0, 0x0, spectypes.JatoV2NetworkID.Byte(), 0x2},
	RegistryContractAddr: ethcommon.HexToAddress("0xC3CD9A0aE89Fff83b71b58b6512D43F8a41f363D"),
	Bootnodes: []string{
		"enr:-Li4QLR4Y1VbwiqFYKy6m-WFHRNDjhMDZ_qJwIABu2PY9BHjIYwCKpTvvkVmZhu43Q6zVA29sEUhtz10rQjDJkK3Hd-GAYiGrW2Bh2F0dG5ldHOIAAAAAAAAAACEZXRoMpD1pf1CAAAAAP__________gmlkgnY0gmlwhCLdu_SJc2VjcDI1NmsxoQJTcI7GHPw-ZqIflPZYYDK_guurp_gsAFF5Erns3-PAvIN0Y3CCE4mDdWRwgg-h",
	},
	TotalEthereumValidators: TestNetwork.TotalEthereumValidators,
}

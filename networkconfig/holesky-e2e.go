package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

const HoleskyE2EName = "holesky-e2e"

var HoleskyE2ESSV = &SSVConfig{
	DomainType:              spectypes.DomainType{0x0, 0x0, 0xee, 0x1},
	RegistryContractAddr:    ethcommon.HexToAddress("0x58410bef803ecd7e63b23664c586a6db72daf59c"),
	RegistrySyncOffset:      big.NewInt(405579),
	Bootnodes:               []string{},
	MaxF:                    4,
	TotalEthereumValidators: HoleskySSV.TotalEthereumValidators,
	GasLimit36Epoch:         0,
}

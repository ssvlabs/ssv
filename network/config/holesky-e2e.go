package networkconfig

import (
	"math/big"

	ethcommon "github.com/ethereum/go-ethereum/common"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var HoleskyE2ESSV = SSV{
	Name:                 "holesky-e2e",
	GenesisDomainType:    spectypes.DomainType{0x0, 0x0, 0xee, 0x0},
	AlanDomainType:       spectypes.DomainType{0x0, 0x0, 0xee, 0x1},
	GenesisEpoch:         1,
	RegistryContractAddr: ethcommon.HexToAddress("0x58410bef803ecd7e63b23664c586a6db72daf59c"),
	RegistrySyncOffset:   big.NewInt(405579),
	Bootnodes:            []string{},
}
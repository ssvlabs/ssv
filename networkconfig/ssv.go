package networkconfig

import (
	"math/big"

	"github.com/ssvlabs/ssv-spec/types"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./ssv_mock.go -source=./ssv.go

type SSV interface {
	GetDomainType() types.DomainType // using spectypes.DomainType breaks mock generation
}

type SSVConfig struct {
	DomainType           types.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr string // TODO: ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
}

func (ssv SSVConfig) GetDomainType() types.DomainType {
	return ssv.DomainType
}

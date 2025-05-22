package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./ssv_mock.go -source=./ssv.go

type SSV interface {
	GetDomainType() spectypes.DomainType
}

type SSVConfig struct {
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr string // TODO: ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
}

func (ssv SSVConfig) GetDomainType() spectypes.DomainType {
	return ssv.DomainType
}

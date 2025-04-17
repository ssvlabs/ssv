package networkconfig

import (
	"math/big"

	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type SSVConfig struct {
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr string // TODO: ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
}

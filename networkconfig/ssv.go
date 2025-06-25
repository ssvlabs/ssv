package networkconfig

import (
	"math/big"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

type SSVConfig struct {
	DomainType           spectypes.DomainType
	RegistrySyncOffset   *big.Int
	RegistryContractAddr string // TODO: ethcommon.Address
	Bootnodes            []string
	DiscoveryProtocolID  [6]byte
	// GasLimit36Epoch is an epoch when to upgrade from default gas limit value of 30_000_000
	// to 36_000_000.
	GasLimit36Epoch phase0.Epoch
}

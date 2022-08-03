package types

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"sync"
)

var (
	// ShifuTestnet is the domain for shifu testnet
	// NOTE: do not use directly unless you want to check domain,
	//       i.e. use GetDefaultDomain() to get the current domain.
	ShifuTestnet = spectypes.DomainType("shifu")
)

var (
	domain spectypes.DomainType
	once   sync.Once
)

// GetDefaultDomain returns the global domain used across the system
func GetDefaultDomain() spectypes.DomainType {
	once.Do(func() {
		if len(domain) == 0 {
			domain = ShifuTestnet
		}
	})
	return domain
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
func SetDefaultDomain(d spectypes.DomainType) {
	domain = d
}

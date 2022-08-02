package message

import (
	spectypes "github.com/bloxapp/ssv-spec/types"
	"sync"
)

var (
	// ShifuTestnet is the domain for shifu testnet
	ShifuTestnet = spectypes.DomainType("shifu")
)

var (
	domain spectypes.DomainType
	once   sync.Once
)

// GetDefaultDomain returns the global domain used across the system
func GetDefaultDomain() spectypes.DomainType {
	once.Do(func() {
		domain = ShifuTestnet
	})
	return domain
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
func SetDefaultDomain(d spectypes.DomainType) {
	domain = d
}

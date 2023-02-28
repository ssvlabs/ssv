package types

import (
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var (
	domain spectypes.DomainType
	once   sync.Once
)

// GetDefaultDomain returns the global domain used across the system
func GetDefaultDomain() spectypes.DomainType {
	once.Do(func() {
		if len(domain) == 0 {
			domain = spectypes.ShifuV2Testnet
		}
	})
	return domain
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
func SetDefaultDomain(d spectypes.DomainType) {
	domain = d
}

package types

import (
	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/ssvlabs/ssv/networkconfig"
)

// TODO: get rid of singleton, pass domain as a parameter
var (
	domain = genesisspectypes.DomainType(networkconfig.Mainnet.DomainType())
)

// GetDefaultDomain returns the global domain used across the system
// DEPRECATED: use networkconfig.NetworkConfig.Domain instead
func GetDefaultDomain() genesisspectypes.DomainType {
	return domain
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
// DEPRECATED: use networkconfig.NetworkConfig.Domain instead
func SetDefaultDomain(d genesisspectypes.DomainType) {
	domain = d
}

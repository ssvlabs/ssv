package types

import (
	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/networkconfig"

	oldspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
)

// TODO: get rid of singleton, pass domain as a parameter
var (
	domain = networkconfig.Mainnet.GenesisDomainType
)

// GetDefaultDomain returns the global domain used across the system
// DEPRECATED: use networkconfig.NetworkConfig.Domain instead
func GetDefaultDomain() oldspectypes.DomainType {
	return oldspectypes.DomainType(domain)
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
// DEPRECATED: use networkconfig.NetworkConfig.Domain instead
func SetDefaultDomain(d oldspectypes.DomainType) {
	domain = spectypes.DomainType(d)
}

package types

import (
	spectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"

	"github.com/bloxapp/ssv/networkconfig"
)

// TODO: get rid of singleton, pass domain as a parameter
var (
	domain = networkconfig.Mainnet.Domain
)

// GetDefaultDomain returns the global domain used across the system
// DEPRECATED: use networkconfig.NetworkConfig.Domain instead
func GetDefaultDomain() spectypes.DomainType {
	return domain
}

// SetDefaultDomain updates the global domain used across the system
// allows injecting domain for testnets
// DEPRECATED: use networkconfig.NetworkConfig.Domain instead
func SetDefaultDomain(d spectypes.DomainType) {
	domain = d
}

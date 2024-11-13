package networkconfig

import (
	"encoding/json"
	"fmt"

	"github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/ssvlabs/ssv-spec/types"
)

var SupportedConfigs = map[string]SSV{
	MainnetSSV.Name:      MainnetSSV,
	HoleskySSV.Name:      HoleskySSV,
	HoleskyStageSSV.Name: HoleskyStageSSV,
	LocalTestnetSSV.Name: LocalTestnetSSV,
	HoleskyE2ESSV.Name:   HoleskyE2ESSV,
}

const alanForkName = "alan"

func GetNetworkConfigByName(name string) (SSV, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return SSV{}, fmt.Errorf("network not supported: %v", name)
}

// DomainTypeProvider is an interface for getting the domain type based on the current or given epoch.
type DomainTypeProvider interface {
	DomainType() spectypes.DomainType
	NextDomainType() spectypes.DomainType
	DomainTypeAtEpoch(epoch phase0.Epoch) spectypes.DomainType
}

type NetworkConfig struct {
	SSV
	Beacon
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return fmt.Sprintf("<malformed: %v>", err)
	}

	return string(b)
}

func (n NetworkConfig) PastAlanFork() bool {
	return n.EstimatedCurrentEpoch() >= n.AlanForkEpoch
}

// DomainType returns current domain type based on the current fork.
func (n NetworkConfig) DomainType() spectypes.DomainType {
	return n.DomainTypeAtEpoch(n.EstimatedCurrentEpoch())
}

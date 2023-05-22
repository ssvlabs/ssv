package networkconfig

import (
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var SupportedConfigs = map[string]spectypes.SSVNetwork{
	Mainnet.Name: Mainnet,
	Jato.Name:    Jato,
}

func GetNetworkByName(name string) (spectypes.SSVNetwork, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return spectypes.SSVNetwork{}, fmt.Errorf("network not supported: %v", name)
}

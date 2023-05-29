package networkconfig

import (
	"encoding/json"
	"fmt"
	"math/big"

	spec "github.com/attestantio/go-eth2-client/spec/phase0"
	spectypes "github.com/bloxapp/ssv-spec/types"
)

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:     Mainnet,
	JatoV2Stage.Name: JatoV2Stage,
}

func GetNetworkByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

type NetworkConfig struct {
	Name                    string
	spectypes.BeaconNetwork // TODO: unembed
	Domain                  spectypes.DomainType
	GenesisEpoch            spec.Epoch
	ETH1SyncOffset          *big.Int
	RegistryContractAddr    string
	Bootnodes               []string
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

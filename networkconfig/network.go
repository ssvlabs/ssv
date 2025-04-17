package networkconfig

import (
	"encoding/json"
	"fmt"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./network_mock.go -source=./network.go

var SupportedConfigs = map[string]NetworkConfig{
	Mainnet.Name:      Mainnet,
	Holesky.Name:      Holesky,
	HoleskyStage.Name: HoleskyStage,
	LocalTestnet.Name: LocalTestnet,
	HoleskyE2E.Name:   HoleskyE2E,
	Hoodi.Name:        Hoodi,
	HoodiStage.Name:   HoodiStage,
	Sepolia.Name:      Sepolia,
}

const forkName = "alan"

func GetNetworkConfigByName(name string) (NetworkConfig, error) {
	if network, ok := SupportedConfigs[name]; ok {
		return network, nil
	}

	return NetworkConfig{}, fmt.Errorf("network not supported: %v", name)
}

type Network interface {
	NetworkName() string
	Beacon
	SSV
}

type NetworkConfig struct {
	Name string
	BeaconConfig
	SSVConfig
}

func (n NetworkConfig) String() string {
	b, err := json.MarshalIndent(n, "", "\t")
	if err != nil {
		return "<malformed>"
	}

	return string(b)
}

func (n NetworkConfig) NetworkName() string {
	return fmt.Sprintf("%s:%s", n.Name, forkName)
}

package networkconfig

import (
	"encoding/json"
	"fmt"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./network_mock.go -source=./network.go

const forkName = "alan"

type Network interface {
	NetworkName() string
	GasLimit36Fork() bool
	NetworkTopologyFork() bool
	Beacon
	SSV
}

type NetworkConfig struct {
	Name string
	*BeaconConfig
	*SSVConfig
}

func (n NetworkConfig) String() string {
	jsonBytes, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}

	return string(jsonBytes)
}

func (n NetworkConfig) NetworkName() string {
	return fmt.Sprintf("%s:%s", n.Name, forkName)
}

func (n NetworkConfig) GasLimit36Fork() bool {
	return n.EstimatedCurrentEpoch() >= n.SSVConfig.Forks.GasLimit36
}

func (n NetworkConfig) NetworkTopologyFork() bool {
	return n.EstimatedCurrentEpoch() >= n.SSVConfig.Forks.NetworkTopology
}

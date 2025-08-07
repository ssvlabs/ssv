package networkconfig

import (
	"encoding/json"
	"fmt"
)

//go:generate go tool -modfile=../tool.mod mockgen -package=networkconfig -destination=./network_mock.go -source=./network.go

const forkName = "alan"

type Network interface {
	NetworkName() string
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

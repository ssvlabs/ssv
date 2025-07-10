package networkconfig

import (
	"encoding/json"
	"fmt"
)

const forkName = "alan"

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

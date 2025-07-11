package networkconfig

import (
	"encoding/json"
	"fmt"
)

type Network struct {
	*Beacon
	*SSV
}

func (n Network) String() string {
	jsonBytes, err := json.Marshal(n)
	if err != nil {
		panic(err)
	}

	return string(jsonBytes)
}

// StorageName returns a config name used to make sure the stored network doesn't differ.
// It combines the network name with fork name.
func (n Network) StorageName() string {
	return fmt.Sprintf("%s:%s", n.SSVName, n.CurrentSSVFork().Name)
}

func (n Network) CurrentSSVFork() SSVFork {
	return n.SSV.ForkAtEpoch(n.EstimatedCurrentEpoch())
}

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

const alanForkName = "alan"

// StorageName returns a config name used to make sure the stored network doesn't differ.
// It combines the network name with fork name.
func (n Network) StorageName() string {
	return fmt.Sprintf("%s:%s", n.SSV.Name, alanForkName) // TODO: decide what forks change DB fork name
}

func (n Network) GasLimit36Fork() bool {
	return n.EstimatedCurrentEpoch() >= n.SSV.Forks.GasLimit36
}

func (n Network) AggregatorCommitteeFork() bool {
	return n.EstimatedCurrentEpoch() >= n.SSV.Forks.AggregatorCommittee
}

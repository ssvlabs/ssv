package networkconfig

import (
	"encoding/json"
	"fmt"
)

const forkName = "alan"

type Network struct {
	Name string
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

func (n Network) NetworkName() string {
	return fmt.Sprintf("%s:%s", n.Name, forkName)
}

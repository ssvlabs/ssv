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

// storageFormatVersion is the storage compatibility version.
// Bump it only when on-disk data compatibility changes.
const storageFormatVersion = "alan"

// StorageName returns a config name used to make sure the stored network doesn't differ.
// It combines the network name with a storage-compatibility version.
func (n Network) StorageName() string {
	return fmt.Sprintf("%s:%s", n.SSV.Name, storageFormatVersion)
}

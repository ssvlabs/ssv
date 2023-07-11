package localevents

import (
	"os"
	"path/filepath"

	ethtypes "github.com/ethereum/go-ethereum/core/types"
	"gopkg.in/yaml.v3"
)

// Event represents an eth1 event log in the system
type Event struct {
	// Log is the raw event log
	Log ethtypes.Log
	// Name is the event name used for internal representation.
	Name string
	// Data is the parsed event
	Data interface{}
}

func Load(path string) ([]*Event, error) {
	yamlFile, err := os.ReadFile(filepath.Clean(path))
	if err != nil {
		return nil, err
	}

	var events []*Event
	if err := yaml.Unmarshal(yamlFile, &events); err != nil {
		return nil, err
	}

	return events, nil
}

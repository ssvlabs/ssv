package scenarios

import (
	"github.com/bloxapp/ssv/ibft"
	"github.com/bloxapp/ssv/storage/collections"
)

// IScenario is an interface for simulator scenarios
type IScenario interface {
	// Start is a blocking call to start scenario
	Start(nodes []ibft.Controller, dbs []collections.Iibft)
}

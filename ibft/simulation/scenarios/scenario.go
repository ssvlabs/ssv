package scenarios

import (
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	"github.com/bloxapp/ssv/storage/collections"
)

// IScenario is an interface for simulator scenarios
type IScenario interface {
	// Start is a blocking call to start scenario
	Start(nodes []controller.Controller, dbs []collections.Iibft)
}

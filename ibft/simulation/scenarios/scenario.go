package scenarios

import (
	ibft "github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

// IScenario is an interface for simulator scenarios
type IScenario interface {
	// Start is a blocking call to start scenario
	Start(nodes []ibft.IController, dbs []qbftstorage.QBFTStore)
}

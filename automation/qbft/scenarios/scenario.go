package scenarios

import (
	"context"
	"encoding/hex"

	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v1/message"
	"github.com/bloxapp/ssv/protocol/v1/qbft/controller"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"go.uber.org/zap"
)

type ScenarioFactory func(name string) Scenario

// ScenarioContext is the context object that is passed in execution
type ScenarioContext struct {
	Ctx         context.Context
	LocalNet    *p2pv1.LocalNet
	Stores      []qbftstorage.QBFTStore
	KeyManagers []beacon.KeyManager
}

type scenarioCfg interface {
	// NumOfOperators returns the desired number of operators for the test
	NumOfOperators() int
	// NumOfExporters returns the desired number of operators for the test
	NumOfExporters() int
}

// Scenario represents a testplan for a specific scenario
type Scenario interface {
	scenarioCfg
	// Name is the name of the scenario
	Name() string
	// PreExecution is invoked prior to the scenario, used for setup
	PreExecution(ctx *ScenarioContext) error
	// Execute is the actual test scenario to run
	Execute(ctx *ScenarioContext) error
	// PostExecution is invoked after execution, used for cleanup etc.
	PostExecution(ctx *ScenarioContext) error
}

type router struct {
	logger      *zap.Logger
	controllers controller.Controllers
}

func (r *router) Route(message message.SSVMessage) {
	if err := r.controllers.ControllerForIdentifier(message.GetIdentifier()).ProcessMsg(&message); err != nil {
		r.logger.Error("failed to process message",
			zap.String("identifier", hex.EncodeToString(message.GetIdentifier())))
	}
}

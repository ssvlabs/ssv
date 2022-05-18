package runner

import (
	"context"
	"github.com/bloxapp/ssv/storage/basedb"

	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
)

// TODO:
// Add following cases for every scenario:
// the requesting node's version is new, others' versions are old
// the requesting node's version is new, others' versions are mixed of new and old
// the requesting node's version is old, others' versions are new
// the requesting node's version is old, others' versions are mixed of new and old

type ScenarioFactory func(name string) Scenario

// ScenarioContext is the context object that is passed in execution
type ScenarioContext struct {
	Ctx         context.Context
	LocalNet    *p2pv1.LocalNet
	Stores      []qbftstorage.QBFTStore
	KeyManagers []beacon.KeyManager
	DBs         []basedb.IDb
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

package runner

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/automation/commons"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/protocol/v1/sync/handlers"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
)

// TODO:
// Add following cases for every scenario:
// the requesting node's version is new, others' versions are old
// the requesting node's version is new, others' versions are mixed of new and old
// the requesting node's version is old, others' versions are new
// the requesting node's version is old, others' versions are mixed of new and old

// ScenarioFactory creates Scenario instances
type ScenarioFactory func(name string) Scenario

// ScenarioContext is the context object that is passed in execution
type ScenarioContext struct {
	Ctx         context.Context
	LocalNet    *p2pv1.LocalNet
	Stores      []qbftstorage.QBFTStore
	KeyManagers []beacon.KeyManager
	DBs         []basedb.IDb
}

// Bootstrapper bootstraps the given scenario
type Bootstrapper func(ctx context.Context, logger *zap.Logger, scenario Scenario) (*ScenarioContext, error)

// QBFTScenarioBootstrapper bootstraps qbft scenarios
func QBFTScenarioBootstrapper() Bootstrapper {
	return func(ctx context.Context, plogger *zap.Logger, scenario Scenario) (*ScenarioContext, error) {
		loggerFactory := func(s string) *zap.Logger {
			return plogger.With(zap.String("who", s))
		}
		logger := loggerFactory(fmt.Sprintf("Bootstrap/%s", scenario.Name()))
		logger.Info("creating resources")

		dbs := make([]basedb.IDb, 0)
		for i := 0; i < scenario.NumOfOperators(); i++ {
			db, err := storage.GetStorageFactory(basedb.Options{
				Type:   "badger-memory",
				Path:   "",
				Logger: logger,
			})
			if err != nil {
				logger.Panic("could not setup storage", zap.Error(err))
			}
			dbs = append(dbs, db)
		}
		ln, err := p2pv1.CreateAndStartLocalNet(ctx, loggerFactory, forksprotocol.V0ForkVersion, scenario.NumOfOperators(), scenario.NumOfOperators()/2, false)
		if err != nil {
			return nil, err
		}
		stores := make([]qbftstorage.QBFTStore, 0)
		kms := make([]beacon.KeyManager, 0)
		for i, node := range ln.Nodes {
			store := qbftstorage.NewQBFTStore(dbs[i], loggerFactory(fmt.Sprintf("qbft-store-%d", i+1)), "attestations")
			stores = append(stores, store)
			km := commons.NewTestSigner()
			kms = append(kms, km)
			node.RegisterHandlers(p2pprotocol.WithHandler(
				p2pprotocol.LastDecidedProtocol,
				handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("decided-handler-%d", i+1)), store, node),
			), p2pprotocol.WithHandler(
				p2pprotocol.LastChangeRoundProtocol,
				handlers.LastChangeRoundHandler(loggerFactory(fmt.Sprintf("changeround-handler-%d", i+1)), store, node),
			), p2pprotocol.WithHandler(
				p2pprotocol.DecidedHistoryProtocol,
				handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-handler-%d", i+1)), store, node, 25),
			))
		}

		return &ScenarioContext{
			Ctx:         ctx,
			LocalNet:    ln,
			Stores:      stores,
			KeyManagers: kms,
			DBs:         dbs,
		}, nil
	}
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

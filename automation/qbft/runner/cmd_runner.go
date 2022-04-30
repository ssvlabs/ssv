package runner

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/commons"
	"github.com/bloxapp/ssv/ibft/sync/v1/handlers"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v1/blockchain/beacon"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v1/p2p"
	qbftstorage "github.com/bloxapp/ssv/protocol/v1/qbft/storage"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
)

func Start(logger *zap.Logger, scenario Scenario) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

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

	if err := run(ctx, dbs, scenario); err != nil {
		logger.Panic("could not run scenario", zap.Error(err))
	}
}

func run(pctx context.Context, dbs []basedb.IDb, scenario Scenario) error {
	ctx, cancel := context.WithCancel(pctx)
	defer cancel()
	loggerFactory := func(s string) *zap.Logger {
		return logex.GetLogger(zap.String("who", s))
	}
	logger := loggerFactory(fmt.Sprintf("RUNNER/%s", scenario.Name()))
	logger.Info("creating resources")
	ln, err := p2pv1.CreateAndStartLocalNet(ctx, loggerFactory, forksprotocol.V0ForkVersion, scenario.NumOfOperators(), scenario.NumOfOperators()/2, false)
	if err != nil {
		return err
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

	sctx := ScenarioContext{
		Ctx:         ctx,
		LocalNet:    ln,
		Stores:      stores,
		KeyManagers: kms,
	}
	logger.Info("all resources were created, starting pre-execution of the scenario")
	if err := scenario.PreExecution(&sctx); err != nil {
		return err
	}
	logger.Info("executing scenario")
	if err := scenario.Execute(&sctx); err != nil {
		return err
	}
	logger.Info("running post-execution of the scenario")
	if err := scenario.PostExecution(&sctx); err != nil {
		return err
	}
	logger.Info("done")

	return nil
}

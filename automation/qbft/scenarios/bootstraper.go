package scenarios

import (
	"context"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	spectestingutils "github.com/bloxapp/ssv-spec/types/testingutils"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/automation/qbft/runner"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	p2pv1 "github.com/bloxapp/ssv/network/p2p"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	p2pprotocol "github.com/bloxapp/ssv/protocol/v2/p2p"
	"github.com/bloxapp/ssv/protocol/v2/sync/handlers"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

// QBFTScenarioBootstrapper bootstraps qbft scenarios
func QBFTScenarioBootstrapper() runner.Bootstrapper {
	return func(ctx context.Context, plogger *zap.Logger, scenario runner.Scenario) (*runner.ScenarioContext, error) {
		loggerFactory := func(s string) *zap.Logger {
			return plogger.With(zap.String("who", s))
		}
		logger := loggerFactory(fmt.Sprintf("Bootstrap/%s", scenario.Name()))
		logger.Info("creating resources")

		scenarioConfig := scenario.Config()
		totalNodes := scenarioConfig.Operators + scenarioConfig.FullNodes
		useDiscV5 := scenarioConfig.BootNodes > 0

		dbs := make([]basedb.IDb, 0)
		for i := 0; i < totalNodes; i++ {
			db, err := storage.GetStorageFactory(basedb.Options{
				Type:   "badger-memory",
				Path:   "",
				Logger: zap.L(),
			})
			if err != nil {
				logger.Panic("could not setup storage", zap.Error(err))
			}

			dbs = append(dbs, db)
		}
		forkVersion := forksprotocol.GenesisForkVersion

		ln, err := p2pv1.CreateAndStartLocalNet(ctx, loggerFactory, forkVersion, totalNodes, totalNodes/2, useDiscV5)
		if err != nil {
			return nil, err
		}

		stores := make([]*qbftstorage.QBFTStores, 0)
		kms := make([]spectypes.KeyManager, 0)
		for i, node := range ln.Nodes {
			store := qbftstorage.New(dbs[i], loggerFactory(fmt.Sprintf("qbft-store-%d", i+1)), "attestations", forkVersion)

			storageMap := qbftstorage.NewStores()
			storageMap.Add(spectypes.BNRoleAttester, store)
			storageMap.Add(spectypes.BNRoleProposer, store)
			storageMap.Add(spectypes.BNRoleAggregator, store)
			storageMap.Add(spectypes.BNRoleSyncCommittee, store)
			storageMap.Add(spectypes.BNRoleSyncCommitteeContribution, store)

			stores = append(stores, storageMap)
			km := spectestingutils.NewTestingKeyManager()
			kms = append(kms, km)
			node.RegisterHandlers(p2pprotocol.WithHandler(
				p2pprotocol.LastDecidedProtocol,
				handlers.LastDecidedHandler(loggerFactory(fmt.Sprintf("decided-handler-%d", i+1)), storageMap, node),
			), p2pprotocol.WithHandler(
				p2pprotocol.DecidedHistoryProtocol,
				handlers.HistoryHandler(loggerFactory(fmt.Sprintf("history-handler-%d", i+1)), storageMap, node, 25),
			))
		}

		return &runner.ScenarioContext{
			Ctx:         ctx,
			LocalNet:    ln,
			Stores:      stores,
			KeyManagers: kms,
			DBs:         dbs,
		}, nil
	}
}

package utils

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.uber.org/zap"
)

func TestingStores() *qbftstorage.QBFTStores {
	//logger := logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{})
	logger := zap.L()
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Logger:    logger,
		Ctx:       context.TODO(),
	})
	if err != nil {
		panic(err)
	}

	stores := qbftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, qbftstorage.New(db, logger, spectypes.BNRoleAttester.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleProposer, qbftstorage.New(db, logger, spectypes.BNRoleProposer.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleAggregator, qbftstorage.New(db, logger, spectypes.BNRoleAggregator.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleSyncCommittee, qbftstorage.New(db, logger, spectypes.BNRoleSyncCommittee.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleSyncCommitteeContribution, qbftstorage.New(db, logger, spectypes.BNRoleSyncCommitteeContribution.String(), forksprotocol.GenesisForkVersion))

	return stores
}

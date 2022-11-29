package utils

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap/zapcore"
)

func TestingStores() *qbftstorage.QBFTStores {
	db, err := storage.GetStorageFactory(basedb.Options{
		Type:      "badger-memory",
		Path:      "",
		Reporting: false,
		Logger:    logex.Build("", zapcore.DebugLevel, &logex.EncodingConfig{}),
		Ctx:       context.TODO(),
	})
	if err != nil {
		panic(err)
	}

	stores := qbftstorage.NewStores()
	stores.Add(spectypes.BNRoleAttester, qbftstorage.New(db, nil, spectypes.BNRoleAttester.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleProposer, qbftstorage.New(db, nil, spectypes.BNRoleProposer.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleAggregator, qbftstorage.New(db, nil, spectypes.BNRoleAggregator.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleSyncCommittee, qbftstorage.New(db, nil, spectypes.BNRoleSyncCommittee.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleSyncCommitteeContribution, qbftstorage.New(db, nil, spectypes.BNRoleSyncCommitteeContribution.String(), forksprotocol.GenesisForkVersion))

	return stores
}

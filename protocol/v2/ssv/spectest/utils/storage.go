package utils

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	storage2 "github.com/bloxapp/ssv/ibft/storage"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap/zapcore"
)

func TestingStores() *storage2.QBFTStores {
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

	stores := storage2.NewStores()
	stores.Add(spectypes.BNRoleAttester, storage2.New(db, nil, spectypes.BNRoleAttester.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleProposer, storage2.New(db, nil, spectypes.BNRoleProposer.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleAggregator, storage2.New(db, nil, spectypes.BNRoleAggregator.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleSyncCommittee, storage2.New(db, nil, spectypes.BNRoleSyncCommittee.String(), forksprotocol.GenesisForkVersion))
	stores.Add(spectypes.BNRoleSyncCommitteeContribution, storage2.New(db, nil, spectypes.BNRoleSyncCommitteeContribution.String(), forksprotocol.GenesisForkVersion))

	return stores
}

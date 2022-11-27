package utils

import (
	"context"
	spectypes "github.com/bloxapp/ssv-spec/types"
	storagev2 "github.com/bloxapp/ssv/ibft/storage/v2"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	qbftstorage2 "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap/zapcore"
)

func TestingStorage() map[spectypes.BeaconRole]qbftstorage2.QBFTStore {
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

	return map[spectypes.BeaconRole]qbftstorage2.QBFTStore{
		spectypes.BNRoleAttester:                  storagev2.New(db, nil, spectypes.BNRoleAttester.String(), forksprotocol.GenesisForkVersion),
		spectypes.BNRoleProposer:                  storagev2.New(db, nil, spectypes.BNRoleProposer.String(), forksprotocol.GenesisForkVersion),
		spectypes.BNRoleAggregator:                storagev2.New(db, nil, spectypes.BNRoleAggregator.String(), forksprotocol.GenesisForkVersion),
		spectypes.BNRoleSyncCommittee:             storagev2.New(db, nil, spectypes.BNRoleSyncCommittee.String(), forksprotocol.GenesisForkVersion),
		spectypes.BNRoleSyncCommitteeContribution: storagev2.New(db, nil, spectypes.BNRoleSyncCommitteeContribution.String(), forksprotocol.GenesisForkVersion),
	}
}

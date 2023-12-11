package testing

import (
	"context"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"

	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
)

var db basedb.Database
var dbOnce sync.Once

func getDB(logger *zap.Logger) basedb.Database {
	dbOnce.Do(func() {
		dbInstance, err := kv.NewInMemory(context.TODO(), logger, basedb.Options{})
		if err != nil {
			panic(err)
		}
		db = dbInstance
	})
	return db
}

var allRoles = []spectypes.BeaconRole{
	spectypes.BNRoleAttester,
	spectypes.BNRoleProposer,
	spectypes.BNRoleAggregator,
	spectypes.BNRoleSyncCommittee,
	spectypes.BNRoleSyncCommitteeContribution,
	spectypes.BNRoleValidatorRegistration,
	spectypes.BNRoleVoluntaryExit,
}

func TestingStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

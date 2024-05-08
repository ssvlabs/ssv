package testing

import (
	"context"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"go.uber.org/zap"
)

var db basedb.Database
var dbOnce sync.Once

func getDB(logger *zap.Logger) basedb.Database {
	dbOnce.Do(func() {
		dbInstance, err := kv.NewInMemory(logger, basedb.Options{
			Ctx: context.TODO(),
		})
		if err != nil {
			panic(err)
		}
		db = dbInstance
	})
	return db
}

var allRoles = []spectypes.RunnerRole{
	spectypes.RoleCommittee,
	spectypes.RoleProposer,
	spectypes.RoleAggregator,
	spectypes.RoleSyncCommitteeContribution,
	spectypes.RoleValidatorRegistration,
	spectypes.RoleVoluntaryExit,
}

func TestingStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

package testing

import (
	"context"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
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

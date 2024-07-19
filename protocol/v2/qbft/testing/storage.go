package testing

import (
	"context"
	"sync"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/exporter/convert"
	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/kv"
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

var allRoles = []convert.RunnerRole{
	convert.RoleAttester,
	convert.RoleAggregator,
	convert.RoleProposer,
	convert.RoleSyncCommitteeContribution,
	convert.RoleSyncCommittee,
	convert.RoleValidatorRegistration,
	convert.RoleVoluntaryExit,
	convert.RoleCommittee,
}

func TestingStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

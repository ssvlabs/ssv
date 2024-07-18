package testing

import (
	"context"
	"sync"

	"github.com/ssvlabs/ssv/exporter/convert"

	"go.uber.org/zap"

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
	convert.RoleCommittee,
	convert.RoleProposer,
	convert.RoleAggregator,
	convert.RoleSyncCommitteeContribution,
	convert.RoleValidatorRegistration,
	convert.RoleVoluntaryExit,
	convert.RoleAttester,      // TODO: check if using RoleAttester is correct
	convert.RoleSyncCommittee, // TODO: check if using RoleSyncCommittee is correct
}

func TestingStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

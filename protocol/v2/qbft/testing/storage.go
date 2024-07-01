package testing

import (
	"context"
	"github.com/ssvlabs/ssv/exporter/exporter_message"
	"sync"

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

var allRoles = []exporter_message.RunnerRole{
	exporter_message.RoleCommittee,
	exporter_message.RoleProposer,
	exporter_message.RoleAggregator,
	exporter_message.RoleSyncCommitteeContribution,
	exporter_message.RoleValidatorRegistration,
	exporter_message.RoleVoluntaryExit,
}

func TestingStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

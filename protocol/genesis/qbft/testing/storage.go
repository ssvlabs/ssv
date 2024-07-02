package testing

import (
	"context"
	"sync"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
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

var allRoles = []genesisspectypes.BeaconRole{
	genesisspectypes.BNRoleAttester,
	genesisspectypes.BNRoleProposer,
	genesisspectypes.BNRoleAggregator,
	genesisspectypes.BNRoleSyncCommittee,
	genesisspectypes.BNRoleSyncCommitteeContribution,
	genesisspectypes.BNRoleValidatorRegistration,
	genesisspectypes.BNRoleVoluntaryExit,
}

func TestingStores(logger *zap.Logger) *qbftstorage.QBFTStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

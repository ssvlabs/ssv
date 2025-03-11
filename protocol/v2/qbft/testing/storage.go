package testing

import (
	"context"
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"
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

var allRoles = []spectypes.BeaconRole{
	spectypes.BNRoleAttester,
	spectypes.BNRoleAggregator,
	spectypes.BNRoleProposer,
	spectypes.BNRoleSyncCommitteeContribution,
	spectypes.BNRoleSyncCommittee,
	spectypes.BNRoleValidatorRegistration,
	spectypes.BNRoleVoluntaryExit,
}

func TestingStores(logger *zap.Logger) *qbftstorage.ParticipantStores {
	return qbftstorage.NewStoresFromRoles(getDB(logger), allRoles...)
}

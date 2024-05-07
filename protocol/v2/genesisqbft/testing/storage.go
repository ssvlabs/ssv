package testing

import (
	"context"
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"
	qbftstorage "github.com/bloxapp/ssv/ibft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"go.uber.org/zap"

	genesisspectypes "github.com/ssvlabs/ssv-spec-pre-cc/types"
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
	roles := []spectypes.RunnerRole{
		spectypes.RunnerRole(spectypes.BNRoleAttester),
		spectypes.RunnerRole(spectypes.BNRoleProposer),
		spectypes.RunnerRole(spectypes.BNRoleAggregator),
		spectypes.RunnerRole(spectypes.BNRoleSyncCommittee),
		spectypes.RunnerRole(spectypes.BNRoleSyncCommitteeContribution),
		spectypes.RunnerRole(spectypes.BNRoleValidatorRegistration),
		spectypes.RunnerRole(spectypes.BNRoleVoluntaryExit),
	}
	return qbftstorage.NewStoresFromRoles(getDB(logger), roles...)
}

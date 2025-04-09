package testing

import (
	"context"
	"sync"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"go.uber.org/zap"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/slotticker"
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
	slotTickerProvider := func() slotticker.SlotTicker {
		return slotticker.New(logger, slotticker.Config{
			SlotDuration: 5 * time.Second,
			GenesisTime:  time.Now(),
		})
	}
	return qbftstorage.NewStoresFromRoles(logger, networkconfig.HoleskyStage, getDB(logger), slotTickerProvider, allRoles...)
}

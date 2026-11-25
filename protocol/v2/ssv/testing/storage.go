package testing

import (
	"context"
	"sync"

	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/protocol/v2/message"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
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

func newStoresFromRoles(logger *zap.Logger, db basedb.Database, roles ...spectypes.BeaconRole) *qbftstorage.ParticipantStores {
	stores := qbftstorage.NewStores()
	for _, role := range roles {
		stores.Add(role, qbftstorage.New(logger, db, role))
	}
	return stores
}

func testingStores(logger *zap.Logger) *qbftstorage.ParticipantStores {
	return newStoresFromRoles(logger, getDB(logger), message.BeaconRoles()...)
}

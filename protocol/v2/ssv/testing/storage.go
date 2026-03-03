package testing

import (
	"context"

	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/pebble"
)

func newDB(ctx context.Context, logger *zap.Logger) basedb.Database {
	db, err := pebble.NewTemporary(logger, basedb.Options{})
	if err != nil {
		panic(err)
	}

	if ctx != nil {
		go func() {
			<-ctx.Done()
			_ = db.Close()
		}()
	}

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

func newStoresFromRoles(logger *zap.Logger, db basedb.Database, roles ...spectypes.BeaconRole) *qbftstorage.ParticipantStores {
	stores := qbftstorage.NewStores()
	for _, role := range roles {
		stores.Add(role, qbftstorage.New(logger, db, role))
	}
	return stores
}

func testingStores(ctx context.Context, logger *zap.Logger) *qbftstorage.ParticipantStores {
	return newStoresFromRoles(logger, newDB(ctx, logger), allRoles...)
}

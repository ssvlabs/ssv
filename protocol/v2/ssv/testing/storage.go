package testing

import (
	"testing"

	"go.uber.org/zap"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	qbftstorage "github.com/ssvlabs/ssv/ibft/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/pebble"
)

func newDB(t *testing.T, logger *zap.Logger) basedb.Database {
	t.Helper()

	db, err := pebble.NewTemporary(logger, basedb.Options{})
	if err != nil {
		t.Fatalf("create temporary pebble db: %v", err)
	}

	t.Cleanup(func() { _ = db.Close() })

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

func testingStores(t *testing.T, logger *zap.Logger) *qbftstorage.ParticipantStores {
	t.Helper()
	return newStoresFromRoles(logger, newDB(t, logger), allRoles...)
}

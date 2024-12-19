package migrations

import (
	"context"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	ibftstorage "github.com/ssvlabs/ssv/ibft/storage"
	qbftstorage "github.com/ssvlabs/ssv/protocol/v2/qbft/storage"
	"go.uber.org/zap"
)

// This migration removes leftover key for highest decided
var migration_5_highest_instance = Migration{
	Name: "migration_5_highest_instance",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {

		storageRoles := []spectypes.BeaconRole{
			spectypes.BNRoleAttester,
			spectypes.BNRoleProposer,
			spectypes.BNRoleSyncCommittee,
			spectypes.BNRoleAggregator,
			spectypes.BNRoleSyncCommitteeContribution,
			spectypes.BNRoleValidatorRegistration,
			spectypes.BNRoleVoluntaryExit,
		}

		stores := ibftstorage.NewStoresFromRoles(opt.Db, storageRoles...)

		return stores.Each(func(role spectypes.BeaconRole, store qbftstorage.ParticipantStore) error {
			return store.CleanAllInstances()
		})
	},
}

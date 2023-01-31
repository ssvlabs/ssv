package migrations

import (
	"context"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var migrationCleanRegistryDataShifuV2 = Migration{
	Name: "migration_13_clean_registry_data_shifu_v2",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		stores := opt.getRegistryStores()
		for _, store := range stores {
			err := store.CleanRegistryData()
			if err != nil {
				return err
			}
		}

		err := opt.ibftStorage(spectypes.BNRoleAttester.String()).CleanRegistryData()
		if err != nil {
			return err
		}
		err = opt.ibftStorage(spectypes.BNRoleProposer.String()).CleanRegistryData()
		if err != nil {
			return err
		}
		err = opt.ibftStorage(spectypes.BNRoleAggregator.String()).CleanRegistryData()
		if err != nil {
			return err
		}
		err = opt.ibftStorage(spectypes.BNRoleSyncCommittee.String()).CleanRegistryData()
		if err != nil {
			return err
		}
		err = opt.ibftStorage(spectypes.BNRoleSyncCommitteeContribution.String()).CleanRegistryData()
		if err != nil {
			return err
		}

		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}

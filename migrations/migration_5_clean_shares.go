package migrations

import (
	"context"
	"github.com/pkg/errors"
)

var migrationCleanValidatorRegistryData = Migration{
	Name: "migration_5_clean_validator_registry_data",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		nodeStorage := opt.nodeStorage()
		err := nodeStorage.CleanRegistryData()
		if err != nil {
			return errors.Wrap(err, "could not clean node registry data")
		}

		validatorStorage := opt.validatorStorage()
		err = validatorStorage.CleanRegistryData()
		if err != nil {
			return errors.Wrap(err, "could not clean validator registry data")
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}

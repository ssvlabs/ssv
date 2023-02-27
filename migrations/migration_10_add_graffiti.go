package migrations

import (
	"context"
	"go.uber.org/zap"
)

var migrationAddGraffiti = Migration{
	Name: "migration_10_add_graffiti",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		validatorStorage := opt.validatorStorage()
		shares, err := validatorStorage.GetAllValidatorShares(logger)
		if err != nil {
			return err
		}

		for _, share := range shares {
			share.Graffiti = []byte("ssv.network")
			if err := validatorStorage.SaveValidatorShare(logger, share); err != nil {
				return err
			}
		}
		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}

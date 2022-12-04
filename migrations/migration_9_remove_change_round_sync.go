package migrations

import (
	"context"

	spectypes "github.com/bloxapp/ssv-spec/types"
)

var migrationRemoveChangeRoundSync = Migration{
	Name: "migration_9_remove_change_round_sync",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		validatorStorage := opt.validatorStorage()
		shares, err := validatorStorage.GetAllValidatorShares()
		if err != nil {
			return err
		}

		qbftStorage := opt.qbftStorage()
		for _, share := range shares {
			messageID := spectypes.NewMsgID(share.ValidatorPubKey, spectypes.BNRoleAttester)
			if err := qbftStorage.CleanAllInstances(messageID[:]); err != nil {
				return err
			}
		}

		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}

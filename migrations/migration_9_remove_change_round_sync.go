package migrations

import (
	"context"
	"fmt"
	"go.uber.org/zap"

	spectypes "github.com/bloxapp/ssv-spec/types"

	"github.com/bloxapp/ssv/storage/basedb"
)

var migrationRemoveChangeRoundSync = Migration{
	Name: "migration_9_remove_change_round_sync",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		validatorStorage := opt.validatorStorage()
		shares, err := validatorStorage.GetAllValidatorShares(logger)
		if err != nil {
			return err
		}

		for _, share := range shares {
			role := spectypes.BNRoleAttester
			messageID := spectypes.NewMsgID(share.ValidatorPubKey, role)
			if err := cleanLastChangeRound(opt.Db, []byte(role.String()), messageID[:]); err != nil {
				return err
			}
		}

		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}

func cleanLastChangeRound(db basedb.IDb, prefix []byte, identifier []byte) error {
	const lastChangeRoundKey = "last_change_round"
	prefix = append(prefix, identifier[:]...)
	prefix = append(prefix, []byte(lastChangeRoundKey)...)

	_, err := db.DeleteByPrefix(prefix)
	if err != nil {
		return fmt.Errorf("failed to remove last change round: %w", err)
	}

	return nil
}

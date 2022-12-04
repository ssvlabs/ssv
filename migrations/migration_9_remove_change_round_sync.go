package migrations

import (
	"context"
	"encoding/hex"
	"fmt"

	spectypes "github.com/bloxapp/ssv-spec/types"
	"go.uber.org/zap"
)

var migrationRemoveChangeRoundSync = Migration{
	Name: "migration_9_remove_change_round_sync",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		validatorStorage := opt.validatorStorage()
		shares, err := validatorStorage.GetAllValidatorShares()
		if err != nil {
			return err
		}

		for _, share := range shares {
			role := spectypes.BNRoleAttester
			messageID := spectypes.NewMsgID(share.ValidatorPubKey, role)
			if err := cleanLastChangeRound(opt, []byte(role.String()), messageID[:]); err != nil {
				return err
			}
		}

		return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
	},
}

func cleanLastChangeRound(opt Options, prefix []byte, identifier []byte) error {
	const lastChangeRoundKey = "last_change_round"
	prefix = append(prefix, identifier[:]...)
	prefix = append(prefix, []byte(lastChangeRoundKey)...)

	n, err := opt.Db.DeleteByPrefix(prefix)
	if err != nil {
		return fmt.Errorf("failed to remove last change round: %w", err)
	}

	opt.Logger.Debug("removed last change round", zap.Int("count", n),
		zap.String("identifier", hex.EncodeToString(identifier)))

	return nil
}

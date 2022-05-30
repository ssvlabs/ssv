package migrations

import (
	"context"
	"github.com/bloxapp/ssv/protocol/v1/message"
)

var migrationCleanV1Decided = Migration{
	Name: "migration_6_clean_v1_decided",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		shares, err := opt.validatorStorage().GetAllValidatorShares()
		if err != nil {
			return err
		}
		var pks []message.Identifier
		for _, share := range shares {
			pks = append(pks, message.NewIdentifier(share.PublicKey.Serialize(), message.RoleTypeAttester))
		}

		s := opt.qbftStorage()
		return s.CleanAllV1Decided(pks) // fake migration
	},
}

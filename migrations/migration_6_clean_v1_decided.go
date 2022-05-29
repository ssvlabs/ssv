package migrations

import (
	"context"
)

var migrationCleanV1Decided = Migration{
	Name: "migration_6_clean_v1_decided",
	Run: func(ctx context.Context, opt Options, key []byte) error {
		shares, err := opt.validatorStorage().GetAllValidatorShares()
		if err != nil {
			return err
		}
		var pks [][]byte
		for _, share := range shares {
			pks = append(pks, share.PublicKey.Serialize())
		}

		s := opt.qbftStorage()
		return s.CleanAllV1Decided(pks)
	},
}

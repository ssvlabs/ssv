package migrations

import (
	"context"
	"fmt"

	"github.com/ssvlabs/ssv/storage/basedb"
	"go.uber.org/zap"
)

// This migration changes share format used for storing share in DB from gob to ssz.
// Note, in general, migration(s) must behave as no-op (not error) when there is no
// data to be targeted - so that SSV node with "fresh" DB can operate just fine.
var migration_5_change_share_format_from_gob_to_ssz = Migration{
	Name: "migration_5_change_share_format_from_gob_to_ssz",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		// storagePrefix is a base prefix we use when storing shares
		var storagePrefix = []byte("operator/")

		// sets is a bunch of updates this migration will need to perform, we cannot do them all in a
		// single transaction (because there is a limit on how large a single transaction can be) so
		// we'll use SetMany func that will split up the data we want to update into batches committing
		// each batch in a separate transaction. I guess that makes this migration non-atomic.
		sets := make([]basedb.Obj, 0)

		err := opt.Db.GetAll(append(storagePrefix, sharesPrefixGOB...), func(i int, obj basedb.Obj) error {
			shareGOB := &storageShareGOB{}
			if err := shareGOB.Decode(obj.Value); err != nil {
				return fmt.Errorf("decode gob share: %w", err)
			}
			share, err := storageShareGOBToSpecShare(shareGOB)
			if err != nil {
				return fmt.Errorf("convert storage share to spec share: %w", err)
			}
			shareSSZ := specShareToStorageShareSSZ(share)
			key := storageKeySSZ(share.ValidatorPubKey[:])
			value, err := shareSSZ.Encode()
			if err != nil {
				return fmt.Errorf("encode ssz share: %w", err)
			}
			sets = append(sets, basedb.Obj{
				Key:   key,
				Value: value,
			})
			return nil
		})
		if err != nil {
			return fmt.Errorf("GetAll: %w", err)
		}

		if err := opt.Db.SetMany(migrationsPrefix, len(sets), func(i int) (basedb.Obj, error) {
			return sets[i], nil
		}); err != nil {
			return fmt.Errorf("SetMany: %w", err)
		}

		// TODO - do not complete migration just yet, we will complete it after testing on stage
		// has been done and when we are ready to merge: https://github.com/ssvlabs/ssv/pull/1837
		// or we'll complete this even later if we go for 100% rollback-supporting approach
		// described in https://github.com/ssvlabs/ssv/pull/1837
		//// Must complete txn before commit (complete func makes sure migration executes once).
		//if err := return completed(opt.Db); err != nil {
		//	return fmt.Errorf("complete transaction: %w", err)
		//}

		return nil
	},
}

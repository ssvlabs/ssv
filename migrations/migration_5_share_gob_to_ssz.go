package migrations

import (
	"context"
	"fmt"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	genesistypes "github.com/ssvlabs/ssv/protocol/genesis/types"
	"github.com/ssvlabs/ssv/storage/basedb"
	"go.uber.org/zap"
)

// This migration changes share format used for storing share in DB from gob to ssz.
// Note, in general, migration(s) must behave as no-op (not error) when there is no
// data to be targeted - so that SSV node with "fresh" DB can operate just fine.
var migration_5_change_share_format_from_gob_to_ssz = Migration{
	Name: "migration_5_change_share_format_from_gob_to_ssz",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		txn := opt.Db.Begin()
		defer txn.Discard()

		// storagePrefix is a base prefix we use when storing shares
		var storagePrefix = []byte("operator/")

		err := txn.GetAll(append(storagePrefix, sharesPrefixGOB...), func(i int, obj basedb.Obj) error {
			shareGOB := &storageShareGOB{}
			if err := shareGOB.Decode(obj.Value); err != nil {
				return fmt.Errorf("decode gob share: %w", err)
			}
			shareGOB.DomainType = spectypes.DomainType(genesistypes.GetDefaultDomain())
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
			err = txn.Set(storagePrefix, key, value)
			if err != nil {
				return fmt.Errorf("set ssz share: %w", err)
			}
			return nil
		})
		if err != nil {
			return fmt.Errorf("get all gob shares: %w", err)
		}

		// TODO - do not complete migration just yet, we will complete it after testing on stage
		// has been done and when we are ready to merge: https://github.com/ssvlabs/ssv/pull/1837
		// or we'll complete this even later if we go for 100% rollback-supporting approach
		// described in https://github.com/ssvlabs/ssv/pull/1837
		//// Must complete txn before commit (complete func makes sure migration executes once).
		//if err := completed(txn); err != nil {
		//	return fmt.Errorf("complete transaction: %w", err)
		//}
		if err := txn.Commit(); err != nil {
			return fmt.Errorf("commit transaction: %w", err)
		}
		return nil
	},
}

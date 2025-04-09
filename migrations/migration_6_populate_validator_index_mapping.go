package migrations

import (
	"context"
	"encoding/binary"
	"fmt"

	opstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
	"go.uber.org/zap"
)

// This migration populates the mapping between validator pubkey -> index
// It reads all the shares to collect relevant data
var migration_7_populate_validator_index_mapping = Migration{
	Name: "migration_7_populate_validator_index_mapping",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) (err error) {
		var validatorsMapped int

		defer func() {
			if err != nil {
				return // cannot complete migration successfully
			}
			// complete migration, this makes sure migration applies only once
			if err = completed(opt.Db); err != nil {
				err = fmt.Errorf("complete transaction: %w", err)
				return
			}
			logger.Info("migration completed", zap.Int("validator_mapped", validatorsMapped))
		}()

		var mappings []mapping

		var shares0 int

		if err = opt.Db.GetAll(storage.SharesDBPrefix(opstorage.OperatorStoragePrefix), func(i int, obj basedb.Obj) error {
			shareSSZ := &storage.Share{}
			if err := shareSSZ.Decode(obj.Value); err != nil {
				return fmt.Errorf("decode ssz share: %w", err)
			}

			if shareSSZ.ValidatorIndex > 0 {
				// TODO(Moshe): should we check for validator index > 0?
			} else {
				shares0++
			}

			mappings = append(mappings, mapping{index: shareSSZ.ValidatorIndex, pubkey: shareSSZ.ValidatorPubKey})

			return nil
		}); err != nil {
			return fmt.Errorf("get validator pubkey and index: %w", err)
		}

		logger.Info("tracer-migration: shares with 0 index", zap.Int("count", shares0))
		logger.Info("tracer-migration: mappings", zap.Int("count", len(mappings)))

		err = opt.Db.SetMany(storage.PubkeyToIndexMappingDBKey(opstorage.OperatorStoragePrefix), len(mappings), func(i int) (basedb.Obj, error) {
			m := mappings[i]
			return basedb.Obj{Key: m.pubkey, Value: uint64ToBytes(m.index)}, nil
		})

		if err != nil {
			return fmt.Errorf("set validator pubkey and index: %w", err)
		}

		var insertedCount = 0
		err = opt.Db.GetAll(storage.PubkeyToIndexMappingDBKey(opstorage.OperatorStoragePrefix), func(i int, o basedb.Obj) error {
			insertedCount++
			return nil
		})

		if err != nil {
			return fmt.Errorf("get all validator pubkey and index: %w", err)
		}

		logger.Info("tracer-migration: inserted", zap.Int("count", insertedCount))

		return nil
	},
}

type mapping struct {
	index  uint64
	pubkey []byte
}

func uint64ToBytes(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}

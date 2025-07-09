package migrations

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	opstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestMigration7PopulateValidatorIndexMapping(t *testing.T) {
	ctx := t.Context()
	logger := zap.NewNop()
	sharesPrefix := storage.SharesDBPrefix(opstorage.OperatorStoragePrefix)
	mappingPrefix := storage.PubkeyToIndexMappingDBKey(opstorage.OperatorStoragePrefix)
	storageSetSharesKey := sharesPrefix

	t.Run("no shares: migration is a no-op", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)

		err = migration_7_populate_validator_index_mapping.Run(ctx,
			logger,
			options,
			[]byte(migration_7_populate_validator_index_mapping.Name),
			func(rw basedb.ReadWriter) error { return nil },
		)
		assert.NoError(t, err)

		var count int
		err = options.Db.GetAll(mappingPrefix, func(i int, obj basedb.Obj) error {
			count++
			return nil
		})
		assert.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("migration completes successfully", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)
		seededShares := seedDatabase7(t, 5, options.Db, storageSetSharesKey)

		called := false
		err = migration_7_populate_validator_index_mapping.Run(ctx,
			logger,
			options,
			[]byte(migration_7_populate_validator_index_mapping.Name),
			func(rw basedb.ReadWriter) error {
				called = true
				return nil
			},
		)
		assertPubkeyToIndexMappingFromOldShares(t, options.Db, mappingPrefix, seededShares)
		assert.NoError(t, err)
		assert.True(t, called)
	})

	t.Run("completion function error: migration not marked complete", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)
		seededShares := seedDatabase7(t, 7, options.Db, storageSetSharesKey)

		err = migration_7_populate_validator_index_mapping.Run(ctx,
			logger,
			options,
			[]byte(migration_7_populate_validator_index_mapping.Name),
			func(rw basedb.ReadWriter) error { return fmt.Errorf("fail complete") },
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "fail complete")

		// Should be able to re-run and succeed
		err = migration_7_populate_validator_index_mapping.Run(ctx,
			logger,
			options,
			[]byte(migration_7_populate_validator_index_mapping.Name),
			func(rw basedb.ReadWriter) error { return nil },
		)
		assert.NoError(t, err)
		assertPubkeyToIndexMappingFromOldShares(t, options.Db, mappingPrefix, seededShares)
	})

	t.Run("share decode error: migration fails", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)
		// Insert a corrupt share
		err = options.Db.Set(sharesPrefix, []byte("corrupt"), []byte{0x01, 0x02, 0x03})
		require.NoError(t, err)

		err = migration_7_populate_validator_index_mapping.Run(ctx,
			logger,
			options,
			[]byte(migration_7_populate_validator_index_mapping.Name),
			func(rw basedb.ReadWriter) error { return nil },
		)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decode ssz share")
	})
}

// Asserts that the pubkey->index mapping matches the seeded migration_6_OldStorageShare objects.
func assertPubkeyToIndexMappingFromOldShares(t *testing.T, db basedb.Database, mappingPrefix []byte, shares []*storage.Share) {
	var mappings = make(map[string]uint64)
	err := db.GetAll(mappingPrefix, func(i int, obj basedb.Obj) error {
		mappings[string(obj.Key)] = bytesToUint64(obj.Value)
		return nil
	})
	require.NoError(t, err)
	for _, share := range shares {
		idx, ok := mappings[string(share.ValidatorPubKey)]
		assert.True(t, ok, "missing mapping for pubkey %x", share.ValidatorPubKey)
		assert.Equal(t, share.ValidatorIndex, idx, "wrong index for pubkey %x", share.ValidatorPubKey)
	}
	assert.Equal(t, len(shares), len(mappings))
}

func bytesToUint64(b []byte) uint64 {
	return uint64(b[0]) | uint64(b[1])<<8 | uint64(b[2])<<16 | uint64(b[3])<<24 |
		uint64(b[4])<<32 | uint64(b[5])<<40 | uint64(b[6])<<48 | uint64(b[7])<<56
}

func seedDatabase7(t *testing.T, numOfItems int, db basedb.Database, storageKey []byte) []*storage.Share {
	shares := make([]*storage.Share, 0, numOfItems)
	for index := range numOfItems {
		var share storage.Share
		share.ValidatorPubKey = generateValidatorPublicKey()
		share.ValidatorIndex = uint64(index)
		shares = append(shares, &share)

		shareBytes, err := share.Encode()
		if err != nil {
			t.Fatal(err)
		}
		err = db.Set(storageKey, share.ValidatorPubKey, shareBytes)
		if err != nil {
			t.Fatal(err)
		}
	}

	return shares
}

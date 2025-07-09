package migrations

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/brianvoe/gofakeit/v7"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/ssvlabs/ssv/logging"
	opstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	"github.com/ssvlabs/ssv/registry/storage"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestMigration6ExitEpochField(t *testing.T) {
	ctx := t.Context()
	newStorageGetSharesKey := storage.SharesDBPrefix(opstorage.OperatorStoragePrefix)
	storageSetSharesKey := opstorage.OperatorStoragePrefix

	t.Run("successfully migrates all shares from old prefix to new prefix", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)

		const initialDBCapacity = 50
		seededShares, err := seedDatabase(initialDBCapacity, options.Db, storageSetSharesKey)
		require.NoError(t, err)

		err = migration_6_share_exit_epoch.Run(ctx,
			logging.TestLogger(t),
			options,
			[]byte(migration_6_share_exit_epoch.Name),
			func(rw basedb.ReadWriter) error { return nil })

		assert.NoError(t, err)

		assertMigratedShares(t, options.Db, newStorageGetSharesKey, seededShares)
	})

	t.Run("runs completion function when no errors occur", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)

		const initialDBCapacity = 50
		_, err = seedDatabase(initialDBCapacity, options.Db, newStorageGetSharesKey)
		require.NoError(t, err)

		completedExecuted := false
		err = migration_6_share_exit_epoch.Run(ctx,
			logging.TestLogger(t),
			options,
			[]byte(migration_6_share_exit_epoch.Name),
			func(rw basedb.ReadWriter) error {
				completedExecuted = true
				return nil
			})

		assert.NoError(t, err)
		assert.True(t, completedExecuted)
	})

	t.Run("successfully re-runs migration when completion function returns error", func(t *testing.T) {
		options, err := setupOptions(ctx, t)
		require.NoError(t, err)

		const initialDBCapacity = 50
		seededShares, err := seedDatabase(initialDBCapacity, options.Db, storageSetSharesKey)
		require.NoError(t, err)

		err = migration_6_share_exit_epoch.Run(ctx,
			logging.TestLogger(t),
			options,
			[]byte(migration_6_share_exit_epoch.Name),
			func(rw basedb.ReadWriter) error { return fmt.Errorf("test error") })

		assert.Error(t, err)
		assert.Equal(t, "test error", err.Error())

		err = migration_6_share_exit_epoch.Run(ctx,
			logging.TestLogger(t),
			options,
			[]byte(migration_6_share_exit_epoch.Name),
			func(rw basedb.ReadWriter) error { return nil })

		assert.NoError(t, err)

		assertMigratedShares(t, options.Db, newStorageGetSharesKey, seededShares)
	})
}

func assertMigratedShares(t *testing.T, db basedb.Database, key []byte, seededShares []*migration_6_OldStorageShare) {
	var persistedShares []*storage.Share
	err := db.GetAll(key, func(i int, o basedb.Obj) error {
		share := &storage.Share{}
		err := share.UnmarshalSSZ(o.Value)
		require.NoError(t, err)
		persistedShares = append(persistedShares, share)
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, len(seededShares), len(persistedShares))
	for _, seededShare := range seededShares {
		found := false
		for _, persistedShare := range persistedShares {
			if bytes.Equal(persistedShare.ValidatorPubKey, seededShare.ValidatorPubKey) {
				found = true
				assert.True(t, sharesEqual(seededShare, persistedShare))
			}
		}
		if !found {
			t.Fatalf("one of the seeded shares was not found after the migration")
		}
	}
}

func seedDatabase(numOfItems int, db basedb.Database, storageKey []byte) ([]*migration_6_OldStorageShare, error) {
	var (
		dbShares     = make([]basedb.Obj, 0, numOfItems)
		seededShares = make([]*migration_6_OldStorageShare, 0, numOfItems)
	)
	for range numOfItems {
		var share *migration_6_OldStorageShare
		if err := gofakeit.Struct(&share); err != nil {
			return nil, err
		}
		share.ValidatorPubKey = generateValidatorPublicKey()
		/**
			'quorum' and 'partialQuorum' cannot be random values, because during the mapping in the migration
			they will be re-set by this function, hence our Assert step will fail during Share comparison
		**/
		quorum, partialQuorum := types.ComputeQuorumAndPartialQuorum(uint64(len(share.Committee)))
		share.Quorum = quorum
		share.PartialQuorum = partialQuorum

		seededShares = append(seededShares, share)

		shareBytes, err := share.Encode()
		if err != nil {
			return nil, err
		}

		dbShares = append(dbShares, basedb.Obj{
			Key:   append(oldSharesPrefix, share.ValidatorPubKey[:]...),
			Value: shareBytes,
		})
	}

	err := db.SetMany(storageKey, len(dbShares), func(i int) (basedb.Obj, error) {
		return dbShares[i], nil
	})

	return seededShares, err
}

func generateValidatorPublicKey() []byte {
	b := make([]byte, 48)
	for i := range b {
		b[i] = byte(rand.Intn(256))
	}

	return b
}

func sharesEqual(left *migration_6_OldStorageShare, right *storage.Share) bool {
	if left.ValidatorIndex != right.ValidatorIndex ||
		left.Status != right.Status ||
		left.ActivationEpoch != right.ActivationEpoch ||
		left.Liquidated != right.Liquidated ||
		!bytes.Equal(left.ValidatorPubKey, right.ValidatorPubKey) ||
		!bytes.Equal(left.SharePubKey, right.SharePubKey) ||
		!bytes.Equal(left.Graffiti, right.Graffiti) ||
		!bytes.Equal(left.DomainType[:], right.DomainType[:]) ||
		!bytes.Equal(left.FeeRecipientAddress[:], right.FeeRecipientAddress[:]) ||
		!bytes.Equal(left.OwnerAddress[:], right.OwnerAddress[:]) {
		return false
	}

	if len(left.Committee) != len(right.Committee) {
		return false
	}

	oldCommitteeMap := make(map[uint64][]byte, len(left.Committee))
	for _, member := range left.Committee {
		oldCommitteeMap[member.OperatorID] = member.PubKey
	}

	for _, member := range right.Committee {
		oldPubKey, exists := oldCommitteeMap[member.OperatorID]
		if !exists || !bytes.Equal(oldPubKey, member.PubKey) {
			return false
		}
	}

	return right.ExitEpoch == 0
}

package migrations

import (
	"bytes"
	"context"
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
	ctx := context.Background()
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
		dbShares     []basedb.Obj
		seededShares []*migration_6_OldStorageShare
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

func sharesEqual(old *migration_6_OldStorageShare, new *storage.Share) bool {
	if old.ValidatorIndex != new.ValidatorIndex ||
		old.Status != new.Status ||
		old.ActivationEpoch != new.ActivationEpoch ||
		old.Liquidated != new.Liquidated ||
		!bytes.Equal(old.ValidatorPubKey, new.ValidatorPubKey) ||
		!bytes.Equal(old.SharePubKey, new.SharePubKey) ||
		!bytes.Equal(old.Graffiti, new.Graffiti) ||
		!bytes.Equal(old.DomainType[:], new.DomainType[:]) ||
		!bytes.Equal(old.FeeRecipientAddress[:], new.FeeRecipientAddress[:]) ||
		!bytes.Equal(old.OwnerAddress[:], new.OwnerAddress[:]) {
		return false
	}

	if len(old.Committee) != len(new.Committee) {
		return false
	}

	oldCommitteeMap := make(map[uint64][]byte, len(old.Committee))
	for _, member := range old.Committee {
		oldCommitteeMap[member.OperatorID] = member.PubKey
	}

	for _, member := range new.Committee {
		oldPubKey, exists := oldCommitteeMap[member.OperatorID]
		if !exists || !bytes.Equal(oldPubKey, member.PubKey) {
			return false
		}
	}

	return new.ExitEpoch == 0
}

package migrations

import (
	"context"

	"github.com/bloxapp/ssv/storage/basedb"
)

// This migration is an example of an atomic migration using SetMany.
func migration_0_init(ctx context.Context, env *environment, key []byte) error {
	// example to syncEth1Data
	// if err := env.node.SyncEth1Data(); err != nil {
	// 	return err
	// }

	// Using SetMany, the following 3 updates either all happen and are committed,
	// or they all rollback.
	// If we used Set 3 times independently, the migration could abort in the middle
	// with only some of the Set's committed, resulting in a corrupted database.
	sets := []basedb.Obj{
		{
			Key:   []byte("owner-address"),
			Value: []byte("123"),
		},
		{
			Key:   []byte("operators-public-keys"),
			Value: []byte("abc"),
		},
		{
			Key:   key,
			Value: migrationCompleted,
		},
	}
	return env.db.SetMany(nil, 3, func(i int) (basedb.Obj, error) {
		return sets[i], nil
	})
}

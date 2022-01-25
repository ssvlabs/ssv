package migrations

import (
	"context"

	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
)

// This migration is an Example of accessing the underlying
// *badger.DB for advanced usage like transactions with View/Update
// and others.
func migration_1_ekm(ctx context.Context, env *environment, key []byte) error {
	bdb, ok := env.db.(*kv.BadgerDb)
	if !ok {
		return errors.New("db is not badger")
	}
	_ = bdb

	// TODO: if you want real atomic transactions, implement bdb.UnderlyingBadger:
	// bdb.UnderlyingBadger().Update(func(txn *badger.Txn) error {
	// 	txn.Put(...)
	// 	txn.Get(...)
	// 	txn.Put(...)
	// })

	return nil
}

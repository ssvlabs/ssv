package pebble

import (
	"io"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type pebbleTxn struct {
	*pebble.Batch
	logger *zap.Logger
}

func newPebbleTxn(logger *zap.Logger, batch *pebble.Batch) basedb.Txn {
	return &pebbleTxn{
		Batch:  batch,
		logger: logger,
	}
}

func (t *pebbleTxn) Commit() error {
	return t.Batch.Commit(pebble.Sync)
}

func (t *pebbleTxn) Discard() {
	_ = t.Close()
}

func (t *pebbleTxn) Set(prefix []byte, key []byte, value []byte) error {
	return t.Batch.Set(append(prefix, key...), value, nil)
}

func (t *pebbleTxn) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	for i := range n {
		item, err := next(i)
		if err != nil {
			return err
		}
		key := append(prefix, item.Key...)
		err = t.Batch.Set(key, item.Value, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (t *pebbleTxn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	return getter(key, func(key []byte) ([]byte, io.Closer, error) {
		return t.Batch.Get(append(prefix, key...))
	})
}

func (t *pebbleTxn) GetMany(prefix []byte, keys [][]byte, fn func(basedb.Obj) error) error {
	return manyGetter(t.logger, keys, func(key []byte) ([]byte, io.Closer, error) {
		return t.Batch.Get(append(prefix, key...))
	}, fn)
}

func (t *pebbleTxn) GetAll(prefix []byte, fn func(int, basedb.Obj) error) error {
	iter, err := makePrefixIter(t.Batch, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()

	return allGetter(t.logger, iter, prefix, fn)
}

func (t *pebbleTxn) Delete(prefix []byte, key []byte) error {
	return t.Batch.Delete(append(prefix, key...), nil)
}

// pebbleReadTxn is a read-only transaction on a pebble.Snapshot.
type pebbleReadTxn struct {
	*pebble.Snapshot
	logger *zap.Logger
}

func (txn *pebbleReadTxn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	return getter(key, func(key []byte) ([]byte, io.Closer, error) {
		return txn.Snapshot.Get(append(prefix, key...))
	})
}

func (txn *pebbleReadTxn) GetAll(prefix []byte, fn func(int, basedb.Obj) error) error {
	iter, err := makePrefixIter(txn.Snapshot, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()

	return allGetter(txn.logger, iter, prefix, fn)
}

func (txn *pebbleReadTxn) GetMany(prefix []byte, keys [][]byte, fn func(basedb.Obj) error) error {
	return manyGetter(txn.logger, keys, func(key []byte) ([]byte, io.Closer, error) {
		return txn.Snapshot.Get(append(prefix, key...))
	}, fn)
}

func (txn *pebbleReadTxn) Discard() {
	_ = txn.Close() // always nil
}

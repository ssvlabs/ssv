package pebble

import (
	"io"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type txn struct {
	*pebble.Batch
	logger *zap.Logger
}

func newTxn(logger *zap.Logger, batch *pebble.Batch) *txn {
	return &txn{
		Batch:  batch,
		logger: logger,
	}
}

func (txn *txn) Commit() error {
	return txn.Batch.Commit(pebble.Sync)
}

func (txn *txn) Discard() {
	_ = txn.Close()
}

func (txn *txn) Set(prefix []byte, key []byte, value []byte) error {
	return txn.Batch.Set(append(prefix, key...), value, nil)
}

func (txn *txn) SetMany(prefix []byte, n int, next func(int) (key, value []byte, err error)) error {
	for i := range n {
		key, value, err := next(i)
		if err != nil {
			return err
		}
		keys := append(prefix, key...)
		err = txn.Batch.Set(keys, value, nil)
		if err != nil {
			return err
		}
	}
	return nil
}

func (txn *txn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	return getter(key, func(key []byte) ([]byte, io.Closer, error) {
		return txn.Batch.Get(append(prefix, key...))
	})
}

func (txn *txn) GetMany(prefix []byte, keys [][]byte, fn func(basedb.Obj) error) error {
	return manyGetter(txn.logger, keys, func(key []byte) ([]byte, io.Closer, error) {
		return txn.Batch.Get(append(prefix, key...))
	}, fn)
}

func (txn *txn) GetAll(prefix []byte, fn func(int, basedb.Obj) error) error {
	iter, err := makePrefixIter(txn.Batch, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()

	return allGetter(txn.logger, iter, prefix, fn)
}

func (txn *txn) Delete(prefix []byte, key []byte) error {
	return txn.Batch.Delete(append(prefix, key...), nil)
}

// readTxn is a read-only transaction on a pebble.Snapshot.
type readTxn struct {
	*pebble.Snapshot
	logger *zap.Logger
}

func newReadTxn(logger *zap.Logger, snapshot *pebble.Snapshot) basedb.ReadTxn {
	return &readTxn{
		Snapshot: snapshot,
		logger:   logger,
	}
}

func (txn *readTxn) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	return getter(key, func(key []byte) ([]byte, io.Closer, error) {
		return txn.Snapshot.Get(append(prefix, key...))
	})
}

func (txn *readTxn) GetAll(prefix []byte, fn func(int, basedb.Obj) error) error {
	iter, err := makePrefixIter(txn.Snapshot, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()

	return allGetter(txn.logger, iter, prefix, fn)
}

func (txn *readTxn) GetMany(prefix []byte, keys [][]byte, fn func(basedb.Obj) error) error {
	return manyGetter(txn.logger, keys, func(key []byte) ([]byte, io.Closer, error) {
		return txn.Snapshot.Get(append(prefix, key...))
	}, fn)
}

func (txn *readTxn) Discard() {
	_ = txn.Close() // always nil
}

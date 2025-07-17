package pebble

import (
	"context"
	"io"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

var _ basedb.Database = &DB{}

type DB struct {
	*pebble.DB
	logger *zap.Logger
}

func New(logger *zap.Logger, path string, opts *pebble.Options) (*DB, error) {
	pbdb := &DB{
		logger: logger,
	}
	pdb, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	pbdb.DB = pdb
	return pbdb, nil
}

func (pdb *DB) Close() error {
	return pdb.DB.Close()
}

func (pdb *DB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	return getter(key, func(key []byte) ([]byte, io.Closer, error) {
		return pdb.DB.Get(append(prefix, key...))
	})
}

func (pdb *DB) Set(prefix, key, value []byte) error {
	return pdb.DB.Set(append(prefix, key...), value, pebble.Sync)
}

func (pdb *DB) Delete(prefix, key []byte) error {
	return pdb.DB.Delete(append(prefix, key...), pebble.Sync)
}

func (pdb *DB) GetMany(prefix []byte, keys [][]byte, fn func(basedb.Obj) error) error {
	return manyGetter(pdb.logger, keys, func(key []byte) ([]byte, io.Closer, error) {
		fullKey := append(prefix, key...)
		return pdb.DB.Get(fullKey)
	}, fn)
}

func makePrefixIter(dbOrBatch pebble.Reader, prefix []byte) (*pebble.Iterator, error) {
	keyUpperBound := func(b []byte) []byte {
		end := make([]byte, len(b))
		copy(end, b)
		for i := len(end) - 1; i >= 0; i-- {
			end[i] = end[i] + 1
			if end[i] != 0 {
				return end[:i+1]
			}
		}
		return nil // no upper-bound
	}

	prefixIterOptions := func(prefix []byte) *pebble.IterOptions {
		return &pebble.IterOptions{
			LowerBound: prefix,
			UpperBound: keyUpperBound(prefix),
		}
	}

	return dbOrBatch.NewIter(prefixIterOptions(prefix))
}

func (pdb *DB) GetAll(prefix []byte, fn func(int, basedb.Obj) error) (err error) {
	iter, err := makePrefixIter(pdb.DB, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()

	return allGetter(pdb.logger, iter, prefix, fn)
}

func (pdb *DB) Begin() basedb.Txn {
	txn := pdb.NewIndexedBatch()
	return newTxn(pdb.logger, txn)
}

func (pdb *DB) Using(rw basedb.ReadWriter) basedb.ReadWriter {
	if rw == nil {
		return pdb
	}
	return rw
}

func (pdb *DB) UsingReader(r basedb.Reader) basedb.Reader {
	if r == nil {
		return pdb
	}
	return r
}

func (pdb *DB) CountPrefix(prefix []byte) (int64, error) {
	iter, err := makePrefixIter(pdb.DB, prefix)
	if err != nil {
		return 0, err
	}

	defer func() { _ = iter.Close() }()

	count := int64(0)
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Error(); err != nil {
		return 0, err
	}

	return count, nil
}

func (pdb *DB) DropPrefix(prefix []byte) error {
	batch := pdb.NewBatch()
	iter, err := makePrefixIter(pdb.DB, prefix)
	if err != nil {
		return err
	}

	defer func() {
		_ = iter.Close()
		_ = batch.Close() // never returns an error
	}()

	for iter.First(); iter.Valid(); iter.Next() {
		if err := batch.Delete(iter.Key(), nil); err != nil {
			return err
		}
	}

	if err := iter.Error(); err != nil {
		return err
	}

	return batch.Commit(pebble.Sync)
}

func (pdb *DB) Update(fn func(basedb.Txn) error) error {
	batch := pdb.NewIndexedBatch()
	txn := newTxn(pdb.logger, batch)
	if err := fn(txn); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (pdb *DB) SetMany(prefix []byte, n int, next func(int) (key, value []byte, err error)) error {
	batch := pdb.NewBatch()
	txn := newTxn(pdb.logger, batch)
	if err := txn.SetMany(prefix, n, next); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (pdb *DB) QuickGC(ctx context.Context) error {
	return nil // pebble db does not require periodic gc
}

func (pdb *DB) FullGC(context.Context) error {
	iter, err := pdb.NewIter(nil)
	if err != nil {
		return err
	}

	var first, last []byte

	if iter.First() {
		first = append(first, iter.Key()...)
	}
	if iter.Last() {
		last = append(last, iter.Key()...)
	}
	if err := iter.Close(); err != nil {
		return err
	}

	return pdb.Compact(first, last, true)
}

func (pdb *DB) BeginRead() basedb.ReadTxn {
	snapshot := pdb.NewSnapshot()
	return newReadTxn(pdb.logger, snapshot)
}

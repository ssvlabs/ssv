package pebble

import (
	"context"
	"io"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// TODO: reconsider prefixes?
var _ basedb.Database = &PebbleDB{}

// PebbleDB struct
type PebbleDB struct {
	*pebble.DB
	logger *zap.Logger
}

func NewPebbleDB(logger *zap.Logger, path string, opts *pebble.Options) (*PebbleDB, error) {
	pbdb := &PebbleDB{
		logger: logger,
	}
	pdb, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	pbdb.DB = pdb
	return pbdb, nil
}

func (pdb *PebbleDB) Close() error {
	return pdb.DB.Close()
}

func (pdb *PebbleDB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	return getter(key, func(key []byte) ([]byte, io.Closer, error) {
		return pdb.DB.Get(append(prefix, key...))
	})
}

func (pdb *PebbleDB) Set(prefix, key, value []byte) error {
	return pdb.DB.Set(append(prefix, key...), value, pebble.Sync)
}

func (pdb *PebbleDB) Delete(prefix, key []byte) error {
	return pdb.DB.Delete(append(prefix, key...), pebble.Sync)
}

func (pdb *PebbleDB) GetMany(prefix []byte, keys [][]byte, fn func(basedb.Obj) error) error {
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

func (pdb *PebbleDB) GetAll(prefix []byte, fn func(int, basedb.Obj) error) (err error) {
	iter, err := makePrefixIter(pdb.DB, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()

	return allGetter(pdb.logger, iter, prefix, fn)
}

func (pdb *PebbleDB) Begin() basedb.Txn {
	txn := pdb.NewIndexedBatch()
	return newPebbleTxn(pdb.logger, txn)
}

func (pdb *PebbleDB) Using(rw basedb.ReadWriter) basedb.ReadWriter {
	if rw == nil {
		return pdb
	}
	return rw
}

func (pdb *PebbleDB) UsingReader(r basedb.Reader) basedb.Reader {
	if r == nil {
		return pdb
	}
	return r
}

func (pdb *PebbleDB) CountPrefix(prefix []byte) (int64, error) {
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

func (pdb *PebbleDB) DropPrefix(prefix []byte) error {
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

func (pdb *PebbleDB) Update(fn func(basedb.Txn) error) error {
	batch := pdb.NewIndexedBatch()
	txn := newPebbleTxn(pdb.logger, batch)
	if err := fn(txn); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (pdb *PebbleDB) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	batch := pdb.NewBatch()
	txn := newPebbleTxn(pdb.logger, batch)
	if err := txn.SetMany(prefix, n, next); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (pdb *PebbleDB) QuickGC(ctx context.Context) error {
	return nil // pebble db does not require periodic gc
}

func (pdb *PebbleDB) FullGC(context.Context) error {
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

func (pdb *PebbleDB) BeginRead() basedb.ReadTxn {
	return &pebbleReadTxn{Snapshot: pdb.NewSnapshot(), logger: pdb.logger}
}

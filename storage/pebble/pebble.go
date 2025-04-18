package pebble

import (
	"context"
	"errors"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// TODO: reconsider prefixes?
var _ basedb.Database = &PebbleDB{}

// PebbleDB struct
type PebbleDB struct {
	logger *zap.Logger

	db *pebble.DB
}

func NewPebbleDB(logger *zap.Logger, path string, opts *pebble.Options) (*PebbleDB, error) {
	pbdb := &PebbleDB{
		logger: logger,
	}
	pdb, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	pbdb.db = pdb
	return pbdb, nil
}

func (pdb *PebbleDB) Close() error {
	return pdb.db.Close()
}

func (pdb *PebbleDB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	b, closer, err := pdb.db.Get(append(prefix, key...))
	if errors.Is(err, pebble.ErrNotFound) {
		return basedb.Obj{}, false, nil
	}
	if err != nil {
		return basedb.Obj{}, true, err
	}

	// PebbleDB returned slice is valid until closer.Close() is called
	// hence we copy
	out := make([]byte, len(b))
	copy(out, b)

	if err := closer.Close(); err != nil {
		return basedb.Obj{}, true, err
	}

	return basedb.Obj{Key: key, Value: out}, true, nil
}

func (pdb *PebbleDB) Set(prefix, key, value []byte) error {
	return pdb.db.Set(append(prefix, key...), value, pebble.Sync)
}

func (pdb *PebbleDB) Delete(prefix, key []byte) error {
	return pdb.db.Delete(append(prefix, key...), pebble.Sync)
}

func (pdb *PebbleDB) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	for _, key := range keys {
		// Create a new slice for the full key (prefix + key)
		fullKey := make([]byte, len(prefix)+len(key))
		copy(fullKey, prefix)
		copy(fullKey[len(prefix):], key)

		// Retrieve the value from Pebble.
		value, closer, err := pdb.db.Get(fullKey)
		if err != nil {
			// If the key isn't found, skip it.
			if errors.Is(err, pebble.ErrNotFound) {
				continue
			}
			return err
		}

		// Since the returned value is only valid until closer.Close(),
		// we make a copy of it.
		vcopy := make([]byte, len(value))
		copy(vcopy, value)

		// Close the closer to release resources.
		if err := closer.Close(); err != nil {
			return err
		}

		// Wrap the key/value in an object satisfying basedb.Obj.
		obj := basedb.Obj{
			Key:   key,
			Value: vcopy,
		}

		// Call the iterator callback.
		if err := iterator(obj); err != nil {
			return err
		}
	}
	return nil
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

func (pdb *PebbleDB) GetAll(prefix []byte, iterator func(int, basedb.Obj) error) error {
	iter, err := makePrefixIter(pdb.db, prefix)
	if err != nil {
		return err
	}

	i := 0
	// SeekPrefixGE starts prefix iteration mode.
	for iter.First(); iter.Valid(); iter.Next() {
		// Even in prefix mode, verify the key begins with the prefix.
		v, err := iter.ValueAndErr()
		if err != nil {
			continue
		}
		err = iterator(i, basedb.Obj{
			Key:   iter.Key()[len(prefix):],
			Value: v,
		})
		if err != nil {
			return err
		}
		i++
	}

	return iter.Close()
}

func (pdb *PebbleDB) Begin() basedb.Txn {
	txn := pdb.db.NewIndexedBatch()
	return newPebbleTxn(txn, pdb)
}

func (pdb *PebbleDB) BeginRead() basedb.ReadTxn {
	txn := pdb.db.NewIndexedBatch() // TODO: Read txn
	return newPebbleTxn(txn, pdb)
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
	iter, err := makePrefixIter(pdb.db, prefix)
	if err != nil {
		return 0, err
	}

	count := int64(0)
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}

	if err := iter.Close(); err != nil {
		return 0, err
	}

	return count, nil
}

func (pdb *PebbleDB) DropPrefix(prefix []byte) error {
	batch := pdb.db.NewBatch()
	iter, err := makePrefixIter(pdb.db, prefix)
	if err != nil {
		return err
	}

	for iter.First(); iter.Valid(); iter.Next() {
		if err := batch.Delete(iter.Key(), nil); err != nil {
			return err
		}
	}

	if err := iter.Close(); err != nil {
		_ = batch.Close() // never returns an error
		return err
	}

	return batch.Commit(pebble.Sync)
}

func (pdb *PebbleDB) Update(fn func(basedb.Txn) error) error {
	batch := pdb.db.NewIndexedBatch()
	txn := newPebbleTxn(batch, pdb)
	if err := fn(txn); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (pdb *PebbleDB) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	batch := pdb.db.NewBatch()
	txn := newPebbleTxn(batch, pdb)
	if err := txn.SetMany(prefix, n, next); err != nil {
		return err
	}
	return batch.Commit(pebble.Sync)
}

func (pdb *PebbleDB) QuickGC(ctx context.Context) error {
	// pebble db does not require periodic gc
	return nil
}

func (pdb *PebbleDB) FullGC(context.Context) error {
	iter, err := pdb.db.NewIter(nil)
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

	return pdb.db.Compact(first, last, true)
}

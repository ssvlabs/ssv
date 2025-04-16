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

	ctx    context.Context
	cancel context.CancelFunc
	//wg     sync.WaitGroup
	//
	//// gcMutex is used to ensure that only one GC cycle is running at a time.
	//gcMutex sync.Mutex
}

func NewPebbleDB(pCtx context.Context, logger *zap.Logger, path string, opts *pebble.Options) (*PebbleDB, error) {
	ctx, cancel := context.WithCancel(pCtx)
	pbdb := &PebbleDB{
		logger: logger,
		ctx:    ctx,
		cancel: cancel,
	}
	pdb, err := pebble.Open(path, opts)
	if err != nil {
		return nil, err
	}
	pbdb.db = pdb
	return pbdb, nil
}

func (pdb *PebbleDB) Close() error {
	pdb.cancel()
	return pdb.db.Close()
}

func (pdb *PebbleDB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {

	b, closer, err := pdb.db.Get(append(prefix, key...))
	if errors.Is(err, pebble.ErrNotFound) {
		return basedb.Obj{}, false, nil
	}
	if err != nil {
		return basedb.Obj{}, false, err
	}
	// PebbleDB returned slice is valid until closer.Close() is called
	// hence we copy
	//
	defer func() { _ = closer.Close() }()

	out := make([]byte, len(b))
	copy(out, b)
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
	defer func() { _ = iter.Close() }()
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
	return iter.Error()
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
		return 0, nil
	}
	defer func() { _ = iter.Close() }()
	count := int64(0)
	for iter.First(); iter.Valid(); iter.Next() {
		count++
	}
	return count, nil
}

func (pdb *PebbleDB) DropPrefix(prefix []byte) error {
	batch := pdb.db.NewBatch()
	iter, err := makePrefixIter(pdb.db, prefix)
	if err != nil {
		return err
	}

	defer func() { _ = iter.Close() }()
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

// TODO: implement gc?

// QuickGC runs a short garbage collection cycle to reclaim some unused disk space.
func (pdb *PebbleDB) QuickGC(ctx context.Context) error {
	return nil
}

// FullGC runs a long garbage collection cycle to reclaim (ideally) all unused disk space.
func (pdb *PebbleDB) FullGC(ctx context.Context) error {
	//pdb.logger.Info("Starting FullGC")
	//start := time.Now()
	//if err := pdb.db.Compact(nil, nil); err != nil {
	//	return err
	//}
	//pdb.logger.Info("FullGC completed", zap.Duration("duration", time.Since(start)))
	//return nil
	return nil
}

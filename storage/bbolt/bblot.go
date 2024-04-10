package bbolt

import (
	"bytes"
	"context"
	"fmt"
	"github.com/bloxapp/ssv/storage/basedb"
	"go.etcd.io/bbolt"
	"go.uber.org/zap"
	"os"
	"sync"
	"time"
)

type BboltDB struct {
	logger *zap.Logger
	db     *bbolt.DB

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// gcMutex is used to ensure that only one GC cycle is running at a time.
	gcMutex sync.Mutex
}

// New creates a persistent DB instance.
func New(logger *zap.Logger, options basedb.Options) (*BboltDB, error) {
	return createDB(logger, options, false)
}

// NewInMemory creates an in-memory DB instance.
func NewInMemory(logger *zap.Logger, options basedb.Options) (*BboltDB, error) {
	return createDB(logger, options, true)
}

func createDB(logger *zap.Logger, options basedb.Options, inMemory bool) (*BboltDB, error) {
	opt := bbolt.DefaultOptions

	db, err := bbolt.Open(options.Path, os.FileMode(os.O_CREATE), opt)
	if err != nil {
		return nil, fmt.Errorf("failed to open bbolt %w", err)
	}

	// Set up context/cancel to control background goroutines.
	parentCtx := options.Ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)

	BboltDB := BboltDB{
		logger: logger,
		db:     db,
		ctx:    ctx,
		cancel: cancel,
	}

	// Start periodic reporting.
	if options.Reporting && options.Ctx != nil {
		BboltDB.wg.Add(1)
		go BboltDB.periodicallyReport(1 * time.Minute)
	}

	// Start periodic garbage collection.
	//if options.GCInterval > 0 {
	//	BboltDB.wg.Add(1)
	//	go BboltDB.periodicallyCollectGarbage(logger, options.GCInterval)
	//}

	return &BboltDB, nil
}

// Badger returns the underlying badger.DB
func (b *BboltDB) BBolt() *bbolt.DB {
	return b.db
}

// Begin creates a read-write transaction.
func (b *BboltDB) Begin() basedb.Txn {
	txn, err := b.db.Begin(true)
	if err != nil {
		return nil
	}
	return newTxn(txn, b)
}

// BeginRead creates a read-only transaction.
func (b *BboltDB) BeginRead() basedb.ReadTxn {
	tx, err := b.db.Begin(false)
	if err != nil {
		return nil
	}

	return newTxn(tx, b)
}

// Set save value with key to storage
func (b *BboltDB) Set(prefix []byte, key []byte, value []byte) error {
	return b.db.Update(func(txn *bbolt.Tx) error {
		return newTxn(txn, b).Set(prefix, key, value)
	})
}

// SetMany save many values with the given keys in a single badger transaction
func (b *BboltDB) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	return b.db.Update(func(txn *bbolt.Tx) error {
		return newTxn(txn, b).SetMany(prefix, n, next)
	})
}

// Get return value for specified key
func (b *BboltDB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	var obj basedb.Obj
	var found bool
	var err error

	bigerr := b.db.View(func(tx *bbolt.Tx) error {
		obj, found, err = newTxn(tx, b).Get(prefix, key)
		if err != nil {
			return err
		}
		return nil
	})

	if bigerr != nil {
		return basedb.Obj{}, false, bigerr
	}

	return obj, found, err
}

// GetMany return values for the given keys
func (b *BboltDB) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	if len(keys) == 0 {
		return nil
	}
	err := b.db.View(b.manyGetter(prefix, keys, iterator))
	return err
}

// Delete key in specific prefix
func (b *BboltDB) Delete(prefix []byte, key []byte) error {
	return b.db.Update(func(txn *bbolt.Tx) error {
		return newTxn(txn, b).Delete(prefix, key)
	})
}

// DeletePrefix all items with this prefix
func (b *BboltDB) DeletePrefix(prefix []byte) (int, error) {
	count := 0
	err := b.db.Update(func(txn *bbolt.Tx) error {
		rawKeys := b.listRawKeys(prefix, txn)
		bu := txn.Bucket(prefix)
		for _, k := range rawKeys {
			if err := bu.Delete(k); err != nil {
				return err
			}
			count++
		}
		return nil
	})
	return count, err
}

// GetAll returns all the items of a given collection
func (b *BboltDB) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
	// we got issues when reading more than 100 items with iterator (items get mixed up)
	// instead, the keys are first fetched using an iterator, and afterwards the values are fetched one by one
	// to avoid issues
	err := b.db.View(b.allGetter(prefix, handler))
	return err
}

// CountPrefix return the object count for all keys under specified prefix(bucket)
func (b *BboltDB) CountPrefix(prefix []byte) (int64, error) {
	var res int64
	err := b.db.View(func(txn *bbolt.Tx) error {
		bu := txn.Bucket(prefix)
		_ = bu.ForEach(func(k, v []byte) error {
			res++
			return nil
		})
		return nil
	})
	return res, err
}

// DropPrefix cleans all items in a collection
func (b *BboltDB) DropPrefix(prefix []byte) error {
	return b.db.Update(func(tx *bbolt.Tx) error {
		return tx.DeleteBucket(prefix)
	})
}

// Close closes the database.
func (b *BboltDB) Close() error {
	// Stop & wait for background goroutines.
	b.cancel()
	b.wg.Wait()

	// Close the database.
	err := b.db.Close()
	if err != nil {
		b.logger.Fatal("failed to close db", zap.Error(err))
	}
	return err
}

// report the db size and metrics
func (b *BboltDB) report() {
	//logger := b.logger.Named(logging.NameBboltDBReporting)
	//lsm, vlog := b.db.Size()
	//blockCache := b.db.BlockCacheMetrics()
	//indexCache := b.db.IndexCacheMetrics()
	//
	//logger.Debug("BboltDBReport", zap.Int64("lsm", lsm), zap.Int64("vlog", vlog),
	//	fields.BlockCacheMetrics(blockCache),
	//	fields.IndexCacheMetrics(indexCache))
}

func (b *BboltDB) periodicallyReport(interval time.Duration) {
	defer b.wg.Done()
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			b.report()
		case <-b.ctx.Done():
			return
		}
	}
}

func (b *BboltDB) listRawKeys(prefix []byte, txn *bbolt.Tx) [][]byte {
	var keys [][]byte

	bu, err := txn.CreateBucketIfNotExists(prefix)
	if err != nil {
		return nil
	}

	c := bu.Cursor()

	for k, _ := c.First(); k != nil; k, _ = c.Next() {
		keys = append(keys, k)
	}

	return keys
}

// Update is a gateway to badger db Update function
// creating and managing a read-write transaction
func (b *BboltDB) Update(fn func(basedb.Txn) error) error {
	return b.db.Update(func(txn *bbolt.Tx) error {
		return fn(newTxn(txn, b))
	})
}

func (b *BboltDB) allGetter(prefix []byte, handler func(int, basedb.Obj) error) func(txn *bbolt.Tx) error {
	return func(txn *bbolt.Tx) error {
		rawKeys := b.listRawKeys(prefix, txn)
		bu := txn.Bucket(prefix)
		if bu == nil {
			return fmt.Errorf("bucket not found")
		}
		for i, k := range rawKeys {
			trimmedResKey := bytes.TrimPrefix(k, prefix)
			item := bu.Get(k)
			if item == nil {
				b.logger.Error("failed to get value",
					zap.String("trimmedResKey", string(trimmedResKey)))
				continue
			}
			if err := handler(i, basedb.Obj{
				Key:   trimmedResKey,
				Value: item,
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

func (b *BboltDB) manyGetter(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) func(txn *bbolt.Tx) error {
	return func(txn *bbolt.Tx) error {
		bu, err := txn.CreateBucketIfNotExists(prefix)
		if err != nil {
			return err
		}

		var value, cp []byte

		for _, k := range keys {
			item := bu.Get(k)
			if item == nil {
				continue
			}
			cp = make([]byte, len(value))
			copy(cp, value)
			if err := iterator(basedb.Obj{
				Key:   k,
				Value: cp,
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

// Using returns the given ReadWriter, falling back to the database if it's nil.
func (b *BboltDB) Using(rw basedb.ReadWriter) basedb.ReadWriter {
	if rw == nil {
		return b
	}
	return rw
}

// UsingReader returns the given Reader, falling back to the database if it's nil.
func (b *BboltDB) UsingReader(r basedb.Reader) basedb.Reader {
	if r == nil {
		return b
	}
	return r
}

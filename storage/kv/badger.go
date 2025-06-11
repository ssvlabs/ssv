package kv

import (
	"bytes"
	"context"
	"sync"
	"time"

	"github.com/dgraph-io/badger/v4"
	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// BadgerDB struct
type BadgerDB struct {
	logger *zap.Logger

	db *badger.DB

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	// gcMutex is used to ensure that only one GC cycle is running at a time.
	gcMutex sync.Mutex
}

// New creates a persistent DB instance.
func New(logger *zap.Logger, options basedb.Options) (*BadgerDB, error) {
	return createDB(logger, options, false)
}

// NewInMemory creates an in-memory DB instance.
func NewInMemory(logger *zap.Logger, options basedb.Options) (*BadgerDB, error) {
	return createDB(logger, options, true)
}

func createDB(logger *zap.Logger, options basedb.Options, inMemory bool) (*BadgerDB, error) {
	// Open the Badger database located in the /tmp/badger directory.
	// It will be created if it doesn't exist.
	opt := badger.DefaultOptions(options.Path)

	if inMemory {
		opt.InMemory = true
		opt.Dir = ""
		opt.ValueDir = ""
	}

	// TODO: we should set the default logger here to log Error and higher levels
	opt.Logger = newLogger(zap.NewNop())
	if logger != nil && options.Reporting {
		opt.Logger = newLogger(logger)
	} else {
		opt.Logger = newLogger(zap.NewNop()) // TODO: we should allow only errors to be logged
	}

	opt.ValueLogFileSize = 1024 * 1024 * 100 // TODO:need to set the vlog proper (max) size
	db, err := badger.Open(opt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open badger")
	}

	// Set up context/cancel to control background goroutines.
	parentCtx := options.Ctx
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	ctx, cancel := context.WithCancel(parentCtx)

	badgerDB := BadgerDB{
		logger: logger,
		db:     db,
		ctx:    ctx,
		cancel: cancel,
	}

	// Start periodic reporting.
	if options.Reporting && options.Ctx != nil {
		badgerDB.wg.Add(1)
		go badgerDB.periodicallyReport(1 * time.Minute)
	}

	// Start periodic garbage collection.
	if options.GCInterval > 0 {
		badgerDB.wg.Add(1)
		go badgerDB.periodicallyCollectGarbage(logger, options.GCInterval)
	}

	return &badgerDB, nil
}

// Badger returns the underlying badger.DB
func (b *BadgerDB) Badger() *badger.DB {
	return b.db
}

// Begin creates a read-write transaction.
func (b *BadgerDB) Begin() basedb.Txn {
	txn := b.db.NewTransaction(true)
	return newTxn(txn, b)
}

// BeginRead creates a read-only transaction.
func (b *BadgerDB) BeginRead() basedb.ReadTxn {
	txn := b.db.NewTransaction(false)
	return newTxn(txn, b)
}

// Set save value with key to storage
func (b *BadgerDB) Set(prefix []byte, key []byte, value []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return newTxn(txn, b).Set(prefix, key, value)
	})
}

// SetMany save many values with the given keys in a single badger transaction
func (b *BadgerDB) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	wb := b.db.NewWriteBatch()
	for i := 0; i < n; i++ {
		item, err := next(i)
		if err != nil {
			wb.Cancel()
			return err
		}
		if err := wb.Set(append(prefix, item.Key...), item.Value); err != nil {
			wb.Cancel()
			return err
		}
	}
	return wb.Flush()
}

// Get return value for specified key
func (b *BadgerDB) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	txn := b.db.NewTransaction(false)
	defer txn.Discard()
	return newTxn(txn, b).Get(prefix, key)
}

// GetMany return values for the given keys
func (b *BadgerDB) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	if len(keys) == 0 {
		return nil
	}
	err := b.db.View(b.manyGetter(prefix, keys, iterator))
	return err
}

// Delete key in specific prefix
func (b *BadgerDB) Delete(prefix []byte, key []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return newTxn(txn, b).Delete(prefix, key)
	})
}

// GetAll returns all the items of a given collection
func (b *BadgerDB) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
	// we got issues when reading more than 100 items with iterator (items get mixed up)
	// instead, the keys are first fetched using an iterator, and afterwards the values are fetched one by one
	// to avoid issues
	err := b.db.View(b.allGetter(prefix, handler))
	return err
}

// CountPrefix return the object count for all keys under specified prefix(bucket)
func (b *BadgerDB) CountPrefix(prefix []byte) (int64, error) {
	var res int64
	err := b.db.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.Prefix = prefix
		it := txn.NewIterator(opt)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			res++
		}
		return nil
	})
	return res, err
}

// DropPrefix cleans all items in a collection
func (b *BadgerDB) DropPrefix(prefix []byte) error {
	return b.db.DropPrefix(prefix)
}

// Close closes the database.
func (b *BadgerDB) Close() error {
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
func (b *BadgerDB) report() {
	logger := b.logger.Named(logging.NameBadgerDBReporting)
	lsm, vlog := b.db.Size()
	blockCache := b.db.BlockCacheMetrics()
	indexCache := b.db.IndexCacheMetrics()

	logger.Debug("BadgerDBReport", zap.Int64("lsm", lsm), zap.Int64("vlog", vlog),
		fields.BlockCacheMetrics(blockCache),
		fields.IndexCacheMetrics(indexCache))
}

func (b *BadgerDB) periodicallyReport(interval time.Duration) {
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

func (b *BadgerDB) listRawKeys(prefix []byte, txn *badger.Txn) [][]byte {
	var keys [][]byte

	opt := badger.DefaultIteratorOptions
	opt.Prefix = prefix
	opt.PrefetchValues = false

	it := txn.NewIterator(opt)
	defer it.Close()

	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		keys = append(keys, it.Item().KeyCopy(nil))
	}

	return keys
}

// Update is a gateway to badger db Update function
// creating and managing a read-write transaction
func (b *BadgerDB) Update(fn func(basedb.Txn) error) error {
	return b.db.Update(func(txn *badger.Txn) error {
		return fn(newTxn(txn, b))
	})
}

func (b *BadgerDB) allGetter(prefix []byte, handler func(int, basedb.Obj) error) func(txn *badger.Txn) error {
	return func(txn *badger.Txn) error {
		rawKeys := b.listRawKeys(prefix, txn)
		for i, k := range rawKeys {
			trimmedResKey := bytes.TrimPrefix(k, prefix)
			item, err := txn.Get(k)
			if err != nil {
				b.logger.Error("failed to get value", zap.Error(err),
					zap.String("trimmedResKey", string(trimmedResKey)))
				continue
			}
			val, err := item.ValueCopy(nil)
			if err != nil {
				b.logger.Error("failed to copy value", zap.Error(err))
				continue
			}
			if err := handler(i, basedb.Obj{
				Key:   trimmedResKey,
				Value: val,
			}); err != nil {
				return err
			}
		}
		return nil
	}
}

func (b *BadgerDB) manyGetter(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) func(txn *badger.Txn) error {
	return func(txn *badger.Txn) error {
		var value, cp []byte
		for _, k := range keys {
			item, err := txn.Get(append(prefix, k...))
			if err != nil {
				if errors.Is(err, badger.ErrKeyNotFound) {
					b.logger.Debug("item not found", zap.String("key", string(k)))
					continue
				}
				b.logger.Warn("failed to get item", zap.String("key", string(k)))
				return err
			}
			value, err = item.ValueCopy(value)
			if err != nil {
				b.logger.Warn("failed to copy item value", zap.String("key", string(k)))
				return err
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
func (b *BadgerDB) Using(rw basedb.ReadWriter) basedb.ReadWriter {
	if rw == nil {
		return b
	}
	return rw
}

// UsingReader returns the given Reader, falling back to the database if it's nil.
func (b *BadgerDB) UsingReader(r basedb.Reader) basedb.Reader {
	if r == nil {
		return b
	}
	return r
}

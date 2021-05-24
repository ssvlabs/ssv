package kv

import (
	"bytes"
	"github.com/bloxapp/ssv/storage"
	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// Options for badger config
type Options struct {
	InMemory bool
}

// BadgerDb struct
type BadgerDb struct {
	db     *badger.DB
	logger zap.Logger
}

// New create new instance of Badger db
func New(path string, logger zap.Logger, opts *Options) (storage.IKvStorage, error) {
	// Open the Badger database located in the /tmp/badger directory.
	// It will be created if it doesn't exist.
	opt := badger.DefaultOptions(path)
	if opts.InMemory {
		opt.InMemory = opts.InMemory
		opt.Dir = ""
		opt.ValueDir = ""
	}

	opt.ValueLogFileSize = 1024 * 1024 * 100 // TODO:need to set the vlog proper (max) size

	db, err := badger.Open(opt)
	if err != nil {
		return &BadgerDb{}, errors.Wrap(err, "failed to open badger")
	}
	_db := BadgerDb{
		db:     db,
		logger: logger,
	}

	logger.Info("Badger db initialized")
	return &_db, nil
}

// Set save value with key to storage
func (b *BadgerDb) Set(prefix []byte, key []byte, value []byte) error {
	return b.db.Update(func(txn *badger.Txn) error {
		err := txn.Set(append(prefix, key...), value)
		return err
	})
}

// Get return value for specified key
func (b *BadgerDb) Get(prefix []byte, key []byte) (storage.Obj, error) {
	var resValue []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(append(prefix, key...))
		if err != nil {
			return err
		}
		resValue, err = item.ValueCopy(nil)
		return err
	})
	return storage.Obj{
		Key:   key,
		Value: resValue,
	}, err
}

// GetAllByCollection return all array of Obj for all keys under specified prefix(bucket)
func (b *BadgerDb) GetAllByCollection(prefix []byte) ([]storage.Obj, error) {
	var res []storage.Obj
	var err error
	err = b.db.View(func(txn *badger.Txn) error {
		opt := badger.DefaultIteratorOptions
		opt.Prefix = prefix
		it := txn.NewIterator(opt)
		defer it.Close()
		for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
			item := it.Item()
			resKey := item.Key()
			trimmedResKey := bytes.TrimPrefix(resKey, prefix)
			val, err := item.ValueCopy(nil)
			if err != nil {
				b.logger.Error("failed to copy value", zap.Error(err))
				continue
			}
			obj := storage.Obj{
				Key:   trimmedResKey,
				Value: val,
			}
			res = append(res, obj)
		}
		return err
	})
	return res, err
}

// Close close db
func (b *BadgerDb) Close() {
	if err := b.db.Close(); err != nil{
		b.logger.Fatal("failed to close db", zap.Error(err))
	}
}
package kv

import (
	"bytes"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// BadgerDb struct
type BadgerDb struct {
	db     *badger.DB
	logger *zap.Logger
}

// New create new instance of Badger db
func New(options basedb.Options) (basedb.IDb, error) {
	// Open the Badger database located in the /tmp/badger directory.
	// It will be created if it doesn't exist.
	opt := badger.DefaultOptions(options.Path)
	db, err := badger.Open(opt)
	if err != nil {
		return nil, errors.Wrap(err, "failed to open badger")
	}
	_db := BadgerDb{
		db:     db,
		logger: options.Logger,
	}

	options.Logger.Info("Badger db initialized")
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
func (b *BadgerDb) Get(prefix []byte, key []byte) (basedb.Obj, error) {
	var resValue []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(append(prefix, key...))
		if err != nil {
			return err
		}
		resValue, err = item.ValueCopy(nil)
		return err
	})
	return basedb.Obj{
		Key: key,
		Value: resValue,
	}, err
}

// GetAllByBucket return all array of Obj for all keys under specified prefix(bucket)
func (b *BadgerDb) GetAllByBucket(prefix []byte) ([]basedb.Obj, error) {
	var res []basedb.Obj
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
			obj := basedb.Obj{
				Key: trimmedResKey,
				Value: val,
			}
			res = append(res, obj)
		}
		return err
	})
	return res, err
}

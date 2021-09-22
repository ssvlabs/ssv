package kv

import (
	"bytes"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/dgraph-io/badger/v3"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

const (
	// EntryNotFoundError is an error for a storage entry not found
	EntryNotFoundError = "EntryNotFoundError"
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
	if options.Type == "badger-memory" {
		opt.InMemory = true
		opt.Dir = ""
		opt.ValueDir = ""
	}

	opt.ValueLogFileSize = 1024 * 1024 * 100 // TODO:need to set the vlog proper (max) size

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
func (b *BadgerDb) Get(prefix []byte, key []byte) (basedb.Obj, bool, error) {
	var resValue []byte
	err := b.db.View(func(txn *badger.Txn) error {
		item, err := txn.Get(append(prefix, key...))
		if err != nil {
			if err.Error() == "not found" || err.Error() == "Key not found" { // in order to couple the not found errors together
				return errors.New(EntryNotFoundError)
			}
			return err
		}
		resValue, err = item.ValueCopy(nil)
		return err
	})
	found := err == nil || err.Error() != EntryNotFoundError
	if !found {
		return basedb.Obj{}, found, nil
	}
	return basedb.Obj{
		Key:   key,
		Value: resValue,
	}, found, err
}

// GetAllByCollection return all array of Obj for all keys under specified prefix(bucket)
func (b *BadgerDb) GetAllByCollection(prefix []byte) ([]basedb.Obj, error) {
	var res []basedb.Obj

	// we got issues when reading more than 100 items with iterator (items get mixed up)
	// instead, the keys are first fetched using an iterator, and afterwards the values are fetched one by one
	// to avoid issues
	err := b.db.View(func(txn *badger.Txn) error {
		rawKeys := b.listRawKeys(prefix, txn)
		res = b.getAll(rawKeys, prefix, txn)
		return nil
	})
	return res, err
}

// CountByCollection return the object count for all keys under specified prefix(bucket)
func (b *BadgerDb) CountByCollection(prefix []byte) (int64, error) {
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

// RemoveAllByCollection cleans all items in a collection
func (b *BadgerDb) RemoveAllByCollection(prefix []byte) error {
	return b.db.DropPrefix(prefix)
}

// Close close db
func (b *BadgerDb) Close() {
	if err := b.db.Close(); err != nil {
		b.logger.Fatal("failed to close db", zap.Error(err))
	}
}

func (b *BadgerDb) getAll(rawKeys [][]byte, prefix []byte, txn *badger.Txn) []basedb.Obj {
	var res []basedb.Obj

	for _, k := range rawKeys {
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
		obj := basedb.Obj{
			Key:   trimmedResKey,
			Value: val,
		}
		res = append(res, obj)
	}

	return res
}

func (b *BadgerDb) listRawKeys(prefix []byte, txn *badger.Txn) [][]byte {
	var keys [][]byte

	opt := badger.DefaultIteratorOptions
	opt.Prefix = prefix
	it := txn.NewIterator(opt)
	defer it.Close()
	for it.Seek(prefix); it.ValidForPrefix(prefix); it.Next() {
		item := it.Item()
		keys = append(keys, item.KeyCopy(nil))
	}

	return keys
}

package badger

import (
	"errors"

	"github.com/dgraph-io/badger/v4"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type badgerTxn struct {
	txn *badger.Txn
	db  *DB
}

func newTxn(txn *badger.Txn, db *DB) basedb.Txn {
	return &badgerTxn{
		txn: txn,
		db:  db,
	}
}

func (t badgerTxn) Commit() error {
	return t.txn.Commit()
}

func (t badgerTxn) Discard() {
	t.txn.Discard()
}

func (t badgerTxn) Set(prefix []byte, key []byte, value []byte) error {
	return t.txn.Set(append(prefix, key...), value)
}

func (t badgerTxn) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	for i := 0; i < n; i++ {
		item, err := next(i)
		if err != nil {
			return err
		}

		if err := t.txn.Set(append(prefix, item.Key...), item.Value); err != nil {
			return err
		}
	}

	return nil
}

func (t badgerTxn) Get(prefix []byte, key []byte) (obj basedb.Obj, found bool, err error) {
	var resValue []byte
	item, err := t.txn.Get(append(prefix, key...))
	if err != nil {
		if errors.Is(err, badger.ErrKeyNotFound) { // in order to couple the not found errors together
			return basedb.Obj{}, false, nil
		}
		return basedb.Obj{}, true, err
	}
	resValue, err = item.ValueCopy(nil)
	if err != nil {
		return basedb.Obj{}, true, err
	}
	return basedb.Obj{
		Key:   key,
		Value: resValue,
	}, true, err
}

func (t badgerTxn) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	if len(keys) == 0 {
		return nil
	}

	return t.db.manyGetter(prefix, keys, iterator)(t.txn)
}

func (t badgerTxn) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
	return t.db.allGetter(prefix, handler)(t.txn)
}

func (t badgerTxn) Delete(prefix []byte, key []byte) error {
	return t.txn.Delete(append(prefix, key...))
}

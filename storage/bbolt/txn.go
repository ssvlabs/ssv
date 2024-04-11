package bbolt

import (
	"go.etcd.io/bbolt"

	"github.com/bloxapp/ssv/storage/basedb"
)

type bboltTxn struct {
	txn *bbolt.Tx
	db  *BboltDB
}

func newTxn(txn *bbolt.Tx, db *BboltDB) basedb.Txn {
	return &bboltTxn{
		txn: txn,
		db:  db,
	}
}

func (t bboltTxn) Commit() error {
	return t.txn.Commit()
}

func (t bboltTxn) Discard() {
	t.txn.Rollback()
}

func (t bboltTxn) Set(prefix []byte, key []byte, value []byte) error {
	b, err := t.txn.CreateBucketIfNotExists(prefix)
	if err != nil {
		return err
	}
	return b.Put(key, value)
}

// TODO: use foreach?
func (t bboltTxn) SetMany(prefix []byte, n int, next func(int) (basedb.Obj, error)) error {
	b, err := t.txn.CreateBucketIfNotExists(prefix)
	if err != nil {
		return err
	}
	for i := 0; i < n; i++ {
		item, err := next(i)
		if err != nil {
			return err
		}

		if err := b.Put(item.Key, item.Value); err != nil {
			return err
		}
	}

	return nil
}

func (t bboltTxn) Get(prefix []byte, key []byte) (obj basedb.Obj, found bool, err error) {
	b := t.txn.Bucket(prefix)
	if b == nil {
		return basedb.Obj{}, false, nil
	}
	value := b.Get(key)
	if value == nil {
		return basedb.Obj{}, false, nil
	}

	// TODO we need to copy value?

	return basedb.Obj{
		Key:   key,
		Value: value,
	}, true, err
}

func (t bboltTxn) GetMany(prefix []byte, keys [][]byte, iterator func(basedb.Obj) error) error {
	if len(keys) == 0 {
		return nil
	}

	return t.db.manyGetter(prefix, keys, iterator)(t.txn)
}

func (t bboltTxn) GetAll(prefix []byte, handler func(int, basedb.Obj) error) error {
	return t.db.allGetter(prefix, handler)(t.txn)
}

func (t bboltTxn) Delete(prefix []byte, key []byte) error {
	b, err := t.txn.CreateBucketIfNotExists(prefix)
	if err != nil {
		return err
	}
	return b.Delete(key)
}

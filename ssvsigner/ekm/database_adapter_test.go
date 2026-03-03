package ekm

import (
	"fmt"

	"github.com/ssvlabs/ssv/storage/basedb"
)

func newTestDatabaseAdapter(db basedb.Database) Database {
	return &testDatabaseAdapter{db: db}
}

type testDatabaseAdapter struct {
	db basedb.Database
}

func (a *testDatabaseAdapter) Get(txn ReadTxn, prefix []byte, key []byte) (Obj, bool, error) {
	if txn == nil {
		obj, found, err := a.db.Get(prefix, key)
		return Obj{Key: obj.Key, Value: obj.Value}, found, err
	}

	r, ok := txn.(basedb.Reader)
	if !ok {
		return Obj{}, false, fmt.Errorf("unexpected read txn type %T", txn)
	}

	obj, found, err := a.db.UsingReader(r).Get(prefix, key)
	return Obj{Key: obj.Key, Value: obj.Value}, found, err
}

func (a *testDatabaseAdapter) GetAll(txn ReadTxn, prefix []byte, handler func(int, Obj) error) error {
	var reader basedb.Reader
	if txn != nil {
		r, ok := txn.(basedb.Reader)
		if !ok {
			return fmt.Errorf("unexpected read txn type %T", txn)
		}
		reader = r
	}

	return a.db.UsingReader(reader).GetAll(prefix, func(i int, obj basedb.Obj) error {
		return handler(i, Obj{Key: obj.Key, Value: obj.Value})
	})
}

func (a *testDatabaseAdapter) Set(txn ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	if txn == nil {
		return a.db.Set(prefix, key, value)
	}

	rw, ok := txn.(basedb.ReadWriter)
	if !ok {
		return fmt.Errorf("unexpected write txn type %T", txn)
	}
	return a.db.Using(rw).Set(prefix, key, value)
}

func (a *testDatabaseAdapter) Delete(txn ReadWriteTxn, prefix []byte, key []byte) error {
	if txn == nil {
		return a.db.Delete(prefix, key)
	}

	rw, ok := txn.(basedb.ReadWriter)
	if !ok {
		return fmt.Errorf("unexpected write txn type %T", txn)
	}
	return a.db.Using(rw).Delete(prefix, key)
}

func (a *testDatabaseAdapter) DropPrefix(prefix []byte) error {
	return a.db.DropPrefix(prefix)
}

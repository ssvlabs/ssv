package testenv

import (
	"fmt"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func newDatabaseAdapter(db basedb.Database) ekm.Database {
	return &databaseAdapter{db: db}
}

type databaseAdapter struct {
	db basedb.Database
}

func (a *databaseAdapter) Get(txn ekm.ReadTxn, prefix []byte, key []byte) (ekm.Obj, bool, error) {
	if txn == nil {
		obj, found, err := a.db.Get(prefix, key)
		return ekm.Obj{Key: obj.Key, Value: obj.Value}, found, err
	}

	r, ok := txn.(basedb.Reader)
	if !ok {
		return ekm.Obj{}, false, fmt.Errorf("unexpected read txn type %T", txn)
	}

	obj, found, err := a.db.UsingReader(r).Get(prefix, key)
	return ekm.Obj{Key: obj.Key, Value: obj.Value}, found, err
}

func (a *databaseAdapter) GetAll(txn ekm.ReadTxn, prefix []byte, handler func(int, ekm.Obj) error) error {
	var reader basedb.Reader
	if txn != nil {
		r, ok := txn.(basedb.Reader)
		if !ok {
			return fmt.Errorf("unexpected read txn type %T", txn)
		}
		reader = r
	}

	return a.db.UsingReader(reader).GetAll(prefix, func(i int, obj basedb.Obj) error {
		return handler(i, ekm.Obj{Key: obj.Key, Value: obj.Value})
	})
}

func (a *databaseAdapter) Set(txn ekm.ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	if txn == nil {
		return a.db.Set(prefix, key, value)
	}

	rw, ok := txn.(basedb.ReadWriter)
	if !ok {
		return fmt.Errorf("unexpected write txn type %T", txn)
	}
	return a.db.Using(rw).Set(prefix, key, value)
}

func (a *databaseAdapter) Delete(txn ekm.ReadWriteTxn, prefix []byte, key []byte) error {
	if txn == nil {
		return a.db.Delete(prefix, key)
	}

	rw, ok := txn.(basedb.ReadWriter)
	if !ok {
		return fmt.Errorf("unexpected write txn type %T", txn)
	}
	return a.db.Using(rw).Delete(prefix, key)
}

func (a *databaseAdapter) DropPrefix(prefix []byte) error {
	return a.db.DropPrefix(prefix)
}

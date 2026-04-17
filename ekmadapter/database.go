package ekmadapter

import (
	"fmt"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/v2/storage/basedb"
)

// NewDatabaseAdapter bridges node's basedb.Database to ekm.Database.
func NewDatabaseAdapter(db basedb.Database) ekm.Database {
	return &databaseAdapter{db: db}
}

type databaseAdapter struct {
	db basedb.Database
}

var (
	// Node callers are expected to pass basedb.Txn/basedb.ReadTxn values through ekm txn parameters.
	_ ekm.ReadTxn      = basedb.Txn(nil)
	_ ekm.ReadWriteTxn = basedb.Txn(nil)
)

func (a *databaseAdapter) Get(txn ekm.ReadTxn, prefix []byte, key []byte) (ekm.Obj, bool, error) {
	reader, err := asReader(txn)
	if err != nil {
		return ekm.Obj{}, false, err
	}

	obj, found, err := a.db.UsingReader(reader).Get(prefix, key)
	return ekm.Obj{Key: obj.Key, Value: obj.Value}, found, err
}

func (a *databaseAdapter) GetAll(txn ekm.ReadTxn, prefix []byte, handler func(int, ekm.Obj) error) error {
	reader, err := asReader(txn)
	if err != nil {
		return err
	}

	return a.db.UsingReader(reader).GetAll(prefix, func(i int, obj basedb.Obj) error {
		return handler(i, ekm.Obj{Key: obj.Key, Value: obj.Value})
	})
}

func (a *databaseAdapter) Set(txn ekm.ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	rw, err := asReadWriter(txn)
	if err != nil {
		return err
	}

	return a.db.Using(rw).Set(prefix, key, value)
}

func (a *databaseAdapter) Delete(txn ekm.ReadWriteTxn, prefix []byte, key []byte) error {
	rw, err := asReadWriter(txn)
	if err != nil {
		return err
	}

	return a.db.Using(rw).Delete(prefix, key)
}

func (a *databaseAdapter) DropPrefix(prefix []byte) error {
	return a.db.DropPrefix(prefix)
}

func asReader(txn ekm.ReadTxn) (basedb.Reader, error) {
	if txn == nil {
		return nil, nil
	}

	reader, ok := txn.(basedb.Reader)
	if !ok {
		return nil, fmt.Errorf("unexpected read txn type %T", txn)
	}
	return reader, nil
}

func asReadWriter(txn ekm.ReadWriteTxn) (basedb.ReadWriter, error) {
	if txn == nil {
		return nil, nil
	}

	rw, ok := txn.(basedb.ReadWriter)
	if !ok {
		return nil, fmt.Errorf("unexpected write txn type %T", txn)
	}
	return rw, nil
}

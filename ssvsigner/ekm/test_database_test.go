package ekm

import (
	"github.com/ssvlabs/ssv/ssvsigner/internal/testdb"
)

type testDB struct {
	store *testdb.Store
}

func newTestMemoryDB() *testDB {
	return &testDB{store: testdb.NewInMemory()}
}

func newTestPersistentDB(path string) (*testDB, error) {
	store, err := testdb.NewPersistentFile(path)
	if err != nil {
		return nil, err
	}
	return &testDB{store: store}, nil
}

func (d *testDB) Close() error {
	return d.store.Close()
}

func (d *testDB) Get(_ ReadTxn, prefix []byte, key []byte) (Obj, bool, error) {
	r, found := d.store.Get(prefix, key)
	if !found {
		return Obj{}, false, nil
	}
	return Obj{
		Key:   append([]byte(nil), r.Key...),
		Value: append([]byte(nil), r.Value...),
	}, true, nil
}

func (d *testDB) GetAll(_ ReadTxn, prefix []byte, handler func(int, Obj) error) error {
	list := d.store.GetAll(prefix)
	for i, r := range list {
		if err := handler(i, Obj{
			Key:   append([]byte(nil), r.Key...),
			Value: append([]byte(nil), r.Value...),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (d *testDB) Set(_ ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	return d.store.Set(prefix, key, value)
}

func (d *testDB) Delete(_ ReadWriteTxn, prefix []byte, key []byte) error {
	return d.store.Delete(prefix, key)
}

func (d *testDB) DropPrefix(prefix []byte) error {
	return d.store.DropPrefix(prefix)
}

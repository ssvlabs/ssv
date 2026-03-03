package testenv

import (
	"path/filepath"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/internal/testdb"
)

type testDB struct {
	store *testdb.Store
}

func newTestPersistentDB(dir string) (*testDB, error) {
	store, err := testdb.NewPersistentFile(filepath.Join(dir, "ekm_test_db.json"))
	if err != nil {
		return nil, err
	}
	return &testDB{store: store}, nil
}

func (d *testDB) Close() error {
	return d.store.Close()
}

func (d *testDB) Get(_ ekm.ReadTxn, prefix []byte, key []byte) (ekm.Obj, bool, error) {
	r, found := d.store.Get(prefix, key)
	if !found {
		return ekm.Obj{}, false, nil
	}
	return ekm.Obj{
		Key:   append([]byte(nil), r.Key...),
		Value: append([]byte(nil), r.Value...),
	}, true, nil
}

func (d *testDB) GetAll(_ ekm.ReadTxn, prefix []byte, handler func(int, ekm.Obj) error) error {
	list := d.store.GetAll(prefix)
	for i, r := range list {
		if err := handler(i, ekm.Obj{
			Key:   append([]byte(nil), r.Key...),
			Value: append([]byte(nil), r.Value...),
		}); err != nil {
			return err
		}
	}
	return nil
}

func (d *testDB) Set(_ ekm.ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	return d.store.Set(prefix, key, value)
}

func (d *testDB) Delete(_ ekm.ReadWriteTxn, prefix []byte, key []byte) error {
	return d.store.Delete(prefix, key)
}

func (d *testDB) DropPrefix(prefix []byte) error {
	return d.store.DropPrefix(prefix)
}

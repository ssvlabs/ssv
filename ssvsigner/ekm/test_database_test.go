package ekm

import (
	"bytes"

	"github.com/ssvlabs/ssv/ssvsigner/internal/testdb"
)

type testDB struct {
	store *testdb.Store
}

func toObj(r testdb.Record) Obj {
	return Obj{
		Key:   bytes.Clone(r.Key),
		Value: bytes.Clone(r.Value),
	}
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
	record, found := d.store.Get(prefix, key)
	if !found {
		return Obj{}, false, nil
	}
	return toObj(record), true, nil
}

func (d *testDB) GetAll(_ ReadTxn, prefix []byte, handler func(int, Obj) error) error {
	list := d.store.GetAll(prefix)
	for i, record := range list {
		if err := handler(i, toObj(record)); err != nil {
			return err
		}
	}
	return nil
}

// Set stores key/value data. The txn parameter is intentionally ignored:
// this helper does not model transactional isolation in tests.
func (d *testDB) Set(_ ReadWriteTxn, prefix []byte, key []byte, value []byte) error {
	return d.store.Set(prefix, key, value)
}

// Delete removes a key. The txn parameter is intentionally ignored (see Set).
func (d *testDB) Delete(_ ReadWriteTxn, prefix []byte, key []byte) error {
	return d.store.Delete(prefix, key)
}

func (d *testDB) DropPrefix(prefix []byte) error {
	return d.store.DropPrefix(prefix)
}

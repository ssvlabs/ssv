package ekm

import (
	"encoding/json"
	"os"
	"sort"
	"sync"

	"go.uber.org/zap"
)

type testDBRecord struct {
	Prefix []byte
	Key    []byte
	Value  []byte
}

type testDB struct {
	mu      sync.RWMutex
	records map[string]testDBRecord
	path    string
}

func newTestMemoryDB(_ *zap.Logger) (*testDB, error) {
	return &testDB{records: map[string]testDBRecord{}}, nil
}

func newTestPersistentDB(_ *zap.Logger, path string) (*testDB, error) {
	db := &testDB{
		records: map[string]testDBRecord{},
		path:    path,
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return db, nil
		}
		return nil, err
	}

	var list []testDBRecord
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}

	for _, r := range list {
		db.records[string(composeDBKey(r.Prefix, r.Key))] = copyRecord(r)
	}

	return db, nil
}

func (d *testDB) Close() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.persistLocked()
}

func (d *testDB) Get(_ ReadTxn, prefix []byte, key []byte) (Obj, bool, error) {
	d.mu.RLock()
	defer d.mu.RUnlock()

	r, ok := d.records[string(composeDBKey(prefix, key))]
	if !ok {
		return Obj{}, false, nil
	}

	return Obj{
		Key:   append([]byte(nil), r.Key...),
		Value: append([]byte(nil), r.Value...),
	}, true, nil
}

func (d *testDB) GetAll(_ ReadTxn, prefix []byte, handler func(int, Obj) error) error {
	d.mu.RLock()
	defer d.mu.RUnlock()

	list := make([]testDBRecord, 0)
	for _, r := range d.records {
		if string(r.Prefix) == string(prefix) {
			list = append(list, copyRecord(r))
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return string(list[i].Key) < string(list[j].Key)
	})

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
	d.mu.Lock()
	defer d.mu.Unlock()

	d.records[string(composeDBKey(prefix, key))] = testDBRecord{
		Prefix: append([]byte(nil), prefix...),
		Key:    append([]byte(nil), key...),
		Value:  append([]byte(nil), value...),
	}

	return d.persistLocked()
}

func (d *testDB) Delete(_ ReadWriteTxn, prefix []byte, key []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	delete(d.records, string(composeDBKey(prefix, key)))
	return d.persistLocked()
}

func (d *testDB) DropPrefix(prefix []byte) error {
	d.mu.Lock()
	defer d.mu.Unlock()

	for k, r := range d.records {
		if string(r.Prefix) == string(prefix) {
			delete(d.records, k)
		}
	}

	return d.persistLocked()
}

func (d *testDB) persistLocked() error {
	if d.path == "" {
		return nil
	}

	list := make([]testDBRecord, 0, len(d.records))
	for _, r := range d.records {
		list = append(list, copyRecord(r))
	}

	data, err := json.Marshal(list)
	if err != nil {
		return err
	}

	return os.WriteFile(d.path, data, 0o600)
}

func composeDBKey(prefix []byte, key []byte) []byte {
	k := make([]byte, 0, len(prefix)+1+len(key))
	k = append(k, prefix...)
	k = append(k, 0)
	k = append(k, key...)
	return k
}

func copyRecord(r testDBRecord) testDBRecord {
	return testDBRecord{
		Prefix: append([]byte(nil), r.Prefix...),
		Key:    append([]byte(nil), r.Key...),
		Value:  append([]byte(nil), r.Value...),
	}
}

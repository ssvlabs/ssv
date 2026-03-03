package testdb

import (
	"encoding/json"
	"os"
	"sort"
	"sync"
)

type Record struct {
	Prefix []byte
	Key    []byte
	Value  []byte
}

type Store struct {
	mu      sync.RWMutex
	records map[string]Record
	path    string
}

func NewInMemory() *Store {
	return &Store{records: map[string]Record{}}
}

func NewPersistentFile(path string) (*Store, error) {
	s := &Store{
		records: map[string]Record{},
		path:    path,
	}

	// #nosec G304 -- test helper path is provided by test code (temp dir / controlled fixture path)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return s, nil
		}
		return nil, err
	}

	var list []Record
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, err
	}

	for _, r := range list {
		s.records[string(composeKey(r.Prefix, r.Key))] = copyRecord(r)
	}

	return s, nil
}

func (s *Store) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.persistLocked()
}

func (s *Store) Get(prefix []byte, key []byte) (Record, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	r, ok := s.records[string(composeKey(prefix, key))]
	if !ok {
		return Record{}, false
	}
	return copyRecord(r), true
}

func (s *Store) GetAll(prefix []byte) []Record {
	s.mu.RLock()
	defer s.mu.RUnlock()

	list := make([]Record, 0)
	for _, r := range s.records {
		if string(r.Prefix) == string(prefix) {
			list = append(list, copyRecord(r))
		}
	}

	sort.Slice(list, func(i, j int) bool {
		return string(list[i].Key) < string(list[j].Key)
	})

	return list
}

func (s *Store) Set(prefix []byte, key []byte, value []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.records[string(composeKey(prefix, key))] = Record{
		Prefix: append([]byte(nil), prefix...),
		Key:    append([]byte(nil), key...),
		Value:  append([]byte(nil), value...),
	}

	return s.persistLocked()
}

func (s *Store) Delete(prefix []byte, key []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	delete(s.records, string(composeKey(prefix, key)))
	return s.persistLocked()
}

func (s *Store) DropPrefix(prefix []byte) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	for k, r := range s.records {
		if string(r.Prefix) == string(prefix) {
			delete(s.records, k)
		}
	}

	return s.persistLocked()
}

func (s *Store) persistLocked() error {
	if s.path == "" {
		return nil
	}

	list := make([]Record, 0, len(s.records))
	for _, r := range s.records {
		list = append(list, copyRecord(r))
	}

	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	// #nosec G304 -- test helper path is provided by test code (temp dir / controlled fixture path)
	return os.WriteFile(s.path, data, 0o600)
}

func composeKey(prefix []byte, key []byte) []byte {
	k := make([]byte, 0, len(prefix)+1+len(key))
	k = append(k, prefix...)
	k = append(k, 0)
	k = append(k, key...)
	return k
}

func copyRecord(r Record) Record {
	return Record{
		Prefix: append([]byte(nil), r.Prefix...),
		Key:    append([]byte(nil), r.Key...),
		Value:  append([]byte(nil), r.Value...),
	}
}

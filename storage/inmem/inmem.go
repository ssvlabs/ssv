package inmem

import (
	"encoding/hex"
	"errors"
	"github.com/bloxapp/ssv/storage"
	"sync"
)

// inMemStorage implements storage.Storage interface
type inMemStorage struct {
	lock   sync.RWMutex
	memory map[string]map[string][]byte
}


// New is the constructor of inMemStorage
func New() storage.IKvStorage {
	return &inMemStorage{memory: make(map[string]map[string][]byte)}
}

func (i *inMemStorage) Set(prefix []byte, key []byte, value []byte) error {
	i.lock.Lock()
	defer i.lock.Unlock()
	if i.memory[hex.EncodeToString(prefix)] == nil {
		i.memory[hex.EncodeToString(prefix)] = make(map[string][]byte)
	}
	i.memory[hex.EncodeToString(prefix)][hex.EncodeToString(key)] = value
	return nil
}

func (i *inMemStorage) Get(prefix []byte, key []byte) (storage.Obj, error) {
	i.lock.RLock()
	defer i.lock.RUnlock()
	if _, found := i.memory[hex.EncodeToString(prefix)]; found {
		if value, found := i.memory[hex.EncodeToString(prefix)][hex.EncodeToString(key)]; found {
			return storage.Obj{
				Key:   key,
				Value: value,
			}, nil
		}
		return storage.Obj{}, errors.New("not found")
	}
	return storage.Obj{}, errors.New("not found")
}

func (i *inMemStorage) GetAllByCollection(prefix []byte) ([]storage.Obj, error) {
	ret := make([]storage.Obj, 0)
	if _, found := i.memory[hex.EncodeToString(prefix)]; found {
		for k, v := range i.memory[hex.EncodeToString(prefix)] {
			key, err := hex.DecodeString(k)
			if err != nil {
				return []storage.Obj{}, err
			}

			ret = append(ret, storage.Obj{
				Key:   key,
				Value: v,
			})
		}
		return ret, nil
	}
	return []storage.Obj{}, errors.New("not found")
}


func (i *inMemStorage) Close() {
	panic("implement me")
}

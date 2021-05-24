package inmem

import (
	"github.com/bloxapp/ssv/storage"
)

// inMemStorage implements storage.Storage interface
type inMemStorage struct {
}


// New is the constructor of inMemStorage
func New() storage.IKvStorage {
	return &inMemStorage{}
}

func (i *inMemStorage) Set(prefix []byte, key []byte, value []byte) error {
	return nil
}

func (i *inMemStorage) Get(prefix []byte, key []byte) (storage.Obj, error) {
	return storage.Obj{
		Key:   key,
		Value: nil,
	}, nil
}

func (i *inMemStorage) GetAllByCollection(prefix []byte) ([]storage.Obj, error) {
	return []storage.Obj{
		{
			Key:   nil,
			Value: nil,
		},
	}, nil
}


func (i *inMemStorage) Close() {
	panic("implement me")
}

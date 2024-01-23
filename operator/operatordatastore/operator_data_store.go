package operatordatastore

import (
	"sync"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

type OperatorDataStore interface {
	GetOperatorData() *registrystorage.OperatorData
	SetOperatorData(data *registrystorage.OperatorData)
}

type operatorDataStore struct {
	operatorData      *registrystorage.OperatorData
	operatorDataMutex sync.RWMutex
}

func NewOperatorDataStore(od *registrystorage.OperatorData) OperatorDataStore {
	return &operatorDataStore{
		operatorData: od,
	}
}

func (od *operatorDataStore) GetOperatorData() *registrystorage.OperatorData {
	od.operatorDataMutex.RLock()
	defer od.operatorDataMutex.RUnlock()

	return od.operatorData
}

func (od *operatorDataStore) SetOperatorData(data *registrystorage.OperatorData) {
	od.operatorDataMutex.Lock()
	defer od.operatorDataMutex.Unlock()

	od.operatorData = data
}

package operatordatastore

import (
	"sync"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

//go:generate mockgen -package=operatordatastore -destination=./mock.go -source=./operator_data_store.go

type OperatorDataStore interface {
	GetOperatorData() *registrystorage.OperatorData
	SetOperatorData(data *registrystorage.OperatorData)
}

type operatorDataStore struct {
	operatorData      *registrystorage.OperatorData
	operatorDataMutex sync.RWMutex
}

func New(od *registrystorage.OperatorData) OperatorDataStore {
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

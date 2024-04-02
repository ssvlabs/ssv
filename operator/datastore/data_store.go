package datastore

import (
	"sync"

	spectypes "github.com/bloxapp/ssv-spec/types"

	registrystorage "github.com/bloxapp/ssv/registry/storage"
)

// OperatorDataStore defines an interface for operations related to operator data.
type OperatorDataStore interface {
	GetOperatorData() *registrystorage.OperatorData
	GetOperatorID() spectypes.OperatorID
	OperatorIDReady() bool
	AwaitOperatorID() spectypes.OperatorID
	SetOperatorData(data *registrystorage.OperatorData)
}

// operatorDataStore provides a thread-safe implementation of OperatorDataStore.
type operatorDataStore struct {
	operatorData    *registrystorage.OperatorData
	operatorDataMu  sync.RWMutex
	operatorIDReady bool
	readyCond       *sync.Cond
}

// New creates and initializes a new instance of OperatorDataStore with optional initial data.
func New(od *registrystorage.OperatorData) OperatorDataStore {
	ods := &operatorDataStore{
		operatorData: od,
	}
	ods.readyCond = sync.NewCond(&ods.operatorDataMu)
	if od != nil && od.ID != 0 {
		ods.setOperatorIDReady()
	}
	return ods
}

// GetOperatorData returns the current operator data in a thread-safe manner.
func (ods *operatorDataStore) GetOperatorData() *registrystorage.OperatorData {
	ods.operatorDataMu.RLock()
	defer ods.operatorDataMu.RUnlock()

	return ods.operatorData
}

// GetOperatorID returns the operator ID in a thread-safe manner. It must be called after OperatorIDReady returns true
func (ods *operatorDataStore) GetOperatorID() spectypes.OperatorID {
	ods.operatorDataMu.RLock()
	defer ods.operatorDataMu.RUnlock()

	if ods.operatorData == nil {
		return 0
	}

	return ods.operatorData.ID
}

// OperatorIDReady checks if the operator ID is ready.
func (ods *operatorDataStore) OperatorIDReady() bool {
	ods.operatorDataMu.RLock()
	defer ods.operatorDataMu.RUnlock()

	return ods.operatorIDReady
}

// AwaitOperatorID blocks until the operator ID is ready and returns it.
func (ods *operatorDataStore) AwaitOperatorID() spectypes.OperatorID {
	ods.operatorDataMu.Lock()
	for !ods.operatorIDReady {
		ods.readyCond.Wait()
	}
	id := ods.operatorData.ID
	ods.operatorDataMu.Unlock()
	return id
}

// SetOperatorData sets the operator data in a thread-safe manner and marks the operator ID as ready if valid.
func (ods *operatorDataStore) SetOperatorData(od *registrystorage.OperatorData) {
	ods.operatorDataMu.Lock()
	defer ods.operatorDataMu.Unlock()

	ods.operatorData = od
	if od != nil && od.ID != 0 {
		ods.setOperatorIDReady()
	}
}

// setOperatorIDReady marks the operator ID as ready and notifies waiting goroutines.
func (ods *operatorDataStore) setOperatorIDReady() {
	ods.operatorIDReady = true
	ods.readyCond.Broadcast()
}

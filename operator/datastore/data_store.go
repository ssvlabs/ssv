package datastore

import (
	"sync"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

// OperatorDataStore defines an interface for operations related to operator data.
type OperatorDataStore interface {
	GetOperatorData() *registrystorage.OperatorData
	GetOperatorID() spectypes.OperatorID
	OperatorIDReady() bool
	SetOperatorData(data *registrystorage.OperatorData)
}

// operatorDataStore provides a thread-safe implementation of OperatorDataStore.
type operatorDataStore struct {
	operatorData    *registrystorage.OperatorData
	operatorDataMu  sync.RWMutex
	operatorIDReady bool
}

// New creates and initializes a new instance of OperatorDataStore with optional initial data.
func New(od *registrystorage.OperatorData) OperatorDataStore {
	return &operatorDataStore{
		operatorData: od,
	}
}

// GetOperatorData returns the current operator data in a thread-safe manner.
func (ods *operatorDataStore) GetOperatorData() *registrystorage.OperatorData {
	ods.operatorDataMu.RLock()
	defer ods.operatorDataMu.RUnlock()

	return ods.operatorData
}

// GetOperatorID returns the operator ID in a thread-safe manner.
func (ods *operatorDataStore) GetOperatorID() spectypes.OperatorID {
	ods.operatorDataMu.RLock()
	defer ods.operatorDataMu.RUnlock()

	if ods.operatorData == nil {
		return 0
	}

	return ods.operatorData.ID
}

// OperatorIDReady returns true if the node has processed its own OperatorAdded event.
// For exporter mode, this always returns false.
func (ods *operatorDataStore) OperatorIDReady() bool {
	ods.operatorDataMu.RLock()
	defer ods.operatorDataMu.RUnlock()

	return ods.operatorIDReady
}

// SetOperatorData sets the operator data in a thread-safe manner and marks the operator ID as ready if valid.
func (ods *operatorDataStore) SetOperatorData(od *registrystorage.OperatorData) {
	ods.operatorDataMu.Lock()
	defer ods.operatorDataMu.Unlock()

	ods.operatorData = od
}

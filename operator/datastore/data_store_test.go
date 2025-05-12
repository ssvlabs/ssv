package datastore

import (
	"testing"
	"time"

	spectypes "github.com/ssvlabs/ssv-spec/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	registrystorage "github.com/ssvlabs/ssv/registry/storage"
)

func TestNewOperatorDataStore(t *testing.T) {
	t.Run("initializes with nil data", func(t *testing.T) {
		store := New(nil).(*operatorDataStore)

		assert.NotNil(t, store)
		assert.Nil(t, store.operatorData)
		assert.False(t, store.operatorIDReady)
	})

	t.Run("initializes with valid data", func(t *testing.T) {
		data := &registrystorage.OperatorData{ID: 1}
		store := New(data).(*operatorDataStore)

		assert.NotNil(t, store)
		require.NotNil(t, store.operatorData)
		assert.Equal(t, spectypes.OperatorID(1), store.operatorData.ID)
		assert.True(t, store.operatorIDReady)
	})
}

func TestSetAndGetOperatorData(t *testing.T) {
	store := New(nil).(*operatorDataStore)

	data := &registrystorage.OperatorData{ID: 123}
	store.SetOperatorData(data)

	returnedData := store.GetOperatorData()
	assert.Equal(t, data, returnedData)
}

func TestSetOperatorDataConditions(t *testing.T) {
	t.Run("valid data sets operator ID ready", func(t *testing.T) {
		store := New(nil).(*operatorDataStore)
		data := &registrystorage.OperatorData{ID: 123}

		store.SetOperatorData(data)

		assert.True(t, store.OperatorIDReady(), "Operator ID should be marked as ready")
		assert.Equal(t, spectypes.OperatorID(123), store.operatorData.ID, "Operator ID should match the input data")
	})

	t.Run("nil data does not set operator ID ready", func(t *testing.T) {
		store := New(nil).(*operatorDataStore)

		store.SetOperatorData(nil)

		assert.False(t, store.OperatorIDReady(), "Operator ID should not be marked as ready with nil data")
		assert.Nil(t, store.operatorData, "Operator data should remain nil")
	})

	t.Run("data with zero ID does not set operator ID ready", func(t *testing.T) {
		store := New(nil).(*operatorDataStore)
		data := &registrystorage.OperatorData{ID: 0}

		store.SetOperatorData(data)

		assert.False(t, store.OperatorIDReady(), "Operator ID should not be marked as ready with zero ID")
		assert.Equal(t, spectypes.OperatorID(0), store.operatorData.ID, "Operator ID should be zero")
	})
}

func TestGetOperatorID(t *testing.T) {
	data := &registrystorage.OperatorData{ID: 123}
	store := New(data).(*operatorDataStore)

	operatorID := store.GetOperatorID()
	assert.Equal(t, spectypes.OperatorID(123), operatorID)
}

func TestOperatorIDReady(t *testing.T) {
	store := New(nil).(*operatorDataStore)
	assert.False(t, store.OperatorIDReady())

	data := &registrystorage.OperatorData{ID: 123}
	store.SetOperatorData(data)
	assert.True(t, store.OperatorIDReady())
}

func TestAwaitOperatorID(t *testing.T) {
	store := New(nil).(*operatorDataStore)

	go func() {
		time.Sleep(1 * time.Millisecond) // Simulate some operation delay
		data := &registrystorage.OperatorData{ID: 456}
		store.SetOperatorData(data)
	}()

	receivedID := store.AwaitOperatorID()
	assert.Equal(t, spectypes.OperatorID(456), receivedID)
}

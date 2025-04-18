package pebble

import (
	"bytes"
	"errors"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// Pebble DB txn requires its own set of tests because it has a different behaviour:
// - it doesn't support concurrent transactions
// - it doesn't support committing a transaction after it's been discarded

func TestPebbleTxn_Commit(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	err := txn.Commit()
	require.NoError(t, err)
}

func TestPebbleTxn_Discard(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	txn.Discard() // Should not panic
}

func TestPebbleTxn_Set(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value := []byte("test_value")

	err := txn.Set(prefix, key, value)
	require.NoError(t, err)

	// Verify the value was set
	obj, found, err := txn.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, key, obj.Key)
	require.Equal(t, value, obj.Value)
}

func TestPebbleTxn_SetMany(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")

	// Test data
	items := []basedb.Obj{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
	}

	err := txn.SetMany(prefix, len(items), func(i int) (basedb.Obj, error) {
		return items[i], nil
	})
	require.NoError(t, err)

	// Verify all items were set
	for _, item := range items {
		obj, found, err := txn.Get(prefix, item.Key)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, item.Key, obj.Key)
		require.Equal(t, item.Value, obj.Value)
	}
}

func TestPebbleTxn_Get(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value := []byte("test_value")

	// Test getting non-existent key
	_, found, err := txn.Get(prefix, key)
	require.NoError(t, err)
	require.False(t, found)

	// Set and then get
	err = txn.Set(prefix, key, value)
	require.NoError(t, err)

	obj, found, err := txn.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, key, obj.Key)
	require.Equal(t, value, obj.Value)
}

func TestPebbleTxn_GetMany(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")

	// Test data
	items := []basedb.Obj{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	// Set items
	for _, item := range items {
		err := txn.Set(prefix, item.Key, item.Value)
		require.NoError(t, err)
	}

	// Test getting all keys
	keys := make([][]byte, len(items))
	for i, item := range items {
		keys[i] = item.Key
	}

	var results []basedb.Obj
	err := txn.GetMany(prefix, keys, func(obj basedb.Obj) error {
		results = append(results, obj)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, len(items))

	// Verify results
	for i, result := range results {
		require.Equal(t, items[i].Key, result.Key)
		require.Equal(t, items[i].Value, result.Value)
	}
}

func TestPebbleTxn_GetAll(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")

	// Test data
	items := []basedb.Obj{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	// Set items
	for _, item := range items {
		err := txn.Set(prefix, item.Key, item.Value)
		require.NoError(t, err)
	}

	var results []basedb.Obj
	err := txn.GetAll(prefix, func(i int, obj basedb.Obj) error {
		results = append(results, obj)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, len(items))

	slices.SortFunc(results, func(a, b basedb.Obj) int {
		return bytes.Compare(a.Key, b.Key)
	})

	// Verify results
	for i, result := range results {
		require.Equal(t, items[i].Key, result.Key)
		require.Equal(t, items[i].Value, result.Value)
	}
}

func TestPebbleTxn_Delete(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value := []byte("test_value")

	// Set value
	err := txn.Set(prefix, key, value)
	require.NoError(t, err)

	// Verify value exists
	obj, found, err := txn.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, value, obj.Value)

	// Delete value
	err = txn.Delete(prefix, key)
	require.NoError(t, err)

	// Verify value is deleted
	obj, found, err = txn.Get(prefix, key)
	require.NoError(t, err)
	require.False(t, found)
}

func TestPebbleTxn_GetMany_EmptyKeys(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	keys := [][]byte{} // Empty keys list

	var results []basedb.Obj
	err := txn.GetMany(prefix, keys, func(obj basedb.Obj) error {
		results = append(results, obj)
		return nil
	})
	require.NoError(t, err)
	require.Empty(t, results)
}

func TestPebbleTxn_GetMany_IteratorError(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value := []byte("test_value")

	// Set a value
	err := txn.Set(prefix, key, value)
	require.NoError(t, err)

	// Test with iterator that returns an error
	keys := [][]byte{key}
	expectedErr := errors.New("iterator error")
	err = txn.GetMany(prefix, keys, func(obj basedb.Obj) error {
		return expectedErr
	})
	require.Error(t, err)
	require.Equal(t, expectedErr, err)
}

func TestPebbleTxn_GetAll_IteratorError(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value := []byte("test_value")

	// Set a value
	err := txn.Set(prefix, key, value)
	require.NoError(t, err)

	// Test with iterator that returns an error
	expectedErr := errors.New("iterator error")
	err = txn.GetAll(prefix, func(i int, obj basedb.Obj) error {
		return expectedErr
	})
	require.Error(t, err)
	require.Equal(t, expectedErr, err)
}

func TestPebbleTxn_SetMany_Error(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	prefix := []byte("test_prefix")
	expectedErr := errors.New("next function error")

	// Test with next function that returns an error
	err := txn.SetMany(prefix, 1, func(i int) (basedb.Obj, error) {
		return basedb.Obj{}, expectedErr
	})
	require.Error(t, err)
	require.Equal(t, expectedErr, err)
}

func TestPebbleTxn_CommitAfterDiscard(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	// Discard the transaction
	txn.Discard()

	// Attempting to commit after discard should panic
	// Note: This is a specific behavior mentioned in the comment at the top of the file
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic, but none")
		}
	}()

	_ = txn.Commit()
}

func TestPebbleTxn_BeginRead(t *testing.T) {
	db := newTestPebbleDB(t)

	// Test BeginRead returns a ReadTxn
	readTxn := db.BeginRead()
	require.NotNil(t, readTxn)

	// Verify it implements the Reader interface
	prefix := []byte("test_prefix")
	key := []byte("test_key")

	// Test Get on read transaction
	_, found, err := readTxn.Get(prefix, key)
	require.NoError(t, err)
	require.False(t, found)

	// Test Discard
	readTxn.Discard() // Should not panic
}

// Helper function to create a test PebbleDB instance
func newTestPebbleDB(t *testing.T) *PebbleDB {
	db, err := NewPebbleDB(zap.NewNop(), t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

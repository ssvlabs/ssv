package pebble

import (
	"context"
	"errors"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

func setupTestDB(t *testing.T) *PebbleDB {
	t.Helper()

	// Use t.TempDir() which automatically handles cleanup
	tmpDir := t.TempDir()

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	db, err := NewPebbleDB(context.Background(), logger, tmpDir, &pebble.Options{})
	require.NoError(t, err)

	return db
}

func TestPebbleDB_GetDelete(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	key := []byte("test-key")
	value := []byte("test-value")

	// Test Set
	err := db.Set(prefix, key, value)
	require.NoError(t, err)

	// Test Get
	obj, exists, err := db.Get(prefix, key)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, key, obj.Key)
	assert.Equal(t, value, obj.Value)

	// Test Get non-existent key
	obj, exists, err = db.Get(prefix, []byte("non-existent"))
	require.NoError(t, err)
	assert.False(t, exists)

	// Test Delete
	err = db.Delete(prefix, key)
	require.NoError(t, err)

	// Verify deletion
	obj, exists, err = db.Get(prefix, key)
	require.NoError(t, err)
	assert.False(t, exists)
}

func TestPebbleDB_GetMany(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}
	values := [][]byte{
		[]byte("value1"),
		[]byte("value2"),
		[]byte("value3"),
	}

	// Set up test data
	for i := range keys {
		require.NoError(t, db.Set(prefix, keys[i], values[i]))
	}

	// Test GetMany
	var retrieved []basedb.Obj
	err := db.GetMany(prefix, keys, func(obj basedb.Obj) error {
		retrieved = append(retrieved, obj)
		return nil
	})
	require.NoError(t, err)
	assert.Len(t, retrieved, len(keys))
}

func TestPebbleDB_GetAll(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	// Set up test data
	for k, v := range testData {
		require.NoError(t, db.Set(prefix, []byte(k), []byte(v)))
	}

	// Test GetAll
	count := 0
	err := db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		count++
		assert.Contains(t, testData, string(obj.Key))
		assert.Equal(t, testData[string(obj.Key)], string(obj.Value))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, len(testData), count)
}

func TestPebbleDB_Txn(t *testing.T) {
	db := setupTestDB(t)

	prefix := []byte("test-prefix")
	key := []byte("test-key")
	value := []byte("test-value")

	// Test successful transaction
	err := db.Update(func(txn basedb.Txn) error {
		return txn.Set(prefix, key, value)
	})
	require.NoError(t, err)

	// Verify the write
	obj, exists, err := db.Get(prefix, key)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, value, obj.Value)

	// Test transaction rollback
	err = db.Update(func(txn basedb.Txn) error {
		if err := txn.Set(prefix, key, []byte("new-value")); err != nil {
			return err
		}
		return errors.New("rollback")
	})
	assert.Error(t, err)

	// Verify the original value is unchanged
	obj, exists, err = db.Get(prefix, key)
	require.NoError(t, err)
	assert.True(t, exists)
	assert.Equal(t, value, obj.Value)
}

func TestPebbleDB_PrefixOperations(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")

	// Set up test data
	testData := map[string]string{
		"key1": "value1",
		"key2": "value2",
		"key3": "value3",
	}

	for k, v := range testData {
		require.NoError(t, db.Set(prefix, []byte(k), []byte(v)))
	}

	// Test CountPrefix
	count, err := db.CountPrefix(prefix)
	require.NoError(t, err)
	assert.Equal(t, int64(len(testData)), count)

	// Test DropPrefix
	err = db.DropPrefix(prefix)
	require.NoError(t, err)

	// Verify prefix was dropped
	count, err = db.CountPrefix(prefix)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
}

func TestPebbleDB_SetMany(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	testData := []basedb.Obj{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	// Test SetMany
	err := db.SetMany(prefix, len(testData), func(i int) (basedb.Obj, error) {
		return testData[i], nil
	})
	require.NoError(t, err)

	// Verify the data was written
	for _, expected := range testData {
		obj, exists, err := db.Get(prefix, expected.Key)
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, expected.Value, obj.Value)
	}
}

func TestPebbleDB_GC(t *testing.T) {
	db := setupTestDB(t)

	// Test QuickGC
	err := db.QuickGC(context.Background())
	require.NoError(t, err)

	// Test FullGC
	err = db.FullGC(context.Background())
	require.NoError(t, err)
}

func TestPebbleDB_CountPrefix(t *testing.T) {
	db := setupTestDB(t)

	prefix := []byte("test_prefix")
	keys := [][]byte{
		[]byte("key1"),
		[]byte("key2"),
		[]byte("key3"),
	}
	values := [][]byte{
		[]byte("value1"),
		[]byte("value2"),
		[]byte("value3"),
	}

	// Set values
	for i, key := range keys {
		err := db.Set(prefix, key, values[i])
		require.NoError(t, err)
	}

	// Count items with prefix
	count, err := db.CountPrefix(prefix)
	require.NoError(t, err)
	require.Equal(t, int64(3), count)

	// Count items with different prefix
	otherPrefix := []byte("other_prefix")
	count, err = db.CountPrefix(otherPrefix)
	require.NoError(t, err)
	require.Equal(t, int64(0), count)
}

func TestPebbleDB_DropPrefix(t *testing.T) {
	db := setupTestDB(t)

	prefix1 := []byte("prefix1")
	prefix2 := []byte("prefix2")

	// Set values with different prefixes
	err := db.Set(prefix1, []byte("key1"), []byte("value1"))
	require.NoError(t, err)
	err = db.Set(prefix1, []byte("key2"), []byte("value2"))
	require.NoError(t, err)
	err = db.Set(prefix2, []byte("key1"), []byte("value3"))
	require.NoError(t, err)

	// Drop prefix1
	err = db.DropPrefix(prefix1)
	require.NoError(t, err)

	// Verify prefix1 items are deleted
	_, found, err := db.Get(prefix1, []byte("key1"))
	require.NoError(t, err)
	require.False(t, found)

	_, found, err = db.Get(prefix1, []byte("key2"))
	require.NoError(t, err)
	require.False(t, found)

	// Verify prefix2 items still exist
	obj, found, err := db.Get(prefix2, []byte("key1"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("value3"), obj.Value)
}

func TestPebbleDB_Update(t *testing.T) {
	db := setupTestDB(t)

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value := []byte("test_value")

	// Update using transaction
	err := db.Update(func(txn basedb.Txn) error {
		return txn.Set(prefix, key, value)
	})
	require.NoError(t, err)

	// Verify value was set
	obj, found, err := db.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, value, obj.Value)

	// Test error handling
	expectedErr := errors.New("update error")
	err = db.Update(func(txn basedb.Txn) error {
		return expectedErr
	})
	require.Error(t, err)
	require.Equal(t, expectedErr, err)
}

func TestPebbleDB_QuickGC(t *testing.T) {
	db := setupTestDB(t)

	// QuickGC is currently a no-op, but we should test it doesn't error
	err := db.QuickGC(context.Background())
	require.NoError(t, err)
}

func TestPebbleDB_FullGC(t *testing.T) {
	db := setupTestDB(t)

	// FullGC is currently a no-op, but we should test it doesn't error
	err := db.FullGC(context.Background())
	require.NoError(t, err)
}

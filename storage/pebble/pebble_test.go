package pebble

import (
	"errors"
	"testing"

	"github.com/cockroachdb/pebble"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

func setupTestDB(t *testing.T) *DB {
	t.Helper()

	db, err := New(zap.NewNop(), t.TempDir(), &pebble.Options{})
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
	assert.Equal(t, key, obj.Key())
	assert.Equal(t, value, obj.Value())

	// Test Get non-existent key
	_, exists, err = db.Get(prefix, []byte("non-existent"))
	require.NoError(t, err)
	assert.False(t, exists)

	// Test Delete
	err = db.Delete(prefix, key)
	require.NoError(t, err)

	// Verify deletion
	_, exists, err = db.Get(prefix, key)
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
	for i, obj := range retrieved {
		assert.Equal(t, keys[i], obj.Key())
		assert.Equal(t, values[i], obj.Value())
	}

	// Test GetMany with non-existent keys
	err = db.GetMany(prefix, [][]byte{[]byte("non-existent")}, func(obj basedb.Obj) error {
		t.Errorf("expected no results, got %v", obj)
		return nil
	})
	require.NoError(t, err)

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
		assert.Equal(t, count, i)
		count++
		assert.Contains(t, testData, string(obj.Key()))
		assert.Equal(t, testData[string(obj.Key())], string(obj.Value()))
		return nil
	})
	require.NoError(t, err)
	assert.Equal(t, len(testData), count)
}

func TestPebbleDB_GetAll_Error(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	err := db.Set(prefix, []byte("key1"), []byte("value1"))
	require.NoError(t, err)

	err = db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		return errors.New("test error")
	})

	require.Error(t, err)
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
	assert.Equal(t, value, obj.Value())

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
	assert.Equal(t, value, obj.Value())
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
	testData := []Obj{
		{key: []byte("key1"), value: []byte("value1")},
		{key: []byte("key2"), value: []byte("value2")},
		{key: []byte("key3"), value: []byte("value3")},
	}

	// Test SetMany
	err := db.SetMany(prefix, len(testData), func(i int) (key, value []byte, err error) {
		return testData[i].key, testData[i].value, nil
	})
	require.NoError(t, err)

	// Verify the data was written
	for _, expected := range testData {
		obj, exists, err := db.Get(prefix, expected.Key())
		require.NoError(t, err)
		assert.True(t, exists)
		assert.Equal(t, expected.Value(), obj.Value())
	}
}

func TestPebbleDB_GC(t *testing.T) {
	db := setupTestDB(t)

	// Test QuickGC
	err := db.QuickGC(t.Context())
	require.NoError(t, err)

	// Test FullGC
	err = db.Update(func(txn basedb.Txn) error {
		_ = txn.Set([]byte("test-prefix"), []byte("test-key"), []byte("test-value"))
		_ = txn.Set([]byte("test-prefix"), []byte("test-key2"), []byte("test-value2"))
		return nil
	})
	require.NoError(t, err)

	err = db.FullGC(t.Context())
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
	assert.Equal(t, int64(3), count)

	// Count items with different prefix
	otherPrefix := []byte("other_prefix")
	count, err = db.CountPrefix(otherPrefix)
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)
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
	assert.Equal(t, []byte("value3"), obj.Value())
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
	assert.Equal(t, value, obj.Value())

	// Test error handling
	expectedErr := errors.New("update error")
	err = db.Update(func(txn basedb.Txn) error {
		return expectedErr
	})
	require.Error(t, err)
	assert.Equal(t, expectedErr, err)
}

func TestPebbleDB_Using(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	key := []byte("test-key")
	value := []byte("test-value")

	// Test Using with nil ReadWriter returns the database itself
	rw := db.Using(nil)
	require.NotNil(t, rw)
	assert.Equal(t, db, rw)

	// Test Using with a transaction
	txn := db.Begin()
	defer txn.Discard()

	// Set a value in the transaction
	err := txn.Set(prefix, key, value)
	require.NoError(t, err)

	// Use the transaction
	rw = db.Using(txn)
	require.NotNil(t, rw)
	assert.Equal(t, txn, rw)

	// Verify we can read the value through the returned ReadWriter
	obj, found, err := rw.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value, obj.Value())
}

func TestPebbleDB_UsingReader(t *testing.T) {
	db := setupTestDB(t)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	prefix := []byte("test-prefix")
	key := []byte("test-key")
	value := []byte("test-value")

	// First set a value in the database
	err := db.Set(prefix, key, value)
	require.NoError(t, err)

	// Test UsingReader with nil Reader returns the database itself
	r := db.UsingReader(nil)
	require.NotNil(t, r)
	assert.Equal(t, db, r)

	// Test UsingReader with a read transaction
	readTxn := db.BeginRead()
	defer readTxn.Discard()

	// Use the read transaction
	r = db.UsingReader(readTxn)
	require.NotNil(t, r)
	assert.Equal(t, readTxn, r)

	// Verify we can read the value through the returned Reader
	obj, found, err := r.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value, obj.Value())
}

package pebble

import (
	"bytes"
	"errors"
	"slices"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestPebbleTxn_Commit(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	err := txn.Commit()
	require.NoError(t, err)
}

func TestPebbleTxn_Discard(t *testing.T) {
	db := newTestPebbleDB(t)
	txn := db.Begin()

	assert.NotPanics(t, txn.Discard)
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
	assert.Equal(t, key, obj.Key)
	assert.Equal(t, value, obj.Value)
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
		assert.Equal(t, item.Key, obj.Key)
		assert.Equal(t, item.Value, obj.Value)
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
	assert.Equal(t, key, obj.Key)
	assert.Equal(t, value, obj.Value)
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
	err := txn.SetMany(prefix, len(items), func(i int) (basedb.Obj, error) {
		return items[i], nil
	})
	require.NoError(t, err)

	// Test getting all keys
	keys := make([][]byte, len(items))
	for i, item := range items {
		keys[i] = item.Key
	}

	var results []basedb.Obj
	err = txn.GetMany(prefix, keys, func(obj basedb.Obj) error {
		results = append(results, obj)
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, len(items))

	// Verify results
	for i, result := range results {
		assert.Equal(t, items[i].Key, result.Key)
		assert.Equal(t, items[i].Value, result.Value)
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
	var count int
	err := txn.GetAll(prefix, func(i int, obj basedb.Obj) error {
		assert.Equal(t, count, i)
		results = append(results, obj)
		count++
		return nil
	})
	require.NoError(t, err)
	require.Len(t, results, len(items))

	slices.SortFunc(results, func(a, b basedb.Obj) int {
		return bytes.Compare(a.Key, b.Key)
	})

	// Verify results
	for i, result := range results {
		assert.Equal(t, items[i].Key, result.Key)
		assert.Equal(t, items[i].Value, result.Value)
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
	assert.Equal(t, value, obj.Value)

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
	assert.Equal(t, expectedErr, err)
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
	assert.Equal(t, expectedErr, err)
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
	assert.Equal(t, expectedErr, err)
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

func TestPebbleTxn_Isolation(t *testing.T) {
	db := newTestPebbleDB(t)

	prefix := []byte("test_prefix")
	key := []byte("test_key")
	value1 := []byte("value1")
	value2 := []byte("value2")

	// Start two concurrent transactions
	txn1 := db.Begin()
	txn2 := db.Begin()

	// Write different values in each transaction
	require.NoError(t, txn1.Set(prefix, key, value1))
	require.NoError(t, txn2.Set(prefix, key, value2))

	// Verify each transaction sees its own value
	obj, found, err := txn1.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value1, obj.Value)

	obj, found, err = txn2.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value2, obj.Value)

	// Commit first transaction
	require.NoError(t, txn1.Commit())

	// Verify second transaction still sees its own value
	obj, found, err = txn2.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value2, obj.Value)
}

func TestPebbleReadTxn_GetAll(t *testing.T) {
	db := newTestPebbleDB(t)
	prefix := []byte("test_prefix")

	// Set up test data
	testData := []basedb.Obj{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	// Write data using a write transaction
	err := db.Update(func(txn basedb.Txn) error {
		for _, item := range testData {
			if err := txn.Set(prefix, item.Key, item.Value); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	// Test 1: Basic GetAll functionality
	t.Run("basic functionality", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		var results []basedb.Obj
		err := readTxn.GetAll(prefix, func(i int, obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, results, len(testData))

		// Sort results for consistent comparison
		slices.SortFunc(results, func(a, b basedb.Obj) int {
			return bytes.Compare(a.Key, b.Key)
		})

		for i, result := range results {
			require.Equal(t, testData[i].Key, result.Key)
			require.Equal(t, testData[i].Value, result.Value)
		}
	})

	// Test 2: Empty prefix
	t.Run("empty prefix", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		count := 0
		err := readTxn.GetAll([]byte{}, func(i int, obj basedb.Obj) error {
			count++
			return nil
		})
		require.NoError(t, err)
		require.Greater(t, count, 0) // Should find all keys in the database
	})

	// Test 3: Non-existent prefix
	t.Run("non-existent prefix", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		count := 0
		err := readTxn.GetAll([]byte("non_existent"), func(i int, obj basedb.Obj) error {
			count++
			return nil
		})
		require.NoError(t, err)
		require.Equal(t, 0, count)
	})

	// Test 4: Iterator error
	t.Run("iterator error", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		expectedErr := errors.New("iterator error")
		err := readTxn.GetAll(prefix, func(i int, obj basedb.Obj) error {
			return expectedErr
		})
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
	})

	// Test 5: Concurrent reads
	t.Run("concurrent reads", func(t *testing.T) {
		const numGoroutines = 10
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		for range numGoroutines {
			go func() {
				defer wg.Done()
				readTxn := db.BeginRead()
				defer readTxn.Discard()

				count := 0
				err := readTxn.GetAll(prefix, func(i int, obj basedb.Obj) error {
					count++
					return nil
				})
				require.NoError(t, err)
				require.Equal(t, len(testData), count)
			}()
		}

		wg.Wait()
	})
}

func TestPebbleReadTxn_GetMany(t *testing.T) {
	db := newTestPebbleDB(t)
	prefix := []byte("test_prefix")

	// Set up test data
	testData := []basedb.Obj{
		{Key: []byte("key1"), Value: []byte("value1")},
		{Key: []byte("key2"), Value: []byte("value2")},
		{Key: []byte("key3"), Value: []byte("value3")},
	}

	// Write data using a write transaction
	err := db.Update(func(txn basedb.Txn) error {
		for _, item := range testData {
			if err := txn.Set(prefix, item.Key, item.Value); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)

	// Test 1: Basic GetMany functionality
	t.Run("basic functionality", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		keys := make([][]byte, len(testData))
		for i, item := range testData {
			keys[i] = item.Key
		}

		var results []basedb.Obj
		err := readTxn.GetMany(prefix, keys, func(obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, results, len(testData))

		// Sort results for consistent comparison
		slices.SortFunc(results, func(a, b basedb.Obj) int {
			return bytes.Compare(a.Key, b.Key)
		})

		for i, result := range results {
			require.Equal(t, testData[i].Key, result.Key)
			require.Equal(t, testData[i].Value, result.Value)
		}
	})

	// Test 2: Empty keys list
	t.Run("empty keys list", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		var results []basedb.Obj
		err := readTxn.GetMany(prefix, [][]byte{}, func(obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})
		require.NoError(t, err)
		require.Empty(t, results)
	})

	// Test 3: Non-existent keys
	t.Run("non-existent keys", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		var results []basedb.Obj
		err := readTxn.GetMany(prefix, [][]byte{[]byte("non_existent")}, func(obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})
		require.NoError(t, err)
		require.Empty(t, results)
	})

	// Test 4: Mixed existent and non-existent keys
	t.Run("mixed keys", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		keys := [][]byte{
			testData[0].Key,
			[]byte("non_existent"),
			testData[1].Key,
		}

		var results []basedb.Obj
		err := readTxn.GetMany(prefix, keys, func(obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})
		require.NoError(t, err)
		require.Len(t, results, 2) // Should only find the two existing keys
	})

	// Test 5: Iterator error
	t.Run("iterator error", func(t *testing.T) {
		readTxn := db.BeginRead()
		defer readTxn.Discard()

		keys := [][]byte{testData[0].Key}
		expectedErr := errors.New("iterator error")
		err := readTxn.GetMany(prefix, keys, func(obj basedb.Obj) error {
			return expectedErr
		})
		require.Error(t, err)
		require.Equal(t, expectedErr, err)
	})

	// Test 6: Concurrent reads
	t.Run("concurrent reads", func(t *testing.T) {
		const numGoroutines = 10
		var wg sync.WaitGroup
		wg.Add(numGoroutines)

		keys := [][]byte{testData[0].Key, testData[1].Key}

		for range numGoroutines {
			go func() {
				defer wg.Done()
				readTxn := db.BeginRead()
				defer readTxn.Discard()

				count := 0
				err := readTxn.GetMany(prefix, keys, func(obj basedb.Obj) error {
					count++
					return nil
				})
				require.NoError(t, err)
				require.Equal(t, 2, count)
			}()
		}

		wg.Wait()
	})
}

// Helper function to create a test PebbleDB instance
func newTestPebbleDB(t *testing.T) *DB {
	db, err := New(zap.NewNop(), t.TempDir(), nil)
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})
	return db
}

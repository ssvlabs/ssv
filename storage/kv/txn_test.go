package kv

import (
	"errors"
	"fmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"testing"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// setupTxn creates a transaction for testing
func setupTxn(t *testing.T) (*BadgerDB, *badgerTxn) {
	t.Helper()
	db := setupDB(t, basedb.Options{})
	txn := db.Begin()

	require.NotNil(t, txn)

	return db, txn.(*badgerTxn)
}

// setupTxnWithData creates a transaction with some predefined data
func setupTxnWithData(t *testing.T, prefix []byte, keyCount int) (*BadgerDB, *badgerTxn) {
	t.Helper()
	db, txn := setupTxn(t)

	// Populate with test data
	for i := 0; i < keyCount; i++ {
		key := []byte(fmt.Sprintf("key-%d", i))
		value := []byte(fmt.Sprintf("value-%d", i))
		err := txn.Set(prefix, key, value)

		require.NoError(t, err)
	}

	return db, txn
}

// TestTxnCommit verifies that transaction changes are persisted when committed
func TestTxnCommit(t *testing.T) {
	db, txn := setupTxn(t)

	prefix := []byte("commit-prefix")
	key := []byte("commit-key")
	value := []byte("commit-value")

	err := txn.Set(prefix, key, value)

	require.NoError(t, err)

	_, found, err := db.Get(prefix, key)

	require.NoError(t, err)
	require.False(t, found)

	err = txn.Commit()

	require.NoError(t, err)

	obj, found, err := db.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, key, obj.Key)
	assert.Equal(t, value, obj.Value)
}

// TestTxnDiscard verifies that transaction changes are abandoned when discarded
func TestTxnDiscard(t *testing.T) {
	db, txn := setupTxn(t)

	prefix := []byte("discard-prefix")
	key := []byte("discard-key")
	value := []byte("discard-value")

	err := txn.Set(prefix, key, value)

	require.NoError(t, err)

	obj, found, err := txn.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value, obj.Value)

	txn.Discard()

	_, found, err = db.Get(prefix, key)

	require.NoError(t, err)
	require.False(t, found)
}

// TestTxnSet verifies transaction Set operations
func TestTxnSet(t *testing.T) {
	_, txn := setupTxn(t)
	prefix := []byte("set-prefix")

	t.Run("Basic set operation", func(t *testing.T) {
		key := []byte("set-key")
		value := []byte("set-value")

		err := txn.Set(prefix, key, value)

		require.NoError(t, err)

		obj, found, err := txn.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, key, obj.Key)
		assert.Equal(t, value, obj.Value)
	})

	t.Run("Overwrite existing value", func(t *testing.T) {
		key := []byte("overwrite-key")
		value1 := []byte("original-value")
		value2 := []byte("updated-value")

		err := txn.Set(prefix, key, value1)

		require.NoError(t, err)

		err = txn.Set(prefix, key, value2)

		require.NoError(t, err)

		obj, found, err := txn.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, value2, obj.Value)
	})

	txn.Commit()
}

// TestTxnSetMany verifies bulk setting of values
func TestTxnSetMany(t *testing.T) {
	_, txn := setupTxn(t)
	prefix := []byte("setmany-prefix")

	t.Run("Set multiple items", func(t *testing.T) {
		itemCount := 10

		err := txn.SetMany(prefix, itemCount, func(i int) (basedb.Obj, error) {
			key := []byte(fmt.Sprintf("key-%d", i))
			value := []byte(fmt.Sprintf("value-%d", i))

			return basedb.Obj{Key: key, Value: value}, nil
		})

		require.NoError(t, err)

		for i := 0; i < itemCount; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			expectedValue := []byte(fmt.Sprintf("value-%d", i))

			obj, found, err := txn.Get(prefix, key)

			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, expectedValue, obj.Value)
		}
	})

	t.Run("Error handling", func(t *testing.T) {
		expectedErr := errors.New("generator error")

		err := txn.SetMany(prefix, 5, func(i int) (basedb.Obj, error) {
			if i == 3 {
				return basedb.Obj{}, expectedErr
			}

			return basedb.Obj{Key: []byte{byte(i)}, Value: []byte{byte(i)}}, nil
		})

		assert.Equal(t, expectedErr, err)
	})

	txn.Commit()
}

// TestTxnGet verifies retrieval of values
func TestTxnGet(t *testing.T) {
	prefix := []byte("get-prefix")
	_, txn := setupTxnWithData(t, prefix, 3)

	t.Run("Get existing key", func(t *testing.T) {
		for i := 0; i < 3; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			expectedValue := []byte(fmt.Sprintf("value-%d", i))

			obj, found, err := txn.Get(prefix, key)

			require.NoError(t, err)
			require.True(t, found)
			assert.Equal(t, key, obj.Key)
			assert.Equal(t, expectedValue, obj.Value)
		}
	})

	t.Run("Get non-existent key", func(t *testing.T) {
		key := []byte("missing-key")

		obj, found, err := txn.Get(prefix, key)

		require.NoError(t, err)
		require.False(t, found)
		assert.Empty(t, obj.Value)
	})

	txn.Commit()
}

// TestTxnGetMany verifies retrieval of multiple values
func TestTxnGetMany(t *testing.T) {
	prefix := []byte("getmany-prefix")
	_, txn := setupTxnWithData(t, prefix, 10)

	t.Run("Get multiple existing keys", func(t *testing.T) {
		keysToGet := [][]byte{
			[]byte("key-1"),
			[]byte("key-3"),
			[]byte("key-7"),
		}

		results := make(map[string][]byte)
		err := txn.GetMany(prefix, keysToGet, func(obj basedb.Obj) error {
			results[string(obj.Key)] = obj.Value
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, len(keysToGet), len(results))

		// verify that each key has a value and it's the expected one
		for _, key := range keysToGet {
			keyStr := string(key)
			value, exists := results[keyStr]

			assert.True(t, exists, "Key %s should exist in results", keyStr)

			var keyNum int
			_, err := fmt.Sscanf(keyStr, "key-%d", &keyNum)

			require.NoError(t, err)

			expectedValue := fmt.Sprintf("value-%d", keyNum)

			assert.Equal(t, expectedValue, string(value))
		}
	})

	t.Run("Get with non-existent keys", func(t *testing.T) {
		keysToGet := [][]byte{
			[]byte("key-2"),
			[]byte("non-existent"),
		}

		results := make(map[string][]byte)
		err := txn.GetMany(prefix, keysToGet, func(obj basedb.Obj) error {
			results[string(obj.Key)] = obj.Value

			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 1, len(results))
		assert.Contains(t, results, "key-2")
	})

	t.Run("Empty keys array", func(t *testing.T) {
		var count int
		err := txn.GetMany(prefix, [][]byte{}, func(obj basedb.Obj) error {
			count++
			return nil
		})

		require.NoError(t, err)
		assert.Equal(t, 0, count)
	})

	t.Run("Iterator error handling", func(t *testing.T) {
		expectedErr := errors.New("iterator error")

		err := txn.GetMany(prefix, [][]byte{[]byte("key-0")}, func(obj basedb.Obj) error {
			return expectedErr
		})

		assert.Equal(t, expectedErr, err)
	})

	txn.Commit()
}

// TestTxnGetAll verifies retrieval of all values with a prefix
func TestTxnGetAll(t *testing.T) {
	prefix := []byte("getall-prefix")
	_, txn := setupTxnWithData(t, prefix, 20)

	t.Run("Get all items", func(t *testing.T) {
		var items []basedb.Obj
		err := txn.GetAll(prefix, func(i int, obj basedb.Obj) error {
			items = append(items, obj)

			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 20, len(items))

		keys := make(map[string]struct{}, 20)
		for _, item := range items {
			keys[string(item.Key)] = struct{}{}
		}

		assert.Equal(t, 20, len(keys))
	})

	t.Run("Handler error", func(t *testing.T) {
		expectedErr := errors.New("handler error")

		err := txn.GetAll(prefix, func(i int, obj basedb.Obj) error {
			if i >= 5 {
				return expectedErr
			}

			return nil
		})

		assert.Equal(t, expectedErr, err)
	})

	t.Run("Empty prefix", func(t *testing.T) {
		emptyPrefix := []byte("empty-prefix")

		var items []basedb.Obj
		err := txn.GetAll(emptyPrefix, func(i int, obj basedb.Obj) error {
			items = append(items, obj)

			return nil
		})

		require.NoError(t, err)
		assert.Empty(t, items)
	})

	txn.Commit()
}

// TestTxnDelete verifies deletion of values
func TestTxnDelete(t *testing.T) {
	prefix := []byte("delete-prefix")
	_, txn := setupTxnWithData(t, prefix, 5)

	t.Run("Delete existing key", func(t *testing.T) {
		keyToDelete := []byte("key-2")

		_, found, err := txn.Get(prefix, keyToDelete)

		require.NoError(t, err)
		require.True(t, found)

		err = txn.Delete(prefix, keyToDelete)
		require.NoError(t, err)

		_, found, err = txn.Get(prefix, keyToDelete)
		require.NoError(t, err)
		require.False(t, found)

		for i := 0; i < 5; i++ {
			if i == 2 {
				continue // we skipped this key
			}
			key := []byte(fmt.Sprintf("key-%d", i))
			_, found, err := txn.Get(prefix, key)

			require.NoError(t, err)
			require.True(t, found)
		}
	})

	t.Run("Delete non-existent key", func(t *testing.T) {
		nonExistentKey := []byte("non-existent")

		err := txn.Delete(prefix, nonExistentKey)
		require.NoError(t, err) // it's okay to delete a non-existent key
	})

	txn.Commit()
}

// TestTxnConsistentView verifies that transactions provide a consistent view
//  1. txn1 is created and sets the value of key to value1, then commits.
//  2. txn2 and txn3 are both started, and they both see the value value1 for key because txn1 has already committed.
//  3. txn2 sets the value of key to value2 but does not commit yet.
//  4. txn3 still sees the value value1 for key because it was started before txn2 made its changes.
//  5. txn2 commits its changes, so the value of key in the database is now value2.
//  6. txn3 still sees the value value1 for key because it is operating in its own isolated view of the database state
//     that was established when it was started.
func TestTxnConsistentView(t *testing.T) {
	db := setupDB(t, basedb.Options{})
	prefix := []byte("consistent-prefix")
	key := []byte("consistent-key")
	originalValue := []byte("original-value")

	err := db.Set(prefix, key, originalValue)

	require.NoError(t, err)

	readTxn := db.BeginRead().(*badgerTxn)

	obj1, found, err := readTxn.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, originalValue, obj1.Value)

	newValue := []byte("updated-value")
	err = db.Set(prefix, key, newValue)

	require.NoError(t, err)

	obj2, found, err := readTxn.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, originalValue, obj2.Value)

	// a new transaction should see the updated value
	newTxn := db.BeginRead().(*badgerTxn)
	obj3, found, err := newTxn.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, newValue, obj3.Value)

	readTxn.Discard()
	newTxn.Discard()
}

// TestTxnIsolation verifies that changes in one transaction don't affect others
func TestTxnIsolation(t *testing.T) {
	db := setupDB(t, basedb.Options{})
	prefix := []byte("isolation-prefix")
	key := []byte("isolation-key")

	txn1 := db.Begin().(*badgerTxn)

	value1 := []byte("value-from-txn1")
	err := txn1.Set(prefix, key, value1)
	require.NoError(t, err)

	err = txn1.Commit()
	require.NoError(t, err)

	obj, found, err := db.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value1, obj.Value)

	txn2 := db.Begin().(*badgerTxn)
	txn3 := db.Begin().(*badgerTxn)

	obj, found, err = txn2.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value1, obj.Value)

	obj, found, err = txn3.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value1, obj.Value)

	// update the value in txn2, but don't commit
	value2 := []byte("value-from-txn2")
	err = txn2.Set(prefix, key, value2)
	require.NoError(t, err)

	// txn3 should still see the original value
	obj, found, err = txn3.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value1, obj.Value)

	// commit txn2
	err = txn2.Commit()

	require.NoError(t, err)

	obj, found, err = db.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value2, obj.Value)

	// txn3 should still see the original value
	obj, found, err = txn3.Get(prefix, key)

	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, value1, obj.Value)

	txn3.Discard()
}

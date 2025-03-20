package kv

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/storage/basedb"
)

// setupDB creates a BadgerDB instance for testing with given options and handles cleanup.
func setupDB(t *testing.T, options basedb.Options) *BadgerDB {
	t.Helper()
	logger := logging.TestLogger(t)

	db, err := NewInMemory(logger, options)

	require.NoError(t, err)

	t.Cleanup(func() {
		err := db.Close()

		assert.NoError(t, err)
	})

	return db
}

// setupTempDir creates a temporary directory for disk-based DB tests.
func setupTempDir(t *testing.T, prefix string) string {
	t.Helper()
	dir, err := os.MkdirTemp("", prefix)
	require.NoError(t, err)

	t.Cleanup(func() {
		os.RemoveAll(dir)
	})

	return dir
}

// setupDataset populates a database with test data of specified size.
func setupDataset(t *testing.T, db *BadgerDB, prefix []byte, count int) {
	t.Helper()
	for i := 0; i < count; i++ {
		id := fmt.Sprintf("test-%d", i)
		err := db.Set(prefix, []byte(id), []byte(id+"-data"))

		require.NoError(t, err)
	}
}

// TestBadger verifies the Badger method returns the correct underlying badger.DB instance.
func TestBadger(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})

	badgerDB := db.Badger()

	require.NotNil(t, badgerDB)
	assert.Equal(t, badgerDB, db.Badger())
}

// TestBasicOperations verifies basic CRUD operations work correctly.
func TestBasicOperations(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})

	t.Run("set and get", func(t *testing.T) {
		prefix := []byte("test-prefix")
		key := []byte("test-key")
		value := []byte("test-value")

		require.NoError(t, db.Set(prefix, key, value))

		obj, found, err := db.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, key, obj.Key)
		assert.Equal(t, value, obj.Value)
	})

	t.Run("get non-existent", func(t *testing.T) {
		prefix := []byte("missing-prefix")
		key := []byte("missing-key")

		obj, found, err := db.Get(prefix, key)

		require.NoError(t, err)
		require.False(t, found)
		assert.Empty(t, obj.Value)
	})

	t.Run("delete", func(t *testing.T) {
		prefix := []byte("delete-prefix")
		key := []byte("delete-key")
		value := []byte("delete-value")

		require.NoError(t, db.Set(prefix, key, value))

		_, found, err := db.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)

		require.NoError(t, db.Delete(prefix, key))

		_, found, err = db.Get(prefix, key)

		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("drop prefix", func(t *testing.T) {
		prefix := []byte("drop-prefix")
		itemCount := 5

		for i := 0; i < itemCount; i++ {
			key := []byte(fmt.Sprintf("key-%d", i))
			value := []byte(fmt.Sprintf("value-%d", i))

			require.NoError(t, db.Set(prefix, key, value))
		}

		count := 0
		err := db.GetAll(prefix, func(i int, obj basedb.Obj) error {
			count++

			return nil
		})

		require.NoError(t, err)
		require.Equal(t, itemCount, count)

		require.NoError(t, db.DropPrefix(prefix))

		count = 0
		err = db.GetAll(prefix, func(i int, obj basedb.Obj) error {
			count++

			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 0, count)
	})
}

// TestGetAll verifies the GetAll method works with different dataset sizes.
func TestGetAll(t *testing.T) {
	t.Parallel()

	testCases := []struct {
		name      string
		itemCount int
	}{
		{"small dataset (100 items)", 100},
		{"medium dataset (1K items)", 1000},
		{"large dataset (5K items)", 5000},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := setupDB(t, basedb.Options{})
			prefix := []byte("test")

			setupDataset(t, db, prefix, tc.itemCount)
			time.Sleep(time.Millisecond)

			var all []basedb.Obj
			err := db.GetAll(prefix, func(i int, obj basedb.Obj) error {
				all = append(all, obj)

				return nil
			})

			require.NoError(t, err)
			assert.Equal(t, tc.itemCount, len(all))

			visited := make(map[string]struct{}, tc.itemCount)
			for _, item := range all {
				visited[string(item.Key)] = struct{}{}
			}

			assert.Equal(t, tc.itemCount, len(visited))
		})
	}

	t.Run("getAll with error", func(t *testing.T) {
		db := setupDB(t, basedb.Options{})
		prefix := []byte("error-prefix")
		setupDataset(t, db, prefix, 10)

		expectedErr := errors.New("handler error")
		err := db.GetAll(prefix, func(i int, obj basedb.Obj) error {
			if i > 5 {
				return expectedErr
			}

			return nil
		})

		assert.Equal(t, expectedErr, err)
	})

	t.Run("getAll empty prefix", func(t *testing.T) {
		db := setupDB(t, basedb.Options{})
		prefix := []byte("empty-prefix")

		var all []basedb.Obj
		err := db.GetAll(prefix, func(i int, obj basedb.Obj) error {
			all = append(all, obj)

			return nil
		})

		require.NoError(t, err)
		assert.Empty(t, all)
	})
}

// TestGetMany verifies the GetMany method retrieves multiple keys correctly.
func TestGetMany(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})
	prefix := []byte("prefix")

	for i := uint64(0); i < 100; i++ {
		require.NoError(t, db.Set(prefix, encodeUint64(i+1), encodeUint64(i+1)))
	}

	t.Run("get multiple keys", func(t *testing.T) {
		results := make([]basedb.Obj, 0)
		err := db.GetMany(prefix, [][]byte{
			encodeUint64(1),
			encodeUint64(2),
			encodeUint64(5),
			encodeUint64(10),
		}, func(obj basedb.Obj) error {

			require.True(t, bytes.Equal(obj.Key, obj.Value))
			results = append(results, obj)

			return nil
		})

		require.NoError(t, err)
		require.Equal(t, 4, len(results))
	})

	t.Run("empty keys array", func(t *testing.T) {
		results := make([]basedb.Obj, 0)
		err := db.GetMany(prefix, [][]byte{}, func(obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})

		require.NoError(t, err)
		require.Empty(t, results)
	})

	t.Run("non-existent key", func(t *testing.T) {
		results := make([]basedb.Obj, 0)
		err := db.GetMany(prefix, [][]byte{encodeUint64(999)}, func(obj basedb.Obj) error {
			results = append(results, obj)
			return nil
		})

		require.NoError(t, err)
		require.Empty(t, results)
	})

	t.Run("iterator error", func(t *testing.T) {
		expectedErr := errors.New("iterator error")
		err := db.GetMany(prefix, [][]byte{encodeUint64(1)}, func(obj basedb.Obj) error {
			return expectedErr
		})

		assert.Equal(t, expectedErr, err)
	})
}

// TestSetMany verifies the SetMany method stores multiple items in a single transaction.
func TestSetMany(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})
	prefix := []byte("prefix")

	t.Run("set multiple items", func(t *testing.T) {
		var values [][]byte
		err := db.SetMany(prefix, 10, func(i int) (basedb.Obj, error) {
			seq := uint64(i + 1)
			values = append(values, encodeUint64(seq))
			return basedb.Obj{Key: encodeUint64(seq), Value: encodeUint64(seq)}, nil
		})

		require.NoError(t, err)

		for i := 0; i < 10; i++ {
			seq := uint64(i + 1)
			obj, found, err := db.Get(prefix, encodeUint64(seq))

			require.NoError(t, err, "should find item %d", i)
			require.True(t, found, "should find item %d", i)
			require.True(t, bytes.Equal(obj.Value, values[i]), "item %d wrong value", i)
		}
	})

	t.Run("error in next function", func(t *testing.T) {
		expectedErr := errors.New("next error")
		err := db.SetMany(prefix, 10, func(i int) (basedb.Obj, error) {
			if i > 2 {
				return basedb.Obj{}, expectedErr
			}

			return basedb.Obj{Key: encodeUint64(uint64(i)), Value: encodeUint64(uint64(i))}, nil
		})

		assert.Equal(t, expectedErr, err)
	})
}

// TestSetMany_SetError verifies that errors from the WriteBatch.Set operation are correctly handled,
// We simulate an error by setting a key that's too large for BadgerDB (exceeds key size limit, usually ~64KB).
func TestSetMany_SetError(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})
	prefix := []byte("error-set-prefix")

	hugeKey := make([]byte, 100*1024) // 100KB key
	for i := range hugeKey {
		hugeKey[i] = byte(i % 256)
	}

	err := db.SetMany(prefix, 1, func(i int) (basedb.Obj, error) {
		return basedb.Obj{
			Key:   hugeKey,
			Value: []byte("value"),
		}, nil
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "exceeded")
}

// TestCountPrefix verifies the CountPrefix method correctly counts items with a given prefix.
func TestCountPrefix(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})
	prefix := []byte("count-prefix")

	t.Run("count existing prefix", func(t *testing.T) {
		for i := uint64(0); i < 100; i++ {
			require.NoError(t, db.Set(prefix, encodeUint64(i+1), encodeUint64(i+1)))
		}

		n, err := db.CountPrefix(prefix)

		require.NoError(t, err)
		require.Equal(t, int64(100), n)
	})

	t.Run("count non-existent prefix", func(t *testing.T) {
		n, err := db.CountPrefix([]byte("nonexistent"))

		require.NoError(t, err)
		require.Equal(t, int64(0), n)
	})
}

// TestUpdate verifies the Update method correctly modifies existing database entries.
func TestUpdate(t *testing.T) {
	t.Parallel()

	db := setupDB(t, basedb.Options{})
	prefix := []byte("update-prefix")
	key := []byte("update-key")
	value := []byte("original-value")

	require.NoError(t, db.Set(prefix, key, value))

	t.Run("successful update", func(t *testing.T) {
		newValue := []byte("updated-value")
		err := db.Update(func(txn basedb.Txn) error {
			return txn.Set(prefix, key, newValue)
		})

		require.NoError(t, err)

		obj, found, err := db.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, newValue, obj.Value)
	})

	t.Run("error in transaction", func(t *testing.T) {
		expectedErr := errors.New("transaction error")
		err := db.Update(func(txn basedb.Txn) error {
			return expectedErr
		})

		assert.Equal(t, expectedErr, err)
	})
}

// TestTransactions verifies transaction functionality for atomicity and isolation.
func TestTransactions(t *testing.T) {
	t.Parallel()

	t.Run("begin and Commit", func(t *testing.T) {
		db := setupDB(t, basedb.Options{})
		prefix := []byte("txn-prefix")
		key := []byte("txn-key")
		value := []byte("txn-value")

		txn := db.Begin()

		require.NotNil(t, txn)

		defer txn.Discard()

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
		assert.Equal(t, value, obj.Value)
	})

	t.Run("begin and discard", func(t *testing.T) {
		db := setupDB(t, basedb.Options{})
		prefix := []byte("discard-prefix")
		key := []byte("discard-key")
		value := []byte("discard-value")

		txn := db.Begin()

		require.NotNil(t, txn)

		err := txn.Set(prefix, key, value)

		require.NoError(t, err)

		txn.Discard()

		_, found, err := db.Get(prefix, key)

		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("begin read", func(t *testing.T) {
		db := setupDB(t, basedb.Options{})
		prefix := []byte("read-prefix")
		key := []byte("read-key")
		value := []byte("read-value")

		require.NoError(t, db.Set(prefix, key, value))

		txn := db.BeginRead()

		require.NotNil(t, txn)

		defer txn.Discard()

		obj, found, err := txn.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, value, obj.Value)

		newKey := []byte("new-key")
		newValue := []byte("new-value")

		require.NoError(t, db.Set(prefix, newKey, newValue))

		_, found, err = txn.Get(prefix, newKey)

		require.NoError(t, err)
		require.False(t, found)
	})

	t.Run("transaction get all", func(t *testing.T) {
		db := setupDB(t, basedb.Options{})
		prefix := []byte("txn-getall-prefix")
		itemCount := 10

		for i := 0; i < itemCount; i++ {
			key := []byte(fmt.Sprintf("key%d", i))
			value := []byte(fmt.Sprintf("value%d", i))

			require.NoError(t, db.Set(prefix, key, value))
		}

		txn := db.Begin()
		defer txn.Discard()

		var items []basedb.Obj
		err := txn.GetAll(prefix, func(i int, obj basedb.Obj) error {
			items = append(items, obj)
			return nil
		})

		require.NoError(t, err)
		require.Equal(t, itemCount, len(items))
	})
}

// TestDBCreation verifies different DB creation options work correctly.
func TestDBCreation(t *testing.T) {
	t.Parallel()

	t.Run("create disk-based DB", func(t *testing.T) {
		logger := logging.TestLogger(t)
		dir := setupTempDir(t, "badger-test")

		options := basedb.Options{
			Path: dir,
		}
		db, err := New(logger, options)
		require.NoError(t, err)

		defer db.Close()

		prefix := []byte("disk-prefix")
		key := []byte("disk-key")
		value := []byte("disk-value")

		require.NoError(t, db.Set(prefix, key, value))

		obj, found, err := db.Get(prefix, key)

		require.NoError(t, err)
		require.True(t, found)
		assert.Equal(t, value, obj.Value)
	})

	t.Run("create with GC enabled", func(t *testing.T) {
		logger := logging.TestLogger(t)
		dir := setupTempDir(t, "badger-gc-test")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		options := basedb.Options{
			Path:       dir,
			GCInterval: 100 * time.Millisecond,
			Ctx:        ctx,
		}
		db, err := New(logger, options)

		require.NoError(t, err)

		defer db.Close()

		time.Sleep(200 * time.Millisecond)

		err = db.QuickGC(context.Background())

		require.NoError(t, err)

		err = db.FullGC(context.Background())

		require.NoError(t, err)
	})

	t.Run("create with reporting", func(t *testing.T) {
		zapCore, observedLogs := observer.New(zap.DebugLevel)
		logger := zap.New(zapCore)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		options := basedb.Options{
			Reporting: true,
			Ctx:       ctx,
		}

		db, err := NewInMemory(logger, options)
		require.NoError(t, err)
		defer db.Close()

		logCountBefore := observedLogs.Len()
		db.report()
		logCountAfter := observedLogs.Len()

		require.Equal(t, logCountBefore+1, logCountAfter)
	})
}

// TestCreationDB_OpenError verifies error handling when badger.Open fails
// This test simulates an error by providing a file path instead of a directory
func TestCreationDB_OpenError(t *testing.T) {
	logger := logging.TestLogger(t)

	dir := setupTempDir(t, "badger-perm-test")

	badgerPath := filepath.Join(dir, "badger.db")
	err := os.WriteFile(badgerPath, []byte("not a directory"), 0644)
	require.NoError(t, err)

	options := basedb.Options{
		Path: badgerPath,
	}

	db, err := New(logger, options)

	assert.Nil(t, db)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open badger")
}

// TestHelperFunctions verifies the Using and UsingReader utility methods work correctly.
func TestHelperFunctions(t *testing.T) {
	t.Parallel()

	db1 := setupDB(t, basedb.Options{})
	db2 := setupDB(t, basedb.Options{})

	t.Run("using with nil", func(t *testing.T) {
		rw := db1.Using(nil)
		require.Equal(t, db1, rw)
	})

	t.Run("using with another DB", func(t *testing.T) {
		rw := db1.Using(db2)
		require.Equal(t, db2, rw)
	})

	t.Run("usingReader with nil", func(t *testing.T) {
		r := db1.UsingReader(nil)
		require.Equal(t, db1, r)
	})

	t.Run("usingReader with another DB", func(t *testing.T) {
		r := db1.UsingReader(db2)
		require.Equal(t, db2, r)
	})
}

// encodeUint64 converts uint64 to byte slice using little endian encoding.
func encodeUint64(n uint64) []byte {
	return binary.LittleEndian.AppendUint64(nil, n)
}

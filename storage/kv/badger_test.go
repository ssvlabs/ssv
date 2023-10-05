package kv

import (
	"bytes"
	"context"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/storage/basedb"
)

func TestBadgerEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	zapCore, observedLogs := observer.New(zap.DebugLevel)
	logger := zap.New(zapCore)
	options := basedb.Options{
		Reporting: true,
	}

	db, err := NewInMemory(ctx, logger, options)
	require.NoError(t, err)

	toSave := []struct {
		prefix []byte
		key    []byte
		value  []byte
	}{
		{
			[]byte("prefix1"),
			[]byte("key1"),
			[]byte("value"),
		},
		{
			[]byte("prefix1"),
			[]byte("key2"),
			[]byte("value"),
		},
		{
			[]byte("prefix2"),
			[]byte("key1"),
			[]byte("value"),
		},
	}

	for _, save := range toSave {
		require.NoError(t, db.Set(save.prefix, save.key, save.value))
	}

	obj, found, err := db.Get(toSave[0].prefix, toSave[0].key)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, toSave[0].key, obj.Key)
	require.EqualValues(t, toSave[0].value, obj.Value)

	count := 0
	err = db.GetAll(toSave[0].prefix, func(i int, obj basedb.Obj) error {
		count++
		return nil
	})
	require.NoError(t, err)
	require.EqualValues(t, 2, count)

	obj, found, err = db.Get(toSave[2].prefix, toSave[2].key)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, toSave[2].key, obj.Key)
	require.EqualValues(t, toSave[2].value, obj.Value)

	logCountBeforeReport := observedLogs.Len()
	db.report()
	logCountAfterReport := observedLogs.Len()
	require.Equal(t, logCountBeforeReport+1, logCountAfterReport)

	require.NoError(t, db.Delete(toSave[0].prefix, toSave[0].key))
	obj, found, err = db.Get(toSave[0].prefix, toSave[0].key)
	require.NoError(t, err)
	require.False(t, found)

	require.NoError(t, db.DropPrefix([]byte("prefix2")))
	deleted, err := db.DeletePrefix([]byte("prefix1"))
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
}

func TestBadgerDb_GetAll(t *testing.T) {
	logger := logging.TestLogger(t)

	t.Run("100_items", func(t *testing.T) {
		db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		getAllTest(t, 100, db)
	})

	t.Run("10K_items", func(t *testing.T) {
		db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		getAllTest(t, 10000, db)
	})

	t.Run("100K_items", func(t *testing.T) {
		db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
		require.NoError(t, err)
		defer db.Close()

		getAllTest(t, 100000, db)
	})
}

func TestBadgerDb_GetMany(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	prefix := []byte("prefix")
	var i uint64
	for i = 0; i < 100; i++ {
		require.NoError(t, db.Set(prefix, uInt64ToByteSlice(i+1), uInt64ToByteSlice(i+1)))
	}

	results := make([]basedb.Obj, 0)
	err = db.GetMany(prefix, [][]byte{uInt64ToByteSlice(1), uInt64ToByteSlice(2),
		uInt64ToByteSlice(5), uInt64ToByteSlice(10)}, func(obj basedb.Obj) error {
		require.True(t, bytes.Equal(obj.Key, obj.Value))
		results = append(results, obj)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 4, len(results))
}

func TestBadgerDb_SetMany(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	prefix := []byte("prefix")
	var values [][]byte
	err = db.SetMany(prefix, 10, func(i int) (basedb.Obj, error) {
		seq := uint64(i + 1)
		values = append(values, uInt64ToByteSlice(seq))
		return basedb.Obj{Key: uInt64ToByteSlice(seq), Value: uInt64ToByteSlice(seq)}, nil
	})
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		seq := uint64(i + 1)
		obj, found, err := db.Get(prefix, uInt64ToByteSlice(seq))
		require.NoError(t, err, "should find item %d", i)
		require.True(t, found, "should find item %d", i)
		require.True(t, bytes.Equal(obj.Value, values[i]), "item %d wrong value", i)
	}
}

func TestBadgerDb_IsEmpty(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	// Initially, the DB should be empty
	isEmpty, err := db.IsEmpty()
	require.NoError(t, err)
	require.True(t, isEmpty, "DB should be empty initially")

	// Add an item to the DB
	prefix := []byte("prefix")
	key := []byte("key")
	value := []byte("value")
	require.NoError(t, db.Set(prefix, key, value))

	// Now, the DB should not be empty
	isEmpty, err = db.IsEmpty()
	require.NoError(t, err)
	require.False(t, isEmpty, "DB should not be empty after adding an item")

	// Delete the item from the DB
	require.NoError(t, db.Delete(prefix, key))

	// The DB should be empty again
	isEmpty, err = db.IsEmpty()
	require.NoError(t, err)
	require.True(t, isEmpty, "DB should be empty after deleting the item")
}

func TestBadgerDb_SetType_and_GetType(t *testing.T) {
	logger := logging.TestLogger(t)
	db, err := NewInMemory(context.TODO(), logger, basedb.Options{})
	require.NoError(t, err)
	defer db.Close()

	// Set the database type
	dbType := "testType"
	err = db.SetType(dbType)
	require.NoError(t, err, "Setting db type should not produce an error")

	// Get the database type
	gotDbType, found, err := db.GetType()
	require.NoError(t, err, "Getting db type should not produce an error")
	require.True(t, found, "DB type should be found")
	require.Equal(t, dbType, gotDbType, "DB type should match the set value")

	// Ensure the db type is correct after adding some data
	prefix := []byte("prefix")
	key := []byte("key")
	value := []byte("value")
	require.NoError(t, db.Set(prefix, key, value))

	gotDbType, found, err = db.GetType()
	require.NoError(t, err, "Getting db type should not produce an error after adding data")
	require.True(t, found, "DB type should be found after adding data")
	require.Equal(t, dbType, gotDbType, "DB type should match the set value after adding data")
}

func uInt64ToByteSlice(n uint64) []byte {
	b := make([]byte, 8)
	binary.LittleEndian.PutUint64(b, n)
	return b
}

func getAllTest(t *testing.T, n int, db basedb.Database) {
	// populating DB
	prefix := []byte("test")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("test-%d", i)
		require.NoError(t, db.Set(prefix, []byte(id), []byte(id+"-data")))
	}
	time.Sleep(1 * time.Millisecond)

	var all []basedb.Obj
	err := db.GetAll(prefix, func(i int, obj basedb.Obj) error {
		all = append(all, obj)
		return nil
	})
	require.Equal(t, n, len(all))
	require.NoError(t, err)
	visited := map[string][]byte{}
	for _, item := range all {
		visited[string(item.Key)] = item.Value
	}
	require.Equal(t, n, len(visited))
	count, err := db.DeletePrefix(prefix)
	require.NoError(t, err)
	require.Equal(t, n, count)
}

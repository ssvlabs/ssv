package kv

import (
	"bytes"
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bloxapp/ssv/storage/basedb"
)

func TestTxn_Set(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	zapCore, _ := observer.New(zap.DebugLevel)
	logger := zap.New(zapCore)
	options := basedb.Options{
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := NewInMemory(logger, options)
	require.NoError(t, err)
	bdb := db.Badger()
	txn := bdb.NewTransaction(true)
	tx := &badgerTxn{
		txn: txn,
		db:  db,
	}
	defer tx.Discard()
	prefix := []byte("prefix")
	key := []byte("key")
	value := []byte("value")
	err = tx.Set(prefix, key, value)
	require.NoError(t, err)
	obj, found, err := tx.Get(prefix, key)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, value, obj.Value)
	err = tx.Commit()
	require.NoError(t, err)
}

func TestTxn_SetMany(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	zapCore, _ := observer.New(zap.DebugLevel)
	logger := zap.New(zapCore)
	options := basedb.Options{
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := NewInMemory(logger, options)
	require.NoError(t, err)
	bdb := db.Badger()
	txn := bdb.NewTransaction(true)
	tx := &badgerTxn{
		txn: txn,
		db:  db,
	}
	defer tx.Discard()
	prefix := []byte("prefix")
	var values [][]byte
	err = tx.SetMany(prefix, 10, func(i int) (basedb.Obj, error) {
		seq := uint64(i + 1)
		values = append(values, uInt64ToByteSlice(seq))
		return basedb.Obj{Key: uInt64ToByteSlice(seq), Value: uInt64ToByteSlice(seq)}, nil
	})
	require.NoError(t, err)
	for i := 0; i < 10; i++ {
		seq := uint64(i + 1)
		obj, found, err := tx.Get(prefix, uInt64ToByteSlice(seq))
		require.NoError(t, err, "should find item %d", i)
		require.True(t, found, "should find item %d", i)
		require.True(t, bytes.Equal(obj.Value, values[i]), "item %d wrong value", i)
	}
}

func TestTxn_GetMany(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	zapCore, _ := observer.New(zap.DebugLevel)
	logger := zap.New(zapCore)
	options := basedb.Options{
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := NewInMemory(logger, options)
	require.NoError(t, err)
	bdb := db.Badger()
	txn := bdb.NewTransaction(true)
	tx := &badgerTxn{
		txn: txn,
		db:  db,
	}
	defer tx.Discard()
	prefix := []byte("prefix")
	var i uint64
	for i = 0; i < 100; i++ {
		require.NoError(t, tx.Set(prefix, uInt64ToByteSlice(i+1), uInt64ToByteSlice(i+1)))
	}

	results := make([]basedb.Obj, 0)
	err = tx.GetMany(prefix, [][]byte{uInt64ToByteSlice(1), uInt64ToByteSlice(2),
		uInt64ToByteSlice(5), uInt64ToByteSlice(10)}, func(obj basedb.Obj) error {
		require.True(t, bytes.Equal(obj.Key, obj.Value))
		results = append(results, obj)
		return nil
	})
	require.NoError(t, err)
	require.Equal(t, 4, len(results))
}

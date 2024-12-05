package kv

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"

	"github.com/bloxapp/ssv/storage/basedb"
)

func TestGC(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	zapCore, _ := observer.New(zap.DebugLevel)
	logger := zap.New(zapCore)
	options := basedb.Options{
		Reporting:  true,
		Ctx:        ctx,
		Path:       os.TempDir(),
		GCInterval: 5,
	}

	db, err := New(logger, options)
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
	err = db.FullGC(db.ctx)
	require.NoError(t, err)
}

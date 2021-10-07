package kv

import (
	"context"
	"fmt"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
	"time"
)

func TestBadgerEndToEnd(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	options := basedb.Options{
		Type:      "badger-memory",
		Logger:    zap.L(),
		Path:      "",
		Reporting: true,
		Ctx:       ctx,
	}

	db, err := New(options)
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

	objs, err := db.GetAllByCollection(toSave[0].prefix)
	require.NoError(t, err)
	require.EqualValues(t, 2, len(objs))

	obj, found, err = db.Get(toSave[2].prefix, toSave[2].key)
	require.True(t, found)
	require.NoError(t, err)
	require.EqualValues(t, toSave[2].key, obj.Key)
	require.EqualValues(t, toSave[2].value, obj.Value)

	db.(*BadgerDb).report()

	require.NoError(t, db.RemoveAllByCollection([]byte("prefix1")))
	require.NoError(t, db.RemoveAllByCollection([]byte("prefix2")))
}

func TestBadgerDb_GetAllByCollection(t *testing.T) {
	options := basedb.Options{
		Type:   "badger-memory",
		Logger: zap.L(),
		Path:   "",
	}

	t.Run("100_items", func(t *testing.T) {
		db, err := New(options)
		require.NoError(t, err)
		defer db.Close()

		getAllByCollectionTest(t, 100, db)
	})

	t.Run("10K_items", func(t *testing.T) {
		db, err := New(options)
		require.NoError(t, err)
		defer db.Close()

		getAllByCollectionTest(t, 10000, db)
	})

	t.Run("100K_items", func(t *testing.T) {
		db, err := New(options)
		require.NoError(t, err)
		defer db.Close()

		getAllByCollectionTest(t, 100000, db)
	})
}

func getAllByCollectionTest(t *testing.T, n int, db basedb.IDb) {
	// populating DB
	prefix := []byte("test")
	for i := 0; i < n; i++ {
		id := fmt.Sprintf("test-%d", i)
		db.Set(prefix, []byte(id), []byte(id+"-data"))
	}
	time.Sleep(1 * time.Millisecond)

	all, err := db.GetAllByCollection(prefix)
	require.Equal(t, n, len(all))
	require.NoError(t, err)
	visited := map[string][]byte{}
	for _, item := range all {
		visited[string(item.Key[:])] = item.Value[:]
	}
	require.Equal(t, n, len(visited))
	require.NoError(t, db.RemoveAllByCollection(prefix))
}

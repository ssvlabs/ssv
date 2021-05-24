package inmem

import (
	"github.com/stretchr/testify/require"
	"testing"
)

func TestInMemStorage(t *testing.T) {
	storage := New()
	require.NoError(t, storage.Set([]byte("prefix"), []byte("key"), []byte("value")))

	obj, err := storage.Get([]byte("prefix"), []byte("key"))
	require.NoError(t, err)
	require.EqualValues(t, []byte("key"), obj.Key)
	require.EqualValues(t, []byte("value"), obj.Value)

	obj, err = storage.Get([]byte("prefix"), []byte("no key"))
	require.EqualError(t, err, "not found")
}

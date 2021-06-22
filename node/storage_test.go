package node

import (
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"testing"
)

func TestStorage_SaveAndGetSyncOffset(t *testing.T) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	require.NoError(t, err)
	s := NewSSVNodeStorage(db, logger)

	offset := new(SyncOffset)
	offset.SetString("49e08f", 16)
	err = s.SaveSyncOffset(offset)
	require.NoError(t, err)

	o, err := s.GetSyncOffset()
	require.NoError(t, err)
	require.Zero(t, offset.Cmp(o))
}

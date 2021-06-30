package storage

import (
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"testing"
)

func TestExporterStorage_SaveAndGetSyncOffset(t *testing.T) {
	s, done := newStorageForTest()
	require.NotNil(t, s)
	defer done()

	offset := new(big.Int)
	offset.SetString("49e08f", 16)
	err := s.SaveSyncOffset(offset)
	require.NoError(t, err)

	o, err := s.GetSyncOffset()
	require.NoError(t, err)
	require.Zero(t, offset.Cmp(o))
}

func newStorageForTest() (Storage, func()) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	if err != nil {
		return nil, func() {}
	}
	s := NewExporterStorage(db, logger)
	return s, func() {
		db.Close()
	}
}

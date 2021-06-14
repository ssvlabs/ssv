package exporter

import (
	ssvstorage "github.com/bloxapp/ssv/storage"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"math/big"
	"testing"
)

func TestExporterStorage_SaveAndGetSyncOffset(t *testing.T) {
	logger := zap.L()
	db, err := ssvstorage.GetStorageFactory(basedb.Options{
		Type:   "badger-memory",
		Logger: logger,
		Path:   "",
	})
	require.NoError(t, err)
	s := NewExporterStorage(db, logger)

	offset := new(big.Int)
	offset.SetString("49e08f", 16)
	err = s.SaveSyncOffset(offset)
	require.NoError(t, err)

	o, err := s.GetSyncOffset()
	require.NoError(t, err)
	require.Zero(t, offset.Cmp(o))
}

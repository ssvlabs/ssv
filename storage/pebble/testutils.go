package pebble

import (
	"errors"
	"os"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type TempDB struct {
	*DB
	cleanupPath string
}

func (pdb *TempDB) Close() error {
	var err error
	if pdb.DB != nil {
		err = pdb.DB.Close()
	}
	if pdb.cleanupPath != "" {
		err = errors.Join(err, os.RemoveAll(pdb.cleanupPath))
	}
	return err
}

// NewTempDB creates a temporary Pebble-backed DB and removes it on Close.
func NewTempDB(logger *zap.Logger, _ basedb.Options) (*TempDB, error) {
	path, err := os.MkdirTemp("", "ssv-pebble-test-*")
	if err != nil {
		return nil, err
	}

	db, err := New(logger, path, &pebble.Options{})
	if err != nil {
		_ = os.RemoveAll(path)
		return nil, err
	}

	return &TempDB{
		DB:          db,
		cleanupPath: path,
	}, nil
}

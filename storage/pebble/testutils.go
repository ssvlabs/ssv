package pebble

import (
	"errors"
	"os"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

type DBTemporary struct {
	*DB
	cleanupPath string
}

func (pdb *DBTemporary) Close() error {
	var err error
	if pdb.DB != nil {
		err = pdb.DB.Close()
	}
	if pdb.cleanupPath != "" {
		err = errors.Join(err, os.RemoveAll(pdb.cleanupPath))
	}
	return err
}

// NewTemporary creates a temporary Pebble-backed DB and removes it on Close.
func NewTemporary(logger *zap.Logger, _ basedb.Options) (*DBTemporary, error) {
	path, err := os.MkdirTemp("", "ssv-pebble-test-*")
	if err != nil {
		return nil, err
	}

	db, err := New(logger, path, &pebble.Options{})
	if err != nil {
		_ = os.RemoveAll(path)
		return nil, err
	}

	return &DBTemporary{
		DB:          db,
		cleanupPath: path,
	}, nil
}

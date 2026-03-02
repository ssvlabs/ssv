package pebble

import (
	"os"

	"github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// NewTemporary creates a temporary Pebble-backed DB and removes it on Close.
func NewTemporary(logger *zap.Logger, _ basedb.Options) (*DB, error) {
	path, err := os.MkdirTemp("", "ssv-pebble-test-*")
	if err != nil {
		return nil, err
	}

	db, err := New(logger, path, &pebble.Options{})
	if err != nil {
		_ = os.RemoveAll(path)
		return nil, err
	}
	db.cleanupPath = path

	return db, nil
}

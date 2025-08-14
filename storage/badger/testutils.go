//go:build testutils

package badger

import (
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

// NewInMemory creates an in-memory DB instance.
func NewInMemory(logger *zap.Logger, options basedb.Options) (*DB, error) {
	return createDB(logger, options, true)
}

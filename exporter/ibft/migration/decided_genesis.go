package migration

import (
	"github.com/bloxapp/ssv/exporter/ibft"
	"github.com/bloxapp/ssv/ibft/sync/history"
	"github.com/bloxapp/ssv/storage/collections"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

// decidedReader is the required interface for running the migrator
type decidedReader interface {
	NewHistorySync() *history.Sync
	IBFTStorage() collections.Iibft
	Identifier() []byte
}

// decidedGenesisMigrator
type decidedGenesisMigrator struct {
	logger *zap.Logger
}

// Migrate take care of decided messages migration
func (m *decidedGenesisMigrator) Migrate(r ibft.Reader) error {
	dr, ok := r.(decidedReader)
	if !ok {
		return nil
	}
	if err := m.migrate(dr); err != nil {
		return errors.Wrap(err, "could not migrate decided 0")
	}
	m.logger.Debug("managed to migrate decided 0", zap.String("identifier", string(dr.Identifier())))
	return nil
}

// migrate performing migration for decided messages
func (m *decidedGenesisMigrator) migrate(dr decidedReader) error {
	if migrateDecided0, err := m.needToMigrate(dr); err != nil {
		return err
	} else if migrateDecided0 {
		return dr.NewHistorySync().StartRange(uint64(0), uint64(1))
	}
	return nil
}

// needToMigrate determines if the given reader should migrate
func (m *decidedGenesisMigrator) needToMigrate(dr decidedReader) (bool, error) {
	_, found, err := dr.IBFTStorage().GetDecided(dr.Identifier(), uint64(0))
	if err != nil {
		return false, err
	}
	if found {
		return false, nil
	}
	return true, nil
}

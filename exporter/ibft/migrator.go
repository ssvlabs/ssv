package ibft

import (
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap"
	"sync"
)

// Migrator is an interface for migrating messages
type Migrator interface {
	Migrate(r Reader) error
}

type mainMigrator struct {
	migrators []Migrator
}

var migrator mainMigrator

var migratorOnce sync.Once

// GetMainMigrator returns the instance of migrator
func GetMainMigrator() Migrator {
	migratorOnce.Do(func() {
		logger := logex.GetLogger(zap.String("who", "migrateManager"))
		// TODO: un-lint
		migrators := []Migrator{&decidedGenesisMigrator{logger}} //nolint
		migrator = mainMigrator{
			migrators: migrators,
		}
	})
	return &migrator
}

// Migrate applies the existing migrators on the given reader
func (mm *mainMigrator) Migrate(r Reader) error {
	for _, migrator := range mm.migrators {
		if err := migrator.Migrate(r); err != nil {
			return err
		}
	}
	return nil
}

package ibft

import (
	"sync"
)

// Migrator is an interface for migrating messages
type Migrator interface {
	Migrate(r Reader) error
}

type mainMigrator struct {
	migrators []Migrator
}

// TODO: un-lint
//nolint
var migrator mainMigrator

// TODO: un-lint
//nolint
var migratorOnce sync.Once

// GetMainMigrator TODO: un-lint
//nolint
//GetMainMigrator returns the instance of migrator
//func GetMainMigrator() Migrator {
//	migratorOnce.Do(func() {
//		logger := logex.GetLogger(zap.String("who", "migrateManager"))
//		migrators := []Migrator{&decidedGenesisMigrator{logger}} //nolint
//		migrator = mainMigrator{
//			migrators: migrators,
//		}
//	})
//	return &migrator
//}

// Migrate applies the existing migrators on the given reader
func (mm *mainMigrator) Migrate(r Reader) error {
	for _, migrator := range mm.migrators {
		if err := migrator.Migrate(r); err != nil {
			return err
		}
	}
	return nil
}

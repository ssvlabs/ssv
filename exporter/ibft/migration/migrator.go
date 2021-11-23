package migration

import (
	"github.com/bloxapp/ssv/exporter/ibft"
	"github.com/bloxapp/ssv/utils/logex"
	"go.uber.org/zap"
	"sync"
)

type Migrator interface {
	Migrate(r ibft.Reader) error
}

type MainMigrator struct {
	migrators []Migrator
}

var mainMigrator MainMigrator

var once sync.Once

func GetMainMigrator() Migrator {
	once.Do(func() {
		logger := logex.GetLogger(zap.String("who", "migrateManager"))
		migrators := []Migrator{&decidedGenesisMigrator{logger}}
		mainMigrator = MainMigrator{
			migrators: migrators,
		}
	})
	return &mainMigrator
}

func (mm *MainMigrator) Migrate(r ibft.Reader) error {
	for _, migrator := range mm.migrators {
		if err := migrator.Migrate(r); err != nil {
			return err
		}
	}
	return nil
}

package migrations

import (
	"bytes"
	"context"

	exporterstorage "github.com/bloxapp/ssv/exporter/storage"
	"github.com/bloxapp/ssv/operator"
	"github.com/bloxapp/ssv/storage/basedb"
	validatorstorage "github.com/bloxapp/ssv/validator/storage"
	"github.com/pkg/errors"
	"go.uber.org/zap"
)

var (
	migrationsPrefix   = []byte("migrations/")
	migrationCompleted = []byte("migrationCompleted")

	defaultMigrations = Migrations{
		migrationExample1,
		migrationExample2,
		migrationCleanAllRegistryData,
	}
)

// Run executes the default migrations.
func Run(ctx context.Context, opt Options) error {
	return defaultMigrations.Run(ctx, opt)
}

// MigrationFunc is a function that performs a migration.
type MigrationFunc func(ctx context.Context, opt Options, key []byte) error

// Migration is a named MigrationFunc.
type Migration struct {
	Name string
	Run  MigrationFunc
}

// Migrations is a slice of named migrations, meant to be executed
// from first to last (order is significant).
type Migrations []Migration

// Options are configurations for migrations
type Options struct {
	Db     basedb.IDb
	Logger *zap.Logger
	// Storage Storage
	// ExporterStorage ExporterStorage
}

func (o Options) exporterStorage() exporterstorage.Storage {
	return exporterstorage.NewExporterStorage(o.Db, o.Logger)
}

func (o Options) validatorStorage() validatorstorage.ICollection {
	opt := validatorstorage.CollectionOptions{
		DB:     o.Db,
		Logger: o.Logger,
	}
	return validatorstorage.NewCollection(opt)
}

func (o Options) operatorStorage() operator.Storage {
	return operator.NewOperatorNodeStorage(o.Db, o.Logger)
}

func (o Options) cleanAll() error {
	if err := o.exporterStorage().Clean(); err != nil {
		return err
	}
	if err := o.validatorStorage().CleanAllShares(); err != nil {
		return err
	}
	return o.operatorStorage().CleanSyncOffset()
}

// Run executes the migrations.
func (m Migrations) Run(ctx context.Context, opt Options) error {
	for _, migration := range m {
		// Skip the migration if it's already completed.
		obj, _, err := opt.Db.Get(migrationsPrefix, []byte(migration.Name))
		if err != nil {
			return err
		}
		if bytes.Equal(obj.Value, migrationCompleted) {
			continue
		}

		// Execute the migration.
		if err := migration.Run(ctx, opt, []byte(migration.Name)); err != nil {
			return errors.Wrapf(err, "migration %q failed", migration.Name)
		}
	}

	return nil
}

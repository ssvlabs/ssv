package migrations

import (
	"bytes"
	"context"
	"time"

	"github.com/bloxapp/ssv/logging/fields"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	migrationsPrefix   = []byte("migrations/")
	migrationCompleted = []byte("migrationCompleted")

	defaultMigrations = Migrations{
		migrationExample1,
		migrationExample2,
	}
)

// Run executes the default migrations.
func Run(ctx context.Context, logger *zap.Logger, opt Options) (applied int, err error) {
	return defaultMigrations.Run(ctx, logger, opt)
}

// MigrationFunc is a function that performs a migration.
type MigrationFunc func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error

// Migration is a named MigrationFunc.
type Migration struct {
	Name string
	Run  MigrationFunc
}

// Migrations is a slice of named migrations, meant to be executed
// from first to last (order is significant).
type Migrations []Migration

// Options is the options for running migrations.
type Options struct {
	Db      basedb.Database
	DbPath  string
	Network beacon.Network
}

// nolint
func (o Options) getRegistryStores(logger *zap.Logger) ([]eth1.RegistryStore, error) {
	nodeStorage, err := o.nodeStorage(logger)
	if err != nil {
		return nil, err
	}
	return []eth1.RegistryStore{nodeStorage, o.signerStorage(logger)}, nil
}

// nolint
func (o Options) nodeStorage(logger *zap.Logger) (operatorstorage.Storage, error) {
	return operatorstorage.NewNodeStorage(logger, o.Db)
}

// nolint
func (o Options) signerStorage(logger *zap.Logger) ekm.Storage {
	return ekm.NewSignerStorage(o.Db, o.Network, logger)
}

// Run executes the migrations.
func (m Migrations) Run(ctx context.Context, logger *zap.Logger, opt Options) (applied int, err error) {
	logger.Info("Running migrations")
	for _, migration := range m {
		// Skip the migration if it's already completed.
		obj, _, err := opt.Db.Get(migrationsPrefix, []byte(migration.Name))
		if err != nil {
			return applied, err
		}
		if bytes.Equal(obj.Value, migrationCompleted) {
			logger.Debug("migration already applied, skipping", fields.Name(migration.Name))
			continue
		}

		// Execute the migration.
		start := time.Now()
		logger = logger.With(zap.String("migration", migration.Name))
		err = migration.Run(ctx, logger, opt, []byte(migration.Name))
		if err != nil {
			return applied, errors.Wrapf(err, "migration %q failed", migration.Name)
		}
		applied++

		logger.Info("migration applied successfully", fields.Name(migration.Name), fields.Duration(start))
	}
	if applied == 0 {
		logger.Info("no migrations to apply")
	}
	return applied, nil
}

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
	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	migrationsPrefix   = []byte("migrations/")
	migrationCompleted = []byte("migrationCompleted")

	defaultMigrations = Migrations{
		migration_0_example,
		migration_1_example,
		migration_2_encrypt_shares,
		migration_3_drop_registry_data,
		migration_4_standalone_slashing_data,
	}
)

// Run executes the default migrations.
func Run(ctx context.Context, logger *zap.Logger, opt Options) (applied int, err error) {
	return defaultMigrations.Run(ctx, logger, opt)
}

// CompletedFunc is a function that marks a migration as completed.
type CompletedFunc func(rw basedb.ReadWriter) error

// MigrationFunc is a function that performs a migration.
type MigrationFunc func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error

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
	SpDb    basedb.Database
	Network beacon.BeaconNetwork
}

func (o Options) nodeStorage(logger *zap.Logger) (operatorstorage.Storage, error) {
	return operatorstorage.NewNodeStorage(logger, o.Db)
}

func (o Options) signerStorage(logger *zap.Logger) ekm.SignerStorage {
	network := o.Network.GetBeaconNetwork()
	return ekm.NewSignerStorage(o.Db, logger, network, []byte(network))
}

func (o Options) legacySlashingProtectionStorage(logger *zap.Logger) ekm.SPStorage {
	return ekm.NewSlashingProtectionStorage(o.Db, logger, []byte(o.Network.GetBeaconNetwork()))
}

func (o Options) slashingProtectionStorage(logger *zap.Logger) ekm.SPStorage {
	return ekm.NewSlashingProtectionStorage(o.SpDb, logger, []byte(o.Network.GetBeaconNetwork()))
}

// Run executes the migrations.
func (m Migrations) Run(ctx context.Context, logger *zap.Logger, opt Options) (applied int, err error) {
	logger.Info("applying migrations", fields.Count(len(m)))
	for _, migration := range m {
		migration := migration

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
		err = migration.Run(
			ctx,
			logger,
			opt,
			[]byte(migration.Name),
			func(rw basedb.ReadWriter) error {
				return rw.Set(migrationsPrefix, []byte(migration.Name), migrationCompleted)
			},
		)
		if err != nil {
			return applied, errors.Wrapf(err, "migration %q failed", migration.Name)
		}
		applied++

		logger.Debug("migration applied successfully", fields.Name(migration.Name), fields.Duration(start))
	}

	logger.Info("applied migrations successfully", fields.Count(applied))

	return applied, nil
}

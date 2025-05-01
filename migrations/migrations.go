package migrations

import (
	"bytes"
	"context"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/logging/fields"
	"github.com/ssvlabs/ssv/networkconfig"
	operatorstorage "github.com/ssvlabs/ssv/operator/storage"
	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	migrationsPrefix   = []byte("migrations/")
	migrationCompleted = []byte("migrationCompleted")

	defaultMigrations = Migrations{
		migration_0_example,
		migration_1_example,
		migration_2_encrypt_shares,
		migration_3_drop_registry_data,
		migration_4_configlock_add_alan_fork_to_network_name,
		migration_5_change_share_format_from_gob_to_ssz,
		migration_6_share_exit_epoch,
	}
)

// Run executes the default migrations.
func Run(ctx context.Context, logger *zap.Logger, opt Options) (applied int, err error) {
	return defaultMigrations.Run(ctx, logger.Named("Migrations"), opt)
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
	Db            basedb.Database
	NodeStorage   operatorstorage.Storage
	DbPath        string
	NetworkConfig networkconfig.NetworkConfig
}

// nolint
func (o Options) nodeStorage(logger *zap.Logger) (operatorstorage.Storage, error) {
	return operatorstorage.NewNodeStorage(o.NetworkConfig, logger, o.Db)
}

// nolint
func (o Options) signerStorage(logger *zap.Logger) ekm.Storage {
	return ekm.NewSignerStorage(o.Db, o.NetworkConfig.Beacon, logger)
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

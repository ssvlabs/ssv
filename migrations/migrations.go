package migrations

import (
	"bytes"
	"context"
	"time"

	"github.com/pkg/errors"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	ibftstorage "github.com/bloxapp/ssv/ibft/storage"
	operatorstorage "github.com/bloxapp/ssv/operator/storage"
	validatorstorage "github.com/bloxapp/ssv/operator/validator"
	forksprotocol "github.com/bloxapp/ssv/protocol/forks"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/beacon"
	"github.com/bloxapp/ssv/protocol/v2/blockchain/eth1"
	qbftstorage "github.com/bloxapp/ssv/protocol/v2/qbft/storage"
	"github.com/bloxapp/ssv/storage/basedb"
)

var (
	migrationsPrefix   = []byte("migrations/")
	migrationCompleted = []byte("migrationCompleted")

	defaultMigrations = Migrations{
		migrationExample1,
		migrationExample2,
		migrationCleanAllRegistryData,
		migrationCleanOperatorNodeRegistryData,
		migrationCleanExporterRegistryData,
		migrationCleanValidatorRegistryData,
		migrationCleanSyncOffset,
		migrationCleanOperatorRemovalCorruptions,
		migrationCleanShares,
		migrationRemoveChangeRoundSync,
		migrationAddGraffiti,
		migrationCleanRegistryData,
		migrationCleanRegistryDataIncludingSignerStorage,
		migrationCleanRegistryDataShifuV2,
		migrationCompactInstances,
	}
)

// Run executes the default migrations.
func Run(ctx context.Context, opt Options) (applied int, err error) {
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

// Options is the options for running migrations.
type Options struct {
	Db      basedb.IDb
	Logger  *zap.Logger
	DbPath  string
	Network beacon.Network
}

func (o Options) getRegistryStores() []eth1.RegistryStore {
	return []eth1.RegistryStore{o.validatorStorage(), o.nodeStorage(), o.signerStorage()}
}

func (o Options) validatorStorage() validatorstorage.ICollection {
	return validatorstorage.NewCollection(validatorstorage.CollectionOptions{
		DB:     o.Db,
		Logger: o.Logger,
	})
}

func (o Options) nodeStorage() operatorstorage.Storage {
	return operatorstorage.NewNodeStorage(o.Db, o.Logger)
}

func (o Options) signerStorage() ekm.Storage {
	return ekm.NewSignerStorage(o.Db, o.Network, o.Logger)
}

func (o Options) ibftStorage(prefix string, fork forksprotocol.ForkVersion) qbftstorage.QBFTStore {
	return ibftstorage.New(o.Db, o.Logger, prefix, fork)
}

// Run executes the migrations.
func (m Migrations) Run(ctx context.Context, opt Options) (applied int, err error) {
	logger := opt.Logger
	logger.Info("Running migrations")
	for _, migration := range m {
		// Skip the migration if it's already completed.
		obj, _, err := opt.Db.Get(migrationsPrefix, []byte(migration.Name))
		if err != nil {
			return applied, err
		}
		if bytes.Equal(obj.Value, migrationCompleted) {
			logger.Debug("migration already applied, skipping", zap.String("name", migration.Name))
			continue
		}

		// Execute the migration.
		start := time.Now()
		opt.Logger = opt.Logger.With(zap.String("migration", migration.Name))
		err = migration.Run(ctx, opt, []byte(migration.Name))
		if err != nil {
			return applied, errors.Wrapf(err, "migration %q failed", migration.Name)
		}
		applied++
		logger.Info("migration applied successfully",
			zap.String("name", migration.Name),
			zap.Duration("took", time.Since(start)))
	}
	if applied == 0 {
		logger.Info("no migrations to apply")
	}
	return applied, nil
}

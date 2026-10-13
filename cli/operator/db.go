package operator

import (
	"context"
	"fmt"
	"time"

	cockroachdb "github.com/cockroachdb/pebble"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/migrations"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/observability/log/fields"
	"github.com/ssvlabs/ssv/ssvsigner/keys"
	"github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
	"github.com/ssvlabs/ssv/storage/pebble"
)

func setupBadgerDB(
	logger *zap.Logger,
	cfg *config,
	beaconConfig *networkconfig.Beacon,
	operatorPrivKey keys.OperatorPrivateKey,
) (_ *badger.DB, err error) {
	db, err := badger.New(logger, cfg.DBOptions)
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}
	// On any later failure here (migrations/GC), close the freshly-opened db; on success the
	// caller (newNode) owns it and tears it down via node.close().
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()

	if err = applyMigrations(logger, cfg, beaconConfig, operatorPrivKey, db, cfg.DBOptions.Path); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return db, nil
}

func setupPebbleDB(
	logger *zap.Logger,
	cfg *config,
	beaconConfig *networkconfig.Beacon,
	operatorPrivKey keys.OperatorPrivateKey,
) (_ *pebble.DB, err error) {
	dbPath := cfg.DBOptions.Path + "-pebble" // opinionated approach to avoid corrupting old db location

	db, err := pebble.New(logger, dbPath, &cockroachdb.Options{})
	if err != nil {
		return nil, fmt.Errorf("failed to open db: %w", err)
	}
	// On any later failure here (migrations/GC), close the freshly-opened db; on success the
	// caller (newNode) owns it and tears it down via node.close().
	defer func() {
		if err != nil {
			_ = db.Close()
		}
	}()

	if err = applyMigrations(logger, cfg, beaconConfig, operatorPrivKey, db, dbPath); err != nil {
		return nil, fmt.Errorf("apply migrations: %w", err)
	}

	return db, nil
}

func applyMigrations(
	logger *zap.Logger,
	cfg *config,
	beaconConfig *networkconfig.Beacon,
	operatorPrivKey keys.OperatorPrivateKey,
	db basedb.Database,
	dbPath string,
) error {
	migrationOpts := migrations.Options{
		Db:              db,
		DbPath:          dbPath,
		BeaconConfig:    beaconConfig,
		OperatorPrivKey: operatorPrivKey,
	}

	applied, err := migrations.Run(cfg.DBOptions.Ctx, logger, migrationOpts)
	if err != nil {
		return fmt.Errorf("failed to run migrations: %w", err)
	}
	if applied == 0 {
		return nil
	}

	// Migrations were applied, so run a full GC cycle to reclaim any freed space.
	logger.Debug("running full GC cycle...")

	ctx, cancel := context.WithTimeout(cfg.DBOptions.Ctx, 6*time.Minute)
	defer cancel()

	start := time.Now()

	if err := db.FullGC(ctx); err != nil {
		return fmt.Errorf("failed to collect garbage: %w", err)
	}

	logger.Debug("post-migrations garbage collection completed", fields.Took(time.Since(start)))

	return nil
}

// openNodeDB opens the node's database, picking the backend by mode: exporter nodes use pebble,
// operator nodes use badger.
func openNodeDB(logger *zap.Logger, cfg *config, res resolved, beaconConfig *networkconfig.Beacon, operatorPrivKey keys.OperatorPrivateKey) (basedb.Database, error) {
	if res.isExporter() {
		logger.Info("using pebble db")
		return setupPebbleDB(logger, cfg, beaconConfig, operatorPrivKey)
	}
	logger.Info("using badger db")
	return setupBadgerDB(logger, cfg, beaconConfig, operatorPrivKey)
}

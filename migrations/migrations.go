package migrations

import (
	"bytes"
	"context"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/pkg/errors"
)

// defaultMigrations are the default migrations.
var defaultMigrations Migrations

func init() {
	// TODO: handle determinism
	// by sorting the keys or using
	// type Migration interface {
	//    Name() string
	//    Run(context.Context, *environment, []byte) error
	// }
	// defaultMigrations = []Migration{ migration_0{}, migration_1{}, ... }
	defaultMigrations = Migrations{
		"migration_0_init": migration_0_init,
		"migration_1_ekm":  migration_1_ekm,
	}
}

var (
	migrationsPrefix   = []byte("migrations-")
	migrationCompleted = []byte("migrationCompleted")
)

type Eth1Syncer interface {
	// SyncEth1Events TODO: implement
	SyncEth1Events() error
}

type environment struct {
	db       basedb.IDb
	exporter Eth1Syncer
	node     Eth1Syncer
}

// Migration is a function that performs a migration.
type Migration func(ctx context.Context, env *environment, key []byte) error

type Migrations map[string]Migration

// Run executes the migrations.
func (m Migrations) Run(ctx context.Context, db basedb.IDb) error {
	env := &environment{
		db: db,
	}

	for key, migration := range m {
		// Skip the migration if it's already completed.
		obj, _, err := env.db.Get(migrationsPrefix, []byte(key))
		if err != nil {
			return err
		}
		if bytes.Equal(obj.Value, migrationCompleted) {
			continue
		}

		// Execute the migration.
		if err := migration(ctx, env, []byte(key)); err != nil {
			return errors.Wrapf(err, "migration %q failed", key)
		}
	}

	return nil
}

// Run executes the default migrations.
// TODO: add args (exporter, node Eth1Syncer)
func Run(ctx context.Context, db basedb.IDb) error {
	return defaultMigrations.Run(ctx, db)
}

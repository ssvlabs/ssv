package migrations

import (
	"context"
	"fmt"
	"os"
	"path"
	"testing"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupOptions(ctx context.Context, t *testing.T) (Options, error) {
	// Create in-memory test DB.
	options := basedb.Options{
		Type:      "badger-memory",
		Logger:    zap.L(),
		Path:      "",
		Reporting: true,
		Ctx:       ctx,
	}
	db, err := kv.New(options)
	if err != nil {
		return Options{}, err
	}
	return Options{
		Db:     db,
		Logger: zap.L(),
		DbPath: t.TempDir(),
	}, nil
}

func Test_RunNotMigratingTwice(t *testing.T) {
	ctx := context.Background()
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	var count int
	migrations := Migrations{
		{
			Name: "not_migrating_twice",
			Run: func(ctx context.Context, opt Options, key []byte) error {
				count++
				return opt.Db.Set(migrationsPrefix, key, migrationCompleted)
			},
		},
	}
	require.NoError(t, migrations.Run(ctx, opt))
	require.NoError(t, migrations.Run(ctx, opt))
	require.Equal(t, count, 1) // Only ran once.
}

func Test_Rollback(t *testing.T) {
	ctx := context.Background()
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	// Test that migration fails and rolls back on error.
	fakeError := errors.New("fake error")
	migrationKey := "test_migration"
	err = Migrations{fakeMigration(migrationKey, fakeError)}.Run(ctx, opt)
	require.Error(t, err, fakeError)
	_, found, err := opt.Db.Get(migrationsPrefix, []byte(migrationKey))
	require.NoError(t, err)
	require.False(t, found)

	// Test that migration doesn't fail without error:
	err = Migrations{fakeMigration(migrationKey, nil)}.Run(ctx, opt)
	require.NoError(t, err)
	obj, found, err := opt.Db.Get(migrationsPrefix, []byte(migrationKey))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte(migrationKey), obj.Key)
	require.Equal(t, migrationCompleted, obj.Value)
}

func Test_NextMigrationNotExecutedOnFailure(t *testing.T) {
	ctx := context.Background()
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	fakeError := errors.New("fake error")
	migrations := Migrations{
		fakeMigration("first", fakeError),
		fakeMigration("second", nil),
	}
	err = migrations.Run(ctx, opt)
	require.Error(t, err)
	require.EqualError(t, err, fmt.Sprintf("migration \"first\" failed: %s", fakeError.Error()))
	_, found, err := opt.Db.Get(migrationsPrefix, []byte("first"))
	require.NoError(t, err)
	require.False(t, found)
	_, found, err = opt.Db.Get(migrationsPrefix, []byte("second"))
	require.NoError(t, err)
	require.False(t, found)
}

func Test_DeprecatedMigrationFakeApplied(t *testing.T) {
	ctx := context.Background()
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	var (
		exporterPrefix = []byte("exporter/")
		exporterKey    = []byte("exporterKey")
		exporterValue  = []byte("exporterValue")
	)
	// create temp key/value under exporter collection
	err = opt.Db.Set(exporterPrefix, exporterKey, exporterValue)
	require.NoError(t, err)

	// create deprecated oa_pks/migration.txt file
	tmpDirPath := path.Join(opt.DbPath, "oa_pks")
	require.NoError(t, os.MkdirAll(tmpDirPath, 0700))
	tmpFilePath := path.Join(tmpDirPath, "migration.txt")
	_, err = os.Create(tmpFilePath)
	require.NoError(t, err)

	migrations := Migrations{
		migrationCleanAllRegistryData,
	}
	err = migrations.Run(ctx, opt)
	require.NoError(t, err)

	// validate that migrationCleanAllRegistryData fake applied
	obj, found, err := opt.Db.Get(exporterPrefix, exporterKey)
	require.NoError(t, err)
	require.NotNil(t, obj)
	require.True(t, found)
	require.Equal(t, obj.Key, exporterKey)
	require.Equal(t, obj.Value, exporterValue)

	_, found, err = opt.Db.Get(migrationsPrefix, []byte(migrationCleanAllRegistryData.Name))
	require.NoError(t, err)
	require.True(t, found)
}

func fakeMigration(name string, returnErr error) Migration {
	return Migration{
		Name: name,
		Run: func(ctx context.Context, opt Options, key []byte) error {
			return opt.Db.Update(func(txn basedb.Txn) error {
				err := txn.Set(migrationsPrefix, key, migrationCompleted)
				if err != nil {
					return err
				}
				return returnErr
			})
		},
	}
}

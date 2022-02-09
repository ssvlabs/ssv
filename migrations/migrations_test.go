package migrations

import (
	"context"
	"fmt"
	"testing"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func setupOptions(ctx context.Context) (Options, error) {
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
	}, nil
}

func Test_RunNotMigratingTwice(t *testing.T) {
	ctx := context.Background()
	opt, err := setupOptions(ctx)
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
	opt, err := setupOptions(ctx)
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
	opt, err := setupOptions(ctx)
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

package migrations

import (
	"context"
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/observability/log"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func setupOptions(ctx context.Context, t *testing.T) (Options, error) {
	// Create in-memory test DB.
	db, err := kv.NewInMemory(log.TestLogger(t), basedb.Options{
		Reporting: true,
		Ctx:       ctx,
	})
	if err != nil {
		return Options{}, err
	}
	return Options{
		Db:     db,
		DbPath: t.TempDir(),
	}, nil
}

func Test_RunNotMigratingTwice(t *testing.T) {
	ctx := t.Context()
	logger := log.TestLogger(t)
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	var count int
	migrations := Migrations{
		{
			Name: "not_migrating_twice",
			Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
				count++
				return completed(opt.Db)
			},
		},
	}

	applied, err := migrations.Run(ctx, logger, opt)
	require.NoError(t, err)
	require.Equal(t, applied, 1)
	require.Equal(t, count, 1) // Only ran once.

	applied, err = migrations.Run(ctx, logger, opt)
	require.NoError(t, err)
	require.Equal(t, applied, 0)
	require.Equal(t, count, 1) // Only ran once.
}

func Test_Rollback(t *testing.T) {
	ctx := t.Context()
	logger := log.TestLogger(t)
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	// Test that migration fails and rolls back on error.
	fakeError := errors.New("fake error")
	migrationKey := "test_migration"
	applied, err := Migrations{fakeMigration(migrationKey, fakeError)}.Run(ctx, logger, opt)
	require.Equal(t, 0, applied)
	require.Error(t, fakeError, err)
	_, found, err := opt.Db.Get(migrationsPrefix, []byte(migrationKey))
	require.NoError(t, err)
	require.False(t, found)

	// Test that migration doesn't fail without error:
	applied, err = Migrations{fakeMigration(migrationKey, nil)}.Run(ctx, logger, opt)
	require.NoError(t, err)
	require.Equal(t, 1, applied)
	obj, found, err := opt.Db.Get(migrationsPrefix, []byte(migrationKey))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte(migrationKey), obj.Key)
	require.Equal(t, migrationCompleted, obj.Value)
}

func Test_NextMigrationNotExecutedOnFailure(t *testing.T) {
	ctx := t.Context()
	logger := log.TestLogger(t)
	opt, err := setupOptions(ctx, t)
	require.NoError(t, err)

	fakeError := errors.New("fake error")
	migrations := Migrations{
		fakeMigration("first", fakeError),
		fakeMigration("second", nil),
	}
	applied, err := migrations.Run(ctx, logger, opt)
	require.Error(t, err)
	require.EqualError(t, err, fmt.Sprintf("migration \"first\" failed: %s", fakeError.Error()))
	require.Equal(t, 0, applied)
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
		Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
			return opt.Db.Update(func(txn basedb.Txn) error {
				err := completed(txn)
				if err != nil {
					return err
				}
				return returnErr
			})
		},
	}
}

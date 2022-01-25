package migrations

import (
	"context"
	"testing"

	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestRun_Not_Migrating_Twice(t *testing.T) {
	ctx := context.Background()

	// Create in-memory test DB.
	options := basedb.Options{
		Type:      "badger-memory",
		Logger:    zap.L(),
		Path:      "",
		Reporting: true,
		Ctx:       ctx,
	}
	db, err := kv.New(options)
	require.NoError(t, err)

	var test1 int
	migrations := Migrations{
		"test1": func(ctx context.Context, env *environment, key []byte) error {
			test1++
			return env.db.Set(migrationsPrefix, key, migrationCompleted)
		},
	}
	require.NoError(t, migrations.Run(ctx, db))
	require.NoError(t, migrations.Run(ctx, db))
	require.Equal(t, test1, 1) // Only ran once.
}

// TODO: Check that the migration fails (does not continue to next migration)
// if the previous returned an error and .Run() returns that error
func TestRun_TODO(t *testing.T) {}

package migration

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	cockroachdb "github.com/cockroachdb/pebble"
	badgerdb "github.com/dgraph-io/badger/v4"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/pebble"
)

func TestResolveDBLayout_NoExistingDatabases(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, basePath, layout.PebblePath)
	require.Empty(t, layout.BadgerImportPath)
}

func TestResolveDBLayout_UsesCanonicalPebble(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	createPebbleDB(t, basePath, map[string][]byte{"a": []byte("1")})

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, basePath, layout.PebblePath)
	require.Empty(t, layout.BadgerImportPath)
}

func TestResolveDBLayout_UsesLegacyPebble(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createPebbleDB(t, legacyPath, map[string][]byte{"a": []byte("1")})

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, legacyPath, layout.PebblePath)
	require.Empty(t, layout.BadgerImportPath)
}

func TestResolveDBLayout_ImportsFromBadgerWhenOnlyBadgerHasData(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	createBadgerDB(t, basePath, map[string][]byte{"a": []byte("1")})

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, basePath+"-pebble", layout.PebblePath)
	require.Equal(t, basePath, layout.BadgerImportPath)
}

func TestResolveDBLayout_FailsOnMultipleNonEmptySources(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createPebbleDB(t, basePath, map[string][]byte{"a": []byte("1")})
	createPebbleDB(t, legacyPath, map[string][]byte{"b": []byte("2")})

	_, err := ResolveDBLayout(basePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "multiple non-empty databases detected")
}

func TestResolveDBLayout_AllowsBadgerAndLegacyPebbleWhenImportDoneMarkerExists(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createBadgerDB(t, basePath, map[string][]byte{"a": []byte("1")})
	createPebbleDB(t, legacyPath, map[string][]byte{"b": []byte("2")})
	require.NoError(t, writeBadgerImportDoneMarker(legacyPath, basePath, 1))

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, legacyPath, layout.PebblePath)
	require.Empty(t, layout.BadgerImportPath)
}

func TestResolveDBLayout_DoneAndInProgressMarkersTriggersCleanupRun(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createBadgerDB(t, basePath, map[string][]byte{"a": []byte("1")})
	createPebbleDB(t, legacyPath, map[string][]byte{"b": []byte("2")})
	require.NoError(t, writeBadgerImportDoneMarker(legacyPath, basePath, 1))
	require.NoError(t, writeBadgerImportInProgressMarker(legacyPath, basePath))

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, legacyPath, layout.PebblePath)
	require.Equal(t, basePath, layout.BadgerImportPath)
}

func TestResolveDBLayout_FastPathSkipsBadgerOpenAfterDoneMarker(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createPebbleDB(t, legacyPath, map[string][]byte{"b": []byte("2")})
	require.NoError(t, writeBadgerImportDoneMarker(legacyPath, basePath, 1))

	require.NoError(t, os.MkdirAll(basePath, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(basePath, "KEYREGISTRY"), []byte("not-a-badger-db"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(basePath, "000001.vlog"), []byte("garbage"), 0o600))

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, legacyPath, layout.PebblePath)
	require.Empty(t, layout.BadgerImportPath)
}

func TestResolveDBLayout_FailsWhenDoneMarkerExistsButLegacyPebbleEmpty(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createPebbleDB(t, legacyPath, map[string][]byte{})
	require.NoError(t, writeBadgerImportDoneMarker(legacyPath, basePath, 1))

	_, err := ResolveDBLayout(basePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "completion marker exists")
}

func TestResolveDBLayout_FailsWhenBothPebbleDirsEmptyAndLegacyDoneMarkerExists(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createPebbleDB(t, basePath, map[string][]byte{})
	createPebbleDB(t, legacyPath, map[string][]byte{})
	require.NoError(t, writeBadgerImportDoneMarker(legacyPath, basePath, 1))

	_, err := ResolveDBLayout(basePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "completion marker exists")
}

func TestResolveDBLayout_FailsWhenDoneMarkerExistsButCanonicalPebbleEmpty(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	createPebbleDB(t, basePath, map[string][]byte{})
	require.NoError(t, writeBadgerImportDoneMarker(basePath, basePath, 1))

	_, err := ResolveDBLayout(basePath)
	require.Error(t, err)
	require.Contains(t, err.Error(), "completion marker exists")
}

func TestResolveDBLayout_AllowsBadgerAndLegacyPebbleWhenImportInProgressMarkerExists(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "db")
	legacyPath := basePath + "-pebble"
	createBadgerDB(t, basePath, map[string][]byte{"a": []byte("1")})
	createPebbleDB(t, legacyPath, map[string][]byte{"b": []byte("2")})
	require.NoError(t, writeBadgerImportInProgressMarker(legacyPath, basePath))

	layout, err := ResolveDBLayout(basePath)
	require.NoError(t, err)
	require.Equal(t, legacyPath, layout.PebblePath)
	require.Equal(t, basePath, layout.BadgerImportPath)
}

func TestMigrateBadgerToPebbleIfNeeded_MigratesWhenPebbleEmpty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	createBadgerDB(t, badgerPath, map[string][]byte{
		"foo": []byte("bar"),
		"abc": []byte("123"),
	})

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.True(t, migrated)
	require.Equal(t, 2, keys)

	obj, found, err := pdb.Get(nil, []byte("foo"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("bar"), obj.Value)

	obj, found, err = pdb.Get(nil, []byte("abc"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("123"), obj.Value)
	require.FileExists(t, doneMarkerPath(pebblePath))
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
}

func TestMigrateBadgerToPebbleIfNeeded_SkipsWhenPebbleNotEmptyAndNoBadgerData(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	require.NoError(t, pdb.DB.Set([]byte("foo"), []byte("from-pebble"), cockroachdb.Sync))

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.False(t, migrated)
	require.Equal(t, 0, keys)

	obj, found, err := pdb.Get(nil, []byte("foo"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("from-pebble"), obj.Value)
}

func TestMigrateBadgerToPebbleIfNeeded_RemovesOrphanedInProgressMarkerWhenBadgerMissing(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	require.NoError(t, pdb.DB.Set([]byte("foo"), []byte("from-pebble"), cockroachdb.Sync))
	require.NoError(t, writeBadgerImportInProgressMarker(pebblePath, badgerPath))

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.False(t, migrated)
	require.Equal(t, 0, keys)
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
}

func TestMigrateBadgerToPebbleIfNeeded_RemovesOrphanedInProgressMarkerWhenBadgerMissingAndPebbleEmpty(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	require.NoError(t, writeBadgerImportInProgressMarker(pebblePath, badgerPath))

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.False(t, migrated)
	require.Equal(t, 0, keys)
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
}

func TestMigrateBadgerToPebbleIfNeeded_SkipsWhenNoBadgerDB(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.False(t, migrated)
	require.Equal(t, 0, keys)
}

func TestMigrateBadgerToPebbleIfNeeded_ResumesWhenInProgressMarkerExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	createBadgerDB(t, badgerPath, map[string][]byte{
		"foo": []byte("from-badger"),
		"bar": []byte("new"),
	})

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	require.NoError(t, pdb.DB.Set([]byte("foo"), []byte("stale-pebble"), cockroachdb.Sync))
	require.NoError(t, writeBadgerImportInProgressMarker(pebblePath, badgerPath))

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.True(t, migrated)
	require.Equal(t, 2, keys)

	obj, found, err := pdb.Get(nil, []byte("foo"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("from-badger"), obj.Value)
	require.FileExists(t, doneMarkerPath(pebblePath))
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
}

func TestMigrateBadgerToPebbleIfNeeded_FailsOnPotentialPartialMigrationWithoutMarkers(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	createBadgerDB(t, badgerPath, map[string][]byte{"foo": []byte("from-badger")})
	createPebbleDB(t, pebblePath, map[string][]byte{"foo": []byte("from-pebble")})

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.Error(t, err)
	require.Contains(t, err.Error(), "without import markers")
	require.False(t, migrated)
	require.Equal(t, 0, keys)
}

func TestMigrateBadgerToPebbleIfNeeded_SkipsAfterCompletionMarker(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	createBadgerDB(t, badgerPath, map[string][]byte{"foo": []byte("from-badger")})
	createPebbleDB(t, pebblePath, map[string][]byte{"foo": []byte("from-pebble")})

	require.NoError(t, writeBadgerImportDoneMarker(pebblePath, badgerPath, 1))

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.False(t, migrated)
	require.Equal(t, 0, keys)
}

func TestMigrateBadgerToPebbleIfNeeded_RemovesStaleInProgressWhenDoneExists(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")
	createBadgerDB(t, badgerPath, map[string][]byte{"foo": []byte("bar")})
	createPebbleDB(t, pebblePath, map[string][]byte{"foo": []byte("bar")})
	require.NoError(t, writeBadgerImportDoneMarker(pebblePath, badgerPath, 1))
	require.NoError(t, writeBadgerImportInProgressMarker(pebblePath, badgerPath))

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.False(t, migrated)
	require.Equal(t, 0, keys)
	require.FileExists(t, doneMarkerPath(pebblePath))
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
}

func TestMigrateBadgerToPebbleIfNeeded_EndToEndInterruptedThenResumed(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	keyCount := defaultImportBatchSize + 1
	data := make(map[string][]byte, keyCount)
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("k-%05d", i)
		data[key] = []byte(fmt.Sprintf("v-%05d", i))
	}
	createBadgerDB(t, badgerPath, data)

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)

	_, _, err = migrateBadgerToPebbleIfNeeded(
		context.Background(),
		zap.NewNop(),
		badgerPath,
		pebblePath,
		pdb,
		func(committedBatches int, copiedKeys int) error {
			if committedBatches == 1 {
				return fmt.Errorf("interrupted after first committed batch (%d copied)", copiedKeys)
			}
			return nil
		},
	)
	require.Error(t, err)
	require.Contains(t, err.Error(), "interrupted after first committed batch")
	require.FileExists(t, inProgressMarkerPath(pebblePath))
	require.NoFileExists(t, doneMarkerPath(pebblePath))
	require.NoError(t, pdb.Close())

	pdb, err = pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.True(t, migrated)
	require.Equal(t, keyCount, keys)
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
	require.FileExists(t, doneMarkerPath(pebblePath))

	obj, found, err := pdb.Get(nil, []byte("k-00000"))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte("v-00000"), obj.Value)
	obj, found, err = pdb.Get(nil, []byte(fmt.Sprintf("k-%05d", keyCount-1)))
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, []byte(fmt.Sprintf("v-%05d", keyCount-1)), obj.Value)
}

func TestMigrateBadgerToPebbleIfNeeded_StopsOnContextCancel(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	badgerPath := filepath.Join(root, "db")
	pebblePath := filepath.Join(root, "db-pebble")

	keyCount := defaultImportBatchSize + 100
	data := make(map[string][]byte, keyCount)
	for i := 0; i < keyCount; i++ {
		key := fmt.Sprintf("k-%05d", i)
		data[key] = []byte(fmt.Sprintf("v-%05d", i))
	}
	createBadgerDB(t, badgerPath, data)

	pdb, err := pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	_, _, err = migrateBadgerToPebbleIfNeeded(
		ctx,
		zap.NewNop(),
		badgerPath,
		pebblePath,
		pdb,
		func(committedBatches int, copiedKeys int) error {
			if committedBatches == 1 {
				cancel()
			}
			return nil
		},
	)
	require.ErrorIs(t, err, context.Canceled)
	require.FileExists(t, inProgressMarkerPath(pebblePath))
	require.NoFileExists(t, doneMarkerPath(pebblePath))
	require.NoError(t, pdb.Close())

	pdb, err = pebble.New(zap.NewNop(), pebblePath, &cockroachdb.Options{})
	require.NoError(t, err)
	t.Cleanup(func() {
		require.NoError(t, pdb.Close())
	})

	migrated, keys, err := MigrateBadgerToPebbleIfNeeded(t.Context(), zap.NewNop(), badgerPath, pebblePath, pdb)
	require.NoError(t, err)
	require.True(t, migrated)
	require.Equal(t, keyCount, keys)
	require.NoFileExists(t, inProgressMarkerPath(pebblePath))
	require.FileExists(t, doneMarkerPath(pebblePath))
}

func TestWrapNoSpaceImportError_WrapsENOSPC(t *testing.T) {
	t.Parallel()

	err := wrapNoSpaceImportError(syscall.ENOSPC, "/badger", "/pebble")
	require.Error(t, err)
	require.ErrorContains(t, err, "insufficient disk space during badger->pebble migration")
	require.ErrorIs(t, err, syscall.ENOSPC)
}

func TestWrapNoSpaceImportError_Passthrough(t *testing.T) {
	t.Parallel()

	baseErr := errors.New("boom")
	err := wrapNoSpaceImportError(baseErr, "/badger", "/pebble")
	require.ErrorIs(t, err, baseErr)
}

func TestHasBadgerFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		files     []string
		expectHas bool
	}{
		{name: "none", files: nil, expectHas: false},
		{name: "keyregistry only", files: []string{"KEYREGISTRY"}, expectHas: false},
		{name: "vlog only", files: []string{"000001.vlog"}, expectHas: false},
		{name: "keyregistry and vlog", files: []string{"KEYREGISTRY", "000001.vlog"}, expectHas: true},
		{name: "keyregistry and vlog zstd", files: []string{"KEYREGISTRY", "000001.vlog.zstd"}, expectHas: true},
		{name: "pebble only", files: []string{"MANIFEST-000001", "CURRENT"}, expectHas: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			dir := t.TempDir()
			for _, f := range tc.files {
				require.NoError(t, os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600))
			}

			has, err := hasBadgerFiles(dir)
			require.NoError(t, err)
			require.Equal(t, tc.expectHas, has)
		})
	}
}

func TestMarkerExistsForSource_EquivalentPaths(t *testing.T) {
	t.Parallel()

	markerPath := filepath.Join(t.TempDir(), "marker.json")
	sourcePath := "./data/../data/badger-db"
	require.NoError(t, writeMarker(markerPath, badgerImportMarker{BadgerDirPath: sourcePath}))

	exists, err := markerExistsForSource(markerPath, filepath.Clean(sourcePath))
	require.NoError(t, err)
	require.True(t, exists)

	absPath, err := filepath.Abs(filepath.Clean(sourcePath))
	require.NoError(t, err)

	exists, err = markerExistsForSource(markerPath, absPath)
	require.NoError(t, err)
	require.True(t, exists)
}

func TestMarkerExistsForSource_DifferentPaths(t *testing.T) {
	t.Parallel()

	markerPath := filepath.Join(t.TempDir(), "marker.json")
	require.NoError(t, writeMarker(markerPath, badgerImportMarker{BadgerDirPath: "path-a"}))

	exists, err := markerExistsForSource(markerPath, "path-b")
	require.NoError(t, err)
	require.False(t, exists)
}

func createPebbleDB(t *testing.T, path string, data map[string][]byte) {
	t.Helper()

	db, err := pebble.New(zap.NewNop(), path, &cockroachdb.Options{})
	require.NoError(t, err)

	for key, value := range data {
		require.NoError(t, db.DB.Set([]byte(key), value, cockroachdb.Sync))
	}

	require.NoError(t, db.Close())
}

func createBadgerDB(t *testing.T, path string, data map[string][]byte) {
	t.Helper()

	opt := badgerdb.DefaultOptions(path)
	opt.Logger = nil

	db, err := badgerdb.Open(opt)
	require.NoError(t, err)

	err = db.Update(func(txn *badgerdb.Txn) error {
		for key, value := range data {
			if err := txn.Set([]byte(key), value); err != nil {
				return err
			}
		}
		return nil
	})
	require.NoError(t, err)
	require.NoError(t, db.Close())
}

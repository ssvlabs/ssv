package migration

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	cockroachdb "github.com/cockroachdb/pebble"
	badgerdb "github.com/dgraph-io/badger/v4"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/pebble"
)

const (
	badgerImportDoneMarkerFileName       = ".ssv-badger-import.done.json"
	badgerImportInProgressMarkerFileName = ".ssv-badger-import.inprogress.json"
	defaultImportBatchSize               = 10_000
)

type importBatchCommitHook func(committedBatches int, copiedKeys int) error

type badgerImportMarker struct {
	SourcePath  string `json:"source_path"`
	Keys        int    `json:"keys,omitempty"`
	StartedAt   string `json:"started_at,omitempty"`
	CompletedAt string `json:"completed_at,omitempty"`
}

// PebbleDBPlan describes which Pebble path to open and whether legacy Badger
// data should be imported into it on startup.
type PebbleDBPlan struct {
	PebblePath       string
	BadgerImportPath string
}

// ResolvePebbleDBPlan selects the Pebble path and optional Badger import path.
func ResolvePebbleDBPlan(basePath string) (PebbleDBPlan, error) {
	legacyPebblePath := basePath + "-pebble"

	canonicalPebbleExists, canonicalPebbleNonEmpty, err := pebble.DirState(basePath)
	if err != nil {
		return PebbleDBPlan{}, fmt.Errorf("check pebble path %q: %w", basePath, err)
	}

	legacyPebbleExists, legacyPebbleNonEmpty, err := pebble.DirState(legacyPebblePath)
	if err != nil {
		return PebbleDBPlan{}, fmt.Errorf("check legacy pebble path %q: %w", legacyPebblePath, err)
	}

	badgerExists, badgerNonEmpty, err := badgerDirState(basePath)
	if err != nil {
		return PebbleDBPlan{}, fmt.Errorf("check badger path %q: %w", basePath, err)
	}

	nonEmptyCandidates := make([]string, 0, 3)
	if canonicalPebbleNonEmpty {
		nonEmptyCandidates = append(nonEmptyCandidates, fmt.Sprintf("pebble:%s", basePath))
	}
	if legacyPebbleNonEmpty {
		nonEmptyCandidates = append(nonEmptyCandidates, fmt.Sprintf("pebble:%s", legacyPebblePath))
	}
	if badgerNonEmpty {
		nonEmptyCandidates = append(nonEmptyCandidates, fmt.Sprintf("badger:%s", basePath))
	}
	if len(nonEmptyCandidates) > 1 {
		if canonicalPebbleNonEmpty && badgerNonEmpty && !legacyPebbleNonEmpty {
			ok, err := canUsePebbleAlongsideBadger(basePath, basePath)
			if err != nil {
				return PebbleDBPlan{}, err
			}
			if ok {
				return PebbleDBPlan{PebblePath: basePath, BadgerImportPath: basePath}, nil
			}
		}
		if legacyPebbleNonEmpty && badgerNonEmpty && !canonicalPebbleNonEmpty {
			ok, err := canUsePebbleAlongsideBadger(legacyPebblePath, basePath)
			if err != nil {
				return PebbleDBPlan{}, err
			}
			if ok {
				return PebbleDBPlan{PebblePath: legacyPebblePath, BadgerImportPath: basePath}, nil
			}
		}

		return PebbleDBPlan{}, fmt.Errorf(
			"multiple non-empty databases detected (%s); keep only one source of truth before starting",
			strings.Join(nonEmptyCandidates, ", "),
		)
	}

	switch {
	case canonicalPebbleNonEmpty:
		return PebbleDBPlan{PebblePath: basePath}, nil
	case legacyPebbleNonEmpty:
		return PebbleDBPlan{PebblePath: legacyPebblePath}, nil
	case badgerNonEmpty:
		return PebbleDBPlan{
			PebblePath:       legacyPebblePath,
			BadgerImportPath: basePath,
		}, nil
	}

	switch {
	case canonicalPebbleExists:
		return PebbleDBPlan{PebblePath: basePath}, nil
	case badgerExists:
		// Keep Badger as import source and use a separate Pebble path.
		return PebbleDBPlan{
			PebblePath:       legacyPebblePath,
			BadgerImportPath: basePath,
		}, nil
	case legacyPebbleExists:
		return PebbleDBPlan{PebblePath: legacyPebblePath}, nil
	default:
		return PebbleDBPlan{PebblePath: basePath}, nil
	}
}

// MigrateBadgerToPebbleIfNeeded imports legacy Badger keys into Pebble.
// It is resumable via marker files stored inside pebblePath.
func MigrateBadgerToPebbleIfNeeded(
	logger *zap.Logger,
	badgerPath string,
	pebblePath string,
	db *pebble.DB,
) (bool, int, error) {
	return migrateBadgerToPebbleIfNeeded(logger, badgerPath, pebblePath, db, nil)
}

func migrateBadgerToPebbleIfNeeded(
	logger *zap.Logger,
	badgerPath string,
	pebblePath string,
	db *pebble.DB,
	hook importBatchCommitHook,
) (bool, int, error) {
	doneMarkerExists, err := badgerImportDoneMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, 0, err
	}
	inProgressMarkerExists, err := badgerImportInProgressMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, 0, err
	}

	pebbleEmpty, err := isPebbleEmpty(db)
	if err != nil {
		return false, 0, err
	}

	hasBadgerData, badgerNonEmpty, err := badgerDirState(badgerPath)
	if err != nil {
		return false, 0, err
	}

	if doneMarkerExists {
		if pebbleEmpty {
			return false, 0, fmt.Errorf("badger import completion marker exists at %q but pebble db at %q is empty; manual recovery required", doneMarkerPath(pebblePath), pebblePath)
		}
		return false, 0, nil
	}

	if !pebbleEmpty {
		if !hasBadgerData || !badgerNonEmpty {
			return false, 0, nil
		}
		if !inProgressMarkerExists {
			return false, 0, fmt.Errorf(
				"pebble db at %q and badger db at %q are both non-empty without import markers; suspected partial migration, remove one source of truth or complete migration manually",
				pebblePath,
				badgerPath,
			)
		}
		logger.Warn("resuming interrupted badger-to-pebble import",
			zap.String("badger_path", badgerPath),
			zap.String("pebble_path", pebblePath),
		)
	}

	if !hasBadgerData {
		return false, 0, nil
	}
	if !badgerNonEmpty {
		logger.Info("legacy badger database is empty, skipping import", zap.String("path", badgerPath))
		return false, 0, nil
	}

	if err := writeBadgerImportInProgressMarker(pebblePath, badgerPath); err != nil {
		return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
	}

	copied, err := copyBadgerToPebble(badgerPath, db, hook)
	if err != nil {
		return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
	}
	if err := writeBadgerImportDoneMarker(pebblePath, badgerPath, copied); err != nil {
		return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
	}
	if err := removeBadgerImportInProgressMarker(pebblePath); err != nil {
		return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
	}

	return true, copied, nil
}

func isPebbleEmpty(db *pebble.DB) (bool, error) {
	iter, err := db.NewIter(nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = iter.Close() }()

	hasEntry := iter.First()
	return !hasEntry, nil
}

func badgerDirState(path string) (exists bool, nonEmpty bool, err error) {
	exists, err = hasBadgerFiles(path)
	if err != nil || !exists {
		return exists, false, err
	}

	opt := badgerdb.DefaultOptions(path)
	opt.ReadOnly = true
	opt.Logger = nil

	bdb, err := badgerdb.Open(opt)
	if err != nil {
		return true, false, err
	}
	defer func() { _ = bdb.Close() }()

	err = bdb.View(func(txn *badgerdb.Txn) error {
		itOpts := badgerdb.DefaultIteratorOptions
		itOpts.PrefetchValues = false
		it := txn.NewIterator(itOpts)
		defer it.Close()

		it.Rewind()
		nonEmpty = it.Valid()

		return nil
	})
	if err != nil {
		return true, false, err
	}

	return true, nonEmpty, nil
}

func hasBadgerFiles(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, entry := range entries {
		if entry.Name() == "KEYREGISTRY" || strings.HasSuffix(entry.Name(), ".vlog") {
			return true, nil
		}
	}

	return false, nil
}

func copyBadgerToPebble(badgerPath string, db *pebble.DB, hook importBatchCommitHook) (int, error) {
	opt := badgerdb.DefaultOptions(badgerPath)
	opt.ReadOnly = true
	opt.Logger = nil

	bdb, err := badgerdb.Open(opt)
	if err != nil {
		return 0, err
	}
	defer func() { _ = bdb.Close() }()

	batchSize := defaultImportBatchSize
	copied := 0
	pending := 0
	committedBatches := 0
	batch := db.NewBatch()
	defer func() { _ = batch.Close() }()

	flushBatch := func() error {
		if pending == 0 {
			return nil
		}
		if err := batch.Commit(cockroachdb.Sync); err != nil {
			return err
		}
		if err := batch.Close(); err != nil {
			return err
		}
		committedBatches++
		if hook != nil {
			if err := hook(committedBatches, copied); err != nil {
				return err
			}
		}
		batch = db.NewBatch()
		pending = 0
		return nil
	}

	err = bdb.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			item := it.Item()

			key := item.KeyCopy(nil)
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			if err := batch.Set(key, val, nil); err != nil {
				return err
			}

			copied++
			pending++

			if pending >= batchSize {
				if err := flushBatch(); err != nil {
					return err
				}
			}
		}

		return nil
	})
	if err != nil {
		return 0, err
	}
	if err := flushBatch(); err != nil {
		return 0, err
	}

	return copied, nil
}

func canUsePebbleAlongsideBadger(pebblePath string, badgerPath string) (bool, error) {
	done, err := badgerImportDoneMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, err
	}
	if done {
		return true, nil
	}
	inProgress, err := badgerImportInProgressMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, err
	}
	return inProgress, nil
}

func writeBadgerImportInProgressMarker(pebblePath string, badgerPath string) error {
	return writeMarker(
		inProgressMarkerPath(pebblePath),
		badgerImportMarker{
			SourcePath: badgerPath,
			StartedAt:  time.Now().UTC().Format(time.RFC3339Nano),
		},
	)
}

func writeBadgerImportDoneMarker(pebblePath string, badgerPath string, keys int) error {
	return writeMarker(
		doneMarkerPath(pebblePath),
		badgerImportMarker{
			SourcePath:  badgerPath,
			Keys:        keys,
			CompletedAt: time.Now().UTC().Format(time.RFC3339Nano),
		},
	)
}

func badgerImportDoneMarkerExists(pebblePath string, badgerPath string) (bool, error) {
	return markerExistsForSource(doneMarkerPath(pebblePath), badgerPath)
}

func badgerImportInProgressMarkerExists(pebblePath string, badgerPath string) (bool, error) {
	return markerExistsForSource(inProgressMarkerPath(pebblePath), badgerPath)
}

func removeBadgerImportInProgressMarker(pebblePath string) error {
	err := os.Remove(inProgressMarkerPath(pebblePath))
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("remove in-progress import marker: %w", err)
	}
	if err == nil {
		if err := syncDirectory(filepath.Dir(inProgressMarkerPath(pebblePath))); err != nil {
			return fmt.Errorf("fsync marker directory after marker removal: %w", err)
		}
	}
	return nil
}

func markerExistsForSource(path string, badgerPath string) (bool, error) {
	marker, exists, err := readMarker(path)
	if err != nil {
		return false, err
	}
	if !exists {
		return false, nil
	}
	return marker.SourcePath == "" || marker.SourcePath == badgerPath, nil
}

func writeMarker(path string, marker badgerImportMarker) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create marker directory: %w", err)
	}

	data, err := json.Marshal(marker)
	if err != nil {
		return fmt.Errorf("marshal marker: %w", err)
	}

	tmpPath := path + ".tmp"
	// #nosec G304 -- marker path is derived from configured DB path plus fixed marker filenames.
	tempFile, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return fmt.Errorf("open marker temp file: %w", err)
	}
	if _, err := tempFile.Write(data); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("write marker temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		return fmt.Errorf("fsync marker temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		return fmt.Errorf("close marker temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("publish marker file: %w", err)
	}
	if err := syncDirectory(filepath.Dir(path)); err != nil {
		return fmt.Errorf("fsync marker directory: %w", err)
	}

	return nil
}

func syncDirectory(path string) error {
	// #nosec G304 -- directory path is derived from configured DB path plus fixed marker filenames.
	dirFile, err := os.Open(path)
	if err != nil {
		return err
	}
	defer func() { _ = dirFile.Close() }()

	if err := dirFile.Sync(); err != nil {
		return fmt.Errorf("sync directory: %w", err)
	}
	return nil
}

func wrapNoSpaceImportError(err error, badgerPath string, pebblePath string) error {
	if err == nil {
		return nil
	}
	if !isNoSpaceError(err) {
		return err
	}
	return fmt.Errorf(
		"insufficient disk space during badger->pebble migration (from=%q to=%q): %w",
		badgerPath,
		pebblePath,
		err,
	)
}

func isNoSpaceError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.ENOSPC) || errors.Is(err, syscall.EDQUOT) {
		return true
	}
	errMsg := strings.ToLower(err.Error())
	return strings.Contains(errMsg, "no space left on device") || strings.Contains(errMsg, "disk quota exceeded")
}

func readMarker(path string) (badgerImportMarker, bool, error) {
	// #nosec G304 -- marker path is derived from configured DB path plus fixed marker filenames.
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return badgerImportMarker{}, false, nil
		}
		return badgerImportMarker{}, false, fmt.Errorf("read marker %q: %w", path, err)
	}

	marker := badgerImportMarker{}
	if err := json.Unmarshal(data, &marker); err != nil {
		return badgerImportMarker{}, false, fmt.Errorf("parse marker %q: %w", path, err)
	}

	return marker, true, nil
}

func doneMarkerPath(pebblePath string) string {
	return filepath.Join(pebblePath, badgerImportDoneMarkerFileName)
}

func inProgressMarkerPath(pebblePath string) string {
	return filepath.Join(pebblePath, badgerImportInProgressMarkerFileName)
}

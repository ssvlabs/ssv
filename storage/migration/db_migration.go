package migration

import (
	"context"
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
	importProgressLogEveryBatches        = 10
)

type importBatchCommitHook func(committedBatches int, copiedKeys int) error

type badgerImportMarker struct {
	// BadgerDirPath identifies the legacy Badger directory this marker belongs to.
	BadgerDirPath string `json:"source_path"`
	// KeyCount records how many keys were copied into Pebble when the import completed.
	KeyCount int `json:"key_count,omitempty"`
	// StartedAt is set only on the in-progress marker.
	StartedAt string `json:"started_at,omitempty"`
	// CompletedAt is set only on the done marker.
	CompletedAt string `json:"completed_at,omitempty"`
}

// PebbleDBPlan describes which Pebble path to open and whether legacy Badger
// data should be imported into it on startup.
type PebbleDBPlan struct {
	PebblePath       string
	BadgerImportPath string
	BadgerStateKnown bool
	BadgerExists     bool
	BadgerNonEmpty   bool
}

// ResolvePebbleDBPlan selects the Pebble path and optional Badger import path.
func ResolvePebbleDBPlan(logger *zap.Logger, basePath string) (PebbleDBPlan, error) {
	legacyPebblePath := basePath + "-pebble"

	canonicalPebbleExists, canonicalPebbleNonEmpty, err := pebble.DirState(basePath)
	if err != nil {
		return PebbleDBPlan{}, fmt.Errorf("check pebble path %q: %w", basePath, err)
	}

	legacyPebbleExists, legacyPebbleNonEmpty, err := pebble.DirState(legacyPebblePath)
	if err != nil {
		return PebbleDBPlan{}, fmt.Errorf("check legacy pebble path %q: %w", legacyPebblePath, err)
	}

	// Fast path: post-migration steady state (non-empty pebble + import marker).
	// This avoids opening legacy Badger on startup when marker state already determines
	// the selected source of truth.
	switch {
	case canonicalPebbleNonEmpty && !legacyPebbleNonEmpty:
		done, inProgress, err := badgerImportMarkerState(basePath, basePath)
		if err != nil {
			return PebbleDBPlan{}, err
		}
		if done {
			if inProgress {
				// Keep import path set only to let migration code remove stale in-progress marker.
				return PebbleDBPlan{PebblePath: basePath, BadgerImportPath: basePath}, nil
			}
			return PebbleDBPlan{PebblePath: basePath}, nil
		}
		if inProgress {
			return PebbleDBPlan{PebblePath: basePath, BadgerImportPath: basePath}, nil
		}
		// Fast path: if no Badger files are present, return without opening Badger.
		// If Badger files are present, fall through to badgerDirState below, which
		// re-checks the directory and performs the authoritative open-based probe.
		hasBadgerData, err := hasBadgerFiles(basePath)
		if err != nil {
			return PebbleDBPlan{}, fmt.Errorf("check badger path %q: %w", basePath, err)
		}
		if !hasBadgerData {
			return PebbleDBPlan{
				PebblePath:       basePath,
				BadgerStateKnown: true,
			}, nil
		}
	case legacyPebbleNonEmpty && !canonicalPebbleNonEmpty:
		done, inProgress, err := badgerImportMarkerState(legacyPebblePath, basePath)
		if err != nil {
			return PebbleDBPlan{}, err
		}
		if done {
			if inProgress {
				// Keep import path set only to let migration code remove stale in-progress marker.
				return PebbleDBPlan{PebblePath: legacyPebblePath, BadgerImportPath: basePath}, nil
			}
			return PebbleDBPlan{PebblePath: legacyPebblePath}, nil
		}
		if inProgress {
			return PebbleDBPlan{PebblePath: legacyPebblePath, BadgerImportPath: basePath}, nil
		}
		// Fast path: if no Badger files are present, return without opening Badger.
		// If Badger files are present, fall through to badgerDirState below, which
		// re-checks the directory and performs the authoritative open-based probe.
		hasBadgerData, err := hasBadgerFiles(basePath)
		if err != nil {
			return PebbleDBPlan{}, fmt.Errorf("check badger path %q: %w", basePath, err)
		}
		if !hasBadgerData {
			return PebbleDBPlan{
				PebblePath:       legacyPebblePath,
				BadgerStateKnown: true,
			}, nil
		}
	}

	badgerExists, badgerNonEmpty, err := badgerDirState(logger, basePath)
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
			done, inProgress, err := badgerImportMarkerState(basePath, basePath)
			if err != nil {
				return PebbleDBPlan{}, err
			}
			if done || inProgress {
				return PebbleDBPlan{
					PebblePath:       basePath,
					BadgerImportPath: basePath,
					BadgerStateKnown: true,
					BadgerExists:     true,
					BadgerNonEmpty:   true,
				}, nil
			}
		}
		if legacyPebbleNonEmpty && badgerNonEmpty && !canonicalPebbleNonEmpty {
			done, inProgress, err := badgerImportMarkerState(legacyPebblePath, basePath)
			if err != nil {
				return PebbleDBPlan{}, err
			}
			if done || inProgress {
				return PebbleDBPlan{
					PebblePath:       legacyPebblePath,
					BadgerImportPath: basePath,
					BadgerStateKnown: true,
					BadgerExists:     true,
					BadgerNonEmpty:   true,
				}, nil
			}
		}

		return PebbleDBPlan{}, fmt.Errorf(
			"multiple non-empty databases detected (%s); keep only one source of truth before starting",
			strings.Join(nonEmptyCandidates, ", "),
		)
	}

	switch {
	case canonicalPebbleNonEmpty:
		return PebbleDBPlan{PebblePath: basePath, BadgerStateKnown: true, BadgerExists: badgerExists, BadgerNonEmpty: badgerNonEmpty}, nil
	case legacyPebbleNonEmpty:
		return PebbleDBPlan{PebblePath: legacyPebblePath, BadgerStateKnown: true, BadgerExists: badgerExists, BadgerNonEmpty: badgerNonEmpty}, nil
	case badgerNonEmpty:
		return PebbleDBPlan{
			PebblePath:       legacyPebblePath,
			BadgerImportPath: basePath,
			BadgerStateKnown: true,
			BadgerExists:     true,
			BadgerNonEmpty:   true,
		}, nil
	}

	switch {
	case canonicalPebbleExists:
		if err := ensureDoneMarkerNotOnEmptyPebble(basePath, basePath, canonicalPebbleNonEmpty); err != nil {
			return PebbleDBPlan{}, err
		}
		return PebbleDBPlan{PebblePath: basePath, BadgerStateKnown: true, BadgerExists: badgerExists, BadgerNonEmpty: badgerNonEmpty}, nil
	case badgerExists:
		// Keep using a separate Pebble path when basePath has Badger files.
		if err := ensureDoneMarkerNotOnEmptyPebble(legacyPebblePath, basePath, legacyPebbleNonEmpty); err != nil {
			return PebbleDBPlan{}, err
		}
		return PebbleDBPlan{PebblePath: legacyPebblePath, BadgerStateKnown: true, BadgerExists: badgerExists, BadgerNonEmpty: badgerNonEmpty}, nil
	case legacyPebbleExists:
		if err := ensureDoneMarkerNotOnEmptyPebble(legacyPebblePath, basePath, legacyPebbleNonEmpty); err != nil {
			return PebbleDBPlan{}, err
		}
		return PebbleDBPlan{PebblePath: legacyPebblePath, BadgerStateKnown: true, BadgerExists: badgerExists, BadgerNonEmpty: badgerNonEmpty}, nil
	default:
		return PebbleDBPlan{PebblePath: basePath, BadgerStateKnown: true, BadgerExists: badgerExists, BadgerNonEmpty: badgerNonEmpty}, nil
	}
}

func ensureDoneMarkerNotOnEmptyPebble(pebblePath string, badgerPath string, pebbleNonEmpty bool) error {
	if pebbleNonEmpty {
		return nil
	}
	done, err := badgerImportDoneMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return err
	}
	if done {
		return fmt.Errorf("badger import completion marker exists at %q but pebble db at %q is empty; manual recovery required", doneMarkerPath(pebblePath), pebblePath)
	}
	return nil
}

// MigrateBadgerToPebbleIfNeeded imports legacy Badger keys into Pebble.
// It is resumable via marker files stored inside pebblePath.
// badgerStateKnown/badgerExists/badgerNonEmpty can be provided by ResolvePebbleDBPlan to skip
// an extra badgerDirState probe.
func MigrateBadgerToPebbleIfNeeded(
	ctx context.Context,
	logger *zap.Logger,
	badgerPath string,
	pebblePath string,
	db *pebble.DB,
	badgerStateKnown bool,
	badgerExists bool,
	badgerNonEmpty bool,
) (bool, int, error) {
	if err := validateBadgerStateHint(badgerStateKnown, badgerExists, badgerNonEmpty); err != nil {
		return false, 0, err
	}
	return migrateBadgerToPebbleIfNeeded(ctx, logger, badgerPath, pebblePath, db, badgerStateKnown, badgerExists, badgerNonEmpty, nil)
}

func validateBadgerStateHint(badgerStateKnown bool, badgerExists bool, badgerNonEmpty bool) error {
	if !badgerStateKnown {
		if badgerExists || badgerNonEmpty {
			return fmt.Errorf("invalid badger state hint: badgerStateKnown=false requires badgerExists=false and badgerNonEmpty=false")
		}
		return nil
	}
	if badgerNonEmpty && !badgerExists {
		return fmt.Errorf("invalid badger state hint: badgerNonEmpty=true requires badgerExists=true")
	}
	return nil
}

func migrateBadgerToPebbleIfNeeded(
	ctx context.Context,
	logger *zap.Logger,
	badgerPath string,
	pebblePath string,
	db *pebble.DB,
	badgerStateKnown bool,
	knownBadgerExists bool,
	knownBadgerNonEmpty bool,
	onBatchCommitHook importBatchCommitHook,
) (bool, int, error) {
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}

	doneMarkerExists, err := badgerImportDoneMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, 0, err
	}
	inProgressMarkerExists, err := badgerImportInProgressMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, 0, err
	}

	if doneMarkerExists {
		if inProgressMarkerExists {
			logger.Warn("both done and in-progress badger import markers exist; removing stale in-progress marker",
				zap.String("badger_path", badgerPath),
				zap.String("pebble_path", pebblePath),
			)
			if err := removeBadgerImportInProgressMarker(pebblePath); err != nil {
				logger.Warn("failed to remove stale badger in-progress marker",
					zap.String("badger_path", badgerPath),
					zap.String("pebble_path", pebblePath),
					zap.Error(err),
				)
			}
		}
		pebbleEmpty, err := isPebbleEmpty(db)
		if err != nil {
			return false, 0, err
		}
		if pebbleEmpty {
			return false, 0, fmt.Errorf("badger import completion marker exists at %q but pebble db at %q is empty; manual recovery required", doneMarkerPath(pebblePath), pebblePath)
		}
		return false, 0, nil
	}

	pebbleEmpty, err := isPebbleEmpty(db)
	if err != nil {
		return false, 0, err
	}

	hasBadgerData := false
	badgerIsNonEmpty := false
	if badgerStateKnown {
		hasBadgerData = knownBadgerExists
		badgerIsNonEmpty = knownBadgerNonEmpty
	} else {
		hasBadgerData, badgerIsNonEmpty, err = badgerDirState(logger, badgerPath)
		if err != nil {
			return false, 0, err
		}
	}

	if inProgressMarkerExists && (!hasBadgerData || !badgerIsNonEmpty) {
		logger.Warn("in-progress badger import marker exists but badger source is missing or empty; removing stale marker",
			zap.String("badger_path", badgerPath),
			zap.String("pebble_path", pebblePath),
			zap.Bool("badger_exists", hasBadgerData),
			zap.Bool("badger_non_empty", badgerIsNonEmpty),
		)
		if err := removeBadgerImportInProgressMarker(pebblePath); err != nil {
			logger.Warn("failed to remove stale badger in-progress marker",
				zap.String("badger_path", badgerPath),
				zap.String("pebble_path", pebblePath),
				zap.Error(err),
			)
		} else {
			inProgressMarkerExists = false
		}
	}

	if !pebbleEmpty {
		if !hasBadgerData || !badgerIsNonEmpty {
			if !badgerIsNonEmpty && hasBadgerData {
				logger.Info("legacy badger database is empty, skipping import", zap.String("path", badgerPath))
			}
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
	if !badgerIsNonEmpty {
		logger.Info("legacy badger database is empty, skipping import", zap.String("path", badgerPath))
		return false, 0, nil
	}

	if !inProgressMarkerExists {
		if err := writeBadgerImportInProgressMarker(pebblePath, badgerPath); err != nil {
			return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
		}
	}

	keysCopiedTotal, err := copyBadgerToPebble(ctx, logger, badgerPath, db, onBatchCommitHook)
	if err != nil {
		return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
	}
	if err := ctx.Err(); err != nil {
		return false, 0, err
	}
	if err := writeBadgerImportDoneMarker(pebblePath, badgerPath, keysCopiedTotal); err != nil {
		return false, 0, wrapNoSpaceImportError(err, badgerPath, pebblePath)
	}
	if err := removeBadgerImportInProgressMarker(pebblePath); err != nil {
		logger.Warn("failed to remove in-progress badger import marker after successful migration; marker will be removed on next startup",
			zap.String("badger_path", badgerPath),
			zap.String("pebble_path", pebblePath),
			zap.Error(err),
		)
	}

	return true, keysCopiedTotal, nil
}

func isPebbleEmpty(db *pebble.DB) (bool, error) {
	iter, err := db.NewIter(nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = iter.Close() }()

	hasEntry := iter.First()
	if err := iter.Error(); err != nil {
		return false, err
	}
	return !hasEntry, nil
}

func badgerDirState(logger *zap.Logger, path string) (exists bool, nonEmpty bool, err error) {
	exists, err = hasBadgerFiles(path)
	if err != nil || !exists {
		return exists, false, err
	}

	bdb, recovered, err := openBadgerReadOnly(path)
	if err != nil {
		return true, false, err
	}
	if recovered {
		logger.Warn("badger value log required truncation while inspecting badger db state",
			zap.String("badger_path", path),
		)
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

	// Heuristic for "Badger files are present" used only to decide whether to attempt
	// opening Badger. For Badger v4, require KEYREGISTRY and at least one value-log
	// file (".vlog" or ".vlog.zstd"). openBadgerReadOnly remains the source of
	// truth for validating the directory.
	hasKeyRegistry := false
	hasValueLog := false
	for _, entry := range entries {
		switch {
		case entry.Name() == "KEYREGISTRY":
			hasKeyRegistry = true
		case strings.HasSuffix(entry.Name(), ".vlog"), strings.HasSuffix(entry.Name(), ".vlog.zstd"):
			hasValueLog = true
		}
		if hasKeyRegistry && hasValueLog {
			return true, nil
		}
	}

	return false, nil
}

func copyBadgerToPebble(ctx context.Context, logger *zap.Logger, badgerPath string, db *pebble.DB, onBatchCommitHook importBatchCommitHook) (int, error) {
	// NOTE: resume always re-copies keys from the beginning of Badger when an in-progress
	// marker exists. Duplicate puts are safe in Pebble (latest write wins), but resume
	// currently has similar write amplification and temporary disk pressure as a full import.
	bdb, recovered, err := openBadgerReadOnly(badgerPath)
	if err != nil {
		return 0, err
	}
	defer func() { _ = bdb.Close() }()
	if recovered {
		logger.Warn(
			"badger value log required truncation during pebble import; proceeding with recovered badger state",
			zap.String("badger_path", badgerPath),
		)
	}

	batchSize := defaultImportBatchSize
	keysCopiedTotal := 0
	pending := 0
	committedBatches := 0
	batch := db.NewBatch()
	defer func() {
		if batch != nil {
			_ = batch.Close()
		}
	}()

	flushBatch := func() error {
		if pending == 0 {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := batch.Commit(cockroachdb.Sync); err != nil {
			return err
		}
		if err := batch.Close(); err != nil {
			batch = nil
			return err
		}
		batch = nil
		committedBatches++
		if committedBatches%importProgressLogEveryBatches == 0 {
			logger.Info("badger import in progress",
				zap.String("badger_path", badgerPath),
				zap.Int("keys_copied", keysCopiedTotal),
				zap.Int("committed_batches", committedBatches),
			)
		}
		batch = db.NewBatch()
		pending = 0
		if onBatchCommitHook != nil {
			if err := onBatchCommitHook(committedBatches, keysCopiedTotal); err != nil {
				return err
			}
		}
		return nil
	}

	err = bdb.View(func(txn *badgerdb.Txn) error {
		it := txn.NewIterator(badgerdb.DefaultIteratorOptions)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			if err := ctx.Err(); err != nil {
				return err
			}

			item := it.Item()

			key := item.KeyCopy(nil)
			val, err := item.ValueCopy(nil)
			if err != nil {
				return err
			}
			if err := batch.Set(key, val, nil); err != nil {
				return err
			}

			keysCopiedTotal++
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

	return keysCopiedTotal, nil
}

func openBadgerReadOnly(path string) (*badgerdb.DB, bool, error) {
	opt := badgerdb.DefaultOptions(path)
	opt.ReadOnly = true
	opt.Logger = nil

	bdb, err := badgerdb.Open(opt)
	if err == nil {
		return bdb, false, nil
	}
	if !isBadgerTruncateRequiredError(err) {
		return nil, false, err
	}

	recoverOpt := badgerdb.DefaultOptions(path)
	recoverOpt.ReadOnly = false
	recoverOpt.Logger = nil

	recoveredDB, recoverErr := badgerdb.Open(recoverOpt)
	if recoverErr != nil {
		return nil, false, fmt.Errorf("open badger read-only failed: %w; recovery open with truncate failed: %w", err, recoverErr)
	}
	if closeErr := recoveredDB.Close(); closeErr != nil {
		return nil, false, fmt.Errorf("close badger after recovery open: %w", closeErr)
	}
	bdb, reopenErr := badgerdb.Open(opt)
	if reopenErr != nil {
		return nil, false, fmt.Errorf("reopen badger read-only after recovery: %w", reopenErr)
	}

	return bdb, true, nil
}

func isBadgerTruncateRequiredError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, badgerdb.ErrTruncateNeeded) {
		return true
	}
	return strings.Contains(strings.ToLower(err.Error()), "log truncate required to run db")
}

func badgerImportMarkerState(pebblePath string, badgerPath string) (done bool, inProgress bool, err error) {
	done, err = badgerImportDoneMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, false, err
	}
	inProgress, err = badgerImportInProgressMarkerExists(pebblePath, badgerPath)
	if err != nil {
		return false, false, err
	}
	return done, inProgress, nil
}

func writeBadgerImportInProgressMarker(pebblePath string, badgerPath string) error {
	return writeMarker(
		inProgressMarkerPath(pebblePath),
		badgerImportMarker{
			BadgerDirPath: badgerPath,
			StartedAt:     time.Now().UTC().Format(time.RFC3339Nano),
		},
	)
}

func writeBadgerImportDoneMarker(pebblePath string, badgerPath string, keys int) error {
	return writeMarker(
		doneMarkerPath(pebblePath),
		badgerImportMarker{
			BadgerDirPath: badgerPath,
			KeyCount:      keys,
			CompletedAt:   time.Now().UTC().Format(time.RFC3339Nano),
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
	// Empty BadgerDirPath is accepted for backward compatibility with early marker
	// files that were written before the source path was persisted.
	return marker.BadgerDirPath == "" || samePath(marker.BadgerDirPath, badgerPath), nil
}

func samePath(left string, right string) bool {
	if left == right {
		return true
	}

	if filepath.Clean(left) == filepath.Clean(right) {
		return true
	}

	absLeft, errLeft := filepath.Abs(left)
	absRight, errRight := filepath.Abs(right)
	if errLeft == nil && errRight == nil && filepath.Clean(absLeft) == filepath.Clean(absRight) {
		return true
	}

	return false
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
		_ = os.Remove(tmpPath)
		return fmt.Errorf("write marker temp file: %w", err)
	}
	if err := tempFile.Sync(); err != nil {
		_ = tempFile.Close()
		_ = os.Remove(tmpPath)
		return fmt.Errorf("fsync marker temp file: %w", err)
	}
	if err := tempFile.Close(); err != nil {
		_ = os.Remove(tmpPath)
		return fmt.Errorf("close marker temp file: %w", err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
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

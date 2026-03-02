package badger

import (
	"os"
	"strings"

	badgerdb "github.com/dgraph-io/badger/v4"
	"go.uber.org/zap"
)

// DirState returns whether a badger directory exists and whether it contains at least one key.
func DirState(path string) (exists bool, nonEmpty bool, err error) {
	exists, err = hasBadgerFiles(path)
	if err != nil || !exists {
		return exists, false, err
	}

	opt := badgerdb.DefaultOptions(path)
	opt.ReadOnly = true
	opt.Logger = NewLogger(zap.NewNop())

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

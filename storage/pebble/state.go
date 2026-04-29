package pebble

import (
	"os"

	"github.com/cockroachdb/pebble"
)

// DirState returns whether a pebble directory exists and whether it contains at least one key.
func DirState(path string) (exists bool, nonEmpty bool, err error) {
	exists, err = hasPebbleFiles(path)
	if err != nil || !exists {
		return exists, false, err
	}

	pdb, err := pebble.Open(path, &pebble.Options{ReadOnly: true})
	if err != nil {
		return true, false, err
	}
	defer func() { _ = pdb.Close() }()

	iter, err := pdb.NewIter(nil)
	if err != nil {
		return true, false, err
	}
	defer func() { _ = iter.Close() }()

	hasEntry := iter.First()
	if err := iter.Error(); err != nil {
		return true, false, err
	}
	return true, hasEntry, nil
}

func hasPebbleFiles(path string) (bool, error) {
	entries, err := os.ReadDir(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}

	for _, entry := range entries {
		if entry.Name() == "CURRENT" {
			return true, nil
		}
	}

	return false, nil
}

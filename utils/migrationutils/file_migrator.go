package migrationutils

import (
	"github.com/pkg/errors"
	"os"
	"path/filepath"
)

// fileMigrator checks if migration file exist.
// returns whether to clean data and if failed to create the file
func fileMigrator(migrationDir string) (bool, error) {
	migrationFileName := "migration.txt"
	fullPath := filepath.Join(migrationDir, migrationFileName)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		if err := os.MkdirAll(migrationDir, 0700); err != nil {
			return true, errors.Wrap(err, "Failed to create migration folder")
		}
		if _, err := os.Create(fullPath); err != nil {
			return true, errors.Wrap(err, "Failed to create migration file")
		}
		return true, nil
	}
	return false, nil
}

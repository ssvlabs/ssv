package migrationutils

import (
	"fmt"
	"go.uber.org/zap"
	"os"
	"path/filepath"
)

// E2kmMigration checks if e2km migration file exist
// is so, skip
// if not - set CleanRegistryData flag to true in order to resync eth1 data from scratch and save secret shares with the new e2km format
// once done - create empty file.txt representing migration already been made
func E2kmMigration(logger *zap.Logger, path string) (bool, error) {
	e2kmMigrationFilePath := fmt.Sprintf("%s/ekm", path)
	e2kmMigrationFileName := "migration.txt"
	fullPath := filepath.Join(e2kmMigrationFilePath, e2kmMigrationFileName)
	if _, err := os.Stat(fullPath); os.IsNotExist(err) {
		logger.Info("Applying e2km migration...")
		if err := os.MkdirAll(e2kmMigrationFilePath, 0700); err != nil {
			return false, err
		}
		if _, err := os.Create(fullPath); err != nil {
			return false, err
		}
		return true, nil
	}
	return false, nil
}

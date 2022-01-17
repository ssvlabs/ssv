package migrationutils

import (
	"fmt"
	"github.com/pkg/errors"
)

// Migrator is the interface for migration
// TODO: rethink the whole migration package
type Migrator func(path string) (bool, error)

// migrators holds references to all the needed Migrator
var migrators []Migrator

func init() {
	migrators = append(migrators,
		e2kmMigration,
		ownerAddrAndOperatorsPKsMigration,
	)
}

// Migrate starts migration
func Migrate(path string) (res bool, err error) {
	for _, m := range migrators {
		if res, err = m(path); err != nil {
			return true, errors.Wrap(err, "migration failed")
		}
	}
	return res, nil
}

// ownerAddrAndOperatorsPKsMigration using fileMigrator to check if migration was done in the past
func ownerAddrAndOperatorsPKsMigration(path string) (bool, error) {
	ok, err := fileMigrator(fmt.Sprintf("%s/oa_pks", path))
	if err != nil {
		return false, errors.Wrap(err, "Failed to migrate owner-address and operators-public-keys")
	}
	return ok, nil
}

// e2kmMigration using fileMigrator to check if e2km migration was done in the past
// is so, skip
// if not - set CleanRegistryData flag to true in order to resync eth1 data from scratch and save secret shares with the new e2km format
// once done - create empty file.txt representing migration already been made
func e2kmMigration(path string) (bool, error) {
	ok, err := fileMigrator(fmt.Sprintf("%s/ekm", path))
	if err != nil {
		return ok, errors.Wrap(err, "Failed to migrate owner-address and operators-public-keys")
	}
	return ok, nil
}

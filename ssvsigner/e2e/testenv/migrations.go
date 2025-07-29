package testenv

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// MigrationFile represents a single migration file
type MigrationFile struct {
	Version  string
	Filename string
	Content  string
}

// migrationOrder defines the correct order for Web3Signer migrations
var migrationOrder = []string{
	"V00001__initial.sql",
	"V00002__removeUniqueConstraints.sql",
	"V00003__addLowWatermark.sql",
	"V00004__addGenesisValidatorsRoot.sql",
	"V00005__xnor_source_target_low_watermark.sql",
	"V00006__signed_data_indexes.sql",
	"V00007__add_db_version.sql",
	"V00008__signed_data_unique_constraints.sql",
	"V00009__upsert_validators.sql",
	"V00010__validator_enabled_status.sql",
	"V00011__bigint_indexes.sql",
	"V00012__add_highwatermark_metadata.sql",
}

// loadMigrations reads migration files from the filesystem
func (env *TestEnvironment) loadMigrations() ([]MigrationFile, error) {
	migrations := make([]MigrationFile, 0, len(migrationOrder))

	for _, filename := range migrationOrder {
		migrationPath := filepath.Join(env.migrationsPath, filename)

		content, err := os.ReadFile(filepath.Clean(migrationPath))
		if err != nil {
			return nil, fmt.Errorf("failed to read migration %s: %w", filename, err)
		}

		version := strings.Split(filename, "__")[0]

		migration := MigrationFile{
			Version:  version,
			Filename: filename,
			Content:  string(content),
		}

		migrations = append(migrations, migration)
	}

	return migrations, nil
}

// createMigrationTable creates the schema_version table for tracking migrations
func (env *TestEnvironment) createMigrationTable(db *sql.DB) error {
	createTableSQL := `
		CREATE TABLE IF NOT EXISTS schema_version (
			version_rank INTEGER NOT NULL,
			installed_rank INTEGER NOT NULL,
			version VARCHAR(50) NOT NULL PRIMARY KEY,
			description VARCHAR(200) NOT NULL,
			type VARCHAR(20) NOT NULL,
			script VARCHAR(1000) NOT NULL,
			checksum INTEGER,
			installed_by VARCHAR(100) NOT NULL,
			installed_on TIMESTAMP NOT NULL DEFAULT CURRENT_TIMESTAMP,
			execution_time INTEGER NOT NULL,
			success BOOLEAN NOT NULL
		);
	`

	_, err := db.Exec(createTableSQL)
	if err != nil {
		return fmt.Errorf("failed to create schema_version table: %w", err)
	}

	return nil
}

// isMigrationApplied checks if a migration has already been applied
func (env *TestEnvironment) isMigrationApplied(db *sql.DB, version string) (bool, error) {
	query := "SELECT COUNT(*) FROM schema_version WHERE version = $1 AND success = true"

	var count int
	err := db.QueryRow(query, version).Scan(&count)
	if err != nil {
		return false, fmt.Errorf("failed to check migration status: %w", err)
	}

	return count > 0, nil
}

// applyMigration executes a single migration
func (env *TestEnvironment) applyMigration(db *sql.DB, migration MigrationFile) error {
	tx, err := db.Begin()
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	_, err = tx.Exec(migration.Content)
	if err != nil {
		return fmt.Errorf("failed to execute migration %s: %w", migration.Filename, err)
	}

	insertSQL := `
		INSERT INTO schema_version (
			version_rank, installed_rank, version, description, type, 
			script, installed_by, execution_time, success
		) VALUES ($1, $2, $3, $4, 'SQL', $5, 'testcontainer', 0, true)
	`

	description := strings.Replace(
		strings.Split(migration.Filename, "__")[1],
		".sql", "", 1,
	)

	_, err = tx.Exec(insertSQL,
		extractVersionRank(migration.Version),
		extractVersionRank(migration.Version),
		migration.Version,
		description,
		migration.Filename,
	)
	if err != nil {
		return fmt.Errorf("failed to record migration %s: %w", migration.Filename, err)
	}

	if err = tx.Commit(); err != nil {
		return fmt.Errorf("failed to commit migration %s: %w", migration.Filename, err)
	}

	return nil
}

// extractVersionRank extracts numeric rank from version string (e.g., 1 from "V00001")
func extractVersionRank(version string) int {
	numStr := strings.TrimPrefix(version, "V")
	numStr = strings.TrimLeft(numStr, "0")

	if numStr == "" {
		return 0
	}

	rank := 0
	for _, c := range numStr {
		if c >= '0' && c <= '9' {
			rank = rank*10 + int(c-'0')
		}
	}

	return rank
}

// applyMigrations applies all pending Web3Signer migrations
func (env *TestEnvironment) applyMigrations() error {
	if env.postgresDB == nil {
		return fmt.Errorf("database connection not initialized")
	}

	if err := env.createMigrationTable(env.postgresDB); err != nil {
		return fmt.Errorf("failed to create migration table: %w", err)
	}

	migrations, err := env.loadMigrations()
	if err != nil {
		return fmt.Errorf("failed to load migrations: %w", err)
	}

	for _, migration := range migrations {
		applied, err := env.isMigrationApplied(env.postgresDB, migration.Version)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}

		if !applied {
			if err = env.applyMigration(env.postgresDB, migration); err != nil {
				return fmt.Errorf("failed to apply migration: %w", err)
			}
		}
	}

	return nil
}

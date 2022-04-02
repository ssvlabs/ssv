package cli

import (
	"bufio"
	"errors"
	"math"
	"os"
	"path/filepath"

	"github.com/dgraph-io/badger/v3"
	"github.com/spf13/cobra"
)

var (
	dir         string
	backupFile  string
	numVersions int
	restoreFile string
)

func init() {
	RootCmd.PersistentFlags().StringVar(&dir, "db-dir", "",
		"DB directory (required)")

	RootCmd.AddCommand(backupCmd)
	backupCmd.Flags().StringVarP(&backupFile, "file", "f",
		"database.bak", "File to backup to")
	backupCmd.Flags().IntVarP(&numVersions, "num-versions", "n",
		0, "Number of versions to keep. A value <= 0 means keep all versions.")

	RootCmd.AddCommand(restoreCmd)
	restoreCmd.Flags().StringVarP(&restoreFile, "file", "f",
		"database.bak", "File to restore from")
}

var backupCmd = &cobra.Command{
	Use:   "db-backup",
	Short: "Backup database.",
	Long:  `Backup Badger database to a file in a version-agnostic manner.`,
	RunE:  doBackup,
}

var restoreCmd = &cobra.Command{
	Use:   "db-restore",
	Short: "Restore database.",
	Long:  `Restore database from a file.`,
	RunE:  doRestore,
}

func doBackup(_ *cobra.Command, _ []string) error {
	if dir == "" {
		return errors.New("no DB directory provided")
	}

	opt := badger.DefaultOptions(dir).
		WithNumVersionsToKeep(math.MaxInt32)

	if numVersions > 0 {
		opt.NumVersionsToKeep = numVersions
	}

	db, err := badger.Open(opt)
	if err != nil {
		return err
	}
	defer db.Close()

	f, err := os.Create(backupFile)
	if err != nil {
		return err
	}

	bw := bufio.NewWriterSize(f, 64<<20)
	if _, err = db.Backup(bw, 0); err != nil {
		return err
	}

	if err = bw.Flush(); err != nil {
		return err
	}

	if err = f.Sync(); err != nil {
		return err
	}

	return f.Close()
}

func doRestore(_ *cobra.Command, _ []string) error {
	if dir == "" {
		return errors.New("no DB directory provided")
	}

	manifestFile := filepath.Join(dir, badger.ManifestFilename)
	if _, err := os.Stat(manifestFile); err == nil {
		return errors.New("directory contains a DB")
	} else if !os.IsNotExist(err) {
		return err
	}

	db, err := badger.Open(badger.DefaultOptions(dir).
		WithNumVersionsToKeep(math.MaxInt32))
	if err != nil {
		return err
	}
	defer db.Close()

	f, err := os.Open(restoreFile)
	if err != nil {
		return err
	}
	defer f.Close()

	const maxPendingWrites = 256
	return db.Load(f, maxPendingWrites)
}

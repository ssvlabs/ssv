package dirk

import (
	"os"
	"path/filepath"
)

// Structure represents the directory structure for a Dirk installation.
type Structure struct {
	// BaseDir is the main directory for Dirk.
	BaseDir string

	// WalletsDir is where wallet configurations are stored.
	WalletsDir string

	// KeystoresDir is where keystores are stored.
	KeystoresDir string
}

// NewStructure creates a new directory structure object with initialized paths.
func NewStructure(baseDir string) *Structure {
	if baseDir == "" {
		homeDir, _ := os.UserHomeDir()
		baseDir = filepath.Join(homeDir, ".dirk")
	}

	return &Structure{
		BaseDir:      baseDir,
		WalletsDir:   filepath.Join(baseDir, "wallets"),
		KeystoresDir: filepath.Join(baseDir, "wallets", "keystores"),
	}
}

// EnsureDirectories creates all required directories.
func (d *Structure) EnsureDirectories() error {
	dirs := []string{
		d.BaseDir,
		d.WalletsDir,
		d.KeystoresDir,
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0700); err != nil {
			return err
		}
	}

	return nil
}

// KeystorePath returns the path to a specific keystore file.
func (d *Structure) KeystorePath(walletName, accountName string) string {
	accountPath := accountName
	if walletName != "" {
		accountPath = filepath.Join(walletName, accountName)
	}
	return filepath.Join(d.KeystoresDir, accountPath+".json")
}

// GetWalletNames returns a list of all wallet directories in the wallets directory.
func (d *Structure) GetWalletNames() ([]string, error) {
	var wallets []string

	// Check if wallets directory exists
	if _, err := os.Stat(d.WalletsDir); os.IsNotExist(err) {
		return wallets, nil
	}

	entries, err := os.ReadDir(d.WalletsDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if entry.IsDir() {
			wallets = append(wallets, entry.Name())
		}
	}

	return wallets, nil
}

// GetAccountNames returns a list of all accounts for a specific wallet.
func (d *Structure) GetAccountNames(walletName string) ([]string, error) {
	accounts := []string{}
	keystoresDir := filepath.Join(d.WalletsDir, walletName, "keystores")

	// Check if keystores directory exists
	if _, err := os.Stat(keystoresDir); os.IsNotExist(err) {
		return accounts, nil
	}

	entries, err := os.ReadDir(keystoresDir)
	if err != nil {
		return nil, err
	}

	for _, entry := range entries {
		if !entry.IsDir() && filepath.Ext(entry.Name()) == ".json" {
			// Remove .json extension
			name := entry.Name()
			accounts = append(accounts, name[:len(name)-5])
		}
	}

	return accounts, nil
}

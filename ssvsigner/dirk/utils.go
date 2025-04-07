package dirk

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// ImportKeystoresFromFileHelper is a convenience function for importing validator keystores
// from files into Dirk's configuration directory.
func ImportKeystoresFromFileHelper(
	configDir string,
	keystorePath string,
	password string,
	walletName string,
) error {
	// Ensure the keystore file exists
	if _, err := os.Stat(keystorePath); os.IsNotExist(err) {
		return fmt.Errorf("keystore file %s does not exist", keystorePath)
	}

	// Create a keystore manager with no client (offline mode)
	manager := NewKeystoreManager(nil, "", configDir, walletName)

	// Import the keystore
	return manager.ImportKeystoreFromFile(context.Background(), keystorePath, password, walletName, "")
}

// ImportKeystoresDirHelper imports all keystores from a directory into Dirk.
func ImportKeystoresDirHelper(
	configDir string,
	keystoreDir string,
	password string,
	walletName string,
) (int, error) {
	// Ensure the directory exists
	if _, err := os.Stat(keystoreDir); os.IsNotExist(err) {
		return 0, fmt.Errorf("directory %s does not exist", keystoreDir)
	}

	// Find all JSON files
	jsonFiles, err := filepath.Glob(filepath.Join(keystoreDir, "*.json"))
	if err != nil {
		return 0, fmt.Errorf("failed to find keystore files: %w", err)
	}

	// Create keystore manager
	manager := NewKeystoreManager(nil, "", configDir, walletName)

	// Import each keystore
	imported := 0
	var lastErr error
	for _, file := range jsonFiles {
		if err := manager.ImportKeystoreFromFile(context.Background(), file, password, walletName, ""); err != nil {
			lastErr = err
			continue
		}
		imported++
	}

	if imported == 0 && lastErr != nil {
		return 0, fmt.Errorf("failed to import any keystores: %w", lastErr)
	}

	return imported, nil
}

// EthdoKeystoreImport uses ethdo to import a keystore directly.
// This is most useful as a command-line utility.
func EthdoKeystoreImport(
	ethdoPath string,
	keystorePath string,
	password string,
	walletName string,
	accountName string,
) error {
	if ethdoPath == "" {
		var err error
		ethdoPath, err = exec.LookPath("ethdo")
		if err != nil {
			return fmt.Errorf("ethdo not found in PATH: %w", err)
		}
	}

	// Construct account path
	accountPath := accountName
	if walletName != "" {
		accountPath = fmt.Sprintf("%s/%s", walletName, accountName)
	}

	// Build command
	args := []string{
		"account", "import",
		"--account=" + accountPath,
		"--keystore=" + keystorePath,
		"--passphrase=" + password,
	}

	// Run command
	cmd := exec.Command(ethdoPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ethdo command failed: %w, output: %s", err, string(output))
	}

	return nil
}

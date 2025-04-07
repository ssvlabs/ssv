package dirk

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// EthdoManager handles keystore import operations for Dirk using the ethdo tool.
// It requires a path to a functional ethdo executable.
type EthdoManager struct {
	ethdoPath     string // Mandatory path to the ethdo executable.
	defaultWallet string
	ethdoOptions  EthdoOptions
}

// EthdoOptions contains configuration options specifically for ethdo commands.
// These are passed directly to the ethdo executable.
type EthdoOptions struct {
	BaseDir      string // --base-dir
	ServerCACert string // --server-ca-cert
	ClientCert   string // --client-cert
	ClientKey    string // --client-key
	Remote       string // --remote
}

// NewEthdoManager creates a new ethdo manager responsible for importing keystores via ethdo.
// It requires the path to the ethdo executable.
func NewEthdoManager(ethdoPath string, defaultWallet string) (*EthdoManager, error) {
	if ethdoPath == "" {
		return nil, fmt.Errorf("ethdo path cannot be empty")
	}
	info, err := os.Stat(ethdoPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("ethdo executable not found at path %s", ethdoPath)
		}
		return nil, fmt.Errorf("failed to stat ethdo path %s: %w", ethdoPath, err)
	}
	if info.IsDir() || info.Mode()&0111 == 0 { // Check if it's a directory or not executable
		return nil, fmt.Errorf("ethdo path %s is not an executable file", ethdoPath)
	}

	// Set default wallet if not provided
	if defaultWallet == "" {
		defaultWallet = "Wallet" // Default Dirk wallet name
	}

	return &EthdoManager{
		ethdoPath:     ethdoPath,
		defaultWallet: defaultWallet,
	}, nil
}

// WithEthdoOptions sets additional options for ethdo commands (e.g., --base-dir, --remote, TLS certs).
// These options are passed directly when invoking the ethdo tool.
func (em *EthdoManager) WithEthdoOptions(opts EthdoOptions) *EthdoManager {
	em.ethdoOptions = opts
	return em
}

// ImportKeystoreFromFile imports a keystore from a file using ethdo.
func (em *EthdoManager) ImportKeystoreFromFile(
	ctx context.Context,
	keystorePath string,
	password string,
	walletName string,
	accountName string,
) error {
	keystoreContent, err := os.ReadFile(keystorePath)
	if err != nil {
		return fmt.Errorf("failed to read keystore file %s: %w", keystorePath, err)
	}
	return em.ImportKeystoreJSON(ctx, keystoreContent, password, walletName, accountName)
}

// ImportKeystoreJSON imports a keystore from JSON data using ethdo.
func (em *EthdoManager) ImportKeystoreJSON(
	ctx context.Context,
	keystoreContent []byte,
	password string,
	walletName string,
	accountName string,
) error {
	if walletName == "" {
		walletName = em.defaultWallet
	}

	// Determine account name if not provided
	if accountName == "" {
		accountName = generateAccountName(keystoreContent)
	}

	// Ensure ethdo path is set (guaranteed by constructor)
	return em.importWithEthdo(ctx, walletName, accountName, keystoreContent, password)
}

// generateAccountName creates a default account name based on the keystore content.
// It uses the first 8 characters of the public key if available, otherwise a timestamp.
func generateAccountName(keystoreContent []byte) string {
	var keystoreData map[string]interface{}
	// Use json.Unmarshal for better error handling if needed, but simple check is fine here.
	_ = json.Unmarshal(keystoreContent, &keystoreData) // Ignore error for simple name generation

	if pubkey, ok := keystoreData["pubkey"].(string); ok && len(pubkey) >= 8 {
		return fmt.Sprintf("validator-%s", pubkey[:8])
	}

	// Fallback to timestamp-based name
	return fmt.Sprintf("validator-%d", time.Now().Unix())
}

// importWithEthdo executes the `ethdo account import` command.
func (em *EthdoManager) importWithEthdo(
	ctx context.Context,
	walletName string,
	accountName string,
	keystoreContent []byte,
	password string,
) error {
	// Create a temporary file for the keystore
	tmpFile, err := os.CreateTemp("", "dirk-keystore-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temporary keystore file: %w", err)
	}
	defer os.Remove(tmpFile.Name()) // Ensure cleanup

	// Write the keystore to the temporary file
	if _, err := tmpFile.Write(keystoreContent); err != nil {
		// Attempt to close before returning error
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write to temporary keystore file %s: %w", tmpFile.Name(), err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temporary keystore file %s: %w", tmpFile.Name(), err)
	}

	// Construct the target account path for ethdo
	accountPath := fmt.Sprintf("%s/%s", walletName, accountName)

	// Build ethdo command arguments
	args := []string{
		"account", "import",
		"--account=" + accountPath,
		"--keystore=" + tmpFile.Name(),
		"--passphrase=" + password,
	}

	// Add optional ethdo arguments from EthdoOptions
	if em.ethdoOptions.BaseDir != "" {
		args = append(args, "--base-dir="+em.ethdoOptions.BaseDir)
	}
	if em.ethdoOptions.Remote != "" {
		args = append(args, "--remote="+em.ethdoOptions.Remote)
	}
	if em.ethdoOptions.ServerCACert != "" {
		args = append(args, "--server-ca-cert="+em.ethdoOptions.ServerCACert)
	}
	if em.ethdoOptions.ClientCert != "" {
		args = append(args, "--client-cert="+em.ethdoOptions.ClientCert)
	}
	if em.ethdoOptions.ClientKey != "" {
		args = append(args, "--client-key="+em.ethdoOptions.ClientKey)
	}
	// Consider adding --allow-weak-passphrases? Maybe as an EthdoOption? For now, rely on ethdo default.

	// Execute ethdo command
	cmd := exec.CommandContext(ctx, em.ethdoPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		// Provide more context in the error message
		return fmt.Errorf("ethdo account import failed for account %s: %w, output: %s", accountPath, err, string(output))
	}

	// ethdo command succeeded
	return nil
}

// ImportKeystoresFromDirectory imports all keystore JSON files from a specified directory
// using the configured ethdo executable.
func ImportKeystoresFromDirectory(
	ctx context.Context,
	ethdoPath string, // Mandatory: Path to ethdo executable
	directoryPath string,
	password string, // Assumes the same password for all keystores in the directory
	walletName string, // Target wallet name in Dirk
	ethdoOpts EthdoOptions, // Optional: Additional options for ethdo
) (imported int, failed int, finalErr error) { // Use named return for clarity
	// Create an EthdoManager instance
	manager, err := NewEthdoManager(ethdoPath, walletName)
	if err != nil {
		finalErr = fmt.Errorf("failed to create ethdo manager: %w", err)
		return
	}
	// Apply ethdo options if provided
	if ethdoOpts != (EthdoOptions{}) {
		manager.ethdoOptions = ethdoOpts
	}

	// Check if the source directory exists
	if _, err := os.Stat(directoryPath); err != nil {
		if os.IsNotExist(err) {
			finalErr = fmt.Errorf("source directory %s does not exist", directoryPath)
		} else {
			finalErr = fmt.Errorf("failed to stat source directory %s: %w", directoryPath, err)
		}
		return
	}

	// Find keystore files
	files, err := filepath.Glob(filepath.Join(directoryPath, "*.json"))
	if err != nil {
		finalErr = fmt.Errorf("failed to find keystore files in %s: %w", directoryPath, err)
		return
	}
	if len(files) == 0 {
		// Not necessarily an error, but maybe worth noting or returning a specific status?
		// For now, return success with 0 imported.
		return 0, 0, nil
	}

	// Process each file
	var importErrors []error
	for _, file := range files {
		baseName := filepath.Base(file)
		// Basic check to avoid hidden files
		if strings.HasPrefix(baseName, ".") {
			continue
		}
		accountName := baseName[:len(baseName)-len(filepath.Ext(baseName))] // Robust way to remove extension

		// Attempt to import the individual keystore file
		if err := manager.ImportKeystoreFromFile(
			ctx,
			file,
			password,
			walletName,
			accountName,
		); err != nil {
			failed++
			// Collect errors instead of just counting failures
			importErrors = append(importErrors, fmt.Errorf("failed to import %s as %s/%s: %w", file, walletName, accountName, err))
			continue // Continue with the next file
		}
		imported++
	}

	// Aggregate errors if any occurred
	if len(importErrors) > 0 {
		// Combine errors into a single error message
		errorMessages := make([]string, len(importErrors))
		for i, e := range importErrors {
			errorMessages[i] = e.Error()
		}
		finalErr = fmt.Errorf("%d keystores imported, %d failed: [\n%s\n]", imported, failed, strings.Join(errorMessages, ",\n"))
	}

	return
}

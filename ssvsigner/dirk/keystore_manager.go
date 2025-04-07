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

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// KeystoreManager handles keystore operations for Dirk.
type KeystoreManager struct {
	client        Client
	ethdoPath     string
	configDir     string
	defaultWallet string
	ethdoOptions  EthdoOptions
}

// EthdoOptions contains configuration options for ethdo commands.
type EthdoOptions struct {
	BaseDir      string
	ServerCACert string
	ClientCert   string
	ClientKey    string
	Remote       string
}

// NewKeystoreManager creates a new keystore manager.
func NewKeystoreManager(
	client Client,
	ethdoPath string,
	configDir string,
	defaultWallet string,
) *KeystoreManager {
	return &KeystoreManager{
		client:        client,
		ethdoPath:     ethdoPath,
		configDir:     configDir,
		defaultWallet: defaultWallet,
	}
}

// WithEthdoOptions sets additional options for ethdo commands.
func (km *KeystoreManager) WithEthdoOptions(baseDir, serverCACert, clientCert, clientKey, remote string) *KeystoreManager {
	km.ethdoOptions = EthdoOptions{
		BaseDir:      baseDir,
		ServerCACert: serverCACert,
		ClientCert:   clientCert,
		ClientKey:    clientKey,
		Remote:       remote,
	}
	return km
}

// ImportKeystoreFromFile imports a keystore from a file.
func (km *KeystoreManager) ImportKeystoreFromFile(
	ctx context.Context,
	keystorePath string,
	password string,
	walletName string,
	accountName string,
) error {
	keystoreContent, err := os.ReadFile(keystorePath)
	if err != nil {
		return fmt.Errorf("failed to read keystore file: %w", err)
	}

	return km.ImportKeystoreJSON(ctx, keystoreContent, password, walletName, accountName)
}

// ImportKeystoreJSON imports a keystore from JSON data.
func (km *KeystoreManager) ImportKeystoreJSON(
	ctx context.Context,
	keystoreContent []byte,
	password string,
	walletName string,
	accountName string,
) error {
	if walletName == "" {
		walletName = km.defaultWallet
		if walletName == "" {
			walletName = "Wallet"
		}
	}

	// Determine account name if not provided
	if accountName == "" {
		accountName = generateAccountName(keystoreContent)
	}

	return km.importKeystore(ctx, keystoreContent, password, walletName, accountName)
}

// importKeystore handles the actual import using the best available method.
func (km *KeystoreManager) importKeystore(
	ctx context.Context,
	keystoreContent []byte,
	password string,
	walletName string,
	accountName string,
) error {
	// Try methods in order of preference:
	// 1. Use ethdo if available (best option)
	if km.ethdoPath != "" {
		return km.importWithEthdo(ctx, walletName, accountName, keystoreContent, password)
	}

	// 2. Use direct file placement if config dir is provided
	if km.configDir != "" {
		accountPath := fmt.Sprintf("%s/%s", walletName, accountName)
		return km.importToConfigDir(accountPath, keystoreContent)
	}

	return fmt.Errorf("no available import method: ethdo not found and config directory not specified")
}

// generateAccountName creates an account name based on the keystore content.
func generateAccountName(keystoreContent []byte) string {
	var keystoreData map[string]interface{}
	if err := json.Unmarshal(keystoreContent, &keystoreData); err == nil {
		if pubkey, ok := keystoreData["pubkey"].(string); ok {
			if len(pubkey) > 8 {
				return fmt.Sprintf("validator-%s", pubkey[:8])
			}
		}
	}

	// Fallback to timestamp-based name
	return fmt.Sprintf("validator-%d", time.Now().Unix())
}

// importWithEthdo imports a keystore using the ethdo command-line tool.
func (km *KeystoreManager) importWithEthdo(
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
	defer os.Remove(tmpFile.Name())

	// Write the keystore to the file
	if _, err := tmpFile.Write(keystoreContent); err != nil {
		return fmt.Errorf("failed to write to temporary keystore file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close keystore file: %w", err)
	}

	accountPath := fmt.Sprintf("%s/%s", walletName, accountName)

	// Build ethdo command arguments
	args := []string{
		"account", "import",
		"--account=" + accountPath,
		"--keystore=" + tmpFile.Name(),
		"--passphrase=" + password,
	}

	// Add optional ethdo arguments if provided
	if km.ethdoOptions.BaseDir != "" {
		args = append(args, "--base-dir="+km.ethdoOptions.BaseDir)
	}
	if km.ethdoOptions.Remote != "" {
		args = append(args, "--remote="+km.ethdoOptions.Remote)
	}
	if km.ethdoOptions.ServerCACert != "" {
		args = append(args, "--server-ca-cert="+km.ethdoOptions.ServerCACert)
	}
	if km.ethdoOptions.ClientCert != "" {
		args = append(args, "--client-cert="+km.ethdoOptions.ClientCert)
	}
	if km.ethdoOptions.ClientKey != "" {
		args = append(args, "--client-key="+km.ethdoOptions.ClientKey)
	}

	// Execute ethdo command
	cmd := exec.CommandContext(ctx, km.ethdoPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ethdo import failed: %w, output: %s", err, string(output))
	}

	return nil
}

// importToConfigDir writes the keystore directly to the Dirk configuration directory.
func (km *KeystoreManager) importToConfigDir(accountPath string, keystoreContent []byte) error {
	dirStructure := NewStructure(km.configDir)

	if err := dirStructure.EnsureDirectories(); err != nil {
		return fmt.Errorf("failed to create Dirk directories: %w", err)
	}

	var walletName, accountName string
	parts := strings.Split(accountPath, "/")
	if len(parts) >= 2 {
		walletName = parts[0]
		accountName = parts[1]
	} else {
		walletName = km.defaultWallet
		if walletName == "" {
			walletName = "Wallet"
		}
		accountName = accountPath
	}

	keystorePath := dirStructure.KeystorePath(walletName, accountName)

	if err := os.MkdirAll(filepath.Dir(keystorePath), 0700); err != nil {
		return fmt.Errorf("failed to create keystore directory: %w", err)
	}

	if err := os.WriteFile(keystorePath, keystoreContent, 0600); err != nil {
		return fmt.Errorf("failed to write keystore file: %w", err)
	}

	return nil
}

// DeleteByPublicKey deletes a keystore by its public key.
func DeleteByPublicKey(
	ctx context.Context,
	client Client,
	configDir string,
	pubKey phase0.BLSPubKey,
) error {
	// Try to get accounts list to find matching public key
	accounts, err := client.ListAccounts(ctx)
	if err != nil {
		return fmt.Errorf("failed to get accounts list: %w", err)
	}

	// Find account with matching public key
	var accountName string
	for _, account := range accounts {
		if account.PublicKey == pubKey {
			accountName = account.Name
			break
		}
	}

	if accountName == "" {
		return fmt.Errorf("account with public key %s not found", pubKey.String())
	}

	return DeleteByAccountName(ctx, configDir, accountName)
}

// DeleteByAccountName deletes a keystore by its account name.
func DeleteByAccountName(
	ctx context.Context,
	configDir string,
	accountName string,
) error {
	// If only base account name provided, add default wallet
	if filepath.Base(accountName) == accountName {
		accountName = fmt.Sprintf("Wallet/%s", accountName)
	}

	// Manual file deletion if config dir provided
	if configDir != "" {
		dirStructure := NewStructure(configDir)

		// Split account path into wallet and account name
		var walletName, accountNameBase string
		parts := strings.Split(accountName, "/")
		if len(parts) >= 2 {
			walletName = parts[0]
			accountNameBase = parts[1]
		} else {
			walletName = "wallet" // Default wallet name
			accountNameBase = accountName
		}

		// Get path to keystore file
		keystorePath := dirStructure.KeystorePath(walletName, accountNameBase)

		// Check if file exists
		_, err := os.Stat(keystorePath)
		if os.IsNotExist(err) {
			return fmt.Errorf("keystore file for account %s does not exist at path %s", accountName, keystorePath)
		}

		// Delete the file
		if err := os.Remove(keystorePath); err != nil {
			return fmt.Errorf("failed to delete keystore file: %w", err)
		}

		return nil
	}

	return fmt.Errorf("config directory not provided, cannot delete keystore")
}

// ImportKeystoresFromDirectory imports all keystore JSON files from a directory.
func ImportKeystoresFromDirectory(
	ctx context.Context,
	client Client,
	ethdoPath string,
	configDir string,
	directoryPath string,
	password string,
	walletName string,
) (imported int, failed int, err error) {
	manager := NewKeystoreManager(client, ethdoPath, configDir, walletName)

	if _, err := os.Stat(directoryPath); os.IsNotExist(err) {
		return 0, 0, fmt.Errorf("directory %s does not exist", directoryPath)
	}

	files, err := filepath.Glob(filepath.Join(directoryPath, "*.json"))
	if err != nil {
		return 0, 0, fmt.Errorf("failed to find keystore files: %w", err)
	}

	for _, file := range files {
		baseName := filepath.Base(file)
		accountName := baseName[:len(baseName)-5] // Remove .json extension

		if err := manager.ImportKeystoreFromFile(
			ctx,
			file,
			password,
			walletName,
			accountName,
		); err != nil {
			failed++
			continue
		}
		imported++
	}

	if failed > 0 {
		return imported, failed, fmt.Errorf("%d keystores imported, %d failed", imported, failed)
	}
	return imported, 0, nil
}

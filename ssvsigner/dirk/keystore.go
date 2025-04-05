package dirk

import (
	"context"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/attestantio/go-eth2-client/spec/phase0"
)

// KeystoreManager handles keystore operations for Dirk
type KeystoreManager struct {
	client        Client
	ethdoPath     string
	configDir     string
	defaultWallet string
	baseDir       string
	serverCACert  string
	clientCert    string
	clientKey     string
	remote        string
}

// NewKeystoreManager creates a new keystore manager
func NewKeystoreManager(client Client, ethdoPath, configDir, defaultWallet string) *KeystoreManager {
	return &KeystoreManager{
		client:        client,
		ethdoPath:     ethdoPath,
		configDir:     configDir,
		defaultWallet: defaultWallet,
	}
}

// WithEthdoOptions sets additional options for ethdo commands
func (km *KeystoreManager) WithEthdoOptions(baseDir, serverCACert, clientCert, clientKey, remote string) *KeystoreManager {
	km.baseDir = baseDir
	km.serverCACert = serverCACert
	km.clientCert = clientCert
	km.clientKey = clientKey
	km.remote = remote
	return km
}

// ImportKeystoreFromFile imports a keystore from a file
func (km *KeystoreManager) ImportKeystoreFromFile(
	ctx context.Context,
	keystorePath string,
	password string,
	walletName string,
	accountName string,
) error {
	keystoreContent, err := ioutil.ReadFile(keystorePath)
	if err != nil {
		return fmt.Errorf("failed to read keystore file: %w", err)
	}

	return km.ImportKeystoreJSON(ctx, keystoreContent, password, walletName, accountName)
}

// ImportKeystoreJSON imports a keystore from JSON data
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

	if accountName == "" {
		var keystoreData map[string]interface{}
		if err := json.Unmarshal(keystoreContent, &keystoreData); err == nil {
			if pubkey, ok := keystoreData["pubkey"].(string); ok {
				if len(pubkey) > 8 {
					accountName = fmt.Sprintf("validator-%s", pubkey[:8])
				}
			}
		}

		if accountName == "" {
			accountName = fmt.Sprintf("validator-%d", time.Now().Unix())
		}
	}

	if km.client != nil {
		return km.client.ImportAccount(
			ctx,
			walletName,
			accountName,
			keystoreContent,
			[]byte(password),
			nil,
		)
	}

	if km.ethdoPath != "" {
		return km.importWithEthdo(ctx, walletName, accountName, keystoreContent, password)
	}

	if km.configDir != "" {
		return km.importToConfigDir(fmt.Sprintf("%s/%s", walletName, accountName), keystoreContent)
	}

	return fmt.Errorf("no available import method")
}

func (km *KeystoreManager) importWithEthdo(
	ctx context.Context,
	walletName string,
	accountName string,
	keystoreContent []byte,
	password string,
) error {
	tmpFile, err := os.CreateTemp("", "dirk-keystore-*.json")
	if err != nil {
		return fmt.Errorf("failed to create temporary keystore file: %w", err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.Write(keystoreContent); err != nil {
		return fmt.Errorf("failed to write to temporary keystore file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close keystore file: %w", err)
	}

	accountPath := fmt.Sprintf("%s/%s", walletName, accountName)

	args := []string{
		"account", "import",
		"--account=" + accountPath,
		"--keystore=" + tmpFile.Name(),
		"--passphrase=" + password,
	}

	if km.baseDir != "" {
		args = append(args, "--base-dir="+km.baseDir)
	}
	if km.remote != "" {
		args = append(args, "--remote="+km.remote)
	}
	if km.serverCACert != "" {
		args = append(args, "--server-ca-cert="+km.serverCACert)
	}
	if km.clientCert != "" {
		args = append(args, "--client-cert="+km.clientCert)
	}
	if km.clientKey != "" {
		args = append(args, "--client-key="+km.clientKey)
	}

	cmd := exec.CommandContext(ctx, km.ethdoPath, args...)

	output, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("ethdo import failed: %w, output: %s", err, string(output))
	}

	return nil
}

func (km *KeystoreManager) importToConfigDir(accountPath string, keystoreContent []byte) error {
	dirStructure := NewDirkStructure(km.configDir)

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

// DeleteByPublicKey deletes a keystore by its public key
func DeleteByPublicKey(
	ctx context.Context,
	client Client,
	configDir string,
	pubKey phase0.BLSPubKey,
) error {
	// Try to get accounts list to find matching public key
	accounts, err := client.ListAccounts()
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

	return DeleteByAccountName(ctx, client, configDir, accountName)
}

// DeleteByAccountName deletes a keystore by its account name
func DeleteByAccountName(
	ctx context.Context,
	client Client,
	configDir string,
	accountName string,
) error {
	// If only base account name provided, add default wallet
	if filepath.Base(accountName) == accountName {
		accountName = fmt.Sprintf("Wallet/%s", accountName)
	}

	// Try to delete using client if available
	err := client.DeleteAccount(ctx, accountName)
	if err != nil {
		// If client method fails, try direct file deletion if config dir provided
		if configDir != "" {
			dirStructure := NewDirkStructure(configDir)

			// Split account path into wallet and account name
			var walletName, accountNameBase string
			parts := strings.Split(accountName, "/")
			if len(parts) >= 2 {
				walletName = parts[0]
				accountNameBase = parts[1]
			} else {
				walletName = "Wallet" // Default value
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

		return fmt.Errorf("failed to delete account: %w", err)
	}

	return nil
}

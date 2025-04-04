package dirk

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/ssvlabs/ssv/ssvsigner/web3signer"
)

// ImportKeystore adds validator shares to Dirk
func (s *Signer) ImportKeystore(ctx context.Context, req web3signer.ImportKeystoreRequest) (web3signer.ImportKeystoreResponse, error) {
	if err := s.connect(ctx); err != nil {
		return web3signer.ImportKeystoreResponse{}, err
	}

	manager := NewKeystoreManager(
		s.client,
		getEthdoPath(s.client),
		s.credentials.ConfigDir,
		s.credentials.WalletName,
	)

	if s.credentials.CACert != "" || s.credentials.ClientCert != "" {
		manager.WithEthdoOptions(
			s.credentials.ConfigDir,
			s.credentials.CACert,
			s.credentials.ClientCert,
			s.credentials.ClientKey,
			s.endpoint,
		)
	}

	responseData := make([]web3signer.KeyManagerResponseData, 0, len(req.Keystores))

	for i, encodedKeystore := range req.Keystores {
		accountName := fmt.Sprintf("validator-%d", i)

		var password string
		if i < len(req.Passwords) {
			password = req.Passwords[i]
		} else {
			responseData = append(responseData, web3signer.KeyManagerResponseData{
				Status:  web3signer.StatusError,
				Message: fmt.Sprintf("no password provided for keystore %d", i),
			})
			continue
		}

		keystoreJSON, err := base64.StdEncoding.DecodeString(encodedKeystore)
		if err != nil {
			responseData = append(responseData, web3signer.KeyManagerResponseData{
				Status:  web3signer.StatusError,
				Message: fmt.Sprintf("failed to decode keystore %d: %v", i, err),
			})
			continue
		}

		var keystoreMap map[string]interface{}
		if err := json.Unmarshal(keystoreJSON, &keystoreMap); err == nil {
			if pubkey, ok := keystoreMap["pubkey"].(string); ok {
				if len(pubkey) > 8 {
					accountName = fmt.Sprintf("validator-%s", pubkey[:8])
				}
			}
		}

		err = manager.ImportKeystoreJSON(
			ctx,
			keystoreJSON,
			password,
			s.credentials.WalletName,
			accountName,
		)

		if err != nil {
			responseData = append(responseData, web3signer.KeyManagerResponseData{
				Status:  web3signer.StatusError,
				Message: fmt.Sprintf("failed to import keystore: %v", err),
			})
			continue
		}

		responseData = append(responseData, web3signer.KeyManagerResponseData{
			Status:  web3signer.StatusImported,
			Message: fmt.Sprintf("imported keystore to %s/%s (restart Dirk to use)", s.credentials.WalletName, accountName),
		})
	}

	return web3signer.ImportKeystoreResponse{
		Data: responseData,
	}, nil
}

// Helper function to get the ethdo path
func getEthdoPath(client Client) string {
	if gc, ok := client.(*GRPCClient); ok && gc.ethdoPath != "" {
		return gc.ethdoPath
	}

	path, _ := exec.LookPath("ethdo")
	return path
}

// ImportKeystoresFromDirectory imports all keystore JSON files from a directory
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

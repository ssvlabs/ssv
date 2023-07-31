package migrations

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"fmt"

	"go.uber.org/zap"
)

var encryptSharesMigration = Migration{
	Name: "encrypt_shares",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		nodeStorage, err := opt.nodeStorage(logger)
		if err != nil {
			return fmt.Errorf("failed to get node storage: %w", err)
		}
		signerStorage := opt.signerStorage(logger)
		accounts, err := signerStorage.ListAccounts()
		if err != nil {
			return fmt.Errorf("failed to list accounts: %w", err)
		}

		operatorKey, found, err := nodeStorage.GetPrivateKey()
		if err != nil {
			return fmt.Errorf("failed to get private key: %w", err)
		}
		if !found {
			return nil
		}
		keyBytes := x509.MarshalPKCS1PrivateKey(operatorKey)
		hash := sha256.Sum256(keyBytes)
		keyString := fmt.Sprintf("%x", hash)
		err = signerStorage.SetEncryptionKey(keyString)
		if err != nil {
			return fmt.Errorf("failed to set encryption key: %w", err)
		}
		for _, account := range accounts {
			err := signerStorage.SaveAccount(account)
			if err != nil {
				return fmt.Errorf("failed to save account %s: %w", account, err)
			}
		}
		return nil
	},
}

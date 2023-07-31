package migrations

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"fmt"

	"go.uber.org/zap"
)

// This migration is an Example migration
var encryptSharesMigration = Migration{
	Name: "encrypt_shares",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte) error {
		// Example to clean registry data for specific storage
		nodeStorage, err := opt.nodeStorage(logger)
		signerStorage := opt.signerStorage(logger)
		accounts, err := signerStorage.ListAccounts()
		if err != nil {
			return err
		}

		operatorKey, found, err := nodeStorage.GetPrivateKey()
		if !found {
			return nil
		}
		keyBytes := x509.MarshalPKCS1PrivateKey(operatorKey)
		hash := sha256.Sum256(keyBytes)
		keyString := fmt.Sprintf("%x", hash)
		err = signerStorage.SetEncryptionKey(keyString)
		if err != nil {
			return err
		}
		for _, account := range accounts {
			err := signerStorage.SaveAccount(account)
			if err != nil {
				return err
			}
		}
		if err != nil {
			return err
		}
		return nil
	},
}

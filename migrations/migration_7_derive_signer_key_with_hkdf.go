package migrations

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"

	"go.uber.org/zap"
	"golang.org/x/crypto/hkdf"

	"github.com/ssvlabs/ssv/storage/basedb"
)

var migration_7_derive_signer_key_with_hkdf = Migration{
	Name: "migration_7_derive_signer_key_with_hkdf",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			hashedKey, found, err := opt.NodeStorage.GetPrivateKeyHash()
			if err != nil {
				return fmt.Errorf("failed to get private key hash: %w", err)
			}

			if !found {
				logger.Warn("private key hash not found, skipping migration")
				return completed(txn)
			}

			signerStorage := opt.signerStorage(logger)

			err = signerStorage.SetEncryptionKey(hashedKey)
			if err != nil {
				return fmt.Errorf("failed to set old encryption key: %w", err)
			}

			accounts, err := signerStorage.ListAccountsTxn(txn)
			if err != nil {
				return fmt.Errorf("failed to list accounts: %w", err)
			}

			if len(accounts) == 0 {
				logger.Info("no accounts to migrate")
				return completed(txn)
			}

			// new key with HKDF from the stored hash
			hashBytes, err := hex.DecodeString(hashedKey)
			if err != nil {
				return fmt.Errorf("failed to decode hash: %w", err)
			}

			kdf := hkdf.New(sha256.New, hashBytes, nil, nil)
			newKeyBytes := make([]byte, 32)
			if _, err := io.ReadFull(kdf, newKeyBytes); err != nil {
				return fmt.Errorf("failed to derive new key: %w", err)
			}
			newKeyString := hex.EncodeToString(newKeyBytes)

			// re-encryption with a new key
			err = signerStorage.SetEncryptionKey(newKeyString)
			if err != nil {
				return fmt.Errorf("failed to set new encryption key: %w", err)
			}

			for _, account := range accounts {
				err := signerStorage.SaveAccountTxn(txn, account)
				if err != nil {
					return fmt.Errorf("failed to re-encrypt account %s: %w", account.ID(), err)
				}
			}

			logger.Info("re-encrypted accounts with HKDF-derived key", zap.Int("count", len(accounts)))
			return completed(txn)
		})
	},
}

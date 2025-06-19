package migrations

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

var migration_7_derive_signer_key_with_hkdf = Migration{
	Name: "migration_7_derive_signer_key_with_hkdf",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			if opt.OperatorPrivKey == nil {
				// No migration needed if using remote signer.
				return nil
			}

			signerStorage := opt.signerStorage(logger)

			err := signerStorage.SetEncryptionKey(opt.OperatorPrivKey.EKMHash())
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

			encryptionKey, err := opt.OperatorPrivKey.EKMEncryptionKey()
			if err != nil {
				return fmt.Errorf("failed to get encryption key: %w", err)
			}

			// re-encryption with the new algorithm

			if err := signerStorage.SetEncryptionKey(encryptionKey); err != nil {
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

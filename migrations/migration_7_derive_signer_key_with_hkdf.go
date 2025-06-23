package migrations

import (
	"context"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/storage/basedb"
)

var migration_7_derive_signer_key_with_hkdf = Migration{
	Name: "migration_7_derive_signer_key_with_hkdf",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) (err error) {
		defer func() {
			if err != nil {
				return // cannot complete migration successfully
			}
			// complete migration, this makes sure migration applies only once
			if err = completed(opt.Db); err != nil {
				err = fmt.Errorf("complete transaction: %w", err)
				return
			}
		}()

		err = opt.Db.Update(func(txn basedb.Txn) error {
			if opt.OperatorPrivKey == nil {
				// No migration needed if using remote signer.
				return nil
			}

			// Set storage key to old format so we can decrypt stored data with it.
			signerStorage := opt.signerStorage(logger)
			signerStorage.SetEncryptionKey(opt.OperatorPrivKey.EKMHash())

			accounts, err := signerStorage.ListAccountsTxn(txn)
			if err != nil {
				return fmt.Errorf("failed to list accounts: %w", err)
			}

			if len(accounts) == 0 {
				logger.Info("no accounts to migrate")
				return nil
			}

			// Set storage key to new format so we can re-encrypt stored data with it.
			encryptionKey, err := opt.OperatorPrivKey.EKMEncryptionKey()
			if err != nil {
				return fmt.Errorf("failed to get encryption key: %w", err)
			}
			signerStorage.SetEncryptionKey(encryptionKey)

			for _, account := range accounts {
				err := signerStorage.SaveAccountTxn(txn, account)
				if err != nil {
					return fmt.Errorf("failed to save account %s: %w", account.ID(), err)
				}
			}

			logger.Info("re-encrypted accounts with HKDF-derived key", zap.Int("count", len(accounts)))
			return nil
		})
		return err
	},
}

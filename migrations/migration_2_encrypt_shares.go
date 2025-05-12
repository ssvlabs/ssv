package migrations

import (
	"context"
	"crypto/sha256"
	"crypto/x509"
	"fmt"

	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/keys/rsaencryption"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var migration_2_encrypt_shares = Migration{
	Name: "migration_2_encrypt_shares",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			err := txn.Set(migrationsPrefix, key, migrationCompleted)
			if err != nil {
				return err
			}
			obj, found, err := txn.Get([]byte("operator/"), []byte("private-key"))
			if err != nil {
				return fmt.Errorf("failed to get private key: %w", err)
			}
			if !found {
				return completed(txn)
			}
			operatorKey, err := rsaencryption.PEMToPrivateKey(obj.Value)
			if err != nil {
				return fmt.Errorf("failed to get private key: %w", err)
			}

			signerStorage := opt.signerStorage(logger)
			accounts, err := signerStorage.ListAccountsTxn(txn)
			if err != nil {
				return fmt.Errorf("failed to list accounts: %w", err)
			}
			keyBytes := x509.MarshalPKCS1PrivateKey(operatorKey)
			hash := sha256.Sum256(keyBytes)
			keyString := fmt.Sprintf("%x", hash)
			err = signerStorage.SetEncryptionKey(keyString)
			if err != nil {
				return fmt.Errorf("failed to set encryption key: %w", err)
			}
			for _, account := range accounts {
				err := signerStorage.SaveAccountTxn(txn, account)
				if err != nil {
					return fmt.Errorf("failed to save account %s: %w", account, err)
				}
			}
			err = txn.Delete([]byte("operator/"), []byte("private-key"))
			if err != nil {
				return fmt.Errorf("failed to delete private key: %w", err)
			}
			return completed(txn)
		})
	},
}

package migrations

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/storage/basedb"
)

var migration_4_standalone_slashing_data = Migration{
	Name: "migration_4_standalone_slashing_data",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			signerStorage := opt.signerStorage(logger)
			legacySPStorage := opt.legacySlashingProtectionStorage(logger)
			spStorage := opt.slashingProtectionStorage(logger)

			obj, found, err := txn.Get([]byte("operator/"), []byte("hashed-private-key"))
			if err != nil {
				return fmt.Errorf("failed to get hashed private key: %w", err)
			}

			if found {
				if err := signerStorage.SetEncryptionKey(string(obj.Value)); err != nil {
					return fmt.Errorf("failed to set encryption key: %w", err)
				}
			}

			accounts, err := signerStorage.ListAccountsTxn(txn)
			if err != nil {
				return fmt.Errorf("failed to list accounts: %w", err)
			}

			initialized, err := spStorage.IsInitialized()
			if err != nil {
				return fmt.Errorf("failed to check if slashing protection db is initialized: %w", err)
			}

			// Edge case where the nodeDB and sp DB exists, but migration was not completed
			// In this case, we want to throw an error
			if len(accounts) > 0 && initialized {
				return fmt.Errorf("can not migrate legacy slashing protection data over existing slashing protection data")
			}

			for _, account := range accounts {
				sharePubKey := account.ValidatorPublicKey()

				// migrate highest attestation slashing protection data
				highAtt, found, err := legacySPStorage.RetrieveHighestAttestation(sharePubKey)
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("highest attestation not found for share %s", hex.EncodeToString(sharePubKey))
				}
				if highAtt == nil {
					return fmt.Errorf("highest attestation is nil for share %s", hex.EncodeToString(sharePubKey))
				}

				// save slashing protection in the new standalone storage
				if err := spStorage.SaveHighestAttestation(sharePubKey, highAtt); err != nil {
					return fmt.Errorf("failed to save highest attestation for share %s: %w", hex.EncodeToString(sharePubKey), err)
				}

				// migrate highest proposal slashing protection data
				highProposal, found, err := legacySPStorage.RetrieveHighestProposal(sharePubKey)
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("highest proposal not found for share %s", hex.EncodeToString(sharePubKey))
				}
				if highProposal == 0 {
					return fmt.Errorf("highest proposal is 0 for share %s", hex.EncodeToString(sharePubKey))
				}

				if err := spStorage.SaveHighestProposal(sharePubKey, highProposal); err != nil {
					return fmt.Errorf("failed to save highest proposal for share %s: %w", hex.EncodeToString(sharePubKey), err)
				}

				// ensure the data is saved and can be read.
				migratedHighAtt, found, err := spStorage.RetrieveHighestAttestation(sharePubKey)
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("migrated highest attestation not found for share %s", hex.EncodeToString(sharePubKey))
				}
				if !reflect.DeepEqual(migratedHighAtt, highAtt) {
					return fmt.Errorf("migrated highest attestation is not equal to original for share %s", hex.EncodeToString(sharePubKey))
				}

				migratedHighProp, found, err := spStorage.RetrieveHighestProposal(sharePubKey)
				if err != nil {
					return err
				}
				if !found {
					return fmt.Errorf("migrated highest proposal not found for share %s", hex.EncodeToString(sharePubKey))
				}
				if migratedHighProp != highProposal {
					return fmt.Errorf("migrated highest proposal is not equal to original for share %s", hex.EncodeToString(sharePubKey))
				}
			}

			if err := opt.SpDb.SetType(ekm.SlashingDBName); err != nil {
				return fmt.Errorf("failed to set slashing protection db type: %w", err)
			}

			logger.Info("migrated slashing protection data for accounts", zap.Int("accounts", len(accounts)))

			// NOTE: skip removing legacy data, for unexpected migration behavior to rescue the slashing protection data
			return completed(txn)
		})
	},
}

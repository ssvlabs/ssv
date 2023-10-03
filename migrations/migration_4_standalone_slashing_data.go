package migrations

import (
	"context"
	"encoding/hex"
	"fmt"
	"reflect"

	"github.com/bloxapp/ssv/storage/basedb"

	"go.uber.org/zap"
)

var migration_4_standalone_slashing_data = Migration{
	Name: "migration_4_standalone_slashing_data",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			signerStorage := opt.signerStorage(logger)
			legacySPStorage := opt.legacySlashingProtectionStorage(logger)
			spStorage := opt.slashingProtectionStorage(logger)

			accounts, err := signerStorage.ListAccountsTxn(txn)
			if err != nil {
				return fmt.Errorf("failed to list accounts: %w", err)
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

			// NOTE: skip removing legacy data, for unexpected migration behavior to rescue the slashing protection data
			return completed(txn)
		})
	},
}

package migrations

import (
	"context"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"os"
	"reflect"

	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/utils/rsaencryption"
)

var migration_4_standalone_slashing_data = Migration{
	Name: "migration_4_standalone_slashing_data",
	Run: func(ctx context.Context, logger *zap.Logger, opt Options, key []byte, completed CompletedFunc) error {
		return opt.Db.Update(func(txn basedb.Txn) error {
			signerStorage := opt.signerStorage(logger)
			legacySPStorage := opt.legacySlashingProtectionStorage(logger)
			spStorage := opt.slashingProtectionStorage(logger)

			rsaPrivateKey, err := getRSAPrivateKey(opt)
			if err != nil {
				return fmt.Errorf("failed to retrieve rsa private key: %w", err)
			}

			keyBytes := x509.MarshalPKCS1PrivateKey(rsaPrivateKey)
			hash := sha256.Sum256(keyBytes)
			keyString := hex.EncodeToString(hash[:])

			if err := signerStorage.SetEncryptionKey(keyString); err != nil {
				return fmt.Errorf("failed to set encryption key: %w", err)
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

// getRSAPrivateKey retrieves the RSA private key based on the provided options.
// It handles two scenarios:
// 1. When a private key file is specified, it reads and decrypts the key from the file.
// 2. Otherwise, it decodes and parses the key from a base64-encoded string.
// Args:
// - opt: Options struct containing configuration for key retrieval.
// Returns:
// - *rsa.PrivateKey: The RSA private key object.
// - error: An error object if any issues occur during the retrieval process.
func getRSAPrivateKey(opt Options) (*rsa.PrivateKey, error) {
	var rsaPrivateKey *rsa.PrivateKey

	// Scenario 1: Private key is provided as a file
	if opt.OperatorKeyConfig.PrivateKeyFile != "" {
		// Read the encrypted private key from the file
		encryptedJSON, err := os.ReadFile(opt.OperatorKeyConfig.PrivateKeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read private key file: %w", err)
		}

		// Read the password for decrypting the private key
		keyStorePassword, err := os.ReadFile(opt.OperatorKeyConfig.PasswordFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read password file: %w", err)
		}

		// Decrypt and convert the private key from PEM format
		rsaPrivateKey, err = rsaencryption.ConvertEncryptedPemToPrivateKey(encryptedJSON, string(keyStorePassword))
		if err != nil {
			return nil, fmt.Errorf("failed to decrypt operator private key: %w", err)
		}
	} else {
		// Scenario 2: Private key is provided as a base64-encoded string

		// Decode the base64-encoded private key string
		operatorPrivKeyBytes, err := base64.StdEncoding.DecodeString(opt.OperatorKeyConfig.Base64EncodedPrivateKey)
		if err != nil {
			return nil, fmt.Errorf("failed to decode operator private key: %w", err)
		}

		// Parse the private key from the decoded byte array
		rsaPrivateKey, err = rsaencryption.ConvertPemToPrivateKey(string(operatorPrivKeyBytes))
		if err != nil {
			return nil, fmt.Errorf("failed to parse operator private key: %w", err)
		}
	}

	// Check if the retrieved private key is nil
	if rsaPrivateKey == nil {
		return nil, fmt.Errorf("rsa private key is nil")
	}

	return rsaPrivateKey, nil
}

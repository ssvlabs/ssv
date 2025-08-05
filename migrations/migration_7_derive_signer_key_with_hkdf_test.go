package migrations

import (
	"fmt"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/ssvlabs/eth2-key-manager/core"
	"github.com/ssvlabs/eth2-key-manager/wallets"
	"github.com/ssvlabs/eth2-key-manager/wallets/hd"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/ssvlabs/ssv/ssvsigner/ekm"
	"github.com/ssvlabs/ssv/ssvsigner/keys"

	"github.com/ssvlabs/ssv/logging"
	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/operator/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

func TestMigration7DeriveSignerKeyWithHKDF(t *testing.T) {
	require.NoError(t, bls.Init(bls.BLS12_381))

	t.Run("successfully migrates accounts with new key derivation", func(t *testing.T) {
		db, logger := setupTest(t)

		operatorPrivKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		nodeStorage, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
		require.NoError(t, err)
		require.NoError(t, nodeStorage.SavePrivateKeyHash(operatorPrivKey.StorageHash()))

		signerStorage := ekm.NewSignerStorage(db, networkconfig.TestNetwork.Beacon, logger)
		signerStorage.SetEncryptionKey(operatorPrivKey.EKMHash())

		wallet, accounts := createTestAccounts(t, signerStorage, 3)
		require.NoError(t, signerStorage.SaveWallet(wallet))

		options := Options{
			Db:              db,
			BeaconConfig:    networkconfig.TestNetwork.Beacon,
			OperatorPrivKey: operatorPrivKey,
		}

		err = migration_7_derive_signer_key_with_hkdf.Run(t.Context(), logger, options,
			[]byte(migration_7_derive_signer_key_with_hkdf.Name),
			func(rw basedb.ReadWriter) error { return nil })
		assert.NoError(t, err)

		encryptionKey, err := operatorPrivKey.EKMEncryptionKey()
		require.NoError(t, err)
		signerStorage.SetEncryptionKey(encryptionKey)

		retrievedAccounts, err := signerStorage.ListAccounts()
		require.NoError(t, err)
		assert.Equal(t, len(accounts), len(retrievedAccounts))

		// Verify old key no longer works
		signerStorage.SetEncryptionKey(operatorPrivKey.EKMHash())
		accounts, err = signerStorage.ListAccounts()
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "decrypt accounts:")
		assert.Empty(t, accounts)
	})

	t.Run("skips migration when no private key hash found", func(t *testing.T) {
		db, logger := setupTest(t)

		operatorPrivKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		options := Options{
			Db:              db,
			BeaconConfig:    networkconfig.TestNetwork.Beacon,
			OperatorPrivKey: operatorPrivKey,
		}

		completedExecuted := false
		err = migration_7_derive_signer_key_with_hkdf.Run(t.Context(), logger, options,
			[]byte(migration_7_derive_signer_key_with_hkdf.Name),
			func(rw basedb.ReadWriter) error {
				completedExecuted = true
				return nil
			})

		assert.NoError(t, err)
		assert.True(t, completedExecuted)
	})

	t.Run("skips migration when no accounts to migrate", func(t *testing.T) {
		db, logger := setupTest(t)

		operatorPrivKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		nodeStorage, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
		require.NoError(t, err)
		require.NoError(t, nodeStorage.SavePrivateKeyHash(operatorPrivKey.StorageHash()))

		signerStorage := ekm.NewSignerStorage(db, networkconfig.TestNetwork.Beacon, logger)
		wallet := hd.NewWallet(&core.WalletContext{Storage: signerStorage})
		require.NoError(t, signerStorage.SaveWallet(wallet))

		options := Options{
			Db:              db,
			BeaconConfig:    networkconfig.TestNetwork.Beacon,
			OperatorPrivKey: operatorPrivKey,
		}

		completedExecuted := false
		err = migration_7_derive_signer_key_with_hkdf.Run(t.Context(), logger, options,
			[]byte(migration_7_derive_signer_key_with_hkdf.Name),
			func(rw basedb.ReadWriter) error {
				completedExecuted = true
				return nil
			})

		assert.NoError(t, err)
		assert.True(t, completedExecuted)
	})

	t.Run("handles completion function error", func(t *testing.T) {
		db, logger := setupTest(t)

		operatorPrivKey, err := keys.GeneratePrivateKey()
		require.NoError(t, err)

		nodeStorage, err := storage.NewNodeStorage(networkconfig.TestNetwork.Beacon, logger, db)
		require.NoError(t, err)
		require.NoError(t, nodeStorage.SavePrivateKeyHash(operatorPrivKey.StorageHash()))

		signerStorage := ekm.NewSignerStorage(db, networkconfig.TestNetwork.Beacon, logger)
		signerStorage.SetEncryptionKey(operatorPrivKey.EKMHash())

		wallet, _ := createTestAccounts(t, signerStorage, 1)
		require.NoError(t, signerStorage.SaveWallet(wallet))

		options := Options{
			Db:              db,
			BeaconConfig:    networkconfig.TestNetwork.Beacon,
			OperatorPrivKey: operatorPrivKey,
		}

		err = migration_7_derive_signer_key_with_hkdf.Run(t.Context(), logger, options,
			[]byte(migration_7_derive_signer_key_with_hkdf.Name),
			func(rw basedb.ReadWriter) error {
				return fmt.Errorf("completion error")
			})

		assert.Error(t, err)
		assert.Contains(t, err.Error(), "completion error")
	})
}

func setupTest(t *testing.T) (basedb.Database, *zap.Logger) {
	t.Helper()

	logger := logging.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)

	t.Cleanup(func() {
		require.NoError(t, db.Close())
	})

	return db, logger
}

func createTestAccounts(t *testing.T, signerStorage ekm.Storage, count int) (core.Wallet, []core.ValidatorAccount) {
	t.Helper()

	wallet := hd.NewWallet(&core.WalletContext{Storage: signerStorage})

	var accounts []core.ValidatorAccount
	for i := 0; i < count; i++ {
		sk := &bls.SecretKey{}
		sk.SetByCSPRNG()

		key, err := core.NewHDKeyFromPrivateKey(sk.Serialize(), "")
		require.NoError(t, err)

		account := wallets.NewValidatorAccount(fmt.Sprintf("test%d", i), key, nil, "", nil)
		require.NoError(t, wallet.AddValidatorAccount(account))
		accounts = append(accounts, account)
	}

	return wallet, accounts
}

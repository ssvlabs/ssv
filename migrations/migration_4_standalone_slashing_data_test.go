package migrations

import (
	"context"
	"crypto/x509"
	"testing"

	"github.com/bloxapp/ssv-spec/types"
	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/rsaencryption"
	"github.com/bloxapp/ssv/utils/threshold"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
)

// TestSlashingProtectionMigration
// NodeDB = Exists
// SpDB = Doesn't Exist
// tests the migration of slashing protection data from a legacy database to a new standalone database.
func TestSlashingProtectionMigration(t *testing.T) {
	ctx, logger, network, db, spDB, km, spStorage, operatorPrivKey := setupCommon(t)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
		OperatorKeyConfig: OperatorKeyConfig{
			Base64EncodedPrivateKey: operatorPrivKey,
		},
	}

	require.NoError(t, migration_4_standalone_slashing_data.Run(
		ctx,
		logger,
		migrationOpts,
		[]byte(migration_4_standalone_slashing_data.Name),
		func(rw basedb.ReadWriter) error {
			return rw.Set(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name), migrationCompleted)
		},
	))

	obj, _, err := db.Get(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name))
	require.NoError(t, err)
	require.Equal(t, migrationCompleted, obj.Value)

	initialized, err := spStorage.IsInitialized()
	require.NoError(t, err)
	require.True(t, initialized)

	// Verification of migration
	// Check that the slashing protection storage has the same data as the legacy database
	ekmStorage := km.(ekm.StorageProvider)
	accounts, err := ekmStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	for _, account := range accounts {
		legacyHighAtt, found, err := ekmStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, legacyHighAtt)

		migratedHighAtt, found, err := spStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, migratedHighAtt)

		require.Equal(t, legacyHighAtt.Source.Epoch, migratedHighAtt.Source.Epoch)
		require.Equal(t, legacyHighAtt.Target.Epoch, migratedHighAtt.Target.Epoch)

		legacyHighProposal, found, err := ekmStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotZero(t, legacyHighProposal)

		migratedHighProposal, found, err := spStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotZero(t, migratedHighProposal)

		require.Equal(t, legacyHighProposal, migratedHighProposal)
	}
}

// TestSlashingProtectionMigration_NodeDB_SPDB_Exists
// NodeDB = Exists
// SlashingDB = Exists
// test that migration fails if the node & sp DBs already exists
func TestSlashingProtectionMigration_NodeDB_SPDB_Exists(t *testing.T) {
	ctx, logger, network, db, spDB, km, spStorage, operatorPrivKey := setupCommon(t)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	// populate slashing protection storage with some data
	err := spStorage.SaveHighestProposal([]byte("mock_pub_key"), network.Beacon.EstimatedCurrentSlot())
	require.NoError(t, err)

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
		OperatorKeyConfig: OperatorKeyConfig{
			Base64EncodedPrivateKey: operatorPrivKey,
		},
	}

	err = migration_4_standalone_slashing_data.Run(
		ctx,
		logger,
		migrationOpts,
		[]byte(migration_4_standalone_slashing_data.Name),
		func(rw basedb.ReadWriter) error {
			return rw.Set(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name), migrationCompleted)
		},
	)
	require.Error(t, err)
	require.Equal(t, "failed to check if slashing protection db is initialized: sp db is not empty but type not found", err.Error())

	_, found, err := db.Get(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name))
	require.NoError(t, err)
	require.False(t, found)

	// Verification of migration
	// Check that the slashing protection storage has the same data as the legacy database
	ekmStorage := km.(ekm.StorageProvider)
	accounts, err := ekmStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	for _, account := range accounts {
		legacyHighAtt, found, err := ekmStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, legacyHighAtt)

		migratedHighAtt, found, err := spStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, migratedHighAtt)

		legacyHighProposal, found, err := ekmStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotZero(t, legacyHighProposal)

		migratedHighProposal, found, err := spStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.False(t, found)
		require.Zero(t, migratedHighProposal)
	}
}

// TestSlashingProtectionMigration_NodeDB_SPDB_Exists
// NodeDB = Exists
// SlashingDB = Exists
// test that migration fails if the node & sp DBs already exists
func TestSlashingProtectionMigration_NodeDB_SPDB_Exists2(t *testing.T) {
	ctx, logger, network, db, spDB, km, spStorage, operatorPrivKey := setupCommon(t)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	// Simulate an incorrect db type by setting a different type
	err := spDB.SetType("IncorrectDBType")
	require.NoError(t, err)

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
		OperatorKeyConfig: OperatorKeyConfig{
			Base64EncodedPrivateKey: operatorPrivKey,
		},
	}

	err = migration_4_standalone_slashing_data.Run(
		ctx,
		logger,
		migrationOpts,
		[]byte(migration_4_standalone_slashing_data.Name),
		func(rw basedb.ReadWriter) error {
			return rw.Set(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name), migrationCompleted)
		},
	)
	require.Error(t, err)
	require.Equal(t, "failed to check if slashing protection db is initialized: sp db is not empty but the db type is incorrect IncorrectDBType", err.Error())

	_, found, err := db.Get(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name))
	require.NoError(t, err)
	require.False(t, found)

	// Verification of migration
	// Check that the slashing protection storage has the same data as the legacy database
	ekmStorage := km.(ekm.StorageProvider)
	accounts, err := ekmStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	for _, account := range accounts {
		legacyHighAtt, found, err := ekmStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, legacyHighAtt)

		migratedHighAtt, found, err := spStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, migratedHighAtt)

		legacyHighProposal, found, err := ekmStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotZero(t, legacyHighProposal)

		migratedHighProposal, found, err := spStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.False(t, found)
		require.Zero(t, migratedHighProposal)
	}
}

// TestSlashingProtectionMigration_NodeDB_SPDB_Exists
// NodeDB = Exists
// SlashingDB = Exists
// test that migration fails if the node & sp DBs already exists
func TestSlashingProtectionMigration_NodeDB_SPDB_Exists3(t *testing.T) {
	ctx, logger, network, db, spDB, km, spStorage, operatorPrivKey := setupCommon(t)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	// populate slashing protection storage with some data
	err := spStorage.Init()
	require.NoError(t, err)

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
		OperatorKeyConfig: OperatorKeyConfig{
			Base64EncodedPrivateKey: operatorPrivKey,
		},
	}

	err = migration_4_standalone_slashing_data.Run(
		ctx,
		logger,
		migrationOpts,
		[]byte(migration_4_standalone_slashing_data.Name),
		func(rw basedb.ReadWriter) error {
			return rw.Set(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name), migrationCompleted)
		},
	)
	require.Error(t, err)
	require.Equal(t, "can not migrate legacy slashing protection data over existing slashing protection data", err.Error())

	_, found, err := db.Get(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name))
	require.NoError(t, err)
	require.False(t, found)

	// Verification of migration
	// Check that the slashing protection storage has the same data as the legacy database
	ekmStorage := km.(ekm.StorageProvider)
	accounts, err := ekmStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 2)

	for _, account := range accounts {
		legacyHighAtt, found, err := ekmStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotNil(t, legacyHighAtt)

		migratedHighAtt, found, err := spStorage.RetrieveHighestAttestation(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, migratedHighAtt)

		legacyHighProposal, found, err := ekmStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.True(t, found)
		require.NotZero(t, legacyHighProposal)

		migratedHighProposal, found, err := spStorage.RetrieveHighestProposal(account.ValidatorPublicKey())
		require.NoError(t, err)
		require.False(t, found)
		require.Zero(t, migratedHighProposal)
	}
}

// TestSlashingProtectionMigration_NodeDB_SPDB_Does_Not_Exists
// NodeDB = Doesn't Exist
// SlashingDB = Doesn't Exist
// test that migration complete without error
func TestSlashingProtectionMigration_NodeDB_SPDB_Does_Not_Exists(t *testing.T) {
	ctx, logger, network, db, spDB, km, spStorage, operatorPrivKey := setupCommon(t)

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
		OperatorKeyConfig: OperatorKeyConfig{
			Base64EncodedPrivateKey: operatorPrivKey,
		},
	}

	require.NoError(t, migration_4_standalone_slashing_data.Run(
		ctx,
		logger,
		migrationOpts,
		[]byte(migration_4_standalone_slashing_data.Name),
		func(rw basedb.ReadWriter) error {
			return rw.Set(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name), migrationCompleted)
		},
	))

	obj, _, err := db.Get(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name))
	require.NoError(t, err)
	require.Equal(t, migrationCompleted, obj.Value)

	initialized, err := spStorage.IsInitialized()
	require.NoError(t, err)
	require.True(t, initialized)

	// Verification of migration
	// Check that the slashing protection storage has the same data as the legacy database
	ekmStorage := km.(ekm.StorageProvider)
	accounts, err := ekmStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 0)

	empty, err := spDB.IsEmpty()
	require.NoError(t, err)
	require.False(t, empty)
}

// TestSlashingProtectionMigration_NodeDB_DoesNot_Exists_SPDB_Exists
// NodeDB = Doesn't Exist
// SlashingDB = Exist
// test that migration complete without error
func TestSlashingProtectionMigration_NodeDB_DoesNot_Exists_SPDB_Exists(t *testing.T) {
	ctx, logger, network, db, spDB, km, spStorage, operatorPrivKey := setupCommon(t)
	// populate slashing protection storage with some data
	err := spStorage.Init()
	require.NoError(t, err)

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
		OperatorKeyConfig: OperatorKeyConfig{
			Base64EncodedPrivateKey: operatorPrivKey,
		},
	}

	require.NoError(t, migration_4_standalone_slashing_data.Run(
		ctx,
		logger,
		migrationOpts,
		[]byte(migration_4_standalone_slashing_data.Name),
		func(rw basedb.ReadWriter) error {
			return rw.Set(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name), migrationCompleted)
		},
	))

	obj, _, err := db.Get(migrationsPrefix, []byte(migration_4_standalone_slashing_data.Name))
	require.NoError(t, err)
	require.Equal(t, migrationCompleted, obj.Value)

	initialized, err := spStorage.IsInitialized()
	require.NoError(t, err)
	require.True(t, initialized)

	// Verification of migration
	// Check that the slashing protection storage has the same data as the legacy database
	ekmStorage := km.(ekm.StorageProvider)
	accounts, err := ekmStorage.ListAccounts()
	require.NoError(t, err)
	require.Len(t, accounts, 0)

	empty, err := spDB.IsEmpty()
	require.NoError(t, err)
	require.False(t, empty)
}

func setupCommon(t *testing.T) (context.Context, *zap.Logger, *networkconfig.NetworkConfig, *kv.BadgerDB, *kv.BadgerDB, types.KeyManager, ekm.SPStorage, string) {
	// Initialization
	threshold.Init()
	ctx := context.Background()
	logger := logging.TestLogger(t)

	// Database setup
	nodeDBPath := t.TempDir()
	db, err := kv.NewInMemory(ctx, logger, basedb.Options{
		Path: nodeDBPath,
	})
	require.NoError(t, err)

	_, sk, err := rsaencryption.GenerateKeys()
	require.NoError(t, err)

	rsaPriv, err := rsaencryption.ConvertPemToPrivateKey(string(sk))
	require.NoError(t, err)

	keyBytes := x509.MarshalPKCS1PrivateKey(rsaPriv)
	hashedKey, err := rsaencryption.HashRsaKey(keyBytes)
	require.NoError(t, err)

	base64EncodedKey := rsaencryption.ExtractPrivateKey(rsaPriv)

	// Network configuration
	network := &networkconfig.NetworkConfig{
		Beacon: utils.SetupMockBeaconNetwork(t, nil),
		Domain: networkconfig.TestNetwork.Domain,
	}

	// Key management setup
	km, err := ekm.NewETHKeyManagerSigner(logger, db, db, *network, true, hashedKey)
	require.NoError(t, err)

	// Standalone slashing protection storage setup
	spDBPath := t.TempDir()
	options := basedb.Options{
		Path:       spDBPath,
		SyncWrites: true,
	}

	spDB, err := kv.NewInMemory(ctx, logger, options)
	require.NoError(t, err)
	spStorage := ekm.NewSlashingProtectionStorage(spDB, logger, []byte(network.Beacon.GetBeaconNetwork()))

	return ctx, logger, network, db, spDB, km, spStorage, base64EncodedKey
}

package migrations

import (
	"context"
	"testing"

	"github.com/herumi/bls-eth-go-binary/bls"
	"github.com/stretchr/testify/require"

	"github.com/bloxapp/ssv/ekm"
	"github.com/bloxapp/ssv/logging"
	"github.com/bloxapp/ssv/networkconfig"
	"github.com/bloxapp/ssv/storage/basedb"
	"github.com/bloxapp/ssv/storage/kv"
	"github.com/bloxapp/ssv/utils"
	"github.com/bloxapp/ssv/utils/threshold"
)

const (
	sk1Str = "3548db63ab5701878daf25fa877638dc7809778815b9d9ecd5369da33ca9e64f"
	sk2Str = "66dd37ae71b35c81022cdde98370e881cff896b689fa9136917f45afce43fd3b"
)

// TestSlashingProtectionMigration tests the migration of slashing protection data from a legacy database to a new standalone database.
func TestSlashingProtectionMigration(t *testing.T) {
	// Initialization
	threshold.Init()
	ctx := context.Background()
	logger := logging.TestLogger(t)

	// Database setup
	nodeDBPath := t.TempDir()
	db, err := kv.New(ctx, logger, basedb.Options{
		Path: nodeDBPath,
	})
	require.NoError(t, err)
	// simulate legacy db
	legacyDb := db

	// Network configuration
	network := &networkconfig.NetworkConfig{
		Beacon: utils.SetupMockBeaconNetwork(t, nil),
		Domain: networkconfig.TestNetwork.Domain,
	}

	// Key management setup
	km, err := ekm.NewETHKeyManagerSigner(logger, db, legacyDb, *network, true, "")
	require.NoError(t, err)

	sk1 := &bls.SecretKey{}
	require.NoError(t, sk1.SetHexString(sk1Str))

	sk2 := &bls.SecretKey{}
	require.NoError(t, sk2.SetHexString(sk2Str))

	require.NoError(t, km.AddShare(sk1))
	require.NoError(t, km.AddShare(sk2))

	// Standalone slashing protection storage setup
	spDBPath := t.TempDir()
	options := basedb.Options{
		Path:       spDBPath,
		SyncWrites: true,
	}

	spDB, err := kv.New(ctx, logger, options)
	require.NoError(t, err)
	spStorage := ekm.NewSlashingProtectionStorage(spDB, logger, []byte(network.Beacon.GetBeaconNetwork()))

	// Migration process
	migrationOpts := Options{
		Db:      db,
		SpDb:    spDB,
		Network: network.Beacon,
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

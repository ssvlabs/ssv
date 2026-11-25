package storage

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"math/big"
	"testing"

	"github.com/attestantio/go-eth2-client/spec/bellatrix"
	"github.com/dgraph-io/badger/v4"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"

	spectypes "github.com/ssvlabs/ssv-spec/types"

	"github.com/ssvlabs/ssv/observability/log"

	"github.com/ssvlabs/ssv/networkconfig"
	"github.com/ssvlabs/ssv/protocol/v2/types"
	registrystorage "github.com/ssvlabs/ssv/registry/storage"
	kv "github.com/ssvlabs/ssv/storage/badger"
	"github.com/ssvlabs/ssv/storage/basedb"
)

var (
	network = networkconfig.TestNetwork
)

func TestOperatorKeyCheckValue(t *testing.T) {
	db, err := kv.NewInMemory(log.TestLogger(t), basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	operatorStorage := storage{
		db: db,
	}

	_, found, err := operatorStorage.GetOperatorKeyCheckValue()
	require.NoError(t, err)
	require.False(t, found)

	checkValue := []byte{1, 2, 3}
	require.NoError(t, operatorStorage.SaveOperatorKeyCheckValue(checkValue))
	stored, found, err := operatorStorage.GetOperatorKeyCheckValue()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, checkValue, stored)
}

func TestDropPrivateKeyHash(t *testing.T) {
	db, err := kv.NewInMemory(log.TestLogger(t), basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	operatorStorage := storage{
		db: db,
	}

	// Two versions, as an older version that rewrites the hash leaves.
	hash := []byte{1, 2, 3}
	for range 2 {
		require.NoError(t, db.Set(OperatorStoragePrefix, []byte(hashedPrivkeyDBKey), []byte(hex.EncodeToString(hash))))
	}
	stored, found, err := operatorStorage.GetPrivateKeyHash()
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, hash, stored)

	require.NoError(t, operatorStorage.DropPrivateKeyHash())

	_, found, err = operatorStorage.GetPrivateKeyHash()
	require.NoError(t, err)
	require.False(t, found)

	var versions int
	require.NoError(t, db.Badger().View(func(txn *badger.Txn) error {
		opts := badger.DefaultIteratorOptions
		opts.AllVersions = true
		opts.Prefix = []byte(string(OperatorStoragePrefix) + hashedPrivkeyDBKey)
		it := txn.NewIterator(opts)
		defer it.Close()

		for it.Rewind(); it.Valid(); it.Next() {
			versions++
		}
		return nil
	}))
	require.Zero(t, versions)
}

// TestDropPrivateKeyHashStillStored covers badger failing to compact level 0: its DropPrefix returns
// nil but leaves the hash.
func TestDropPrivateKeyHashStillStored(t *testing.T) {
	db, err := kv.NewInMemory(log.TestLogger(t), basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	operatorStorage := storage{
		db: ignoreDropPrefixDB{db},
	}

	require.NoError(t, db.Set(OperatorStoragePrefix, []byte(hashedPrivkeyDBKey), []byte(hex.EncodeToString([]byte{1, 2, 3}))))
	require.ErrorContains(t, operatorStorage.DropPrivateKeyHash(), "private key hash still stored")
}

// ignoreDropPrefixDB returns nil from DropPrefix without dropping anything.
type ignoreDropPrefixDB struct {
	basedb.Database
}

func (ignoreDropPrefixDB) DropPrefix([]byte) error { return nil }

func TestDropRegistryData(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	storage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)

	sharePubkey := func(id int) []byte {
		v := make([]byte, 48)
		_, err := rand.Read(v)
		require.NoError(t, err)
		v[0] = byte(id) // makes sure every generated pubkey is unique
		return v
	}
	// Save operators, shares and recipients.
	var (
		operatorIDs     = []uint64{1, 2, 3}
		sharePubKeys    = [][]byte{sharePubkey(1), sharePubkey(2), sharePubkey(3)}
		recipientOwners = []common.Address{{1}, {2}, {3}}
	)
	for _, id := range operatorIDs {
		found, err := storage.SaveOperatorData(nil, &registrystorage.OperatorData{
			ID:           id,
			PublicKey:    base64.StdEncoding.EncodeToString([]byte("publicKey")),
			OwnerAddress: common.Address{byte(id)},
		})
		require.NoError(t, err)
		require.False(t, found)

		found, err = storage.OperatorsExist(nil, []spectypes.OperatorID{id})
		require.NoError(t, err)
		require.True(t, found)
	}
	for _, pk := range sharePubKeys {
		err := storage.Shares().Save(nil, &types.SSVShare{
			Share: spectypes.Share{
				SharePubKey:     pk,
				ValidatorPubKey: spectypes.ValidatorPK(pk),
			},
		})
		require.NoError(t, err)
	}
	for _, owner := range recipientOwners {
		var fr bellatrix.ExecutionAddress
		copy(fr[:], append([]byte{1}, owner[:]...))
		_, err := storage.SaveRecipientData(nil, owner, fr)
		require.NoError(t, err)
	}

	// Check that everything was saved.
	requireSaved := func(t *testing.T, operators, shares, recipients int) {
		allOperators, err := storage.ListOperators(nil, 0, 0)
		require.NoError(t, err)
		require.Len(t, allOperators, operators)

		allShares := storage.Shares().List(nil)
		require.NoError(t, err)
		require.Len(t, allShares, shares)

		// Check that all recipients were saved
		recipientCount := 0
		for _, owner := range recipientOwners {
			_, err := storage.GetFeeRecipient(owner)
			if err == nil {
				recipientCount++
			}
		}
		require.Equal(t, recipients, recipientCount)
	}
	requireSaved(t, len(operatorIDs), len(sharePubKeys), len(recipientOwners))

	// Re-open storage and check again that everything is still saved.
	// Re-opening helps ensure that the changes were persisted and not just cached.
	storage, err = NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)
	requireSaved(t, len(operatorIDs), len(sharePubKeys), len(recipientOwners))

	// Drop registry data.
	err = storage.DropRegistryData()
	require.NoError(t, err)

	// Check that everything was dropped.
	requireSaved(t, 0, 0, 0)

	// Re-open storage and check again that everything is still dropped.
	storage, err = NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)
}

func TestNetworkAndLocalEventsConfig(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	require.NoError(t, err)
	defer func() {
		_ = db.Close()
	}()

	storage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)

	storedCfg, found, err := storage.GetConfig(nil)
	require.NoError(t, err)
	require.False(t, found)
	require.Nil(t, storedCfg)

	c1 := &ConfigLock{
		NetworkName:      networkconfig.TestNetwork.StorageName(),
		UsingLocalEvents: false,
	}
	require.NoError(t, storage.SaveConfig(nil, c1))

	storedCfg, found, err = storage.GetConfig(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, c1, storedCfg)

	c2 := &ConfigLock{
		NetworkName:      networkconfig.TestNetwork.StorageName() + "1",
		UsingLocalEvents: false,
	}
	require.NoError(t, storage.SaveConfig(nil, c2))

	storedCfg, found, err = storage.GetConfig(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, c2, storedCfg)
}

func TestGetOperatorsPrefix(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)
	require.Equal(t, []byte("operators"), operatorStorage.GetOperatorsPrefix())
}

func Test_Config(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)

	cfgData := &ConfigLock{
		NetworkName:      "test",
		UsingLocalEvents: false,
	}

	err = operatorStorage.SaveConfig(nil, cfgData)
	require.NoError(t, err)

	cfg, validAndFound, err := operatorStorage.GetConfig(nil)
	require.NoError(t, err)
	require.True(t, validAndFound)
	require.NotNil(t, cfg)
	require.Equal(t, cfgData.NetworkName, cfg.NetworkName)
	require.Equal(t, cfgData.UsingLocalEvents, cfg.UsingLocalEvents)

	require.NoError(t, operatorStorage.DeleteConfig(nil))

	cfg, validAndFound, err = operatorStorage.GetConfig(nil)
	require.NoError(t, err)
	require.False(t, validAndFound)
	require.Nil(t, cfg)
}

func Test_LastProcessedBlock(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)

	_, found, err := operatorStorage.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.False(t, found)

	err = operatorStorage.SaveLastProcessedBlock(nil, big.NewInt(123))
	require.NoError(t, err)

	blockNum, found, err := operatorStorage.GetLastProcessedBlock(nil)
	require.NoError(t, err)
	require.True(t, found)
	require.Equal(t, *big.NewInt(123), *blockNum)
}

func Test_OperatorData(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)

	operatorIDs := []uint64{1, 2, 3}

	for _, id := range operatorIDs {
		pubkey := base64.StdEncoding.EncodeToString(fmt.Appendf(nil, "publicKey%d", id))
		operatorData := &registrystorage.OperatorData{
			ID:           id,
			PublicKey:    pubkey,
			OwnerAddress: common.Address{byte(id)},
		}

		found, err := operatorStorage.SaveOperatorData(nil, operatorData)
		require.NoError(t, err)
		require.False(t, found)

		opData, found, err := operatorStorage.GetOperatorData(nil, id)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, *operatorData, *opData)

		opData, found, err = operatorStorage.GetOperatorDataByPubKey(nil, pubkey)
		require.NoError(t, err)
		require.True(t, found)
		require.Equal(t, *operatorData, *opData)

		err = operatorStorage.DeleteOperatorData(nil, id)
		require.NoError(t, err)

		opData, found, err = operatorStorage.GetOperatorData(nil, id)
		require.NoError(t, err)
		require.False(t, found)
		require.Nil(t, opData)
	}
}

func Test_NonceBumping(t *testing.T) {
	logger := log.TestLogger(t)
	db, err := kv.NewInMemory(logger, basedb.Options{})
	defer func() {
		_ = db.Close()
	}()

	require.NoError(t, err)

	operatorStorage, err := NewNodeStorage(network.Beacon, logger, db)
	require.NoError(t, err)

	owner := common.Address{1}

	var fr bellatrix.ExecutionAddress
	copy(fr[:], append([]byte{1}, owner[:]...))

	_, err = operatorStorage.SaveRecipientData(nil, owner, fr)
	require.NoError(t, err)

	// Check that fee recipient was saved
	recipient, err := operatorStorage.GetFeeRecipient(owner)
	require.NoError(t, err)
	require.Equal(t, fr, recipient)

	require.NoError(t, operatorStorage.BumpNonce(nil, owner))
	require.NoError(t, operatorStorage.BumpNonce(nil, owner))
	nonce, err := operatorStorage.GetNextNonce(nil, owner)
	require.NoError(t, err)
	require.Equal(t, registrystorage.Nonce(2), nonce)

	err = operatorStorage.DeleteRecipientData(nil, owner)
	require.NoError(t, err)

	// Check that fee recipient was deleted
	_, err = operatorStorage.GetFeeRecipient(owner)
	require.Error(t, err)

	nonce, err = operatorStorage.GetNextNonce(nil, owner)
	require.NoError(t, err)
	require.Equal(t, registrystorage.Nonce(0), nonce)
}
